package ledger

import (
	"sort"
	"time"
)

const (
	ConsensusActionProposal     = "proposal"
	ConsensusActionVote         = "vote"
	ConsensusActionRoundAdvance = "round_advance"
	ConsensusActionBlockImport  = "block_import"
	ConsensusActionSnapshotSync = "snapshot_restore"

	ConsensusActionPending    = "pending"
	ConsensusActionCompleted  = "completed"
	ConsensusActionSuperseded = "superseded"

	consensusActionHistoryLimit = 32
	consensusActionRecentLimit  = 10
)

type ConsensusAction struct {
	Type           string     `json:"type"`
	Height         uint64     `json:"height"`
	Round          uint64     `json:"round"`
	BlockHash      string     `json:"blockHash,omitempty"`
	Validator      string     `json:"validator,omitempty"`
	RecordedAt     time.Time  `json:"recordedAt"`
	LastReplayAt   *time.Time `json:"lastReplayAt,omitempty"`
	ReplayAttempts int        `json:"replayAttempts"`
	Status         string     `json:"status"`
	Note           string     `json:"note,omitempty"`
	CompletedAt    *time.Time `json:"completedAt,omitempty"`
}

type ConsensusRecoveryView struct {
	PendingActionCount           int               `json:"pendingActionCount"`
	PendingReplayCount           int               `json:"pendingReplayCount"`
	PendingImportCount           int               `json:"pendingImportCount"`
	PendingImportHeights         []uint64          `json:"pendingImportHeights"`
	NeedsReplay                  bool              `json:"needsReplay"`
	NeedsRecovery                bool              `json:"needsRecovery"`
	LastSnapshotRestoreAt        *time.Time        `json:"lastSnapshotRestoreAt,omitempty"`
	LastSnapshotRestoreHeight    uint64            `json:"lastSnapshotRestoreHeight,omitempty"`
	LastSnapshotRestoreBlockHash string            `json:"lastSnapshotRestoreBlockHash,omitempty"`
	PendingActions               []ConsensusAction `json:"pendingActions"`
	RecentActions                []ConsensusAction `json:"recentActions"`
}

func (s *Store) ConsensusRecovery() ConsensusRecoveryView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusRecoveryFromState(s.snapshotLocked())
}

func (s *Store) RecordConsensusAction(action ConsensusAction) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	state = recordConsensusActionIntoState(state, action)
	if err := s.writeState(state); err != nil {
		return err
	}

	s.applyStateLocked(state)
	return nil
}

func (s *Store) MarkConsensusActionReplayed(actionType string, height uint64, round uint64, blockHash string, validator string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	state = markConsensusActionReplayedInState(state, actionType, height, round, blockHash, validator, now)
	if err := s.writeState(state); err != nil {
		return err
	}

	s.applyStateLocked(state)
	return nil
}

func consensusRecoveryFromState(state persistedState) ConsensusRecoveryView {
	state = normalizeState(state)
	view := ConsensusRecoveryView{
		PendingImportHeights: make([]uint64, 0),
		PendingActions:       make([]ConsensusAction, 0),
		RecentActions:        make([]ConsensusAction, 0),
	}
	pendingImportHeights := make(map[uint64]struct{})

	for index := len(state.ConsensusActions) - 1; index >= 0; index-- {
		action := cloneConsensusAction(state.ConsensusActions[index])
		if action.Status == ConsensusActionPending {
			view.PendingActions = append(view.PendingActions, action)
			switch action.Type {
			case ConsensusActionProposal, ConsensusActionVote:
				view.PendingReplayCount++
			case ConsensusActionBlockImport:
				view.PendingImportCount++
				pendingImportHeights[action.Height] = struct{}{}
			}
		}
		if len(view.RecentActions) < consensusActionRecentLimit {
			view.RecentActions = append(view.RecentActions, action)
		}
		if view.LastSnapshotRestoreAt == nil && action.Type == ConsensusActionSnapshotSync {
			view.LastSnapshotRestoreAt = cloneTimePointer(action.CompletedAt)
			if view.LastSnapshotRestoreAt == nil {
				view.LastSnapshotRestoreAt = cloneNonZeroTimePointer(action.RecordedAt)
			}
			view.LastSnapshotRestoreHeight = action.Height
			view.LastSnapshotRestoreBlockHash = action.BlockHash
		}
	}

	view.PendingActionCount = len(view.PendingActions)
	for height := range pendingImportHeights {
		view.PendingImportHeights = append(view.PendingImportHeights, height)
	}
	sort.Slice(view.PendingImportHeights, func(i, j int) bool {
		return view.PendingImportHeights[i] < view.PendingImportHeights[j]
	})
	view.NeedsReplay = view.PendingReplayCount > 0
	view.NeedsRecovery = view.PendingActionCount > 0
	return view
}

func recordConsensusActionIntoState(state persistedState, action ConsensusAction) persistedState {
	state = normalizeState(state)
	if action.RecordedAt.IsZero() {
		action.RecordedAt = time.Now().UTC()
	}
	action.RecordedAt = action.RecordedAt.UTC()
	if action.Status == "" {
		switch action.Type {
		case ConsensusActionProposal, ConsensusActionVote, ConsensusActionBlockImport:
			action.Status = ConsensusActionPending
		default:
			action.Status = ConsensusActionCompleted
		}
	}
	if action.Status != ConsensusActionPending && action.CompletedAt == nil {
		completedAt := action.RecordedAt
		action.CompletedAt = &completedAt
	}

	for index, existing := range state.ConsensusActions {
		if !sameConsensusAction(existing, action) {
			continue
		}
		state.ConsensusActions[index] = mergeConsensusAction(existing, action)
		state.ConsensusActions = compactConsensusActions(state.ConsensusActions)
		return state
	}

	if action.Status == ConsensusActionPending && action.Validator != "" {
		for index, existing := range state.ConsensusActions {
			if existing.Type != action.Type || existing.Validator != action.Validator || existing.Height != action.Height || existing.Status != ConsensusActionPending {
				continue
			}
			completedAt := action.RecordedAt
			existing.Status = ConsensusActionSuperseded
			existing.CompletedAt = &completedAt
			if existing.Note == "" {
				existing.Note = "superseded by later local action"
			}
			state.ConsensusActions[index] = existing
		}
	}

	state.ConsensusActions = append(state.ConsensusActions, cloneConsensusAction(action))
	state.ConsensusActions = compactConsensusActions(state.ConsensusActions)
	return state
}

func markConsensusActionReplayedInState(state persistedState, actionType string, height uint64, round uint64, blockHash string, validator string, now time.Time) persistedState {
	state = normalizeState(state)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	for index := len(state.ConsensusActions) - 1; index >= 0; index-- {
		action := state.ConsensusActions[index]
		if action.Type != actionType || action.Height != height || action.Round != round || action.BlockHash != blockHash || action.Validator != validator {
			continue
		}
		action.ReplayAttempts++
		action.LastReplayAt = cloneNonZeroTimePointer(now)
		state.ConsensusActions[index] = action
		break
	}
	state.ConsensusActions = compactConsensusActions(state.ConsensusActions)
	return state
}

func completeConsensusActionsForHeightInState(state persistedState, height uint64, now time.Time, note string) persistedState {
	state = normalizeState(state)
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	for index, action := range state.ConsensusActions {
		if action.Status != ConsensusActionPending || action.Height > height {
			continue
		}
		action.Status = ConsensusActionCompleted
		action.CompletedAt = cloneNonZeroTimePointer(now)
		if action.Note == "" && note != "" {
			action.Note = note
		}
		state.ConsensusActions[index] = action
	}
	state.ConsensusActions = compactConsensusActions(state.ConsensusActions)
	return state
}

func compactConsensusActions(actions []ConsensusAction) []ConsensusAction {
	if actions == nil {
		return make([]ConsensusAction, 0)
	}
	if len(actions) <= consensusActionHistoryLimit {
		return cloneConsensusActions(actions)
	}

	pending := make([]ConsensusAction, 0)
	history := make([]ConsensusAction, 0)
	for _, action := range actions {
		if action.Status == ConsensusActionPending {
			pending = append(pending, cloneConsensusAction(action))
			continue
		}
		history = append(history, cloneConsensusAction(action))
	}
	keepHistory := consensusActionHistoryLimit - len(pending)
	if keepHistory < 0 {
		keepHistory = 0
	}
	if len(history) > keepHistory {
		history = history[len(history)-keepHistory:]
	}
	return append(pending, history...)
}

func normalizeConsensusActions(actions []ConsensusAction) []ConsensusAction {
	if actions == nil {
		return make([]ConsensusAction, 0)
	}
	cloned := make([]ConsensusAction, len(actions))
	for i, action := range actions {
		cloned[i] = cloneConsensusAction(action)
	}
	return compactConsensusActions(cloned)
}

func cloneConsensusAction(action ConsensusAction) ConsensusAction {
	return ConsensusAction{
		Type:           action.Type,
		Height:         action.Height,
		Round:          action.Round,
		BlockHash:      action.BlockHash,
		Validator:      action.Validator,
		RecordedAt:     action.RecordedAt,
		LastReplayAt:   cloneTimePointer(action.LastReplayAt),
		ReplayAttempts: action.ReplayAttempts,
		Status:         action.Status,
		Note:           action.Note,
		CompletedAt:    cloneTimePointer(action.CompletedAt),
	}
}

func cloneConsensusActions(actions []ConsensusAction) []ConsensusAction {
	cloned := make([]ConsensusAction, len(actions))
	for i, action := range actions {
		cloned[i] = cloneConsensusAction(action)
	}
	return cloned
}

func sameConsensusAction(left ConsensusAction, right ConsensusAction) bool {
	return left.Type == right.Type &&
		left.Height == right.Height &&
		left.Round == right.Round &&
		left.BlockHash == right.BlockHash &&
		left.Validator == right.Validator
}

func mergeConsensusAction(existing ConsensusAction, incoming ConsensusAction) ConsensusAction {
	merged := cloneConsensusAction(existing)
	if merged.RecordedAt.IsZero() || (!incoming.RecordedAt.IsZero() && incoming.RecordedAt.Before(merged.RecordedAt)) {
		merged.RecordedAt = incoming.RecordedAt
	}
	if incoming.LastReplayAt != nil {
		merged.LastReplayAt = cloneTimePointer(incoming.LastReplayAt)
	}
	if incoming.ReplayAttempts > merged.ReplayAttempts {
		merged.ReplayAttempts = incoming.ReplayAttempts
	}
	if incoming.Status != "" {
		merged.Status = incoming.Status
	}
	if incoming.Note != "" {
		merged.Note = incoming.Note
	}
	if incoming.CompletedAt != nil {
		merged.CompletedAt = cloneTimePointer(incoming.CompletedAt)
	}
	return merged
}

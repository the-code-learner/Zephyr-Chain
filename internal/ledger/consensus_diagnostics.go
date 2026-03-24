package ledger

import "time"

const consensusDiagnosticRecentLimit = 20

type ConsensusDiagnostic struct {
	Kind      string    `json:"kind"`
	Code      string    `json:"code"`
	Message   string    `json:"message"`
	Height    uint64    `json:"height,omitempty"`
	Round     uint64    `json:"round,omitempty"`
	BlockHash string    `json:"blockHash,omitempty"`
	Validator string    `json:"validator,omitempty"`
	Source    string    `json:"source,omitempty"`
	ObservedAt time.Time `json:"observedAt"`
}

type ConsensusDiagnosticsView struct {
	Recent []ConsensusDiagnostic `json:"recent"`
}

func (s *Store) ConsensusDiagnostics() ConsensusDiagnosticsView {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return consensusDiagnosticsFromState(s.snapshotLocked())
}

func (s *Store) RecordConsensusDiagnostic(diagnostic ConsensusDiagnostic) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	state := s.snapshotLocked()
	state = recordConsensusDiagnosticIntoState(state, diagnostic)
	if err := s.writeState(state); err != nil {
		return err
	}

	s.applyStateLocked(state)
	return nil
}

func consensusDiagnosticsFromState(state persistedState) ConsensusDiagnosticsView {
	state = normalizeState(state)
	recent := cloneConsensusDiagnostics(state.ConsensusDiagnostics)
	if len(recent) > consensusDiagnosticRecentLimit {
		recent = recent[len(recent)-consensusDiagnosticRecentLimit:]
	}
	for left, right := 0, len(recent)-1; left < right; left, right = left+1, right-1 {
		recent[left], recent[right] = recent[right], recent[left]
	}
	return ConsensusDiagnosticsView{Recent: recent}
}

func recordConsensusDiagnosticIntoState(state persistedState, diagnostic ConsensusDiagnostic) persistedState {
	state = normalizeState(state)
	if diagnostic.ObservedAt.IsZero() {
		diagnostic.ObservedAt = time.Now().UTC()
	}
	diagnostic.ObservedAt = diagnostic.ObservedAt.UTC()
	state.ConsensusDiagnostics = append(state.ConsensusDiagnostics, cloneConsensusDiagnostic(diagnostic))
	if len(state.ConsensusDiagnostics) > consensusDiagnosticRecentLimit {
		state.ConsensusDiagnostics = cloneConsensusDiagnostics(state.ConsensusDiagnostics[len(state.ConsensusDiagnostics)-consensusDiagnosticRecentLimit:])
	} else {
		state.ConsensusDiagnostics = cloneConsensusDiagnostics(state.ConsensusDiagnostics)
	}
	return state
}

func normalizeConsensusDiagnostics(diagnostics []ConsensusDiagnostic) []ConsensusDiagnostic {
	if diagnostics == nil {
		return make([]ConsensusDiagnostic, 0)
	}
	cloned := cloneConsensusDiagnostics(diagnostics)
	if len(cloned) > consensusDiagnosticRecentLimit {
		cloned = cloned[len(cloned)-consensusDiagnosticRecentLimit:]
	}
	return cloned
}

func cloneConsensusDiagnostic(diagnostic ConsensusDiagnostic) ConsensusDiagnostic {
	return ConsensusDiagnostic{
		Kind:       diagnostic.Kind,
		Code:       diagnostic.Code,
		Message:    diagnostic.Message,
		Height:     diagnostic.Height,
		Round:      diagnostic.Round,
		BlockHash:  diagnostic.BlockHash,
		Validator:  diagnostic.Validator,
		Source:     diagnostic.Source,
		ObservedAt: diagnostic.ObservedAt,
	}
}

func cloneConsensusDiagnostics(diagnostics []ConsensusDiagnostic) []ConsensusDiagnostic {
	cloned := make([]ConsensusDiagnostic, len(diagnostics))
	for i, diagnostic := range diagnostics {
		cloned[i] = cloneConsensusDiagnostic(diagnostic)
	}
	return cloned
}

package ledger

import "time"

func (s *Store) RestoreFromPeerSnapshot(snapshot Snapshot, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	incoming := persistedFromSnapshot(snapshot)
	localState := s.snapshotLocked()
	incoming.ConsensusActions = normalizeConsensusActions(localState.ConsensusActions)
	incoming.ConsensusDiagnostics = normalizeConsensusDiagnostics(localState.ConsensusDiagnostics)
	incoming = completeConsensusActionsForHeightInState(incoming, uint64(len(incoming.Blocks)), now, "state restored from peer snapshot")
	if err := s.writeState(incoming); err != nil {
		return err
	}

	s.applyStateLocked(incoming)
	return nil
}

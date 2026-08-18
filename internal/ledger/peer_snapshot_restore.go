package ledger

import "time"

// RestoreFromPeerSnapshot is intentionally disabled. Peer snapshots require
// quorum proofs from the locally trusted validator set.
func (s *Store) RestoreFromPeerSnapshot(snapshot Snapshot, now time.Time) error {
	return ErrSnapshotQuorumRequired
}

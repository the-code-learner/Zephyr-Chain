package api

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type peerSyncResult struct {
	Attempted                bool
	UsedSnapshot             bool
	ImportErrorCode          string
	ImportErrorMessage       string
	ImportFailureAt          *time.Time
	ImportFailureHeight      uint64
	ImportFailureBlockHash   string
	SnapshotRestoreAt        *time.Time
	SnapshotRestoreHeight    uint64
	SnapshotRestoreBlockHash string
	SnapshotRestoreReason    string
}

type peerSnapshotRestoreResult struct {
	Applied    bool
	RestoredAt time.Time
	Height     uint64
	BlockHash  string
	Reason     string
}

func derivePeerSyncState(local ledger.StatusView, remote ledger.StatusView) (string, int64, bool) {
	heightDelta := int64(remote.Height) - int64(local.Height)
	switch {
	case remote.Height > local.Height:
		return "peer_ahead", heightDelta, true
	case remote.Height < local.Height:
		return "peer_behind", heightDelta, false
	case remote.Height > 0 && remote.LatestBlockHash != local.LatestBlockHash:
		return "diverged", 0, true
	case remote.MempoolSize > local.MempoolSize:
		return "peer_ahead", 0, true
	default:
		return "aligned", 0, false
	}
}

func mergePeerSyncHistory(current PeerView, previous PeerView) PeerView {
	if !current.Reachable {
		if current.NodeID == "" {
			current.NodeID = previous.NodeID
		}
		if current.ValidatorAddress == "" {
			current.ValidatorAddress = previous.ValidatorAddress
		}
		if current.ExpectedValidator == "" {
			current.ExpectedValidator = previous.ExpectedValidator
		}
		if current.Height == 0 {
			current.Height = previous.Height
		}
		if current.LatestBlockHash == "" {
			current.LatestBlockHash = previous.LatestBlockHash
		}
		if current.MempoolSize == 0 {
			current.MempoolSize = previous.MempoolSize
		}
		if !current.IdentityPresent {
			current.IdentityPresent = previous.IdentityPresent
		}
		if !current.IdentityVerified {
			current.IdentityVerified = previous.IdentityVerified
		}
		if current.IdentityError == "" {
			current.IdentityError = previous.IdentityError
		}
		if !current.Admitted {
			current.Admitted = previous.Admitted
		}
		if current.AdmissionError == "" {
			current.AdmissionError = previous.AdmissionError
		}
		if current.LastSeenAt == nil {
			current.LastSeenAt = cloneAPITimePointer(previous.LastSeenAt)
		}
	}
	if current.SyncState == "" {
		current.SyncState = previous.SyncState
	}
	if current.LastSyncAttemptAt == nil {
		current.LastSyncAttemptAt = cloneAPITimePointer(previous.LastSyncAttemptAt)
	}
	if current.LastSyncSuccessAt == nil {
		current.LastSyncSuccessAt = cloneAPITimePointer(previous.LastSyncSuccessAt)
	}
	if current.LastImportErrorCode == "" {
		current.LastImportErrorCode = previous.LastImportErrorCode
	}
	if current.LastImportErrorMessage == "" {
		current.LastImportErrorMessage = previous.LastImportErrorMessage
	}
	if current.LastImportFailureAt == nil {
		current.LastImportFailureAt = cloneAPITimePointer(previous.LastImportFailureAt)
	}
	if current.LastImportFailureHeight == 0 {
		current.LastImportFailureHeight = previous.LastImportFailureHeight
	}
	if current.LastImportFailureBlockHash == "" {
		current.LastImportFailureBlockHash = previous.LastImportFailureBlockHash
	}
	if current.LastSnapshotRestoreAt == nil {
		current.LastSnapshotRestoreAt = cloneAPITimePointer(previous.LastSnapshotRestoreAt)
	}
	if current.LastSnapshotRestoreHeight == 0 {
		current.LastSnapshotRestoreHeight = previous.LastSnapshotRestoreHeight
	}
	if current.LastSnapshotRestoreBlockHash == "" {
		current.LastSnapshotRestoreBlockHash = previous.LastSnapshotRestoreBlockHash
	}
	if current.LastSnapshotRestoreReason == "" {
		current.LastSnapshotRestoreReason = previous.LastSnapshotRestoreReason
	}
	if current.LastReplicationErrorCode == "" {
		current.LastReplicationErrorCode = previous.LastReplicationErrorCode
	}
	if current.LastReplicationErrorMessage == "" {
		current.LastReplicationErrorMessage = previous.LastReplicationErrorMessage
	}
	if current.LastReplicationFailureAt == nil {
		current.LastReplicationFailureAt = cloneAPITimePointer(previous.LastReplicationFailureAt)
	}
	if current.LastReplicationFailureHeight == 0 {
		current.LastReplicationFailureHeight = previous.LastReplicationFailureHeight
	}
	if current.LastReplicationFailureBlockHash == "" {
		current.LastReplicationFailureBlockHash = previous.LastReplicationFailureBlockHash
	}
	if current.LastReplicationFailureReason == "" {
		current.LastReplicationFailureReason = previous.LastReplicationFailureReason
	}
	return current
}

func applyPeerSyncResult(view PeerView, result peerSyncResult) PeerView {
	if result.ImportFailureAt != nil {
		view.LastImportErrorCode = result.ImportErrorCode
		view.LastImportErrorMessage = result.ImportErrorMessage
		view.LastImportFailureAt = cloneAPITimePointer(result.ImportFailureAt)
		view.LastImportFailureHeight = result.ImportFailureHeight
		view.LastImportFailureBlockHash = result.ImportFailureBlockHash
		view.SyncState = "import_blocked"
	}
	if result.SnapshotRestoreAt != nil {
		view.LastSnapshotRestoreAt = cloneAPITimePointer(result.SnapshotRestoreAt)
		view.LastSnapshotRestoreHeight = result.SnapshotRestoreHeight
		view.LastSnapshotRestoreBlockHash = result.SnapshotRestoreBlockHash
		view.LastSnapshotRestoreReason = result.SnapshotRestoreReason
		view.SyncState = "snapshot_restored"
	}
	return view
}

func cloneAPITimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func cloneAPITimeValue(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	cloned := value
	return &cloned
}

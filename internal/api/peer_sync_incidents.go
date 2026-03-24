package api

import (
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

const peerIncidentLimitPerPeer = 5

func (s *Server) enrichPeerView(view PeerView) PeerView {
	peerSummary := s.ledger.PeerSyncPeerSummary(view.URL)
	view.IncidentCount = peerSummary.IncidentCount
	view.IncidentOccurrences = peerSummary.TotalOccurrences
	view.LatestIncidentAt = cloneAPITimePointer(peerSummary.LatestObservedAt)
	view.RecentIncidents = s.ledger.PeerSyncIncidents(view.URL, peerIncidentLimitPerPeer)
	return view
}

func (s *Server) recordPeerIncident(view PeerView) {
	incident, ok := peerSyncIncidentFromView(view, s.ledger.Status())
	if !ok {
		return
	}
	if err := s.ledger.RecordPeerSyncIncident(incident); err != nil {
		recordPeerLog("peer-sync-incident", err)
	}
}

func peerSyncIncidentFromView(view PeerView, localStatus ledger.StatusView) (ledger.PeerSyncIncident, bool) {
	incident := ledger.PeerSyncIncident{
		PeerURL:     view.URL,
		State:       view.SyncState,
		LocalHeight: localStatus.Height,
		PeerHeight:  view.Height,
		HeightDelta: view.HeightDelta,
	}

	switch view.SyncState {
	case "snapshot_restored":
		incident.Reason = strings.TrimSpace(view.LastSnapshotRestoreReason)
		incident.BlockHash = strings.TrimSpace(view.LastSnapshotRestoreBlockHash)
		incident.ErrorCode = strings.TrimSpace(view.LastImportErrorCode)
		incident.ErrorMessage = strings.TrimSpace(view.LastImportErrorMessage)
		if view.LastSnapshotRestoreHeight > 0 {
			incident.PeerHeight = view.LastSnapshotRestoreHeight
		}
		setPeerIncidentObservedAt(&incident, view.LastSnapshotRestoreAt, view.LastImportFailureAt, view.LastSyncSuccessAt)
		return incident, true
	case "import_blocked":
		incident.BlockHash = strings.TrimSpace(view.LastImportFailureBlockHash)
		incident.ErrorCode = strings.TrimSpace(view.LastImportErrorCode)
		incident.ErrorMessage = strings.TrimSpace(view.LastImportErrorMessage)
		if view.LastImportFailureHeight > 0 {
			incident.PeerHeight = view.LastImportFailureHeight
		}
		setPeerIncidentObservedAt(&incident, view.LastImportFailureAt, view.LastSyncAttemptAt)
		return incident, true
	case "unadmitted":
		incident.Reason = firstNonEmpty(strings.TrimSpace(view.AdmissionError), strings.TrimSpace(view.IdentityError))
		setPeerIncidentObservedAt(&incident, view.LastSeenAt, view.LastSyncAttemptAt)
		return incident, true
	case "unreachable", "sync_error":
		incident.BlockHash = strings.TrimSpace(view.LastImportFailureBlockHash)
		incident.ErrorCode = strings.TrimSpace(view.LastImportErrorCode)
		incident.ErrorMessage = strings.TrimSpace(view.Error)
		if incident.ErrorMessage == "" {
			incident.ErrorMessage = strings.TrimSpace(view.LastImportErrorMessage)
		}
		if view.LastImportFailureHeight > 0 {
			incident.PeerHeight = view.LastImportFailureHeight
		}
		setPeerIncidentObservedAt(&incident, view.LastImportFailureAt, view.LastSyncAttemptAt, view.LastSeenAt)
		return incident, true
	default:
		return ledger.PeerSyncIncident{}, false
	}
}

func setPeerIncidentObservedAt(incident *ledger.PeerSyncIncident, values ...*time.Time) {
	for _, value := range values {
		if value == nil || value.IsZero() {
			continue
		}
		observedAt := value.UTC()
		incident.FirstObservedAt = observedAt
		incident.LastObservedAt = observedAt
		return
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

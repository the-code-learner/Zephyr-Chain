package api

import (
	"errors"
	"net"
	"strconv"
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
	backfillPeerViewFromIncidents(&view, view.RecentIncidents)
	return view
}

func backfillPeerViewFromIncidents(view *PeerView, incidents []ledger.PeerSyncIncident) {
	for _, incident := range incidents {
		switch incident.State {
		case "snapshot_restored":
			backfillPeerSnapshotRestore(view, incident)
			if incident.ErrorCode != "" || incident.ErrorMessage != "" {
				backfillPeerImportFailure(view, incident)
			}
		case "import_blocked":
			backfillPeerImportFailure(view, incident)
		case "replication_blocked":
			backfillPeerReplicationFailure(view, incident)
		}
	}
}

func backfillPeerSnapshotRestore(view *PeerView, incident ledger.PeerSyncIncident) {
	if view.LastSnapshotRestoreAt == nil {
		view.LastSnapshotRestoreAt = cloneAPITimeValue(incident.LastObservedAt)
	}
	if view.LastSnapshotRestoreHeight == 0 {
		view.LastSnapshotRestoreHeight = incident.PeerHeight
	}
	if view.LastSnapshotRestoreBlockHash == "" {
		view.LastSnapshotRestoreBlockHash = strings.TrimSpace(incident.BlockHash)
	}
	if view.LastSnapshotRestoreReason == "" {
		view.LastSnapshotRestoreReason = strings.TrimSpace(incident.Reason)
	}
}

func backfillPeerImportFailure(view *PeerView, incident ledger.PeerSyncIncident) {
	if view.LastImportErrorCode == "" {
		view.LastImportErrorCode = strings.TrimSpace(incident.ErrorCode)
	}
	if view.LastImportErrorMessage == "" {
		view.LastImportErrorMessage = strings.TrimSpace(incident.ErrorMessage)
	}
	if view.LastImportFailureAt == nil {
		view.LastImportFailureAt = cloneAPITimeValue(incident.LastObservedAt)
	}
	if view.LastImportFailureHeight == 0 {
		view.LastImportFailureHeight = incident.PeerHeight
	}
	if view.LastImportFailureBlockHash == "" {
		view.LastImportFailureBlockHash = strings.TrimSpace(incident.BlockHash)
	}
}

func backfillPeerReplicationFailure(view *PeerView, incident ledger.PeerSyncIncident) {
	if view.LastReplicationErrorCode == "" {
		view.LastReplicationErrorCode = strings.TrimSpace(incident.ErrorCode)
	}
	if view.LastReplicationErrorMessage == "" {
		view.LastReplicationErrorMessage = strings.TrimSpace(incident.ErrorMessage)
	}
	if view.LastReplicationFailureAt == nil {
		view.LastReplicationFailureAt = cloneAPITimeValue(incident.LastObservedAt)
	}
	if view.LastReplicationFailureHeight == 0 {
		view.LastReplicationFailureHeight = incident.LocalHeight
	}
	if view.LastReplicationFailureBlockHash == "" {
		view.LastReplicationFailureBlockHash = strings.TrimSpace(incident.BlockHash)
	}
	if view.LastReplicationFailureReason == "" {
		view.LastReplicationFailureReason = strings.TrimSpace(incident.Reason)
	}
}

func (s *Server) recordPeerIncident(view PeerView) {
	incident, ok := peerSyncIncidentFromView(view, s.ledger.Status())
	if !ok {
		return
	}
	s.recordExplicitPeerIncident(incident)
}

func (s *Server) recordExplicitPeerIncident(incident ledger.PeerSyncIncident) {
	if err := s.ledger.RecordPeerSyncIncident(incident); err != nil {
		recordPeerLog("peer-sync-incident", err)
		return
	}
	s.eventLogger.logPeerIncident(incident)
}

func (s *Server) recordReplicationIncident(peerURL string, reason string, blockHash string, err error) {
	if err == nil {
		return
	}
	peerURL = strings.TrimSpace(peerURL)
	if peerURL == "" {
		return
	}

	now := time.Now().UTC()
	s.recordExplicitPeerIncident(ledger.PeerSyncIncident{
		PeerURL:         peerURL,
		State:           "replication_blocked",
		Reason:          strings.TrimSpace(reason),
		LocalHeight:     s.ledger.Status().Height,
		BlockHash:       strings.TrimSpace(blockHash),
		ErrorCode:       peerTransportIncidentCode(err),
		ErrorMessage:    strings.TrimSpace(err.Error()),
		FirstObservedAt: now,
		LastObservedAt:  now,
	})
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

func peerTransportIncidentCode(err error) string {
	if err == nil {
		return ""
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout"
	}
	message := strings.TrimSpace(err.Error())
	const statusPrefix = "peer returned status "
	if strings.HasPrefix(message, statusPrefix) {
		statusCode := strings.TrimSpace(strings.TrimPrefix(message, statusPrefix))
		if _, convErr := strconv.Atoi(statusCode); convErr == nil {
			return "http_status_" + statusCode
		}
	}
	if strings.Contains(strings.ToLower(message), "timeout") {
		return "timeout"
	}
	return "transport_error"
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

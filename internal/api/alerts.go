package api

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

const (
	alertSeverityCritical = "critical"
	alertSeverityWarning  = "warning"
)

type Alert struct {
	Code       string     `json:"code"`
	Severity   string     `json:"severity"`
	Component  string     `json:"component"`
	Summary    string     `json:"summary"`
	Detail     string     `json:"detail,omitempty"`
	ObservedAt *time.Time `json:"observedAt,omitempty"`
}

type AlertsResponse struct {
	GeneratedAt                time.Time `json:"generatedAt"`
	NodeID                     string    `json:"nodeId"`
	ValidatorAddress           string    `json:"validatorAddress,omitempty"`
	PeerCount                  int       `json:"peerCount"`
	Ready                      bool      `json:"ready"`
	Status                     string    `json:"status"`
	ConsensusAutomationEnabled bool      `json:"consensusAutomationEnabled"`
	PeerSyncEnabled            bool      `json:"peerSyncEnabled"`
	StructuredLogsEnabled      bool      `json:"structuredLogsEnabled"`
	AlertCount                 int       `json:"alertCount"`
	CriticalCount              int       `json:"criticalCount"`
	WarningCount               int       `json:"warningCount"`
	Alerts                     []Alert   `json:"alerts"`
}

func (s *Server) handleAlerts(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.buildAlertsResponse(time.Now().UTC()))
}

func (s *Server) buildAlertsResponse(now time.Time) AlertsResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	health := s.buildHealthResponse(now)
	recovery := s.ledger.ConsensusRecovery()
	diagnostics := s.ledger.ConsensusDiagnostics()
	peerSummary := s.ledger.PeerSyncSummary()
	peers := s.peerSnapshot()
	roundEvidence := s.buildRoundEvidence(now)
	blockReadiness := s.buildBlockReadiness(now)

	response := AlertsResponse{
		GeneratedAt:                now,
		NodeID:                     s.nodeID,
		ValidatorAddress:           s.config.ValidatorAddress,
		PeerCount:                  len(s.config.PeerURLs),
		Ready:                      health.Ready,
		Status:                     health.Status,
		ConsensusAutomationEnabled: s.config.EnableConsensusAutomation,
		PeerSyncEnabled:            s.config.EnablePeerSync,
		StructuredLogsEnabled:      s.config.EnableStructuredLogs,
		Alerts:                     make([]Alert, 0, len(health.Checks)),
	}

	if check, ok := findHealthCheck(health.Checks, "validator_set"); ok && check.Status == healthCheckWarn {
		appendAlert(&response, Alert{
			Code:      "validator_set_missing",
			Severity:  alertSeverityWarning,
			Component: "consensus",
			Summary:   check.Summary,
			Detail:    firstNonEmpty(check.Detail, "validator-driven mode is enabled but no active validator set has been elected yet"),
		})
	}

	if check, ok := findHealthCheck(health.Checks, "recovery"); ok && check.Status == healthCheckFail {
		appendAlert(&response, Alert{
			Code:       "consensus_recovery_backlog",
			Severity:   alertSeverityCritical,
			Component:  "recovery",
			Summary:    check.Summary,
			Detail:     firstNonEmpty(check.Detail, buildRecoveryAlertDetail(recovery)),
			ObservedAt: latestPendingActionObservedAt(recovery),
		})
	}

	if check, ok := findHealthCheck(health.Checks, "consensus"); ok && check.Status == healthCheckWarn {
		appendAlert(&response, Alert{
			Code:       "consensus_state_warning",
			Severity:   alertSeverityWarning,
			Component:  "consensus",
			Summary:    check.Summary,
			Detail:     firstNonEmpty(check.Detail, buildConsensusAlertDetail(roundEvidence, blockReadiness)),
			ObservedAt: consensusAlertObservedAt(roundEvidence, blockReadiness),
		})
	}

	if check, ok := findHealthCheck(health.Checks, "peer_sync"); ok && (check.Status == healthCheckWarn || check.Status == healthCheckFail) {
		code := "peer_sync_degraded"
		severity := alertSeverityWarning
		switch {
		case check.Status == healthCheckFail:
			code = "peer_sync_unavailable"
			severity = alertSeverityCritical
		case strings.Contains(check.Summary, "not observed yet"):
			code = "peer_sync_unobserved"
		}
		appendAlert(&response, Alert{
			Code:       code,
			Severity:   severity,
			Component:  "peer_sync",
			Summary:    check.Summary,
			Detail:     check.Detail,
			ObservedAt: latestPeerObservedAt(peers, peerSummary),
		})
	}
	if state, ok := peerSyncStateSummary(peerSummary, "import_blocked"); ok {
		appendAlert(&response, Alert{
			Code:       "peer_import_blocked",
			Severity:   alertSeverityWarning,
			Component:  "peer_sync",
			Summary:    "peer import is blocked on one or more peers",
			Detail:     buildPeerStateAlertDetail(state, "errorCode", representativePeerSyncErrorCode(peerSummary, "import_blocked")),
			ObservedAt: cloneAlertTime(state.LatestObservedAt),
		})
	}
	if state, ok := peerSyncStateSummary(peerSummary, "unadmitted"); ok {
		appendAlert(&response, Alert{
			Code:       "peer_admission_blocked",
			Severity:   alertSeverityWarning,
			Component:  "peer_sync",
			Summary:    "peer admission is rejecting one or more configured peers",
			Detail:     buildPeerStateAlertDetail(state, "reason", representativePeerSyncReason(peerSummary, "unadmitted")),
			ObservedAt: cloneAlertTime(state.LatestObservedAt),
		})
	}
	if state, ok := peerSyncStateSummary(peerSummary, "replication_blocked"); ok {
		detail := buildPeerStateAlertDetail(state, "reason", representativePeerSyncReason(peerSummary, "replication_blocked"))
		detail = appendPeerStateAlertLabel(detail, "errorCode", representativePeerSyncErrorCode(peerSummary, "replication_blocked"))
		appendAlert(&response, Alert{
			Code:       "peer_replication_blocked",
			Severity:   alertSeverityWarning,
			Component:  "peer_sync",
			Summary:    "consensus replication is failing on one or more peers",
			Detail:     detail,
			ObservedAt: cloneAlertTime(state.LatestObservedAt),
		})
	}

	if check, ok := findHealthCheck(health.Checks, "diagnostics"); ok && check.Status == healthCheckWarn {
		appendAlert(&response, Alert{
			Code:       "recent_consensus_diagnostics",
			Severity:   alertSeverityWarning,
			Component:  "consensus",
			Summary:    check.Summary,
			Detail:     firstNonEmpty(check.Detail, latestDiagnosticDetail(diagnostics)),
			ObservedAt: latestDiagnosticObservedAt(diagnostics),
		})
	}

	sortAlerts(response.Alerts)
	return response
}

func appendAlert(response *AlertsResponse, alert Alert) {
	response.Alerts = append(response.Alerts, alert)
	response.AlertCount++
	switch alert.Severity {
	case alertSeverityCritical:
		response.CriticalCount++
	case alertSeverityWarning:
		response.WarningCount++
	}
}

func findHealthCheck(checks []HealthCheck, name string) (HealthCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return HealthCheck{}, false
}

func buildRecoveryAlertDetail(recovery ledger.ConsensusRecoveryView) string {
	parts := make([]string, 0, 3)
	if recovery.PendingReplayCount > 0 {
		parts = append(parts, "pendingReplay="+strconv.Itoa(recovery.PendingReplayCount))
	}
	if recovery.PendingImportCount > 0 {
		parts = append(parts, "pendingImport="+strconv.Itoa(recovery.PendingImportCount))
	}
	if len(recovery.PendingImportHeights) > 0 {
		parts = append(parts, "importHeights="+joinUint64s(recovery.PendingImportHeights))
	}
	return strings.Join(parts, ", ")
}

func buildConsensusAlertDetail(roundEvidence RoundEvidence, blockReadiness BlockReadiness) string {
	parts := make([]string, 0, len(roundEvidence.Warnings)+len(blockReadiness.Warnings))
	for _, warning := range roundEvidence.Warnings {
		parts = append(parts, "round:"+warning)
	}
	for _, warning := range blockReadiness.Warnings {
		parts = append(parts, "block:"+warning)
	}
	return strings.Join(parts, ", ")
}

func peerSyncStateSummary(summary ledger.PeerSyncSummaryView, state string) (ledger.PeerSyncStateSummary, bool) {
	state = strings.TrimSpace(state)
	for _, bucket := range summary.States {
		if bucket.State == state {
			return bucket, true
		}
	}
	return ledger.PeerSyncStateSummary{}, false
}

func buildPeerStateAlertDetail(state ledger.PeerSyncStateSummary, labelName string, labelValue string) string {
	parts := []string{
		"incidents=" + strconv.Itoa(state.IncidentCount),
		"affectedPeers=" + strconv.Itoa(state.AffectedPeerCount),
		"occurrences=" + strconv.Itoa(state.TotalOccurrences),
	}
	labelName = strings.TrimSpace(labelName)
	labelValue = strings.TrimSpace(labelValue)
	if labelName != "" && labelValue != "" {
		parts = append(parts, labelName+"="+labelValue)
	}
	return strings.Join(parts, ", ")
}

func appendPeerStateAlertLabel(detail string, labelName string, labelValue string) string {
	labelName = strings.TrimSpace(labelName)
	labelValue = strings.TrimSpace(labelValue)
	if detail == "" || labelName == "" || labelValue == "" {
		return detail
	}
	return detail + ", " + labelName + "=" + labelValue
}

func representativePeerSyncErrorCode(summary ledger.PeerSyncSummaryView, state string) string {
	if peer, ok := latestPeerSummaryForState(summary, state); ok {
		value := strings.TrimSpace(peer.LatestErrorCode)
		if value != "" && value != "unknown" {
			return value
		}
	}
	for _, bucket := range summary.ErrorCodes {
		value := strings.TrimSpace(bucket.ErrorCode)
		if value != "" && value != "unknown" {
			return value
		}
	}
	return ""
}

func representativePeerSyncReason(summary ledger.PeerSyncSummaryView, state string) string {
	if peer, ok := latestPeerSummaryForState(summary, state); ok {
		value := strings.TrimSpace(peer.LatestReason)
		if value != "" && value != "unknown" {
			return value
		}
	}
	for _, bucket := range summary.Reasons {
		value := strings.TrimSpace(bucket.Reason)
		if value != "" && value != "unknown" {
			return value
		}
	}
	return ""
}

func latestPeerSummaryForState(summary ledger.PeerSyncSummaryView, state string) (ledger.PeerSyncPeerSummary, bool) {
	state = strings.TrimSpace(state)
	for _, peer := range summary.Peers {
		if strings.TrimSpace(peer.LatestState) == state {
			return peer, true
		}
	}
	return ledger.PeerSyncPeerSummary{}, false
}

func latestPendingActionObservedAt(recovery ledger.ConsensusRecoveryView) *time.Time {
	var latest *time.Time
	for _, action := range recovery.PendingActions {
		updateLatestAlertTime(&latest, action.RecordedAt)
		if action.LastReplayAt != nil {
			updateLatestAlertTime(&latest, *action.LastReplayAt)
		}
	}
	return latest
}

func latestDiagnosticObservedAt(diagnostics ledger.ConsensusDiagnosticsView) *time.Time {
	if len(diagnostics.Recent) == 0 {
		return nil
	}
	observedAt := diagnostics.Recent[0].ObservedAt.UTC()
	return &observedAt
}

func latestDiagnosticDetail(diagnostics ledger.ConsensusDiagnosticsView) string {
	if len(diagnostics.Recent) == 0 {
		return ""
	}
	latest := diagnostics.Recent[0]
	parts := make([]string, 0, 3)
	if latest.Code != "" {
		parts = append(parts, latest.Code)
	}
	if latest.Source != "" {
		parts = append(parts, "source="+latest.Source)
	}
	if latest.Message != "" {
		parts = append(parts, latest.Message)
	}
	return strings.Join(parts, ": ")
}

func latestPeerObservedAt(peers []PeerView, summary ledger.PeerSyncSummaryView) *time.Time {
	latest := cloneAlertTime(summary.LatestObservedAt)
	for _, peer := range peers {
		if peer.LastSeenAt != nil {
			updateLatestAlertTime(&latest, *peer.LastSeenAt)
		}
		if peer.LastSyncAttemptAt != nil {
			updateLatestAlertTime(&latest, *peer.LastSyncAttemptAt)
		}
		if peer.LastSyncSuccessAt != nil {
			updateLatestAlertTime(&latest, *peer.LastSyncSuccessAt)
		}
		if peer.LastImportFailureAt != nil {
			updateLatestAlertTime(&latest, *peer.LastImportFailureAt)
		}
		if peer.LastSnapshotRestoreAt != nil {
			updateLatestAlertTime(&latest, *peer.LastSnapshotRestoreAt)
		}
		if peer.LatestIncidentAt != nil {
			updateLatestAlertTime(&latest, *peer.LatestIncidentAt)
		}
	}
	return latest
}

func consensusAlertObservedAt(roundEvidence RoundEvidence, blockReadiness BlockReadiness) *time.Time {
	latest := cloneAlertTime(roundEvidence.DeadlineAt)
	if latest == nil {
		latest = cloneAlertTime(roundEvidence.StartedAt)
	}
	if blockReadiness.LocalTemplateProducedAt != nil {
		updateLatestAlertTime(&latest, *blockReadiness.LocalTemplateProducedAt)
	}
	if blockReadiness.LatestCertifiedProducedAt != nil {
		updateLatestAlertTime(&latest, *blockReadiness.LatestCertifiedProducedAt)
	}
	return latest
}

func sortAlerts(alerts []Alert) {
	sort.Slice(alerts, func(i, j int) bool {
		leftRank := alertSeverityRank(alerts[i].Severity)
		rightRank := alertSeverityRank(alerts[j].Severity)
		if leftRank != rightRank {
			return leftRank < rightRank
		}
		leftObserved := alerts[i].ObservedAt
		rightObserved := alerts[j].ObservedAt
		switch {
		case leftObserved == nil && rightObserved == nil:
		case leftObserved == nil:
			return false
		case rightObserved == nil:
			return true
		case !leftObserved.Equal(*rightObserved):
			return leftObserved.After(*rightObserved)
		}
		if alerts[i].Component != alerts[j].Component {
			return alerts[i].Component < alerts[j].Component
		}
		return alerts[i].Code < alerts[j].Code
	})
}

func alertSeverityRank(severity string) int {
	switch severity {
	case alertSeverityCritical:
		return 0
	case alertSeverityWarning:
		return 1
	default:
		return 2
	}
}

func cloneAlertTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := value.UTC()
	return &cloned
}

func updateLatestAlertTime(target **time.Time, candidate time.Time) {
	if candidate.IsZero() {
		return
	}
	candidate = candidate.UTC()
	if *target == nil || candidate.After(**target) {
		*target = &candidate
	}
}

func joinUint64s(values []uint64) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatUint(value, 10))
	}
	return strings.Join(parts, "|")
}

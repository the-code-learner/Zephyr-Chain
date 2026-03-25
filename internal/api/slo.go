package api

import (
	"net/http"
	"strings"
	"time"
)

const (
	sloStatusMeeting       = "meeting"
	sloStatusAtRisk        = "at_risk"
	sloStatusBreached      = "breached"
	sloStatusNotApplicable = "not_applicable"
)

type SLOObjective struct {
	Name          string   `json:"name"`
	Target        string   `json:"target"`
	Status        string   `json:"status"`
	Summary       string   `json:"summary"`
	Detail        string   `json:"detail,omitempty"`
	RelatedAlerts []string `json:"relatedAlerts,omitempty"`
}

type SLOSummaryResponse struct {
	GeneratedAt        time.Time      `json:"generatedAt"`
	NodeID             string         `json:"nodeId"`
	ValidatorAddress   string         `json:"validatorAddress,omitempty"`
	PeerCount          int            `json:"peerCount"`
	Ready              bool           `json:"ready"`
	HealthStatus       string         `json:"healthStatus"`
	AlertCount         int            `json:"alertCount"`
	CriticalCount      int            `json:"criticalCount"`
	WarningCount       int            `json:"warningCount"`
	ObjectiveCount     int            `json:"objectiveCount"`
	MeetingCount       int            `json:"meetingCount"`
	AtRiskCount        int            `json:"atRiskCount"`
	BreachedCount      int            `json:"breachedCount"`
	NotApplicableCount int            `json:"notApplicableCount"`
	Objectives         []SLOObjective `json:"objectives"`
}

func (s *Server) handleSLO(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.buildSLOSummary(time.Now().UTC()))
}

func (s *Server) buildSLOSummary(now time.Time) SLOSummaryResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	health := s.buildHealthResponse(now)
	alerts := s.buildAlertsResponse(now)
	objectives := []SLOObjective{
		buildReadinessObjective(health, alerts),
		s.buildConsensusContinuityObjective(alerts),
		s.buildPeerSyncContinuityObjective(alerts),
		s.buildSettlementThroughputObjective(now, alerts),
	}

	response := SLOSummaryResponse{
		GeneratedAt:      now,
		NodeID:           s.nodeID,
		ValidatorAddress: s.config.ValidatorAddress,
		PeerCount:        len(s.config.PeerURLs),
		Ready:            health.Ready,
		HealthStatus:     health.Status,
		AlertCount:       alerts.AlertCount,
		CriticalCount:    alerts.CriticalCount,
		WarningCount:     alerts.WarningCount,
		ObjectiveCount:   len(objectives),
		Objectives:       objectives,
	}

	for _, objective := range objectives {
		switch objective.Status {
		case sloStatusMeeting:
			response.MeetingCount++
		case sloStatusAtRisk:
			response.AtRiskCount++
		case sloStatusBreached:
			response.BreachedCount++
		case sloStatusNotApplicable:
			response.NotApplicableCount++
		}
	}

	return response
}

func buildReadinessObjective(health HealthResponse, alerts AlertsResponse) SLOObjective {
	objective := SLOObjective{
		Name:          "node_readiness",
		Target:        "Keep the node ready for operator and automation traffic.",
		RelatedAlerts: collectAlertCodes(alerts.Alerts),
	}

	switch {
	case !health.Ready:
		objective.Status = sloStatusBreached
		objective.Summary = "node readiness objective is currently breached"
		objective.Detail = firstNonEmpty(strings.Join(health.Warnings, "; "), summarizeAlertSummaries(alerts.Alerts))
	case health.Status == healthStatusWarn || alerts.WarningCount > 0:
		objective.Status = sloStatusAtRisk
		objective.Summary = "node is serving but readiness margin is reduced by active warnings"
		objective.Detail = firstNonEmpty(strings.Join(health.Warnings, "; "), summarizeAlertSummaries(alerts.Alerts))
	default:
		objective.Status = sloStatusMeeting
		objective.Summary = "node currently meets the readiness objective"
	}
	return objective
}

func (s *Server) buildConsensusContinuityObjective(alerts AlertsResponse) SLOObjective {
	relatedCodes := []string{"validator_set_missing", "consensus_recovery_backlog", "consensus_state_warning", "recent_consensus_diagnostics"}
	relatedAlerts := filterAlertsByCode(alerts.Alerts, relatedCodes...)
	objective := SLOObjective{
		Name:          "consensus_continuity",
		Target:        "Keep the next-height consensus pipeline clear enough to progress without manual recovery.",
		RelatedAlerts: collectAlertCodes(relatedAlerts),
	}

	switch {
	case hasAlertCode(alerts.Alerts, "consensus_recovery_backlog"):
		objective.Status = sloStatusBreached
		objective.Summary = "consensus continuity is blocked by active recovery backlog"
		objective.Detail = summarizeAlertDetails(relatedAlerts)
	case len(relatedAlerts) > 0:
		objective.Status = sloStatusAtRisk
		objective.Summary = "consensus continuity is degraded but not fully blocked"
		objective.Detail = summarizeAlertDetails(relatedAlerts)
	default:
		objective.Status = sloStatusMeeting
		objective.Summary = "consensus continuity objective is currently met"
	}

	return objective
}

func (s *Server) buildPeerSyncContinuityObjective(alerts AlertsResponse) SLOObjective {
	objective := SLOObjective{
		Name:   "peer_sync_continuity",
		Target: "Maintain at least one admitted and reachable peer when peer sync is enabled.",
	}

	if !s.config.EnablePeerSync || len(s.config.PeerURLs) == 0 {
		objective.Status = sloStatusNotApplicable
		objective.Summary = "peer sync continuity objective is not applicable in the current configuration"
		if !s.config.EnablePeerSync {
			objective.Detail = "peer sync is disabled by configuration"
		} else {
			objective.Detail = "no peers are configured for the current node"
		}
		return objective
	}

	relatedAlerts := filterAlertsByCode(alerts.Alerts, "peer_sync_unavailable", "peer_sync_degraded", "peer_sync_unobserved")
	objective.RelatedAlerts = collectAlertCodes(relatedAlerts)
	switch {
	case hasAlertCode(alerts.Alerts, "peer_sync_unavailable"):
		objective.Status = sloStatusBreached
		objective.Summary = "peer sync continuity is currently breached"
		objective.Detail = summarizeAlertDetails(relatedAlerts)
	case len(relatedAlerts) > 0:
		objective.Status = sloStatusAtRisk
		objective.Summary = "peer sync continuity is degraded or still warming up"
		objective.Detail = summarizeAlertDetails(relatedAlerts)
	default:
		objective.Status = sloStatusMeeting
		objective.Summary = "peer sync continuity objective is currently met"
	}
	return objective
}

func (s *Server) buildSettlementThroughputObjective(now time.Time, alerts AlertsResponse) SLOObjective {
	objective := SLOObjective{
		Name:   "settlement_throughput",
		Target: "Keep queued transactions clearing within the expected automatic block-production window.",
	}
	assessment := s.assessSettlementThroughput(now)
	if !assessment.Applicable {
		objective.Status = sloStatusNotApplicable
		objective.Summary = "settlement throughput objective is not applicable in the current configuration"
		objective.Detail = assessment.Detail
		return objective
	}

	relatedAlerts := filterAlertsByCode(alerts.Alerts, settlementThroughputAlertStalled, settlementThroughputAlertReduced)
	objective.RelatedAlerts = collectAlertCodes(relatedAlerts)
	switch {
	case hasAlertCode(alerts.Alerts, settlementThroughputAlertStalled):
		objective.Status = sloStatusBreached
		objective.Summary = "settlement throughput objective is currently breached"
		objective.Detail = firstNonEmpty(summarizeAlertDetails(relatedAlerts), assessment.Detail)
	case len(relatedAlerts) > 0:
		objective.Status = sloStatusAtRisk
		objective.Summary = "settlement throughput objective is at risk"
		objective.Detail = firstNonEmpty(summarizeAlertDetails(relatedAlerts), assessment.Detail)
	default:
		objective.Status = sloStatusMeeting
		objective.Summary = "settlement throughput objective is currently met"
	}
	return objective
}

func filterAlertsByCode(alerts []Alert, codes ...string) []Alert {
	if len(alerts) == 0 || len(codes) == 0 {
		return nil
	}
	matches := make([]Alert, 0, len(codes))
	for _, code := range codes {
		if alert, ok := findAlertByCode(alerts, code); ok {
			matches = append(matches, alert)
		}
	}
	return matches
}

func collectAlertCodes(alerts []Alert) []string {
	if len(alerts) == 0 {
		return nil
	}
	codes := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		codes = append(codes, alert.Code)
	}
	return codes
}

func findAlertByCode(alerts []Alert, code string) (Alert, bool) {
	for _, alert := range alerts {
		if alert.Code == code {
			return alert, true
		}
	}
	return Alert{}, false
}

func hasAlertCode(alerts []Alert, code string) bool {
	_, ok := findAlertByCode(alerts, code)
	return ok
}

func summarizeAlertSummaries(alerts []Alert) string {
	if len(alerts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		parts = append(parts, alert.Summary)
	}
	return strings.Join(parts, "; ")
}

func summarizeAlertDetails(alerts []Alert) string {
	if len(alerts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(alerts))
	for _, alert := range alerts {
		parts = append(parts, firstNonEmpty(alert.Detail, alert.Summary))
	}
	return strings.Join(parts, "; ")
}

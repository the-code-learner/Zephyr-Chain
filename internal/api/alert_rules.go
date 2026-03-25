package api

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const prometheusAlertRuleContentType = "application/yaml; charset=utf-8"

type AlertRule struct {
	Name              string   `json:"name"`
	Severity          string   `json:"severity"`
	Component         string   `json:"component"`
	Summary           string   `json:"summary"`
	Description       string   `json:"description"`
	Expression        string   `json:"expression"`
	For               string   `json:"for,omitempty"`
	Enabled           bool     `json:"enabled"`
	DisabledReason    string   `json:"disabledReason,omitempty"`
	SourceMetrics     []string `json:"sourceMetrics,omitempty"`
	RelatedAlertCodes []string `json:"relatedAlertCodes,omitempty"`
	RelatedObjectives []string `json:"relatedObjectives,omitempty"`
}

type AlertRuleGroup struct {
	Name  string      `json:"name"`
	Title string      `json:"title"`
	Rules []AlertRule `json:"rules"`
}

type AlertRuleBundleResponse struct {
	GeneratedAt           time.Time        `json:"generatedAt"`
	NodeID                string           `json:"nodeId"`
	ValidatorAddress      string           `json:"validatorAddress,omitempty"`
	PeerCount             int              `json:"peerCount"`
	PeerSyncEnabled       bool             `json:"peerSyncEnabled"`
	HealthStatus          string           `json:"healthStatus"`
	CurrentAlertCount     int              `json:"currentAlertCount"`
	CurrentObjectiveCount int              `json:"currentObjectiveCount"`
	RuleCount             int              `json:"ruleCount"`
	EnabledRuleCount      int              `json:"enabledRuleCount"`
	DisabledRuleCount     int              `json:"disabledRuleCount"`
	Groups                []AlertRuleGroup `json:"groups"`
}

func (s *Server) handleAlertRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.buildAlertRuleBundle(time.Now().UTC()))
}

func (s *Server) handlePrometheusAlertRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", prometheusAlertRuleContentType)
	_, _ = w.Write([]byte(s.buildPrometheusAlertRules(time.Now().UTC())))
}

func (s *Server) buildAlertRuleBundle(now time.Time) AlertRuleBundleResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	alerts := s.buildAlertsResponse(now)
	slo := s.buildSLOSummary(now)
	groups := s.buildAlertRuleGroups()

	response := AlertRuleBundleResponse{
		GeneratedAt:           now,
		NodeID:                s.nodeID,
		ValidatorAddress:      s.config.ValidatorAddress,
		PeerCount:             len(s.config.PeerURLs),
		PeerSyncEnabled:       s.config.EnablePeerSync,
		HealthStatus:          slo.HealthStatus,
		CurrentAlertCount:     alerts.AlertCount,
		CurrentObjectiveCount: slo.ObjectiveCount,
		Groups:                groups,
	}

	for _, group := range groups {
		response.RuleCount += len(group.Rules)
		for _, rule := range group.Rules {
			if rule.Enabled {
				response.EnabledRuleCount++
			} else {
				response.DisabledRuleCount++
			}
		}
	}

	return response
}

func (s *Server) buildPrometheusAlertRules(now time.Time) string {
	bundle := s.buildAlertRuleBundle(now)

	var builder strings.Builder
	builder.WriteString("# Zephyr recommended Prometheus alert rules\n")
	builder.WriteString("# generatedAt: ")
	builder.WriteString(bundle.GeneratedAt.Format(time.RFC3339Nano))
	builder.WriteString("\n# nodeId: ")
	builder.WriteString(bundle.NodeID)
	builder.WriteString("\ngroups:\n")

	for _, group := range bundle.Groups {
		enabledRules := make([]AlertRule, 0, len(group.Rules))
		for _, rule := range group.Rules {
			if rule.Enabled {
				enabledRules = append(enabledRules, rule)
			}
		}
		if len(enabledRules) == 0 {
			continue
		}

		builder.WriteString("  - name: ")
		builder.WriteString(group.Name)
		builder.WriteString("\n    rules:\n")
		for _, rule := range enabledRules {
			builder.WriteString("      - alert: ")
			builder.WriteString(rule.Name)
			builder.WriteString("\n        expr: ")
			builder.WriteString(yamlSingleQuoted(rule.Expression))
			builder.WriteByte('\n')
			if rule.For != "" {
				builder.WriteString("        for: ")
				builder.WriteString(rule.For)
				builder.WriteByte('\n')
			}
			builder.WriteString("        labels:\n")
			builder.WriteString("          severity: ")
			builder.WriteString(rule.Severity)
			builder.WriteString("\n          component: ")
			builder.WriteString(rule.Component)
			builder.WriteString("\n          group: ")
			builder.WriteString(group.Name)
			builder.WriteByte('\n')
			if len(rule.RelatedObjectives) == 1 {
				builder.WriteString("          objective: ")
				builder.WriteString(rule.RelatedObjectives[0])
				builder.WriteByte('\n')
			}
			if len(rule.RelatedAlertCodes) == 1 {
				builder.WriteString("          alert_code: ")
				builder.WriteString(rule.RelatedAlertCodes[0])
				builder.WriteByte('\n')
			}
			builder.WriteString("        annotations:\n")
			builder.WriteString("          summary: ")
			builder.WriteString(yamlSingleQuoted(rule.Summary))
			builder.WriteByte('\n')
			builder.WriteString("          description: ")
			builder.WriteString(yamlSingleQuoted(rule.Description))
			builder.WriteByte('\n')
			if len(rule.SourceMetrics) > 0 {
				builder.WriteString("          source_metrics: ")
				builder.WriteString(yamlSingleQuoted(strings.Join(rule.SourceMetrics, ", ")))
				builder.WriteByte('\n')
			}
		}
	}

	return builder.String()
}

func (s *Server) buildAlertRuleGroups() []AlertRuleGroup {
	peerSyncDisabledReason := ""
	switch {
	case !s.config.EnablePeerSync:
		peerSyncDisabledReason = "peer sync is disabled by configuration"
	case len(s.config.PeerURLs) == 0:
		peerSyncDisabledReason = "no peers are configured for peer sync"
	}
	throughputDisabledReason := s.settlementThroughputDisabledReason()

	groups := []AlertRuleGroup{
		{
			Name:  "zephyr.readiness",
			Title: "Node readiness and operator availability",
			Rules: []AlertRule{
				newAlertRule(
					"ZephyrNodeNotReady",
					alertSeverityCritical,
					"readiness",
					"Zephyr node is not ready",
					"The node is failing its derived readiness gate. Inspect /v1/health, /v1/alerts, and /v1/slo for the current incident context.",
					"zephyr_node_ready == 0",
					"2m",
					[]string{"zephyr_node_ready", "zephyr_health_check_status"},
					nil,
					[]string{"node_readiness"},
				),
				newAlertRule(
					"ZephyrNodeReadinessAtRisk",
					alertSeverityWarning,
					"readiness",
					"Zephyr node readiness objective is at risk",
					"The node is still serving traffic, but warnings are eroding readiness margin. Inspect /v1/slo and /v1/alerts before this escalates into a hard outage.",
					"zephyr_slo_objective_status{objective=\"node_readiness\",status=\"at_risk\"} == 1",
					"10m",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"node_readiness"},
				),
			},
		},
		{
			Name:  "zephyr.consensus",
			Title: "Consensus continuity and recovery",
			Rules: []AlertRule{
				newAlertRule(
					"ZephyrConsensusRecoveryBacklog",
					alertSeverityCritical,
					"consensus",
					"Consensus recovery backlog is blocking progress",
					"The node has pending replay or import recovery work that is already reflected as a critical derived alert. Inspect /v1/alerts and /v1/status recovery details.",
					"zephyr_alert_active{code=\"consensus_recovery_backlog\",severity=\"critical\"} == 1",
					"1m",
					[]string{"zephyr_alert_active", "zephyr_recovery_pending_action_count", "zephyr_recovery_pending_import_count"},
					[]string{"consensus_recovery_backlog"},
					[]string{"consensus_continuity"},
				),
				newAlertRule(
					"ZephyrConsensusContinuityBreached",
					alertSeverityCritical,
					"consensus",
					"Consensus continuity objective is breached",
					"The next-height agreement pipeline is blocked. Inspect /v1/slo, /v1/alerts, roundEvidence, and recovery state to distinguish quorum, proposal, template, and transport failures.",
					"zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"breached\"} == 1",
					"2m",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"consensus_continuity"},
				),
				newAlertRule(
					"ZephyrConsensusContinuityAtRisk",
					alertSeverityWarning,
					"consensus",
					"Consensus continuity objective is at risk",
					"Consensus is still progressing or recoverable, but recent evidence suggests growing instability. Inspect /v1/slo, /v1/alerts, and /v1/status diagnostics before the round stalls.",
					"zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"at_risk\"} == 1",
					"10m",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"consensus_continuity"},
				),
			},
		},
		{
			Name:  "zephyr.throughput",
			Title: "Settlement throughput and queue drain",
			Rules: []AlertRule{
				disableAlertRule(
					newAlertRule(
						"ZephyrSettlementThroughputStalled",
						alertSeverityCritical,
						"throughput",
						"Queued transactions are not settling within the expected block window",
						"Automatic block production has fallen behind a live mempool backlog. Inspect /v1/health, /v1/alerts, /v1/slo, /v1/metrics, and the throughput dashboard panel to confirm whether queue drain has stalled.",
						"zephyr_slo_objective_status{objective=\"settlement_throughput\",status=\"breached\"} == 1",
						"2m",
						[]string{"zephyr_slo_objective_status", "zephyr_chain_mempool_transaction_count", "zephyr_chain_latest_block_timestamp_seconds"},
						[]string{settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrSettlementThroughputAtRisk",
						alertSeverityWarning,
						"throughput",
						"Queued transactions are settling slower than expected",
						"The node is still committing blocks, but queued transactions are draining slower than the configured block cadence should allow. Inspect /v1/slo, /v1/alerts, /v1/metrics, and the throughput dashboard panel before this escalates into a stall.",
						"zephyr_slo_objective_status{objective=\"settlement_throughput\",status=\"at_risk\"} == 1",
						"5m",
						[]string{"zephyr_slo_objective_status", "zephyr_chain_mempool_transaction_count", "zephyr_chain_latest_block_timestamp_seconds"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
			},
		},
		{
			Name:  "zephyr.peer_sync",
			Title: "Peer sync continuity and repair",
			Rules: []AlertRule{
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerSyncUnavailable",
						alertSeverityCritical,
						"peer_sync",
						"Peer sync is unavailable",
						"No admitted and reachable peer path is currently available. Inspect /v1/alerts, /v1/slo, and /v1/peers to separate admission failures, unreachable peers, and repair incidents.",
						"zephyr_alert_active{code=\"peer_sync_unavailable\",severity=\"critical\"} == 1",
						"2m",
						[]string{"zephyr_alert_active", "zephyr_peer_runtime_by_sync_state_count"},
						[]string{"peer_sync_unavailable"},
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerSyncContinuityBreached",
						alertSeverityCritical,
						"peer_sync",
						"Peer sync continuity objective is breached",
						"The peer-sync continuity objective has moved into a breached state. Inspect /v1/slo, /v1/alerts, and /v1/peers for the latest peer admission and repair evidence.",
						"zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"breached\"} == 1",
						"3m",
						[]string{"zephyr_slo_objective_status"},
						nil,
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerSyncContinuityAtRisk",
						alertSeverityWarning,
						"peer_sync",
						"Peer sync continuity objective is at risk",
						"Peer sync is degraded or still warming up. Inspect /v1/slo, /v1/alerts, and /v1/peers to confirm whether the issue is expected startup behavior or a developing incident.",
						"zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"at_risk\"} == 1",
						"10m",
						[]string{"zephyr_slo_objective_status"},
						nil,
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerImportBlocked",
						alertSeverityWarning,
						"peer_sync",
						"Peer import is blocked",
						"One or more peers are retaining import-blocked incidents. Inspect /v1/alerts, /v1/status peerSyncSummary, /v1/metrics, and /v1/peers to separate consensus import errors from transport churn.",
						"zephyr_alert_active{code=\"peer_import_blocked\",severity=\"warning\",component=\"peer_sync\"} == 1",
						"5m",
						[]string{"zephyr_alert_active", "zephyr_peer_sync_state_occurrence_count", "zephyr_peer_sync_error_code_occurrence_count"},
						[]string{"peer_import_blocked"},
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerAdmissionBlocked",
						alertSeverityWarning,
						"peer_sync",
						"Peer admission is blocking configured peers",
						"One or more peers are being retained in an unadmitted state. Inspect /v1/alerts, /v1/status peerSyncSummary, /v1/metrics, and /v1/peers to confirm whether validator binding or identity policy is rejecting traffic.",
						"zephyr_alert_active{code=\"peer_admission_blocked\",severity=\"warning\",component=\"peer_sync\"} == 1",
						"5m",
						[]string{"zephyr_alert_active", "zephyr_peer_sync_state_occurrence_count", "zephyr_peer_sync_reason_occurrence_count"},
						[]string{"peer_admission_blocked"},
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableAlertRule(
					newAlertRule(
						"ZephyrPeerReplicationBlocked",
						alertSeverityWarning,
						"peer_sync",
						"Consensus replication is failing on one or more peers",
						"One or more peers are retaining replication-blocked incidents. Inspect /v1/alerts, /v1/status peerSyncSummary, /v1/metrics, and /v1/peers to confirm whether proposal, vote, or block dissemination is hitting transport errors.",
						"zephyr_alert_active{code=\"peer_replication_blocked\",severity=\"warning\",component=\"peer_sync\"} == 1",
						"5m",
						[]string{"zephyr_alert_active", "zephyr_peer_sync_state_occurrence_count", "zephyr_peer_sync_reason_occurrence_count", "zephyr_peer_sync_error_code_occurrence_count"},
						[]string{"peer_replication_blocked"},
						[]string{"consensus_continuity", "peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
			},
		},
	}

	for idx := range groups {
		sort.Slice(groups[idx].Rules, func(i, j int) bool {
			return groups[idx].Rules[i].Name < groups[idx].Rules[j].Name
		})
	}
	return groups
}

func newAlertRule(name string, severity string, component string, summary string, description string, expression string, duration string, sourceMetrics []string, relatedAlerts []string, relatedObjectives []string) AlertRule {
	return AlertRule{
		Name:              name,
		Severity:          severity,
		Component:         component,
		Summary:           summary,
		Description:       description,
		Expression:        expression,
		For:               duration,
		Enabled:           true,
		SourceMetrics:     cloneStrings(sourceMetrics),
		RelatedAlertCodes: cloneStrings(relatedAlerts),
		RelatedObjectives: cloneStrings(relatedObjectives),
	}
}

func disableAlertRule(rule AlertRule, reason string) AlertRule {
	if strings.TrimSpace(reason) == "" {
		return rule
	}
	rule.Enabled = false
	rule.DisabledReason = reason
	return rule
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	cloned := make([]string, len(values))
	copy(cloned, values)
	return cloned
}

func yamlSingleQuoted(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

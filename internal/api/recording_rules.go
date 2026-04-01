package api

import (
	"net/http"
	"sort"
	"strings"
	"time"
)

const prometheusRecordingRuleContentType = "application/yaml; charset=utf-8"

type RecordingRule struct {
	Name              string   `json:"name"`
	Record            string   `json:"record"`
	Component         string   `json:"component"`
	Summary           string   `json:"summary"`
	Description       string   `json:"description"`
	Expression        string   `json:"expression"`
	Enabled           bool     `json:"enabled"`
	DisabledReason    string   `json:"disabledReason,omitempty"`
	SourceMetrics     []string `json:"sourceMetrics,omitempty"`
	RelatedAlertCodes []string `json:"relatedAlertCodes,omitempty"`
	RelatedObjectives []string `json:"relatedObjectives,omitempty"`
}

type RecordingRuleGroup struct {
	Name  string          `json:"name"`
	Title string          `json:"title"`
	Rules []RecordingRule `json:"rules"`
}

type RecordingRuleBundleResponse struct {
	GeneratedAt           time.Time            `json:"generatedAt"`
	NodeID                string               `json:"nodeId"`
	ValidatorAddress      string               `json:"validatorAddress,omitempty"`
	PeerCount             int                  `json:"peerCount"`
	PeerSyncEnabled       bool                 `json:"peerSyncEnabled"`
	HealthStatus          string               `json:"healthStatus"`
	CurrentAlertCount     int                  `json:"currentAlertCount"`
	CurrentObjectiveCount int                  `json:"currentObjectiveCount"`
	RuleCount             int                  `json:"ruleCount"`
	EnabledRuleCount      int                  `json:"enabledRuleCount"`
	DisabledRuleCount     int                  `json:"disabledRuleCount"`
	Groups                []RecordingRuleGroup `json:"groups"`
}

func (s *Server) handleRecordingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.buildRecordingRuleBundle(time.Now().UTC()))
}

func (s *Server) handlePrometheusRecordingRules(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", prometheusRecordingRuleContentType)
	_, _ = w.Write([]byte(s.buildPrometheusRecordingRules(time.Now().UTC())))
}

func (s *Server) buildRecordingRuleBundle(now time.Time) RecordingRuleBundleResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	alerts := s.buildAlertsResponse(now)
	slo := s.buildSLOSummary(now)
	groups := s.buildRecordingRuleGroups()

	response := RecordingRuleBundleResponse{
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

func (s *Server) buildPrometheusRecordingRules(now time.Time) string {
	bundle := s.buildRecordingRuleBundle(now)

	var builder strings.Builder
	builder.WriteString("# Zephyr recommended Prometheus recording rules\n")
	builder.WriteString("# generatedAt: ")
	builder.WriteString(bundle.GeneratedAt.Format(time.RFC3339Nano))
	builder.WriteString("\n# nodeId: ")
	builder.WriteString(bundle.NodeID)
	builder.WriteString("\ngroups:\n")

	for _, group := range bundle.Groups {
		enabledRules := make([]RecordingRule, 0, len(group.Rules))
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
			builder.WriteString("      - record: ")
			builder.WriteString(rule.Record)
			builder.WriteString("\n        expr: ")
			builder.WriteString(yamlSingleQuoted(rule.Expression))
			builder.WriteByte('\n')
			builder.WriteString("        labels:\n")
			builder.WriteString("          component: ")
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
		}
	}

	return builder.String()
}

func (s *Server) buildRecordingRuleGroups() []RecordingRuleGroup {
	peerSyncDisabledReason := ""
	switch {
	case !s.config.EnablePeerSync:
		peerSyncDisabledReason = "peer sync is disabled by configuration"
	case len(s.config.PeerURLs) == 0:
		peerSyncDisabledReason = "no peers are configured for peer sync"
	}
	throughputDisabledReason := s.settlementThroughputDisabledReason()

	groups := []RecordingRuleGroup{
		{
			Name:  "zephyr.readiness",
			Title: "Derived readiness rollups",
			Rules: []RecordingRule{
				newRecordingRule(
					"node readiness ready",
					"zephyr:node_readiness:ready",
					"readiness",
					"Canonical ready or not-ready signal for Zephyr node dashboards.",
					"Projects the base readiness gauge into a stable series for dashboards, recording-rule composition, and top-level service views.",
					"zephyr_node_ready",
					[]string{"zephyr_node_ready"},
					nil,
					[]string{"node_readiness"},
				),
				newRecordingRule(
					"node readiness at risk",
					"zephyr:node_readiness:at_risk",
					"readiness",
					"Reusable series for nodes serving traffic with reduced readiness margin.",
					"Projects the node_readiness SLO into an at-risk series that can feed dashboards, burn-rate style summaries, or fleet rollups.",
					"zephyr_slo_objective_status{objective=\"node_readiness\",status=\"at_risk\"}",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"node_readiness"},
				),
				newRecordingRule(
					"node readiness breached",
					"zephyr:node_readiness:breached",
					"readiness",
					"Reusable series for nodes currently failing readiness.",
					"Projects the node_readiness SLO into a breached series so dashboards and fleet summaries can separate hard outages from early warnings.",
					"zephyr_slo_objective_status{objective=\"node_readiness\",status=\"breached\"}",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"node_readiness"},
				),
			},
		},
		{
			Name:  "zephyr.consensus",
			Title: "Consensus continuity and recovery rollups",
			Rules: []RecordingRule{
				newRecordingRule(
					"consensus recovery backlog",
					"zephyr:consensus:recovery_backlog",
					"consensus",
					"Current retained recovery backlog for local consensus follow-up.",
					"Carries the retained pending-action count into a stable dashboard series that operators can pair with diagnostics, alerts, and replay history.",
					"zephyr_recovery_pending_action_count",
					[]string{"zephyr_recovery_pending_action_count"},
					[]string{"consensus_recovery_backlog"},
					[]string{"consensus_continuity"},
				),
				newRecordingRule(
					"consensus continuity at risk",
					"zephyr:consensus_continuity:at_risk",
					"consensus",
					"Reusable series for degraded but not yet blocked next-height agreement.",
					"Projects the consensus_continuity SLO into an at-risk series so dashboards can show unstable rounds separately from fully blocked ones.",
					"zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"at_risk\"}",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"consensus_continuity"},
				),
				newRecordingRule(
					"consensus continuity breached",
					"zephyr:consensus_continuity:breached",
					"consensus",
					"Reusable series for blocked next-height consensus.",
					"Projects the consensus_continuity SLO into a breached series so operators can build fleet-level outage views without re-encoding the objective logic.",
					"zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"breached\"}",
					[]string{"zephyr_slo_objective_status"},
					nil,
					[]string{"consensus_continuity"},
				),
			},
		},
		{
			Name:  "zephyr.throughput",
			Title: "Settlement throughput and queue-drain rollups",
			Rules: []RecordingRule{
				disableRecordingRule(
					newRecordingRule(
						"settlement throughput at risk",
						"zephyr:settlement_throughput:at_risk",
						"throughput",
						"Reusable series for slowed queue drain under automatic block production.",
						"Projects the settlement_throughput SLO into an at-risk series so dashboards can surface slower-than-expected queue drain separately from fully stalled settlement.",
						"zephyr_slo_objective_status{objective=\"settlement_throughput\",status=\"at_risk\"}",
						[]string{"zephyr_slo_objective_status"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement throughput breached",
						"zephyr:settlement_throughput:breached",
						"throughput",
						"Reusable series for stalled queue drain under automatic block production.",
						"Projects the settlement_throughput SLO into a breached series so dashboards and fleet rollups can separate settlement stalls from milder queue pressure.",
						"zephyr_slo_objective_status{objective=\"settlement_throughput\",status=\"breached\"}",
						[]string{"zephyr_slo_objective_status"},
						[]string{settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain warn utilization",
						"zephyr:settlement_queue_drain:warn_utilization",
						"throughput",
						"Normalized queue-drain lag versus the warn threshold.",
						"Projects raw settlement queue-drain lag against the warn threshold so dashboards and fleet summaries can compare backlog pressure across nodes with different block cadences.",
						"zephyr_settlement_queue_drain_lag_seconds / clamp_min(zephyr_settlement_queue_drain_threshold_seconds{threshold=\"warn\"}, 1)",
						[]string{"zephyr_settlement_queue_drain_lag_seconds", "zephyr_settlement_queue_drain_threshold_seconds"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain fail utilization",
						"zephyr:settlement_queue_drain:fail_utilization",
						"throughput",
						"Normalized queue-drain lag versus the fail threshold.",
						"Projects raw settlement queue-drain lag against the fail threshold so dashboards and fleet summaries can see how close the node is to a fully stalled settlement window.",
						"zephyr_settlement_queue_drain_lag_seconds / clamp_min(zephyr_settlement_queue_drain_threshold_seconds{threshold=\"fail\"}, 1)",
						[]string{"zephyr_settlement_queue_drain_lag_seconds", "zephyr_settlement_queue_drain_threshold_seconds"},
						[]string{settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate seconds 1m",
						"zephyr:settlement_queue_drain:estimate_seconds_1m",
						"throughput",
						"Estimated queue-drain seconds at the recent 1-minute throughput baseline.",
						"Carries the structured 1-minute settlement drain estimate into a canonical series for dashboards and summaries when a recent short-horizon throughput baseline exists.",
						"zephyr_settlement_estimated_queue_drain_seconds{window=\"1m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_seconds"},
						[]string{settlementThroughputAlertReduced, settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate seconds 5m",
						"zephyr:settlement_queue_drain:estimate_seconds_5m",
						"throughput",
						"Estimated queue-drain seconds at the recent 5-minute throughput baseline.",
						"Carries the structured 5-minute settlement drain estimate into a canonical series so dashboards can compare the current backlog against a medium-horizon committed-throughput baseline.",
						"zephyr_settlement_estimated_queue_drain_seconds{window=\"5m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_seconds"},
						[]string{settlementThroughputAlertReduced, settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate seconds 15m",
						"zephyr:settlement_queue_drain:estimate_seconds_15m",
						"throughput",
						"Estimated queue-drain seconds at the recent 15-minute throughput baseline.",
						"Carries the structured 15-minute settlement drain estimate into a canonical series for longer-horizon backlog expectations when operators need to compare short bursts against a steadier baseline.",
						"zephyr_settlement_estimated_queue_drain_seconds{window=\"15m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_seconds"},
						[]string{settlementThroughputAlertReduced, settlementThroughputAlertStalled},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate warn utilization 1m",
						"zephyr:settlement_queue_drain:estimate_warn_utilization_1m",
						"throughput",
						"Estimated queue-drain time normalized against the warn threshold at the 1-minute throughput baseline.",
						"Projects the 1-minute settlement drain estimate against the warn threshold so dashboards can compare short-horizon backlog pressure without re-encoding interval math.",
						"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio{window=\"1m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate warn utilization 5m",
						"zephyr:settlement_queue_drain:estimate_warn_utilization_5m",
						"throughput",
						"Estimated queue-drain time normalized against the warn threshold at the 5-minute throughput baseline.",
						"Projects the 5-minute settlement drain estimate against the warn threshold so dashboards can compare medium-horizon backlog pressure without re-encoding interval math.",
						"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio{window=\"5m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate warn utilization 15m",
						"zephyr:settlement_queue_drain:estimate_warn_utilization_15m",
						"throughput",
						"Estimated queue-drain time normalized against the warn threshold at the 15-minute throughput baseline.",
						"Projects the 15-minute settlement drain estimate against the warn threshold so dashboards can compare steadier backlog pressure without re-encoding interval math.",
						"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio{window=\"15m\"}",
						[]string{"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"settlement queue-drain estimate warn utilization max",
						"zephyr:settlement_queue_drain:estimate_warn_utilization_max",
						"throughput",
						"Highest estimated queue-drain time normalized against the warn threshold across recent throughput windows.",
						"Projects the worst available settlement drain estimate against the warn threshold so dashboards and fleet summaries can track the highest projected queue-drain pressure without re-aggregating per-window series in PromQL.",
						"max without(window) (zephyr_settlement_estimated_queue_drain_warn_utilization_ratio)",
						[]string{"zephyr_settlement_estimated_queue_drain_warn_utilization_ratio"},
						[]string{settlementThroughputAlertReduced},
						[]string{"settlement_throughput"},
					),
					throughputDisabledReason,
				),
			},
		},
		{
			Name:  "zephyr.peer_sync",
			Title: "Peer sync continuity and repair rollups",
			Rules: []RecordingRule{
				disableRecordingRule(
					newRecordingRule(
						"peer sync admitted ratio",
						"zephyr:peer_sync:admitted_ratio",
						"peer_sync",
						"Share of configured peers currently admitted by policy.",
						"Normalizes admitted peers by configured peers to make peer policy regressions easier to compare across nodes with different topology sizes.",
						"zephyr_peer_runtime_admitted_count / clamp_min(zephyr_peer_runtime_configured_count, 1)",
						[]string{"zephyr_peer_runtime_admitted_count", "zephyr_peer_runtime_configured_count"},
						nil,
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"peer sync incident pressure by peer",
						"zephyr:peer_sync:incident_pressure_by_peer",
						"peer_sync",
						"Reusable per-peer retained incident pressure series for dashboards and fleet drill-down.",
						"Carries the retained per-peer incident occurrence metric into the recording-rule bundle so dashboards can pivot on peer pressure without rebuilding labels or rollup conventions in PromQL.",
						"zephyr_peer_sync_peer_occurrence_count",
						[]string{"zephyr_peer_sync_peer_occurrence_count"},
						[]string{"peer_import_blocked", "peer_admission_blocked", "peer_replication_blocked"},
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"peer sync continuity at risk",
						"zephyr:peer_sync_continuity:at_risk",
						"peer_sync",
						"Reusable series for degraded or warming peer-sync continuity.",
						"Projects the peer_sync_continuity SLO into an at-risk series so dashboards can separate startup or partial repair conditions from outright peer loss.",
						"zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"at_risk\"}",
						[]string{"zephyr_slo_objective_status"},
						nil,
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
				disableRecordingRule(
					newRecordingRule(
						"peer sync continuity breached",
						"zephyr:peer_sync_continuity:breached",
						"peer_sync",
						"Reusable series for peer-sync outage conditions.",
						"Projects the peer_sync_continuity SLO into a breached series so multi-node dashboards can roll up peer outages without repeating the objective logic.",
						"zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"breached\"}",
						[]string{"zephyr_slo_objective_status"},
						nil,
						[]string{"peer_sync_continuity"},
					),
					peerSyncDisabledReason,
				),
			},
		},
		{
			Name:  "zephyr.operator",
			Title: "Operator-facing alert, objective, and throughput rollups",
			Rules: []RecordingRule{
				newRecordingRule(
					"chain transactions per second 1m",
					"zephyr:chain:transactions_per_second_1m",
					"observability",
					"Recent committed transaction throughput over the last minute.",
					"Carries the 1-minute transaction throughput into a stable series for dashboards and performance baselines without repeating the window selector in downstream PromQL.",
					"zephyr_chain_window_transactions_per_second{window=\"1m\"}",
					[]string{"zephyr_chain_window_transactions_per_second"},
					nil,
					nil,
				),
				newRecordingRule(
					"chain transactions per second 5m",
					"zephyr:chain:transactions_per_second_5m",
					"observability",
					"Recent committed transaction throughput over the last five minutes.",
					"Carries the 5-minute transaction throughput into a stable series so operators can compare current bursts against a less noisy rolling baseline.",
					"zephyr_chain_window_transactions_per_second{window=\"5m\"}",
					[]string{"zephyr_chain_window_transactions_per_second"},
					nil,
					nil,
				),
				newRecordingRule(
					"chain transactions per second 15m",
					"zephyr:chain:transactions_per_second_15m",
					"observability",
					"Recent committed transaction throughput over the last fifteen minutes.",
					"Carries the 15-minute transaction throughput into a stable series for longer smoothing when evaluating sustained throughput rather than short bursts.",
					"zephyr_chain_window_transactions_per_second{window=\"15m\"}",
					[]string{"zephyr_chain_window_transactions_per_second"},
					nil,
					nil,
				),
				newRecordingRule(
					"critical alert count",
					"zephyr:alerts:critical_count",
					"observability",
					"Current number of active critical alerts.",
					"Carries the critical alert count into a stable series for dashboard summaries, NOC-style tables, and higher-level alertmanager routing views.",
					"zephyr_alert_count_by_severity{severity=\"critical\"}",
					[]string{"zephyr_alert_count_by_severity"},
					nil,
					nil,
				),
				newRecordingRule(
					"breached objective count",
					"zephyr:slo:breached_count",
					"observability",
					"Current number of breached Zephyr objectives.",
					"Carries the breached-objective count into a single series that can feed fleet dashboards and reporting without re-aggregating the status labels.",
					"zephyr_slo_status_count{status=\"breached\"}",
					[]string{"zephyr_slo_status_count"},
					nil,
					nil,
				),
			},
		},
	}

	for idx := range groups {
		sort.Slice(groups[idx].Rules, func(i, j int) bool {
			return groups[idx].Rules[i].Record < groups[idx].Rules[j].Record
		})
	}
	return groups
}

func newRecordingRule(name string, record string, component string, summary string, description string, expression string, sourceMetrics []string, relatedAlerts []string, relatedObjectives []string) RecordingRule {
	return RecordingRule{
		Name:              name,
		Record:            record,
		Component:         component,
		Summary:           summary,
		Description:       description,
		Expression:        expression,
		Enabled:           true,
		SourceMetrics:     cloneStrings(sourceMetrics),
		RelatedAlertCodes: cloneStrings(relatedAlerts),
		RelatedObjectives: cloneStrings(relatedObjectives),
	}
}

func disableRecordingRule(rule RecordingRule, reason string) RecordingRule {
	if strings.TrimSpace(reason) == "" {
		return rule
	}
	rule.Enabled = false
	rule.DisabledReason = reason
	return rule
}

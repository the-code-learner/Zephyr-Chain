package api

import (
	"encoding/json"
	"net/http"
	"time"
)

const grafanaDashboardContentType = "application/json; charset=utf-8"

type DashboardQuery struct {
	Ref        string `json:"ref"`
	Expression string `json:"expression"`
	Legend     string `json:"legend,omitempty"`
}

type DashboardPanel struct {
	ID                string           `json:"id"`
	Title             string           `json:"title"`
	Kind              string           `json:"kind"`
	Summary           string           `json:"summary"`
	Description       string           `json:"description,omitempty"`
	Unit              string           `json:"unit,omitempty"`
	Enabled           bool             `json:"enabled"`
	DisabledReason    string           `json:"disabledReason,omitempty"`
	Queries           []DashboardQuery `json:"queries,omitempty"`
	SourceMetrics     []string         `json:"sourceMetrics,omitempty"`
	SourceEndpoints   []string         `json:"sourceEndpoints,omitempty"`
	RecordingRules    []string         `json:"recordingRules,omitempty"`
	RelatedAlertCodes []string         `json:"relatedAlertCodes,omitempty"`
	RelatedObjectives []string         `json:"relatedObjectives,omitempty"`
}

type Dashboard struct {
	Name            string           `json:"name"`
	Title           string           `json:"title"`
	Summary         string           `json:"summary"`
	Description     string           `json:"description,omitempty"`
	Enabled         bool             `json:"enabled"`
	DisabledReason  string           `json:"disabledReason,omitempty"`
	Tags            []string         `json:"tags,omitempty"`
	SourceEndpoints []string         `json:"sourceEndpoints,omitempty"`
	RecordingRules  []string         `json:"recordingRules,omitempty"`
	Panels          []DashboardPanel `json:"panels"`
}

type DashboardBundleResponse struct {
	GeneratedAt            time.Time   `json:"generatedAt"`
	NodeID                 string      `json:"nodeId"`
	ValidatorAddress       string      `json:"validatorAddress,omitempty"`
	PeerCount              int         `json:"peerCount"`
	PeerSyncEnabled        bool        `json:"peerSyncEnabled"`
	StructuredLogsEnabled  bool        `json:"structuredLogsEnabled"`
	HealthStatus           string      `json:"healthStatus"`
	CurrentAlertCount      int         `json:"currentAlertCount"`
	CurrentObjectiveCount  int         `json:"currentObjectiveCount"`
	DashboardCount         int         `json:"dashboardCount"`
	EnabledDashboardCount  int         `json:"enabledDashboardCount"`
	DisabledDashboardCount int         `json:"disabledDashboardCount"`
	PanelCount             int         `json:"panelCount"`
	EnabledPanelCount      int         `json:"enabledPanelCount"`
	DisabledPanelCount     int         `json:"disabledPanelCount"`
	Dashboards             []Dashboard `json:"dashboards"`
}

type GrafanaDashboardBundleResponse struct {
	GeneratedAt      time.Time                `json:"generatedAt"`
	NodeID           string                   `json:"nodeId"`
	ValidatorAddress string                   `json:"validatorAddress,omitempty"`
	DashboardCount   int                      `json:"dashboardCount"`
	PanelCount       int                      `json:"panelCount"`
	Dashboards       []GrafanaDashboardExport `json:"dashboards"`
}

type GrafanaDashboardExport struct {
	Name      string           `json:"name"`
	Title     string           `json:"title"`
	Filename  string           `json:"filename"`
	Dashboard GrafanaDashboard `json:"dashboard"`
}

type GrafanaDashboard struct {
	UID           string            `json:"uid"`
	Title         string            `json:"title"`
	Description   string            `json:"description,omitempty"`
	Tags          []string          `json:"tags,omitempty"`
	SchemaVersion int               `json:"schemaVersion"`
	Version       int               `json:"version"`
	Editable      bool              `json:"editable"`
	GraphTooltip  int               `json:"graphTooltip"`
	Refresh       string            `json:"refresh,omitempty"`
	Time          GrafanaTimeRange  `json:"time"`
	Templating    GrafanaTemplating `json:"templating"`
	Panels        []GrafanaPanel    `json:"panels"`
}

type GrafanaTimeRange struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type GrafanaTemplating struct {
	List []map[string]any `json:"list"`
}

type GrafanaPanel struct {
	ID          int                `json:"id"`
	Title       string             `json:"title"`
	Type        string             `json:"type"`
	Description string             `json:"description,omitempty"`
	GridPos     GrafanaGridPos     `json:"gridPos"`
	Targets     []GrafanaTarget    `json:"targets,omitempty"`
	FieldConfig GrafanaFieldConfig `json:"fieldConfig"`
	Options     map[string]any     `json:"options,omitempty"`
}

type GrafanaGridPos struct {
	H int `json:"h"`
	W int `json:"w"`
	X int `json:"x"`
	Y int `json:"y"`
}

type GrafanaTarget struct {
	RefID        string `json:"refId"`
	Expr         string `json:"expr"`
	LegendFormat string `json:"legendFormat,omitempty"`
	Instant      bool   `json:"instant,omitempty"`
	Range        bool   `json:"range,omitempty"`
}

type GrafanaFieldConfig struct {
	Defaults  map[string]any `json:"defaults,omitempty"`
	Overrides []any          `json:"overrides"`
}

func (s *Server) handleDashboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	writeJSON(w, http.StatusOK, s.buildDashboardBundle(time.Now().UTC()))
}

func (s *Server) handleGrafanaDashboards(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", grafanaDashboardContentType)
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(s.buildGrafanaDashboardBundle(time.Now().UTC()))
}

func (s *Server) buildDashboardBundle(now time.Time) DashboardBundleResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	alerts := s.buildAlertsResponse(now)
	slo := s.buildSLOSummary(now)
	dashboards := s.buildDashboards()

	response := DashboardBundleResponse{
		GeneratedAt:           now,
		NodeID:                s.nodeID,
		ValidatorAddress:      s.config.ValidatorAddress,
		PeerCount:             len(s.config.PeerURLs),
		PeerSyncEnabled:       s.config.EnablePeerSync,
		StructuredLogsEnabled: s.config.EnableStructuredLogs,
		HealthStatus:          slo.HealthStatus,
		CurrentAlertCount:     alerts.AlertCount,
		CurrentObjectiveCount: slo.ObjectiveCount,
		Dashboards:            dashboards,
	}

	for _, dashboard := range dashboards {
		response.DashboardCount++
		if dashboard.Enabled {
			response.EnabledDashboardCount++
		} else {
			response.DisabledDashboardCount++
		}
		for _, panel := range dashboard.Panels {
			response.PanelCount++
			if panel.Enabled {
				response.EnabledPanelCount++
			} else {
				response.DisabledPanelCount++
			}
		}
	}

	return response
}

func (s *Server) buildGrafanaDashboardBundle(now time.Time) GrafanaDashboardBundleResponse {
	bundle := s.buildDashboardBundle(now)
	response := GrafanaDashboardBundleResponse{
		GeneratedAt:      bundle.GeneratedAt,
		NodeID:           bundle.NodeID,
		ValidatorAddress: bundle.ValidatorAddress,
		Dashboards:       make([]GrafanaDashboardExport, 0, bundle.EnabledDashboardCount),
	}

	for _, dashboard := range bundle.Dashboards {
		if !dashboard.Enabled {
			continue
		}

		enabledPanels := filterEnabledPanels(dashboard.Panels)
		if len(enabledPanels) == 0 {
			continue
		}

		panels := make([]GrafanaPanel, 0, len(enabledPanels))
		for idx, panel := range enabledPanels {
			panels = append(panels, buildGrafanaPanel(panel, idx))
		}

		response.Dashboards = append(response.Dashboards, GrafanaDashboardExport{
			Name:      dashboard.Name,
			Title:     dashboard.Title,
			Filename:  grafanaDashboardFilename(dashboard.Name),
			Dashboard: buildGrafanaDashboard(dashboard, panels),
		})
		response.DashboardCount++
		response.PanelCount += len(panels)
	}

	return response
}

func (s *Server) buildDashboards() []Dashboard {
	peerSyncDisabledReason := dashboardPeerSyncDisabledReason(s.config.EnablePeerSync, s.config.PeerURLs)

	return []Dashboard{
		{
			Name:        "zephyr.overview",
			Title:       "Zephyr Operator Overview",
			Summary:     "Top-level readiness, alert, and objective posture for the current node.",
			Description: "Start here before drilling into consensus continuity or peer incidents.",
			Enabled:     true,
			Tags:        []string{"zephyr", "overview", "readiness", "alerts", "slo"},
			SourceEndpoints: []string{
				"/metrics",
				"/v1/health",
				"/v1/alerts",
				"/v1/slo",
				"/v1/recording-rules",
			},
			RecordingRules: []string{
				"zephyr:node_readiness:ready",
				"zephyr:alerts:critical_count",
				"zephyr:slo:breached_count",
				"zephyr:settlement_throughput:at_risk",
				"zephyr:settlement_throughput:breached",
				"zephyr:chain:transactions_per_second_1m",
				"zephyr:chain:transactions_per_second_5m",
				"zephyr:chain:transactions_per_second_15m",
			},
			Panels: []DashboardPanel{
				newDashboardPanel(
					"node_readiness",
					"Node readiness",
					"stat",
					"Stable ready or not-ready summary for the node.",
					"Uses the canonical readiness recording rule so operators can anchor the dashboard on the same signal exported by the recording bundle.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:node_readiness:ready", Legend: "ready"}},
					[]string{"zephyr:node_readiness:ready"},
					[]string{"/metrics", "/v1/health", "/v1/recording-rules"},
					[]string{"zephyr:node_readiness:ready"},
					nil,
					[]string{"node_readiness"},
				),
				newDashboardPanel(
					"critical_alert_count",
					"Critical alert count",
					"stat",
					"Current count of active critical alerts.",
					"Highlights whether operator attention is needed immediately without rebuilding severity rollups in the dashboard layer.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:alerts:critical_count", Legend: "critical"}},
					[]string{"zephyr:alerts:critical_count"},
					[]string{"/metrics", "/v1/alerts", "/v1/recording-rules"},
					[]string{"zephyr:alerts:critical_count"},
					nil,
					nil,
				),
				newDashboardPanel(
					"breached_objective_count",
					"Breached objective count",
					"stat",
					"Current number of breached Zephyr objectives.",
					"Turns the SLO status projection into a top-level summary for NOC-style dashboards and fleet status pages.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:slo:breached_count", Legend: "breached"}},
					[]string{"zephyr:slo:breached_count"},
					[]string{"/metrics", "/v1/slo", "/v1/recording-rules"},
					[]string{"zephyr:slo:breached_count"},
					nil,
					nil,
				),
				newDashboardPanel(
					"transaction_throughput",
					"Recent transaction throughput",
					"bargauge",
					"Committed transactions per second across recent windows.",
					"Uses canonical throughput recording rules so operators can compare short bursts against medium and longer rolling baselines while evaluating headroom for future execution and confidential-compute lanes.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr:chain:transactions_per_second_1m", Legend: "tps_1m"},
						{Ref: "B", Expression: "zephyr:chain:transactions_per_second_5m", Legend: "tps_5m"},
						{Ref: "C", Expression: "zephyr:chain:transactions_per_second_15m", Legend: "tps_15m"},
					},
					[]string{"zephyr:chain:transactions_per_second_1m", "zephyr:chain:transactions_per_second_5m", "zephyr:chain:transactions_per_second_15m"},
					[]string{"/metrics", "/v1/metrics", "/v1/recording-rules"},
					[]string{"zephyr:chain:transactions_per_second_1m", "zephyr:chain:transactions_per_second_5m", "zephyr:chain:transactions_per_second_15m"},
					nil,
					nil,
				),
				newDashboardPanel(
					"settlement_throughput_state",
					"Settlement throughput state",
					"bargauge",
					"At-risk versus breached settlement-throughput rollups.",
					"Shows when queued transactions are draining slower than expected or have fully stalled under the configured automatic block cadence, using canonical recording rules instead of re-encoding objective logic in the dashboard layer.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr:settlement_throughput:at_risk", Legend: "at_risk"},
						{Ref: "B", Expression: "zephyr:settlement_throughput:breached", Legend: "breached"},
					},
					[]string{"zephyr:settlement_throughput:at_risk", "zephyr:settlement_throughput:breached"},
					[]string{"/metrics", "/v1/health", "/v1/alerts", "/v1/slo", "/v1/recording-rules"},
					[]string{"zephyr:settlement_throughput:at_risk", "zephyr:settlement_throughput:breached"},
					[]string{settlementThroughputAlertReduced, settlementThroughputAlertStalled},
					[]string{"settlement_throughput"},
				),
				newDashboardPanel(
					"alert_severity_mix",
					"Alert severity mix",
					"bargauge",
					"Warning and critical alert counts side by side.",
					"Pairs with the top-level alert count so operators can see whether the current load is noise, degraded service, or hard failure.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr_alert_count_by_severity{severity=\"critical\"}", Legend: "critical"},
						{Ref: "B", Expression: "zephyr_alert_count_by_severity{severity=\"warning\"}", Legend: "warning"},
					},
					[]string{"zephyr_alert_count_by_severity"},
					[]string{"/metrics", "/v1/alerts"},
					nil,
					nil,
					nil,
				),
				newDashboardPanel(
					"objective_status_mix",
					"Objective status mix",
					"bargauge",
					"Meeting, at-risk, breached, and not-applicable objective counts.",
					"Shows whether the node is trending toward trouble even when the top-level readiness gate is still green.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr_slo_status_count{status=\"meeting\"}", Legend: "meeting"},
						{Ref: "B", Expression: "zephyr_slo_status_count{status=\"at_risk\"}", Legend: "at_risk"},
						{Ref: "C", Expression: "zephyr_slo_status_count{status=\"breached\"}", Legend: "breached"},
						{Ref: "D", Expression: "zephyr_slo_status_count{status=\"not_applicable\"}", Legend: "not_applicable"},
					},
					[]string{"zephyr_slo_status_count"},
					[]string{"/metrics", "/v1/slo"},
					nil,
					nil,
					[]string{"node_readiness", "consensus_continuity", "peer_sync_continuity", "settlement_throughput"},
				),
			},
		},
		{
			Name:        "zephyr.consensus",
			Title:       "Zephyr Consensus And Recovery",
			Summary:     "Consensus continuity, recovery backlog, and diagnostic panels for the active node.",
			Description: "Use this dashboard when readiness or alert posture points to the next-height agreement path.",
			Enabled:     true,
			Tags:        []string{"zephyr", "consensus", "recovery", "diagnostics"},
			SourceEndpoints: []string{
				"/metrics",
				"/v1/status",
				"/v1/consensus",
				"/v1/dev/block-template",
				"/v1/recording-rules",
			},
			RecordingRules: []string{
				"zephyr:consensus:recovery_backlog",
				"zephyr:consensus_continuity:at_risk",
				"zephyr:consensus_continuity:breached",
			},
			Panels: []DashboardPanel{
				newDashboardPanel(
					"consensus_continuity_state",
					"Consensus continuity state",
					"bargauge",
					"At-risk versus breached consensus continuity rollups.",
					"Uses the same objective projection exported by the recording-rule bundle so dashboard state matches alert and SLO logic.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr:consensus_continuity:at_risk", Legend: "at_risk"},
						{Ref: "B", Expression: "zephyr:consensus_continuity:breached", Legend: "breached"},
					},
					[]string{"zephyr:consensus_continuity:at_risk", "zephyr:consensus_continuity:breached"},
					[]string{"/metrics", "/v1/slo", "/v1/recording-rules"},
					[]string{"zephyr:consensus_continuity:at_risk", "zephyr:consensus_continuity:breached"},
					nil,
					[]string{"consensus_continuity"},
				),
				newDashboardPanel(
					"consensus_recovery_backlog",
					"Consensus recovery backlog",
					"timeseries",
					"Pending retained recovery work over time.",
					"Tracks the durable recovery backlog that can block commit, import, or restart-safe progress.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:consensus:recovery_backlog", Legend: "pending_actions"}},
					[]string{"zephyr:consensus:recovery_backlog"},
					[]string{"/metrics", "/v1/status", "/v1/consensus", "/v1/recording-rules"},
					[]string{"zephyr:consensus:recovery_backlog"},
					[]string{"consensus_recovery_backlog"},
					[]string{"consensus_continuity"},
				),
				newDashboardPanel(
					"recovery_detail",
					"Replay and import backlog detail",
					"timeseries",
					"Replayable local actions and blocked import-recovery actions.",
					"Separates restart replay pressure from peer-import repair pressure when consensus continuity starts to degrade.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr_recovery_pending_replay_count", Legend: "pending_replay"},
						{Ref: "B", Expression: "zephyr_recovery_pending_import_count", Legend: "pending_import"},
					},
					[]string{"zephyr_recovery_pending_replay_count", "zephyr_recovery_pending_import_count"},
					[]string{"/metrics", "/v1/status", "/v1/consensus"},
					nil,
					[]string{"consensus_recovery_backlog"},
					[]string{"consensus_continuity"},
				),
				newDashboardPanel(
					"diagnostic_codes",
					"Consensus diagnostic codes",
					"bargauge",
					"Retained diagnostic counts grouped by operator-facing code.",
					"Useful for spotting template mismatches, stale rounds, conflicting proposals, and import failures without leaving the metrics stack.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr_consensus_diagnostic_by_code_count", Legend: "{{code}}"}},
					[]string{"zephyr_consensus_diagnostic_by_code_count"},
					[]string{"/metrics", "/v1/status", "/v1/consensus"},
					nil,
					nil,
					nil,
				),
				newDashboardPanel(
					"round_and_height",
					"Consensus round and next height",
					"timeseries",
					"Current round number and pending next height.",
					"Pairs protocol progress with the recovery and diagnostics panels so stalls are visible without reloading the raw APIs.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr_consensus_current_round", Legend: "current_round"},
						{Ref: "B", Expression: "zephyr_consensus_next_height", Legend: "next_height"},
					},
					[]string{"zephyr_consensus_current_round", "zephyr_consensus_next_height"},
					[]string{"/metrics", "/v1/status", "/v1/consensus"},
					nil,
					nil,
					nil,
				),
			},
		},
		disableDashboard(Dashboard{
			Name:        "zephyr.peer_sync",
			Title:       "Zephyr Peer Sync And Repair",
			Summary:     "Peer admission, reachability, continuity, and incident panels.",
			Description: "Use this dashboard when operators need to separate peer loss, admission problems, and repair churn.",
			Enabled:     true,
			Tags:        []string{"zephyr", "peer-sync", "repair", "networking"},
			SourceEndpoints: []string{
				"/metrics",
				"/v1/peers",
				"/v1/status",
				"/v1/alerts",
				"/v1/recording-rules",
			},
			RecordingRules: []string{
				"zephyr:peer_sync:admitted_ratio",
				"zephyr:peer_sync_continuity:at_risk",
				"zephyr:peer_sync_continuity:breached",
			},
			Panels: []DashboardPanel{
				newDashboardPanel(
					"peer_admitted_ratio",
					"Admitted peer ratio",
					"stat",
					"Share of configured peers currently admitted by policy.",
					"Normalizes admitted peers by topology size so policy regressions stand out across nodes with different peer counts.",
					"percentunit",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:peer_sync:admitted_ratio", Legend: "admitted_ratio"}},
					[]string{"zephyr:peer_sync:admitted_ratio"},
					[]string{"/metrics", "/v1/peers", "/v1/recording-rules"},
					[]string{"zephyr:peer_sync:admitted_ratio"},
					nil,
					[]string{"peer_sync_continuity"},
				),
				newDashboardPanel(
					"peer_sync_continuity_state",
					"Peer sync continuity state",
					"bargauge",
					"At-risk versus breached peer-sync continuity rollups.",
					"Shows whether the node is merely degraded or has already lost its admitted and reachable peer path.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr:peer_sync_continuity:at_risk", Legend: "at_risk"},
						{Ref: "B", Expression: "zephyr:peer_sync_continuity:breached", Legend: "breached"},
					},
					[]string{"zephyr:peer_sync_continuity:at_risk", "zephyr:peer_sync_continuity:breached"},
					[]string{"/metrics", "/v1/slo", "/v1/recording-rules"},
					[]string{"zephyr:peer_sync_continuity:at_risk", "zephyr:peer_sync_continuity:breached"},
					[]string{"peer_sync_unavailable"},
					[]string{"peer_sync_continuity"},
				),
				newDashboardPanel(
					"peer_runtime_state",
					"Peer runtime state mix",
					"bargauge",
					"Configured peers grouped by current runtime sync state.",
					"Highlights whether peers are warming up, unreachable, blocked on import, or moving through recovery states.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr_peer_runtime_by_sync_state_count", Legend: "{{state}}"}},
					[]string{"zephyr_peer_runtime_by_sync_state_count"},
					[]string{"/metrics", "/v1/peers"},
					nil,
					nil,
					nil,
				),
				newDashboardPanel(
					"peer_incident_occurrences",
					"Peer incident occurrences by state",
					"bargauge",
					"Retained incident occurrence counts grouped by dominant peer state.",
					"Summarizes whether recent trouble is dominated by unreachable peers, unadmitted peers, replication failures, import failures, or snapshot restores.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr_peer_sync_state_occurrence_count", Legend: "{{state}}"}},
					[]string{"zephyr_peer_sync_state_occurrence_count"},
					[]string{"/metrics", "/v1/peers", "/v1/status"},
					nil,
					[]string{"peer_import_blocked", "peer_admission_blocked", "peer_replication_blocked"},
					nil,
				),
				newDashboardPanel(
					"peer_incident_reasons",
					"Peer incident reasons",
					"bargauge",
					"Retained incident occurrence counts grouped by dominant peer reason.",
					"Separates admission causes, snapshot-repair reasons, and proposal, vote, or block replication failures across peers.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr_peer_sync_reason_occurrence_count", Legend: "{{reason}}"}},
					[]string{"zephyr_peer_sync_reason_occurrence_count"},
					[]string{"/metrics", "/v1/peers", "/v1/status"},
					nil,
					[]string{"peer_import_blocked", "peer_admission_blocked", "peer_replication_blocked"},
					nil,
				),
				newDashboardPanel(
					"peer_incident_error_codes",
					"Peer incident error codes",
					"bargauge",
					"Retained incident occurrence counts grouped by dominant peer error code.",
					"Separates replication transport failures from import-blocked consensus errors so repair churn is easier to diagnose from the dashboard bundle alone.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr_peer_sync_error_code_occurrence_count", Legend: "{{code}}"}},
					[]string{"zephyr_peer_sync_error_code_occurrence_count"},
					[]string{"/metrics", "/v1/peers", "/v1/status"},
					nil,
					[]string{"peer_import_blocked", "peer_replication_blocked"},
					nil,
				),
				newDashboardPanel(
					"peer_incidents_by_peer",
					"Peer incident pressure by peer",
					"bargauge",
					"Retained incident occurrence counts grouped by peer.",
					"Highlights which peers are driving the retained incident backlog while carrying the latest dominant state, reason, and error code as Prometheus labels for deeper dashboard drill-down.",
					"none",
					[]DashboardQuery{{Ref: "A", Expression: "zephyr:peer_sync:incident_pressure_by_peer", Legend: "{{peer_url}} {{latest_state}}"}},
					[]string{"zephyr:peer_sync:incident_pressure_by_peer"},
					[]string{"/metrics", "/v1/metrics", "/v1/status", "/v1/recording-rules"},
					[]string{"zephyr:peer_sync:incident_pressure_by_peer"},
					[]string{"peer_import_blocked", "peer_admission_blocked", "peer_replication_blocked"},
					[]string{"peer_sync_continuity"},
				),
				newDashboardPanel(
					"peer_reachability_summary",
					"Peer reachability summary",
					"bargauge",
					"Configured, reachable, admitted, unreachable, and unadmitted peer counts.",
					"Provides a compact operator view before drilling into individual peer evidence on `/v1/peers`.",
					"none",
					[]DashboardQuery{
						{Ref: "A", Expression: "zephyr_peer_runtime_configured_count", Legend: "configured"},
						{Ref: "B", Expression: "zephyr_peer_runtime_reachable_count", Legend: "reachable"},
						{Ref: "C", Expression: "zephyr_peer_runtime_admitted_count", Legend: "admitted"},
						{Ref: "D", Expression: "zephyr_peer_runtime_unreachable_count", Legend: "unreachable"},
						{Ref: "E", Expression: "zephyr_peer_runtime_unadmitted_count", Legend: "unadmitted"},
					},
					[]string{
						"zephyr_peer_runtime_configured_count",
						"zephyr_peer_runtime_reachable_count",
						"zephyr_peer_runtime_admitted_count",
						"zephyr_peer_runtime_unreachable_count",
						"zephyr_peer_runtime_unadmitted_count",
					},
					[]string{"/metrics", "/v1/peers"},
					nil,
					nil,
					nil,
				),
			},
		}, peerSyncDisabledReason),
	}
}

func newDashboardPanel(id string, title string, kind string, summary string, description string, unit string, queries []DashboardQuery, sourceMetrics []string, sourceEndpoints []string, recordingRules []string, relatedAlertCodes []string, relatedObjectives []string) DashboardPanel {
	return DashboardPanel{
		ID:                id,
		Title:             title,
		Kind:              kind,
		Summary:           summary,
		Description:       description,
		Unit:              unit,
		Enabled:           true,
		Queries:           append([]DashboardQuery(nil), queries...),
		SourceMetrics:     cloneStrings(sourceMetrics),
		SourceEndpoints:   cloneStrings(sourceEndpoints),
		RecordingRules:    cloneStrings(recordingRules),
		RelatedAlertCodes: cloneStrings(relatedAlertCodes),
		RelatedObjectives: cloneStrings(relatedObjectives),
	}
}

func disableDashboard(dashboard Dashboard, reason string) Dashboard {
	if reason == "" {
		return dashboard
	}
	dashboard.Enabled = false
	dashboard.DisabledReason = reason
	for idx := range dashboard.Panels {
		dashboard.Panels[idx].Enabled = false
		dashboard.Panels[idx].DisabledReason = reason
	}
	return dashboard
}

func dashboardPeerSyncDisabledReason(enabled bool, peerURLs []string) string {
	switch {
	case !enabled:
		return "peer sync is disabled by configuration"
	case len(peerURLs) == 0:
		return "no peers are configured for peer sync"
	default:
		return ""
	}
}

func filterEnabledPanels(panels []DashboardPanel) []DashboardPanel {
	if len(panels) == 0 {
		return nil
	}
	enabled := make([]DashboardPanel, 0, len(panels))
	for _, panel := range panels {
		if panel.Enabled {
			enabled = append(enabled, panel)
		}
	}
	return enabled
}

func grafanaDashboardFilename(name string) string {
	switch name {
	case "zephyr.overview":
		return "zephyr-overview.grafana-dashboard.json"
	case "zephyr.consensus":
		return "zephyr-consensus.grafana-dashboard.json"
	case "zephyr.peer_sync":
		return "zephyr-peer-sync.grafana-dashboard.json"
	default:
		return name + ".grafana-dashboard.json"
	}
}

func buildGrafanaDashboard(dashboard Dashboard, panels []GrafanaPanel) GrafanaDashboard {
	return GrafanaDashboard{
		UID:           grafanaDashboardUID(dashboard.Name),
		Title:         dashboard.Title,
		Description:   dashboard.Description,
		Tags:          cloneStrings(dashboard.Tags),
		SchemaVersion: 39,
		Version:       1,
		Editable:      true,
		GraphTooltip:  0,
		Refresh:       "30s",
		Time: GrafanaTimeRange{
			From: "now-6h",
			To:   "now",
		},
		Templating: GrafanaTemplating{
			List: []map[string]any{},
		},
		Panels: panels,
	}
}

func grafanaDashboardUID(name string) string {
	switch name {
	case "zephyr.overview":
		return "zephyr-overview"
	case "zephyr.consensus":
		return "zephyr-consensus"
	case "zephyr.peer_sync":
		return "zephyr-peer-sync"
	default:
		return name
	}
}

func buildGrafanaPanel(panel DashboardPanel, index int) GrafanaPanel {
	gridWidth := 8
	gridHeight := 6
	column := index % 3
	row := index / 3

	targets := make([]GrafanaTarget, 0, len(panel.Queries))
	for _, query := range panel.Queries {
		targets = append(targets, GrafanaTarget{
			RefID:        query.Ref,
			Expr:         query.Expression,
			LegendFormat: query.Legend,
			Instant:      panel.Kind != "timeseries",
			Range:        panel.Kind == "timeseries",
		})
	}

	defaults := map[string]any{}
	if panel.Unit != "" {
		defaults["unit"] = panel.Unit
	}

	return GrafanaPanel{
		ID:          index + 1,
		Title:       panel.Title,
		Type:        grafanaPanelType(panel.Kind),
		Description: panel.Description,
		GridPos: GrafanaGridPos{
			H: gridHeight,
			W: gridWidth,
			X: column * gridWidth,
			Y: row * gridHeight,
		},
		Targets: targets,
		FieldConfig: GrafanaFieldConfig{
			Defaults:  defaults,
			Overrides: []any{},
		},
		Options: grafanaPanelOptions(panel.Kind),
	}
}

func grafanaPanelType(kind string) string {
	switch kind {
	case "stat":
		return "stat"
	case "bargauge":
		return "bargauge"
	default:
		return "timeseries"
	}
}

func grafanaPanelOptions(kind string) map[string]any {
	switch kind {
	case "stat":
		return map[string]any{
			"colorMode":   "value",
			"graphMode":   "none",
			"justifyMode": "auto",
			"textMode":    "auto",
			"reduceOptions": map[string]any{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
		}
	case "bargauge":
		return map[string]any{
			"displayMode": "basic",
			"orientation": "horizontal",
			"reduceOptions": map[string]any{
				"calcs":  []string{"lastNotNull"},
				"fields": "",
				"values": false,
			},
			"showUnfilled": true,
		}
	default:
		return map[string]any{
			"legend": map[string]any{
				"displayMode": "list",
				"placement":   "bottom",
			},
			"tooltip": map[string]any{
				"mode": "single",
				"sort": "none",
			},
		}
	}
}

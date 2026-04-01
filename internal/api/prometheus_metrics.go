package api

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

const prometheusContentType = "text/plain; version=0.0.4; charset=utf-8"

type prometheusLabel struct {
	Name  string
	Value string
}

type prometheusMetricWriter struct {
	builder  strings.Builder
	declared map[string]struct{}
}

func (s *Server) handlePrometheusMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", prometheusContentType)
	_, _ = w.Write([]byte(s.buildPrometheusMetrics(time.Now().UTC())))
}

func (s *Server) buildPrometheusMetrics(now time.Time) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	status := s.ledger.Status()
	consensusView := s.ledger.Consensus()
	recovery := s.ledger.ConsensusRecovery()
	actionMetrics := s.ledger.ConsensusActionMetrics()
	diagnosticMetrics := s.ledger.ConsensusDiagnosticMetrics()
	throughput := s.ledger.ChainThroughputMetrics(now)
	settlementThroughput := s.buildSettlementThroughputMetrics(now)
	peerSummary := s.ledger.PeerSyncSummary()
	peerRuntime := buildPeerRuntimeMetrics(s.peerSnapshot())
	health := s.buildHealthResponse(now)
	alerts := s.buildAlertsResponse(now)
	slo := s.buildSLOSummary(now)

	writer := newPrometheusMetricWriter()

	writer.gauge(
		"zephyr_node_info",
		"Static node identity and runtime flag information.",
		1,
		prometheusLabel{Name: "node_id", Value: s.nodeID},
		prometheusLabel{Name: "validator_address", Value: s.config.ValidatorAddress},
		prometheusLabel{Name: "block_production", Value: strconv.FormatBool(s.config.EnableBlockProduction)},
		prometheusLabel{Name: "consensus_automation", Value: strconv.FormatBool(s.config.EnableConsensusAutomation)},
		prometheusLabel{Name: "peer_sync", Value: strconv.FormatBool(s.config.EnablePeerSync)},
		prometheusLabel{Name: "structured_logs", Value: strconv.FormatBool(s.config.EnableStructuredLogs)},
		prometheusLabel{Name: "peer_identity_required", Value: strconv.FormatBool(s.peerIdentityRequired())},
		prometheusLabel{Name: "proposer_schedule_enforced", Value: strconv.FormatBool(s.config.EnforceProposerSchedule)},
		prometheusLabel{Name: "consensus_certificates_required", Value: strconv.FormatBool(s.config.RequireConsensusCertificates)},
	)
	writer.gauge("zephyr_node_live", "Whether the node HTTP process is live.", boolMetric(health.Live))
	writer.gauge("zephyr_node_ready", "Whether the node is currently ready according to /v1/health.", boolMetric(health.Ready))
	writer.gauge("zephyr_health_warning_count", "Number of active readiness warnings derived from /v1/health.", float64(len(health.Warnings)))
	for _, statusLabel := range []string{healthStatusOK, healthStatusWarn, healthStatusFail} {
		writer.gauge(
			"zephyr_health_status",
			"Current node health status projected as mutually exclusive status labels.",
			boolMetric(health.Status == statusLabel),
			prometheusLabel{Name: "status", Value: statusLabel},
		)
	}
	for _, check := range health.Checks {
		for _, statusLabel := range []string{healthCheckPass, healthCheckWarn, healthCheckFail} {
			writer.gauge(
				"zephyr_health_check_status",
				"Current health-check status projected as mutually exclusive check and status labels.",
				boolMetric(check.Status == statusLabel),
				prometheusLabel{Name: "check", Value: check.Name},
				prometheusLabel{Name: "status", Value: statusLabel},
			)
		}
	}

	writer.gauge("zephyr_alert_count", "Number of currently active derived alerts.", float64(alerts.AlertCount))
	writer.gauge("zephyr_alert_count_by_severity", "Number of currently active derived alerts grouped by severity.", float64(alerts.CriticalCount), prometheusLabel{Name: "severity", Value: alertSeverityCritical})
	writer.gauge("zephyr_alert_count_by_severity", "Number of currently active derived alerts grouped by severity.", float64(alerts.WarningCount), prometheusLabel{Name: "severity", Value: alertSeverityWarning})
	for _, alert := range alerts.Alerts {
		labels := []prometheusLabel{
			{Name: "code", Value: alert.Code},
			{Name: "severity", Value: alert.Severity},
			{Name: "component", Value: alert.Component},
		}
		writer.gauge("zephyr_alert_active", "Currently active derived alerts grouped by code, severity, and component.", 1, labels...)
		if alert.ObservedAt != nil {
			writer.gauge("zephyr_alert_observed_at_seconds", "Unix timestamp of the latest observation behind each active derived alert.", unixSeconds(*alert.ObservedAt), labels...)
		}
	}

	writer.gauge("zephyr_slo_objective_count", "Number of current SLO-oriented objectives exposed by /v1/slo.", float64(slo.ObjectiveCount))
	writer.gauge("zephyr_slo_status_count", "Number of current SLO-oriented objectives grouped by status.", float64(slo.MeetingCount), prometheusLabel{Name: "status", Value: sloStatusMeeting})
	writer.gauge("zephyr_slo_status_count", "Number of current SLO-oriented objectives grouped by status.", float64(slo.AtRiskCount), prometheusLabel{Name: "status", Value: sloStatusAtRisk})
	writer.gauge("zephyr_slo_status_count", "Number of current SLO-oriented objectives grouped by status.", float64(slo.BreachedCount), prometheusLabel{Name: "status", Value: sloStatusBreached})
	writer.gauge("zephyr_slo_status_count", "Number of current SLO-oriented objectives grouped by status.", float64(slo.NotApplicableCount), prometheusLabel{Name: "status", Value: sloStatusNotApplicable})
	for _, objective := range slo.Objectives {
		for _, statusLabel := range []string{sloStatusMeeting, sloStatusAtRisk, sloStatusBreached, sloStatusNotApplicable} {
			writer.gauge(
				"zephyr_slo_objective_status",
				"Current SLO-oriented objective status projected as mutually exclusive objective and status labels.",
				boolMetric(objective.Status == statusLabel),
				prometheusLabel{Name: "objective", Value: objective.Name},
				prometheusLabel{Name: "status", Value: statusLabel},
			)
		}
	}

	writer.gauge("zephyr_chain_height", "Latest committed local block height.", float64(status.Height))
	writer.gauge("zephyr_chain_mempool_transaction_count", "Current number of queued mempool transactions.", float64(status.MempoolSize))
	writer.gauge("zephyr_chain_total_block_count", "Total committed block count retained locally.", float64(throughput.TotalBlockCount))
	writer.gauge("zephyr_chain_total_committed_transaction_count", "Total committed transaction count retained locally.", float64(throughput.TotalTransactionCount))
	if status.LatestBlockAt != nil {
		writer.gauge("zephyr_chain_latest_block_timestamp_seconds", "Unix timestamp of the latest committed block.", unixSeconds(*status.LatestBlockAt))
	}
	if throughput.LatestBlockIntervalSeconds > 0 {
		writer.gauge("zephyr_chain_latest_block_interval_seconds", "Seconds between the latest two committed blocks.", throughput.LatestBlockIntervalSeconds)
	}
	for _, window := range throughput.Windows {
		labels := []prometheusLabel{{Name: "window", Value: window.Window}}
		writer.gauge("zephyr_chain_window_block_count", "Committed block count observed within each recent throughput window.", float64(window.BlockCount), labels...)
		writer.gauge("zephyr_chain_window_transaction_count", "Committed transaction count observed within each recent throughput window.", float64(window.TransactionCount), labels...)
		writer.gauge("zephyr_chain_window_blocks_per_second", "Committed block rate derived from each recent throughput window.", window.BlocksPerSecond, labels...)
		writer.gauge("zephyr_chain_window_transactions_per_second", "Committed transaction throughput derived from each recent throughput window.", window.TransactionsPerSecond, labels...)
		writer.gauge("zephyr_chain_window_average_transactions_per_block", "Average committed transactions per block observed within each recent throughput window.", window.AverageTransactionsPerBlock, labels...)
	}
	writer.gauge("zephyr_settlement_monitoring_applicable", "Whether settlement queue-drain monitoring is applicable for the current node configuration.", boolMetric(settlementThroughput.Applicable))
	writer.gauge("zephyr_settlement_queue_drain_lag_seconds", "Seconds since the latest committed block while queued transactions are awaiting settlement; zero when no backlog is pending.", settlementThroughput.QueueDrainLagSeconds)
	if settlementThroughput.LatestBlockAt != nil {
		writer.gauge("zephyr_settlement_latest_commit_age_seconds", "Seconds since the latest committed block regardless of backlog state.", settlementThroughput.LatestCommitAgeSeconds)
	}
	if settlementThroughput.ExpectedIntervalSeconds > 0 {
		writer.gauge("zephyr_settlement_expected_interval_seconds", "Configured automatic block-production interval used for settlement-throughput monitoring.", settlementThroughput.ExpectedIntervalSeconds)
		writer.gauge("zephyr_settlement_queue_drain_threshold_seconds", "Warn and fail thresholds derived from the automatic block-production interval for settlement queue-drain monitoring.", settlementThroughput.WarnAfterSeconds, prometheusLabel{Name: "threshold", Value: "warn"})
		writer.gauge("zephyr_settlement_queue_drain_threshold_seconds", "Warn and fail thresholds derived from the automatic block-production interval for settlement queue-drain monitoring.", settlementThroughput.FailAfterSeconds, prometheusLabel{Name: "threshold", Value: "fail"})
		writer.gauge("zephyr_settlement_queue_drain_utilization_ratio", "Normalized settlement queue-drain lag divided by the warn or fail threshold.", settlementThroughput.WarnUtilizationRatio, prometheusLabel{Name: "threshold", Value: "warn"})
		writer.gauge("zephyr_settlement_queue_drain_utilization_ratio", "Normalized settlement queue-drain lag divided by the warn or fail threshold.", settlementThroughput.FailUtilizationRatio, prometheusLabel{Name: "threshold", Value: "fail"})
	}

	writer.gauge("zephyr_consensus_current_height", "Current committed consensus height.", float64(consensusView.CurrentHeight))
	writer.gauge("zephyr_consensus_next_height", "Next block height expected by consensus.", float64(consensusView.NextHeight))
	writer.gauge("zephyr_consensus_current_round", "Current active consensus round.", float64(consensusView.CurrentRound))
	writer.gauge("zephyr_consensus_validator_count", "Number of validators in the active validator set.", float64(consensusView.ValidatorCount))
	writer.gauge("zephyr_consensus_total_voting_power", "Total voting power in the active validator set.", float64(consensusView.TotalVotingPower))
	writer.gauge("zephyr_consensus_quorum_voting_power", "Voting power threshold currently required for quorum.", float64(consensusView.QuorumVotingPower))

	writer.gauge("zephyr_recovery_pending_action_count", "Number of pending consensus recovery actions.", float64(recovery.PendingActionCount))
	writer.gauge("zephyr_recovery_pending_replay_count", "Number of pending replayable local consensus actions.", float64(recovery.PendingReplayCount))
	writer.gauge("zephyr_recovery_pending_import_count", "Number of pending import-recovery actions.", float64(recovery.PendingImportCount))
	writer.gauge("zephyr_recovery_needs_replay", "Whether local replay is still required.", boolMetric(recovery.NeedsReplay))
	writer.gauge("zephyr_recovery_needs_recovery", "Whether any recovery follow-up is still required.", boolMetric(recovery.NeedsRecovery))
	if recovery.LastSnapshotRestoreAt != nil {
		writer.gauge("zephyr_recovery_last_snapshot_restore_at_seconds", "Unix timestamp of the latest snapshot restore applied locally.", unixSeconds(*recovery.LastSnapshotRestoreAt))
		writer.gauge("zephyr_recovery_last_snapshot_restore_height", "Height of the latest snapshot restore applied locally.", float64(recovery.LastSnapshotRestoreHeight))
	}

	writer.gauge("zephyr_consensus_action_total_count", "Total number of retained consensus actions in the local recovery history.", float64(actionMetrics.TotalCount))
	writer.gauge("zephyr_consensus_action_pending_count", "Number of retained consensus actions still marked pending.", float64(actionMetrics.PendingCount))
	writer.gauge("zephyr_consensus_action_replay_attempt_count", "Total replay attempts recorded across retained consensus actions.", float64(actionMetrics.TotalReplayAttempts))
	if actionMetrics.LatestRecordedAt != nil {
		writer.gauge("zephyr_consensus_action_latest_recorded_at_seconds", "Unix timestamp of the latest retained consensus action.", unixSeconds(*actionMetrics.LatestRecordedAt))
	}
	if actionMetrics.LatestCompletedAt != nil {
		writer.gauge("zephyr_consensus_action_latest_completed_at_seconds", "Unix timestamp of the latest completed retained consensus action.", unixSeconds(*actionMetrics.LatestCompletedAt))
	}
	for _, bucket := range actionMetrics.ByType {
		writer.gauge(
			"zephyr_consensus_action_by_type_count",
			"Retained consensus actions grouped by action type.",
			float64(bucket.Count),
			prometheusLabel{Name: "type", Value: bucket.Label},
		)
	}
	for _, bucket := range actionMetrics.ByStatus {
		writer.gauge(
			"zephyr_consensus_action_by_status_count",
			"Retained consensus actions grouped by local action status.",
			float64(bucket.Count),
			prometheusLabel{Name: "status", Value: bucket.Label},
		)
	}

	writer.gauge("zephyr_consensus_diagnostic_total_count", "Total number of retained consensus diagnostics.", float64(diagnosticMetrics.TotalCount))
	if diagnosticMetrics.LatestObservedAt != nil {
		writer.gauge("zephyr_consensus_diagnostic_latest_observed_at_seconds", "Unix timestamp of the latest retained consensus diagnostic.", unixSeconds(*diagnosticMetrics.LatestObservedAt))
	}
	for _, bucket := range diagnosticMetrics.ByKind {
		writer.gauge(
			"zephyr_consensus_diagnostic_by_kind_count",
			"Retained consensus diagnostics grouped by kind.",
			float64(bucket.Count),
			prometheusLabel{Name: "kind", Value: bucket.Label},
		)
	}
	for _, bucket := range diagnosticMetrics.ByCode {
		writer.gauge(
			"zephyr_consensus_diagnostic_by_code_count",
			"Retained consensus diagnostics grouped by operator-facing code.",
			float64(bucket.Count),
			prometheusLabel{Name: "code", Value: bucket.Label},
		)
	}
	for _, bucket := range diagnosticMetrics.BySource {
		writer.gauge(
			"zephyr_consensus_diagnostic_by_source_count",
			"Retained consensus diagnostics grouped by source.",
			float64(bucket.Count),
			prometheusLabel{Name: "source", Value: bucket.Label},
		)
	}

	writer.gauge("zephyr_peer_runtime_configured_count", "Configured peer count in the current runtime.", float64(peerRuntime.ConfiguredPeerCount))
	writer.gauge("zephyr_peer_runtime_reachable_count", "Configured peers currently marked reachable.", float64(peerRuntime.ReachablePeerCount))
	writer.gauge("zephyr_peer_runtime_admitted_count", "Configured peers currently admitted by policy.", float64(peerRuntime.AdmittedPeerCount))
	writer.gauge("zephyr_peer_runtime_unreachable_count", "Configured peers currently marked unreachable.", float64(peerRuntime.UnreachablePeerCount))
	writer.gauge("zephyr_peer_runtime_unadmitted_count", "Configured peers currently not admitted by policy.", float64(peerRuntime.UnadmittedPeerCount))
	for _, bucket := range peerRuntime.BySyncState {
		writer.gauge(
			"zephyr_peer_runtime_by_sync_state_count",
			"Configured peers grouped by current runtime sync state.",
			float64(bucket.Count),
			prometheusLabel{Name: "state", Value: bucket.Label},
		)
	}

	writer.gauge("zephyr_peer_sync_incident_count", "Number of retained peer-sync incidents.", float64(peerSummary.IncidentCount))
	writer.gauge("zephyr_peer_sync_affected_peer_count", "Number of peers represented in retained peer-sync incidents.", float64(peerSummary.AffectedPeerCount))
	writer.gauge("zephyr_peer_sync_occurrence_count", "Total occurrences represented by retained peer-sync incidents.", float64(peerSummary.TotalOccurrences))
	if peerSummary.LatestObservedAt != nil {
		writer.gauge("zephyr_peer_sync_latest_observed_at_seconds", "Unix timestamp of the latest retained peer-sync incident.", unixSeconds(*peerSummary.LatestObservedAt))
	}
	for _, state := range peerSummary.States {
		writer.gauge(
			"zephyr_peer_sync_state_incident_count",
			"Retained peer-sync incidents grouped by dominant state.",
			float64(state.IncidentCount),
			prometheusLabel{Name: "state", Value: state.State},
		)
		writer.gauge(
			"zephyr_peer_sync_state_affected_peer_count",
			"Peers affected by retained peer-sync incidents grouped by dominant state.",
			float64(state.AffectedPeerCount),
			prometheusLabel{Name: "state", Value: state.State},
		)
		writer.gauge(
			"zephyr_peer_sync_state_occurrence_count",
			"Total occurrences represented by retained peer-sync incidents grouped by dominant state.",
			float64(state.TotalOccurrences),
			prometheusLabel{Name: "state", Value: state.State},
		)
	}
	for _, reason := range peerSummary.Reasons {
		writer.gauge(
			"zephyr_peer_sync_reason_incident_count",
			"Retained peer-sync incidents grouped by dominant reason.",
			float64(reason.IncidentCount),
			prometheusLabel{Name: "reason", Value: reason.Reason},
		)
		writer.gauge(
			"zephyr_peer_sync_reason_affected_peer_count",
			"Peers affected by retained peer-sync incidents grouped by dominant reason.",
			float64(reason.AffectedPeerCount),
			prometheusLabel{Name: "reason", Value: reason.Reason},
		)
		writer.gauge(
			"zephyr_peer_sync_reason_occurrence_count",
			"Total occurrences represented by retained peer-sync incidents grouped by dominant reason.",
			float64(reason.TotalOccurrences),
			prometheusLabel{Name: "reason", Value: reason.Reason},
		)
	}
	for _, errorCode := range peerSummary.ErrorCodes {
		writer.gauge(
			"zephyr_peer_sync_error_code_incident_count",
			"Retained peer-sync incidents grouped by dominant error code.",
			float64(errorCode.IncidentCount),
			prometheusLabel{Name: "code", Value: errorCode.ErrorCode},
		)
		writer.gauge(
			"zephyr_peer_sync_error_code_affected_peer_count",
			"Peers affected by retained peer-sync incidents grouped by dominant error code.",
			float64(errorCode.AffectedPeerCount),
			prometheusLabel{Name: "code", Value: errorCode.ErrorCode},
		)
		writer.gauge(
			"zephyr_peer_sync_error_code_occurrence_count",
			"Total occurrences represented by retained peer-sync incidents grouped by dominant error code.",
			float64(errorCode.TotalOccurrences),
			prometheusLabel{Name: "code", Value: errorCode.ErrorCode},
		)
	}
	for _, peer := range peerSummary.Peers {
		labels := []prometheusLabel{
			{Name: "peer_url", Value: peer.PeerURL},
			{Name: "latest_state", Value: normalizePeerSyncPrometheusLabel(peer.LatestState)},
			{Name: "latest_reason", Value: normalizePeerSyncPrometheusLabel(peer.LatestReason)},
			{Name: "latest_error_code", Value: normalizePeerSyncPrometheusLabel(peer.LatestErrorCode)},
		}
		writer.gauge(
			"zephyr_peer_sync_peer_incident_count",
			"Retained peer-sync incidents grouped by peer with the latest dominant state, reason, and error code attached as labels.",
			float64(peer.IncidentCount),
			labels...,
		)
		writer.gauge(
			"zephyr_peer_sync_peer_occurrence_count",
			"Total retained peer-sync incident occurrences grouped by peer with the latest dominant state, reason, and error code attached as labels.",
			float64(peer.TotalOccurrences),
			labels...,
		)
		if peer.LatestObservedAt != nil {
			writer.gauge(
				"zephyr_peer_sync_peer_latest_observed_at_seconds",
				"Unix timestamp of the latest retained peer-sync incident grouped by peer with the latest dominant state, reason, and error code attached as labels.",
				unixSeconds(*peer.LatestObservedAt),
				labels...,
			)
		}
	}

	return writer.String()
}

func normalizePeerSyncPrometheusLabel(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "unknown"
	}
	return value
}

func newPrometheusMetricWriter() *prometheusMetricWriter {
	return &prometheusMetricWriter{declared: make(map[string]struct{})}
}

func (w *prometheusMetricWriter) gauge(name string, help string, value float64, labels ...prometheusLabel) {
	if _, ok := w.declared[name]; !ok {
		w.builder.WriteString("# HELP ")
		w.builder.WriteString(name)
		w.builder.WriteByte(' ')
		w.builder.WriteString(help)
		w.builder.WriteByte('\n')
		w.builder.WriteString("# TYPE ")
		w.builder.WriteString(name)
		w.builder.WriteString(" gauge\n")
		w.declared[name] = struct{}{}
	}

	w.builder.WriteString(name)
	if len(labels) > 0 {
		w.builder.WriteByte('{')
		for i, label := range labels {
			if i > 0 {
				w.builder.WriteByte(',')
			}
			w.builder.WriteString(label.Name)
			w.builder.WriteString("=\"")
			w.builder.WriteString(escapePrometheusLabelValue(label.Value))
			w.builder.WriteByte('"')
		}
		w.builder.WriteByte('}')
	}
	w.builder.WriteByte(' ')
	w.builder.WriteString(strconv.FormatFloat(value, 'f', -1, 64))
	w.builder.WriteByte('\n')
}

func (w *prometheusMetricWriter) String() string {
	return w.builder.String()
}

func boolMetric(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

func unixSeconds(value time.Time) float64 {
	value = value.UTC()
	return float64(value.UnixNano()) / float64(time.Second)
}

func escapePrometheusLabelValue(value string) string {
	value = strings.ReplaceAll(value, "\\", "\\\\")
	value = strings.ReplaceAll(value, "\"", "\\\"")
	value = strings.ReplaceAll(value, "`n", "\\n")
	return value
}

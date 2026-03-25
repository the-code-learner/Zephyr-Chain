package api

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

const (
	healthCheckPass            = "pass"
	healthCheckWarn            = "warn"
	healthCheckFail            = "fail"
	healthStatusOK             = "ok"
	healthStatusWarn           = "warn"
	healthStatusFail           = "fail"
	recentDiagnosticWarnWindow = 1 * time.Hour
)

type HealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Summary string `json:"summary"`
	Detail  string `json:"detail,omitempty"`
}

type HealthResponse struct {
	GeneratedAt                time.Time     `json:"generatedAt"`
	NodeID                     string        `json:"nodeId"`
	ValidatorAddress           string        `json:"validatorAddress,omitempty"`
	PeerCount                  int           `json:"peerCount"`
	Live                       bool          `json:"live"`
	Ready                      bool          `json:"ready"`
	Status                     string        `json:"status"`
	ConsensusAutomationEnabled bool          `json:"consensusAutomationEnabled"`
	PeerSyncEnabled            bool          `json:"peerSyncEnabled"`
	StructuredLogsEnabled      bool          `json:"structuredLogsEnabled"`
	Checks                     []HealthCheck `json:"checks"`
	Warnings                   []string      `json:"warnings"`
}

func (s *Server) handleNodeHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	response := s.buildHealthResponse(time.Now().UTC())
	status := http.StatusOK
	if !response.Ready {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, response)
}

func (s *Server) buildHealthResponse(now time.Time) HealthResponse {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	consensusView := s.ledger.Consensus()
	roundEvidence := s.buildRoundEvidence(now)
	blockReadiness := s.buildBlockReadiness(now)
	recovery := s.ledger.ConsensusRecovery()
	diagnosticMetrics := s.ledger.ConsensusDiagnosticMetrics()
	peerRuntime := buildPeerRuntimeMetrics(s.peerSnapshot())
	peerSummary := s.ledger.PeerSyncSummary()

	response := HealthResponse{
		GeneratedAt:                now,
		NodeID:                     s.nodeID,
		ValidatorAddress:           s.config.ValidatorAddress,
		PeerCount:                  len(s.config.PeerURLs),
		Live:                       true,
		Ready:                      true,
		Status:                     healthStatusOK,
		ConsensusAutomationEnabled: s.config.EnableConsensusAutomation,
		PeerSyncEnabled:            s.config.EnablePeerSync,
		StructuredLogsEnabled:      s.config.EnableStructuredLogs,
		Checks:                     make([]HealthCheck, 0, 7),
		Warnings:                   make([]string, 0),
	}

	appendHealthCheck(&response, HealthCheck{
		Name:    "api",
		Status:  healthCheckPass,
		Summary: "http api responding",
	})

	requiresValidatorSet := s.config.EnableConsensusAutomation || s.config.RequireConsensusCertificates || s.config.EnforceProposerSchedule || s.config.ValidatorAddress != ""
	switch {
	case consensusView.ValidatorCount == 0 && requiresValidatorSet:
		appendHealthCheck(&response, HealthCheck{
			Name:    "validator_set",
			Status:  healthCheckWarn,
			Summary: "validator set required by current mode is missing",
		})
	case consensusView.ValidatorCount == 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "validator_set",
			Status:  healthCheckPass,
			Summary: "validator set optional in current mode",
		})
	default:
		appendHealthCheck(&response, HealthCheck{
			Name:    "validator_set",
			Status:  healthCheckPass,
			Summary: fmt.Sprintf("%d validators active", consensusView.ValidatorCount),
		})
	}

	recoveryDetailParts := []string{}
	if recovery.PendingReplayCount > 0 {
		recoveryDetailParts = append(recoveryDetailParts, fmt.Sprintf("pendingReplay=%d", recovery.PendingReplayCount))
	}
	if recovery.PendingImportCount > 0 {
		recoveryDetailParts = append(recoveryDetailParts, fmt.Sprintf("pendingImport=%d", recovery.PendingImportCount))
	}
	if len(recovery.PendingImportHeights) > 0 {
		recoveryDetailParts = append(recoveryDetailParts, fmt.Sprintf("importHeights=%v", recovery.PendingImportHeights))
	}
	recoveryDetail := strings.Join(recoveryDetailParts, ", ")
	switch {
	case recovery.NeedsRecovery || recovery.PendingReplayCount > 0 || recovery.PendingImportCount > 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "recovery",
			Status:  healthCheckFail,
			Summary: "consensus recovery backlog pending",
			Detail:  recoveryDetail,
		})
	case recovery.LastSnapshotRestoreAt != nil:
		appendHealthCheck(&response, HealthCheck{
			Name:    "recovery",
			Status:  healthCheckPass,
			Summary: "recovery backlog clear",
			Detail:  fmt.Sprintf("lastSnapshotRestoreAt=%s", recovery.LastSnapshotRestoreAt.Format(time.RFC3339)),
		})
	default:
		appendHealthCheck(&response, HealthCheck{
			Name:    "recovery",
			Status:  healthCheckPass,
			Summary: "recovery backlog clear",
		})
	}

	consensusWarnings := make([]string, 0, len(roundEvidence.Warnings)+len(blockReadiness.Warnings))
	for _, warning := range roundEvidence.Warnings {
		consensusWarnings = append(consensusWarnings, "round:"+warning)
	}
	for _, warning := range blockReadiness.Warnings {
		consensusWarnings = append(consensusWarnings, "block:"+warning)
	}
	switch {
	case len(consensusWarnings) > 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "consensus",
			Status:  healthCheckWarn,
			Summary: "consensus has active warnings",
			Detail:  strings.Join(consensusWarnings, ", "),
		})
	case roundEvidence.CertificatePresent && blockReadiness.ReadyToCommitStoredProposal:
		appendHealthCheck(&response, HealthCheck{
			Name:    "consensus",
			Status:  healthCheckPass,
			Summary: "certified proposal available",
		})
	case roundEvidence.ProposalPresent:
		appendHealthCheck(&response, HealthCheck{
			Name:    "consensus",
			Status:  healthCheckPass,
			Summary: "proposal collected for active round",
		})
	default:
		appendHealthCheck(&response, HealthCheck{
			Name:    "consensus",
			Status:  healthCheckPass,
			Summary: "no active consensus warnings",
		})
	}

	throughput := s.assessSettlementThroughput(now)
	appendHealthCheck(&response, HealthCheck{
		Name:    settlementThroughputCheckName,
		Status:  throughput.HealthStatus,
		Summary: throughput.Summary,
		Detail:  throughput.Detail,
	})

	unknownPeerCount := metricCountForLabel(peerRuntime.BySyncState, "unknown")
	peerDetail := fmt.Sprintf("configured=%d reachable=%d admitted=%d unreachable=%d unadmitted=%d incidents=%d", peerRuntime.ConfiguredPeerCount, peerRuntime.ReachablePeerCount, peerRuntime.AdmittedPeerCount, peerRuntime.UnreachablePeerCount, peerRuntime.UnadmittedPeerCount, peerSummary.IncidentCount)
	switch {
	case !s.config.EnablePeerSync:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckPass,
			Summary: "peer sync disabled by configuration",
		})
	case len(s.config.PeerURLs) == 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckPass,
			Summary: "no peers configured",
		})
	case unknownPeerCount == peerRuntime.ConfiguredPeerCount && peerSummary.IncidentCount == 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckWarn,
			Summary: "peer sync not observed yet",
			Detail:  peerDetail,
		})
	case peerRuntime.AdmittedPeerCount == 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckFail,
			Summary: "no admitted peers available",
			Detail:  peerDetail,
		})
	case peerRuntime.ReachablePeerCount == 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckFail,
			Summary: "all configured peers unreachable",
			Detail:  peerDetail,
		})
	case peerRuntime.UnreachablePeerCount > 0 || peerRuntime.UnadmittedPeerCount > 0 || peerSummary.IncidentCount > 0:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckWarn,
			Summary: "peer sync degraded or recovering",
			Detail:  peerDetail,
		})
	default:
		appendHealthCheck(&response, HealthCheck{
			Name:    "peer_sync",
			Status:  healthCheckPass,
			Summary: "peer sync healthy",
			Detail:  peerDetail,
		})
	}

	diagnosticDetail := summarizeMetricBuckets(diagnosticMetrics.ByCode, 3)
	switch {
	case diagnosticMetrics.TotalCount == 0 || diagnosticMetrics.LatestObservedAt == nil:
		appendHealthCheck(&response, HealthCheck{
			Name:    "diagnostics",
			Status:  healthCheckPass,
			Summary: "no recent consensus diagnostics",
		})
	case now.Sub(*diagnosticMetrics.LatestObservedAt) > recentDiagnosticWarnWindow:
		appendHealthCheck(&response, HealthCheck{
			Name:    "diagnostics",
			Status:  healthCheckPass,
			Summary: "no recent consensus diagnostics",
			Detail:  fmt.Sprintf("retainedDiagnostics=%d", diagnosticMetrics.TotalCount),
		})
	default:
		appendHealthCheck(&response, HealthCheck{
			Name:    "diagnostics",
			Status:  healthCheckWarn,
			Summary: "recent rejected consensus actions recorded",
			Detail:  diagnosticDetail,
		})
	}

	return response
}

func appendHealthCheck(response *HealthResponse, check HealthCheck) {
	response.Checks = append(response.Checks, check)
	switch check.Status {
	case healthCheckFail:
		response.Ready = false
		response.Status = healthStatusFail
		response.Warnings = append(response.Warnings, fmt.Sprintf("%s: %s", check.Name, check.Summary))
	case healthCheckWarn:
		if response.Status != healthStatusFail {
			response.Status = healthStatusWarn
		}
		response.Warnings = append(response.Warnings, fmt.Sprintf("%s: %s", check.Name, check.Summary))
	}
}

func metricCountForLabel(buckets []ledger.MetricCount, label string) int {
	for _, bucket := range buckets {
		if bucket.Label == label {
			return bucket.Count
		}
	}
	return 0
}

func summarizeMetricBuckets(buckets []ledger.MetricCount, limit int) string {
	if len(buckets) == 0 || limit <= 0 {
		return ""
	}
	if len(buckets) < limit {
		limit = len(buckets)
	}
	parts := make([]string, 0, limit)
	for _, bucket := range buckets[:limit] {
		parts = append(parts, fmt.Sprintf("%s=%d", bucket.Label, bucket.Count))
	}
	return strings.Join(parts, ", ")
}

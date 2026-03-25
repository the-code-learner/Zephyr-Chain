package api

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

const (
	settlementThroughputCheckName      = "settlement_throughput"
	settlementThroughputAlertReduced   = "settlement_throughput_reduced"
	settlementThroughputAlertStalled   = "settlement_throughput_stalled"
	settlementThroughputWarnMultiplier = 4
	settlementThroughputFailMultiplier = 8
)

type settlementThroughputAssessment struct {
	Applicable    bool
	HealthStatus  string
	Summary       string
	Detail        string
	AlertCode     string
	AlertSeverity string
	ObservedAt    *time.Time
}

func (s *Server) assessSettlementThroughput(now time.Time) settlementThroughputAssessment {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	status := s.ledger.Status()
	throughput := s.ledger.ChainThroughputMetrics(now)
	assessment := settlementThroughputAssessment{
		Applicable:   true,
		HealthStatus: healthCheckPass,
		Summary:      "no queued transactions awaiting settlement",
		Detail:       buildSettlementThroughputDetail(now, status, throughput, s.config.BlockInterval),
	}

	switch {
	case !s.config.EnableBlockProduction:
		assessment.Applicable = false
		assessment.Summary = "settlement throughput monitoring not applicable when block production is disabled"
		assessment.Detail = "block production is disabled by configuration"
		return assessment
	case s.config.BlockInterval <= 0:
		assessment.Applicable = false
		assessment.Summary = "settlement throughput monitoring not applicable without an automatic block interval"
		assessment.Detail = "automatic block interval is disabled"
		return assessment
	case throughput.LatestBlockAt == nil:
		assessment.Applicable = false
		assessment.Summary = "settlement throughput baseline not available yet"
		return assessment
	case status.MempoolSize == 0:
		return assessment
	}

	latestBlockAge := now.Sub(*throughput.LatestBlockAt)
	if latestBlockAge < 0 {
		latestBlockAge = 0
	}
	warnAfter := time.Duration(settlementThroughputWarnMultiplier) * s.config.BlockInterval
	failAfter := time.Duration(settlementThroughputFailMultiplier) * s.config.BlockInterval
	observedAt := now
	assessment.ObservedAt = &observedAt

	switch {
	case latestBlockAge >= failAfter:
		assessment.HealthStatus = healthCheckFail
		assessment.Summary = "queued transactions are not settling within the expected block window"
		assessment.AlertCode = settlementThroughputAlertStalled
		assessment.AlertSeverity = alertSeverityCritical
	case latestBlockAge >= warnAfter:
		assessment.HealthStatus = healthCheckWarn
		assessment.Summary = "queued transactions are settling slower than expected"
		assessment.AlertCode = settlementThroughputAlertReduced
		assessment.AlertSeverity = alertSeverityWarning
	default:
		assessment.Summary = "recent block commits are keeping up with queued transactions"
	}

	return assessment
}

func (s *Server) settlementThroughputDisabledReason() string {
	switch {
	case !s.config.EnableBlockProduction:
		return "block production is disabled by configuration"
	case s.config.BlockInterval <= 0:
		return "automatic block interval is disabled"
	default:
		return ""
	}
}

func buildSettlementThroughputDetail(now time.Time, status ledger.StatusView, throughput ledger.ChainThroughputMetricsView, blockInterval time.Duration) string {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	parts := []string{
		"mempool=" + strconv.Itoa(status.MempoolSize),
		"committedBlocks=" + strconv.Itoa(throughput.TotalBlockCount),
		"committedTransactions=" + strconv.Itoa(throughput.TotalTransactionCount),
	}
	if throughput.LatestBlockAt != nil {
		latestBlockAge := now.Sub(*throughput.LatestBlockAt)
		if latestBlockAge < 0 {
			latestBlockAge = 0
		}
		parts = append(parts, "lastCommitAge="+durationSecondsString(latestBlockAge))
	}
	if blockInterval > 0 {
		parts = append(parts,
			"expectedInterval="+durationSecondsString(blockInterval),
			"warnAfter="+durationSecondsString(time.Duration(settlementThroughputWarnMultiplier)*blockInterval),
			"failAfter="+durationSecondsString(time.Duration(settlementThroughputFailMultiplier)*blockInterval),
		)
	}
	if window, ok := throughputWindowByName(throughput.Windows, "1m"); ok {
		parts = append(parts, fmt.Sprintf("tps1m=%.4g", window.TransactionsPerSecond))
	}
	if window, ok := throughputWindowByName(throughput.Windows, "5m"); ok {
		parts = append(parts, fmt.Sprintf("tps5m=%.4g", window.TransactionsPerSecond))
	}
	return strings.Join(parts, ", ")
}

func throughputWindowByName(windows []ledger.ChainThroughputWindowView, name string) (ledger.ChainThroughputWindowView, bool) {
	name = strings.TrimSpace(name)
	for _, window := range windows {
		if window.Window == name {
			return window, true
		}
	}
	return ledger.ChainThroughputWindowView{}, false
}

func durationSecondsString(value time.Duration) string {
	if value < 0 {
		value = 0
	}
	return strconv.FormatInt(int64(value/time.Second), 10) + "s"
}

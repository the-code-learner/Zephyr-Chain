package api

import (
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
)

type SettlementDrainEstimateView struct {
	Window                string  `json:"window"`
	WindowSeconds         int64   `json:"windowSeconds"`
	TransactionsPerSecond float64 `json:"transactionsPerSecond"`
	EstimatedDrainSeconds float64 `json:"estimatedDrainSeconds"`
	Available             bool    `json:"available"`
}

type SettlementThroughputMetricsView struct {
	Applicable              bool                          `json:"applicable"`
	HealthStatus            string                        `json:"healthStatus"`
	Summary                 string                        `json:"summary"`
	Detail                  string                        `json:"detail,omitempty"`
	AlertCode               string                        `json:"alertCode,omitempty"`
	AlertSeverity           string                        `json:"alertSeverity,omitempty"`
	ObservedAt              *time.Time                    `json:"observedAt,omitempty"`
	MempoolTransactionCount int                           `json:"mempoolTransactionCount"`
	LatestBlockAt           *time.Time                    `json:"latestBlockAt,omitempty"`
	LatestCommitAgeSeconds  float64                       `json:"latestCommitAgeSeconds"`
	QueueDrainLagSeconds    float64                       `json:"queueDrainLagSeconds"`
	ExpectedIntervalSeconds float64                       `json:"expectedIntervalSeconds,omitempty"`
	WarnAfterSeconds        float64                       `json:"warnAfterSeconds,omitempty"`
	FailAfterSeconds        float64                       `json:"failAfterSeconds,omitempty"`
	WarnUtilizationRatio    float64                       `json:"warnUtilizationRatio"`
	FailUtilizationRatio    float64                       `json:"failUtilizationRatio"`
	DrainEstimates          []SettlementDrainEstimateView `json:"drainEstimates,omitempty"`
}

func (s *Server) buildSettlementThroughputMetrics(now time.Time) SettlementThroughputMetricsView {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	status := s.ledger.Status()
	throughput := s.ledger.ChainThroughputMetrics(now)
	assessment := s.assessSettlementThroughput(now)

	view := SettlementThroughputMetricsView{
		Applicable:              assessment.Applicable,
		HealthStatus:            assessment.HealthStatus,
		Summary:                 assessment.Summary,
		Detail:                  assessment.Detail,
		AlertCode:               assessment.AlertCode,
		AlertSeverity:           assessment.AlertSeverity,
		MempoolTransactionCount: status.MempoolSize,
		DrainEstimates:          buildSettlementDrainEstimates(assessment.Applicable, status.MempoolSize, throughput.Windows),
	}
	if assessment.ObservedAt != nil {
		observedAt := assessment.ObservedAt.UTC()
		view.ObservedAt = &observedAt
	}
	if s.config.BlockInterval > 0 {
		view.ExpectedIntervalSeconds = s.config.BlockInterval.Seconds()
		view.WarnAfterSeconds = (time.Duration(settlementThroughputWarnMultiplier) * s.config.BlockInterval).Seconds()
		view.FailAfterSeconds = (time.Duration(settlementThroughputFailMultiplier) * s.config.BlockInterval).Seconds()
	}
	if throughput.LatestBlockAt != nil {
		latestBlockAt := throughput.LatestBlockAt.UTC()
		view.LatestBlockAt = &latestBlockAt
		lag := now.Sub(*throughput.LatestBlockAt)
		if lag < 0 {
			lag = 0
		}
		view.LatestCommitAgeSeconds = lag.Seconds()
		if status.MempoolSize > 0 {
			view.QueueDrainLagSeconds = lag.Seconds()
		}
	}
	if view.WarnAfterSeconds > 0 {
		view.WarnUtilizationRatio = view.QueueDrainLagSeconds / view.WarnAfterSeconds
	}
	if view.FailAfterSeconds > 0 {
		view.FailUtilizationRatio = view.QueueDrainLagSeconds / view.FailAfterSeconds
	}

	return view
}

func buildSettlementDrainEstimates(applicable bool, mempoolTransactionCount int, windows []ledger.ChainThroughputWindowView) []SettlementDrainEstimateView {
	if len(windows) == 0 {
		return nil
	}

	estimates := make([]SettlementDrainEstimateView, 0, len(windows))
	for _, window := range windows {
		estimate := SettlementDrainEstimateView{
			Window:                window.Window,
			WindowSeconds:         window.WindowSeconds,
			TransactionsPerSecond: window.TransactionsPerSecond,
		}
		switch {
		case !applicable:
		case mempoolTransactionCount == 0:
			estimate.Available = true
		case window.TransactionsPerSecond > 0:
			estimate.Available = true
			estimate.EstimatedDrainSeconds = float64(mempoolTransactionCount) / window.TransactionsPerSecond
		}
		estimates = append(estimates, estimate)
	}
	return estimates
}

package api

import "time"

type SettlementThroughputMetricsView struct {
	Applicable              bool       `json:"applicable"`
	HealthStatus            string     `json:"healthStatus"`
	Summary                 string     `json:"summary"`
	Detail                  string     `json:"detail,omitempty"`
	MempoolTransactionCount int        `json:"mempoolTransactionCount"`
	LatestBlockAt           *time.Time `json:"latestBlockAt,omitempty"`
	LatestCommitAgeSeconds  float64    `json:"latestCommitAgeSeconds"`
	QueueDrainLagSeconds    float64    `json:"queueDrainLagSeconds"`
	ExpectedIntervalSeconds float64    `json:"expectedIntervalSeconds,omitempty"`
	WarnAfterSeconds        float64    `json:"warnAfterSeconds,omitempty"`
	FailAfterSeconds        float64    `json:"failAfterSeconds,omitempty"`
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
		MempoolTransactionCount: status.MempoolSize,
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

	return view
}

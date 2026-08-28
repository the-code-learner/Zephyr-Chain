package lab

import (
	"encoding/json"
	"errors"
	"runtime"
	"sort"
	"time"
)

var ErrInvalidFinalityBenchmark = errors.New("invalid finalized-through-consensus benchmark")

type FinalityBenchmarkConfig struct {
	ShardCount        uint32 `json:"shard_count"`
	ValidatorCount    uint32 `json:"validator_count"`
	TransactionsBlock uint64 `json:"transactions_per_block"`
	BlockCadenceMS    uint64 `json:"block_cadence_ms"`
	CrossShardBPS     uint32 `json:"cross_shard_bps"`
	Workload          string `json:"workload"`
}

type FinalityBenchmarkEnvironment struct {
	CommitSHA    string `json:"commit_sha"`
	CPUCount     int    `json:"cpu_count"`
	GOMAXPROCS   int    `json:"gomaxprocs"`
	RAMBytes     uint64 `json:"ram_bytes,omitempty"`
	NetworkBytes uint64 `json:"network_bytes,omitempty"`
	DABytes      uint64 `json:"da_bytes,omitempty"`
}

type FinalitySample struct {
	SubmittedAt time.Time
	FinalizedAt time.Time
}

type FinalityBenchmarkReport struct {
	Version              uint16                       `json:"version"`
	Valid                bool                         `json:"valid"`
	InvalidReason        string                       `json:"invalid_reason,omitempty"`
	Config               FinalityBenchmarkConfig      `json:"config"`
	Environment          FinalityBenchmarkEnvironment `json:"environment"`
	WarmupFinalized      uint64                       `json:"warmup_finalized"`
	MeasuredFinalized    uint64                       `json:"measured_finalized"`
	MeasuredDurationNS   int64                        `json:"measured_duration_ns"`
	FinalizedTPS         float64                      `json:"finalized_tps"`
	FinalityP50NS        int64                        `json:"finality_p50_ns"`
	FinalityP95NS        int64                        `json:"finality_p95_ns"`
	FinalityP99NS        int64                        `json:"finality_p99_ns"`
	RejectedTransactions uint64                       `json:"rejected_transactions"`
	Errors               uint64                       `json:"errors"`
	SafetyFailures       uint64                       `json:"safety_failures"`
	LivenessFailures     uint64                       `json:"liveness_failures"`
}

type FinalityBenchmarkInput struct {
	Config               FinalityBenchmarkConfig
	Environment          FinalityBenchmarkEnvironment
	WarmupFinalized      uint64
	MeasuredStart        time.Time
	MeasuredEnd          time.Time
	Samples              []FinalitySample
	RejectedTransactions uint64
	Errors               uint64
	SafetyFailures       uint64
	LivenessFailures     uint64
}

func BuildFinalityBenchmarkReport(in FinalityBenchmarkInput) (FinalityBenchmarkReport, error) {
	if err := validateBenchmarkConfig(in.Config); err != nil {
		return FinalityBenchmarkReport{}, err
	}
	if in.MeasuredStart.IsZero() || !in.MeasuredEnd.After(in.MeasuredStart) || len(in.Samples) == 0 {
		return FinalityBenchmarkReport{}, ErrInvalidFinalityBenchmark
	}
	latencies := make([]time.Duration, len(in.Samples))
	for i, sample := range in.Samples {
		if sample.SubmittedAt.IsZero() || sample.FinalizedAt.Before(sample.SubmittedAt) || sample.FinalizedAt.After(in.MeasuredEnd) {
			return FinalityBenchmarkReport{}, ErrInvalidFinalityBenchmark
		}
		latencies[i] = sample.FinalizedAt.Sub(sample.SubmittedAt)
	}
	duration := in.MeasuredEnd.Sub(in.MeasuredStart)
	seconds := duration.Seconds()
	if seconds <= 0 {
		return FinalityBenchmarkReport{}, ErrInvalidFinalityBenchmark
	}
	env := in.Environment
	if env.CPUCount == 0 {
		env.CPUCount = runtime.NumCPU()
	}
	if env.GOMAXPROCS == 0 {
		env.GOMAXPROCS = runtime.GOMAXPROCS(0)
	}
	report := FinalityBenchmarkReport{
		Version:              1,
		Valid:                in.SafetyFailures == 0 && in.LivenessFailures == 0,
		Config:               in.Config,
		Environment:          env,
		WarmupFinalized:      in.WarmupFinalized,
		MeasuredFinalized:    uint64(len(in.Samples)),
		MeasuredDurationNS:   int64(duration),
		FinalizedTPS:         float64(len(in.Samples)) / seconds,
		FinalityP50NS:        int64(nearestRankDuration(latencies, 50)),
		FinalityP95NS:        int64(nearestRankDuration(latencies, 95)),
		FinalityP99NS:        int64(nearestRankDuration(latencies, 99)),
		RejectedTransactions: in.RejectedTransactions,
		Errors:               in.Errors,
		SafetyFailures:       in.SafetyFailures,
		LivenessFailures:     in.LivenessFailures,
	}
	if !report.Valid {
		report.InvalidReason = "safety or liveness failure observed"
	}
	return report, nil
}

func (r FinalityBenchmarkReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func validateBenchmarkConfig(config FinalityBenchmarkConfig) error {
	if config.ShardCount == 0 || config.ValidatorCount == 0 || config.TransactionsBlock == 0 || config.BlockCadenceMS == 0 || config.CrossShardBPS > 10_000 || config.Workload == "" {
		return ErrInvalidFinalityBenchmark
	}
	return nil
}

func nearestRankDuration(values []time.Duration, percentile int) time.Duration {
	if len(values) == 0 || percentile <= 0 || percentile > 100 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	rank := (percentile*len(ordered) + 99) / 100
	if rank < 1 {
		rank = 1
	}
	return ordered[rank-1]
}

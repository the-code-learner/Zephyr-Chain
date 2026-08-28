package lab

import (
	"encoding/json"
	"math"
	"testing"
	"time"
)

func TestBuildFinalityBenchmarkReportCountsOnlyFinalizedWindow(t *testing.T) {
	start := time.Unix(1_000, 0)
	samples := make([]FinalitySample, 100)
	for i := range samples {
		submitted := start.Add(time.Duration(i) * time.Millisecond)
		latency := time.Duration(i+1) * time.Millisecond
		samples[i] = FinalitySample{SubmittedAt: submitted, FinalizedAt: submitted.Add(latency)}
	}
	report, err := BuildFinalityBenchmarkReport(FinalityBenchmarkInput{
		Config:               FinalityBenchmarkConfig{ShardCount: 4, ValidatorCount: 7, TransactionsBlock: 1_000, BlockCadenceMS: 100, CrossShardBPS: 2_500, Workload: "native-transfer"},
		Environment:          FinalityBenchmarkEnvironment{CommitSHA: "deadbeef", CPUCount: 8, GOMAXPROCS: 8, RAMBytes: 16 << 30},
		WarmupFinalized:      20,
		MeasuredStart:        start,
		MeasuredEnd:          start.Add(2 * time.Second),
		Samples:              samples,
		RejectedTransactions: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !report.Valid || report.MeasuredFinalized != 100 || report.WarmupFinalized != 20 {
		t.Fatalf("unexpected report: %#v", report)
	}
	if math.Abs(report.FinalizedTPS-50) > 0.000001 {
		t.Fatalf("TPS = %f, want 50", report.FinalizedTPS)
	}
	if got, want := time.Duration(report.FinalityP50NS), 50*time.Millisecond; got != want {
		t.Fatalf("p50 = %v, want %v", got, want)
	}
	if got, want := time.Duration(report.FinalityP95NS), 95*time.Millisecond; got != want {
		t.Fatalf("p95 = %v, want %v", got, want)
	}
	if got, want := time.Duration(report.FinalityP99NS), 99*time.Millisecond; got != want {
		t.Fatalf("p99 = %v, want %v", got, want)
	}
	if report.RejectedTransactions != 3 {
		t.Fatalf("rejected = %d", report.RejectedTransactions)
	}
}

func TestFinalityBenchmarkSafetyOrLivenessFailureInvalidatesReport(t *testing.T) {
	start := time.Unix(2_000, 0)
	report, err := BuildFinalityBenchmarkReport(FinalityBenchmarkInput{
		Config:         FinalityBenchmarkConfig{ShardCount: 1, ValidatorCount: 4, TransactionsBlock: 10, BlockCadenceMS: 500, Workload: "native-transfer"},
		MeasuredStart:  start,
		MeasuredEnd:    start.Add(time.Second),
		Samples:        []FinalitySample{{SubmittedAt: start, FinalizedAt: start.Add(100 * time.Millisecond)}},
		SafetyFailures: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.Valid || report.InvalidReason == "" {
		t.Fatalf("unsafe report was valid: %#v", report)
	}
}

func TestFinalityBenchmarkRejectsNonFinalizedOrInvalidSamples(t *testing.T) {
	start := time.Unix(3_000, 0)
	base := FinalityBenchmarkInput{
		Config:        FinalityBenchmarkConfig{ShardCount: 2, ValidatorCount: 4, TransactionsBlock: 10, BlockCadenceMS: 500, Workload: "native-transfer"},
		MeasuredStart: start,
		MeasuredEnd:   start.Add(time.Second),
	}
	base.Samples = []FinalitySample{{SubmittedAt: start.Add(time.Second), FinalizedAt: start.Add(2 * time.Second)}}
	if _, err := BuildFinalityBenchmarkReport(base); err == nil {
		t.Fatal("sample finalized outside measurement window accepted")
	}
	base.Samples = []FinalitySample{{SubmittedAt: start.Add(200 * time.Millisecond), FinalizedAt: start.Add(100 * time.Millisecond)}}
	if _, err := BuildFinalityBenchmarkReport(base); err == nil {
		t.Fatal("negative finality latency accepted")
	}
}

func TestFinalityBenchmarkJSONContainsMachineReadableConfiguration(t *testing.T) {
	start := time.Unix(4_000, 0)
	report, err := BuildFinalityBenchmarkReport(FinalityBenchmarkInput{
		Config:        FinalityBenchmarkConfig{ShardCount: 8, ValidatorCount: 7, TransactionsBlock: 2_000, BlockCadenceMS: 100, CrossShardBPS: 5_000, Workload: "native-transfer"},
		Environment:   FinalityBenchmarkEnvironment{CommitSHA: "abc123", CPUCount: 16, GOMAXPROCS: 16, NetworkBytes: 1234, DABytes: 5678},
		MeasuredStart: start,
		MeasuredEnd:   start.Add(time.Second),
		Samples:       []FinalitySample{{SubmittedAt: start, FinalizedAt: start.Add(50 * time.Millisecond)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["valid"] != true || decoded["finalized_tps"] == nil || decoded["config"] == nil || decoded["environment"] == nil {
		t.Fatalf("incomplete JSON report: %s", raw)
	}
}

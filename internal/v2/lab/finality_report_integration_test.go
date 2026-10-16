package lab

import (
	"os"
	"testing"
	"time"
)

func TestV2LabFinalityReportFromConsensusCommit(t *testing.T) {
	result, err := RunFinalityHarness(FinalityHarnessConfig{
		ShardCount:           1,
		ValidatorCount:       7,
		TransactionsPerBlock: 4,
		WarmupBlocks:         1,
		MeasuredBlocks:       2,
		CrossShardBPS:        0,
		BlockCadence:         time.Millisecond,
		Workers:              4,
		CommitSHA:            os.Getenv("GITHUB_SHA"),
		Workload:             "native-transfer-v2-lab-single-process",
		Topology:             "single-process-in-memory",
	})
	if err != nil {
		t.Fatal(err)
	}
	report := result.Report
	if !report.Valid || report.WarmupFinalized != 4 || report.MeasuredFinalized != 8 ||
		report.FinalizedTPS <= 0 || report.FinalityP50NS <= 0 ||
		report.FinalityP50NS > report.FinalityP95NS || report.FinalityP95NS > report.FinalityP99NS ||
		result.GeneratedCrossShard != 0 || result.ImportedCrossShard != 0 || result.DrainBlocks != 0 ||
		result.FinalizedConsensusBlocks != 3 {
		t.Fatalf("unexpected finalized-through-consensus result: %#v", result)
	}
	raw, err := report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINALIZED_THROUGH_CONSENSUS_REPORT=%s", raw)
}

func TestV2LabFinalityReportCrossShardReceiptImport(t *testing.T) {
	result, err := RunFinalityHarness(FinalityHarnessConfig{
		ShardCount:           2,
		ValidatorCount:       7,
		TransactionsPerBlock: 4,
		WarmupBlocks:         1,
		MeasuredBlocks:       2,
		CrossShardBPS:        5_000,
		BlockCadence:         time.Millisecond,
		Workers:              4,
		CommitSHA:            os.Getenv("GITHUB_SHA"),
		Workload:             "native-transfer-receipt-import-v2-lab-single-process",
		Topology:             "single-process-in-memory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Report.Valid || result.Report.MeasuredFinalized != 8 || result.Report.Config.CrossShardBPS != 5_000 ||
		result.GeneratedCrossShard != 6 || result.ImportedCrossShard != 6 || result.DrainBlocks != 1 ||
		result.FinalizedConsensusBlocks != 4 {
		t.Fatalf("unexpected cross-shard finalized result: %#v", result)
	}
	raw, err := result.Report.JSON()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("FINALIZED_THROUGH_CONSENSUS_CROSS_SHARD_REPORT=%s", raw)
}

func TestFinalityHarnessRejectsUnrepresentableCrossShardRatio(t *testing.T) {
	_, err := RunFinalityHarness(FinalityHarnessConfig{
		ShardCount:           2,
		ValidatorCount:       7,
		TransactionsPerBlock: 4,
		MeasuredBlocks:       1,
		CrossShardBPS:        1_000,
		BlockCadence:         time.Millisecond,
		Workers:              1,
	})
	if err == nil {
		t.Fatal("harness accepted a cross-shard ratio that cannot be represented exactly per block")
	}
}


func TestV2LabFinalityMatrixFourShardCrossShardSmoke(t *testing.T) {
	result, err := RunFinalityHarness(FinalityHarnessConfig{
		ShardCount:           4,
		ValidatorCount:       7,
		TransactionsPerBlock: 4,
		WarmupBlocks:         0,
		MeasuredBlocks:       1,
		CrossShardBPS:        2_500,
		BlockCadence:         time.Millisecond,
		Workers:              4,
		CommitSHA:            os.Getenv("GITHUB_SHA"),
		Workload:             "native-transfer-receipt-import-four-shard-smoke",
		Topology:             "single-process-in-memory",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Report.Valid || result.Report.Config.ShardCount != 4 ||
		result.Report.Config.CrossShardBPS != 2_500 ||
		result.GeneratedCrossShard != 1 || result.ImportedCrossShard != 1 ||
		result.DrainBlocks != 1 || result.FinalizedConsensusBlocks != 2 {
		t.Fatalf("unexpected four-shard matrix smoke result: %#v", result)
	}
}

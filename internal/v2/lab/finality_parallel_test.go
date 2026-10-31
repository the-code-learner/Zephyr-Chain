package lab

import (
	"strconv"
	"testing"
	"time"
)

// Ensure concurrently built/committed replicas agree on the same certified
// block and import every cross-shard receipt at larger validator counts.
func TestV2ConcurrentValidatorFinalityMatrix(t *testing.T) {
	for _, validators := range []uint32{4, 7, 16, 32} {
		for _, shardCount := range []uint32{1, 2} {
			t.Run(fmtValidatorShard(validators, shardCount), func(t *testing.T) {
				cross := uint32(0)
				if shardCount > 1 {
					cross = 5000
				}
				result, err := RunFinalityHarness(FinalityHarnessConfig{
					ValidatorCount: validators, ShardCount: shardCount,
					TransactionsPerBlock: 4, WarmupBlocks: 1, MeasuredBlocks: 2,
					CrossShardBPS: cross, BlockCadence: time.Millisecond,
					Workers: 2, Topology: "single-process-in-memory",
				})
				if err != nil {
					t.Fatal(err)
				}
				if !result.Report.Valid || result.GeneratedCrossShard != result.ImportedCrossShard {
					t.Fatalf("concurrent validator finality or receipt parity failed: %+v", result)
				}
			})
		}
	}
}

func fmtValidatorShard(validators, shards uint32) string {
	return strconv.FormatUint(uint64(validators), 10) + "-validators-" + strconv.FormatUint(uint64(shards), 10) + "-shards"
}

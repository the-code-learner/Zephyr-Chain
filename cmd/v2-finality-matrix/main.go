package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/lab"
)

type matrixSkipped struct {
	ShardCount    uint32 `json:"shard_count"`
	CrossShardBPS uint32 `json:"cross_shard_bps"`
	Reason        string `json:"reason"`
}

type matrixOutput struct {
	Version    uint16                      `json:"version"`
	Disclaimer string                      `json:"disclaimer"`
	Results    []lab.FinalityHarnessResult `json:"results"`
	Skipped    []matrixSkipped             `json:"skipped,omitempty"`
}

func main() {
	shardsRaw := flag.String("shards", "1,2,4,8", "comma-separated shard counts")
	crossRaw := flag.String("cross-shard-bps", "0,1000,2500,5000", "comma-separated cross-shard ratios in basis points")
	txPerBlock := flag.Uint64("transactions-per-block", 40, "transactions finalized per traffic block")
	warmupBlocks := flag.Uint64("warmup-blocks", 1, "warmup traffic blocks excluded from measured samples")
	measuredBlocks := flag.Uint64("measured-blocks", 3, "traffic blocks included in measured samples")
	cadence := flag.Duration("block-cadence", time.Millisecond, "minimum submission cadence between traffic blocks")
	validators := flag.Uint("validators", 7, "validator count")
	workers := flag.Int("workers", runtime.GOMAXPROCS(0), "execution workers per runtime")
	commit := flag.String("commit", os.Getenv("GITHUB_SHA"), "commit SHA recorded in every report")
	topology := flag.String("topology", "single-process-in-memory", "topology label recorded in every report")
	workload := flag.String("workload", "native-transfer-receipt-import-matrix", "workload label recorded in every report")
	flag.Parse()

	shards, err := parseUint32List(*shardsRaw, false)
	if err != nil {
		fatal(err)
	}
	crossRatios, err := parseUint32List(*crossRaw, true)
	if err != nil {
		fatal(err)
	}
	if *validators > uint(^uint32(0)) {
		fatal(fmt.Errorf("validator count overflows uint32"))
	}

	output := matrixOutput{
		Version:    1,
		Disclaimer: "single-process finalized-through-consensus instrumentation; not a production-capacity or TPS claim",
		Results:    make([]lab.FinalityHarnessResult, 0, len(shards)*len(crossRatios)),
	}
	for _, shardCount := range shards {
		for _, crossShardBPS := range crossRatios {
			if shardCount < 2 && crossShardBPS > 0 {
				output.Skipped = append(output.Skipped, matrixSkipped{
					ShardCount: shardCount, CrossShardBPS: crossShardBPS,
					Reason: "cross-shard traffic requires at least two shards",
				})
				continue
			}
			result, err := lab.RunFinalityHarness(lab.FinalityHarnessConfig{
				ShardCount:           shardCount,
				ValidatorCount:       uint32(*validators),
				TransactionsPerBlock: *txPerBlock,
				WarmupBlocks:         *warmupBlocks,
				MeasuredBlocks:       *measuredBlocks,
				CrossShardBPS:        crossShardBPS,
				BlockCadence:         *cadence,
				Workers:              *workers,
				CommitSHA:            *commit,
				Workload:             *workload,
				Topology:             *topology,
			})
			if err != nil {
				fatal(fmt.Errorf("shards=%d cross_shard_bps=%d: %w", shardCount, crossShardBPS, err))
			}
			output.Results = append(output.Results, result)
		}
	}

	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		fatal(err)
	}
}

func parseUint32List(raw string, allowZero bool) ([]uint32, error) {
	parts := strings.Split(raw, ",")
	out := make([]uint32, 0, len(parts))
	seen := make(map[uint32]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, fmt.Errorf("empty matrix value")
		}
		value, err := strconv.ParseUint(part, 10, 32)
		if err != nil {
			return nil, err
		}
		parsed := uint32(value)
		if parsed == 0 && !allowZero {
			return nil, fmt.Errorf("matrix values must be positive")
		}
		if _, duplicate := seen[parsed]; duplicate {
			return nil, fmt.Errorf("duplicate matrix value %d", parsed)
		}
		seen[parsed] = struct{}{}
		out = append(out, parsed)
	}
	return out, nil
}

func fatal(err error) {
	_, _ = fmt.Fprintf(os.Stderr, "v2-finality-matrix: %v\n", err)
	os.Exit(1)
}

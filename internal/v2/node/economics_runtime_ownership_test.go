package node

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestRuntimeOwnsIndependentEconomicsCollector(t *testing.T) {
	network := types.NetworkID{1}
	native := types.TokenID{2}
	runtime, err := NewRuntime(network, native, types.Hash{3}, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := economics.NewEpochCollector(economics.EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: native,
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000},
		ResourceCapacityPerBlock: map[uint32]uint64{0: 100},
		VelocityPolicy: economics.VelocityPolicy{
			MinAgeBlocks: 1, FullWeightAgeBlocks: 10, MaxVelocityBps: 10_000,
		},
		FeePolicy: economics.CompatibilityFeePolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomics(collector); err != nil {
		t.Fatal(err)
	}
	if err := collector.AdvanceEpoch(2); err != nil {
		t.Fatal(err)
	}
	metrics, _, err := runtime.EconomicEpochSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].Epoch != 1 {
		t.Fatalf("external collector mutation leaked into runtime: %#v", metrics)
	}
}

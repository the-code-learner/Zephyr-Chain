package node

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestEconomicCheckpointRestoresPendingEpochAndFinalizesIt(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economic-checkpoint")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	store := worldstate.NewMemory()
	runtime, engineConfig := newCheckpointEconomicsRuntime(t, network, native, validatorRoot, store)

	commitEmptySchedulerBlock(t, runtime, key, validators, 1)
	commitEmptySchedulerBlock(t, runtime, key, validators, 2)
	pendingBefore, ok := runtime.PendingEconomicState()
	if !ok || pendingBefore.Epoch != 1 {
		t.Fatalf("expected pending first epoch before checkpoint: %#v", pendingBefore)
	}

	path := filepath.Join(t.TempDir(), "economics.checkpoint")
	if err := runtime.SaveEconomicCheckpoint(path); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("checkpoint permissions = %o, want 600", info.Mode().Perm())
	}

	restored, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	// Normal consensus/world-state recovery owns these anchors. Economic restore
	// is deliberately forbidden from inventing them.
	restored.Height = runtime.Height
	restored.ParentHash = runtime.ParentHash
	if err := restored.RestoreEconomicCheckpoint(path, nil, engineConfig); err != nil {
		t.Fatal(err)
	}
	pendingAfter, ok := restored.PendingEconomicState()
	if !ok || pendingAfter != pendingBefore {
		t.Fatalf("pending state changed across restart: %#v != %#v", pendingAfter, pendingBefore)
	}
	candidate, err := restored.BuildCandidate(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidate(t, restored, key, validators, candidate)
	finalized, ok := restored.FinalizedEconomicState()
	if !ok || finalized != pendingBefore {
		t.Fatalf("restored pending state did not finalize: %#v", finalized)
	}
	if _, exists := store.GetObject(economics.MonetaryStateObjectID(network)); !exists {
		t.Fatal("restored pending monetary object was not committed to world state")
	}
}

func TestEconomicCheckpointRejectsWrongChainAnchorAndTamper(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economic-checkpoint-reject")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, validatorRoot := schedulerValidatorSet(t, network)
	store := worldstate.NewMemory()
	runtime, engineConfig := newCheckpointEconomicsRuntime(t, network, native, validatorRoot, store)
	commitEmptySchedulerBlock(t, runtime, key, validators, 1)

	raw, err := runtime.EconomicCheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	wrongAnchor, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	wrongAnchor.Height = runtime.Height
	wrongAnchor.ParentHash = types.Hash{99}
	if err := wrongAnchor.RestoreEconomicCheckpointBytes(raw, nil, engineConfig); err == nil {
		t.Fatal("economic checkpoint restored against a different parent hash")
	}

	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)/2] ^= 0xff
	matching, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	matching.Height = runtime.Height
	matching.ParentHash = runtime.ParentHash
	if err := matching.RestoreEconomicCheckpointBytes(tampered, nil, engineConfig); err == nil {
		t.Fatal("tampered economic checkpoint was accepted")
	}
}

func newCheckpointEconomicsRuntime(t *testing.T, network types.NetworkID, native types.TokenID, validatorRoot types.Hash, store worldstate.Backend) (*Runtime, economics.ShadowEpochEngineConfig) {
	t.Helper()
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := economics.NewEpochCollector(economics.EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: native,
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000_000},
		ResourceCapacityPerBlock: map[uint32]uint64{0: 100},
		VelocityPolicy: economics.VelocityPolicy{MinAgeBlocks: 1, FullWeightAgeBlocks: 10, MaxVelocityBps: 10_000},
		FeePolicy: economics.CompatibilityFeePolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomics(collector); err != nil {
		t.Fatal(err)
	}
	index := economics.ComputeIndexConfig{MinSamplesPerClass: 1, MinCoverageBps: 10_000, EWMABps: 10_000}
	index.WeightsBps[compute.WorkCPUGeneral] = 10_000
	engineConfig := economics.ShadowEpochEngineConfig{
		ComputeIndex: index, ComputeScarcity: economics.DefaultComputeScarcityConfig(),
		Monetary: economics.DefaultShadowPolicy(),
		ComputeFeedback: economics.DefaultComputeFeedbackPolicy(economics.ComputeFeedbackObserveOnly),
	}
	engine, err := economics.NewShadowEpochEngine(network, engineConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomicEpochs(engine, 2, economics.MonetaryBalanceSnapshot{TotalSupply: 1_000_000, BaseFee: 1}); err != nil {
		t.Fatal(err)
	}
	return runtime, engineConfig
}

var _ v2consensus.ValidatorSet

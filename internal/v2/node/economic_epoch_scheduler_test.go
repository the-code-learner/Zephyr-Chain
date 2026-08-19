package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestShadowEconomicEpochStateEntersNextConsensusCandidate(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economic-epoch-scheduler")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validatorKey, validators, validatorRoot := schedulerValidatorSet(t, network)
	store := worldstate.NewMemory()
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: store}, 1)
	if err != nil {
		t.Fatal(err)
	}

	collector, err := economics.NewEpochCollector(economics.EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: native,
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000_000},
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
	index := economics.ComputeIndexConfig{MinSamplesPerClass: 1, MinCoverageBps: 10_000, EWMABps: 10_000}
	index.WeightsBps[compute.WorkCPUGeneral] = 10_000
	engine, err := economics.NewShadowEpochEngine(network, economics.ShadowEpochEngineConfig{
		ComputeIndex:    index,
		ComputeScarcity: economics.DefaultComputeScarcityConfig(),
		Monetary:        economics.DefaultShadowPolicy(),
		ComputeFeedback: economics.DefaultComputeFeedbackPolicy(economics.ComputeFeedbackObserveOnly),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomicEpochs(engine, 2, economics.MonetaryBalanceSnapshot{
		TotalSupply: 1_000_000, BaseFee: 1,
	}); err != nil {
		t.Fatal(err)
	}

	commitEmptySchedulerBlock(t, runtime, validatorKey, validators, 1)
	if _, pending := runtime.PendingEconomicState(); pending {
		t.Fatal("epoch closed before configured boundary")
	}

	commitEmptySchedulerBlock(t, runtime, validatorKey, validators, 2)
	pending, ok := runtime.PendingEconomicState()
	if !ok || pending.Epoch != 1 || !pending.Shadow || pending.TotalSupply != 1_000_000 {
		t.Fatalf("unexpected pending epoch state: %#v", pending)
	}
	if _, exists := store.GetObject(economics.MonetaryStateObjectID(network)); exists {
		t.Fatal("pending monetary state entered world state before a consensus candidate")
	}

	rootBefore := store.Root()
	candidate, err := runtime.BuildCandidate(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	if candidate.Commitments[0].StateRoot == rootBefore {
		t.Fatal("next candidate did not commit the pending monetary object")
	}
	commitSchedulerCandidate(t, runtime, validatorKey, validators, candidate)

	if _, pending := runtime.PendingEconomicState(); pending {
		t.Fatal("finalized pending state was not cleared")
	}
	finalized, ok := runtime.FinalizedEconomicState()
	if !ok || finalized.Epoch != 1 || !finalized.Shadow {
		t.Fatalf("shadow monetary state was not finalized: %#v", finalized)
	}
	obj, exists := store.GetObject(economics.MonetaryStateObjectID(network))
	if !exists || obj.Version != 1 {
		t.Fatalf("monetary system object missing after consensus finality: %#v", obj)
	}
	parsed, err := economics.ParseMonetaryEpochState(obj.Data)
	if err != nil || parsed != finalized {
		t.Fatalf("committed monetary object mismatch: %#v %v", parsed, err)
	}
	if finalized.ShadowGrossMintTarget == 0 {
		t.Fatal("expected a shadow issuance suggestion for simulation")
	}
	if supply, _ := runtime.economicCollector.CirculatingSupply(0); supply != 1_000_000 {
		t.Fatalf("shadow issuance mutated live circulating supply: %d", supply)
	}
}

func TestShadowEconomicEpochSchedulerRejectsOneBlockEpochs(t *testing.T) {
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
	index := economics.ComputeIndexConfig{MinSamplesPerClass: 1, MinCoverageBps: 10_000, EWMABps: 10_000}
	index.WeightsBps[compute.WorkCPUGeneral] = 10_000
	engine, err := economics.NewShadowEpochEngine(network, economics.ShadowEpochEngineConfig{
		ComputeIndex: index, ComputeScarcity: economics.DefaultComputeScarcityConfig(),
		Monetary: economics.DefaultShadowPolicy(), ComputeFeedback: economics.DefaultComputeFeedbackPolicy(economics.ComputeFeedbackObserveOnly),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomicEpochs(engine, 1, economics.MonetaryBalanceSnapshot{TotalSupply: 1_000, BaseFee: 1}); err != ErrRuntimeConfig {
		t.Fatalf("one-block shadow epoch accepted: %v", err)
	}
}

func schedulerValidatorSet(t *testing.T, network types.NetworkID) (*ecdsa.PrivateKey, v2consensus.ValidatorSet, types.Hash) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	validators := v2consensus.ValidatorSet{
		Network: network,
		Validators: []v2consensus.Validator{{
			ID: types.ValidatorIDFromPublicKey(publicKey), PublicKey: publicKey, Power: 10,
		}},
	}
	root, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}
	return key, validators, root
}

func commitEmptySchedulerBlock(t *testing.T, runtime *Runtime, key *ecdsa.PrivateKey, validators v2consensus.ValidatorSet, height uint64) {
	t.Helper()
	candidate, err := runtime.BuildCandidate(height, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidate(t, runtime, key, validators, candidate)
}

func commitSchedulerCandidate(t *testing.T, runtime *Runtime, key *ecdsa.PrivateKey, validators v2consensus.ValidatorSet, candidate Candidate) {
	t.Helper()
	proposal, err := v2consensus.SignProposal(key, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(key, runtime.Network, candidate.Header.Height, 0, v2consensus.HeaderConsensusHash(candidate.Header))
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := validators.BuildCertificate(proposal, []v2consensus.Vote{vote})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Commit(candidate, certificate, validators); err != nil {
		t.Fatal(err)
	}
}

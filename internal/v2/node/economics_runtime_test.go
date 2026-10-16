package node

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestRuntimeEconomicsAdvancesOnlyAfterValidQCCommit(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("economics-runtime")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	validatorKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	validatorPub := elliptic.Marshal(elliptic.P256(), validatorKey.PublicKey.X, validatorKey.PublicKey.Y)
	validators := v2consensus.ValidatorSet{
		Network: network,
		Validators: []v2consensus.Validator{{
			ID: types.ValidatorIDFromPublicKey(validatorPub), PublicKey: validatorPub, Power: 10,
		}},
	}
	validatorRoot, err := validators.Root()
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := NewRuntime(network, native, validatorRoot, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := economics.NewEpochCollector(economics.EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: native,
		InitialCirculatingSupply: map[uint32]uint64{0: 1_000},
		OpeningComputeBacklog:    map[uint32]uint64{},
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

	candidate, err := runtime.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v2consensus.SignProposal(validatorKey, candidate.Header, 0)
	if err != nil {
		t.Fatal(err)
	}
	vote, err := v2consensus.SignVote(validatorKey, network, 1, 0, v2consensus.HeaderConsensusHash(candidate.Header))
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
	metrics, _, err := runtime.EconomicEpochSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(metrics) != 1 || metrics[0].ResourceCapacity != 100 || metrics[0].CirculatingNativeSupply != 1_000 {
		t.Fatalf("unexpected finalized economics: %#v", metrics)
	}

	candidate2, err := runtime.BuildCandidate(2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runtime.Commit(candidate2, v2consensus.Certificate{}, validators); err == nil {
		t.Fatal("invalid certificate unexpectedly committed")
	}
	after, _, err := runtime.EconomicEpochSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if after[0].ResourceCapacity != metrics[0].ResourceCapacity {
		t.Fatalf("failed commit advanced economics: before=%#v after=%#v", metrics[0], after[0])
	}
}

func TestRuntimeRejectsMismatchedEconomicsCollector(t *testing.T) {
	network := types.NetworkID{1}
	native := types.TokenID{2}
	runtime, err := NewRuntime(network, native, types.Hash{3}, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, 1)
	if err != nil {
		t.Fatal(err)
	}
	collector, err := economics.NewEpochCollector(economics.EpochCollectorConfig{
		Epoch: 1, ShardCount: 1, NativeToken: types.TokenID{9},
		InitialCirculatingSupply: map[uint32]uint64{0: 1},
		ResourceCapacityPerBlock: map[uint32]uint64{0: 1},
		VelocityPolicy: economics.VelocityPolicy{
			MinAgeBlocks: 1, FullWeightAgeBlocks: 1, MaxVelocityBps: 1,
		},
		FeePolicy: economics.CompatibilityFeePolicy(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.EnableShadowEconomics(collector); err != ErrRuntimeConfig {
		t.Fatalf("mismatched collector accepted: %v", err)
	}
}

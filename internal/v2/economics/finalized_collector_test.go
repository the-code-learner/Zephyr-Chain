package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func testCollectorConfig(shards uint32, native types.TokenID) EpochCollectorConfig {
	supply := make(map[uint32]uint64, shards)
	capacity := make(map[uint32]uint64, shards)
	for shard := uint32(0); shard < shards; shard++ {
		capacity[shard] = 10_000
	}
	return EpochCollectorConfig{
		Epoch: 1, ShardCount: shards, NativeToken: native,
		InitialCirculatingSupply: supply,
		OpeningComputeBacklog:    make(map[uint32]uint64),
		ResourceCapacityPerBlock: capacity,
		VelocityPolicy: VelocityPolicy{
			MinAgeBlocks: 1, FullWeightAgeBlocks: 10, MaxVelocityBps: 10_000,
		},
		FeePolicy: CompatibilityFeePolicy(),
	}
}

func TestEpochCollectorMovesCrossShardSupplyOnImport(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 1_000
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{2}, native, 100, 1)
	if err != nil {
		t.Fatal(err)
	}
	receipt := sharding.CrossShardReceipt{SourceShard: 0, DestinationShard: 1, Output: output}
	if err := collector.ObserveFinalizedBlock(1, map[uint32]FinalizedShardObservation{
		1: {Imports: []sharding.CrossShardReceipt{receipt}},
	}); err != nil {
		t.Fatal(err)
	}
	left, _ := collector.CirculatingSupply(0)
	right, _ := collector.CirculatingSupply(1)
	if left != 900 || right != 100 {
		t.Fatalf("unexpected shard supply attribution: %d/%d", left, right)
	}
	metrics, _, err := collector.FinalizeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if metrics[0].CirculatingNativeSupply+metrics[1].CirculatingNativeSupply != 1_000 {
		t.Fatal("cross-shard import changed global supply")
	}
}

func TestEpochCollectorRejectsBlockAtomically(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 100
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{2}, native, 200, 1)
	if err != nil {
		t.Fatal(err)
	}
	err = collector.ObserveFinalizedBlock(1, map[uint32]FinalizedShardObservation{
		1: {Imports: []sharding.CrossShardReceipt{{SourceShard: 0, DestinationShard: 1, Output: output}}},
	})
	if err == nil {
		t.Fatal("expected impossible cross-shard supply rejection")
	}
	left, _ := collector.CirculatingSupply(0)
	right, _ := collector.CirculatingSupply(1)
	if left != 100 || right != 0 {
		t.Fatalf("failed observation partially mutated collector: %d/%d", left, right)
	}
}

func TestEpochCollectorCarriesComputeBacklogAndBuildsVerifiedWork(t *testing.T) {
	native := types.TokenID{1}
	workload := types.Hash{3}
	registry, err := compute.NewWorkRegistry([]compute.WorkSpec{{
		Version: compute.WorkSpecVersion, Class: compute.WorkAITraining, Units: 100,
		WorkloadHash: workload, BenchmarkHash: types.Hash{4},
		Vector: compute.WorkVector{TensorUnits: 100},
	}})
	if err != nil {
		t.Fatal(err)
	}
	cfg := testCollectorConfig(1, native)
	cfg.InitialCirculatingSupply[0] = 1_000
	cfg.WorkRegistry = registry
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}

	job := compute.Job{
		Owner: types.AccountID{5}, WorkloadHash: workload, InputRoot: types.Hash{6},
		Resources: compute.Resources{CPUCores: 1, MemoryMiB: 1}, MaxPrice: 30,
		CollateralRequired: 2, Verification: compute.VerificationReplicated,
		DeadlineHeight: 100, Replicas: 2,
	}
	jobRaw, err := job.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	post := tx.Transaction{ShardID: 0, Operations: []tx.Operation{{Kind: tx.OpComputeJob, Payload: jobRaw}}}
	postResult := execution.Result{TxID: post.ID()}
	if err := collector.ObserveFinalizedBlock(1, map[uint32]FinalizedShardObservation{
		0: {Transactions: []tx.Transaction{post}, Results: []execution.Result{postResult}},
	}); err != nil {
		t.Fatal(err)
	}
	first, _, err := collector.FinalizeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if first[0].EscrowBackedComputeDemand != 100 || first[0].ComputeBacklog != 100 {
		t.Fatalf("unexpected first epoch compute flow: %#v", first[0])
	}
	if err := collector.AdvanceEpoch(2); err != nil {
		t.Fatal(err)
	}

	jobID := types.JobID{7}
	providers := []types.AccountID{{8}, {9}}
	root := types.Hash{10}
	record := compute.OnChainJob{
		ID: jobID, Job: job, Escrow: 30, Status: compute.JobAwaitingVerification,
		Assignments: []compute.Assignment{
			{OfferID: types.Hash{11}, Provider: providers[0], Price: 10},
			{OfferID: types.Hash{12}, Provider: providers[1], Price: 20},
		},
		Results: []compute.Result{
			{JobID: jobID, Provider: providers[0], ResultRoot: root, CompletedHeight: 2},
			{JobID: jobID, Provider: providers[1], ResultRoot: root, CompletedHeight: 2},
		},
	}
	recordRaw, err := record.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	receipt := compute.SettlementReceipt{
		JobID: jobID, ResultRoot: root,
		Payments: map[types.AccountID]uint64{providers[0]: 10, providers[1]: 20},
	}
	jobObject := object.Object{ID: types.ObjectID{13}, Version: 1, Owner: job.Owner, Kind: object.KindComputeJob, Data: recordRaw}
	finalize := tx.Transaction{
		ShardID:    0,
		Operations: []tx.Operation{{Kind: tx.OpComputeFinalize}},
		Witnesses:  []tx.Witness{{Object: jobObject}},
	}
	finalizeResult := execution.Result{
		TxID:    finalize.ID(),
		Created: []object.Object{{ID: types.ObjectID{14}, Version: 1, Kind: object.KindSystem, Data: receipt.MarshalBinary()}},
	}
	if err := collector.ObserveFinalizedBlock(2, map[uint32]FinalizedShardObservation{
		0: {
			Transactions: []tx.Transaction{finalize}, Results: []execution.Result{finalizeResult},
			ComputeCapacityUnits: 100, ComputeCapacityReliable: true,
		},
	}); err != nil {
		t.Fatal(err)
	}
	second, verified, err := collector.FinalizeEpoch()
	if err != nil {
		t.Fatal(err)
	}
	if second[0].OpeningComputeBacklog != 100 || second[0].ComputeFulfilled != 100 || second[0].ComputeBacklog != 0 {
		t.Fatalf("unexpected second epoch compute flow: %#v", second[0])
	}
	if !second[0].ComputeSupplyReliable || second[0].VerifiedComputeSupply != 100 {
		t.Fatalf("unexpected compute supply: %#v", second[0])
	}
	if len(verified) != 1 || verified[0].PaidZPH != 30 || verified[0].Units != 100 {
		t.Fatalf("unexpected verified work: %#v", verified)
	}
}

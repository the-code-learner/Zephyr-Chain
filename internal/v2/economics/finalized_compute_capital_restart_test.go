package economics

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestFinalizedComputeSettlementRollbackDoesNotLeakTemporaryLegs(t *testing.T) {
	native := types.TokenID{1}
	record := finalizedComputeRecord(false)
	tracker := seededComputeComponentTracker(t, record)
	transaction, result, outputs := finalizedComputeSettlementFixture(t, native, record, tx.OpComputeFinalize, 60)

	// Force a failure after the settlement components have been split into
	// temporary payment/refund/collateral legs inside the preview tracker.
	// The externally visible tracker must remain byte-for-byte unchanged.
	collision := outputs[record.Assignments[0].Provider]
	tracker.objects[collision] = []CapitalLot{{
		LineageID:       types.Hash{99},
		Amount:          1,
		IdleSinceHeight: 4,
	}}
	before, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}

	applied, err := ApplyFinalizedComputeCapitalLineage(tracker, 60, native, transaction, result, nil)
	if err == nil || applied {
		t.Fatalf("settlement with an occupied output target was accepted: applied=%v err=%v", applied, err)
	}
	after, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("failed compute settlement mutated tracker state")
	}

	escrowID, err := ComputeEscrowComponentID(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tracker.ObjectLots(types.ObjectID(escrowID)); !ok {
		t.Fatal("failed settlement consumed escrow component")
	}
	for _, assignment := range record.Assignments {
		collateralID, err := ComputeCollateralComponentID(record.ID, assignment.Provider)
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := tracker.ObjectLots(types.ObjectID(collateralID)); !ok {
			t.Fatalf("failed settlement consumed collateral component for %s", assignment.Provider)
		}
		for _, kind := range []computeSettlementLegKind{computeSettlementLegPayment, computeSettlementLegCollateralReturn} {
			legID, err := computeSettlementLegObjectID(record.ID, kind, assignment.Provider, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := tracker.ObjectLots(legID); ok {
				t.Fatalf("failed settlement leaked temporary leg %s", legID)
			}
		}
	}
	refundLeg, err := computeSettlementLegObjectID(record.ID, computeSettlementLegRefund, record.Job.Owner, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := tracker.ObjectLots(refundLeg); ok {
		t.Fatal("failed settlement leaked temporary refund leg")
	}
}

func TestEpochCollectorCheckpointRestoresLargeComputePendingCrossShardState(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 2_000_000
	cfg.InitialCirculatingSupply[1] = 1_000_000
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.EnableIdleCapitalTracking(NewIdleCapitalTracker()); err != nil {
		t.Fatal(err)
	}

	record := finalizedComputeRecord(false)
	remoteProvider := types.AccountID{}
	remoteProvider[7] = 1
	if types.AccountShard(remoteProvider, 2) != 1 {
		t.Fatal("test remote provider did not map to shard 1")
	}
	record.Assignments[0].Provider = remoteProvider
	record.Results[0].Provider = remoteProvider
	collector.idleCapital = seededComputeComponentTracker(t, record)
	addLargeRestartNoise(t, collector.idleCapital, 512)

	transaction, result, exports, remoteReceipt := finalizedComputeCrossShardSettlementFixture(
		t, native, record, tx.OpComputeFinalize, 60, remoteProvider,
	)
	if err := collector.ObserveFinalizedBlock(60, map[uint32]FinalizedShardObservation{
		0: {
			Transactions: []tx.Transaction{transaction},
			Results:      []execution.Result{result},
			Exports:      exports,
		},
	}); err != nil {
		t.Fatal(err)
	}

	remoteTransferID, err := remoteReceipt.Hash()
	if err != nil {
		t.Fatal(err)
	}
	pendingLots, ok := collector.idleCapital.pendingTransfers[remoteTransferID]
	if !ok {
		t.Fatal("finalized cross-shard compute settlement did not retain pending lineage")
	}
	if idleCapitalAmountForTest(t, pendingLots) != 50 ||
		trackedLotsAtHeight(pendingLots, 60) != 40 ||
		trackedLotsAtHeight(pendingLots, 6) != 10 {
		t.Fatalf("unexpected pending payment/collateral lineage: %#v", pendingLots)
	}
	beforeSnapshot, enabled, err := collector.IdleCapitalSnapshot(60, []uint64{10, 100})
	if err != nil || !enabled {
		t.Fatalf("pre-checkpoint idle snapshot failed: enabled=%v err=%v", enabled, err)
	}
	if beforeSnapshot.LiveObjects < 512 || beforeSnapshot.PendingTransfers != 513 ||
		beforeSnapshot.ProductiveObservedCapital != 100 || beforeSnapshot.ProductiveCoverageBps != 7_000 {
		t.Fatalf("unexpected large pre-checkpoint snapshot: %#v", beforeSnapshot)
	}

	raw, err := collector.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	restored, err := RestoreEpochCollector(raw, nil)
	if err != nil {
		t.Fatal(err)
	}
	rawAgain, err := restored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(raw, rawAgain) {
		t.Fatal("large compute collector checkpoint is not canonical across restore")
	}
	afterSnapshot, enabled, err := restored.IdleCapitalSnapshot(60, []uint64{10, 100})
	if err != nil || !enabled {
		t.Fatalf("restored idle snapshot failed: enabled=%v err=%v", enabled, err)
	}
	if beforeSnapshot != afterSnapshot {
		t.Fatalf("collector checkpoint changed aggregate idle state: before=%#v after=%#v", beforeSnapshot, afterSnapshot)
	}
	restoredPending, ok := restored.idleCapital.pendingTransfers[remoteTransferID]
	if !ok || idleCapitalAmountForTest(t, restoredPending) != 50 ||
		trackedLotsAtHeight(restoredPending, 60) != 40 ||
		trackedLotsAtHeight(restoredPending, 6) != 10 {
		t.Fatalf("checkpoint lost pending compute settlement lineage: %#v", restoredPending)
	}

	for _, cut := range []int{1, len(raw) / 2, len(raw) - 1} {
		if _, err := RestoreEpochCollector(raw[:cut], nil); err == nil {
			t.Fatalf("truncated collector checkpoint restored at cut %d/%d", cut, len(raw))
		}
	}
	trailing := append(append([]byte(nil), raw...), 0)
	if _, err := RestoreEpochCollector(trailing, nil); err == nil {
		t.Fatal("collector checkpoint with trailing data restored")
	}

	if err := restored.ObserveFinalizedBlock(61, map[uint32]FinalizedShardObservation{
		1: {Imports: []sharding.CrossShardReceipt{remoteReceipt}},
	}); err != nil {
		t.Fatal(err)
	}
	destination, err := remoteReceipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	materialized := mustTrackedLots(t, restored.idleCapital, destination.ID)
	if idleCapitalAmountForTest(t, materialized) != 50 ||
		trackedLotsAtHeight(materialized, 60) != 40 ||
		trackedLotsAtHeight(materialized, 6) != 10 {
		t.Fatalf("restart/import changed compute payment or collateral age: %#v", materialized)
	}
	finalSnapshot, enabled, err := restored.IdleCapitalSnapshot(61, []uint64{10, 100})
	if err != nil || !enabled {
		t.Fatalf("post-import idle snapshot failed: enabled=%v err=%v", enabled, err)
	}
	if finalSnapshot.PendingTransfers != 512 ||
		finalSnapshot.ProductiveObservedCapital != 100 ||
		finalSnapshot.ProductiveCoverageBps != 7_000 {
		t.Fatalf("restart/import changed pending or productive telemetry: %#v", finalSnapshot)
	}

	postImportRaw, err := restored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	postImportRestored, err := RestoreEpochCollector(postImportRaw, nil)
	if err != nil {
		t.Fatal(err)
	}
	postImportRawAgain, err := postImportRestored.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(postImportRaw, postImportRawAgain) {
		t.Fatal("post-import collector checkpoint is not canonical")
	}
}

func addLargeRestartNoise(t *testing.T, tracker *IdleCapitalTracker, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		live := trackerObjectIDWithIndex("restart-live", i)
		amount := uint64(10 + i%7)
		height := uint64(10 + i%20)
		if err := tracker.BootstrapObject(live, amount, height); err != nil {
			t.Fatalf("bootstrap live noise %d: %v", i, err)
		}

		seed := trackerObjectIDWithIndex("restart-pending-seed", i)
		if err := tracker.BootstrapObject(seed, amount, height); err != nil {
			t.Fatalf("bootstrap pending noise %d: %v", i, err)
		}
		var index [4]byte
		binary.BigEndian.PutUint32(index[:], uint32(i))
		transferID := types.HashBytes("economics/restart-noise-transfer/v1", index[:])
		if err := tracker.ApplyTransition(
			[]types.ObjectID{seed},
			[]CapitalTarget{{TransferID: transferID, Amount: amount}},
			0,
		); err != nil {
			t.Fatalf("create pending noise %d: %v", i, err)
		}
	}
}

func finalizedComputeCrossShardSettlementFixture(
	t *testing.T,
	native types.TokenID,
	record compute.OnChainJob,
	kind uint16,
	height uint64,
	remoteAccount types.AccountID,
) (tx.Transaction, execution.Result, []sharding.CrossShardReceipt, sharding.CrossShardReceipt) {
	t.Helper()
	raw, err := record.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	jobObjectID := types.ObjectID{10}
	refRaw, err := (compute.JobRef{JobObject: jobObjectID}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0,
		Sender:  record.Job.Owner,
		Operations: []tx.Operation{{
			Kind: kind, Payload: refRaw,
		}},
		Witnesses: []tx.Witness{{Object: object.Object{
			ID: jobObjectID, Version: 4, Owner: record.Job.Owner, Kind: object.KindComputeJob, Data: raw,
		}}},
	}

	var settlement compute.OnChainSettlement
	switch kind {
	case tx.OpComputeFinalize:
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
	case tx.OpComputeResolveReplicated:
		_, settlement, err = compute.ResolveReplicatedMajority(record)
	default:
		t.Fatalf("unsupported settlement fixture kind %d", kind)
	}
	if err != nil {
		t.Fatal(err)
	}
	receipt := compute.SettlementReceipt{
		JobID:       record.ID,
		ResultRoot:  settlement.ResultRoot,
		Payments:    settlement.Payments,
		Refund:      settlement.Refund,
		Slashed:     settlement.SlashedCollateral,
		SlashReward: settlement.SlashReward,
	}
	result := execution.Result{
		Consumed: []types.ObjectID{jobObjectID},
		Created: []object.Object{{
			ID:      types.ObjectIDForShard(transaction.ID(), computeSettlementReceiptOutputIndex, 0),
			Version: 1,
			Kind:    object.KindSystem,
			Data:    receipt.MarshalBinary(),
		}},
		TxID: transaction.ID(),
	}

	amounts := make(map[types.AccountID]uint64)
	for provider, amount := range settlement.Payments {
		amounts[provider] += amount
	}
	for provider, amount := range settlement.CollateralReturns {
		amounts[provider] += amount
	}
	amounts[record.Job.Owner] += settlement.Refund + settlement.SlashReward

	accounts := sortedPositiveAccountAmounts(amounts)
	exports := make([]sharding.CrossShardReceipt, 0, 1)
	var remoteReceipt sharding.CrossShardReceipt
	for i, account := range accounts {
		spec, err := object.NewCoinOutput(account, native, amounts[account])
		if err != nil {
			t.Fatal(err)
		}
		index := computeSettlementReceiptOutputIndex + 1 + uint32(i)
		if account == remoteAccount {
			result.Outbound = append(result.Outbound, execution.OutboundOutput{
				DestinationShard: 1,
				OutputIndex:      index,
				Output:           spec,
			})
			remoteReceipt = sharding.CrossShardReceipt{
				SourceShard:      0,
				DestinationShard: 1,
				SourceHeight:     height,
				TransactionID:    transaction.ID(),
				OutputIndex:      index,
				Output:           spec,
				SourceStateRoot:  types.Hash{77},
			}
			exports = append(exports, remoteReceipt)
			continue
		}
		result.Created = append(result.Created, object.Object{
			ID:      types.ObjectIDForShard(transaction.ID(), index, 0),
			Version: 1,
			Owner:   spec.Owner,
			Kind:    spec.Kind,
			Data:    spec.Data,
		})
	}
	if err := remoteReceipt.Validate(); err != nil {
		t.Fatalf("remote settlement receipt invalid: %v", err)
	}
	return transaction, result, exports, remoteReceipt
}

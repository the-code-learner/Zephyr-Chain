package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestFinalizedIdleCapitalTransferUsesCanonicalReceiptIdentity(t *testing.T) {
	native := types.TokenID{1}
	inputID := types.ObjectID{2}
	owner := types.AccountID{3}
	input := object.Object{
		ID: inputID, Version: 1, Owner: owner, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 1_000, CreatedHeight: 10}.MarshalBinary(),
	}
	localOutput, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 600, 100)
	if err != nil {
		t.Fatal(err)
	}
	crossOutput, err := object.NewCoinOutputAtHeight(types.AccountID{5}, native, 390, 100)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID:    0,
		Inputs:     []tx.InputRef{{ObjectID: inputID}},
		Outputs:    []object.OutputSpec{localOutput, crossOutput},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee:        10,
		Witnesses:  []tx.Witness{{Object: input}},
	}
	txID := transaction.ID()
	localID := types.ObjectIDForShard(txID, 0, 0)
	result := execution.Result{
		Consumed: []types.ObjectID{inputID},
		Created: []object.Object{{
			ID: localID, Version: 1, Owner: localOutput.Owner, Kind: localOutput.Kind,
			Data: append([]byte(nil), localOutput.Data...),
		}},
		Outbound: []execution.OutboundOutput{{DestinationShard: 1, OutputIndex: 1, Output: crossOutput}},
		TxID:     txID,
	}
	receipt := sharding.CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 100,
		TransactionID: txID, OutputIndex: 1, Output: crossOutput, SourceStateRoot: types.Hash{9},
	}
	tracker := NewIdleCapitalTracker()
	applied, err := ApplyFinalizedIdleCapitalTransfer(tracker, 100, native, transaction, result, []sharding.CrossShardReceipt{receipt})
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("finalized transfer was not applied")
	}
	receiptHash, err := receipt.Hash()
	if err != nil {
		t.Fatal(err)
	}
	pending, ok := tracker.pendingTransfers[receiptHash]
	if !ok {
		t.Fatal("tracker did not use canonical receipt hash as transfer identity")
	}
	if amount, err := idleCapitalLotsAmount(pending); err != nil || amount != 390 {
		t.Fatalf("unexpected pending lineage amount: %d %v", amount, err)
	}
	localLots, ok := tracker.ObjectLots(localID)
	if !ok || len(localLots) != 1 || localLots[0].Amount != 600 || localLots[0].IdleSinceHeight != 10 {
		t.Fatalf("local output lost input lineage age: %#v", localLots)
	}
	snapshot, err := tracker.Snapshot(100, []uint64{20, 100})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TrackedCapital != 990 || snapshot.PendingTransfers != 1 || snapshot.LiveObjects != 1 {
		t.Fatalf("unexpected post-export snapshot: %#v", snapshot)
	}

	imported, err := ApplyFinalizedIdleCapitalImport(tracker, native, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !imported {
		t.Fatal("finalized import was not materialized")
	}
	destination, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	destinationLots, ok := tracker.ObjectLots(destination.ID)
	if !ok || len(destinationLots) != 1 || destinationLots[0].Amount != 390 || destinationLots[0].IdleSinceHeight != 10 {
		t.Fatalf("cross-shard import lost lineage age: %#v", destinationLots)
	}
}

func TestFinalizedIdleCapitalRejectsReceiptMismatchAtomically(t *testing.T) {
	native := types.TokenID{1}
	inputID := types.ObjectID{2}
	input := object.Object{
		ID: inputID, Version: 1, Owner: types.AccountID{3}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 100, CreatedHeight: 5}.MarshalBinary(),
	}
	crossOutput, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0, Inputs: []tx.InputRef{{ObjectID: inputID}}, Outputs: []object.OutputSpec{crossOutput},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}}, Fee: 10, Witnesses: []tx.Witness{{Object: input}},
	}
	txID := transaction.ID()
	result := execution.Result{
		Consumed: []types.ObjectID{inputID},
		Outbound: []execution.OutboundOutput{{DestinationShard: 1, OutputIndex: 0, Output: crossOutput}},
		TxID:     txID,
	}
	bad := sharding.CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 20,
		TransactionID: txID, OutputIndex: 1, Output: crossOutput, SourceStateRoot: types.Hash{8},
	}
	tracker := NewIdleCapitalTracker()
	if _, err := ApplyFinalizedIdleCapitalTransfer(tracker, 20, native, transaction, result, []sharding.CrossShardReceipt{bad}); err == nil {
		t.Fatal("mismatched finalized receipt was accepted")
	}
	if len(tracker.objects) != 0 || len(tracker.pendingTransfers) != 0 {
		t.Fatal("rejected receipt mismatch partially mutated tracker")
	}
}

func TestFinalizedIdleCapitalUnknownAgeBootstrapSkipsWithoutMutation(t *testing.T) {
	native := types.TokenID{1}
	inputID := types.ObjectID{2}
	input := object.Object{
		ID: inputID, Version: 1, Owner: types.AccountID{3}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 100, CreatedHeight: 0}.MarshalBinary(),
	}
	localOutput, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0, Inputs: []tx.InputRef{{ObjectID: inputID}}, Outputs: []object.OutputSpec{localOutput},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}}, Fee: 10, Witnesses: []tx.Witness{{Object: input}},
	}
	txID := transaction.ID()
	result := execution.Result{
		Consumed: []types.ObjectID{inputID},
		Created: []object.Object{{
			ID: types.ObjectIDForShard(txID, 0, 0), Version: 1, Owner: localOutput.Owner,
			Kind: localOutput.Kind, Data: append([]byte(nil), localOutput.Data...),
		}},
		TxID: txID,
	}
	tracker := NewIdleCapitalTracker()
	applied, err := ApplyFinalizedIdleCapitalTransfer(tracker, 20, native, transaction, result, nil)
	if err != nil {
		t.Fatal(err)
	}
	if applied || len(tracker.objects) != 0 || len(tracker.pendingTransfers) != 0 {
		t.Fatal("unknown-age prospective bootstrap should skip without inventing history")
	}
}

func TestFinalizedIdleCapitalImportBootstrapsWhenExportPredatesTracker(t *testing.T) {
	native := types.TokenID{1}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{2}, native, 250, 40)
	if err != nil {
		t.Fatal(err)
	}
	receipt := sharding.CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 40,
		TransactionID: types.Hash{3}, OutputIndex: 2, Output: output, SourceStateRoot: types.Hash{4},
	}
	tracker := NewIdleCapitalTracker()
	applied, err := ApplyFinalizedIdleCapitalImport(tracker, native, receipt)
	if err != nil {
		t.Fatal(err)
	}
	if !applied {
		t.Fatal("finalized import was not prospectively bootstrapped")
	}
	destination, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	lots, ok := tracker.ObjectLots(destination.ID)
	if !ok || len(lots) != 1 || lots[0].Amount != 250 || lots[0].IdleSinceHeight != 40 {
		t.Fatalf("unexpected prospective import lineage: %#v", lots)
	}
	snapshot, err := tracker.Snapshot(50, []uint64{20, 100})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.BootstrapCapital != 250 {
		t.Fatalf("prospective import bootstrap was hidden: %#v", snapshot)
	}
}

func TestFinalizedIdleCapitalMixedTrackedAndUnknownAgeFailsClosed(t *testing.T) {
	native := types.TokenID{1}
	trackedID := types.ObjectID{2}
	unknownID := types.ObjectID{3}
	tracker := NewIdleCapitalTracker()
	if err := tracker.BootstrapObject(trackedID, 60, 5); err != nil {
		t.Fatal(err)
	}
	tracked := object.Object{
		ID: trackedID, Version: 1, Owner: types.AccountID{4}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 60, CreatedHeight: 5}.MarshalBinary(),
	}
	unknown := object.Object{
		ID: unknownID, Version: 1, Owner: types.AccountID{4}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 40, CreatedHeight: 0}.MarshalBinary(),
	}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{5}, native, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0,
		Inputs:  []tx.InputRef{{ObjectID: trackedID}, {ObjectID: unknownID}},
		Outputs: []object.OutputSpec{output}, Operations: []tx.Operation{{Kind: tx.OpTransfer}}, Fee: 10,
		Witnesses: []tx.Witness{{Object: tracked}, {Object: unknown}},
	}
	txID := transaction.ID()
	result := execution.Result{
		Consumed: []types.ObjectID{trackedID, unknownID},
		Created: []object.Object{{
			ID: types.ObjectIDForShard(txID, 0, 0), Version: 1, Owner: output.Owner,
			Kind: output.Kind, Data: append([]byte(nil), output.Data...),
		}},
		TxID: txID,
	}
	before, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyFinalizedIdleCapitalTransfer(tracker, 20, native, transaction, result, nil); err == nil {
		t.Fatal("mixed tracked and unknown-age capital did not fail closed")
	}
	after, err := tracker.CheckpointBytes()
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("failed mixed-coverage observation mutated tracker")
	}
}

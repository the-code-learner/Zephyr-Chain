package economics

import (
	"bytes"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestEpochCollectorIdleCapitalTracksFinalizedLocalTransferAndCheckpoint(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(1, native)
	cfg.InitialCirculatingSupply[0] = 1_000
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.EnableIdleCapitalTracking(NewIdleCapitalTracker()); err != nil {
		t.Fatal(err)
	}

	inputID := types.ObjectID{2}
	input := object.Object{
		ID: inputID, Version: 1, Owner: types.AccountID{3}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 1_000, CreatedHeight: 10}.MarshalBinary(),
	}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 990, 100)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0, Inputs: []tx.InputRef{{ObjectID: inputID}}, Outputs: []object.OutputSpec{output},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}}, Fee: 10, Witnesses: []tx.Witness{{Object: input}},
	}
	txID := transaction.ID()
	outputID := types.ObjectIDForShard(txID, 0, 0)
	result := execution.Result{
		Consumed: []types.ObjectID{inputID},
		Created: []object.Object{{
			ID: outputID, Version: 1, Owner: output.Owner, Kind: output.Kind, Data: append([]byte(nil), output.Data...),
		}},
		TxID: txID,
	}
	if err := collector.ObserveFinalizedBlock(100, map[uint32]FinalizedShardObservation{
		0: {Transactions: []tx.Transaction{transaction}, Results: []execution.Result{result}},
	}); err != nil {
		t.Fatal(err)
	}
	snapshot, enabled, err := collector.IdleCapitalSnapshot(100, []uint64{20, 100})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || snapshot.TrackedCapital != 990 || snapshot.BootstrapCapital != 990 || snapshot.LiveObjects != 1 {
		t.Fatalf("unexpected collector idle-capital snapshot: %#v enabled=%v", snapshot, enabled)
	}
	lots, ok := collector.idleCapital.ObjectLots(outputID)
	if !ok || len(lots) != 1 || lots[0].IdleSinceHeight != 10 || lots[0].Amount != 990 {
		t.Fatalf("collector lost finalized lineage: %#v", lots)
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
		t.Fatal("idle-capital collector checkpoint is not canonical across restore")
	}
	restoredLots, ok := restored.idleCapital.ObjectLots(outputID)
	if !ok || len(restoredLots) != 1 || restoredLots[0].IdleSinceHeight != 10 || restoredLots[0].Amount != 990 {
		t.Fatalf("collector checkpoint lost idle lineage: %#v", restoredLots)
	}
}

func TestEpochCollectorIdleCapitalTracksCanonicalCrossShardExportAndImport(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 1_000
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.EnableIdleCapitalTracking(NewIdleCapitalTracker()); err != nil {
		t.Fatal(err)
	}

	inputID := types.ObjectID{2}
	input := object.Object{
		ID: inputID, Version: 1, Owner: types.AccountID{3}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 1_000, CreatedHeight: 10}.MarshalBinary(),
	}
	crossOutput, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 990, 100)
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
	receipt := sharding.CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 100,
		TransactionID: txID, OutputIndex: 0, Output: crossOutput, SourceStateRoot: types.Hash{9},
	}
	if err := collector.ObserveFinalizedBlock(100, map[uint32]FinalizedShardObservation{
		0: {Transactions: []tx.Transaction{transaction}, Results: []execution.Result{result}, Exports: []sharding.CrossShardReceipt{receipt}},
	}); err != nil {
		t.Fatal(err)
	}
	receiptHash, err := receipt.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := collector.idleCapital.pendingTransfers[receiptHash]; !ok {
		t.Fatal("collector did not retain canonical pending receipt lineage")
	}
	if err := collector.ObserveFinalizedBlock(101, map[uint32]FinalizedShardObservation{
		1: {Imports: []sharding.CrossShardReceipt{receipt}},
	}); err != nil {
		t.Fatal(err)
	}
	destination, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	lots, ok := collector.idleCapital.ObjectLots(destination.ID)
	if !ok || len(lots) != 1 || lots[0].IdleSinceHeight != 10 || lots[0].Amount != 990 {
		t.Fatalf("collector cross-shard import lost lineage: %#v", lots)
	}
	if len(collector.idleCapital.pendingTransfers) != 0 {
		t.Fatal("collector retained pending transfer after finalized import")
	}
}

func TestEpochCollectorIdleCapitalRequiresExportsForTrackedCrossShardTransfer(t *testing.T) {
	native := types.TokenID{1}
	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 100
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.EnableIdleCapitalTracking(NewIdleCapitalTracker()); err != nil {
		t.Fatal(err)
	}
	inputID := types.ObjectID{2}
	input := object.Object{
		ID: inputID, Version: 1, Owner: types.AccountID{3}, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 100, CreatedHeight: 5}.MarshalBinary(),
	}
	output, err := object.NewCoinOutputAtHeight(types.AccountID{4}, native, 90, 20)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		ShardID: 0, Inputs: []tx.InputRef{{ObjectID: inputID}}, Outputs: []object.OutputSpec{output},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}}, Fee: 10, Witnesses: []tx.Witness{{Object: input}},
	}
	result := execution.Result{
		Consumed: []types.ObjectID{inputID},
		Outbound: []execution.OutboundOutput{{DestinationShard: 1, OutputIndex: 0, Output: output}},
		TxID:     transaction.ID(),
	}
	if err := collector.ObserveFinalizedBlock(20, map[uint32]FinalizedShardObservation{
		0: {Transactions: []tx.Transaction{transaction}, Results: []execution.Result{result}},
	}); err == nil {
		t.Fatal("tracked cross-shard transfer without canonical export receipt was accepted")
	}
	if collector.LastHeight() != 0 || len(collector.idleCapital.objects) != 0 || len(collector.idleCapital.pendingTransfers) != 0 {
		t.Fatal("rejected missing-export observation partially mutated collector")
	}
}

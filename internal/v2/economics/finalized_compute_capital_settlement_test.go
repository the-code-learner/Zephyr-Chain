package economics

import (
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestFinalizedComputeSettlementResetsOnlyPaidEscrow(t *testing.T) {
	native := types.TokenID{1}
	record := finalizedComputeRecord(false)
	tracker := seededComputeComponentTracker(t, record)
	transaction, result, outputs := finalizedComputeSettlementFixture(t, native, record, tx.OpComputeFinalize, 60)

	applied, err := ApplyFinalizedComputeCapitalLineage(tracker, 60, native, transaction, result, nil)
	if err != nil || !applied {
		t.Fatalf("settlement lineage failed: applied=%v err=%v", applied, err)
	}
	escrowID, _ := ComputeEscrowComponentID(record.ID)
	if _, ok := tracker.ObjectLots(types.ObjectID(escrowID)); ok {
		t.Fatal("settled escrow component remained live")
	}
	for _, assignment := range record.Assignments {
		id, _ := ComputeCollateralComponentID(record.ID, assignment.Provider)
		if _, ok := tracker.ObjectLots(types.ObjectID(id)); ok {
			t.Fatalf("settled collateral component remained live for %s", assignment.Provider)
		}
	}

	ownerLots := mustTrackedLots(t, tracker, outputs[record.Job.Owner])
	if trackedLotsAtHeight(ownerLots, 60) != 0 || trackedLotsAtHeight(ownerLots, 5) != 30 {
		t.Fatalf("refund lineage was reset productive: %#v", ownerLots)
	}
	providerA := record.Assignments[0].Provider
	providerB := record.Assignments[1].Provider
	providerALots := mustTrackedLots(t, tracker, outputs[providerA])
	providerBLots := mustTrackedLots(t, tracker, outputs[providerB])
	if trackedLotsAtHeight(providerALots, 60) != 40 || trackedLotsAtHeight(providerALots, 6) != 10 {
		t.Fatalf("provider A payment/collateral lineage mixed incorrectly: %#v", providerALots)
	}
	if trackedLotsAtHeight(providerBLots, 60) != 30 || trackedLotsAtHeight(providerBLots, 7) != 10 {
		t.Fatalf("provider B payment/collateral lineage mixed incorrectly: %#v", providerBLots)
	}
	snapshot, err := tracker.Snapshot(60, []uint64{1, 100})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.TrackedCapital != 120 || snapshot.ProductiveObservedCapital != 100 || snapshot.ProductiveCoverageBps != 7_000 {
		t.Fatalf("unexpected productive settlement snapshot: %#v", snapshot)
	}
}

func TestFinalizedComputeExpiryPreservesEscrowAndCollateralAge(t *testing.T) {
	native := types.TokenID{1}
	record := finalizedComputeRecord(false)
	tracker := seededComputeComponentTracker(t, record)
	transaction, result, outputs := finalizedComputeSettlementFixture(t, native, record, tx.OpComputeExpire, 101)

	applied, err := ApplyFinalizedComputeCapitalLineage(tracker, 101, native, transaction, result, nil)
	if err != nil || !applied {
		t.Fatalf("expiry lineage failed: applied=%v err=%v", applied, err)
	}
	if trackedLotsAtHeight(mustTrackedLots(t, tracker, outputs[record.Job.Owner]), 5) != 100 {
		t.Fatal("expiry reset escrow age")
	}
	if trackedLotsAtHeight(mustTrackedLots(t, tracker, outputs[record.Assignments[0].Provider]), 6) != 10 ||
		trackedLotsAtHeight(mustTrackedLots(t, tracker, outputs[record.Assignments[1].Provider]), 7) != 10 {
		t.Fatal("expiry reset collateral age")
	}
	snapshot, err := tracker.Snapshot(101, []uint64{10, 200})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.ProductiveObservedCapital != 0 || snapshot.ProductiveCoverageBps != 0 {
		t.Fatalf("expired compute was classified productive: %#v", snapshot)
	}
}

func TestTrackedPendingComputeCoinMaterializesWithZeroCreatedHeight(t *testing.T) {
	native := types.TokenID{1}
	tracker := NewIdleCapitalTracker()
	source := types.ObjectID{90}
	tracker.objects[source] = []CapitalLot{{LineageID: types.Hash{91}, Amount: 10, IdleSinceHeight: 5}}
	output, err := object.NewCoinOutput(types.AccountID{8}, native, 10)
	if err != nil {
		t.Fatal(err)
	}
	receipt := sharding.CrossShardReceipt{
		SourceShard: 0, DestinationShard: 1, SourceHeight: 20,
		TransactionID: types.Hash{7}, OutputIndex: 3, Output: output, SourceStateRoot: types.Hash{9},
	}
	transferID, err := receipt.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if err := tracker.ApplyTransition([]types.ObjectID{source}, []CapitalTarget{{TransferID: transferID, Amount: 10}}, 0); err != nil {
		t.Fatal(err)
	}
	applied, err := ApplyFinalizedIdleCapitalImport(tracker, native, receipt)
	if err != nil || !applied {
		t.Fatalf("tracked zero-height compute receipt was not materialized: applied=%v err=%v", applied, err)
	}
	destination, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	assertTrackedAmountAndAge(t, tracker, destination.ID, 10, 5)
}

func seededComputeComponentTracker(t *testing.T, record compute.OnChainJob) *IdleCapitalTracker {
	t.Helper()
	tracker := NewIdleCapitalTracker()
	escrowID, err := ComputeEscrowComponentID(record.ID)
	if err != nil {
		t.Fatal(err)
	}
	tracker.objects[types.ObjectID(escrowID)] = []CapitalLot{{LineageID: types.Hash{21}, Amount: record.Escrow, IdleSinceHeight: 5}}
	for i, assignment := range record.Assignments {
		id, err := ComputeCollateralComponentID(record.ID, assignment.Provider)
		if err != nil {
			t.Fatal(err)
		}
		tracker.objects[types.ObjectID(id)] = []CapitalLot{{
			LineageID: types.Hash{byte(22 + i)}, Amount: record.Job.CollateralRequired, IdleSinceHeight: uint64(6 + i),
		}}
	}
	return tracker
}

func finalizedComputeSettlementFixture(
	t *testing.T,
	native types.TokenID,
	record compute.OnChainJob,
	kind uint16,
	height uint64,
) (tx.Transaction, execution.Result, map[types.AccountID]types.ObjectID) {
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
		ShardID: 0, Sender: record.Job.Owner,
		Operations: []tx.Operation{{Kind: kind, Payload: refRaw}},
		Witnesses: []tx.Witness{{Object: object.Object{
			ID: jobObjectID, Version: 4, Owner: record.Job.Owner, Kind: object.KindComputeJob, Data: raw,
		}}},
	}
	var settlement compute.OnChainSettlement
	if kind == tx.OpComputeExpire {
		_, refund, collateral, err := compute.ExpireOnChain(record, height)
		if err != nil {
			t.Fatal(err)
		}
		settlement = compute.OnChainSettlement{
			Settlement:        compute.Settlement{JobID: record.ID, Payments: map[types.AccountID]uint64{}, Refund: refund},
			CollateralReturns: collateral,
			SlashedCollateral: map[types.AccountID]uint64{},
		}
	} else {
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
		if err != nil {
			t.Fatal(err)
		}
	}
	receipt := compute.SettlementReceipt{
		JobID: record.ID, ResultRoot: settlement.ResultRoot, Payments: settlement.Payments,
		Refund: settlement.Refund, Slashed: settlement.SlashedCollateral, SlashReward: settlement.SlashReward,
		Expired: kind == tx.OpComputeExpire,
	}
	result := execution.Result{
		Consumed: []types.ObjectID{jobObjectID},
		Created: []object.Object{{
			ID:      types.ObjectIDForShard(transaction.ID(), computeSettlementReceiptOutputIndex, 0),
			Version: 1, Kind: object.KindSystem, Data: receipt.MarshalBinary(),
		}},
		TxID: transaction.ID(),
	}
	amounts := map[types.AccountID]uint64{}
	for provider, amount := range settlement.Payments {
		amounts[provider] += amount
	}
	for provider, amount := range settlement.CollateralReturns {
		amounts[provider] += amount
	}
	amounts[record.Job.Owner] += settlement.Refund + settlement.SlashReward
	accounts := make([]types.AccountID, 0, len(amounts))
	for account, amount := range amounts {
		if amount > 0 {
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].String() < accounts[j].String() })
	outputs := make(map[types.AccountID]types.ObjectID, len(accounts))
	for i, account := range accounts {
		spec, err := object.NewCoinOutput(account, native, amounts[account])
		if err != nil {
			t.Fatal(err)
		}
		index := computeSettlementReceiptOutputIndex + 1 + uint32(i)
		id := types.ObjectIDForShard(transaction.ID(), index, 0)
		outputs[account] = id
		result.Created = append(result.Created, object.Object{ID: id, Version: 1, Owner: account, Kind: spec.Kind, Data: spec.Data})
	}
	return transaction, result, outputs
}

func assertTrackedAmountAndAge(t *testing.T, tracker *IdleCapitalTracker, id types.ObjectID, amount, height uint64) {
	t.Helper()
	lots := mustTrackedLots(t, tracker, id)
	if idleCapitalAmountForTest(t, lots) != amount || trackedLotsAtHeight(lots, height) != amount {
		t.Fatalf("unexpected tracked lots for %s: %#v", id, lots)
	}
}

func mustTrackedLots(t *testing.T, tracker *IdleCapitalTracker, id types.ObjectID) []CapitalLot {
	t.Helper()
	lots, ok := tracker.ObjectLots(id)
	if !ok {
		t.Fatalf("object %s is not tracked", id)
	}
	return lots
}

func idleCapitalAmountForTest(t *testing.T, lots []CapitalLot) uint64 {
	t.Helper()
	amount, err := idleCapitalLotsAmount(lots)
	if err != nil {
		t.Fatal(err)
	}
	return amount
}

func trackedLotsAtHeight(lots []CapitalLot, height uint64) uint64 {
	var total uint64
	for _, lot := range lots {
		if lot.IdleSinceHeight == height {
			total += lot.Amount
		}
	}
	return total
}

func TestEpochCollectorAppliesFinalizedComputeCapitalLineage(t *testing.T) {
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

	record := finalizedComputeRecord(false)
	collector.idleCapital = seededComputeComponentTracker(t, record)
	transaction, result, outputs := finalizedComputeSettlementFixture(t, native, record, tx.OpComputeFinalize, 60)
	if err := collector.ObserveFinalizedBlock(60, map[uint32]FinalizedShardObservation{
		0: {Transactions: []tx.Transaction{transaction}, Results: []execution.Result{result}},
	}); err != nil {
		t.Fatal(err)
	}

	provider := record.Assignments[0].Provider
	lots := mustTrackedLots(t, collector.idleCapital, outputs[provider])
	if trackedLotsAtHeight(lots, 60) != record.Assignments[0].Price ||
		trackedLotsAtHeight(lots, 6) != record.Job.CollateralRequired {
		t.Fatalf("collector mixed productive payment with collateral lineage: %#v", lots)
	}
	snapshot, enabled, err := collector.IdleCapitalSnapshot(60, []uint64{1, 100})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || snapshot.ProductiveObservedCapital != record.Escrow || snapshot.ProductiveCoverageBps != 7_000 {
		t.Fatalf("collector did not apply compute productive coverage: %#v enabled=%v", snapshot, enabled)
	}
}

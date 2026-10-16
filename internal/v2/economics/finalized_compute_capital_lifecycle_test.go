package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestFinalizedComputeCapitalRoutesJobOfferAcceptAndAssignmentIngest(t *testing.T) {
	native := types.TokenID{1}
	record := finalizedComputeRecord(false)
	job := record.Job
	jobRaw, err := job.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	tracker := NewIdleCapitalTracker()

	ownerInputID := types.ObjectID{40}
	ownerInput := object.Object{
		ID: ownerInputID, Version: 1, Owner: job.Owner, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 110, CreatedHeight: 5}.MarshalBinary(),
	}
	change, err := object.NewCoinOutput(job.Owner, native, 9)
	if err != nil {
		t.Fatal(err)
	}
	jobTx := tx.Transaction{
		ShardID: 0, Sender: job.Owner,
		Outputs:    []object.OutputSpec{change},
		Operations: []tx.Operation{{Kind: tx.OpComputeJob, Payload: jobRaw}},
		Fee:        1,
		Witnesses:  []tx.Witness{{Object: ownerInput}},
	}
	jobObject, createdRecord, err := compute.NewJobObject(jobTx.ID(), 0, job, job.MaxPrice)
	if err != nil {
		t.Fatal(err)
	}
	jobChangeID := types.ObjectIDForShard(jobTx.ID(), 0, 0)
	jobResult := execution.Result{
		Consumed: []types.ObjectID{ownerInputID},
		Created: []object.Object{
			{ID: jobChangeID, Version: 1, Owner: change.Owner, Kind: change.Kind, Data: change.Data},
			jobObject,
		},
		TxID: jobTx.ID(),
	}
	applied, err := ApplyFinalizedComputeCapitalLineage(tracker, 10, native, jobTx, jobResult, nil)
	if err != nil || !applied {
		t.Fatalf("job capital lineage failed: applied=%v err=%v", applied, err)
	}
	escrowID, err := ComputeEscrowComponentID(createdRecord.ID)
	if err != nil {
		t.Fatal(err)
	}
	assertTrackedAmountAndAge(t, tracker, types.ObjectID(escrowID), 100, 5)
	assertTrackedAmountAndAge(t, tracker, jobChangeID, 9, 5)

	provider := record.Assignments[0].Provider
	offer := compute.Offer{
		Provider: provider, Resources: job.Resources, PricePerUnit: record.Assignments[0].Price,
		Collateral: 10, Verification: []compute.VerificationMode{compute.VerificationReplicated}, ValidUntilHeight: 90,
	}
	offerRaw, err := offer.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	providerInputID := types.ObjectID{41}
	providerInput := object.Object{
		ID: providerInputID, Version: 1, Owner: provider, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 11, CreatedHeight: 6}.MarshalBinary(),
	}
	offerTx := tx.Transaction{
		ShardID: 0, Sender: provider,
		Operations: []tx.Operation{{Kind: tx.OpComputeOffer, Payload: offerRaw}}, Fee: 1,
		Witnesses: []tx.Witness{{Object: providerInput}},
	}
	offerObject, err := compute.NewOfferObject(offerTx.ID(), 0, offer)
	if err != nil {
		t.Fatal(err)
	}
	offerResult := execution.Result{Consumed: []types.ObjectID{providerInputID}, Created: []object.Object{offerObject}, TxID: offerTx.ID()}
	applied, err = ApplyFinalizedComputeCapitalLineage(tracker, 11, native, offerTx, offerResult, nil)
	if err != nil || !applied {
		t.Fatalf("offer capital lineage failed: applied=%v err=%v", applied, err)
	}
	assertTrackedAmountAndAge(t, tracker, offerObject.ID, 10, 6)

	message := compute.AssignmentMessage{
		JobID: createdRecord.ID, JobOwner: job.Owner, JobShard: 0,
		OfferID: types.Hash(offerObject.ID), Offer: offer, Job: job,
	}
	messageRaw, err := message.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	feeInputID := types.ObjectID{42}
	feeInput := object.Object{
		ID: feeInputID, Version: 1, Owner: provider, Kind: object.KindCoin,
		Data: object.Coin{Token: native, Amount: 1, CreatedHeight: 7}.MarshalBinary(),
	}
	acceptTx := tx.Transaction{
		ShardID: 0, Sender: provider,
		Operations: []tx.Operation{{Kind: tx.OpComputeAccept, Payload: messageRaw}}, Fee: 1,
		Witnesses: []tx.Witness{{Object: offerObject}, {Object: feeInput}},
	}
	assignmentSpec, err := message.Output()
	if err != nil {
		t.Fatal(err)
	}
	assignmentObject := object.Object{
		ID: types.ObjectIDForShard(acceptTx.ID(), computeSettlementReceiptOutputIndex, 0), Version: 1,
		Owner: assignmentSpec.Owner, Kind: assignmentSpec.Kind, Data: assignmentSpec.Data,
	}
	acceptResult := execution.Result{
		Consumed: []types.ObjectID{offerObject.ID, feeInputID}, Created: []object.Object{assignmentObject}, TxID: acceptTx.ID(),
	}
	applied, err = ApplyFinalizedComputeCapitalLineage(tracker, 12, native, acceptTx, acceptResult, nil)
	if err != nil || !applied {
		t.Fatalf("accept capital lineage failed: applied=%v err=%v", applied, err)
	}
	if _, ok := tracker.ObjectLots(offerObject.ID); ok {
		t.Fatal("consumed offer collateral remained tracked")
	}
	collateralID, err := ComputeCollateralComponentID(createdRecord.ID, provider)
	if err != nil {
		t.Fatal(err)
	}
	assertTrackedAmountAndAge(t, tracker, types.ObjectID(collateralID), 10, 6)

	updatedRecord, _, _, err := compute.AssignOnChain(createdRecord, message.OfferID, offer, 13)
	if err != nil {
		t.Fatal(err)
	}
	updatedRaw, err := updatedRecord.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	refRaw, err := (compute.IngestRef{JobObject: jobObject.ID, MessageObject: assignmentObject.ID}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	ingestTx := tx.Transaction{
		ShardID: 0, Sender: job.Owner,
		Operations: []tx.Operation{{Kind: tx.OpComputeIngestAssignment, Payload: refRaw}},
		Witnesses:  []tx.Witness{{Object: jobObject}, {Object: assignmentObject}},
	}
	updatedJobObject := jobObject
	updatedJobObject.Version++
	updatedJobObject.Data = updatedRaw
	ingestResult := execution.Result{
		Consumed: []types.ObjectID{jobObject.ID, assignmentObject.ID}, Created: []object.Object{updatedJobObject}, TxID: ingestTx.ID(),
	}
	if _, err := ApplyFinalizedComputeCapitalLineage(tracker, 13, native, ingestTx, ingestResult, nil); err != nil {
		t.Fatal(err)
	}
	assertTrackedAmountAndAge(t, tracker, types.ObjectID(escrowID), 100, 5)
	assertTrackedAmountAndAge(t, tracker, types.ObjectID(collateralID), 10, 6)
}

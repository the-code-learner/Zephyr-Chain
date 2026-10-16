package economics

import (
	"bytes"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const computeSettlementReceiptOutputIndex uint32 = 0xC0000000

type computeSettlementLegKind uint8

const (
	computeSettlementLegUnknown computeSettlementLegKind = iota
	computeSettlementLegPayment
	computeSettlementLegRefund
	computeSettlementLegCollateralReturn
	computeSettlementLegCollateralSlash
	computeSettlementLegOfferExcess
)

// ApplyFinalizedComputeCapitalLineage routes prospective idle-capital lineage
// through finalized compute-market locks and settlements. It keeps owner escrow
// and provider collateral in distinct deterministic shadow objects and only
// resets the paid escrow portion after a canonical finalized settlement.
//
// Compute-job/result object version changes do not move native capital. Their
// stable shadow component identities therefore survive those updates unchanged.
func ApplyFinalizedComputeCapitalLineage(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	if tracker == nil || height == 0 || types.IsZero32([32]byte(nativeToken)) || result.TxID != transaction.ID() {
		return false, ErrFinalizedEconomics
	}
	if len(transaction.Operations) != 1 {
		return false, nil
	}

	preview := tracker.Clone()
	if preview == nil {
		return false, ErrFinalizedEconomics
	}
	var applied bool
	var err error
	switch transaction.Operations[0].Kind {
	case tx.OpComputeOffer:
		applied, err = applyFinalizedComputeOfferCapital(preview, height, nativeToken, transaction, result, exports)
	case tx.OpComputeJob:
		applied, err = applyFinalizedComputeJobCapital(preview, height, nativeToken, transaction, result, exports)
	case tx.OpComputeAccept:
		applied, err = applyFinalizedComputeAcceptCapital(preview, height, nativeToken, transaction, result, exports)
	case tx.OpComputeIngestAssignment:
		applied, err = applyFinalizedComputeAssignmentIngestCapital(preview, height, nativeToken, transaction, result, exports)
	case tx.OpComputeResult, tx.OpComputeIngestResult:
		applied, err = applyFinalizedComputeSideCapital(preview, height, nativeToken, transaction, result, exports)
	case tx.OpComputeFinalize, tx.OpComputeResolveReplicated, tx.OpComputeExpire:
		applied, err = applyFinalizedComputeSettlementCapital(preview, height, nativeToken, transaction, result, exports)
	default:
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !applied {
		return false, nil
	}
	if err := preview.validate(); err != nil {
		return false, err
	}
	*tracker = *preview
	return true, nil
}

func applyFinalizedComputeOfferCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	offer, err := compute.ParseOffer(transaction.Operations[0].Payload)
	if err != nil || offer.Provider != transaction.Sender {
		return false, ErrFinalizedEconomics
	}
	offerObject, ok, err := uniqueCreatedObjectOfKind(result.Created, object.KindComputeOffer)
	if err != nil || !ok || offerObject.Owner != offer.Provider {
		return false, ErrFinalizedEconomics
	}
	parsed, err := compute.ParseOffer(offerObject.Data)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	left, _ := offer.MarshalBinary()
	right, _ := parsed.MarshalBinary()
	if !bytes.Equal(left, right) {
		return false, ErrFinalizedEconomics
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	if offer.Collateral > 0 {
		targets = append(targets, CapitalTarget{ObjectID: offerObject.ID, Amount: offer.Collateral})
	}
	return applyFinalizedNativeInputsTransition(tracker, nativeToken, transaction, result, targets, transaction.Fee)
}

func applyFinalizedComputeJobCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	job, err := compute.ParseJob(transaction.Operations[0].Payload)
	if err != nil || job.Owner != transaction.Sender {
		return false, ErrFinalizedEconomics
	}
	jobObject, ok, err := uniqueCreatedObjectOfKind(result.Created, object.KindComputeJob)
	if err != nil || !ok || jobObject.Owner != job.Owner {
		return false, ErrFinalizedEconomics
	}
	record, err := compute.ParseOnChainJob(jobObject.Data)
	if err != nil || record.Job.Owner != job.Owner || record.Escrow != job.MaxPrice || len(record.Assignments) != 0 {
		return false, ErrFinalizedEconomics
	}
	left, _ := job.MarshalBinary()
	right, _ := record.Job.MarshalBinary()
	if !bytes.Equal(left, right) {
		return false, ErrFinalizedEconomics
	}
	escrowID, err := ComputeEscrowComponentID(record.ID)
	if err != nil {
		return false, err
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	targets = append(targets, CapitalTarget{ObjectID: types.ObjectID(escrowID), Amount: record.Escrow})
	return applyFinalizedNativeInputsTransition(tracker, nativeToken, transaction, result, targets, transaction.Fee)
}

func applyFinalizedComputeAcceptCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	message, err := compute.ParseAssignmentMessage(transaction.Operations[0].Payload)
	if err != nil || message.Offer.Provider != transaction.Sender {
		return false, ErrFinalizedEconomics
	}
	offerWitness, ok := uniqueWitnessOfKind(transaction, object.KindComputeOffer)
	if !ok || offerWitness.Object.Owner != transaction.Sender || types.Hash(offerWitness.Object.ID) != message.OfferID {
		return false, ErrFinalizedEconomics
	}
	offer, err := compute.ParseOffer(offerWitness.Object.Data)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	left, _ := offer.MarshalBinary()
	right, _ := message.Offer.MarshalBinary()
	if !bytes.Equal(left, right) || finalizedConsumedCount(result.Consumed, offerWitness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	if !finalizedComputeMessageCreated(transaction, result, exports, object.KindComputeAssignment, message.JobShard, message.Output) {
		return false, ErrFinalizedEconomics
	}

	excess := message.Offer.Collateral - message.Job.CollateralRequired
	var excessID types.ObjectID
	if message.Offer.Collateral > 0 {
		offerLots, tracked := tracker.ObjectLots(offerWitness.Object.ID)
		if !tracked {
			if finalizedNativeInputsContainTracked(tracker, nativeToken, transaction, result) {
				return false, ErrFinalizedEconomics
			}
			return false, nil
		}
		amount, err := idleCapitalLotsAmount(offerLots)
		if err != nil || amount != message.Offer.Collateral {
			return false, ErrFinalizedEconomics
		}
		lockTargets := make([]CapitalTarget, 0, 2)
		if message.Job.CollateralRequired > 0 {
			collateralID, err := ComputeCollateralComponentID(message.JobID, message.Offer.Provider)
			if err != nil {
				return false, err
			}
			lockTargets = append(lockTargets, CapitalTarget{ObjectID: types.ObjectID(collateralID), Amount: message.Job.CollateralRequired})
		}
		if excess > 0 {
			excessID, err = computeSettlementLegObjectID(message.JobID, computeSettlementLegOfferExcess, message.Offer.Provider, offerWitness.Object.ID[:])
			if err != nil {
				return false, err
			}
			lockTargets = append(lockTargets, CapitalTarget{ObjectID: excessID, Amount: excess})
		}
		if len(lockTargets) == 0 {
			return false, ErrFinalizedEconomics
		}
		if err := tracker.ApplyTransition([]types.ObjectID{offerWitness.Object.ID}, lockTargets, 0); err != nil {
			return false, err
		}
	}

	ordinaryTargets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	nativeInputs, existingTracked, unknownAge, err := prepareFinalizedNativeInputs(tracker, nativeToken, transaction, result)
	if err != nil {
		return false, err
	}
	if unknownAge {
		if existingTracked || excess > 0 {
			return false, ErrFinalizedEconomics
		}
		return true, nil
	}
	inputs := nativeInputs
	if excess > 0 {
		inputs = append(inputs, excessID)
	}
	if len(inputs) == 0 {
		if len(ordinaryTargets) != 0 || transaction.Fee != 0 {
			return false, ErrFinalizedEconomics
		}
		return true, nil
	}
	if err := tracker.ApplyTransition(inputs, ordinaryTargets, transaction.Fee); err != nil {
		return false, err
	}
	return true, nil
}

func applyFinalizedComputeAssignmentIngestCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	applied, err := applyFinalizedComputeSideCapital(tracker, height, nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	ref, err := compute.ParseIngestRef(transaction.Operations[0].Payload)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	jobWitness, ok := witnessByObjectID(transaction, ref.JobObject)
	if !ok || jobWitness.Object.Kind != object.KindComputeJob {
		return false, ErrFinalizedEconomics
	}
	messageWitness, ok := witnessByObjectID(transaction, ref.MessageObject)
	if !ok || messageWitness.Object.Kind != object.KindComputeAssignment {
		return false, ErrFinalizedEconomics
	}
	record, err := compute.ParseOnChainJob(jobWitness.Object.Data)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	message, err := compute.ParseAssignmentMessage(messageWitness.Object.Data)
	if err != nil || message.ValidateForRecord(record) != nil {
		return false, ErrFinalizedEconomics
	}
	escrowID, err := ComputeEscrowComponentID(record.ID)
	if err != nil {
		return false, err
	}
	if lots, ok := tracker.ObjectLots(types.ObjectID(escrowID)); ok {
		amount, err := idleCapitalLotsAmount(lots)
		if err != nil || amount != record.Escrow {
			return false, ErrFinalizedEconomics
		}
	}
	collateralID, err := ComputeCollateralComponentID(record.ID, message.Offer.Provider)
	if err != nil {
		return false, err
	}
	if lots, ok := tracker.ObjectLots(types.ObjectID(collateralID)); ok {
		amount, err := idleCapitalLotsAmount(lots)
		if err != nil || amount != record.Job.CollateralRequired {
			return false, ErrFinalizedEconomics
		}
	}
	return applied, nil
}

func applyFinalizedComputeSideCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	_ = height
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	return applyFinalizedNativeInputsTransition(tracker, nativeToken, transaction, result, targets, transaction.Fee)
}

func applyFinalizedComputeSettlementCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	sideApplied, err := applyFinalizedComputeSideCapital(tracker, height, nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	record, settlement, evidence, productive, err := finalizedComputeSettlement(height, transaction, result)
	if err != nil {
		return false, err
	}
	components, err := DeriveComputeCapitalComponents(record)
	if err != nil {
		return false, err
	}
	componentIDs := make([]types.ObjectID, 0, 1+len(components.Collateral))
	componentIDs = append(componentIDs, types.ObjectID(components.Escrow.ID))
	for _, component := range components.Collateral {
		componentIDs = append(componentIDs, types.ObjectID(component.ID))
	}
	trackedCount := 0
	for i, id := range componentIDs {
		lots, ok := tracker.ObjectLots(id)
		if !ok {
			continue
		}
		trackedCount++
		expected := components.Escrow.Amount
		if i > 0 {
			expected = components.Collateral[i-1].Amount
		}
		amount, err := idleCapitalLotsAmount(lots)
		if err != nil || amount != expected {
			return false, ErrFinalizedEconomics
		}
	}
	if trackedCount == 0 {
		return sideApplied, nil
	}
	if trackedCount != len(componentIDs) {
		return false, ErrFinalizedEconomics
	}

	outputTargets, err := finalizedComputeSettlementTargets(nativeToken, transaction, result, exports, record, settlement)
	if err != nil {
		return false, err
	}
	legsByAccount := make(map[types.AccountID][]types.ObjectID)
	legLots := make(map[types.ObjectID][]CapitalLot)

	escrowObjectID := types.ObjectID(components.Escrow.ID)
	escrowLots, _ := tracker.ObjectLots(escrowObjectID)
	if productive {
		productiveLots, idleLots, err := splitProductiveCapitalAmount(escrowLots, height, evidence.Paid)
		if err != nil {
			return false, err
		}
		paymentProviders := sortedPositiveAccountAmounts(settlement.Payments)
		amounts := make([]uint64, len(paymentProviders))
		for i, provider := range paymentProviders {
			amounts[i] = settlement.Payments[provider]
		}
		paymentLots, err := SplitCapitalLineage(productiveLots, amounts)
		if err != nil {
			return false, err
		}
		for i, provider := range paymentProviders {
			legID, err := computeSettlementLegObjectID(record.ID, computeSettlementLegPayment, provider, nil)
			if err != nil {
				return false, err
			}
			legLots[legID] = paymentLots[i]
			legsByAccount[provider] = append(legsByAccount[provider], legID)
		}
		if settlement.Refund > 0 {
			if amount, err := idleCapitalLotsAmount(idleLots); err != nil || amount != settlement.Refund {
				return false, ErrFinalizedEconomics
			}
			legID, err := computeSettlementLegObjectID(record.ID, computeSettlementLegRefund, record.Job.Owner, nil)
			if err != nil {
				return false, err
			}
			legLots[legID] = idleLots
			legsByAccount[record.Job.Owner] = append(legsByAccount[record.Job.Owner], legID)
		} else if len(idleLots) != 0 {
			return false, ErrFinalizedEconomics
		}
		if err := tracker.productive.Observe(record.Escrow, evidence.CoverageBps); err != nil {
			return false, err
		}
	} else {
		if settlement.Refund != record.Escrow {
			return false, ErrFinalizedEconomics
		}
		legID, err := computeSettlementLegObjectID(record.ID, computeSettlementLegRefund, record.Job.Owner, nil)
		if err != nil {
			return false, err
		}
		legLots[legID] = escrowLots
		legsByAccount[record.Job.Owner] = append(legsByAccount[record.Job.Owner], legID)
	}

	for _, component := range components.Collateral {
		componentID := types.ObjectID(component.ID)
		lots, _ := tracker.ObjectLots(componentID)
		returned := settlement.CollateralReturns[component.Provider]
		slashed := settlement.SlashedCollateral[component.Provider]
		if returned > 0 && slashed > 0 || returned+slashed != component.Amount {
			return false, ErrFinalizedEconomics
		}
		kind := computeSettlementLegCollateralReturn
		recipient := component.Provider
		amount := returned
		if slashed > 0 {
			kind = computeSettlementLegCollateralSlash
			recipient = record.Job.Owner
			amount = slashed
		}
		if amount != component.Amount {
			return false, ErrFinalizedEconomics
		}
		legID, err := computeSettlementLegObjectID(record.ID, kind, component.Provider, nil)
		if err != nil {
			return false, err
		}
		legLots[legID] = lots
		legsByAccount[recipient] = append(legsByAccount[recipient], legID)
	}

	if err := replaceComputeComponentsWithSettlementLegs(tracker, componentIDs, legLots); err != nil {
		return false, err
	}
	accounts := make([]types.AccountID, 0, len(outputTargets))
	for account := range outputTargets {
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return bytes.Compare(accounts[i][:], accounts[j][:]) < 0 })
	for _, account := range accounts {
		inputs := legsByAccount[account]
		if len(inputs) == 0 {
			return false, ErrFinalizedEconomics
		}
		if err := tracker.ApplyTransition(inputs, []CapitalTarget{outputTargets[account]}, 0); err != nil {
			return false, err
		}
	}
	return true, nil
}

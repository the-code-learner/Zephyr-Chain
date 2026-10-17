package economics

import (
	"bytes"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ApplyFinalizedCrossLendingCapitalLineage follows native capital through the
// explicit cross-shard lending protocol. Message value is never inferred from
// an RPC assertion: it is checked against the finalized operation, message
// payload and canonical CrossShardReceipt.
func ApplyFinalizedCrossLendingCapitalLineage(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	if tracker == nil || height == 0 || result.TxID != transaction.ID() || len(transaction.Operations) != 1 {
		return false, ErrFinalizedEconomics
	}
	op := transaction.Operations[0]
	switch op.Kind {
	case tx.OpCrossLoanOffer:
		return applyCrossLoanOfferCapital(tracker, height, nativeToken, transaction, result, exports)
	case tx.OpCrossLoanAccept:
		return applyCrossLoanAcceptCapital(tracker, height, nativeToken, transaction, result, exports)
	case tx.OpCrossLoanRepay:
		return applyCrossLoanRepayCapital(tracker, height, nativeToken, transaction, result, exports)
	case tx.OpCrossLoanSettle:
		return applyCrossLoanSettleCapital(tracker, height, nativeToken, transaction, result, exports)
	case tx.OpCrossLoanDefault:
		return applyCrossLoanDefaultCapital(tracker, height, nativeToken, transaction, result, exports)
	case tx.OpCrossLoanExpire:
		return applyCrossLoanExpireCapital(tracker, height, nativeToken, transaction, result, exports)
	default:
		return false, nil
	}
}

type lendingValueInput struct {
	id     types.ObjectID
	amount uint64
}

func applyCrossLoanOfferCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	terms, err := lending.ParseTerms(transaction.Operations[0].Payload)
	if err != nil || terms.Lender != transaction.Sender {
		return false, ErrFinalizedEconomics
	}
	borrowerShard, ok := uniqueExportDestination(result, lending.CrossOfferMessageIndex)
	if !ok || borrowerShard == transaction.ShardID {
		return false, ErrFinalizedEconomics
	}
	offerID := types.ObjectIDForShard(transaction.ID(), lending.CrossOfferMessageIndex, borrowerShard)
	message := lending.CrossOffer{
		OfferObject: offerID, LenderShard: transaction.ShardID, BorrowerShard: borrowerShard,
		OfferedHeight: height, Terms: terms,
	}
	raw, err := message.MarshalBinary()
	if err != nil {
		return false, err
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	target, err := finalizedLendingMessageTarget(transaction, result, exports, borrowerShard, lending.CrossOfferMessageIndex,
		object.OutputSpec{Owner: terms.Borrower, Kind: object.KindCrossLoanOffer, Data: raw}, terms.Principal)
	if err != nil {
		return false, err
	}
	targets = append(targets, target)
	return applyLendingValueTransition(tracker, nativeToken, transaction, result, nil, targets, transaction.Fee)
}

func applyCrossLoanAcceptCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	ref, err := lending.ParseRef(transaction.Operations[0].Payload)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	witness, ok := witnessByObjectID(transaction, ref.ObjectID)
	if !ok || witness.Object.Kind != object.KindCrossLoanOffer || finalizedConsumedCount(result.Consumed, witness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	offer, err := lending.ParseCrossOffer(witness.Object.Data)
	if err != nil || offer.OfferObject != witness.Object.ID || offer.Terms.Borrower != transaction.Sender ||
		offer.BorrowerShard != transaction.ShardID || height <= offer.OfferedHeight {
		return false, ErrFinalizedEconomics
	}
	activationID := types.ObjectIDForShard(transaction.ID(), lending.CrossActivationObjectIndex, offer.BorrowerShard)
	acceptance := lending.CrossAcceptance{
		OfferObject: offer.OfferObject, ActivationObject: activationID,
		LenderShard: offer.LenderShard, BorrowerShard: offer.BorrowerShard,
		AcceptedHeight: height, Terms: offer.Terms,
	}
	raw, err := acceptance.MarshalBinary()
	if err != nil {
		return false, err
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	principalSpec, err := object.NewCoinOutputAtHeight(offer.Terms.Borrower, nativeToken, offer.Terms.Principal, height)
	if err != nil {
		return false, err
	}
	principalTarget, err := finalizedCapitalTargetForOutput(transaction, result, exports, lending.CrossPrincipalObjectIndex, principalSpec)
	if err != nil {
		return false, err
	}
	targets = append(targets, principalTarget)
	if offer.Terms.Collateral > 0 {
		acceptanceTarget, err := finalizedLendingMessageTarget(transaction, result, exports, offer.LenderShard,
			lending.CrossAcceptanceMessageIndex,
			object.OutputSpec{Owner: offer.Terms.Lender, Kind: object.KindCrossLoanAcceptance, Data: raw},
			offer.Terms.Collateral)
		if err != nil {
			return false, err
		}
		targets = append(targets, acceptanceTarget)
	}
	return applyLendingValueTransition(
		tracker, nativeToken, transaction, result,
		[]lendingValueInput{{id: witness.Object.ID, amount: offer.Terms.Principal}},
		targets, transaction.Fee,
	)
}

func applyCrossLoanRepayCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	ref, err := lending.ParseRef(transaction.Operations[0].Payload)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	witness, ok := witnessByObjectID(transaction, ref.ObjectID)
	if !ok || witness.Object.Kind != object.KindCrossLoanActivation || finalizedConsumedCount(result.Consumed, witness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	activation, err := lending.ParseCrossActivation(witness.Object.Data)
	if err != nil || activation.Terms.Borrower != transaction.Sender || activation.BorrowerShard != transaction.ShardID {
		return false, ErrFinalizedEconomics
	}
	due, err := activation.Terms.RepaymentDue()
	if err != nil {
		return false, err
	}
	termsHash, err := activation.Terms.Hash()
	if err != nil {
		return false, err
	}
	message := lending.CrossRepayment{
		AcceptanceObject: activation.AcceptanceObject, ActivationObject: witness.Object.ID,
		LenderShard: activation.LenderShard, BorrowerShard: activation.BorrowerShard,
		Amount: due, PaidHeight: height, TermsHash: termsHash,
	}
	raw, err := message.MarshalBinary()
	if err != nil {
		return false, err
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	repaymentTarget, err := finalizedLendingMessageTarget(transaction, result, exports, activation.LenderShard,
		lending.CrossRepaymentMessageIndex,
		object.OutputSpec{Owner: activation.Terms.Lender, Kind: object.KindCrossLoanRepayment, Data: raw}, due)
	if err != nil {
		return false, err
	}
	targets = append(targets, repaymentTarget)
	return applyLendingValueTransition(tracker, nativeToken, transaction, result, nil, targets, transaction.Fee)
}

func applyCrossLoanSettleCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	acceptanceWitness, ok := uniqueWitnessOfKind(transaction, object.KindCrossLoanAcceptance)
	if !ok || finalizedConsumedCount(result.Consumed, acceptanceWitness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	repaymentWitness, ok := uniqueWitnessOfKind(transaction, object.KindCrossLoanRepayment)
	if !ok || finalizedConsumedCount(result.Consumed, repaymentWitness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	acceptance, err := lending.ParseCrossAcceptance(acceptanceWitness.Object.Data)
	if err != nil || acceptance.LenderShard != transaction.ShardID {
		return false, ErrFinalizedEconomics
	}
	repayment, err := lending.ParseCrossRepayment(repaymentWitness.Object.Data)
	if err != nil || repayment.AcceptanceObject != acceptanceWitness.Object.ID ||
		repayment.ActivationObject != acceptance.ActivationObject {
		return false, ErrFinalizedEconomics
	}
	due, err := acceptance.Terms.RepaymentDue()
	if err != nil || repayment.Amount != due {
		return false, ErrFinalizedEconomics
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	lenderSpec, err := object.NewCoinOutputAtHeight(acceptance.Terms.Lender, nativeToken, due, height)
	if err != nil {
		return false, err
	}
	lenderTarget, err := finalizedCapitalTargetForOutput(transaction, result, exports, lending.PayoutObjectIndex, lenderSpec)
	if err != nil {
		return false, err
	}
	targets = append(targets, lenderTarget)
	if acceptance.Terms.Collateral > 0 {
		collateralSpec, err := object.NewCoinOutputAtHeight(acceptance.Terms.Borrower, nativeToken, acceptance.Terms.Collateral, height)
		if err != nil {
			return false, err
		}
		collateralTarget, err := finalizedCapitalTargetForOutput(transaction, result, exports, lending.CrossCollateralReturnIndex, collateralSpec)
		if err != nil {
			return false, err
		}
		targets = append(targets, collateralTarget)
	}
	special := []lendingValueInput{{id: repaymentWitness.Object.ID, amount: due}}
	if acceptance.Terms.Collateral > 0 {
		special = append(special, lendingValueInput{id: acceptanceWitness.Object.ID, amount: acceptance.Terms.Collateral})
	}
	return applyLendingValueTransition(tracker, nativeToken, transaction, result, special, targets, transaction.Fee)
}

func applyCrossLoanDefaultCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	ref, err := lending.ParseRef(transaction.Operations[0].Payload)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	witness, ok := witnessByObjectID(transaction, ref.ObjectID)
	if !ok || witness.Object.Kind != object.KindCrossLoanAcceptance || finalizedConsumedCount(result.Consumed, witness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	acceptance, err := lending.ParseCrossAcceptance(witness.Object.Data)
	if err != nil || acceptance.Terms.Lender != transaction.Sender {
		return false, ErrFinalizedEconomics
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	special := []lendingValueInput{}
	if acceptance.Terms.Collateral > 0 {
		spec, err := object.NewCoinOutputAtHeight(acceptance.Terms.Lender, nativeToken, acceptance.Terms.Collateral, height)
		if err != nil {
			return false, err
		}
		target, err := finalizedCapitalTargetForOutput(transaction, result, exports, lending.PayoutObjectIndex, spec)
		if err != nil {
			return false, err
		}
		targets = append(targets, target)
		special = append(special, lendingValueInput{id: witness.Object.ID, amount: acceptance.Terms.Collateral})
	}
	return applyLendingValueTransition(tracker, nativeToken, transaction, result, special, targets, transaction.Fee)
}

func applyCrossLoanExpireCapital(
	tracker *IdleCapitalTracker,
	height uint64,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) (bool, error) {
	ref, err := lending.ParseRef(transaction.Operations[0].Payload)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	witness, ok := witnessByObjectID(transaction, ref.ObjectID)
	if !ok || witness.Object.Kind != object.KindCrossLoanOffer || finalizedConsumedCount(result.Consumed, witness.Object.ID) != 1 {
		return false, ErrFinalizedEconomics
	}
	offer, err := lending.ParseCrossOffer(witness.Object.Data)
	if err != nil {
		return false, ErrFinalizedEconomics
	}
	targets, err := finalizedComputeOrdinaryTargets(nativeToken, transaction, result, exports)
	if err != nil {
		return false, err
	}
	spec, err := object.NewCoinOutputAtHeight(offer.Terms.Lender, nativeToken, offer.Terms.Principal, height)
	if err != nil {
		return false, err
	}
	target, err := finalizedCapitalTargetForOutput(transaction, result, exports, lending.CrossPrincipalReturnIndex, spec)
	if err != nil {
		return false, err
	}
	targets = append(targets, target)
	return applyLendingValueTransition(
		tracker, nativeToken, transaction, result,
		[]lendingValueInput{{id: witness.Object.ID, amount: offer.Terms.Principal}},
		targets, transaction.Fee,
	)
}

func applyLendingValueTransition(
	tracker *IdleCapitalTracker,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	special []lendingValueInput,
	targets []CapitalTarget,
	burned uint64,
) (bool, error) {
	preview := tracker.Clone()
	if preview == nil {
		return false, ErrFinalizedEconomics
	}
	inputs, existingTracked, unknownAge, err := prepareFinalizedNativeInputs(preview, nativeToken, transaction, result)
	if err != nil {
		return false, err
	}
	for _, value := range special {
		if value.amount == 0 || types.IsZero32([32]byte(value.id)) {
			return false, ErrFinalizedEconomics
		}
		lots, ok := preview.ObjectLots(value.id)
		if !ok {
			return false, ErrFinalizedEconomics
		}
		amount, err := idleCapitalLotsAmount(lots)
		if err != nil || amount != value.amount {
			return false, ErrFinalizedEconomics
		}
		inputs = append(inputs, value.id)
		existingTracked = true
	}
	if unknownAge {
		if existingTracked {
			return false, ErrFinalizedEconomics
		}
		return false, nil
	}
	if len(inputs) == 0 {
		return false, nil
	}
	if err := preview.ApplyTransition(inputs, targets, burned); err != nil {
		return false, err
	}
	*tracker = *preview
	return true, nil
}

func finalizedLendingMessageTarget(
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
	destinationShard uint32,
	outputIndex uint32,
	spec object.OutputSpec,
	amount uint64,
) (CapitalTarget, error) {
	if amount == 0 || destinationShard == transaction.ShardID {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	var outbound *execution.OutboundOutput
	for i := range result.Outbound {
		item := &result.Outbound[i]
		if item.DestinationShard == destinationShard && item.OutputIndex == outputIndex &&
			bytes.Equal(item.Output.CanonicalBytes(), spec.CanonicalBytes()) {
			if outbound != nil {
				return CapitalTarget{}, ErrFinalizedEconomics
			}
			outbound = item
		}
	}
	if outbound == nil {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	var receipt *sharding.CrossShardReceipt
	for i := range exports {
		item := &exports[i]
		if item.SourceShard == transaction.ShardID && item.DestinationShard == destinationShard &&
			item.TransactionID == result.TxID && item.OutputIndex == outputIndex &&
			bytes.Equal(item.Output.CanonicalBytes(), spec.CanonicalBytes()) && item.Validate() == nil {
			if receipt != nil {
				return CapitalTarget{}, ErrFinalizedEconomics
			}
			receipt = item
		}
	}
	if receipt == nil {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	transferID, err := receipt.Hash()
	if err != nil {
		return CapitalTarget{}, err
	}
	return CapitalTarget{TransferID: transferID, Amount: amount}, nil
}

func uniqueExportDestination(result execution.Result, outputIndex uint32) (uint32, bool) {
	var destination uint32
	found := false
	for _, outbound := range result.Outbound {
		if outbound.OutputIndex != outputIndex {
			continue
		}
		if found {
			return 0, false
		}
		destination = outbound.DestinationShard
		found = true
	}
	return destination, found
}

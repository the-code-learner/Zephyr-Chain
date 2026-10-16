package execution

import (
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func (e Engine) executeCrossShardLending(t tx.Transaction, op tx.Operation) (Result, error) {
	switch op.Kind {
	case tx.OpCrossLoanOffer:
		return e.executeCrossLoanOffer(t, op.Payload)
	case tx.OpCrossLoanAccept:
		return e.executeCrossLoanAccept(t, op.Payload)
	case tx.OpCrossLoanRepay:
		return e.executeCrossLoanRepay(t, op.Payload)
	case tx.OpCrossLoanSettle:
		return e.executeCrossLoanSettle(t)
	case tx.OpCrossLoanDefault:
		return e.executeCrossLoanDefault(t, op.Payload)
	case tx.OpCrossLoanExpire:
		return e.executeCrossLoanExpire(t, op.Payload)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) executeCrossLoanOffer(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 {
		return Result{}, lending.ErrLendingState
	}
	terms, err := lending.ParseTerms(payload)
	if err != nil || terms.Lender != t.Sender || terms.MaturityHeight <= e.Height ||
		terms.MaturityHeight > math.MaxUint64-lending.CrossShardSettlementGraceBlocks {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(terms)
	if err != nil || lenderShard != t.ShardID || lenderShard == borrowerShard {
		return Result{}, ErrShard
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	created, outbound, consumed, err := e.accountNativeValue(t, nil, 0, terms.Principal)
	if err != nil {
		return Result{}, err
	}
	txID := t.ID()
	offerID := types.ObjectIDForShard(txID, lending.CrossOfferMessageIndex, borrowerShard)
	message := lending.CrossOffer{
		OfferObject: offerID, LenderShard: lenderShard, BorrowerShard: borrowerShard,
		OfferedHeight: e.Height, Terms: terms,
	}
	raw, err := message.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	outbound = append(outbound, OutboundOutput{
		DestinationShard: borrowerShard,
		OutputIndex:      lending.CrossOfferMessageIndex,
		Output: object.OutputSpec{
			Owner: terms.Borrower,
			Kind:  object.KindCrossLoanOffer,
			Data:  raw,
		},
	})
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: txID}, nil
}

func (e Engine) executeCrossLoanAccept(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 {
		return Result{}, lending.ErrLendingState
	}
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	offerWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || offerWitness.Object.Kind != object.KindCrossLoanOffer || offerWitness.Object.Owner != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	offer, err := lending.ParseCrossOffer(offerWitness.Object.Data)
	if err != nil || offer.OfferObject != offerWitness.Object.ID || offer.Terms.Borrower != t.Sender ||
		e.Height >= offer.Terms.MaturityHeight {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(offer.Terms)
	if err != nil || lenderShard != offer.LenderShard || borrowerShard != offer.BorrowerShard ||
		borrowerShard != t.ShardID || lenderShard == borrowerShard {
		return Result{}, ErrShard
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	internalOut, err := addLendingValue(offer.Terms.Principal, offer.Terms.Collateral)
	if err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{offerWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, offer.Terms.Principal, internalOut)
	if err != nil {
		return Result{}, err
	}
	txID := t.ID()
	activationID := types.ObjectIDForShard(txID, lending.CrossActivationObjectIndex, borrowerShard)
	acceptanceID := types.ObjectIDForShard(txID, lending.CrossAcceptanceMessageIndex, lenderShard)
	acceptance := lending.CrossAcceptance{
		OfferObject: offer.OfferObject, ActivationObject: activationID,
		LenderShard: lenderShard, BorrowerShard: borrowerShard,
		AcceptedHeight: e.Height, Terms: offer.Terms,
	}
	acceptanceRaw, err := acceptance.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	activation := lending.CrossActivation{
		AcceptanceObject: acceptanceID, LenderShard: lenderShard, BorrowerShard: borrowerShard,
		AcceptedHeight: e.Height, Terms: offer.Terms,
	}
	activationRaw, err := activation.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	principal, err := e.lendingPayout(t, offer.Terms.Borrower, offer.Terms.Principal, lending.CrossPrincipalObjectIndex)
	if err != nil {
		return Result{}, err
	}
	created = append(created,
		principal,
		object.Object{
			ID: activationID, Version: 1, Owner: offer.Terms.Borrower,
			Kind: object.KindCrossLoanActivation, Data: activationRaw,
		},
	)
	outbound = append(outbound, OutboundOutput{
		DestinationShard: lenderShard,
		OutputIndex:      lending.CrossAcceptanceMessageIndex,
		Output: object.OutputSpec{
			Owner: offer.Terms.Lender,
			Kind:  object.KindCrossLoanAcceptance,
			Data:  acceptanceRaw,
		},
	})
	consumed = append(consumed, offerWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: txID}, nil
}

func (e Engine) executeCrossLoanRepay(t tx.Transaction, payload []byte) (Result, error) {
	if e.Height == 0 {
		return Result{}, lending.ErrLendingState
	}
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	activationWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || activationWitness.Object.Kind != object.KindCrossLoanActivation || activationWitness.Object.Owner != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	activation, err := lending.ParseCrossActivation(activationWitness.Object.Data)
	if err != nil || activation.Terms.Borrower != t.Sender || e.Height > activation.Terms.MaturityHeight {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(activation.Terms)
	if err != nil || lenderShard != activation.LenderShard || borrowerShard != activation.BorrowerShard ||
		borrowerShard != t.ShardID || lenderShard == borrowerShard {
		return Result{}, ErrShard
	}
	due, err := activation.Terms.RepaymentDue()
	if err != nil {
		return Result{}, err
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{activationWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, 0, due)
	if err != nil {
		return Result{}, err
	}
	termsHash, err := activation.Terms.Hash()
	if err != nil {
		return Result{}, err
	}
	repayment := lending.CrossRepayment{
		AcceptanceObject: activation.AcceptanceObject,
		ActivationObject: activationWitness.Object.ID,
		LenderShard: lenderShard, BorrowerShard: borrowerShard,
		Amount: due, PaidHeight: e.Height, TermsHash: termsHash,
	}
	raw, err := repayment.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	outbound = append(outbound, OutboundOutput{
		DestinationShard: lenderShard,
		OutputIndex:      lending.CrossRepaymentMessageIndex,
		Output: object.OutputSpec{
			Owner: activation.Terms.Lender,
			Kind:  object.KindCrossLoanRepayment,
			Data:  raw,
		},
	})
	consumed = append(consumed, activationWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeCrossLoanSettle(t tx.Transaction) (Result, error) {
	acceptanceWitness, ok := witnessByKind(t, object.KindCrossLoanAcceptance)
	if !ok {
		return Result{}, lending.ErrLendingState
	}
	repaymentWitness, ok := witnessByKind(t, object.KindCrossLoanRepayment)
	if !ok {
		return Result{}, lending.ErrLendingState
	}
	acceptance, err := lending.ParseCrossAcceptance(acceptanceWitness.Object.Data)
	if err != nil || acceptanceWitness.Object.Owner != acceptance.Terms.Lender {
		return Result{}, lending.ErrLendingState
	}
	repayment, err := lending.ParseCrossRepayment(repaymentWitness.Object.Data)
	if err != nil || repaymentWitness.Object.Owner != acceptance.Terms.Lender {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(acceptance.Terms)
	if err != nil || lenderShard != t.ShardID || lenderShard != acceptance.LenderShard ||
		borrowerShard != acceptance.BorrowerShard || lenderShard == borrowerShard {
		return Result{}, ErrShard
	}
	due, err := acceptance.Terms.RepaymentDue()
	if err != nil {
		return Result{}, err
	}
	termsHash, err := acceptance.Terms.Hash()
	if err != nil {
		return Result{}, err
	}
	if repayment.AcceptanceObject != acceptanceWitness.Object.ID ||
		repayment.ActivationObject != acceptance.ActivationObject ||
		repayment.LenderShard != lenderShard || repayment.BorrowerShard != borrowerShard ||
		repayment.Amount != due || repayment.PaidHeight > acceptance.Terms.MaturityHeight ||
		repayment.TermsHash != termsHash {
		return Result{}, lending.ErrLendingState
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	internal, err := addLendingValue(acceptance.Terms.Collateral, due)
	if err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{
		acceptanceWitness.Object.ID: true,
		repaymentWitness.Object.ID:  true,
	}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, internal, internal)
	if err != nil {
		return Result{}, err
	}
	lenderPayout, err := e.lendingPayout(t, acceptance.Terms.Lender, due, lending.PayoutObjectIndex)
	if err != nil {
		return Result{}, err
	}
	created = append(created, lenderPayout)
	if acceptance.Terms.Collateral > 0 {
		spec, err := object.NewCoinOutputAtHeight(acceptance.Terms.Borrower, e.NativeToken, acceptance.Terms.Collateral, e.Height)
		if err != nil {
			return Result{}, err
		}
		outbound = append(outbound, OutboundOutput{
			DestinationShard: borrowerShard,
			OutputIndex:      lending.CrossCollateralReturnIndex,
			Output:           spec,
		})
	}
	consumed = append(consumed, acceptanceWitness.Object.ID, repaymentWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeCrossLoanDefault(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	acceptanceWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || acceptanceWitness.Object.Kind != object.KindCrossLoanAcceptance || acceptanceWitness.Object.Owner != t.Sender {
		return Result{}, lending.ErrLendingState
	}
	acceptance, err := lending.ParseCrossAcceptance(acceptanceWitness.Object.Data)
	if err != nil || acceptance.Terms.Lender != t.Sender ||
		acceptance.Terms.MaturityHeight > math.MaxUint64-lending.CrossShardSettlementGraceBlocks ||
		e.Height <= acceptance.Terms.MaturityHeight+lending.CrossShardSettlementGraceBlocks {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(acceptance.Terms)
	if err != nil || lenderShard != t.ShardID || lenderShard != acceptance.LenderShard ||
		borrowerShard != acceptance.BorrowerShard {
		return Result{}, ErrShard
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{acceptanceWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, acceptance.Terms.Collateral, acceptance.Terms.Collateral)
	if err != nil {
		return Result{}, err
	}
	if acceptance.Terms.Collateral > 0 {
		collateral, err := e.lendingPayout(t, acceptance.Terms.Lender, acceptance.Terms.Collateral, lending.PayoutObjectIndex)
		if err != nil {
			return Result{}, err
		}
		created = append(created, collateral)
	}
	consumed = append(consumed, acceptanceWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeCrossLoanExpire(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := lending.ParseRef(payload)
	if err != nil {
		return Result{}, err
	}
	offerWitness, ok := witnessByID(t, ref.ObjectID)
	if !ok || offerWitness.Object.Kind != object.KindCrossLoanOffer {
		return Result{}, lending.ErrLendingState
	}
	offer, err := lending.ParseCrossOffer(offerWitness.Object.Data)
	if err != nil || offer.Terms.MaturityHeight > math.MaxUint64-lending.CrossShardSettlementGraceBlocks ||
		e.Height <= offer.Terms.MaturityHeight+lending.CrossShardSettlementGraceBlocks {
		return Result{}, lending.ErrLendingState
	}
	lenderShard, borrowerShard, err := e.crossLendingShards(offer.Terms)
	if err != nil || borrowerShard != t.ShardID || lenderShard != offer.LenderShard ||
		borrowerShard != offer.BorrowerShard {
		return Result{}, ErrShard
	}
	if err := requireLendingChangeOnly(t); err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{offerWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, offer.Terms.Principal, offer.Terms.Principal)
	if err != nil {
		return Result{}, err
	}
	spec, err := object.NewCoinOutputAtHeight(offer.Terms.Lender, e.NativeToken, offer.Terms.Principal, e.Height)
	if err != nil {
		return Result{}, err
	}
	outbound = append(outbound, OutboundOutput{
		DestinationShard: lenderShard,
		OutputIndex:      lending.CrossPrincipalReturnIndex,
		Output:           spec,
	})
	consumed = append(consumed, offerWitness.Object.ID)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) crossLendingShards(terms lending.Terms) (uint32, uint32, error) {
	count := e.ShardCount
	if count < 2 {
		return 0, 0, ErrShard
	}
	lenderShard := types.AccountShard(terms.Lender, count)
	borrowerShard := types.AccountShard(terms.Borrower, count)
	if lenderShard == borrowerShard {
		return 0, 0, ErrShard
	}
	return lenderShard, borrowerShard, nil
}

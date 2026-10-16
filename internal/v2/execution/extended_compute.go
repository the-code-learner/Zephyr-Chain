package execution

import (
	"bytes"
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const computeReceiptIndex uint32 = 0xC0000000

func (e Engine) executeCompute(t tx.Transaction, op tx.Operation) (Result, error) {
	switch op.Kind {
	case tx.OpComputeOffer:
		return e.executeComputeOffer(t, op.Payload)
	case tx.OpComputeJob:
		return e.executeComputeJob(t, op.Payload)
	case tx.OpComputeAccept:
		return e.executeComputeAccept(t, op.Payload)
	case tx.OpComputeResult:
		return e.executeComputeResult(t, op.Payload)
	case tx.OpComputeIngestAssignment:
		return e.executeComputeIngestAssignment(t, op.Payload)
	case tx.OpComputeIngestResult:
		return e.executeComputeIngestResult(t, op.Payload)
	case tx.OpComputeFinalize:
		return e.executeComputeFinalize(t, op.Payload, false)
	case tx.OpComputeResolveReplicated:
		return e.executeComputeFinalize(t, op.Payload, true)
	case tx.OpComputeExpire:
		return e.executeComputeExpire(t, op.Payload)
	default:
		return Result{}, ErrUnsupportedOperation
	}
}

func (e Engine) executeComputeOffer(t tx.Transaction, payload []byte) (Result, error) {
	offer, err := compute.ParseOffer(payload)
	if err != nil || offer.Provider != t.Sender || (e.Height > 0 && offer.ValidUntilHeight < e.Height) {
		return Result{}, ErrOwnership
	}
	created, outbound, consumed, err := e.accountNativeValue(t, nil, 0, offer.Collateral)
	if err != nil {
		return Result{}, err
	}
	offerObject, err := compute.NewOfferObject(t.ID(), t.ShardID, offer)
	if err != nil {
		return Result{}, err
	}
	created = append(created, offerObject)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeJob(t tx.Transaction, payload []byte) (Result, error) {
	job, err := compute.ParseJob(payload)
	if err != nil || job.Owner != t.Sender || (e.Height > 0 && job.DeadlineHeight <= e.Height) {
		return Result{}, ErrOwnership
	}
	created, outbound, consumed, err := e.accountNativeValue(t, nil, 0, job.MaxPrice)
	if err != nil {
		return Result{}, err
	}
	jobObject, _, err := compute.NewJobObject(t.ID(), t.ShardID, job, job.MaxPrice)
	if err != nil {
		return Result{}, err
	}
	created = append(created, jobObject)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeAccept(t tx.Transaction, payload []byte) (Result, error) {
	message, err := compute.ParseAssignmentMessage(payload)
	if err != nil || message.Offer.Provider != t.Sender || message.JobShard >= e.ShardCount {
		return Result{}, ErrOwnership
	}
	offerWitness, ok := witnessByKind(t, object.KindComputeOffer)
	if !ok || offerWitness.Object.Owner != t.Sender || types.Hash(offerWitness.Object.ID) != message.OfferID || !bytes.Equal(offerWitness.Object.Data, mustOfferBytes(message.Offer)) {
		return Result{}, ErrOwnership
	}
	locked := message.Job.CollateralRequired
	excluded := map[types.ObjectID]bool{offerWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, message.Offer.Collateral, locked)
	if err != nil {
		return Result{}, err
	}
	consumed = append(consumed, offerWitness.Object.ID)
	spec, err := message.Output()
	if err != nil {
		return Result{}, err
	}
	if message.JobShard == t.ShardID {
		created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), computeReceiptIndex, t.ShardID), Version: 1, Owner: spec.Owner, Kind: spec.Kind, Data: spec.Data})
	} else {
		outbound = append(outbound, OutboundOutput{DestinationShard: message.JobShard, OutputIndex: computeReceiptIndex, Output: spec})
	}
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeResult(t tx.Transaction, payload []byte) (Result, error) {
	message, err := compute.ParseResultMessage(payload)
	if err != nil || message.Result.Provider != t.Sender || message.JobShard >= e.ShardCount {
		return Result{}, ErrOwnership
	}
	created, outbound, consumed, err := e.accountNativeValue(t, nil, 0, 0)
	if err != nil {
		return Result{}, err
	}
	spec, _ := message.Output()
	if message.JobShard == t.ShardID {
		created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), computeReceiptIndex, t.ShardID), Version: 1, Owner: spec.Owner, Kind: spec.Kind, Data: spec.Data})
	} else {
		outbound = append(outbound, OutboundOutput{DestinationShard: message.JobShard, OutputIndex: computeReceiptIndex, Output: spec})
	}
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeIngestAssignment(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := compute.ParseIngestRef(payload)
	if err != nil {
		return Result{}, err
	}
	jobWitness, ok := witnessByID(t, ref.JobObject)
	if !ok || jobWitness.Object.Kind != object.KindComputeJob || jobWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	messageWitness, ok := witnessByID(t, ref.MessageObject)
	if !ok || messageWitness.Object.Kind != object.KindComputeAssignment || messageWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	record, err := compute.ParseOnChainJob(jobWitness.Object.Data)
	if err != nil {
		return Result{}, err
	}
	message, err := compute.ParseAssignmentMessage(messageWitness.Object.Data)
	if err != nil || message.JobShard != t.ShardID || message.ValidateForRecord(record) != nil {
		return Result{}, compute.ErrComputeMessage
	}
	updated, _, _, err := compute.AssignOnChain(record, message.OfferID, message.Offer, e.Height)
	if err != nil {
		return Result{}, err
	}
	updatedRaw, err := updated.MarshalBinary()
	if err != nil {
		return Result{}, err
	}
	excluded := map[types.ObjectID]bool{jobWitness.Object.ID: true, messageWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, record.Job.CollateralRequired, record.Job.CollateralRequired)
	if err != nil {
		return Result{}, err
	}
	consumed = append(consumed, jobWitness.Object.ID, messageWitness.Object.ID)
	jobObject := jobWitness.Object
	jobObject.Version++
	jobObject.Data = updatedRaw
	created = append(created, jobObject)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeIngestResult(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := compute.ParseIngestRef(payload)
	if err != nil {
		return Result{}, err
	}
	jobWitness, ok := witnessByID(t, ref.JobObject)
	if !ok || jobWitness.Object.Kind != object.KindComputeJob || jobWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	messageWitness, ok := witnessByID(t, ref.MessageObject)
	if !ok || messageWitness.Object.Kind != object.KindComputeResult || messageWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	record, err := compute.ParseOnChainJob(jobWitness.Object.Data)
	if err != nil {
		return Result{}, err
	}
	message, err := compute.ParseResultMessage(messageWitness.Object.Data)
	if err != nil || message.JobID != record.ID || message.JobOwner != record.Job.Owner || message.JobShard != t.ShardID {
		return Result{}, compute.ErrComputeMessage
	}
	updated, err := compute.SubmitOnChainResult(record, message.Result)
	if err != nil {
		return Result{}, err
	}
	updatedRaw, _ := updated.MarshalBinary()
	excluded := map[types.ObjectID]bool{jobWitness.Object.ID: true, messageWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, 0, 0)
	if err != nil {
		return Result{}, err
	}
	consumed = append(consumed, jobWitness.Object.ID, messageWitness.Object.ID)
	jobObject := jobWitness.Object
	jobObject.Version++
	jobObject.Data = updatedRaw
	created = append(created, jobObject)
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeFinalize(t tx.Transaction, payload []byte, majority bool) (Result, error) {
	ref, err := compute.ParseJobRef(payload)
	if err != nil {
		return Result{}, err
	}
	jobWitness, ok := witnessByID(t, ref.JobObject)
	if !ok || jobWitness.Object.Kind != object.KindComputeJob || jobWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	record, err := compute.ParseOnChainJob(jobWitness.Object.Data)
	if err != nil || record.Job.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	var settlement compute.OnChainSettlement
	if majority {
		_, settlement, err = compute.ResolveReplicatedMajority(record)
	} else {
		if record.Job.Verification != compute.VerificationReplicated {
			return Result{}, compute.ErrMarketVerification
		}
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
	}
	if err != nil {
		return Result{}, err
	}
	locked, err := jobLockedValue(record)
	if err != nil {
		return Result{}, err
	}
	generated, generatedOutbound, generatedAmount, err := e.settlementOutputs(t, record, settlement)
	if err != nil || generatedAmount != locked {
		return Result{}, compute.ErrMarketEscrow
	}
	excluded := map[types.ObjectID]bool{jobWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, locked, generatedAmount)
	if err != nil {
		return Result{}, err
	}
	created = append(created, generated...)
	outbound = append(outbound, generatedOutbound...)
	consumed = append(consumed, jobWitness.Object.ID)
	receipt := compute.SettlementReceipt{JobID: record.ID, ResultRoot: settlement.ResultRoot, Payments: settlement.Payments, Refund: settlement.Refund, Slashed: settlement.SlashedCollateral, SlashReward: settlement.SlashReward}
	created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), computeReceiptIndex, t.ShardID), Version: 1, Kind: object.KindSystem, Data: receipt.MarshalBinary()})
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) executeComputeExpire(t tx.Transaction, payload []byte) (Result, error) {
	ref, err := compute.ParseJobRef(payload)
	if err != nil {
		return Result{}, err
	}
	jobWitness, ok := witnessByID(t, ref.JobObject)
	if !ok || jobWitness.Object.Kind != object.KindComputeJob || jobWitness.Object.Owner != t.Sender {
		return Result{}, ErrOwnership
	}
	record, err := compute.ParseOnChainJob(jobWitness.Object.Data)
	if err != nil {
		return Result{}, err
	}
	_, refund, collateral, err := compute.ExpireOnChain(record, e.Height)
	if err != nil {
		return Result{}, err
	}
	settlement := compute.OnChainSettlement{Settlement: compute.Settlement{JobID: record.ID, Payments: map[types.AccountID]uint64{}, Refund: refund}, CollateralReturns: collateral, SlashedCollateral: map[types.AccountID]uint64{}}
	locked, err := jobLockedValue(record)
	if err != nil {
		return Result{}, err
	}
	generated, generatedOutbound, generatedAmount, err := e.settlementOutputs(t, record, settlement)
	if err != nil || generatedAmount != locked {
		return Result{}, compute.ErrMarketEscrow
	}
	excluded := map[types.ObjectID]bool{jobWitness.Object.ID: true}
	created, outbound, consumed, err := e.accountNativeValue(t, excluded, locked, generatedAmount)
	if err != nil {
		return Result{}, err
	}
	created = append(created, generated...)
	outbound = append(outbound, generatedOutbound...)
	consumed = append(consumed, jobWitness.Object.ID)
	receipt := compute.SettlementReceipt{JobID: record.ID, Refund: refund, Expired: true}
	created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), computeReceiptIndex, t.ShardID), Version: 1, Kind: object.KindSystem, Data: receipt.MarshalBinary()})
	return Result{Consumed: consumed, Created: created, Outbound: outbound, TxID: t.ID()}, nil
}

func (e Engine) accountNativeValue(t tx.Transaction, excluded map[types.ObjectID]bool, internalIn, internalOut uint64) ([]object.Object, []OutboundOutput, []types.ObjectID, error) {
	var available uint64 = internalIn
	consumed := make([]types.ObjectID, 0)
	for _, witness := range t.Witnesses {
		if excluded[witness.Object.ID] {
			continue
		}
		if witness.Object.Kind != object.KindCoin || witness.Object.Owner != t.Sender {
			return nil, nil, nil, ErrOwnership
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil || coin.Token != e.NativeToken || math.MaxUint64-available < coin.Amount {
			return nil, nil, nil, ErrConservation
		}
		available += coin.Amount
		consumed = append(consumed, witness.Object.ID)
	}
	var required uint64 = internalOut
	created := make([]object.Object, 0, len(t.Outputs))
	outbound := make([]OutboundOutput, 0)
	router := sharding.Router{ShardCount: e.ShardCount}
	for i, spec := range t.Outputs {
		if spec.Kind != object.KindCoin {
			return nil, nil, nil, ErrConservation
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil || coin.Token != e.NativeToken || math.MaxUint64-required < coin.Amount {
			return nil, nil, nil, ErrConservation
		}
		required += coin.Amount
		destination, err := router.ShardForAccount(spec.Owner)
		if err != nil {
			return nil, nil, nil, ErrShard
		}
		if destination == t.ShardID {
			created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), uint32(i), t.ShardID), Version: 1, Owner: spec.Owner, Kind: spec.Kind, Data: append([]byte(nil), spec.Data...)})
		} else {
			outbound = append(outbound, OutboundOutput{DestinationShard: destination, OutputIndex: uint32(i), Output: spec})
		}
	}
	if math.MaxUint64-required < t.Fee {
		return nil, nil, nil, ErrOverflow
	}
	required += t.Fee
	if available != required {
		return nil, nil, nil, ErrConservation
	}
	return created, outbound, consumed, nil
}

func (e Engine) settlementOutputs(t tx.Transaction, record compute.OnChainJob, settlement compute.OnChainSettlement) ([]object.Object, []OutboundOutput, uint64, error) {
	amounts := make(map[types.AccountID]uint64)
	for provider, value := range settlement.Payments {
		amounts[provider] += value
	}
	for provider, value := range settlement.CollateralReturns {
		if math.MaxUint64-amounts[provider] < value {
			return nil, nil, 0, ErrOverflow
		}
		amounts[provider] += value
	}
	ownerAmount := settlement.Refund
	if math.MaxUint64-ownerAmount < settlement.SlashReward {
		return nil, nil, 0, ErrOverflow
	}
	ownerAmount += settlement.SlashReward
	if ownerAmount > 0 {
		amounts[record.Job.Owner] += ownerAmount
	}
	ids := make([]types.AccountID, 0, len(amounts))
	for id := range amounts {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i].String() < ids[j].String() })
	router := sharding.Router{ShardCount: e.ShardCount}
	created := make([]object.Object, 0, len(ids))
	outbound := make([]OutboundOutput, 0)
	var total uint64
	for i, id := range ids {
		amount := amounts[id]
		if math.MaxUint64-total < amount {
			return nil, nil, 0, ErrOverflow
		}
		total += amount
		spec, err := object.NewCoinOutput(id, e.NativeToken, amount)
		if err != nil {
			return nil, nil, 0, err
		}
		destination, err := router.ShardForAccount(id)
		if err != nil {
			return nil, nil, 0, ErrShard
		}
		index := computeReceiptIndex + 1 + uint32(i)
		if destination == t.ShardID {
			created = append(created, object.Object{ID: types.ObjectIDForShard(t.ID(), index, t.ShardID), Version: 1, Owner: spec.Owner, Kind: spec.Kind, Data: spec.Data})
		} else {
			outbound = append(outbound, OutboundOutput{DestinationShard: destination, OutputIndex: index, Output: spec})
		}
	}
	return created, outbound, total, nil
}

func jobLockedValue(record compute.OnChainJob) (uint64, error) {
	locked := record.Escrow
	for range record.Assignments {
		if math.MaxUint64-locked < record.Job.CollateralRequired {
			return 0, ErrOverflow
		}
		locked += record.Job.CollateralRequired
	}
	return locked, nil
}

func witnessByKind(t tx.Transaction, kind object.Kind) (tx.Witness, bool) {
	var found tx.Witness
	ok := false
	for _, witness := range t.Witnesses {
		if witness.Object.Kind == kind {
			if ok {
				return tx.Witness{}, false
			}
			found, ok = witness, true
		}
	}
	return found, ok
}

func witnessByID(t tx.Transaction, id types.ObjectID) (tx.Witness, bool) {
	for _, witness := range t.Witnesses {
		if witness.Object.ID == id {
			return witness, true
		}
	}
	return tx.Witness{}, false
}

func mustOfferBytes(offer compute.Offer) []byte {
	raw, _ := offer.MarshalBinary()
	return raw
}

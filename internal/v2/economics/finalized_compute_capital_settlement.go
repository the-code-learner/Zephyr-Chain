package economics

import (
	"bytes"
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func finalizedComputeSettlement(
	height uint64,
	transaction tx.Transaction,
	result execution.Result,
) (compute.OnChainJob, compute.OnChainSettlement, FinalizedComputeProductiveEvidence, bool, error) {
	if height == 0 || len(transaction.Operations) != 1 {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	op := transaction.Operations[0]
	ref, err := compute.ParseJobRef(op.Payload)
	if err != nil {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	witness, ok := witnessByObjectID(transaction, ref.JobObject)
	if !ok || witness.Object.Kind != object.KindComputeJob || witness.Object.Owner != transaction.Sender || finalizedConsumedCount(result.Consumed, ref.JobObject) != 1 {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	record, err := compute.ParseOnChainJob(witness.Object.Data)
	if err != nil || record.Job.Owner != transaction.Sender {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}

	var settlement compute.OnChainSettlement
	var evidence FinalizedComputeProductiveEvidence
	productive := false
	switch op.Kind {
	case tx.OpComputeFinalize:
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
	case tx.OpComputeResolveReplicated:
		_, settlement, err = compute.ResolveReplicatedMajority(record)
	case tx.OpComputeExpire:
		var refund uint64
		var collateral map[types.AccountID]uint64
		_, refund, collateral, err = compute.ExpireOnChain(record, height)
		settlement = compute.OnChainSettlement{
			Settlement:        compute.Settlement{JobID: record.ID, Payments: map[types.AccountID]uint64{}, Refund: refund},
			CollateralReturns: collateral,
			SlashedCollateral: map[types.AccountID]uint64{},
		}
	default:
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	if err != nil {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	expected := compute.SettlementReceipt{
		JobID:       record.ID,
		ResultRoot:  settlement.ResultRoot,
		Payments:    settlement.Payments,
		Refund:      settlement.Refund,
		Slashed:     settlement.SlashedCollateral,
		SlashReward: settlement.SlashReward,
		Expired:     op.Kind == tx.OpComputeExpire,
	}
	receipt, ok, err := uniqueSettlementReceiptForJob(result.Created, record.ID)
	if err != nil || !ok || !bytes.Equal(receipt.MarshalBinary(), expected.MarshalBinary()) {
		return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	if op.Kind != tx.OpComputeExpire {
		evidence, productive, err = DeriveFinalizedComputeProductiveEvidence(transaction, result)
		if err != nil || !productive || evidence.JobID != record.ID || evidence.Escrow != record.Escrow {
			return compute.OnChainJob{}, compute.OnChainSettlement{}, FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
		}
	}
	return record, settlement, evidence, productive, nil
}

func finalizedComputeSettlementTargets(
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
	record compute.OnChainJob,
	settlement compute.OnChainSettlement,
) (map[types.AccountID]CapitalTarget, error) {
	amounts := make(map[types.AccountID]uint64)
	for provider, amount := range settlement.Payments {
		if err := addAccountAmount(amounts, provider, amount); err != nil {
			return nil, err
		}
	}
	for provider, amount := range settlement.CollateralReturns {
		if err := addAccountAmount(amounts, provider, amount); err != nil {
			return nil, err
		}
	}
	ownerAmount := settlement.Refund
	if math.MaxUint64-ownerAmount < settlement.SlashReward {
		return nil, ErrFinalizedEconomics
	}
	ownerAmount += settlement.SlashReward
	if ownerAmount > 0 {
		if err := addAccountAmount(amounts, record.Job.Owner, ownerAmount); err != nil {
			return nil, err
		}
	}
	accounts := make([]types.AccountID, 0, len(amounts))
	for account, amount := range amounts {
		if amount == 0 {
			return nil, ErrFinalizedEconomics
		}
		accounts = append(accounts, account)
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].String() < accounts[j].String() })
	out := make(map[types.AccountID]CapitalTarget, len(accounts))
	for i, account := range accounts {
		spec, err := object.NewCoinOutput(account, nativeToken, amounts[account])
		if err != nil {
			return nil, err
		}
		index := computeSettlementReceiptOutputIndex + 1 + uint32(i)
		target, err := finalizedCapitalTargetForOutput(transaction, result, exports, index, spec)
		if err != nil {
			return nil, err
		}
		out[account] = target
	}
	return out, nil
}

func finalizedComputeOrdinaryTargets(
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
) ([]CapitalTarget, error) {
	targets := make([]CapitalTarget, 0, len(transaction.Outputs))
	for i, spec := range transaction.Outputs {
		if spec.Kind != object.KindCoin {
			return nil, ErrFinalizedEconomics
		}
		coin, err := object.ParseCoin(spec.Data)
		if err != nil || coin.Token != nativeToken {
			return nil, ErrFinalizedEconomics
		}
		target, err := finalizedCapitalTargetForOutput(transaction, result, exports, uint32(i), spec)
		if err != nil {
			return nil, err
		}
		targets = append(targets, target)
	}
	return targets, nil
}

func finalizedCapitalTargetForOutput(
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
	index uint32,
	spec object.OutputSpec,
) (CapitalTarget, error) {
	coin, err := object.ParseCoin(spec.Data)
	if err != nil || spec.Kind != object.KindCoin {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	localID := types.ObjectIDForShard(transaction.ID(), index, transaction.ShardID)
	localFound := false
	for _, created := range result.Created {
		if created.ID != localID {
			continue
		}
		if localFound || created.Owner != spec.Owner || created.Kind != spec.Kind || !bytes.Equal(created.Data, spec.Data) {
			return CapitalTarget{}, ErrFinalizedEconomics
		}
		localFound = true
	}

	var outbound *execution.OutboundOutput
	for i := range result.Outbound {
		candidate := &result.Outbound[i]
		if candidate.OutputIndex != index || !bytes.Equal(candidate.Output.CanonicalBytes(), spec.CanonicalBytes()) {
			continue
		}
		if outbound != nil || candidate.DestinationShard == transaction.ShardID {
			return CapitalTarget{}, ErrFinalizedEconomics
		}
		outbound = candidate
	}
	if localFound == (outbound != nil) {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	if localFound {
		return CapitalTarget{ObjectID: localID, Amount: coin.Amount}, nil
	}

	var receipt *sharding.CrossShardReceipt
	for i := range exports {
		candidate := &exports[i]
		if candidate.TransactionID != result.TxID || candidate.SourceShard != transaction.ShardID ||
			candidate.DestinationShard != outbound.DestinationShard || candidate.OutputIndex != index ||
			!bytes.Equal(candidate.Output.CanonicalBytes(), spec.CanonicalBytes()) {
			continue
		}
		if receipt != nil || candidate.Validate() != nil {
			return CapitalTarget{}, ErrFinalizedEconomics
		}
		receipt = candidate
	}
	if receipt == nil {
		return CapitalTarget{}, ErrFinalizedEconomics
	}
	transferID, err := receipt.Hash()
	if err != nil {
		return CapitalTarget{}, err
	}
	return CapitalTarget{TransferID: transferID, Amount: coin.Amount}, nil
}

func applyFinalizedNativeInputsTransition(
	tracker *IdleCapitalTracker,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
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
	if unknownAge {
		if existingTracked {
			return false, ErrFinalizedEconomics
		}
		return false, nil
	}
	if len(inputs) == 0 {
		if len(targets) != 0 || burned != 0 {
			return false, ErrFinalizedEconomics
		}
		return false, nil
	}
	if err := preview.ApplyTransition(inputs, targets, burned); err != nil {
		return false, err
	}
	*tracker = *preview
	return true, nil
}

func prepareFinalizedNativeInputs(
	tracker *IdleCapitalTracker,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
) ([]types.ObjectID, bool, bool, error) {
	consumed := make(map[types.ObjectID]struct{}, len(result.Consumed))
	for _, id := range result.Consumed {
		if types.IsZero32([32]byte(id)) {
			return nil, false, false, ErrFinalizedEconomics
		}
		consumed[id] = struct{}{}
	}
	inputs := make([]types.ObjectID, 0)
	existingTracked := false
	unknownAge := false
	for _, witness := range transaction.Witnesses {
		if _, ok := consumed[witness.Object.ID]; !ok || witness.Object.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil {
			return nil, false, false, err
		}
		if coin.Token != nativeToken {
			continue
		}
		inputs = append(inputs, witness.Object.ID)
		if lots, ok := tracker.ObjectLots(witness.Object.ID); ok {
			existingTracked = true
			amount, err := idleCapitalLotsAmount(lots)
			if err != nil || amount != coin.Amount {
				return nil, false, false, ErrFinalizedEconomics
			}
			continue
		}
		if coin.CreatedHeight == 0 {
			unknownAge = true
			continue
		}
		if err := tracker.BootstrapObject(witness.Object.ID, coin.Amount, coin.CreatedHeight); err != nil {
			return nil, false, false, err
		}
	}
	return inputs, existingTracked, unknownAge, nil
}

func finalizedNativeInputsContainTracked(
	tracker *IdleCapitalTracker,
	nativeToken types.TokenID,
	transaction tx.Transaction,
	result execution.Result,
) bool {
	consumed := make(map[types.ObjectID]struct{}, len(result.Consumed))
	for _, id := range result.Consumed {
		consumed[id] = struct{}{}
	}
	for _, witness := range transaction.Witnesses {
		if _, ok := consumed[witness.Object.ID]; !ok || witness.Object.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err == nil && coin.Token == nativeToken {
			if _, ok := tracker.ObjectLots(witness.Object.ID); ok {
				return true
			}
		}
	}
	return false
}

func finalizedComputeMessageCreated(
	transaction tx.Transaction,
	result execution.Result,
	exports []sharding.CrossShardReceipt,
	kind object.Kind,
	destinationShard uint32,
	outputFn func() (object.OutputSpec, error),
) bool {
	spec, err := outputFn()
	if err != nil || spec.Kind != kind {
		return false
	}
	if destinationShard == transaction.ShardID {
		id := types.ObjectIDForShard(transaction.ID(), computeSettlementReceiptOutputIndex, transaction.ShardID)
		for _, created := range result.Created {
			if created.ID == id && created.Owner == spec.Owner && created.Kind == spec.Kind && bytes.Equal(created.Data, spec.Data) {
				return true
			}
		}
		return false
	}
	for _, outbound := range result.Outbound {
		if outbound.DestinationShard != destinationShard || outbound.OutputIndex != computeSettlementReceiptOutputIndex ||
			!bytes.Equal(outbound.Output.CanonicalBytes(), spec.CanonicalBytes()) {
			continue
		}
		for _, receipt := range exports {
			if receipt.TransactionID == result.TxID && receipt.SourceShard == transaction.ShardID &&
				receipt.DestinationShard == destinationShard && receipt.OutputIndex == outbound.OutputIndex &&
				bytes.Equal(receipt.Output.CanonicalBytes(), spec.CanonicalBytes()) && receipt.Validate() == nil {
				return true
			}
		}
	}
	return false
}

func splitProductiveCapitalAmount(lots []CapitalLot, height, productiveAmount uint64) ([]CapitalLot, []CapitalLot, error) {
	if height == 0 || productiveAmount == 0 {
		return nil, nil, ErrIdleCapital
	}
	canonical, err := compactCapitalLots(lots)
	if err != nil {
		return nil, nil, err
	}
	var total uint64
	for _, lot := range canonical {
		if lot.IdleSinceHeight > height || math.MaxUint64-total < lot.Amount {
			return nil, nil, ErrIdleCapital
		}
		total += lot.Amount
	}
	if productiveAmount > total {
		return nil, nil, ErrIdleCapital
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].IdleSinceHeight != canonical[j].IdleSinceHeight {
			return canonical[i].IdleSinceHeight < canonical[j].IdleSinceHeight
		}
		return bytes.Compare(canonical[i].LineageID[:], canonical[j].LineageID[:]) < 0
	})
	productive := make([]CapitalLot, 0, len(canonical))
	idle := make([]CapitalLot, 0, len(canonical))
	remaining := productiveAmount
	for _, lot := range canonical {
		if remaining == 0 {
			idle = append(idle, lot)
			continue
		}
		take := lot.Amount
		if take > remaining {
			take = remaining
		}
		productiveLot := lot
		productiveLot.Amount = take
		productiveLot.IdleSinceHeight = height
		productive = append(productive, productiveLot)
		if take < lot.Amount {
			idleLot := lot
			idleLot.Amount -= take
			idle = append(idle, idleLot)
		}
		remaining -= take
	}
	if remaining != 0 {
		return nil, nil, ErrIdleCapital
	}
	productive, err = compactCapitalLots(productive)
	if err != nil {
		return nil, nil, err
	}
	if len(idle) > 0 {
		idle, err = compactCapitalLots(idle)
		if err != nil {
			return nil, nil, err
		}
	}
	return productive, idle, nil
}

func replaceComputeComponentsWithSettlementLegs(
	tracker *IdleCapitalTracker,
	componentIDs []types.ObjectID,
	legs map[types.ObjectID][]CapitalLot,
) error {
	if tracker == nil || len(componentIDs) == 0 || len(legs) == 0 {
		return ErrIdleCapital
	}
	sources := make(map[types.ObjectID]struct{}, len(componentIDs))
	for _, id := range componentIDs {
		if _, duplicate := sources[id]; duplicate {
			return ErrIdleCapital
		}
		if _, ok := tracker.objects[id]; !ok {
			return ErrIdleCapital
		}
		sources[id] = struct{}{}
	}
	if len(tracker.objects)-len(sources)+len(legs) > maxIdleTrackerObjects {
		return ErrIdleCapital
	}
	for id, lots := range legs {
		if types.IsZero32([32]byte(id)) {
			return ErrIdleCapital
		}
		if _, source := sources[id]; source {
			return ErrIdleCapital
		}
		if _, exists := tracker.objects[id]; exists {
			return ErrIdleCapital
		}
		canonical, err := compactCapitalLots(lots)
		if err != nil || len(canonical) != len(lots) {
			return ErrIdleCapital
		}
		for i := range canonical {
			if canonical[i] != lots[i] {
				return ErrIdleCapital
			}
		}
	}
	for id := range sources {
		delete(tracker.objects, id)
	}
	for id, lots := range legs {
		tracker.objects[id] = append([]CapitalLot(nil), lots...)
	}
	return nil
}

func computeSettlementLegObjectID(jobID types.JobID, kind computeSettlementLegKind, account types.AccountID, extra []byte) (types.ObjectID, error) {
	if types.IsZero32([32]byte(jobID)) || kind <= computeSettlementLegUnknown || kind > computeSettlementLegOfferExcess {
		return types.ObjectID{}, ErrFinalizedEconomics
	}
	payload := make([]byte, 0, len(jobID)+1+len(account)+len(extra))
	payload = append(payload, jobID[:]...)
	payload = append(payload, byte(kind))
	payload = append(payload, account[:]...)
	payload = append(payload, extra...)
	id := types.ObjectID(types.HashBytes("economics/compute-capital/settlement-leg/v1", payload))
	if types.IsZero32([32]byte(id)) {
		return types.ObjectID{}, ErrFinalizedEconomics
	}
	return id, nil
}

func uniqueCreatedObjectOfKind(created []object.Object, kind object.Kind) (object.Object, bool, error) {
	var found object.Object
	ok := false
	for _, item := range created {
		if item.Kind != kind {
			continue
		}
		if ok {
			return object.Object{}, false, ErrFinalizedEconomics
		}
		found, ok = item, true
	}
	return found, ok, nil
}

func uniqueWitnessOfKind(transaction tx.Transaction, kind object.Kind) (tx.Witness, bool) {
	var found tx.Witness
	ok := false
	for _, witness := range transaction.Witnesses {
		if witness.Object.Kind != kind {
			continue
		}
		if ok {
			return tx.Witness{}, false
		}
		found, ok = witness, true
	}
	return found, ok
}

func witnessByObjectID(transaction tx.Transaction, id types.ObjectID) (tx.Witness, bool) {
	for _, witness := range transaction.Witnesses {
		if witness.Object.ID == id {
			return witness, true
		}
	}
	return tx.Witness{}, false
}

func finalizedConsumedCount(consumed []types.ObjectID, id types.ObjectID) int {
	count := 0
	for _, candidate := range consumed {
		if candidate == id {
			count++
		}
	}
	return count
}

func sortedPositiveAccountAmounts(values map[types.AccountID]uint64) []types.AccountID {
	accounts := make([]types.AccountID, 0, len(values))
	for account, amount := range values {
		if amount > 0 {
			accounts = append(accounts, account)
		}
	}
	sort.Slice(accounts, func(i, j int) bool { return bytes.Compare(accounts[i][:], accounts[j][:]) < 0 })
	return accounts
}

func addAccountAmount(values map[types.AccountID]uint64, account types.AccountID, amount uint64) error {
	if amount == 0 || types.IsZero32([32]byte(account)) || math.MaxUint64-values[account] < amount {
		return ErrFinalizedEconomics
	}
	values[account] += amount
	return nil
}

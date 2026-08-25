package economics

import (
	"bytes"
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ApplyFinalizedIdleCapitalTransfer derives shadow capital-lineage movement
// only from a finalized native transfer and its canonical exported receipts.
// It deliberately refuses to invent cross-shard transfer identifiers from the
// execution result: every cross-shard target is keyed by CrossShardReceipt.Hash.
//
// The helper is prospective. A previously unseen native input can be
// bootstrapped only when its consensus-stamped CreatedHeight is known. If all
// unseen inputs have unknown historical age the observation is skipped without
// mutation. Mixing already-tracked capital with an unknown-age input fails
// closed because silently dropping either lineage would make telemetry false.
func ApplyFinalizedIdleCapitalTransfer(
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
	if len(transaction.Operations) != 1 || transaction.Operations[0].Kind != tx.OpTransfer {
		return false, nil
	}

	preview := tracker.Clone()
	if preview == nil {
		return false, ErrFinalizedEconomics
	}
	consumed := make(map[types.ObjectID]struct{}, len(result.Consumed))
	for _, id := range result.Consumed {
		if types.IsZero32([32]byte(id)) {
			return false, ErrFinalizedEconomics
		}
		consumed[id] = struct{}{}
	}

	inputs := make([]types.ObjectID, 0, len(transaction.Witnesses))
	trackedInput := false
	unknownAgeInput := false
	for _, witness := range transaction.Witnesses {
		if _, ok := consumed[witness.Object.ID]; !ok || witness.Object.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(witness.Object.Data)
		if err != nil {
			return false, err
		}
		if coin.Token != nativeToken {
			continue
		}
		inputs = append(inputs, witness.Object.ID)
		if lots, ok := preview.ObjectLots(witness.Object.ID); ok {
			trackedInput = true
			amount, err := idleCapitalLotsAmount(lots)
			if err != nil || amount != coin.Amount {
				return false, ErrFinalizedEconomics
			}
			continue
		}
		if coin.CreatedHeight == 0 {
			unknownAgeInput = true
			continue
		}
		if err := preview.BootstrapObject(witness.Object.ID, coin.Amount, coin.CreatedHeight); err != nil {
			return false, err
		}
	}
	if len(inputs) == 0 {
		return false, nil
	}
	if unknownAgeInput {
		if trackedInput {
			return false, ErrFinalizedEconomics
		}
		return false, nil
	}

	targets := make([]CapitalTarget, 0, len(result.Created)+len(exports))
	for _, created := range result.Created {
		if created.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(created.Data)
		if err != nil {
			return false, err
		}
		if coin.Token != nativeToken {
			continue
		}
		if coin.CreatedHeight != height || types.IsZero32([32]byte(created.ID)) {
			return false, ErrFinalizedEconomics
		}
		targets = append(targets, CapitalTarget{ObjectID: created.ID, Amount: coin.Amount})
	}

	type exportKey struct {
		destination uint32
		index       uint32
	}
	expected := make(map[exportKey]execution.OutboundOutput, len(result.Outbound))
	for _, outbound := range result.Outbound {
		if outbound.Output.Kind != object.KindCoin {
			continue
		}
		coin, err := object.ParseCoin(outbound.Output.Data)
		if err != nil {
			return false, err
		}
		if coin.Token != nativeToken {
			continue
		}
		if coin.CreatedHeight != height || outbound.DestinationShard == transaction.ShardID {
			return false, ErrFinalizedEconomics
		}
		key := exportKey{destination: outbound.DestinationShard, index: outbound.OutputIndex}
		if _, duplicate := expected[key]; duplicate {
			return false, ErrFinalizedEconomics
		}
		expected[key] = outbound
	}

	seenExports := make(map[exportKey]struct{}, len(exports))
	for _, receipt := range exports {
		if err := receipt.Validate(); err != nil || receipt.SourceShard != transaction.ShardID ||
			receipt.SourceHeight != height || receipt.TransactionID != result.TxID {
			return false, ErrFinalizedEconomics
		}
		key := exportKey{destination: receipt.DestinationShard, index: receipt.OutputIndex}
		if _, duplicate := seenExports[key]; duplicate {
			return false, ErrFinalizedEconomics
		}
		seenExports[key] = struct{}{}
		outbound, ok := expected[key]
		if !ok || !bytes.Equal(outbound.Output.CanonicalBytes(), receipt.Output.CanonicalBytes()) {
			return false, ErrFinalizedEconomics
		}
		coin, err := object.ParseCoin(receipt.Output.Data)
		if err != nil || coin.Token != nativeToken || coin.CreatedHeight != height {
			return false, ErrFinalizedEconomics
		}
		transferID, err := receipt.Hash()
		if err != nil {
			return false, err
		}
		targets = append(targets, CapitalTarget{TransferID: transferID, Amount: coin.Amount})
		delete(expected, key)
	}
	if len(expected) != 0 {
		return false, ErrFinalizedEconomics
	}

	// The collector currently requires CompatibilityFeePolicy, so the full
	// native transaction fee is a real finalized burn rather than a reward or
	// reserve transfer that would require its own capital target.
	if err := preview.ApplyTransition(inputs, targets, transaction.Fee); err != nil {
		return false, err
	}
	*tracker = *preview
	return true, nil
}

// ApplyFinalizedIdleCapitalImport materializes a previously tracked pending
// cross-shard lineage using the consensus-canonical receipt hash and protocol
// destination object identity. If tracking began after the export, the
// destination is prospectively bootstrapped from the finalized stamped coin.
func ApplyFinalizedIdleCapitalImport(
	tracker *IdleCapitalTracker,
	nativeToken types.TokenID,
	receipt sharding.CrossShardReceipt,
) (bool, error) {
	if tracker == nil || types.IsZero32([32]byte(nativeToken)) {
		return false, ErrFinalizedEconomics
	}
	if err := receipt.Validate(); err != nil {
		return false, ErrFinalizedEconomics
	}
	if receipt.Output.Kind != object.KindCoin {
		return false, nil
	}
	coin, err := object.ParseCoin(receipt.Output.Data)
	if err != nil {
		return false, err
	}
	if coin.Token != nativeToken {
		return false, nil
	}
	if coin.CreatedHeight == 0 || coin.CreatedHeight != receipt.SourceHeight {
		return false, ErrFinalizedEconomics
	}
	transferID, err := receipt.Hash()
	if err != nil {
		return false, err
	}
	destination, err := receipt.DestinationObject()
	if err != nil {
		return false, err
	}

	preview := tracker.Clone()
	if preview == nil {
		return false, ErrFinalizedEconomics
	}
	if lots, ok := preview.pendingTransfers[transferID]; ok {
		amount, err := idleCapitalLotsAmount(lots)
		if err != nil || amount != coin.Amount {
			return false, ErrFinalizedEconomics
		}
		if err := preview.MaterializeTransfer(transferID, destination.ID); err != nil {
			return false, err
		}
		*tracker = *preview
		return true, nil
	}
	if _, exists := preview.objects[destination.ID]; exists {
		return false, ErrFinalizedEconomics
	}
	if err := preview.BootstrapObject(destination.ID, coin.Amount, coin.CreatedHeight); err != nil {
		return false, err
	}
	*tracker = *preview
	return true, nil
}

func idleCapitalLotsAmount(lots []CapitalLot) (uint64, error) {
	if len(lots) == 0 {
		return 0, ErrFinalizedEconomics
	}
	var total uint64
	for _, lot := range lots {
		if err := lot.Validate(); err != nil || math.MaxUint64-total < lot.Amount {
			return 0, ErrFinalizedEconomics
		}
		total += lot.Amount
	}
	return total, nil
}

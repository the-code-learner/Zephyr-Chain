package compute

import (
	"bytes"
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ParseSettlementReceipt decodes the canonical settlement evidence committed by
// compute finalization. The receipt is intentionally self-contained so light
// and economic replay code can verify settlement without trusting an RPC.
func ParseSettlementReceipt(data []byte) (SettlementReceipt, error) {
	r := codec.NewReader(data)
	jobRaw, err := r.Fixed(32)
	if err != nil {
		return SettlementReceipt{}, ErrMarketState
	}
	rootRaw, err := r.Fixed(32)
	if err != nil {
		return SettlementReceipt{}, ErrMarketState
	}
	payments, err := readAccountAmounts(r)
	if err != nil {
		return SettlementReceipt{}, err
	}
	refund, err := r.U64()
	if err != nil {
		return SettlementReceipt{}, ErrMarketState
	}
	slashed, err := readAccountAmounts(r)
	if err != nil {
		return SettlementReceipt{}, err
	}
	slashReward, err := r.U64()
	if err != nil {
		return SettlementReceipt{}, ErrMarketState
	}
	expired, err := r.Bool()
	if err != nil || r.Done() != nil {
		return SettlementReceipt{}, ErrMarketState
	}
	var jobID types.JobID
	var resultRoot types.Hash
	copy(jobID[:], jobRaw)
	copy(resultRoot[:], rootRaw)
	if types.IsZero32([32]byte(jobID)) {
		return SettlementReceipt{}, ErrMarketState
	}
	out := SettlementReceipt{
		JobID: jobID, ResultRoot: resultRoot, Payments: payments, Refund: refund,
		Slashed: slashed, SlashReward: slashReward, Expired: expired,
	}
	if expired {
		if !types.IsZero32([32]byte(resultRoot)) || len(payments) != 0 || len(slashed) != 0 || slashReward != 0 {
			return SettlementReceipt{}, ErrMarketState
		}
	} else if types.IsZero32([32]byte(resultRoot)) || len(payments) == 0 {
		return SettlementReceipt{}, ErrMarketState
	}
	return out, nil
}

func readAccountAmounts(r *codec.Reader) (map[types.AccountID]uint64, error) {
	count, err := r.U32()
	if err != nil || count > 1024 {
		return nil, ErrMarketState
	}
	out := make(map[types.AccountID]uint64, int(count))
	for i := uint32(0); i < count; i++ {
		raw, err := r.Fixed(32)
		if err != nil {
			return nil, ErrMarketState
		}
		amount, err := r.U64()
		if err != nil || amount == 0 {
			return nil, ErrMarketState
		}
		var account types.AccountID
		copy(account[:], raw)
		if types.IsZero32([32]byte(account)) {
			return nil, ErrMarketState
		}
		if _, duplicate := out[account]; duplicate {
			return nil, ErrMarketState
		}
		out[account] = amount
	}
	return out, nil
}

// ObserveFinalizedSettlement verifies that a settlement receipt exactly matches
// the deterministic on-chain settlement for the supplied pre-finalization job
// record, then returns the ZCPI-eligible VerifiedWork observation.
func ObserveFinalizedSettlement(record OnChainJob, receipt SettlementReceipt, registry *WorkRegistry) (VerifiedWork, error) {
	if registry == nil || receipt.Expired || receipt.JobID != record.ID || record.Status != JobAwaitingVerification {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}

	updated, settlement, err := FinalizeOnChain(record, VerificationEvidence{})
	if err != nil {
		updated, settlement, err = ResolveReplicatedMajority(record)
		if err != nil {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
	}
	expected := SettlementReceipt{
		JobID: record.ID, ResultRoot: settlement.ResultRoot, Payments: settlement.Payments,
		Refund: settlement.Refund, Slashed: settlement.SlashedCollateral, SlashReward: settlement.SlashReward,
	}
	if !bytes.Equal(receipt.MarshalBinary(), expected.MarshalBinary()) {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	return ObserveVerifiedWork(updated, settlement, registry)
}

// SettlementPaid returns the amount actually paid to compute providers. Refunds
// and collateral movements are deliberately excluded from ZCPI pricing.
func SettlementPaid(receipt SettlementReceipt) (uint64, error) {
	var paid uint64
	for _, amount := range receipt.Payments {
		if math.MaxUint64-paid < amount {
			return 0, ErrMarketEscrow
		}
		paid += amount
	}
	return paid, nil
}

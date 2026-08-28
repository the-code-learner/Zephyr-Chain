package economics

import (
	"bytes"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/compute"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/execution"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// FinalizedComputeProductiveEvidence is a narrow, read-only evidence record for
// productive-capital accounting. It deliberately separates the job owner's
// compute escrow from provider collateral and from refund flows. The record is
// derived only from the finalized transaction intent, the consumed pre-settle
// job object and the canonical settlement receipt emitted by execution.
//
// This evidence does not mutate IdleCapitalTracker. In particular, callers must
// not mark an entire compute job object productive: that object can also carry
// provider collateral, which is economically distinct from paid compute spend.
type FinalizedComputeProductiveEvidence struct {
	JobObjectID types.ObjectID
	JobID       types.JobID
	Escrow      uint64
	Paid        uint64
	CoverageBps uint32
	ResultRoot  types.Hash
	Verification compute.VerificationMode
}

// DeriveFinalizedComputeProductiveEvidence verifies one finalized compute
// settlement and returns the capital quantities that are safe to feed into a
// future productive-lineage adapter. Non-settlement operations are ignored.
//
// The function fails closed when the operation does not consume the referenced
// compute job, when the pre-settlement witness is ambiguous, or when the
// canonical receipt differs from the deterministic on-chain settlement.
func DeriveFinalizedComputeProductiveEvidence(
	transaction tx.Transaction,
	result execution.Result,
) (FinalizedComputeProductiveEvidence, bool, error) {
	if result.TxID != transaction.ID() {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	if len(transaction.Operations) != 1 {
		return FinalizedComputeProductiveEvidence{}, false, nil
	}

	op := transaction.Operations[0]
	if op.Kind != tx.OpComputeFinalize && op.Kind != tx.OpComputeResolveReplicated {
		return FinalizedComputeProductiveEvidence{}, false, nil
	}
	ref, err := compute.ParseJobRef(op.Payload)
	if err != nil {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}

	var record compute.OnChainJob
	found := false
	for _, witness := range transaction.Witnesses {
		if witness.Object.ID != ref.JobObject {
			continue
		}
		if found || witness.Object.Kind != object.KindComputeJob || witness.Object.Owner != transaction.Sender {
			return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
		}
		parsed, err := compute.ParseOnChainJob(witness.Object.Data)
		if err != nil || parsed.Job.Owner != transaction.Sender {
			return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
		}
		record = parsed
		found = true
	}
	if !found {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}

	consumedCount := 0
	for _, id := range result.Consumed {
		if id == ref.JobObject {
			consumedCount++
		}
	}
	if consumedCount != 1 {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}

	var settlement compute.OnChainSettlement
	switch op.Kind {
	case tx.OpComputeFinalize:
		// This mirrors execution.executeComputeFinalize: the direct finalize
		// path is currently restricted to replicated jobs with an exact match.
		if record.Job.Verification != compute.VerificationReplicated {
			return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
		}
		_, settlement, err = compute.FinalizeOnChain(record, compute.VerificationEvidence{})
	case tx.OpComputeResolveReplicated:
		_, settlement, err = compute.ResolveReplicatedMajority(record)
	}
	if err != nil {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}

	expected := compute.SettlementReceipt{
		JobID:       record.ID,
		ResultRoot:  settlement.ResultRoot,
		Payments:    settlement.Payments,
		Refund:      settlement.Refund,
		Slashed:     settlement.SlashedCollateral,
		SlashReward: settlement.SlashReward,
	}
	receipt, ok, err := uniqueSettlementReceiptForJob(result.Created, record.ID)
	if err != nil || !ok || !bytes.Equal(receipt.MarshalBinary(), expected.MarshalBinary()) {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	paid, err := compute.SettlementPaid(receipt)
	if err != nil || paid == 0 || paid > record.Escrow {
		return FinalizedComputeProductiveEvidence{}, false, ErrFinalizedEconomics
	}
	coverage, err := productiveCoverageBps(paid, record.Escrow)
	if err != nil {
		return FinalizedComputeProductiveEvidence{}, false, err
	}

	return FinalizedComputeProductiveEvidence{
		JobObjectID: ref.JobObject,
		JobID:       record.ID,
		Escrow:      record.Escrow,
		Paid:        paid,
		CoverageBps: coverage,
		ResultRoot:  receipt.ResultRoot,
		Verification: record.Job.Verification,
	}, true, nil
}

func uniqueSettlementReceiptForJob(created []object.Object, jobID types.JobID) (compute.SettlementReceipt, bool, error) {
	var out compute.SettlementReceipt
	found := false
	for _, createdObject := range created {
		if createdObject.Kind != object.KindSystem {
			continue
		}
		receipt, err := compute.ParseSettlementReceipt(createdObject.Data)
		if err != nil || receipt.JobID != jobID {
			continue
		}
		if found {
			return compute.SettlementReceipt{}, false, ErrFinalizedEconomics
		}
		out = receipt
		found = true
	}
	return out, found, nil
}

func productiveCoverageBps(paid, escrow uint64) (uint32, error) {
	if paid == 0 || escrow == 0 || paid > escrow {
		return 0, ErrFinalizedEconomics
	}
	numerator := new(big.Int).SetUint64(paid)
	numerator.Mul(numerator, big.NewInt(10_000))
	numerator.Quo(numerator, new(big.Int).SetUint64(escrow))
	if !numerator.IsUint64() || numerator.Uint64() > 10_000 {
		return 0, ErrFinalizedEconomics
	}
	return uint32(numerator.Uint64()), nil
}

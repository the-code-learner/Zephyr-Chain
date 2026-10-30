package economics

import (
	"bytes"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const FinalizedProviderComputeEvidenceSetVersion uint16 = 1

// HashFinalizedProviderComputeEvidenceSet commits to the complete accepted,
// paid provider evidence set for exactly one finalized compute settlement.
//
// It is a canonical commitment over already-derived records, not an independent
// verifier of transaction finality or a contribution score. A caller must first
// derive the records from a trusted finalized transaction/result pair and its
// protocol WorkRegistry. In particular, this function must not be used to
// authenticate arbitrary records supplied by an RPC or a provider.
//
// Records are sorted by provider account without mutating the caller's slice.
// Empty, incomplete, duplicated or mixed-settlement sets fail closed. There is
// intentionally no sum of payments or replicated work units.
func HashFinalizedProviderComputeEvidenceSet(records []FinalizedProviderComputeEvidence) (types.Hash, error) {
	if len(records) == 0 || uint64(len(records)) > uint64(^uint32(0)) {
		return types.Hash{}, ErrFinalizedEconomics
	}

	ordered := append([]FinalizedProviderComputeEvidence(nil), records...)
	sort.Slice(ordered, func(i, j int) bool {
		return bytes.Compare(ordered[i].Provider[:], ordered[j].Provider[:]) < 0
	})

	first := ordered[0]
	if err := first.Validate(); err != nil {
		return types.Hash{}, ErrFinalizedEconomics
	}
	if first.PaidProviderCount != uint32(len(ordered)) {
		return types.Hash{}, ErrFinalizedEconomics
	}

	var w codec.Writer
	w.U16(FinalizedProviderComputeEvidenceSetVersion)
	w.U32(uint32(len(ordered)))
	for i, record := range ordered {
		if err := record.Validate(); err != nil {
			return types.Hash{}, ErrFinalizedEconomics
		}
		if i > 0 && record.Provider == ordered[i-1].Provider {
			return types.Hash{}, ErrFinalizedEconomics
		}
		if record.JobObjectID != first.JobObjectID ||
			record.JobID != first.JobID ||
			record.JobOwner != first.JobOwner ||
			record.WorkloadHash != first.WorkloadHash ||
			record.BenchmarkHash != first.BenchmarkHash ||
			record.Class != first.Class ||
			record.Units != first.Units ||
			record.Vector != first.Vector ||
			record.Verification != first.Verification ||
			record.ResultRoot != first.ResultRoot ||
			record.AssignedProviderCount != first.AssignedProviderCount ||
			record.PaidProviderCount != first.PaidProviderCount ||
			record.JobStateHash != first.JobStateHash ||
			record.SettlementReceiptHash != first.SettlementReceiptHash ||
			record.WorkRegistryHash != first.WorkRegistryHash {
			return types.Hash{}, ErrFinalizedEconomics
		}
		raw, err := record.CanonicalBytes()
		if err != nil {
			return types.Hash{}, ErrFinalizedEconomics
		}
		w.Bytes(raw)
	}
	return types.Hash(codec.DomainHash("zephyr/finalized-provider-compute-evidence-set/v1", w.BytesCopy())), nil
}

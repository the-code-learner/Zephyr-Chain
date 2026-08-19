package compute

import (
	"math"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// ObserveSettledRecord reconstructs an index-eligible VerifiedWork observation
// from finalized compute state alone. No provider offer price or external RPC
// settlement summary is trusted. For replicated jobs, only providers on the
// strict-majority result root are counted as paid, matching the on-chain
// majority settlement rule.
func ObserveSettledRecord(record OnChainJob, registry *WorkRegistry) (VerifiedWork, error) {
	if record.Status != JobSettled || registry == nil || len(record.Assignments) == 0 || len(record.Results) == 0 {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	spec, ok := registry.Resolve(record.Job.WorkloadHash)
	if !ok || spec.Validate() != nil {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}

	resultByProvider := make(map[types.AccountID]types.Hash, len(record.Results))
	rootCounts := make(map[types.Hash]int, len(record.Results))
	for _, result := range record.Results {
		if err := result.Validate(); err != nil || result.JobID != record.ID {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		if _, duplicate := resultByProvider[result.Provider]; duplicate {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		resultByProvider[result.Provider] = result.ResultRoot
		rootCounts[result.ResultRoot]++
	}

	var acceptedRoot types.Hash
	acceptedCount := 0
	for root, count := range rootCounts {
		if count > acceptedCount {
			acceptedRoot = root
			acceptedCount = count
		}
	}
	if types.IsZero32([32]byte(acceptedRoot)) {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}
	if record.Job.Verification == VerificationReplicated {
		if acceptedCount*2 <= len(record.Results) {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
	} else if acceptedCount != len(record.Results) {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}

	var paid uint64
	seenAssignments := make(map[types.AccountID]struct{}, len(record.Assignments))
	for _, assignment := range record.Assignments {
		if _, duplicate := seenAssignments[assignment.Provider]; duplicate || assignment.Price == 0 {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		seenAssignments[assignment.Provider] = struct{}{}
		root, hasResult := resultByProvider[assignment.Provider]
		if !hasResult {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		if record.Job.Verification == VerificationReplicated && root != acceptedRoot {
			continue
		}
		if math.MaxUint64-paid < assignment.Price {
			return VerifiedWork{}, ErrInvalidWorkSettlement
		}
		paid += assignment.Price
	}
	if paid == 0 || paid > record.Escrow {
		return VerifiedWork{}, ErrInvalidWorkSettlement
	}

	return VerifiedWork{
		JobID:        record.ID,
		Class:        spec.Class,
		Units:        spec.Units,
		Vector:       spec.Vector,
		PaidZPH:      paid,
		Verification: record.Job.Verification,
		ResultRoot:   acceptedRoot,
	}, nil
}

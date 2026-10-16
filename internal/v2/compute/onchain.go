package compute

import (
	"math"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type OnChainSettlement struct {
	Settlement
	CollateralReturns map[types.AccountID]uint64
	SlashedCollateral map[types.AccountID]uint64
	SlashReward       uint64
}

func AssignOnChain(record OnChainJob, offerID types.Hash, offer Offer, height uint64) (OnChainJob, Assignment, uint64, error) {
	if record.Status == JobSettled || record.Status == JobExpired || height == 0 || height > record.Job.DeadlineHeight || height > offer.ValidUntilHeight {
		return OnChainJob{}, Assignment{}, 0, ErrMarketState
	}
	if !offerMatchesJob(offer, record.Job) || offer.Collateral < record.Job.CollateralRequired {
		return OnChainJob{}, Assignment{}, 0, ErrMarketMatch
	}
	for _, existing := range record.Assignments {
		if existing.Provider == offer.Provider {
			return OnChainJob{}, Assignment{}, 0, ErrMarketDuplicate
		}
	}
	target := requiredAssignments(record.Job)
	if len(record.Assignments) >= target {
		return OnChainJob{}, Assignment{}, 0, ErrMarketState
	}
	var committed uint64
	for _, existing := range record.Assignments {
		if math.MaxUint64-committed < existing.Price {
			return OnChainJob{}, Assignment{}, 0, ErrMarketEscrow
		}
		committed += existing.Price
	}
	if offer.PricePerUnit > record.Job.MaxPrice || math.MaxUint64-committed < offer.PricePerUnit || committed+offer.PricePerUnit > record.Job.MaxPrice || committed+offer.PricePerUnit > record.Escrow {
		return OnChainJob{}, Assignment{}, 0, ErrMarketEscrow
	}
	assignment := Assignment{OfferID: offerID, Provider: offer.Provider, Price: offer.PricePerUnit}
	updated := cloneOnChainJob(record)
	updated.Assignments = append(updated.Assignments, assignment)
	if len(updated.Assignments) == target {
		updated.Status = JobAssigned
	}
	return updated, assignment, offer.Collateral - record.Job.CollateralRequired, nil
}

func SubmitOnChainResult(record OnChainJob, result Result) (OnChainJob, error) {
	if err := result.Validate(); err != nil || result.JobID != record.ID {
		return OnChainJob{}, ErrInvalidResult
	}
	if record.Status != JobAssigned && record.Status != JobAwaitingVerification {
		return OnChainJob{}, ErrMarketState
	}
	if result.CompletedHeight > record.Job.DeadlineHeight {
		return OnChainJob{}, ErrMarketState
	}
	assigned := false
	for _, assignment := range record.Assignments {
		if assignment.Provider == result.Provider {
			assigned = true
			break
		}
	}
	if !assigned {
		return OnChainJob{}, ErrMarketMatch
	}
	for _, existing := range record.Results {
		if existing.Provider == result.Provider {
			return OnChainJob{}, ErrMarketDuplicate
		}
	}
	updated := cloneOnChainJob(record)
	updated.Results = append(updated.Results, result)
	if len(updated.Results) == requiredAssignments(updated.Job) {
		updated.Status = JobAwaitingVerification
	}
	return updated, nil
}

func FinalizeOnChain(record OnChainJob, evidence VerificationEvidence) (OnChainJob, OnChainSettlement, error) {
	if record.Status != JobAwaitingVerification || len(record.Results) != requiredAssignments(record.Job) {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketState
	}
	results := make(map[types.AccountID]Result, len(record.Results))
	for _, result := range record.Results {
		results[result.Provider] = result
	}
	root, replicatedMatch := commonResultRootRecord(record.Results)
	if types.IsZero32([32]byte(root)) || !verificationSatisfied(record.Job.Verification, replicatedMatch, results, evidence) {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketVerification
	}
	payments := make(map[types.AccountID]uint64, len(record.Assignments))
	collateral := make(map[types.AccountID]uint64, len(record.Assignments))
	var paid uint64
	for _, assignment := range record.Assignments {
		if math.MaxUint64-paid < assignment.Price {
			return OnChainJob{}, OnChainSettlement{}, ErrMarketEscrow
		}
		paid += assignment.Price
		payments[assignment.Provider] += assignment.Price
		collateral[assignment.Provider] += record.Job.CollateralRequired
	}
	if paid > record.Escrow {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketEscrow
	}
	updated := cloneOnChainJob(record)
	updated.Status = JobSettled
	return updated, OnChainSettlement{
		Settlement:        Settlement{JobID: record.ID, ResultRoot: root, Payments: payments, Refund: record.Escrow - paid},
		CollateralReturns: collateral,
		SlashedCollateral: make(map[types.AccountID]uint64),
	}, nil
}

// ResolveReplicatedMajority provides an objective dispute rule for replicated
// jobs. At least three replicas must have reported. Providers on the strict
// majority result root are paid and recover collateral. Minority collateral is
// slashed to the job owner, while unpaid compute budget is refunded.
func ResolveReplicatedMajority(record OnChainJob) (OnChainJob, OnChainSettlement, error) {
	if record.Job.Verification != VerificationReplicated || len(record.Results) < 3 || len(record.Results) != len(record.Assignments) {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketVerification
	}
	counts := make(map[types.Hash]int)
	for _, result := range record.Results {
		counts[result.ResultRoot]++
	}
	var majority types.Hash
	majorityCount := 0
	for root, count := range counts {
		if count > majorityCount {
			majority, majorityCount = root, count
		}
	}
	if majorityCount*2 <= len(record.Results) {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketVerification
	}
	resultByProvider := make(map[types.AccountID]types.Hash, len(record.Results))
	for _, result := range record.Results {
		resultByProvider[result.Provider] = result.ResultRoot
	}
	payments := make(map[types.AccountID]uint64)
	collateralReturns := make(map[types.AccountID]uint64)
	slashed := make(map[types.AccountID]uint64)
	var paid, slashReward uint64
	for _, assignment := range record.Assignments {
		if resultByProvider[assignment.Provider] == majority {
			if math.MaxUint64-paid < assignment.Price {
				return OnChainJob{}, OnChainSettlement{}, ErrMarketEscrow
			}
			paid += assignment.Price
			payments[assignment.Provider] += assignment.Price
			collateralReturns[assignment.Provider] += record.Job.CollateralRequired
		} else {
			slashed[assignment.Provider] += record.Job.CollateralRequired
			if math.MaxUint64-slashReward < record.Job.CollateralRequired {
				return OnChainJob{}, OnChainSettlement{}, ErrMarketEscrow
			}
			slashReward += record.Job.CollateralRequired
		}
	}
	if paid > record.Escrow {
		return OnChainJob{}, OnChainSettlement{}, ErrMarketEscrow
	}
	updated := cloneOnChainJob(record)
	updated.Status = JobSettled
	return updated, OnChainSettlement{
		Settlement:        Settlement{JobID: record.ID, ResultRoot: majority, Payments: payments, Refund: record.Escrow - paid},
		CollateralReturns: collateralReturns,
		SlashedCollateral: slashed,
		SlashReward:       slashReward,
	}, nil
}

func ExpireOnChain(record OnChainJob, height uint64) (OnChainJob, uint64, map[types.AccountID]uint64, error) {
	if record.Status == JobSettled || record.Status == JobExpired || height <= record.Job.DeadlineHeight {
		return OnChainJob{}, 0, nil, ErrMarketState
	}
	updated := cloneOnChainJob(record)
	updated.Status = JobExpired
	collateral := make(map[types.AccountID]uint64, len(record.Assignments))
	for _, assignment := range record.Assignments {
		collateral[assignment.Provider] += record.Job.CollateralRequired
	}
	return updated, record.Escrow, collateral, nil
}

func cloneOnChainJob(record OnChainJob) OnChainJob {
	out := record
	out.Assignments = append([]Assignment(nil), record.Assignments...)
	out.Results = append([]Result(nil), record.Results...)
	return out
}

func commonResultRootRecord(results []Result) (types.Hash, bool) {
	if len(results) == 0 {
		return types.Hash{}, false
	}
	ordered := append([]Result(nil), results...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].Provider.String() < ordered[j].Provider.String() })
	root := ordered[0].ResultRoot
	for _, result := range ordered[1:] {
		if result.ResultRoot != root {
			return root, false
		}
	}
	return root, true
}

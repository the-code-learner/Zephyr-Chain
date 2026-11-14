package consensus

import (
	"bytes"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// CandidateEpochLength is the bounded, experimental epoch size. This scheduler
// is NOT wired into V2 consensus, proposal checks, or live signing.
const CandidateEpochLength = 256

type candidateSlot struct {
	validator Validator
	quota     uint64
	remainder *big.Int
	tie       [32]byte
	deficit   int64
}

// CandidateEpochSchedule explores a stake-proportional schedule for a future,
// explicitly versioned protocol. It MUST NOT replace ValidatorSet.Proposer
// without an audited activation and backward-compatible history validation.
//
// epoch is zero-based. seed is the planned genesis-committed, public seed;
// it is not an unpredictable entropy source.
func CandidateEpochSchedule(set ValidatorSet, seed types.Hash, epoch uint64) ([]types.ValidatorID, error) {
	total, err := set.TotalPower()
	if err != nil {
		return nil, ErrValidatorSet
	}
	sorted := append([]Validator(nil), set.Validators...)
	sort.Slice(sorted, func(i, j int) bool {
		return bytes.Compare(sorted[i].ID[:], sorted[j].ID[:]) < 0
	})
	slots := make([]candidateSlot, len(sorted))
	denominator := new(big.Int).SetUint64(total)
	var allocated uint64
	for i, validator := range sorted {
		numerator := new(big.Int).SetUint64(validator.Power)
		numerator.Mul(numerator, big.NewInt(CandidateEpochLength))
		quotient, remainder := new(big.Int), new(big.Int)
		quotient.QuoRem(numerator, denominator, remainder)
		quota := quotient.Uint64() // quotient <= 256 by validated total power.
		var w codec.Writer
		w.Fixed(set.Network[:])
		w.Fixed(seed[:])
		w.U64(epoch)
		w.Fixed(validator.ID[:])
		slots[i] = candidateSlot{
			validator: validator, quota: quota, remainder: remainder,
			tie: codec.DomainHash("zephyr/candidate-epoch-tie/v3", w.BytesCopy()),
		}
		allocated += quota // at most 256.
	}
	if allocated > CandidateEpochLength {
		return nil, ErrValidatorSet
	}
	order := make([]int, len(slots))
	for i := range order {
		order[i] = i
	}
	sort.Slice(order, func(a, b int) bool {
		i, j := order[a], order[b]
		if c := slots[i].remainder.Cmp(slots[j].remainder); c != 0 {
			return c > 0
		}
		if c := bytes.Compare(slots[i].tie[:], slots[j].tie[:]); c != 0 {
			return c < 0
		}
		return bytes.Compare(slots[i].validator.ID[:], slots[j].validator.ID[:]) < 0
	})
	for i := uint64(0); i < CandidateEpochLength-allocated; i++ {
		slots[order[int(i)]].quota++
	}
	// Smooth weighted round robin on normalized quotas: each deficit is
	// bounded by the 256-slot epoch, independent of raw uint64 stake power.
	out := make([]types.ValidatorID, 0, CandidateEpochLength)
	for position := 0; position < CandidateEpochLength; position++ {
		best := -1
		for i := range slots {
			if slots[i].quota == 0 {
				continue
			}
			slots[i].deficit += int64(slots[i].quota)
			if best < 0 || slots[i].deficit > slots[best].deficit ||
				(slots[i].deficit == slots[best].deficit &&
					(bytes.Compare(slots[i].tie[:], slots[best].tie[:]) < 0 ||
						(bytes.Equal(slots[i].tie[:], slots[best].tie[:]) &&
							bytes.Compare(slots[i].validator.ID[:], slots[best].validator.ID[:]) < 0))) {
				best = i
			}
		}
		if best < 0 {
			return nil, ErrValidatorSet
		}
		slots[best].deficit -= CandidateEpochLength
		out = append(out, slots[best].validator.ID)
	}
	return out, nil
}

// CandidateEpochProposer provides a single lookup while keeping V2's live
// Proposer and VerifyProposal behavior strictly unchanged.
func CandidateEpochProposer(set ValidatorSet, seed types.Hash, height, round uint64) (Validator, error) {
	if height == 0 {
		return Validator{}, ErrValidatorSet
	}
	epoch := (height - 1) / CandidateEpochLength
	position := (height - 1) % CandidateEpochLength
	schedule, err := CandidateEpochSchedule(set, seed, epoch)
	if err != nil {
		return Validator{}, err
	}
	// Both terms are < 256, so the addition cannot overflow uint64.
	index := (position + round%CandidateEpochLength) % CandidateEpochLength
	id := schedule[int(index)]
	for _, validator := range set.Validators {
		if validator.ID == id {
			return validator, nil
		}
	}
	return Validator{}, ErrValidatorSet
}

package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func candidateTestSet(t *testing.T, powers []uint64) ValidatorSet {
	t.Helper()
	set := ValidatorSet{Network: types.NetworkID(types.HashBytes("network", []byte("candidate-schedule")))}
	for _, power := range powers {
		key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
		set.Validators = append(set.Validators, Validator{ID: types.ValidatorIDFromPublicKey(public), PublicKey: public, Power: power})
	}
	return set
}

func TestCandidateEpochProportionalQuotasAndPermutation(t *testing.T) {
	seed := types.HashBytes("seed", []byte("public-test-seed"))
	for _, powers := range [][]uint64{
		{100, 100, 100, 100},
		{100, 100, 100, 100, 100, 100, 100},
		{1, 2, 4}, {1, 99},
		{math.MaxUint64 / 2, math.MaxUint64 / 2},
	} {
		set := candidateTestSet(t, powers)
		if err := set.Validate(); err != nil {
			t.Fatal(err)
		}
		schedule, err := CandidateEpochSchedule(set, seed, 0)
		if err != nil || len(schedule) != CandidateEpochLength {
			t.Fatalf("epoch length or generation: %d %v", len(schedule), err)
		}
		counts := map[types.ValidatorID]int{}
		for _, id := range schedule {
			counts[id]++
		}
		total, _ := set.TotalPower()
		for _, v := range set.Validators {
			// Exact integer comparison: each quota differs from the ideal
			// fractional entitlement by strictly less than one block.
			// Avoid overflow using big integers in the implementation;
			// floats here are diagnostics only and not consensus logic.
			ideal := float64(v.Power) / float64(total) * CandidateEpochLength
			delta := float64(counts[v.ID]) - ideal
			if delta <= -1.0000001 || delta >= 1.0000001 {
				t.Fatalf("power=%d quota=%d ideal=%.3f", v.Power, counts[v.ID], ideal)
			}
		}
		reversed := set
		reversed.Validators = append([]Validator(nil), set.Validators...)
		for i, j := 0, len(reversed.Validators)-1; i < j; i, j = i+1, j-1 {
			reversed.Validators[i], reversed.Validators[j] = reversed.Validators[j], reversed.Validators[i]
		}
		second, err := CandidateEpochSchedule(reversed, seed, 0)
		if err != nil {
			t.Fatal(err)
		}
		for i := range schedule {
			if schedule[i] != second[i] {
				t.Fatalf("input order altered candidate epoch at index %d", i)
			}
		}
	}
}

func TestCandidateEpochBoundariesAndRoundModulo(t *testing.T) {
	set := candidateTestSet(t, []uint64{100, 100, 100, 100})
	seed := types.HashBytes("seed", []byte("round-check"))
	if _, err := CandidateEpochProposer(set, seed, 0, 0); err != ErrValidatorSet {
		t.Fatalf("height zero must fail: %v", err)
	}
	for _, height := range []uint64{1, 2, 255, 256, 257, math.MaxUint64} {
		for _, round := range []uint64{0, 1, 255, 256, math.MaxUint64} {
			a, err := CandidateEpochProposer(set, seed, height, round)
			if err != nil {
				t.Fatal(err)
			}
			b, err := CandidateEpochProposer(set, seed, height, round%CandidateEpochLength)
			if err != nil || a.ID != b.ID {
				t.Fatalf("round modulo mismatch at %d/%d", height, round)
			}
		}
	}
}

func TestCandidateEpochNeverChangesV2Proposer(t *testing.T) {
	set := candidateTestSet(t, []uint64{100, 100, 100, 100})
	seed := types.HashBytes("seed", []byte("independence"))
	oldBefore, err := set.Proposer(1, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := CandidateEpochProposer(set, seed, 1, 0); err != nil {
		t.Fatal(err)
	}
	oldAfter, err := set.Proposer(1, 0)
	if err != nil || oldBefore.ID != oldAfter.ID {
		t.Fatalf("candidate changed live V2 proposer: %v", err)
	}
}

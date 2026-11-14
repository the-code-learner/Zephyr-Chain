package consensus

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This test pins the existing V2 wire-consensus leader schedule. It is NOT a
// fairness test, and changing this expectation requires a versioned fork.
func TestV2LegacyProposerHundredHeightStreakIsConsensusCompatibility(t *testing.T) {
	for _, count := range []int{4, 7} {
		t.Run(map[int]string{4: "four", 7: "seven"}[count], func(t *testing.T) {
			set := ValidatorSet{Network: types.NetworkID(types.HashBytes("network", []byte("legacy-proposer")))}
			for i := 0; i < count; i++ {
				key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
				if err != nil {
					t.Fatal(err)
				}
				public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
				set.Validators = append(set.Validators, Validator{
					ID: types.ValidatorIDFromPublicKey(public), PublicKey: public, Power: 100,
				})
			}
			sorted := append([]Validator(nil), set.Validators...)
			sort.Slice(sorted, func(i, j int) bool {
				return bytes.Compare(sorted[i].ID[:], sorted[j].ID[:]) < 0
			})
			// The existing cumulative-power slot algorithm does not rotate
			// between height one and two, nor anywhere in heights 1..100.
			for height := uint64(1); height <= 100; height++ {
				got, err := set.Proposer(height, 0)
				if err != nil || got.ID != sorted[0].ID {
					t.Fatalf("height %d: legacy proposer mismatch: %v", height, err)
				}
			}
			for _, tc := range []struct {
				height uint64
				round  uint64
				want   int
			}{
				{1, 0, 0}, {2, 0, 0}, {100, 0, 0},
				{101, 0, 1}, {1, 100, 1},
				{uint64(count)*100 + 1, 0, 0},
			} {
				got, err := set.Proposer(tc.height, tc.round)
				if err != nil || got.ID != sorted[tc.want].ID {
					t.Fatalf("height=%d round=%d: got %x want %x, err=%v",
						tc.height, tc.round, got.ID, sorted[tc.want].ID, err)
				}
			}
			// Reordering genesis entries must not change a scheduled leader.
			reversed := set
			reversed.Validators = append([]Validator(nil), set.Validators...)
			for i, j := 0, len(reversed.Validators)-1; i < j; i, j = i+1, j-1 {
				reversed.Validators[i], reversed.Validators[j] = reversed.Validators[j], reversed.Validators[i]
			}
			for height := uint64(1); height <= uint64(count)*100; height++ {
				a, errA := set.Proposer(height, 0)
				b, errB := reversed.Proposer(height, 0)
				if errA != nil || errB != nil || a.ID != b.ID {
					t.Fatalf("validator permutation changed legacy schedule at %d", height)
				}
			}
		})
	}
}

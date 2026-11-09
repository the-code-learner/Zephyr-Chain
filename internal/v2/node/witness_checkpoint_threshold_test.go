//go:build linux || darwin

package node

import (
	"crypto/ed25519"
	"errors"
	"fmt"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Verify the strict >2/3 rule across policy sizes instead of assuming that
// the 3-of-4 witness fixture is representative of every supported committee.
// Distinct fixture keys are deterministic and never used outside tests.
func TestWitnessCheckpointStrictSupermajorityCommitteeSizes(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-threshold-matrix", []byte("network")))
	root := types.HashBytes("witness-threshold-matrix", []byte("validator-root"))
	checkpoint := FinalityCheckpoint{
		Network:       network,
		ValidatorRoot: root,
		Height:        14,
		HeaderHash:    types.HashBytes("witness-threshold-matrix", []byte("header")),
	}
	const maxFixtureKeys = 17 // 17 is deliberately beyond the 16-key policy maximum.
	keys := make([][]byte, maxFixtureKeys)
	privates := make([]ed25519.PrivateKey, maxFixtureKeys)
	for i := range keys {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(i + 1)
		seed[1] = 0xA5
		privates[i] = ed25519.NewKeyFromSeed(seed)
		keys[i] = append([]byte(nil), privates[i].Public().(ed25519.PublicKey)...)
	}
	for _, size := range []int{3, 4, 5, 6, 7, 10, 16} {
		t.Run(fmt.Sprintf("committee-%d", size), func(t *testing.T) {
			threshold := (2*size)/3 + 1
			for _, count := range []int{threshold - 1, threshold, size} {
				bundle := signedWitnessTestBundle(t, checkpoint, keys[:size], privates[:size], count)
				got, err := ParseSignedWitnessCheckpoint(bundle, keys[:size], network, root)
				if count < threshold {
					if !errors.Is(err, ErrWitnessCheckpoint) || got != nil {
						t.Fatalf("accepted %d/%d signatures below strict threshold %d: checkpoint=%+v err=%v", count, size, threshold, got, err)
					}
					continue
				}
				if err != nil || got == nil || *got != checkpoint {
					t.Fatalf("rejected %d/%d valid signatures (threshold %d): checkpoint=%+v err=%v", count, size, threshold, got, err)
				}
			}
		})
	}
	for _, size := range []int{2, 17} {
		t.Run(fmt.Sprintf("unsupported-committee-%d", size), func(t *testing.T) {
			bundle := signedWitnessTestBundle(t, checkpoint, keys[:size], privates[:size], size)
			if got, err := ParseSignedWitnessCheckpoint(bundle, keys[:size], network, root); !errors.Is(err, ErrWitnessCheckpoint) || got != nil {
				t.Fatalf("accepted unsupported %d-key policy: checkpoint=%+v err=%v", size, got, err)
			}
		})
	}
}

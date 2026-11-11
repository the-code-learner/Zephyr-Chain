//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A second descriptor can edit the canonical inode without acquiring its
// advisory flock. The live custody process must fail closed before invoking
// either private signing operation, including on idempotent checkpoint retry.
func TestLocalWitnessCustodyRejectsInPlaceAnchorMutationBeforeSigning(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*testing.T, string, []byte)
	}{
		{"truncate", func(t *testing.T, path string, raw []byte) {
			t.Helper()
			if err := os.Truncate(path, int64(len(raw)-1)); err != nil {
				t.Fatal(err)
			}
		}},
		{"append", func(t *testing.T, path string, _ []byte) {
			t.Helper()
			f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte{0x99}); err != nil {
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{"rewrite_header", func(t *testing.T, path string, _ []byte) {
			writeAnchorTestByte(t, path, 0, 0x00)
		}},
		{"rewrite_earlier_record", func(t *testing.T, path string, _ []byte) {
			// The last record stays unchanged. Checking only its size and
			// footer would miss a previously certified record's corruption.
			writeAnchorTestByte(t, path, finalityAnchorHeaderSize+8, 0xe7)
		}},
		{"rewrite_last_digest", func(t *testing.T, path string, raw []byte) {
			writeAnchorTestByte(t, path, int64(len(raw)-1), 0x00)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			authority, witness := witnessAtomicTestKeys()
			network := types.NetworkID(types.HashBytes("anchor-in-place", []byte(tc.name)))
			root := types.HashBytes("anchor-in-place-root", []byte(tc.name))
			first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 7, HeaderHash: types.Hash{1}}
			second := first
			second.Height = 8
			second.HeaderHash = types.Hash{2}
			third := first
			third.Height = 9
			third.HeaderHash = types.Hash{3}
			path := privateAnchorTestPath(t)
			signer := &localCustodyTestSigner{witness: witness, authority: authority}
			verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
				first.Height: first.HeaderHash, second.Height: second.HeaderHash, third.Height: third.HeaderHash,
			}}
			backend, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer,
				witness.Public().(ed25519.PublicKey), authority.Public().(ed25519.PublicKey))
			if err != nil {
				t.Fatal(err)
			}
			defer backend.Close()
			for _, cp := range []FinalityCheckpoint{first, second} {
				if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
					t.Fatalf("baseline signing height=%d: %v", cp.Height, err)
				}
			}
			baseline, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			before := signer.calls
			tc.mutate(t, path, baseline)
			for _, cp := range []FinalityCheckpoint{second, third} {
				if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
					t.Fatalf("mutated inode allowed signing height=%d: err=%v signer calls=%d (want %d)", cp.Height, err, signer.calls, before)
				}
			}
			if _, err := backend.Current(); !errors.Is(err, ErrFinalityAnchor) {
				t.Fatalf("mutated anchor exposed trusted current checkpoint: %v", err)
			}
			// Even if the original exact bytes are put back on the same inode,
			// the unsafe process handle must not resume signing.
			if err := os.WriteFile(path, baseline, 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), third, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
				t.Fatalf("restoration unpoisoned custody: err=%v signer calls=%d", err, signer.calls)
			}
		})
	}
}

func writeAnchorTestByte(t *testing.T, path string, offset int64, value byte) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte{value}, offset); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

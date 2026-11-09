//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// The bytes are untrusted. Unlike a mock floor that returns a checkpoint,
// QuorumAuthenticatedWitnessFloor validates the canonical signed envelope
// against separately pinned witness keys on EACH request.
type mutableSignedCustodyFloorSource struct {
	payload []byte
}

func (s *mutableSignedCustodyFloorSource) SignedWitnessFloor() ([]byte, error) {
	return append([]byte(nil), s.payload...), nil
}

func TestLocalWitnessCustodySignedQuorumFloorRejectsSnapshotRollback(t *testing.T) {
	network := types.NetworkID(types.HashBytes("signed-custody-recovery", []byte("network")))
	root := types.HashBytes("signed-custody-recovery", []byte("validators"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 101, HeaderHash: types.Hash{1}}
	next := first
	next.Height++
	next.HeaderHash = types.Hash{2}
	keys, private := witnessTestKeys()
	source := &mutableSignedCustodyFloorSource{payload: signedWitnessTestBundle(t, first, keys, private, 3)}
	remoteFloor, err := NewQuorumAuthenticatedWitnessFloor(source, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	authority, witness := witnessAtomicTestKeys()
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		first.Height: first.HeaderHash, next.Height: next.HeaderHash,
	}}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "local-highwater")
	base, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := base.ReserveAndSignWitnessCheckpoint(context.Background(), first, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := base.Close(); err != nil {
		t.Fatal(err)
	}
	oldSnapshot, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	guarded, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remoteFloor)
	if err != nil {
		t.Fatalf("valid independent quorum floor denied recovery: %v", err)
	}
	// The 3-of-4 signed floor authorizes an advance, but cannot silently
	// authorize another signer call until independent publication catches up.
	att, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{2})
	if err != nil || att.Revision != 2 {
		t.Fatalf("valid quorum-verified advancement rejected: %+v %v", att, err)
	}
	before := signer.calls
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("stale yet signed witness floor authorized unacknowledged signing: %v", err)
	}
	// A forged envelope with apparently newer height cannot self-authorize:
	// tampering invalidates the Ed25519 witness quorum.
	source.payload = signedWitnessTestBundle(t, next, keys, private, 3)
	source.payload[len(source.payload)-2] ^= 1
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{4}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("corrupted signed remote floor authorized custody signer: %v", err)
	}
	source.payload = signedWitnessTestBundle(t, next, keys, private, 3)
	if _, err := guarded.ReserveAndSignWitnessCheckpoint(context.Background(), next, [32]byte{5}); err != nil {
		t.Fatalf("fresh 3-of-4 quorum floor did not reauthorize idempotent custody: %v", err)
	}
	if err := guarded.Close(); err != nil {
		t.Fatal(err)
	}
	// The restored local file is syntactically valid, hash chained, and
	// correctly fsynced. Only the separately retained newer quorum floor
	// distinguishes rollback from legitimate older progress.
	if err := os.WriteFile(path, oldSnapshot, 0600); err != nil {
		t.Fatal(err)
	}
	before = signer.calls
	if b, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remoteFloor); !errors.Is(err, ErrWitnessSigningIntent) || b != nil {
		t.Fatalf("valid stale snapshot accepted against fresh pinned signed quorum: %v", err)
	}
	if signer.calls != before {
		t.Fatal("rollback recovery touched private-key signing provider")
	}
	// IMPORTANT LIMITATION: if BOTH the local file and the independently
	// authenticated source are restored to the exact same historical tip,
	// this interface cannot detect the coordinated rollback. Mainnet still
	// requires nonrollbackable hardware/global fencing and independent audit.
	source.payload = signedWitnessTestBundle(t, first, keys, private, 3)
	if b, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, remoteFloor); err != nil || b == nil {
		t.Fatalf("matching restored historical floor refused: %v", err)
	} else {
		_ = b.Close()
	}
	// The previous positive control deliberately demonstrates the LIMIT:
	// coordinated rollback of BOTH domains cannot be detected by this mode.
}

//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This test-only reference implementation models one custody authority
// shared by independent requesters. It is not a production HSM, network
// service, or rollback-proof remote storage. The same mutex protects durable
// reservation AND use of both private signing keys.
type witnessDurableCustodyFixture struct {
	mu        sync.Mutex
	store     *FinalityAnchorStore
	verifier  WitnessFinalityVerifier
	authority ed25519.PrivateKey
	witness   ed25519.PrivateKey
	closed    bool
	revision  uint64
}

func (f *witnessDurableCustodyFixture) ReserveAndSignWitnessCheckpoint(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessAtomicCustodyAttestation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed || f.store == nil || f.verifier == nil || ctx.Err() != nil ||
		cp.Validate(cp.Network, cp.ValidatorRoot) != nil || isZeroWitnessFenceChallenge(challenge) {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	// The custody authority itself must verify QC ancestry: never infer
	// finality from the caller's local verification or an untrusted RPC.
	if err := f.verifier.VerifyFinalizedCheckpoint(cp); err != nil {
		return WitnessAtomicCustodyAttestation{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	before, err := f.store.Current()
	if err != nil {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	if err := f.store.advance(cp); err != nil {
		return WitnessAtomicCustodyAttestation{}, errors.Join(ErrWitnessSigningIntent, err)
	}
	after, err := f.store.Current()
	if err != nil || after != cp || ctx.Err() != nil {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	if before != cp {
		f.revision++
	}
	if f.revision == 0 {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	cpDigest, err := WitnessCheckpointDigest(cp)
	if err != nil {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	a := WitnessAtomicCustodyAttestation{
		Checkpoint: cp, Challenge: challenge, Revision: f.revision,
		WitnessPublicKey: append([]byte(nil), f.witness.Public().(ed25519.PublicKey)...),
		WitnessSignature: ed25519.Sign(f.witness, cpDigest[:]),
	}
	digest, err := WitnessAtomicCustodyDigest(a)
	if err != nil {
		return WitnessAtomicCustodyAttestation{}, ErrWitnessSigningIntent
	}
	a.AuthoritySignature = ed25519.Sign(f.authority, digest[:])
	return a, nil
}

func (f *witnessDurableCustodyFixture) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return f.store.Close()
}

func openWitnessDurableCustodyFixture(t *testing.T, path string, cp FinalityCheckpoint, create bool, verifier WitnessFinalityVerifier, authority, witness ed25519.PrivateKey) *witnessDurableCustodyFixture {
	t.Helper()
	store, err := OpenFinalityAnchorStore(path, cp.Network, cp.ValidatorRoot, create)
	if err != nil {
		t.Fatal(err)
	}
	f := &witnessDurableCustodyFixture{store: store, verifier: verifier, authority: authority, witness: witness}
	current, err := store.Current()
	if err != nil {
		t.Fatal(err)
	}
	if current.Height > 0 {
		// This fixture uses one strictly increasing revision per finalized
		// checkpoint, including across close/reopen. Unlike production,
		// revision is reconstructed from an accessible test-only journal.
		f.revision = uint64(store.count)
	}
	return f
}

func TestAtomicWitnessCustodySharedRemoteRejectsConflictingHostSignatures(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("atomic-custody-hosts", []byte("net")))
	root := types.HashBytes("atomic-custody-hosts", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 11, HeaderHash: types.Hash{9}}
	conflict := cp
	conflict.HeaderHash[0]++
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "remote-custody-anchor")
	remote := openWitnessDurableCustodyFixture(t, path, cp, true,
		witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}},
		authority, witness)
	defer remote.Close()

	// Two independent local journals represent two separately restarted
	// requesters. Both can claim finalized history, but the remote custodian
	// independently accepts only its own QC-verified finalized checkpoint.
	j1 := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	j2 := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: conflict.HeaderHash}})
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, errs[0] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j1, cp, remote, keyA, keyW)
	}()
	go func() {
		defer wg.Done()
		_, errs[1] = ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j2, conflict, remote, keyA, keyW)
	}()
	wg.Wait()
	if errs[0] != nil || !errors.Is(errs[1], ErrWitnessSigningIntent) {
		t.Fatalf("shared custody produced conflicting host signing evidence: authorized=%v conflicting=%v", errs[0], errs[1])
	}
	stored, err := remote.store.Current()
	if err != nil || stored != cp {
		t.Fatalf("remote durable custody state changed on equivocation: %+v %v", stored, err)
	}
}

func TestAtomicWitnessCustodyRestartRejectsStaleRestoredHost(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("atomic-custody-restart", []byte("net")))
	root := types.HashBytes("atomic-custody-restart", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 8, HeaderHash: types.Hash{1}}
	second := first
	second.Height = 12
	second.HeaderHash = types.Hash{2}
	directory := t.TempDir()
	if err := os.Chmod(directory, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(directory, "custody-anchor")
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{first.Height: first.HeaderHash, second.Height: second.HeaderHash}}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	j1 := witnessFenceTestJournal(t, first, verifier)
	j2 := witnessFenceTestJournal(t, first, verifier)
	remote := openWitnessDurableCustodyFixture(t, path, first, true, verifier, authority, witness)
	a1, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j1, first, remote, keyA, keyW)
	if err != nil || a1.Revision != 1 {
		t.Fatalf("initial custody reservation failed: %v revision=%d", err, a1.Revision)
	}
	a2, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j2, second, remote, keyA, keyW)
	if err != nil || a2.Revision != 2 {
		t.Fatalf("second custody reservation failed: %v revision=%d", err, a2.Revision)
	}
	if err := remote.Close(); err != nil {
		t.Fatal(err)
	}
	remote = openWitnessDurableCustodyFixture(t, path, first, false, verifier, authority, witness)
	defer remote.Close()
	// The second host still has an older valid local journal, yet the
	// separately retained custody journal must refuse its stale request.
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j1, first, remote, keyA, keyW); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("restored older host was allowed to sign after remote advance: %v", err)
	}
	a3, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j2, second, remote, keyA, keyW)
	if err != nil || a3.Revision != a2.Revision {
		t.Fatalf("same-height idempotent request after remote restart failed: %v revision=%d", err, a3.Revision)
	}
}

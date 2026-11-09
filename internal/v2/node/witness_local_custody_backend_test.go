//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Only these tests possess private keys. LocalWitnessCustodyBackend itself
// sees only public keys and opaque, pluggable signing operations.
type localCustodyTestSigner struct {
	mu sync.Mutex
	witness ed25519.PrivateKey
	authority ed25519.PrivateKey
	failAuthority bool
	badWitness bool
	calls int
}

func (s *localCustodyTestSigner) SignWitnessCheckpointDigest(ctx context.Context, digest [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sig := ed25519.Sign(s.witness, digest[:])
	if s.badWitness {
		sig[0] ^= 1
	}
	return sig, nil
}

func (s *localCustodyTestSigner) SignCustodyAttestationDigest(ctx context.Context, digest [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.failAuthority {
		return nil, ErrWitnessSigningIntent
	}
	return ed25519.Sign(s.authority, digest[:]), nil
}

func TestLocalWitnessCustodyFencesRivalRequestsAcrossRestart(t *testing.T) {
	a, w := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("local-custody", []byte("network")))
	root := types.HashBytes("local-custody", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 10, HeaderHash: types.Hash{8}}
	conflict := cp
	conflict.HeaderHash = types.Hash{9}
	later := cp
	later.Height = 11
	later.HeaderHash = types.Hash{10}
	// This verifier is deliberately permissive about the conflicting same-
	// height claim: the fsynced single-writer store must still fence it.
	verifier := witnessFaultyCustodyVerifier{}
	sign := &localCustodyTestSigner{witness: w, authority: a}
	path := filepath.Join(t.TempDir(), "single-writer-anchor")
	open := func(create bool) *LocalWitnessCustodyBackend {
		t.Helper()
		backend, err := OpenLocalWitnessCustodyBackend(path, network, root, create, verifier, sign, w.Public().(ed25519.PublicKey), a.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		return backend
	}
	backend := open(true)
	if _, err := OpenLocalWitnessCustodyBackend(path, network, root, false, verifier, sign, w.Public().(ed25519.PublicKey), a.Public().(ed25519.PublicKey)); err == nil {
		t.Fatal("another writer acquired already-locked custody anchor")
	}
	var wg sync.WaitGroup
	results := make([]WitnessAtomicCustodyAttestation, 2)
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		results[0], errs[0] = backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1})
	}()
	go func() {
		defer wg.Done()
		results[1], errs[1] = backend.ReserveAndSignWitnessCheckpoint(context.Background(), conflict, [32]byte{2})
	}()
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("custody issued two conflicting signatures or denied both: %v %v", errs[0], errs[1])
	}
	winner := cp
	if errs[0] != nil {
		winner = conflict
	}
	for i, result := range results {
		if errs[i] != nil {
			if !errors.Is(errs[i], ErrWitnessSigningIntent) {
				t.Fatalf("unexpected loser error: %v", errs[i])
			}
			continue
		}
		expectedChallenge := [32]byte{byte(i + 1)}
		if err := VerifyWitnessAtomicCustodyAttestation(result, winner, expectedChallenge, a.Public().(ed25519.PublicKey), w.Public().(ed25519.PublicKey)); err != nil {
			t.Fatalf("winner signature invalid: %v", err)
		}
		if result.Revision != 1 {
			t.Fatalf("expected first revision, got %d", result.Revision)
		}
	}
	if err := backend.Close(); err != nil {
		t.Fatal(err)
	}
	backend = open(false)
	defer backend.Close()
	loser := conflict
	if winner == conflict {
		loser = cp
	}
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), loser, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("equivocation after restart was allowed: %v", err)
	}
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{4}); err != nil {
		t.Fatalf("higher finalized checkpoint rejected: %v", err)
	}
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), winner, [32]byte{5}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("stale checkpoint after durable advance allowed: %v", err)
	}
	if state, err := backend.Current(); err != nil || state != later {
		t.Fatalf("failed to retain new durable high-water mark: %v %+v", err, state)
	}
}

func TestLocalWitnessCustodySignerFailureNeverUndoesDurableIntent(t *testing.T) {
	a, w := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("local-custody-failure", []byte("network")))
	root := types.HashBytes("local-custody-failure", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 23, HeaderHash: types.Hash{1}}
	evil := cp
	evil.HeaderHash = types.Hash{2}
	signer := &localCustodyTestSigner{authority: a, witness: w, failAuthority: true}
	path := filepath.Join(t.TempDir(), "durable-failure-anchor")
	open := func(create bool) *LocalWitnessCustodyBackend {
		t.Helper()
		b, err := OpenLocalWitnessCustodyBackend(path, network, root, create, witnessFaultyCustodyVerifier{}, signer, w.Public().(ed25519.PublicKey), a.Public().(ed25519.PublicKey))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	b := open(true)
	if _, err := b.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("failed provider signed successfully: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	b = open(false)
	defer b.Close()
	signer.failAuthority = false
	if _, err := b.ReserveAndSignWitnessCheckpoint(context.Background(), evil, [32]byte{2}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("provider failure rolled back durable reservation: %v", err)
	}
	signer.badWitness = true
	if _, err := b.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{3}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("backend published invalid witness signature: %v", err)
	}
	signer.badWitness = false
	att, err := b.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{4})
	if err != nil || att.Revision != 1 {
		t.Fatalf("idempotent retry after provider failure rejected: revision=%d err=%v", att.Revision, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := signer.calls
	if _, err := b.ReserveAndSignWitnessCheckpoint(ctx, cp, [32]byte{5}); !errors.Is(err, ErrWitnessSigningIntent) || signer.calls != before {
		t.Fatalf("canceled custody request signed: %v", err)
	}
}

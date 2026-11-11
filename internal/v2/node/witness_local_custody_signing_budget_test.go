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
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This signer observes the request deadline of BOTH operations. The underlying
// test signer is purely local and does not model an HSM or remote custody.
type custodyDeadlineObservingSigner struct {
	inner     *localCustodyTestSigner
	mu        sync.Mutex
	deadlines []time.Time
}

func (s *custodyDeadlineObservingSigner) remember(ctx context.Context) error {
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) > 5*time.Second || time.Until(deadline) <= 0 {
		return errors.New("custody signer was invoked without live bounded deadline")
	}
	s.mu.Lock()
	s.deadlines = append(s.deadlines, deadline)
	s.mu.Unlock()
	return nil
}

func (s *custodyDeadlineObservingSigner) SignWitnessCheckpointDigest(ctx context.Context, digest [32]byte) ([]byte, error) {
	if err := s.remember(ctx); err != nil {
		return nil, err
	}
	return s.inner.SignWitnessCheckpointDigest(ctx, digest)
}

func (s *custodyDeadlineObservingSigner) SignCustodyAttestationDigest(ctx context.Context, digest [32]byte) ([]byte, error) {
	if err := s.remember(ctx); err != nil {
		return nil, err
	}
	return s.inner.SignCustodyAttestationDigest(ctx, digest)
}

func TestLocalWitnessCustodyDirectCallSharesOneDeadlineAcrossBothSignerOperations(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-shared-signing-budget", []byte("network")))
	root := types.HashBytes("custody-shared-signing-budget", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 19, HeaderHash: types.Hash{8}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	signer := &custodyDeadlineObservingSigner{inner: &localCustodyTestSigner{witness: witness, authority: authority}}
	backend, err := OpenLocalWitnessCustodyBackend(filepath.Join(dir, "anchor"), network, root, true,
		witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}, signer,
		witness.Public().(ed25519.PublicKey), authority.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatalf("valid direct signing request failed: %v", err)
	}
	signer.mu.Lock()
	defer signer.mu.Unlock()
	if len(signer.deadlines) != 2 || !signer.deadlines[0].Equal(signer.deadlines[1]) {
		t.Fatalf("two signing operations did not share one request deadline: %v", signer.deadlines)
	}
}

type custodyBlockingFloor struct {
	original FinalityCheckpoint
	block    bool
}

func (f *custodyBlockingFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return FinalityCheckpoint{}, ErrWitnessSigningIntent
}

func (f *custodyBlockingFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	if !f.block {
		return f.original, nil
	}
	<-ctx.Done()
	return FinalityCheckpoint{}, ctx.Err()
}

func TestLocalWitnessCustodySigningTimeoutDoesNotAdvanceIntentOrReachSigner(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-shared-floor-budget", []byte("network")))
	root := types.HashBytes("custody-shared-floor-budget", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 27, HeaderHash: types.Hash{6}}
	later := cp
	later.Height++
	later.HeaderHash = types.Hash{7}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash, later.Height: later.HeaderHash}}
	path := filepath.Join(dir, "anchor")
	keyW := witness.Public().(ed25519.PublicKey)
	keyA := authority.Public().(ed25519.PublicKey)
	seed, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, keyW, keyA)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	floor := &custodyBlockingFloor{original: cp}
	backend, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, keyW, keyA, floor)
	if err != nil {
		t.Fatal(err)
	}
	defer backend.Close()
	floor.block = true
	signer.mu.Lock()
	before := signer.calls
	signer.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := backend.ReserveAndSignWitnessCheckpoint(ctx, later, [32]byte{2}); !errors.Is(err, context.DeadlineExceeded) || !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("timed-out floor authorized signing: %v", err)
	}
	signer.mu.Lock()
	after := signer.calls
	signer.mu.Unlock()
	if after != before {
		t.Fatal("deadline-expired request reached private-key signer")
	}
	if got, err := backend.Current(); err != nil || got != cp {
		t.Fatalf("timeout changed durable intent: %+v %v", got, err)
	}
	floor.block = false
	if _, err := backend.ReserveAndSignWitnessCheckpoint(context.Background(), later, [32]byte{3}); err != nil {
		t.Fatalf("recovered source could not resume authorized signing: %v", err)
	}
}

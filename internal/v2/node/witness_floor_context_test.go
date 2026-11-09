//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// No test origin should be mistaken for an independent deployed witness.
// The signatures and TLS certificates in this file are ephemeral fixtures.
type countedSignedFloorSource struct {
	payload []byte
	calls   atomic.Int32
}

func (s *countedSignedFloorSource) SignedWitnessFloor() ([]byte, error) {
	s.calls.Add(1)
	return s.payload, nil
}

func TestWitnessFloorCancellationPropagatesThroughHTTPSQuorumAndMultiSource(t *testing.T) {
	network := types.NetworkID(types.HashBytes("floor-context", []byte("network")))
	root := types.HashBytes("floor-context", []byte("genesis"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 17, HeaderHash: types.Hash{5}}
	keys, private := witnessTestKeys()
	valid := signedWitnessTestBundle(t, cp, keys, private, 3)

	received := make(chan struct{})
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(received)
		<-r.Context().Done()
	}))
	defer server.Close()

	// Production constructors refuse loopback/IP literals. Only this
	// ephemeral TLS test injects a test-CA-trusting HTTP client.
	client := server.Client()
	client.Timeout = 5 * time.Second
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	httpSource := &HTTPWitnessFloorSource{endpoint: server.URL + "/floor", client: client}
	slow, err := NewQuorumAuthenticatedWitnessFloor(httpSource, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	otherSource := &countedSignedFloorSource{payload: valid}
	other, err := NewQuorumAuthenticatedWitnessFloor(otherSource, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	multi, err := NewMultiSourceWitnessFloor(slow, other)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := multi.AuthenticatedWitnessFloorContext(ctx)
		done <- err
	}()
	select {
	case <-received:
	case <-time.After(10 * time.Second):
		t.Fatal("HTTPS floor never received the request")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled HTTPS quorum unexpectedly succeeded: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled witness floor continued waiting on HTTPS")
	}
	if otherSource.calls.Load() != 0 || multi.observed {
		t.Fatal("cancelled witness request queried another origin or updated high water")
	}
}

// First authenticated read is the startup reconciliation. The second read,
// made under the custody mutex before any signer call, must honor the
// request cancellation rather than wait for the external floor to recover.
type custodyCancellationFloor struct {
	cp      FinalityCheckpoint
	entered chan struct{}
	calls   atomic.Int32
}

func (f *custodyCancellationFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return FinalityCheckpoint{}, errors.New("unexpected legacy unbounded floor read")
}

func (f *custodyCancellationFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	if f.calls.Add(1) == 1 {
		return f.cp, nil
	}
	close(f.entered)
	<-ctx.Done()
	return FinalityCheckpoint{}, ctx.Err()
}

func TestLocalWitnessCustodyCancellationFencesSignerBeforeRemoteFloorReturns(t *testing.T) {
	network := types.NetworkID(types.HashBytes("custody-context", []byte("network")))
	root := types.HashBytes("custody-context", []byte("genesis"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 39, HeaderHash: types.Hash{4}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	authority, witness := witnessAtomicTestKeys()
	signer := &localCustodyTestSigner{witness: witness, authority: authority}
	witnessKey := witness.Public().(ed25519.PublicKey)
	authorityKey := authority.Public().(ed25519.PublicKey)
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "custody-anchor")
	seed, err := OpenLocalWitnessCustodyBackend(path, network, root, true, verifier, signer, witnessKey, authorityKey)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := seed.ReserveAndSignWitnessCheckpoint(context.Background(), cp, [32]byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := seed.Close(); err != nil {
		t.Fatal(err)
	}
	floor := &custodyCancellationFloor{cp: cp, entered: make(chan struct{})}
	guarded, err := OpenReconciledLocalWitnessCustodyBackend(path, network, root, verifier, signer, witnessKey, authorityKey, floor)
	if err != nil {
		t.Fatal(err)
	}
	defer guarded.Close()
	before := signer.calls
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := guarded.ReserveAndSignWitnessCheckpoint(ctx, cp, [32]byte{2})
		done <- err
	}()
	select {
	case <-floor.entered:
	case <-time.After(10 * time.Second):
		t.Fatal("custody never reached cancellable external floor")
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, ErrWitnessSigningIntent) || !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled custody read did not fail closed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("custody held signing mutex after external floor cancellation")
	}
	if signer.calls != before {
		t.Fatal("cancelled external floor invoked custody signing provider")
	}
	if got, err := guarded.Current(); err != nil || got != cp {
		t.Fatalf("cancelled floor mutated durable custody tip: %+v %v", got, err)
	}
}

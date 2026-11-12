//go:build linux || darwin

package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This is a synthetic fixture only. A production authenticated floor must
// validate pinned signatures and distinct operator failure domains.
type reconciledPublicationTestFloor struct {
	mu    sync.Mutex
	cp    FinalityCheckpoint
	err   error
	calls int
}

func (f *reconciledPublicationTestFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.AuthenticatedWitnessFloorContext(context.Background())
}

func (f *reconciledPublicationTestFloor) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if err := ctx.Err(); err != nil {
		return FinalityCheckpoint{}, err
	}
	return f.cp, f.err
}

func (f *reconciledPublicationTestFloor) set(cp FinalityCheckpoint, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cp, f.err = cp, err
}

type nonContextualPublicationTestFloor struct {
	cp FinalityCheckpoint
}

func (f nonContextualPublicationTestFloor) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.cp, nil
}

// A separate contextual, authenticated floor must EXACTLY agree with the
// candidate checkpoint. Native QC verification by itself does not reject a
// previously valid ancestor that an online publisher tries to re-sign.
func TestReconciledWitnessHTTPSChallengePublicationHandlerRejectsStaleQCValidCheckpointBeforeSigning(t *testing.T) {
	network := types.NetworkID(types.HashBytes("publisher-monotonic-fence", []byte("network")))
	root := types.HashBytes("publisher-monotonic-fence", []byte("root"))
	older := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 21, HeaderHash: types.Hash{7}}
	newer := older
	newer.Height = 22
	newer.HeaderHash = types.Hash{8}
	keys, privates := witnessTestKeys()
	oldBundle := signedWitnessTestBundle(t, older, keys, privates, 3)
	newBundle := signedWitnessTestBundle(t, newer, keys, privates, 3)
	offlineSeed := make([]byte, ed25519.SeedSize)
	offlineSeed[0] = 204
	offlineKey := ed25519.NewKeyFromSeed(offlineSeed)
	onlineSeed := make([]byte, ed25519.SeedSize)
	onlineSeed[0] = 205
	onlineKey := ed25519.NewKeyFromSeed(onlineSeed)
	onlineSigner := &challengedPublicationTestSigner{key: onlineKey}
	source := &challengedPublicationTestSource{payload: oldBundle}
	localQC := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		older.Height: older.HeaderHash, newer.Height: newer.HeaderHash,
	}}
	external := &reconciledPublicationTestFloor{cp: older}
	newHandler := func(floor WitnessExternalFloorVerifier) (*WitnessHTTPSChallengePublicationHandler, error) {
		return NewReconciledWitnessHTTPSChallengePublicationHandler(
			source, localQC, onlineSigner, network, root, "operator-one", 9,
			offlineKey.Public().(ed25519.PublicKey),
			onlineKey.Public().(ed25519.PublicKey), keys, floor,
		)
	}
	if _, err := newHandler(nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("strict publisher accepted missing external floor: %v", err)
	}
	if _, err := newHandler(nonContextualPublicationTestFloor{cp: older}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("strict publisher accepted unbounded legacy floor: %v", err)
	}
	handler, err := newHandler(external)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()
	client := ts.Client()
	defer client.CloseIdleConnections()
	doRequest := func() (int, []byte, []byte) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, ts.URL+witnessHTTPSChallengePublicationPath, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set(witnessHTTPSChallengeHeader, strings.Repeat("ab", 32))
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		sig, err := hex.DecodeString(response.Header.Get(witnessHTTPSOperatorSignatureHeader))
		if err != nil {
			t.Fatal(err)
		}
		return response.StatusCode, body, sig
	}
	assertSuccess := func(expected []byte, cp FinalityCheckpoint, calls int) {
		t.Helper()
		status, body, sig := doRequest()
		if status != http.StatusOK || !bytes.Equal(body, expected) || onlineSigner.signingCalls() != calls {
			t.Fatalf("valid external floor rejected: status=%d bodyLen=%d signer calls=%d", status, len(body), onlineSigner.signingCalls())
		}
		var challenge [32]byte
		copy(challenge[:], bytes.Repeat([]byte{0xab}, 32))
		digest, err := WitnessHTTPSPublicationChallengeDigest(network, root, "operator-one", 9, challenge, expected)
		if err != nil || !ed25519.Verify(onlineKey.Public().(ed25519.PublicKey), digest[:], sig) {
			t.Fatalf("publisher signed unbound digest: %v", err)
		}
	}
	assertDenied := func(calls int) {
		t.Helper()
		status, _, _ := doRequest()
		if status != http.StatusServiceUnavailable || onlineSigner.signingCalls() != calls {
			t.Fatalf("external floor mismatch reached signer: status=%d signer calls=%d expected=%d", status, onlineSigner.signingCalls(), calls)
		}
	}
	setBundle := func(raw []byte) {
		source.mu.Lock()
		defer source.mu.Unlock()
		source.payload = append([]byte(nil), raw...)
	}

	assertSuccess(oldBundle, older, 1)
	setBundle(newBundle)
	assertDenied(1) // Candidate is QC-valid but remote floor is still older.
	external.set(newer, nil)
	assertSuccess(newBundle, newer, 2)
	setBundle(oldBundle)
	external.set(older, nil)
	assertDenied(2) // The floor also rolled back: local process high-water rejects it.
	setBundle(newBundle)
	external.set(newer, errors.New("operator publication unavailable"))
	assertDenied(2)
	conflict := newer
	conflict.HeaderHash[0] ^= 1
	external.set(conflict, nil)
	assertDenied(2)
	external.set(newer, nil)
	assertSuccess(newBundle, newer, 3)

	// A private-key failure after the high-water reservation cannot authorize
	// an older checkpoint later in the same live process.
	after := newer
	after.Height = 23
	after.HeaderHash = types.Hash{9}
	newestBundle := signedWitnessTestBundle(t, after, keys, privates, 3)
	localQC.allowed[after.Height] = after.HeaderHash
	setBundle(newestBundle)
	external.set(after, nil)
	onlineSigner.mu.Lock()
	onlineSigner.bad = true
	onlineSigner.mu.Unlock()
	assertDenied(4) // The signer was called and returned an invalid signature.
	onlineSigner.mu.Lock()
	onlineSigner.bad = false
	onlineSigner.mu.Unlock()
	setBundle(newBundle)
	external.set(newer, nil)
	assertDenied(4) // The high-water reservation survived the signer error.
}

type alternatingPublicationTestSource struct {
	mu      sync.Mutex
	first   []byte
	later   []byte
	queries int
}

func (s *alternatingPublicationTestSource) SignedWitnessFloor() ([]byte, error) {
	return s.SignedWitnessFloorContext(context.Background())
}

func (s *alternatingPublicationTestSource) SignedWitnessFloorContext(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.queries++
	if s.queries == 1 {
		return append([]byte(nil), s.first...), nil
	}
	return append([]byte(nil), s.later...), nil
}

// Concurrent GETs must never authorize a QC-valid old ancestor after a newer
// checkpoint has been signed. The bounded admission semaphore serializes
// observation and signer calls; tests run repeatedly under Go's race detector.
func TestWitnessHTTPSChallengePublicationHandlerConcurrentRollbackDenied(t *testing.T) {
	network := types.NetworkID{4}
	root := types.Hash{5}
	older := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 31, HeaderHash: types.Hash{1}}
	newer := older
	newer.Height = 32
	newer.HeaderHash = types.Hash{2}
	keys, privates := witnessTestKeys()
	source := &alternatingPublicationTestSource{
		first: signedWitnessTestBundle(t, newer, keys, privates, 3),
		later: signedWitnessTestBundle(t, older, keys, privates, 3),
	}
	offlineSeed := make([]byte, ed25519.SeedSize)
	offlineSeed[0] = 206
	offline := ed25519.NewKeyFromSeed(offlineSeed)
	onlineSeed := make([]byte, ed25519.SeedSize)
	onlineSeed[0] = 207
	online := ed25519.NewKeyFromSeed(onlineSeed)
	signer := &challengedPublicationTestSigner{key: online}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{
		older.Height: older.HeaderHash, newer.Height: newer.HeaderHash,
	}}
	handler, err := NewWitnessHTTPSChallengePublicationHandler(
		source, verifier, signer, network, root, "operator-one", 9,
		offline.Public().(ed25519.PublicKey),
		online.Public().(ed25519.PublicKey), keys,
	)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()
	client := ts.Client()
	defer client.CloseIdleConnections()
	const concurrent = 12
	statuses := make(chan int, concurrent)
	errs := make(chan error, concurrent)
	var wg sync.WaitGroup
	for i := 0; i < concurrent; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, err := http.NewRequest(http.MethodGet, ts.URL+witnessHTTPSChallengePublicationPath, nil)
			if err != nil {
				errs <- err
				return
			}
			req.Header.Set(witnessHTTPSChallengeHeader, strings.Repeat("cd", 32))
			resp, err := client.Do(req)
			if err != nil {
				errs <- err
				return
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			statuses <- resp.StatusCode
		}()
	}
	wg.Wait()
	close(statuses)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent HTTPS request failed: %v", err)
	}
	success := 0
	for status := range statuses {
		if status == http.StatusOK {
			success++
		} else if status != http.StatusServiceUnavailable {
			t.Fatalf("unexpected concurrent publisher status %d", status)
		}
	}
	if success != 1 || signer.signingCalls() != 1 {
		t.Fatalf("QC-valid ancestor signed after newer checkpoint: success=%d calls=%d", success, signer.signingCalls())
	}
}

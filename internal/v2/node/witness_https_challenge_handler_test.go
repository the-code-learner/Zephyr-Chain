//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type challengedPublicationTestSource struct {
	mu      sync.Mutex
	payload []byte
	calls   int
}

func (s *challengedPublicationTestSource) SignedWitnessFloor() ([]byte, error) {
	return s.SignedWitnessFloorContext(context.Background())
}

func (s *challengedPublicationTestSource) SignedWitnessFloorContext(ctx context.Context) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	return append([]byte(nil), s.payload...), nil
}

type challengedPublicationTestSigner struct {
	mu    sync.Mutex
	key   ed25519.PrivateKey
	calls int
	bad   bool
}

func (s *challengedPublicationTestSigner) SignWitnessHTTPSPublicationDigest(ctx context.Context, digest [32]byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if s.bad {
		return make([]byte, ed25519.SignatureSize), nil
	}
	return ed25519.Sign(s.key, digest[:]), nil
}

func (s *challengedPublicationTestSigner) signingCalls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func TestWitnessHTTPSChallengePublicationHandlerChecksQCBeforeSigning(t *testing.T) {
	n := types.NetworkID(types.HashBytes("challenge-serve", []byte("network")))
	root := types.HashBytes("challenge-serve", []byte("root"))
	cp := FinalityCheckpoint{Network: n, ValidatorRoot: root, Height: 11, HeaderHash: types.Hash{7}}
	witnessKeys, witnessPrivate := witnessTestKeys()
	payload := signedWitnessTestBundle(t, cp, witnessKeys, witnessPrivate, 3)
	offlineSeed := make([]byte, ed25519.SeedSize)
	offlineSeed[0] = 140
	offline := ed25519.NewKeyFromSeed(offlineSeed)
	onlineSeed := make([]byte, ed25519.SeedSize)
	onlineSeed[0] = 170
	online := ed25519.NewKeyFromSeed(onlineSeed)
	source := &challengedPublicationTestSource{payload: payload}
	signer := &challengedPublicationTestSigner{key: online}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	handler, err := NewWitnessHTTPSChallengePublicationHandler(
		source, verifier, signer, n, root, "operator-one", 5,
		offline.Public().(ed25519.PublicKey), online.Public().(ed25519.PublicKey), witnessKeys,
	)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewTLSServer(handler)
	defer ts.Close()
	client := ts.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return http.ErrUseLastResponse
	}
	remote := &HTTPWitnessFloorSource{
		endpoint: ts.URL + witnessHTTPSChallengePublicationPath,
		client: client,
		challengePolicy: &witnessHTTPSChallengePolicy{
			network: n, validatorRoot: root, operatorID: "operator-one", revision: 5,
			publicationKey: online.Public().(ed25519.PublicKey),
		},
	}
	floor, err := NewQuorumAuthenticatedWitnessFloor(remote, witnessKeys, n, root)
	if err != nil {
		t.Fatal(err)
	}
	read, err := floor.AuthenticatedWitnessFloorContext(context.Background())
	if err != nil || read != cp || signer.signingCalls() != 1 {
		t.Fatalf("independently checked HTTPS challenge round-trip failed: %+v %v calls=%d", read, err, signer.signingCalls())
	}
	// Bad request targets and challenges must never reach any private key
	// operation or even a QC-signature lookup.
	badRequests := []struct {
		name      string
		path      string
		method    string
		challenge []string
	}{
		{"missing", witnessHTTPSChallengePublicationPath, "GET", nil},
		{"short", witnessHTTPSChallengePublicationPath, "GET", []string{"beef"}},
		{"uppercase", witnessHTTPSChallengePublicationPath, "GET", []string{strings.Repeat("A", 64)}},
		{"duplicate", witnessHTTPSChallengePublicationPath, "GET", []string{strings.Repeat("a", 64), strings.Repeat("b", 64)}},
		{"zero", witnessHTTPSChallengePublicationPath, "GET", []string{strings.Repeat("0", 64)}},
		{"query", witnessHTTPSChallengePublicationPath + "?x=1", "GET", []string{strings.Repeat("a", 64)}},
		{"path", "/v1/%63heckpoint", "GET", []string{strings.Repeat("a", 64)}},
		{"post", witnessHTTPSChallengePublicationPath, "POST", []string{strings.Repeat("a", 64)}},
	}
	for _, test := range badRequests {
		t.Run(test.name, func(t *testing.T) {
			before := signer.signingCalls()
			req, err := http.NewRequest(test.method, ts.URL+test.path, nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, value := range test.challenge {
				req.Header.Add(witnessHTTPSChallengeHeader, value)
			}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK || signer.signingCalls() != before {
				t.Fatalf("bad publication request authorized signer: status=%d calls=%d", resp.StatusCode, signer.signingCalls()-before)
			}
		})
	}
	// A false TLS assertion must not authorize the same valid request.
	noTLS := httptest.NewRequest(http.MethodGet, witnessHTTPSChallengePublicationPath, nil)
	noTLS.Header.Set(witnessHTTPSChallengeHeader, strings.Repeat("a", 64))
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, noTLS)
	if recorder.Code != http.StatusForbidden || signer.signingCalls() != 1 {
		t.Fatalf("non-TLS request authorized publisher: status=%d calls=%d", recorder.Code, signer.signingCalls())
	}
	// An independent QC verifier must approve a *specific* valid certified
	// checkpoint, never just a well-formed quorum-signed response.
	disallowed := cp
	disallowed.Height++
	disallowed.HeaderHash = types.Hash{9}
	forged := signedWitnessTestBundle(t, disallowed, witnessKeys, witnessPrivate, 3)
	for _, invalid := range [][]byte{[]byte("{}"), forged, append([]byte(nil), payload[:len(payload)-1]...)} {
		source.mu.Lock()
		source.payload = invalid
		source.mu.Unlock()
		before := signer.signingCalls()
		if _, err := remote.SignedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) ||
			signer.signingCalls() != before {
			t.Fatalf("invalid/uncertified bundle got online publisher signature: err=%v extra=%d", err, signer.signingCalls()-before)
		}
	}
	source.mu.Lock()
	source.payload = payload
	source.mu.Unlock()
	signer.mu.Lock()
	signer.bad = true
	signer.mu.Unlock()
	if _, err := remote.SignedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("bad online signer signature returned to client: %v", err)
	}
}

func TestWitnessHTTPSChallengePublicationHandlerRejectsUnsafeKeyRoles(t *testing.T) {
	n := types.NetworkID{1}
	root := types.Hash{2}
	witnessKeys, _ := witnessTestKeys()
	offlineSeed := make([]byte, ed25519.SeedSize)
	offlineSeed[0] = 150
	offline := ed25519.NewKeyFromSeed(offlineSeed)
	onlineSeed := make([]byte, ed25519.SeedSize)
	onlineSeed[0] = 151
	online := ed25519.NewKeyFromSeed(onlineSeed)
	source := &challengedPublicationTestSource{}
	signer := &challengedPublicationTestSigner{key: online}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{}}
	inventoryKey := offline.Public().(ed25519.PublicKey)
	pubKey := online.Public().(ed25519.PublicKey)
	for _, test := range []struct {
		name   string
		id     string
		rev    uint64
		offline []byte
		online []byte
		keys   [][]byte
	}{
		{"same key", "operator-one", 1, pubKey, pubKey, witnessKeys},
		{"witness key reused", "operator-one", 1, inventoryKey, witnessKeys[0], witnessKeys},
		{"inventory reused by witness", "operator-one", 1, witnessKeys[0], pubKey, witnessKeys},
		{"too few witnesses", "operator-one", 1, inventoryKey, pubKey, witnessKeys[:2]},
		{"zero revision", "operator-one", 0, inventoryKey, pubKey, witnessKeys},
		{"missing label", "", 1, inventoryKey, pubKey, witnessKeys},
		{"bad online length", "operator-one", 1, inventoryKey, []byte{2}, witnessKeys},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewWitnessHTTPSChallengePublicationHandler(
				source, verifier, signer, n, root, test.id, test.rev,
				test.offline, test.online, test.keys,
			); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("unsafe publisher key policy accepted: %v", err)
			}
		})
	}
	nonce, _ := hex.DecodeString(strings.Repeat("a", 64))
	if len(nonce) != 32 {
		t.Fatal("invalid fixture challenge")
	}
}

//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestAtomicWitnessCustodyHTTPEndpointPolicy(t *testing.T) {
	valid := "https://custody.operator.example.org/v1/reserve-and-sign"
	if _, err := NewHTTPWitnessAtomicCustodian(valid); err != nil {
		t.Fatalf("valid HTTPS DNS endpoint rejected: %v", err)
	}
	for _, endpoint := range []string{
		"http://custody.operator.example.org/v1/reserve-and-sign",
		"https://127.0.0.1/v1/reserve-and-sign",
		"https://localhost/v1/reserve-and-sign",
		"https://custody.local/v1/reserve-and-sign",
		"https://CUSTODY.operator.example.org/v1/reserve-and-sign",
		"https://custody.operator.example.org./v1/reserve-and-sign",
		"https://user:pass@custody.operator.example.org/v1/reserve-and-sign",
		"https://custody.operator.example.org/v1/reserve-and-sign?x=1",
		"https://custody.operator.example.org/v1/reserve-and-sign#part",
		"https://custody.operator.example.org",
	} {
		t.Run(endpoint, func(t *testing.T) {
			if _, err := NewHTTPWitnessAtomicCustodian(endpoint); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("untrusted endpoint accepted: %v", err)
			}
		})
	}
}

func TestAtomicWitnessCustodyHTTPSAuthenticatedRoundTripAndBadResponses(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-https-e2e", []byte("network")))
	root := types.HashBytes("custody-https-e2e", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 51, HeaderHash: types.Hash{5}}
	j := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	fixture := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	mode := "ok"
	requests := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected custody request: method=%s type=%q", r.Method, r.Header.Get("Content-Type"))
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if mode == "redirect" {
			w.Header().Set("Location", "/another-path")
			w.WriteHeader(http.StatusTemporaryRedirect)
			return
		}
		if mode == "non-200" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		if mode == "oversized" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(strings.Repeat("x", maxWitnessCustodyResponseBytes+1)))
			return
		}
		var request witnessCustodyRequestJSON
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Errorf("could not decode request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if request.NetworkID != hex.EncodeToString(cp.Network[:]) ||
			request.ValidatorRoot != hex.EncodeToString(cp.ValidatorRoot[:]) ||
			request.Height != cp.Height || request.HeaderHash != hex.EncodeToString(cp.HeaderHash[:]) {
			t.Errorf("HTTP request changed checkpoint identity: %+v", request)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		rawChallenge, err := hex.DecodeString(request.Challenge)
		if err != nil || len(rawChallenge) != 32 {
			t.Errorf("invalid HTTP challenge %q: %v", request.Challenge, err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		var challenge [32]byte
		copy(challenge[:], rawChallenge)
		a, err := fixture.ReserveAndSignWitnessCheckpoint(r.Context(), cp, challenge)
		if err != nil {
			t.Errorf("fixture signer failed: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		wire := witnessCustodyResponseJSON{
			NetworkID: request.NetworkID, ValidatorRoot: request.ValidatorRoot,
			Height: cp.Height, HeaderHash: request.HeaderHash, Challenge: request.Challenge,
			Revision: a.Revision,
			WitnessPublicKey: hex.EncodeToString(a.WitnessPublicKey),
			WitnessSignature: hex.EncodeToString(a.WitnessSignature),
			AuthoritySignature: hex.EncodeToString(a.AuthoritySignature),
		}
		if mode == "foreign-checkpoint" {
			wire.Height++
		}
		if mode == "wrong-challenge" {
			wire.Challenge = strings.Repeat("0", 64)
		}
		if mode == "bad-signature" {
			wire.WitnessSignature = strings.Repeat("0", 128)
		}
		raw, err := json.Marshal(wire)
		if err != nil {
			t.Errorf("marshal custody response: %v", err)
			return
		}
		if mode == "duplicate-member" {
			raw = append([]byte("{\"revision\":1,"), raw[1:]...)
		}
		if mode == "unknown-member" {
			raw = append([]byte("{\"dangerous\":1,"), raw[1:]...)
		}
		if mode == "noncanonical" {
			raw = append([]byte("  "), raw...)
		}
		if mode == "wrong-media" {
			w.Header().Set("Content-Type", "text/plain")
		} else {
			w.Header().Set("Content-Type", "application/json")
		}
		_, _ = w.Write(raw)
	}))
	defer ts.Close()
	// The production constructor forbids loopback/IP origins. A TLS test
	// server is injected only inside this package-private test fixture.
	client := ts.Client()
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	remote := &HTTPWitnessAtomicCustodian{endpoint: ts.URL + "/v1/reserve-and-sign", client: client}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	if a, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, remote, keyA, keyW); err != nil || a.Revision != 1 {
		t.Fatalf("valid authenticated custody HTTPS result rejected: %+v %v", a, err)
	}
	for _, bad := range []string{"foreign-checkpoint", "wrong-challenge", "bad-signature", "duplicate-member", "unknown-member", "noncanonical", "wrong-media", "non-200", "redirect", "oversized"} {
		t.Run(bad, func(t *testing.T) {
			mode = bad
			before := requests
			if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, remote, keyA, keyW); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("bad HTTPS custody response %q accepted: %v", bad, err)
			}
			if requests != before+1 {
				t.Fatalf("unexpected redirect/follow-up request for %q: %d", bad, requests-before)
			}
		})
	}
	if stored, err := j.Current(); err != nil || stored != cp {
		t.Fatalf("remote HTTP failure erased durable local intent: %+v %v", stored, err)
	}
}

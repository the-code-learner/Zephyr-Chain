//go:build linux || darwin

package node

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This unit harness supplies a synthetic verified TLS chain to exercise the
// handler's admission boundary. Real mTLS handshakes must still be tested
// against the deployed reverse proxy and server TLS configuration.
func witnessCustodyHandlerUnitRequest(cp FinalityCheckpoint, challenge [32]byte) *http.Request {
	request := witnessCustodyRequestJSON{
		NetworkID: hex.EncodeToString(cp.Network[:]), ValidatorRoot: hex.EncodeToString(cp.ValidatorRoot[:]),
		Height: cp.Height, HeaderHash: hex.EncodeToString(cp.HeaderHash[:]),
		Challenge: hex.EncodeToString(challenge[:]),
	}
	raw, _ := json.Marshal(request)
	r := httptest.NewRequest(http.MethodPost, "https://custody.example.org"+witnessCustodyHandlerPath, bytes.NewReader(raw))
	r.Header.Set("Content-Type", "application/json")
	peer := &x509.Certificate{Raw: []byte{1, 2, 3, 4}}
	r.TLS = &tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{peer},
		VerifiedChains:   [][]*x509.Certificate{{peer}},
	}
	return r
}

func TestAtomicWitnessCustodyHTTPHandlerAuthenticatedCanonicalRoundTrip(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-http-handler", []byte("network")))
	root := types.HashBytes("custody-http-handler", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 32, HeaderHash: types.Hash{5}}
	challenge := [32]byte{7}
	fixture := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	if _, err := NewHTTPWitnessCustodyHandler(nil, network, root, keyA, keyW); err == nil {
		t.Fatal("nil remote custodian was accepted")
	}
	if _, err := NewHTTPWitnessCustodyHandler(fixture, network, root, keyA, keyA); err == nil {
		t.Fatal("same signing and authority key was accepted")
	}
	h, err := NewHTTPWitnessCustodyHandler(fixture, network, root, keyA, keyW)
	if err != nil {
		t.Fatal(err)
	}
	r := witnessCustodyHandlerUnitRequest(cp, challenge)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, r)
	if response.Code != http.StatusOK {
		t.Fatalf("authentic request rejected: status=%d response=%s", response.Code, response.Body.String())
	}
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Content-Type") != "application/json" {
		t.Fatal("custody response was not noncacheable canonical JSON")
	}
	var wire witnessCustodyResponseJSON
	if err := json.Unmarshal(response.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	publicKey, err := decodeCanonicalWitnessCustodyHex(wire.WitnessPublicKey, ed25519.PublicKeySize)
	if err != nil {
		t.Fatal(err)
	}
	sigW, err := decodeCanonicalWitnessCustodyHex(wire.WitnessSignature, ed25519.SignatureSize)
	if err != nil {
		t.Fatal(err)
	}
	sigA, err := decodeCanonicalWitnessCustodyHex(wire.AuthoritySignature, ed25519.SignatureSize)
	if err != nil {
		t.Fatal(err)
	}
	a := WitnessAtomicCustodyAttestation{
		Checkpoint: cp, Challenge: challenge, Revision: wire.Revision,
		WitnessPublicKey: publicKey, WitnessSignature: sigW, AuthoritySignature: sigA,
	}
	if err := VerifyWitnessAtomicCustodyAttestation(a, cp, challenge, keyA, keyW); err != nil {
		t.Fatalf("server returned unauthenticated custody evidence: %v", err)
	}
	if fixture.calls != 1 {
		t.Fatalf("custody backend called %d times", fixture.calls)
	}
}

func TestAtomicWitnessCustodyHTTPHandlerRejectsUntrustedRequestsBeforeSigning(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-http-reject", []byte("network")))
	root := types.HashBytes("custody-http-reject", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 10, HeaderHash: types.Hash{1}}
	challenge := [32]byte{9}
	fixture := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	h, err := NewHTTPWitnessCustodyHandler(fixture, network, root, authority.Public().(ed25519.PublicKey), witness.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		change func(*http.Request)
	}{
		{"no mTLS", func(r *http.Request) { r.TLS = nil }},
		{"unverified TLS client", func(r *http.Request) { r.TLS.VerifiedChains = nil }},
		{"nil presented leaf", func(r *http.Request) { r.TLS.PeerCertificates[0] = nil }},
		{"empty presented leaf", func(r *http.Request) { r.TLS.PeerCertificates[0].Raw = nil }},
		{"empty verified chain", func(r *http.Request) { r.TLS.VerifiedChains[0] = nil }},
		{"nil verified leaf", func(r *http.Request) { r.TLS.VerifiedChains[0][0] = nil }},
		{"mismatched verified leaf", func(r *http.Request) {
			r.TLS.VerifiedChains[0][0] = &x509.Certificate{Raw: []byte{9, 8, 7, 6}}
		}},
		{"wrong method", func(r *http.Request) { r.Method = http.MethodGet }},
		{"wrong path", func(r *http.Request) { r.URL.Path = "/other" }},
		{"query", func(r *http.Request) { r.URL.RawQuery = "x=1" }},
		{"empty query delimiter", func(r *http.Request) { r.URL.ForceQuery = true }},
		{"escaped equivalent route", func(r *http.Request) {
			r.URL.RawPath = "/v1/%72eserve-and-sign"
		}},
		{"opaque target", func(r *http.Request) { r.URL.Opaque = "/v1/reserve-and-sign" }},
		{"fragment", func(r *http.Request) { r.URL.Fragment = "other" }},
		{"wrong media", func(r *http.Request) { r.Header.Set("Content-Type", "text/plain") }},
		{"unknown JSON field", func(r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(append([]byte("{\"unexpected\":1,"), raw[1:]...)))
		}},
		{"duplicate JSON member", func(r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(bytes.NewReader(append([]byte("{\"height\":10,"), raw[1:]...)))
		}},
		{"noncanonical JSON", func(r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			r.Body = io.NopCloser(strings.NewReader(strings.Replace(string(raw), ",", ", ", 1)))
		}},
		{"oversized body", func(r *http.Request) {
			r.Body = io.NopCloser(strings.NewReader(strings.Repeat("x", maxWitnessCustodyRequestBytes+1)))
		}},
		{"foreign network", func(r *http.Request) {
			var body witnessCustodyRequestJSON
			_ = json.NewDecoder(r.Body).Decode(&body)
			body.NetworkID = strings.Repeat("a", 64)
			raw, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}},
		{"zero challenge", func(r *http.Request) {
			var body witnessCustodyRequestJSON
			_ = json.NewDecoder(r.Body).Decode(&body)
			body.Challenge = strings.Repeat("0", 64)
			raw, _ := json.Marshal(body)
			r.Body = io.NopCloser(bytes.NewReader(raw))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := witnessCustodyHandlerUnitRequest(cp, challenge)
			tc.change(r)
			before := fixture.calls
			response := httptest.NewRecorder()
			h.ServeHTTP(response, r)
			if response.Code == http.StatusOK || fixture.calls != before {
				t.Fatalf("invalid request reached custody signer: status=%d calls=%d", response.Code, fixture.calls-before)
			}
		})
	}
	fixture.mutate = func(a WitnessAtomicCustodyAttestation) WitnessAtomicCustodyAttestation {
		a.AuthoritySignature = append([]byte(nil), a.AuthoritySignature...)
		a.AuthoritySignature[0] ^= 1
		return a
	}
	r := witnessCustodyHandlerUnitRequest(cp, challenge)
	response := httptest.NewRecorder()
	h.ServeHTTP(response, r)
	if response.Code != http.StatusServiceUnavailable || response.Body.String() == "" {
		t.Fatalf("forged backend signature was published: %d", response.Code)
	}
}

type witnessCancelOnReadCloser struct {
	io.ReadCloser
	cancel context.CancelFunc
}

func (b *witnessCancelOnReadCloser) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.cancel()
	}
	return n, err
}

// A valid, fully read and canonical request is still unauthorized for
// remote key-custody work once its caller has canceled. In particular,
// the request may be canceled DURING an otherwise successful body read.
// Some custody backends do not respect context cancellation on entry.
func TestAtomicWitnessCustodyHTTPHandlerCancelledBeforeRemoteSigning(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("custody-cancel-before-sign", []byte("network")))
	root := types.HashBytes("custody-cancel-before-sign", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 52, HeaderHash: types.Hash{7}}
	challenge := [32]byte{9}
	fixture := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	h, err := NewHTTPWitnessCustodyHandler(fixture, network, root,
		authority.Public().(ed25519.PublicKey), witness.Public().(ed25519.PublicKey))
	if err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name             string
		cancelOnBodyRead bool
	}{
		{name: "already canceled"},
		{name: "canceled while reading canonical JSON", cancelOnBodyRead: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			request := witnessCustodyHandlerUnitRequest(cp, challenge).WithContext(ctx)
			if tc.cancelOnBodyRead {
				request.Body = &witnessCancelOnReadCloser{ReadCloser: request.Body, cancel: cancel}
			} else {
				cancel()
			}
			before := fixture.calls
			response := httptest.NewRecorder()
			h.ServeHTTP(response, request)
			if response.Code != http.StatusServiceUnavailable {
				t.Fatalf("canceled custody request was accepted: status=%d", response.Code)
			}
			if fixture.calls != before {
				t.Fatalf("abandoned request delegated to signer: before=%d after=%d", before, fixture.calls)
			}
		})
	}

	// An abandoned request must not fence out a later, independently
	// authenticated valid request to the exact same checkpoint.
	before := fixture.calls
	response := httptest.NewRecorder()
	h.ServeHTTP(response, witnessCustodyHandlerUnitRequest(cp, challenge))
	if response.Code != http.StatusOK || fixture.calls != before+1 {
		t.Fatalf("fresh valid request could not resume: status=%d signerCalls=%d", response.Code, fixture.calls-before)
	}
}

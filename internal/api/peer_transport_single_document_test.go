package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A single JSON Decoder.Decode accepts the first valid document and silently
// discards extra material. Peer block, certified evidence and snapshot data
// must never ignore a second document or attacker-controlled suffix.
func TestHTTPPeerTransportRejectsTrailingJSONOnRecoveryResponses(t *testing.T) {
	signer := newConsensusSigner(t)
	config := DefaultConfig()
	config.NodeID = "peer-json-single-document"
	config.ValidatorPrivateKey = encodedPrivateKey(t, signer.privateKey)
	identity, _, err := newTransportIdentitySigner(config)
	if err != nil || identity == nil {
		t.Fatalf("build peer request proof signer: %v", err)
	}

	var responseBody atomic.Value
	responseBody.Store("{}")
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/blocks/1" &&
			r.URL.Path != "/v1/internal/block-evidence/1" &&
			r.URL.Path != "/v1/internal/snapshot" {
			http.Error(w, "unexpected route", http.StatusNotFound)
			return
		}
		if strings.HasPrefix(r.URL.Path, "/v1/internal/") && r.Header.Get(sourceSignatureHeader) == "" {
			http.Error(w, "missing signed peer request", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(responseBody.Load().(string)))
	}))
	defer peer.Close()

	transport := newHTTPPeerTransport(&http.Client{Timeout: 3 * time.Second}, config.NodeID, identity)
	for _, endpoint := range []struct {
		name string
		path string
		valid string
		fetch func() error
	}{
		{name: "block", path: "/v1/blocks/1", valid: "{\"block\":{}}", fetch: func() error {
			_, err := transport.FetchBlock(peer.URL, 1)
			return err
		}},
		{name: "certified evidence", path: "/v1/internal/block-evidence/1", valid: "{\"evidence\":[]}", fetch: func() error {
			_, err := transport.(certifiedBlockEvidenceTransport).FetchBlockEvidence(peer.URL, 1)
			return err
		}},
		{name: "snapshot", path: "/v1/internal/snapshot", valid: "{\"snapshot\":{}}", fetch: func() error {
			_, err := transport.FetchSnapshot(peer.URL)
			return err
		}},
	} {
		t.Run(endpoint.name, func(t *testing.T) {
			for _, tc := range []struct {
				name string
				body string
				ok bool
			}{
				{name: "single document", body: endpoint.valid, ok: true},
				{name: "surrounding whitespace", body: " \n" + endpoint.valid + " \t", ok: true},
				{name: "second valid JSON", body: endpoint.valid + " {}", ok: false},
				{name: "trailing null", body: endpoint.valid + " null", ok: false},
				{name: "trailing non-JSON", body: endpoint.valid + " malicious", ok: false},
				{name: "incomplete second JSON", body: endpoint.valid + " {", ok: false},
				{name: "empty body", body: "", ok: false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					responseBody.Store(tc.body)
					err := endpoint.fetch()
					if tc.ok && err != nil {
						t.Fatalf("%s rejected valid single JSON document: %v", endpoint.name, err)
					}
					if !tc.ok && err == nil {
						t.Fatal(fmt.Sprintf("%s silently accepted malformed or extra JSON data", endpoint.name))
					}
				})
			}
		})
	}
}

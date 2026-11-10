package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Peer status is fetched during authenticated admission and consensus
// liveness checks. A peer must not force unbounded JSON allocation, and
// one valid JSON document followed by a second must not be accepted.
func TestHTTPPeerTransportBoundsAndValidatesStatusJSON(t *testing.T) {
	payload := "{}"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			t.Errorf("unexpected peer status request path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(payload))
	}))
	defer server.Close()
	transport := newHTTPPeerTransport(&http.Client{Timeout: 3 * time.Second}, "status-bound-test", nil)

	for _, tc := range []struct {
		name string
		body string
		ok   bool
	}{
		{name: "valid status document", body: "{}", ok: true},
		{name: "whitespace padded single document", body: " \n{} \t", ok: true},
		{name: "empty body", body: "", ok: false},
		{name: "non JSON body", body: "not-json", ok: false},
		{name: "concatenated JSON documents", body: "{} {}", ok: false},
		{name: "trailing injection bytes", body: "{} ignored", ok: false},
		{name: "oversized JSON response", body: "{\"unused\":\"" + strings.Repeat("A", maxPeerStatusResponseBytes) + "\"}", ok: false},
		{name: "oversized trailing padding", body: "{}" + strings.Repeat(" ", maxPeerStatusResponseBytes), ok: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload = tc.body
			_, err := transport.FetchStatus(server.URL)
			if tc.ok && err != nil {
				t.Fatalf("valid bounded status rejected: %v", err)
			}
			if !tc.ok && err == nil {
				t.Fatal("untrusted invalid or oversized peer status was accepted")
			}
		})
	}
}

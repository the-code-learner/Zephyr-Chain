package node

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHTTPWitnessFloorRejectsUntrustedURLAndResponses(t *testing.T) {
	for _, address := range []string{
		"", "http://example.org/checkpoint", "file:///tmp/checkpoint",
		"https://user:pass@example.org/checkpoint",
		"https://example.org/checkpoint#frag",
		"https://example.org/checkpoint?token=secret",
	} {
		if _, err := NewHTTPWitnessFloorSource(address); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("accepted insecure endpoint %q: %v", address, err)
		}
	}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/good":
			_, _ = w.Write([]byte("{}"))
		case "/redirect":
			http.Redirect(w, r, "/good", http.StatusFound)
		case "/oversized":
			_, _ = w.Write([]byte(strings.Repeat("x", maxWitnessBundleBytes+1)))
		case "/empty":
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()
	for _, path := range []string{"/good", "/redirect", "/oversized", "/empty", "/missing"} {
		source, err := NewHTTPWitnessFloorSource(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		// Test-only trusted TLS client: production never substitutes an
		// insecure TLS verifier or trusts a server for witness signatures.
		source.client = server.Client()
		source.client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		data, err := source.SignedWitnessFloor()
		if path == "/good" {
			if err != nil || string(data) != "{}" {
				t.Fatalf("valid bounded fetch failed: %v %q", err, data)
			}
		} else if !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("%s unexpectedly accepted: %v", path, err)
		}
	}
}

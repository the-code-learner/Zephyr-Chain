package node

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPWitnessFloorRejectsUntrustedURLAndResponses(t *testing.T) {
	for _, address := range []string{
		"", "http://example.org/checkpoint", "file:///tmp/checkpoint",
		"https://user:pass@example.org/checkpoint",
		"https://example.org/checkpoint#frag",
		"https://example.org/checkpoint?token=secret",
		"https://127.0.0.1/checkpoint",
		"https://192.168.1.10/checkpoint",
		"https://[2001:db8::10]/checkpoint",
		"https://localhost/checkpoint",
		"https://witness.local/checkpoint",
		"https://UPPER.example.org/checkpoint",
		"https://example.org./checkpoint",
		"https://example.org",
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
		// The production constructor intentionally forbids loopback and
		// IP-literal origins. This TLS test harness injects its ephemeral
		// trusted test CA directly, without weakening production policy.
		client := server.Client()
		client.Timeout = 5 * time.Second
		client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		}
		source := &HTTPWitnessFloorSource{endpoint: server.URL + path, client: client}
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

// The HTTPS transport must not silently inherit HTTPS_PROXY from the
// process environment, nor allow redirects, even though the witness
// envelope is separately authenticated by quorum Ed25519 signatures.
func TestHTTPWitnessFloorProductionDNSPolicyAndNoAmbientProxy(t *testing.T) {
	source, err := NewHTTPWitnessFloorSource("https://witness.operator.example.org/v1/checkpoint")
	if err != nil {
		t.Fatal(err)
	}
	transport, ok := source.client.Transport.(*http.Transport)
	if !ok || transport.Proxy != nil {
		t.Fatal("floor source inherited ambient proxy or lacked dedicated HTTP transport")
	}
	if source.client.Timeout != 5*time.Second {
		t.Fatalf("unexpected floor request timeout: %s", source.client.Timeout)
	}
	if source.client.CheckRedirect == nil {
		t.Fatal("floor source unexpectedly follows redirects")
	}
}

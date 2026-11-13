package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An authenticated (or unauthenticated) peer must not force unbounded
// allocation during signed QC request verification or JSON decoding.
func TestCertifiedDeliveryRejectsOversizedDeclaredAndStreamingBodies(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "bounded-certified-target",
		EnablePeerSync: false, BlockInterval: 0, SyncInterval: 0,
		RequireConsensusCertificates: true,
	})

	t.Run("oversized content length before proof verification", func(t *testing.T) {
		request := httptest.NewRequest(http.MethodPost, "/v1/internal/certified-blocks", bytes.NewReader([]byte("{}")))
		request.ContentLength = maxCertifiedBlockDeliveryBytes + 1
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized declared body returned %d, expected 413", recorder.Code)
		}
	})

	t.Run("oversized chunked signed peer body", func(t *testing.T) {
		// Set unknown Content-Length to force streaming enforcement. A real
		// signed peer proof may itself inspect the body; the limit must
		// apply BEFORE that verification as well as JSON parsing.
		signer := newConsensusSigner(t)
		body := bytes.Repeat([]byte(" "), int(maxCertifiedBlockDeliveryBytes)+1)
		request := signedPeerRequestWithSigner(t, signer, http.MethodPost, "/v1/internal/certified-blocks", body)
		request.ContentLength = -1
		recorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(recorder, request)
		if recorder.Code != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized streaming body returned %d, expected 413", recorder.Code)
		}
	})
	if got := server.ledger.Status().Height; got != 0 {
		t.Fatalf("oversized body advanced finalized height to %d", got)
	}
}

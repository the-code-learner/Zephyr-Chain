package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicHandlerBlocksDevelopmentEndpointsByDefault(t *testing.T) {
	server, err := NewServerWithConfig(Config{
		DataDir:               t.TempDir(),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	defer server.Close()

	request := httptest.NewRequest(http.MethodPost, "/v1/dev/faucet", nil)
	recorder := httptest.NewRecorder()
	server.PublicHandler(PublicHandlerOptions{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected disabled dev endpoint status 404, got %d", recorder.Code)
	}
}

func TestPublicHandlerKeepsHealthPublic(t *testing.T) {
	server, err := NewServerWithConfig(Config{
		DataDir:               t.TempDir(),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	defer server.Close()

	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	recorder := httptest.NewRecorder()
	server.PublicHandler(PublicHandlerOptions{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected health status 200, got %d", recorder.Code)
	}
}

func TestPublicHandlerRejectsOversizedRequestBodies(t *testing.T) {
	server, err := NewServerWithConfig(Config{
		DataDir:               t.TempDir(),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	defer server.Close()

	body := strings.NewReader(strings.Repeat("x", int(maxPublicRequestBodyBytes)+1))
	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", body)
	recorder := httptest.NewRecorder()
	server.PublicHandler(PublicHandlerOptions{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected oversized request status 413, got %d", recorder.Code)
	}
}

func TestPublicHandlerRequiresPeerSourceForInternalSnapshot(t *testing.T) {
	server, err := NewServerWithConfig(Config{
		DataDir:               t.TempDir(),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})
	if err != nil {
		t.Fatalf("create server: %v", err)
	}
	defer server.Close()

	request := httptest.NewRequest(http.MethodGet, "/v1/internal/snapshot", nil)
	recorder := httptest.NewRecorder()
	server.PublicHandler(PublicHandlerOptions{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected internal snapshot status 403 without peer source, got %d", recorder.Code)
	}

	request = httptest.NewRequest(http.MethodGet, "/v1/internal/snapshot", nil)
	request.Header.Set(sourceNodeHeader, "peer-node")
	recorder = httptest.NewRecorder()
	server.PublicHandler(PublicHandlerOptions{}).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected source-only internal snapshot request to be forbidden without a signed proof, got %d", recorder.Code)
	}
}

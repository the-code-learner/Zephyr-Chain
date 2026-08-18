package api

import (
	"bytes"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
)

func signedPeerRequest(t *testing.T, server *Server, method, path string, body []byte) *http.Request {
	t.Helper()
	proof, err := server.identitySigner.buildRequestProof(method, path, body, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set(sourceNodeHeader, proof.NodeID)
	r.Header.Set(sourceValidatorHeader, proof.ValidatorAddress)
	r.Header.Set(sourceIdentityPayloadHeader, proof.Payload)
	r.Header.Set(sourcePublicKeyHeader, proof.PublicKey)
	r.Header.Set(sourceSignatureHeader, proof.Signature)
	r.Header.Set(sourceSignedAtHeader, proof.SignedAt.Format(time.RFC3339Nano))
	r.Header.Set(sourceChainIDHeader, proof.ChainID)
	r.Header.Set(sourceRequestDomainHeader, proof.Domain)
	r.Header.Set(sourceRequestNonceHeader, proof.Nonce)
	return r
}

func TestRequestProofRejectsReplayCrossPathAndSurvivesRestart(t *testing.T) {
	dataDir := t.TempDir()
	signer := newConsensusSigner(t)
	config := Config{DataDir: dataDir, NodeID: "validator-a", ValidatorPrivateKey: encodedPrivateKey(t, signer.privateKey), BlockInterval: 0, SyncInterval: 0, EnablePeerSync: false}
	server := newTestServer(t, config)
	if _, err := server.ledger.SetValidators([]dpos.Validator{{Rank: 1, Address: signer.address, VotingPower: 100, SelfStake: 100}}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"height":1}`)
	request := signedPeerRequest(t, server, http.MethodPost, "/v1/internal/blocks", body)
	if _, err := validateAndRememberRequestProof(server, request); err != nil {
		t.Fatalf("first proof should pass: %v", err)
	}
	request.Body = http.NoBody
	request = signedPeerRequest(t, server, http.MethodPost, "/v1/internal/blocks", body)
	proof, _, err := requestProofFromRequest(request, server.config.ChainID)
	if err != nil {
		t.Fatal(err)
	}
	if proof == nil {
		t.Fatal("expected proof")
	}
	guard, err := replayGuardForServer(server)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.remember(proof); err != nil {
		t.Fatalf("fresh nonce should pass: %v", err)
	}
	if err := guard.remember(proof); !errors.Is(err, errRequestReplay) {
		t.Fatalf("expected replay rejection, got %v", err)
	}

	crossPath := httptest.NewRequest(http.MethodPost, "/v1/internal/snapshot", bytes.NewReader(body))
	for key, values := range request.Header {
		for _, value := range values {
			crossPath.Header.Add(key, value)
		}
	}
	if _, _, err := requestProofFromRequest(crossPath, server.config.ChainID); !errors.Is(err, errInvalidRequestProof) {
		t.Fatalf("expected cross-path proof rejection, got %v", err)
	}

	server.Close()
	reopened, err := NewServerWithConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.ledger.SetValidators([]dpos.Validator{{Rank: 1, Address: signer.address, VotingPower: 100, SelfStake: 100}}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := validateAndRememberRequestProof(reopened, request); !errors.Is(err, errRequestReplay) {
		t.Fatalf("expected replay rejection after restart, got %v", err)
	}
}

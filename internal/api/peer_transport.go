package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type peerTransport interface {
	FetchStatus(peerURL string) (StatusResponse, error)
	FetchBlock(peerURL string, height uint64) (ledger.Block, error)
	FetchSnapshot(peerURL string) (ledger.Snapshot, error)
	PostTransaction(peerURL string, envelope tx.Envelope) error
	PostBlock(peerURL string, block ledger.Block) error
	PostFaucet(peerURL string, request FaucetRequest) error
	PostProposal(peerURL string, proposal consensus.Proposal) error
	PostVote(peerURL string, vote consensus.Vote) error
}

type certifiedBlockEvidenceTransport interface {
	FetchBlockEvidence(peerURL string, height uint64) ([]ledger.CertifiedBlockEvidence, error)
}

type httpPeerTransport struct {
	client         *http.Client
	sourceNode     string
	identitySigner *transportIdentitySigner
}

func newHTTPPeerTransport(client *http.Client, sourceNode string, identitySigner *transportIdentitySigner) peerTransport {
	return &httpPeerTransport{client: client, sourceNode: sourceNode, identitySigner: identitySigner}
}

func (t *httpPeerTransport) FetchStatus(peerURL string) (StatusResponse, error) {
	response, err := t.client.Get(peerURL + "/v1/status")
	if err != nil {
		return StatusResponse{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return StatusResponse{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload StatusResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return StatusResponse{}, err
	}
	return payload, nil
}

func (t *httpPeerTransport) FetchBlock(peerURL string, height uint64) (ledger.Block, error) {
	response, err := t.client.Get(fmt.Sprintf("%s/v1/blocks/%d", peerURL, height))
	if err != nil {
		return ledger.Block{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ledger.Block{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload BlockAtHeightResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ledger.Block{}, err
	}
	return payload.Block, nil
}

func (t *httpPeerTransport) FetchBlockEvidence(peerURL string, height uint64) ([]ledger.CertifiedBlockEvidence, error) {
	request, err := http.NewRequest(http.MethodGet, fmt.Sprintf("%s/v1/internal/block-evidence/%d", peerURL, height), nil)
	if err != nil {
		return nil, err
	}
	if err := t.applyPeerHeaders(request, nil); err != nil {
		return nil, err
	}

	response, err := t.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload BlockEvidenceResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, err
	}
	return payload.Evidence, nil
}

func (t *httpPeerTransport) FetchSnapshot(peerURL string) (ledger.Snapshot, error) {
	request, err := http.NewRequest(http.MethodGet, peerURL+"/v1/internal/snapshot", nil)
	if err != nil {
		return ledger.Snapshot{}, err
	}
	if err := t.applyPeerHeaders(request, nil); err != nil {
		return ledger.Snapshot{}, err
	}

	response, err := t.client.Do(request)
	if err != nil {
		return ledger.Snapshot{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ledger.Snapshot{}, fmt.Errorf("peer returned status %d", response.StatusCode)
	}

	var payload SnapshotResponse
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ledger.Snapshot{}, err
	}
	return payload.Snapshot, nil
}

func (t *httpPeerTransport) PostTransaction(peerURL string, envelope tx.Envelope) error {
	return t.postJSON(peerURL+"/v1/transactions", envelope)
}

func (t *httpPeerTransport) PostBlock(peerURL string, block ledger.Block) error {
	return t.postJSON(peerURL+"/v1/internal/blocks", block)
}

func (t *httpPeerTransport) PostFaucet(peerURL string, request FaucetRequest) error {
	return t.postJSON(peerURL+"/v1/dev/faucet", request)
}

func (t *httpPeerTransport) PostProposal(peerURL string, proposal consensus.Proposal) error {
	return t.postJSON(peerURL+"/v1/consensus/proposals", proposal)
}

func (t *httpPeerTransport) PostVote(peerURL string, vote consensus.Vote) error {
	return t.postJSON(peerURL+"/v1/consensus/votes", vote)
}

func (t *httpPeerTransport) postJSON(target string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	request, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	if err := t.applyPeerHeaders(request, body); err != nil {
		return err
	}

	response, err := t.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode >= 400 && response.StatusCode != http.StatusConflict && response.StatusCode != http.StatusAccepted && response.StatusCode != http.StatusOK {
		return fmt.Errorf("peer returned status %d", response.StatusCode)
	}
	return nil
}

func (t *httpPeerTransport) applyPeerHeaders(request *http.Request, body []byte) error {
	if t.identitySigner == nil {
		return errMissingRequestProof
	}
	proof, err := t.identitySigner.buildRequestProof(request.Method, canonicalRequestPath(request), body, time.Now().UTC())
	if err != nil {
		return err
	}
	request.Header.Set(sourceNodeHeader, proof.NodeID)
	request.Header.Set(sourceValidatorHeader, proof.ValidatorAddress)
	request.Header.Set(sourceIdentityPayloadHeader, proof.Payload)
	request.Header.Set(sourcePublicKeyHeader, proof.PublicKey)
	request.Header.Set(sourceSignatureHeader, proof.Signature)
	request.Header.Set(sourceSignedAtHeader, proof.SignedAt.UTC().Format(time.RFC3339Nano))
	request.Header.Set(sourceChainIDHeader, proof.ChainID)
	request.Header.Set(sourceRequestDomainHeader, proof.Domain)
	request.Header.Set(sourceRequestNonceHeader, proof.Nonce)
	return nil
}

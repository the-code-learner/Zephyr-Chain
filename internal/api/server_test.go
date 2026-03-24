package api

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestHandleFaucetAndAccount(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	faucetBody := bytes.NewBufferString(`{"address":"zph_test","amount":125}`)
	faucetRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/faucet", faucetBody)
	faucetRecorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(faucetRecorder, faucetRequest)
	if faucetRecorder.Code != http.StatusOK {
		t.Fatalf("expected faucet status 200, got %d", faucetRecorder.Code)
	}

	accountRequest := httptest.NewRequest(http.MethodGet, "/v1/accounts/zph_test", nil)
	accountRecorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(accountRecorder, accountRequest)
	if accountRecorder.Code != http.StatusOK {
		t.Fatalf("expected account status 200, got %d", accountRecorder.Code)
	}

	var response AccountResponse
	if err := json.NewDecoder(accountRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode account response: %v", err)
	}

	if response.Account.Balance != 125 {
		t.Fatalf("expected account balance 125, got %d", response.Account.Balance)
	}
}

func TestHandleBroadcastTransactionRejectsInvalidSignature(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	envelope := signedEnvelope(t, 25, 1, "hello")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	envelope.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleBroadcastTransactionAcceptsFundedTransaction(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	envelope := signedEnvelope(t, 25, 1, "hello")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected status 202, got %d", recorder.Code)
	}
}

func TestHandleStatusExposesSignedTransportIdentity(t *testing.T) {
	signer := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:               t.TempDir(),
		NodeID:                "node-a",
		ValidatorPrivateKey:   encodedPrivateKey(t, signer.privateKey),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if response.ValidatorAddress != signer.address {
		t.Fatalf("expected validator address %s, got %s", signer.address, response.ValidatorAddress)
	}
	if response.Identity == nil {
		t.Fatal("expected signed transport identity in status response")
	}
	if response.Identity.NodeID != response.NodeID {
		t.Fatalf("expected identity node %s, got %s", response.NodeID, response.Identity.NodeID)
	}
	if err := response.Identity.ValidateAt(time.Now().UTC()); err != nil {
		t.Fatalf("validate transport identity: %v", err)
	}
}

func TestNewServerWithConfigRejectsMismatchedValidatorIdentity(t *testing.T) {
	signer := newConsensusSigner(t)
	_, err := NewServerWithConfig(Config{
		DataDir:               t.TempDir(),
		NodeID:                "node-a",
		ValidatorAddress:      "zph_other_validator",
		ValidatorPrivateKey:   encodedPrivateKey(t, signer.privateKey),
		BlockInterval:         0,
		SyncInterval:          0,
		EnableBlockProduction: false,
		EnablePeerSync:        false,
	})
	if !errors.Is(err, errValidatorIdentityMismatch) {
		t.Fatalf("expected validator identity mismatch error, got %v", err)
	}
}

func TestHandleBroadcastTransactionRejectsInvalidTransportIdentity(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	envelope := signedEnvelope(t, 25, 1, "peer-identity")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	signer := newConsensusSigner(t)
	identity := signedTransportIdentity(t, signer, "peer-node", time.Now().UTC())
	identity.Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	request.Header.Set(sourceNodeHeader, identity.NodeID)
	request.Header.Set(sourceValidatorHeader, identity.ValidatorAddress)
	request.Header.Set(sourceIdentityPayloadHeader, identity.Payload)
	request.Header.Set(sourcePublicKeyHeader, identity.PublicKey)
	request.Header.Set(sourceSignatureHeader, identity.Signature)
	request.Header.Set(sourceSignedAtHeader, identity.SignedAt.UTC().Format(time.RFC3339Nano))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", recorder.Code)
	}
}

func TestHandleBroadcastTransactionRejectsMissingPeerIdentityWhenRequired(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
		RequirePeerIdentity:     true,
	})

	envelope := signedEnvelope(t, 25, 1, "peer-missing-identity")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	request.Header.Set(sourceNodeHeader, "peer-node")
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", recorder.Code)
	}
}

func TestHandleBroadcastTransactionRejectsUnboundPeerValidator(t *testing.T) {
	allowedSigner := newConsensusSigner(t)
	peerSigner := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
		RequirePeerIdentity:     true,
		PeerValidatorBindings: map[string]string{
			"http://peer.example": allowedSigner.address,
		},
	})

	envelope := signedEnvelope(t, 25, 1, "peer-unbound-validator")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	identity := signedTransportIdentity(t, peerSigner, "peer-node", time.Now().UTC())
	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	request.Header.Set(sourceNodeHeader, identity.NodeID)
	request.Header.Set(sourceValidatorHeader, identity.ValidatorAddress)
	request.Header.Set(sourceIdentityPayloadHeader, identity.Payload)
	request.Header.Set(sourcePublicKeyHeader, identity.PublicKey)
	request.Header.Set(sourceSignatureHeader, identity.Signature)
	request.Header.Set(sourceSignedAtHeader, identity.SignedAt.UTC().Format(time.RFC3339Nano))
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("expected status 403, got %d", recorder.Code)
	}
}

func TestHandleProduceBlockCommitsAndExposesLatestBlock(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	envelope := signedEnvelope(t, 25, 1, "hello")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	broadcastRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	broadcastRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(broadcastRecorder, broadcastRequest)
	if broadcastRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected broadcast status 202, got %d", broadcastRecorder.Code)
	}

	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", nil)
	produceRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusOK {
		t.Fatalf("expected produce block status 200, got %d", produceRecorder.Code)
	}

	var produceResponse ProduceBlockResponse
	if err := json.NewDecoder(produceRecorder.Body).Decode(&produceResponse); err != nil {
		t.Fatalf("decode produce block response: %v", err)
	}
	if produceResponse.Block.Height != 1 {
		t.Fatalf("expected block height 1, got %d", produceResponse.Block.Height)
	}

	latestRequest := httptest.NewRequest(http.MethodGet, "/v1/blocks/latest", nil)
	latestRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(latestRecorder, latestRequest)
	if latestRecorder.Code != http.StatusOK {
		t.Fatalf("expected latest block status 200, got %d", latestRecorder.Code)
	}

	var latestResponse LatestBlockResponse
	if err := json.NewDecoder(latestRecorder.Body).Decode(&latestResponse); err != nil {
		t.Fatalf("decode latest block response: %v", err)
	}
	if latestResponse.Block.Hash != produceResponse.Block.Hash {
		t.Fatalf("expected latest block hash %s, got %s", produceResponse.Block.Hash, latestResponse.Block.Hash)
	}
}

func TestHandleElectionPersistsValidatorsAndConsensusAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	server := newTestServer(t, Config{
		DataDir:                 dataDir,
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	requestBody := bytes.NewBufferString(`{
		"candidates": [
			{"address":"zph_validator_a","selfStake":20000,"commissionRate":0.05,"missedBlocks":1},
			{"address":"zph_validator_b","selfStake":15000,"commissionRate":0.08,"missedBlocks":0}
		],
		"votes": [
			{"delegator":"delegator-1","candidate":"zph_validator_a","amount":5000},
			{"delegator":"delegator-2","candidate":"zph_validator_b","amount":3000}
		],
		"config": {"maxValidators":2,"minSelfStake":10000,"maxMissedBlocks":50}
	}`)
	request := httptest.NewRequest(http.MethodPost, "/v1/election", requestBody)
	recorder := httptest.NewRecorder()

	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected election status 200, got %d", recorder.Code)
	}

	var electionResponse ElectionResponse
	if err := json.NewDecoder(recorder.Body).Decode(&electionResponse); err != nil {
		t.Fatalf("decode election response: %v", err)
	}
	if electionResponse.ValidatorSetVersion != 1 {
		t.Fatalf("expected validator set version 1, got %d", electionResponse.ValidatorSetVersion)
	}
	if len(electionResponse.Validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(electionResponse.Validators))
	}
	if electionResponse.Consensus.NextProposer != "zph_validator_a" {
		t.Fatalf("expected proposer zph_validator_a, got %s", electionResponse.Consensus.NextProposer)
	}

	validatorsRequest := httptest.NewRequest(http.MethodGet, "/v1/validators", nil)
	validatorsRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(validatorsRecorder, validatorsRequest)
	if validatorsRecorder.Code != http.StatusOK {
		t.Fatalf("expected validators status 200, got %d", validatorsRecorder.Code)
	}

	server.Close()
	reopened, err := NewServerWithConfig(Config{
		DataDir:                 dataDir,
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})
	if err != nil {
		t.Fatalf("reopen server: %v", err)
	}
	defer reopened.Close()

	consensusRequest := httptest.NewRequest(http.MethodGet, "/v1/consensus", nil)
	consensusRecorder := httptest.NewRecorder()
	reopened.Handler().ServeHTTP(consensusRecorder, consensusRequest)
	if consensusRecorder.Code != http.StatusOK {
		t.Fatalf("expected consensus status 200, got %d", consensusRecorder.Code)
	}

	var consensusResponse ConsensusResponse
	if err := json.NewDecoder(consensusRecorder.Body).Decode(&consensusResponse); err != nil {
		t.Fatalf("decode consensus response: %v", err)
	}
	if consensusResponse.ValidatorSet.Version != 1 {
		t.Fatalf("expected reopened validator set version 1, got %d", consensusResponse.ValidatorSet.Version)
	}
	if len(consensusResponse.ValidatorSet.Validators) != 2 {
		t.Fatalf("expected reopened validator count 2, got %d", len(consensusResponse.ValidatorSet.Validators))
	}
	if consensusResponse.Consensus.NextProposer != "zph_validator_a" {
		t.Fatalf("expected reopened proposer zph_validator_a, got %s", consensusResponse.Consensus.NextProposer)
	}
}

func TestHandleProduceBlockRejectsWhenLocalValidatorIsNotScheduled(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		ValidatorAddress:        "zph_validator_b",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
		EnforceProposerSchedule: true,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: "zph_validator_a", VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: "zph_validator_b", VotingPower: 30, SelfStake: 20, DelegatedStake: 10},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "scheduled")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := server.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", nil)
	produceRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusConflict {
		t.Fatalf("expected produce block status 409, got %d", produceRecorder.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(produceRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if !strings.Contains(response["error"], "zph_validator_a") {
		t.Fatalf("expected scheduled proposer in error message, got %q", response["error"])
	}
}

func TestHandleConsensusProposalAndVotesExposeArtifacts(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedConsensusProposal(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 13, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "block-1-tx")})
	proposalBody, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	proposalRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(proposalBody))
	proposalRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(proposalRecorder, proposalRequest)
	if proposalRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected proposal status 202, got %d", proposalRecorder.Code)
	}

	firstVote := signedConsensusVote(t, proposer, 1, 0, proposal.BlockHash)
	firstVoteBody, err := json.Marshal(firstVote)
	if err != nil {
		t.Fatalf("marshal first vote: %v", err)
	}
	firstVoteRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(firstVoteBody))
	firstVoteRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(firstVoteRecorder, firstVoteRequest)
	if firstVoteRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected first vote status 202, got %d", firstVoteRecorder.Code)
	}

	secondVote := signedConsensusVote(t, voter, 1, 0, proposal.BlockHash)
	secondVoteBody, err := json.Marshal(secondVote)
	if err != nil {
		t.Fatalf("marshal second vote: %v", err)
	}
	secondVoteRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(secondVoteBody))
	secondVoteRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(secondVoteRecorder, secondVoteRequest)
	if secondVoteRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected second vote status 202, got %d", secondVoteRecorder.Code)
	}

	consensusRequest := httptest.NewRequest(http.MethodGet, "/v1/consensus", nil)
	consensusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(consensusRecorder, consensusRequest)
	if consensusRecorder.Code != http.StatusOK {
		t.Fatalf("expected consensus status 200, got %d", consensusRecorder.Code)
	}

	var response ConsensusResponse
	if err := json.NewDecoder(consensusRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode consensus response: %v", err)
	}
	if response.Artifacts.LatestCertificate == nil {
		t.Fatal("expected latest certificate in consensus response")
	}
	if response.Artifacts.LatestCertificate.BlockHash != proposal.BlockHash {
		t.Fatalf("expected certificate for block %s, got %+v", proposal.BlockHash, response.Artifacts.LatestCertificate)
	}
	if response.Artifacts.ProposalCount != 1 || response.Artifacts.VoteCount != 2 || response.Artifacts.CertificateCount != 1 {
		t.Fatalf("unexpected artifact counts: %+v", response.Artifacts)
	}
}

func TestHandleBlockTemplateAndConsensusGatedProduceBlock(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorAddress:             proposer.address,
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "template-produce")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := server.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	templateRequest := httptest.NewRequest(http.MethodGet, "/v1/dev/block-template", nil)
	templateRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(templateRecorder, templateRequest)
	if templateRecorder.Code != http.StatusOK {
		t.Fatalf("expected template status 200, got %d", templateRecorder.Code)
	}

	var templateResponse BlockTemplateResponse
	if err := json.NewDecoder(templateRecorder.Body).Decode(&templateResponse); err != nil {
		t.Fatalf("decode template response: %v", err)
	}

	produceBody, err := json.Marshal(ProduceBlockRequest{ProducedAt: &templateResponse.Block.ProducedAt})
	if err != nil {
		t.Fatalf("marshal produce request: %v", err)
	}
	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", bytes.NewReader(produceBody))
	produceRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusConflict {
		t.Fatalf("expected gated produce status 409 without certificate, got %d", produceRecorder.Code)
	}

	proposal := signedConsensusProposal(t, proposer, templateResponse.Block.Height, 0, templateResponse.Block.PreviousHash, templateResponse.Block.ProducedAt, templateResponse.Block.Transactions)
	proposalBody, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	proposalRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(proposalBody))
	proposalRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(proposalRecorder, proposalRequest)
	if proposalRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected proposal status 202, got %d", proposalRecorder.Code)
	}

	for _, vote := range []consensus.Vote{
		signedConsensusVote(t, proposer, templateResponse.Block.Height, 0, templateResponse.Block.Hash),
		signedConsensusVote(t, voter, templateResponse.Block.Height, 0, templateResponse.Block.Hash),
	} {
		voteBody, err := json.Marshal(vote)
		if err != nil {
			t.Fatalf("marshal vote: %v", err)
		}
		voteRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(voteBody))
		voteRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(voteRecorder, voteRequest)
		if voteRecorder.Code != http.StatusAccepted {
			t.Fatalf("expected vote status 202, got %d", voteRecorder.Code)
		}
	}

	wrongProducedAt := templateResponse.Block.ProducedAt.Add(time.Second)
	wrongProduceBody, err := json.Marshal(ProduceBlockRequest{ProducedAt: &wrongProducedAt})
	if err != nil {
		t.Fatalf("marshal wrong produce request: %v", err)
	}
	wrongProduceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", bytes.NewReader(wrongProduceBody))
	wrongProduceRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(wrongProduceRecorder, wrongProduceRequest)
	if wrongProduceRecorder.Code != http.StatusConflict {
		t.Fatalf("expected produce status 409 for mismatched producedAt, got %d", wrongProduceRecorder.Code)
	}

	produceRequest = httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", bytes.NewReader(produceBody))
	produceRecorder = httptest.NewRecorder()
	server.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusOK {
		t.Fatalf("expected produce status 200 after certificate, got %d", produceRecorder.Code)
	}

	var produceResponse ProduceBlockResponse
	if err := json.NewDecoder(produceRecorder.Body).Decode(&produceResponse); err != nil {
		t.Fatalf("decode produce response: %v", err)
	}
	if produceResponse.Block.Hash != templateResponse.Block.Hash {
		t.Fatalf("expected produced block hash %s, got %s", templateResponse.Block.Hash, produceResponse.Block.Hash)
	}
}
func TestHandleConsensusGatedProduceBlockUsesProposalBodyWithoutMempool(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorAddress:             proposer.address,
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "proposal-body-api")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	producedAt := time.Date(2026, time.March, 24, 9, 45, 0, 0, time.UTC)
	proposal := signedConsensusProposal(t, proposer, 1, 0, "", producedAt, []tx.Envelope{envelope})
	proposalBody, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	proposalRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(proposalBody))
	proposalRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(proposalRecorder, proposalRequest)
	if proposalRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected proposal status 202, got %d", proposalRecorder.Code)
	}

	for _, vote := range []consensus.Vote{
		signedConsensusVote(t, proposer, 1, 0, proposal.BlockHash),
		signedConsensusVote(t, voter, 1, 0, proposal.BlockHash),
	} {
		voteBody, err := json.Marshal(vote)
		if err != nil {
			t.Fatalf("marshal vote: %v", err)
		}
		voteRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(voteBody))
		voteRecorder := httptest.NewRecorder()
		server.Handler().ServeHTTP(voteRecorder, voteRequest)
		if voteRecorder.Code != http.StatusAccepted {
			t.Fatalf("expected vote status 202, got %d", voteRecorder.Code)
		}
	}
	if server.ledger.MempoolSize() != 0 {
		t.Fatalf("expected empty mempool before certified produce, got %d", server.ledger.MempoolSize())
	}

	produceBody, err := json.Marshal(ProduceBlockRequest{ProducedAt: &producedAt})
	if err != nil {
		t.Fatalf("marshal produce request: %v", err)
	}
	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", bytes.NewReader(produceBody))
	produceRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusOK {
		t.Fatalf("expected produce status 200, got %d", produceRecorder.Code)
	}

	var produceResponse ProduceBlockResponse
	if err := json.NewDecoder(produceRecorder.Body).Decode(&produceResponse); err != nil {
		t.Fatalf("decode produce response: %v", err)
	}
	if produceResponse.Block.Hash != proposal.BlockHash {
		t.Fatalf("expected produced block hash %s, got %s", proposal.BlockHash, produceResponse.Block.Hash)
	}
	if sender := server.ledger.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("unexpected sender state after proposal-body produce: %+v", sender)
	}
}

func TestPeerSyncRecordsVerifiedAndAdmittedTransportIdentity(t *testing.T) {
	peerSigner := newConsensusSigner(t)
	peerServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		ValidatorPrivateKey:     encodedPrivateKey(t, peerSigner.privateKey),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		PeerURLs:                []string{peerHTTP.URL},
		PeerValidatorBindings:   map[string]string{peerHTTP.URL: peerSigner.address},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
		RequirePeerIdentity:     true,
	})

	waitFor(t, func() bool {
		peers := mainServer.peerSnapshot()
		return len(peers) == 1 && peers[0].Admitted
	})

	peers := mainServer.peerSnapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer view, got %d", len(peers))
	}
	if !peers[0].IdentityPresent || !peers[0].IdentityVerified || !peers[0].Admitted {
		t.Fatalf("expected admitted verified transport identity, got %+v", peers[0])
	}
	if peers[0].ValidatorAddress != peerSigner.address {
		t.Fatalf("expected peer validator %s, got %s", peerSigner.address, peers[0].ValidatorAddress)
	}
	if peers[0].ExpectedValidator != peerSigner.address {
		t.Fatalf("expected bound validator %s, got %s", peerSigner.address, peers[0].ExpectedValidator)
	}
	if peers[0].IdentityError != "" {
		t.Fatalf("expected empty identity error, got %q", peers[0].IdentityError)
	}
	if peers[0].AdmissionError != "" {
		t.Fatalf("expected empty admission error, got %q", peers[0].AdmissionError)
	}
}

func TestPeerSyncRejectsUnexpectedPeerValidatorBinding(t *testing.T) {
	peerSigner := newConsensusSigner(t)
	expectedSigner := newConsensusSigner(t)
	peerServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		ValidatorPrivateKey:     encodedPrivateKey(t, peerSigner.privateKey),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		PeerURLs:                []string{peerHTTP.URL},
		PeerValidatorBindings:   map[string]string{peerHTTP.URL: expectedSigner.address},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
		RequirePeerIdentity:     true,
	})

	waitFor(t, func() bool {
		peers := mainServer.peerSnapshot()
		return len(peers) == 1 && peers[0].Reachable && !peers[0].Admitted
	})

	peers := mainServer.peerSnapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer view, got %d", len(peers))
	}
	if !peers[0].IdentityPresent || !peers[0].IdentityVerified {
		t.Fatalf("expected verified identity before admission failure, got %+v", peers[0])
	}
	if peers[0].ExpectedValidator != expectedSigner.address {
		t.Fatalf("expected bound validator %s, got %s", expectedSigner.address, peers[0].ExpectedValidator)
	}
	if peers[0].Admitted {
		t.Fatalf("expected peer admission to fail, got %+v", peers[0])
	}
	if !strings.Contains(peers[0].AdmissionError, expectedSigner.address) {
		t.Fatalf("expected admission error to mention %s, got %q", expectedSigner.address, peers[0].AdmissionError)
	}
}

func TestPeerBroadcastSkipsUnadmittedPeer(t *testing.T) {
	peerSigner := newConsensusSigner(t)
	unexpectedSigner := newConsensusSigner(t)
	peerServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		ValidatorPrivateKey:     encodedPrivateKey(t, peerSigner.privateKey),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		PeerURLs:                []string{peerHTTP.URL},
		PeerValidatorBindings:   map[string]string{peerHTTP.URL: unexpectedSigner.address},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
		RequirePeerIdentity:     true,
	})

	envelope := signedEnvelope(t, 25, 1, "unadmitted-peer")
	if _, err := mainServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusAccepted {
		t.Fatalf("expected broadcast status 202, got %d", recorder.Code)
	}

	time.Sleep(200 * time.Millisecond)
	if peerServer.ledger.MempoolSize() != 0 {
		t.Fatalf("expected unadmitted peer mempool to stay empty, got %d", peerServer.ledger.MempoolSize())
	}
	if peerServer.ledger.View(envelope.From).Balance != 0 {
		t.Fatalf("expected unadmitted peer account to remain untouched, got %+v", peerServer.ledger.View(envelope.From))
	}

	peers := mainServer.peerSnapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer view, got %d", len(peers))
	}
	if peers[0].Admitted {
		t.Fatalf("expected peer to remain unadmitted, got %+v", peers[0])
	}
}

func TestPeerReplicationPropagatesConsensusProposalAndVotes(t *testing.T) {
	peerServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		PeerURLs:                []string{peerHTTP.URL},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := mainServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set main validators: %v", err)
	}
	if _, err := peerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set peer validators: %v", err)
	}

	proposal := signedConsensusProposal(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 13, 15, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "peer-block-1-tx")})
	proposalBody, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	proposalRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(proposalBody))
	proposalRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(proposalRecorder, proposalRequest)
	if proposalRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected proposal status 202, got %d", proposalRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.ConsensusArtifacts().ProposalCount == 1
	})

	voteA := signedConsensusVote(t, proposer, 1, 0, proposal.BlockHash)
	voteABody, err := json.Marshal(voteA)
	if err != nil {
		t.Fatalf("marshal vote A: %v", err)
	}
	voteARequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(voteABody))
	voteARecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(voteARecorder, voteARequest)
	if voteARecorder.Code != http.StatusAccepted {
		t.Fatalf("expected vote A status 202, got %d", voteARecorder.Code)
	}

	voteB := signedConsensusVote(t, voter, 1, 0, proposal.BlockHash)
	voteBBody, err := json.Marshal(voteB)
	if err != nil {
		t.Fatalf("marshal vote B: %v", err)
	}
	voteBRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(voteBBody))
	voteBRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(voteBRecorder, voteBRequest)
	if voteBRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected vote B status 202, got %d", voteBRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.ConsensusArtifacts().LatestCertificate != nil
	})
	if peerServer.ledger.ConsensusArtifacts().LatestCertificate.BlockHash != proposal.BlockHash {
		t.Fatalf("expected replicated certificate for block %s, got %+v", proposal.BlockHash, peerServer.ledger.ConsensusArtifacts().LatestCertificate)
	}
}

func TestPeerReplicationImportsCertifiedBlockWhenConsensusRequired(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}

	peerServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorAddress:             proposer.address,
		PeerURLs:                     []string{peerHTTP.URL},
		BlockInterval:                0,
		SyncInterval:                 50 * time.Millisecond,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnablePeerSync:               true,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	if _, err := mainServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set main validators: %v", err)
	}
	if _, err := peerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set peer validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "peer-certified")
	faucetBody := bytes.NewBufferString(`{"address":"` + envelope.From + `","amount":100}`)
	faucetRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/faucet", faucetBody)
	faucetRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(faucetRecorder, faucetRequest)
	if faucetRecorder.Code != http.StatusOK {
		t.Fatalf("expected faucet status 200, got %d", faucetRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.View(envelope.From).Balance == 100
	})

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}
	broadcastRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	broadcastRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(broadcastRecorder, broadcastRequest)
	if broadcastRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected broadcast status 202, got %d", broadcastRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.MempoolSize() == 1
	})

	templateRequest := httptest.NewRequest(http.MethodGet, "/v1/dev/block-template", nil)
	templateRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(templateRecorder, templateRequest)
	if templateRecorder.Code != http.StatusOK {
		t.Fatalf("expected template status 200, got %d", templateRecorder.Code)
	}

	var templateResponse BlockTemplateResponse
	if err := json.NewDecoder(templateRecorder.Body).Decode(&templateResponse); err != nil {
		t.Fatalf("decode template response: %v", err)
	}

	proposal := signedConsensusProposal(t, proposer, templateResponse.Block.Height, 0, templateResponse.Block.PreviousHash, templateResponse.Block.ProducedAt, templateResponse.Block.Transactions)
	proposalBody, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	proposalRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(proposalBody))
	proposalRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(proposalRecorder, proposalRequest)
	if proposalRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected proposal status 202, got %d", proposalRecorder.Code)
	}

	for _, vote := range []consensus.Vote{
		signedConsensusVote(t, proposer, templateResponse.Block.Height, 0, templateResponse.Block.Hash),
		signedConsensusVote(t, voter, templateResponse.Block.Height, 0, templateResponse.Block.Hash),
	} {
		voteBody, err := json.Marshal(vote)
		if err != nil {
			t.Fatalf("marshal vote: %v", err)
		}
		voteRequest := httptest.NewRequest(http.MethodPost, "/v1/consensus/votes", bytes.NewReader(voteBody))
		voteRecorder := httptest.NewRecorder()
		mainServer.Handler().ServeHTTP(voteRecorder, voteRequest)
		if voteRecorder.Code != http.StatusAccepted {
			t.Fatalf("expected vote status 202, got %d", voteRecorder.Code)
		}
	}

	waitFor(t, func() bool {
		return peerServer.ledger.ConsensusArtifacts().LatestCertificate != nil && peerServer.ledger.ConsensusArtifacts().LatestCertificate.BlockHash == templateResponse.Block.Hash
	})

	produceBody, err := json.Marshal(ProduceBlockRequest{ProducedAt: &templateResponse.Block.ProducedAt})
	if err != nil {
		t.Fatalf("marshal produce request: %v", err)
	}
	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", bytes.NewReader(produceBody))
	produceRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusOK {
		t.Fatalf("expected produce status 200, got %d", produceRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.Status().Height == 1
	})
	peerAccount := peerServer.ledger.View(envelope.From)
	if peerAccount.Balance != 75 || peerAccount.Nonce != 1 {
		t.Fatalf("unexpected peer sender state after certified replication: %+v", peerAccount)
	}
}
func TestPeerReplicationPropagatesFaucetTransactionAndBlock(t *testing.T) {
	peerServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-b",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	peerHTTP := httptest.NewServer(peerServer.Handler())
	defer peerHTTP.Close()

	mainServer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		PeerURLs:                []string{peerHTTP.URL},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          true,
	})

	envelope := signedEnvelope(t, 25, 1, "peer-test")
	faucetBody := bytes.NewBufferString(`{"address":"` + envelope.From + `","amount":100}`)
	faucetRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/faucet", faucetBody)
	faucetRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(faucetRecorder, faucetRequest)
	if faucetRecorder.Code != http.StatusOK {
		t.Fatalf("expected faucet status 200, got %d", faucetRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.View(envelope.From).Balance == 100
	})

	body, err := json.Marshal(envelope)
	if err != nil {
		t.Fatalf("marshal transaction: %v", err)
	}

	broadcastRequest := httptest.NewRequest(http.MethodPost, "/v1/transactions", bytes.NewReader(body))
	broadcastRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(broadcastRecorder, broadcastRequest)
	if broadcastRecorder.Code != http.StatusAccepted {
		t.Fatalf("expected broadcast status 202, got %d", broadcastRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.MempoolSize() == 1
	})

	produceRequest := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", nil)
	produceRecorder := httptest.NewRecorder()
	mainServer.Handler().ServeHTTP(produceRecorder, produceRequest)
	if produceRecorder.Code != http.StatusOK {
		t.Fatalf("expected produce block status 200, got %d", produceRecorder.Code)
	}

	waitFor(t, func() bool {
		return peerServer.ledger.Status().Height == 1
	})

	peerAccount := peerServer.ledger.View(envelope.From)
	if peerAccount.Balance != 75 || peerAccount.Nonce != 1 {
		t.Fatalf("unexpected peer sender state after replication: %+v", peerAccount)
	}
}

func TestPeerSyncRestoresSnapshotForLateJoiningNode(t *testing.T) {
	producer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "producer",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})
	producerHTTP := httptest.NewServer(producer.Handler())
	defer producerHTTP.Close()

	envelope := signedEnvelope(t, 25, 1, "late-join")
	if _, err := producer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}
	if _, err := producer.produceLocalBlock(time.Time{}); err != nil {
		t.Fatalf("produce local block: %v", err)
	}

	replica := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "replica",
		PeerURLs:                []string{producerHTTP.URL},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	waitFor(t, func() bool {
		return replica.ledger.Status().Height == 1
	})

	replicaAccount := replica.ledger.View(envelope.From)
	if replicaAccount.Balance != 75 || replicaAccount.Nonce != 1 {
		t.Fatalf("unexpected replica sender state after sync: %+v", replicaAccount)
	}
}

func TestPeerSyncRestoresSnapshotWhenSameHeightDiverges(t *testing.T) {
	producer := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "producer",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})
	producerHTTP := httptest.NewServer(producer.Handler())
	defer producerHTTP.Close()

	producerEnvelope := signedEnvelope(t, 25, 1, "canonical")
	if _, err := producer.ledger.Credit(producerEnvelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.ledger.Accept(producerEnvelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}
	producerBlock, err := producer.produceLocalBlock(time.Time{})
	if err != nil {
		t.Fatalf("produce producer block: %v", err)
	}

	replicaDataDir := t.TempDir()
	replicaSeed := newTestServer(t, Config{
		DataDir:                 replicaDataDir,
		NodeID:                  "replica-seed",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   true,
		EnablePeerSync:          false,
	})

	replicaEnvelope := signedEnvelope(t, 10, 1, "divergent")
	if _, err := replicaSeed.ledger.Credit(replicaEnvelope.From, 100); err != nil {
		t.Fatalf("credit replica sender: %v", err)
	}
	if _, err := replicaSeed.ledger.Accept(replicaEnvelope); err != nil {
		t.Fatalf("accept replica transaction: %v", err)
	}
	if _, err := replicaSeed.produceLocalBlock(time.Time{}); err != nil {
		t.Fatalf("produce replica block: %v", err)
	}
	replicaSeed.Close()

	replica := newTestServer(t, Config{
		DataDir:                 replicaDataDir,
		NodeID:                  "replica",
		PeerURLs:                []string{producerHTTP.URL},
		BlockInterval:           0,
		SyncInterval:            50 * time.Millisecond,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	waitFor(t, func() bool {
		latest, ok := replica.ledger.LatestBlock()
		return ok && latest.Hash == producerBlock.Hash
	})

	latest, ok := replica.ledger.LatestBlock()
	if !ok {
		t.Fatal("expected latest block on replica after divergence repair")
	}
	if latest.Hash != producerBlock.Hash {
		t.Fatalf("expected replica hash %s after repair, got %s", producerBlock.Hash, latest.Hash)
	}

	replicaAccount := replica.ledger.View(producerEnvelope.From)
	if replicaAccount.Balance != 75 || replicaAccount.Nonce != 1 {
		t.Fatalf("unexpected replica sender state after divergence repair: %+v", replicaAccount)
	}
}

func newTestServer(t *testing.T, config Config) *Server {
	t.Helper()

	server, err := NewServerWithConfig(config)
	if err != nil {
		t.Fatalf("create server: %v", err)
	}

	t.Cleanup(server.Close)
	return server
}

func waitFor(t *testing.T, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}

	t.Fatal("condition was not met before timeout")
}

func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) tx.Envelope {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}

	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal public key: %v", err)
	}

	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil {
		t.Fatalf("derive address: %v", err)
	}

	envelope := tx.Envelope{
		From:      address,
		To:        "zph_receiver",
		Amount:    amount,
		Nonce:     nonce,
		Memo:      memo,
		PublicKey: encodedPublicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature = signPayload(t, privateKey, envelope.Payload)

	return envelope
}

func signPayload(t *testing.T, privateKey *ecdsa.PrivateKey, payload string) string {
	t.Helper()

	digest := sha256.Sum256([]byte(payload))
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		t.Fatalf("sign payload: %v", err)
	}

	signature := append(pad32(r), pad32(s)...)
	return base64.StdEncoding.EncodeToString(signature)
}

func pad32(value *big.Int) []byte {
	bytes := value.Bytes()
	if len(bytes) >= 32 {
		return bytes[len(bytes)-32:]
	}

	padded := make([]byte, 32)
	copy(padded[32-len(bytes):], bytes)
	return padded
}

type consensusSigner struct {
	privateKey *ecdsa.PrivateKey
	address    string
	publicKey  string
}

func newConsensusSigner(t *testing.T) consensusSigner {
	t.Helper()

	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate consensus key: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal consensus public key: %v", err)
	}
	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil {
		t.Fatalf("derive consensus address: %v", err)
	}
	return consensusSigner{privateKey: privateKey, address: address, publicKey: encodedPublicKey}
}

func signedConsensusProposal(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {
	t.Helper()

	transactionIDs := make([]string, 0, len(transactions))
	for _, envelope := range transactions {
		transactionIDs = append(transactionIDs, tx.ID(envelope))
	}
	proposal := consensus.Proposal{
		Height:         height,
		Round:          round,
		PreviousHash:   previousHash,
		ProducedAt:     producedAt,
		TransactionIDs: append([]string(nil), transactionIDs...),
		Transactions:   append([]tx.Envelope(nil), transactions...),
		Proposer:       signer.address,
		PublicKey:      signer.publicKey,
	}
	proposal.BlockHash = proposal.CandidateHash()
	proposal.Payload = proposal.CanonicalPayload()
	proposal.Signature = signPayload(t, signer.privateKey, proposal.Payload)
	return proposal
}

func signedConsensusVote(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string) consensus.Vote {
	t.Helper()

	vote := consensus.Vote{
		Height:    height,
		Round:     round,
		BlockHash: blockHash,
		Voter:     signer.address,
		PublicKey: signer.publicKey,
	}
	vote.Payload = vote.CanonicalPayload()
	vote.Signature = signPayload(t, signer.privateKey, vote.Payload)
	return vote
}

func encodedPrivateKey(t *testing.T, privateKey *ecdsa.PrivateKey) string {
	t.Helper()

	encoded, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		t.Fatalf("marshal private key: %v", err)
	}
	return base64.StdEncoding.EncodeToString(encoded)
}

func signedTransportIdentity(t *testing.T, signer consensusSigner, nodeID string, signedAt time.Time) TransportIdentity {
	t.Helper()

	identity := TransportIdentity{
		NodeID:           nodeID,
		ValidatorAddress: signer.address,
		PublicKey:        signer.publicKey,
		SignedAt:         signedAt.UTC(),
	}
	identity.Payload = identity.CanonicalPayload()
	signature, err := signTransportIdentityPayload(signer.privateKey, identity.Payload)
	if err != nil {
		t.Fatalf("sign transport identity: %v", err)
	}
	identity.Signature = signature
	return identity
}

func consensusTestHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

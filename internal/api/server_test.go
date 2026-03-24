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
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
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

func TestNewServerWithConfigRejectsConsensusAutomationWithoutValidatorKey(t *testing.T) {
	_, err := NewServerWithConfig(Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "node-a",
		EnableConsensusAutomation: true,
		ConsensusInterval:         10 * time.Millisecond,
		EnableBlockProduction:     false,
		EnablePeerSync:            false,
	})
	if !errors.Is(err, errConsensusAutomationRequiresIdentity) {
		t.Fatalf("expected consensus automation identity error, got %v", err)
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

func TestHandleConsensusExposesRoundEvidence(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "node-a",
		ValidatorPrivateKey:       encodedPrivateKey(t, proposer.privateKey),
		ConsensusRoundTimeout:     30 * time.Second,
		BlockInterval:             0,
		SyncInterval:              0,
		MaxTransactionsPerBlock:   10,
		EnableBlockProduction:     false,
		EnableConsensusAutomation: false,
		EnablePeerSync:            false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}
	startedAt := time.Date(2026, time.March, 24, 14, 0, 0, 0, time.UTC)
	if _, err := server.ledger.EnsureRoundStarted(startedAt); err != nil {
		t.Fatalf("ensure round started: %v", err)
	}

	proposal := signedConsensusProposal(t, proposer, 1, 0, "", time.Date(2026, time.March, 24, 14, 1, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "round-evidence")})
	if err := server.ledger.RecordProposal(proposal); err != nil {
		t.Fatalf("record proposal: %v", err)
	}
	if _, _, err := server.ledger.RecordVote(signedConsensusVote(t, proposer, 1, 0, proposal.BlockHash)); err != nil {
		t.Fatalf("record local vote: %v", err)
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
	if response.RoundEvidence.State != "collecting_votes" {
		t.Fatalf("expected collecting_votes state, got %+v", response.RoundEvidence)
	}
	if !response.RoundEvidence.ProposalPresent || response.RoundEvidence.ProposalBlockHash != proposal.BlockHash {
		t.Fatalf("expected round evidence proposal for %s, got %+v", proposal.BlockHash, response.RoundEvidence)
	}
	if !response.RoundEvidence.LocalVotePresent || response.RoundEvidence.LocalVoteBlockHash != proposal.BlockHash {
		t.Fatalf("expected local vote evidence for %s, got %+v", proposal.BlockHash, response.RoundEvidence)
	}
	if response.RoundEvidence.CertificatePresent {
		t.Fatalf("expected no certificate yet, got %+v", response.RoundEvidence)
	}
	if response.RoundEvidence.DeadlineAt == nil || !response.RoundEvidence.DeadlineAt.After(startedAt) {
		t.Fatalf("expected round deadline after %s, got %+v", startedAt, response.RoundEvidence)
	}
	if !response.RoundEvidence.PartialQuorum || response.RoundEvidence.LeadingVotePower != 60 || response.RoundEvidence.QuorumRemaining != 7 {
		t.Fatalf("expected partial quorum details in round evidence, got %+v", response.RoundEvidence)
	}
	foundPartialQuorum := false
	for _, warning := range response.RoundEvidence.Warnings {
		if warning == "partial_quorum" {
			foundPartialQuorum = true
			break
		}
	}
	if !foundPartialQuorum {
		t.Fatalf("expected partial_quorum warning in round evidence, got %+v", response.RoundEvidence.Warnings)
	}
}

func TestHandleConsensusExposesReproposalAndTimeoutWarnings(t *testing.T) {
	proposer := newConsensusSigner(t)
	nextProposer := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "node-a",
		ValidatorPrivateKey:       encodedPrivateKey(t, proposer.privateKey),
		ConsensusRoundTimeout:     30 * time.Second,
		BlockInterval:             0,
		SyncInterval:              0,
		MaxTransactionsPerBlock:   10,
		EnableBlockProduction:     false,
		EnableConsensusAutomation: false,
		EnablePeerSync:            false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: nextProposer.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}
	startedAt := time.Now().UTC().Add(-2 * time.Minute)
	if _, err := server.ledger.EnsureRoundStarted(startedAt); err != nil {
		t.Fatalf("ensure round started: %v", err)
	}

	proposal := signedConsensusProposal(t, proposer, 1, 0, "", startedAt.Add(5*time.Second), []tx.Envelope{signedEnvelope(t, 5, 1, "reproposal-warning")})
	if err := server.ledger.RecordProposal(proposal); err != nil {
		t.Fatalf("record proposal: %v", err)
	}
	if _, err := server.ledger.AdvanceRound(startedAt.Add(45 * time.Second)); err != nil {
		t.Fatalf("advance round: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/consensus", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ConsensusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode consensus response: %v", err)
	}
	if response.RoundEvidence.State != "waiting_for_reproposal" || !response.RoundEvidence.TimedOut {
		t.Fatalf("expected waiting_for_reproposal timed-out state, got %+v", response.RoundEvidence)
	}
	if response.RoundEvidence.LatestKnownProposalRound == nil || *response.RoundEvidence.LatestKnownProposalRound != 0 {
		t.Fatalf("expected latest known proposal round 0, got %+v", response.RoundEvidence)
	}
	foundTimeout := false
	foundReproposal := false
	for _, warning := range response.RoundEvidence.Warnings {
		if warning == "timeout_elapsed" {
			foundTimeout = true
		}
		if warning == "reproposal_pending" {
			foundReproposal = true
		}
	}
	if !foundTimeout || !foundReproposal {
		t.Fatalf("expected timeout and reproposal warnings, got %+v", response.RoundEvidence.Warnings)
	}
}

func TestHandleConsensusExposesRoundHistoryAcrossRounds(t *testing.T) {
	proposer := newConsensusSigner(t)
	nextProposer := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "node-a",
		ValidatorPrivateKey:       encodedPrivateKey(t, proposer.privateKey),
		ConsensusRoundTimeout:     30 * time.Second,
		BlockInterval:             0,
		SyncInterval:              0,
		MaxTransactionsPerBlock:   10,
		EnableBlockProduction:     false,
		EnableConsensusAutomation: false,
		EnablePeerSync:            false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: nextProposer.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}
	roundZeroStarted := time.Date(2026, time.March, 24, 16, 0, 0, 0, time.UTC)
	if _, err := server.ledger.EnsureRoundStarted(roundZeroStarted); err != nil {
		t.Fatalf("ensure round started: %v", err)
	}

	proposalRound0 := signedConsensusProposal(t, proposer, 1, 0, "", roundZeroStarted.Add(5*time.Second), []tx.Envelope{signedEnvelope(t, 5, 1, "history-round-0")})
	if err := server.ledger.RecordProposal(proposalRound0); err != nil {
		t.Fatalf("record round 0 proposal: %v", err)
	}
	if _, _, err := server.ledger.RecordVote(signedConsensusVote(t, proposer, 1, 0, proposalRound0.BlockHash)); err != nil {
		t.Fatalf("record round 0 vote: %v", err)
	}

	roundOneStarted := roundZeroStarted.Add(45 * time.Second)
	if _, err := server.ledger.AdvanceRound(roundOneStarted); err != nil {
		t.Fatalf("advance round: %v", err)
	}
	proposalRound1 := signedConsensusProposal(t, nextProposer, 1, 1, "", roundOneStarted.Add(5*time.Second), []tx.Envelope{signedEnvelope(t, 6, 1, "history-round-1")})
	if err := server.ledger.RecordProposal(proposalRound1); err != nil {
		t.Fatalf("record round 1 proposal: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/v1/consensus", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", recorder.Code)
	}

	var response ConsensusResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode consensus response: %v", err)
	}
	if response.RoundHistory.Height != 1 {
		t.Fatalf("expected round history height 1, got %+v", response.RoundHistory)
	}
	if len(response.RoundHistory.Rounds) != 2 {
		t.Fatalf("expected two rounds in round history, got %+v", response.RoundHistory)
	}

	round0 := response.RoundHistory.Rounds[0]
	if round0.Round != 0 || round0.Active || round0.ScheduledProposer != proposer.address {
		t.Fatalf("unexpected round 0 history %+v", round0)
	}
	if !round0.ProposalPresent || round0.ProposalBlockHash != proposalRound0.BlockHash || round0.ProposalProposer != proposer.address {
		t.Fatalf("unexpected round 0 proposal history %+v", round0)
	}
	if len(round0.VoteTallies) != 1 || round0.VoteTallies[0].VotingPower != 60 || round0.VoteTallies[0].BlockHash != proposalRound0.BlockHash {
		t.Fatalf("unexpected round 0 tallies %+v", round0.VoteTallies)
	}

	round1 := response.RoundHistory.Rounds[1]
	if round1.Round != 1 || !round1.Active || round1.ScheduledProposer != nextProposer.address {
		t.Fatalf("unexpected round 1 history %+v", round1)
	}
	if round1.StartedAt == nil || !round1.StartedAt.Equal(roundOneStarted) {
		t.Fatalf("expected round 1 started at %s, got %+v", roundOneStarted, round1)
	}
	if !round1.ProposalPresent || round1.ProposalBlockHash != proposalRound1.BlockHash || round1.ProposalProposer != nextProposer.address {
		t.Fatalf("unexpected round 1 proposal history %+v", round1)
	}
	if len(round1.VoteTallies) != 0 {
		t.Fatalf("expected no round 1 votes yet, got %+v", round1.VoteTallies)
	}
	if round1.CertificatePresent {
		t.Fatalf("expected no round 1 certificate, got %+v", round1)
	}
}

func TestHandleStatusExposesConsensusRecovery(t *testing.T) {
	validator := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "node-a",
		ValidatorPrivateKey:     encodedPrivateKey(t, validator.privateKey),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: validator.address, VotingPower: 100, SelfStake: 100},
	}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedConsensusProposal(t, validator, 1, 0, "", time.Date(2026, time.March, 24, 14, 30, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "recovery-view")})
	if err := server.ledger.RecordProposalWithAction(proposal, &ledger.ConsensusAction{
		Type:       ledger.ConsensusActionProposal,
		Height:     proposal.Height,
		Round:      proposal.Round,
		BlockHash:  proposal.BlockHash,
		Validator:  proposal.Proposer,
		RecordedAt: proposal.ProposedAt,
		Note:       "test local proposal",
	}); err != nil {
		t.Fatalf("record proposal with action: %v", err)
	}
	vote := signedConsensusVote(t, validator, 1, 0, proposal.BlockHash)
	if _, _, err := server.ledger.RecordVoteWithAction(vote, &ledger.ConsensusAction{
		Type:       ledger.ConsensusActionVote,
		Height:     vote.Height,
		Round:      vote.Round,
		BlockHash:  vote.BlockHash,
		Validator:  vote.Voter,
		RecordedAt: vote.VotedAt,
		Note:       "test local vote",
	}); err != nil {
		t.Fatalf("record vote with action: %v", err)
	}

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
	if !response.Recovery.NeedsReplay || !response.Recovery.NeedsRecovery || response.Recovery.PendingActionCount != 2 || response.Recovery.PendingReplayCount != 2 || response.Recovery.PendingImportCount != 0 {
		t.Fatalf("expected pending recovery actions in status response, got %+v", response.Recovery)
	}
	if len(response.Recovery.PendingActions) != 2 {
		t.Fatalf("expected two pending recovery actions, got %+v", response.Recovery.PendingActions)
	}
}

func TestHandleStatusRecordsConsensusDiagnosticForUnexpectedProposer(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedConsensusProposal(t, voter, 1, 0, "", time.Date(2026, time.March, 24, 15, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "unexpected-proposer")})
	body, err := json.Marshal(proposal)
	if err != nil {
		t.Fatalf("marshal proposal: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/consensus/proposals", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", recorder.Code)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusRecorder.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if len(response.Diagnostics.Recent) == 0 {
		t.Fatal("expected recent consensus diagnostics in status response")
	}
	diagnostic := response.Diagnostics.Recent[0]
	if diagnostic.Kind != "proposal_rejected" || diagnostic.Code != "unexpected_proposer" || diagnostic.Source != "local_api" {
		t.Fatalf("unexpected diagnostic %+v", diagnostic)
	}
	if diagnostic.Height != 1 || diagnostic.Round != 0 {
		t.Fatalf("expected diagnostic for height 1 round 0, got %+v", diagnostic)
	}
}

func TestHandleStatusRecordsConsensusDiagnosticForMissingCertificate(t *testing.T) {
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
	envelope := signedEnvelope(t, 25, 1, "missing-certificate")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := server.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	request := httptest.NewRequest(http.MethodPost, "/v1/dev/produce-block", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status 409, got %d", recorder.Code)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusRecorder.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if len(response.Diagnostics.Recent) == 0 {
		t.Fatal("expected recent diagnostics after failed commit")
	}
	diagnostic := response.Diagnostics.Recent[0]
	if diagnostic.Kind != "block_commit_rejected" || diagnostic.Code != "proposal_required" || diagnostic.Source != "local_api" {
		t.Fatalf("unexpected diagnostic %+v", diagnostic)
	}
}

func TestHandleBlockTemplateExposesBlockReadinessAcrossCertificateLifecycle(t *testing.T) {
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
	envelope := signedEnvelope(t, 25, 1, "readiness-lifecycle")
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
	if !templateResponse.BlockReadiness.LocalTemplateAvailable || templateResponse.BlockReadiness.StoredProposalCount != 0 {
		t.Fatalf("unexpected initial block readiness %+v", templateResponse.BlockReadiness)
	}
	foundProposalMissing := false
	for _, warning := range templateResponse.BlockReadiness.Warnings {
		if warning == "proposal_missing" {
			foundProposalMissing = true
		}
	}
	if !foundProposalMissing {
		t.Fatalf("expected proposal_missing warning, got %+v", templateResponse.BlockReadiness.Warnings)
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

	consensusRequest := httptest.NewRequest(http.MethodGet, "/v1/consensus", nil)
	consensusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(consensusRecorder, consensusRequest)
	if consensusRecorder.Code != http.StatusOK {
		t.Fatalf("expected consensus status 200, got %d", consensusRecorder.Code)
	}

	var consensusResponse ConsensusResponse
	if err := json.NewDecoder(consensusRecorder.Body).Decode(&consensusResponse); err != nil {
		t.Fatalf("decode consensus response: %v", err)
	}
	if consensusResponse.BlockReadiness.MatchingLocalProposalRound == nil || *consensusResponse.BlockReadiness.MatchingLocalProposalRound != 0 {
		t.Fatalf("expected matching local proposal round 0, got %+v", consensusResponse.BlockReadiness)
	}
	if consensusResponse.BlockReadiness.MatchingLocalCertificate {
		t.Fatalf("expected certificate to still be missing, got %+v", consensusResponse.BlockReadiness)
	}
	foundCertificateMissing := false
	for _, warning := range consensusResponse.BlockReadiness.Warnings {
		if warning == "certificate_missing" {
			foundCertificateMissing = true
		}
	}
	if !foundCertificateMissing {
		t.Fatalf("expected certificate_missing warning, got %+v", consensusResponse.BlockReadiness.Warnings)
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

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusRecorder.Code)
	}

	var statusResponse StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&statusResponse); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if !statusResponse.BlockReadiness.ReadyToCommitLocalTemplate || !statusResponse.BlockReadiness.ReadyToCommitStoredProposal || !statusResponse.BlockReadiness.ReadyToImportCertifiedBlock {
		t.Fatalf("expected ready certified block readiness, got %+v", statusResponse.BlockReadiness)
	}
	if !statusResponse.BlockReadiness.MatchingLocalCertificate {
		t.Fatalf("expected matching local certificate, got %+v", statusResponse.BlockReadiness)
	}
	if statusResponse.BlockReadiness.LatestCertifiedRound == nil || *statusResponse.BlockReadiness.LatestCertifiedRound != 0 {
		t.Fatalf("expected latest certified round 0, got %+v", statusResponse.BlockReadiness)
	}
}

func TestHandleStatusRecordsConsensusDiagnosticForTemplateMismatch(t *testing.T) {
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
	envelope := signedEnvelope(t, 25, 1, "template-mismatch")
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

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusRecorder.Code)
	}

	var statusResponse StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&statusResponse); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if len(statusResponse.Diagnostics.Recent) == 0 {
		t.Fatal("expected recent diagnostics after template mismatch")
	}
	diagnostic := statusResponse.Diagnostics.Recent[0]
	if diagnostic.Kind != "block_commit_rejected" || diagnostic.Code != "template_mismatch" || diagnostic.Source != "local_api" {
		t.Fatalf("unexpected diagnostic %+v", diagnostic)
	}
}

func TestConsensusAutomationRebroadcastsProposalAfterPeerLinkRestored(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)

	proposerServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})
	proposerHTTP := httptest.NewServer(proposerServer.Handler())
	defer proposerHTTP.Close()

	voterServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		ValidatorPrivateKey:          encodedPrivateKey(t, voter.privateKey),
		PeerURLs:                     []string{proposerHTTP.URL},
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	voterHTTP := httptest.NewServer(voterServer.Handler())
	defer voterHTTP.Close()

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := proposerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set proposer validators: %v", err)
	}
	if _, err := voterServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set voter validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "proposal-rebroadcast")
	if _, err := proposerServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit proposer sender: %v", err)
	}
	if _, err := voterServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit voter sender: %v", err)
	}
	if _, err := proposerServer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		return proposerServer.ledger.HasVote(1, 0, proposer.address)
	})

	proposerServer.config.PeerURLs = []string{voterHTTP.URL}

	waitFor(t, func() bool {
		return proposerServer.ledger.Status().Height == 1 && voterServer.ledger.Status().Height == 1
	})

	voterArtifacts := voterServer.ledger.ConsensusArtifacts()
	if voterArtifacts.LatestProposal == nil || voterArtifacts.LatestProposal.BlockHash == "" {
		t.Fatalf("expected voter to receive rebroadcast proposal, got %+v", voterArtifacts)
	}
	if voterArtifacts.LatestCertificate == nil {
		t.Fatalf("expected voter to receive certificate after rebroadcast path, got %+v", voterArtifacts)
	}
}

func TestConsensusAutomationRebroadcastsVoteAfterPeerLinkRestored(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)

	proposerServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),
		PeerURLs:                     []string{"placeholder"},
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})
	proposerHTTP := httptest.NewServer(proposerServer.Handler())
	defer proposerHTTP.Close()
	proposerServer.config.PeerURLs = []string{}

	voterServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		ValidatorPrivateKey:          encodedPrivateKey(t, voter.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	voterHTTP := httptest.NewServer(voterServer.Handler())
	defer voterHTTP.Close()
	proposerServer.config.PeerURLs = []string{voterHTTP.URL}

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := proposerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set proposer validators: %v", err)
	}
	if _, err := voterServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set voter validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "vote-rebroadcast")
	if _, err := proposerServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit proposer sender: %v", err)
	}
	if _, err := voterServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit voter sender: %v", err)
	}
	if _, err := proposerServer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		_, exists := voterServer.ledger.ProposalAt(1, 0)
		return exists
	})

	proposerArtifacts := proposerServer.ledger.ConsensusArtifacts()
	if proposerArtifacts.LatestCertificate != nil || proposerArtifacts.VoteCount != 1 {
		t.Fatalf("expected proposer to be waiting on the remote vote before link recovery, got %+v", proposerArtifacts)
	}

	voterServer.config.PeerURLs = []string{proposerHTTP.URL}

	waitFor(t, func() bool {
		return proposerServer.ledger.Status().Height == 1 && voterServer.ledger.Status().Height == 1
	})

	voterVote, exists := voterServer.ledger.LatestVoteByValidatorForHeight(1, voter.address)
	if !exists {
		t.Fatal("expected voter to persist a local vote for the recovered height")
	}

	proposerArtifacts = proposerServer.ledger.ConsensusArtifacts()
	if proposerArtifacts.LatestCertificate == nil {
		t.Fatalf("expected proposer certificate after vote rebroadcast, got %+v", proposerArtifacts)
	}
	if proposerArtifacts.VoteCount != 2 {
		t.Fatalf("expected proposer to observe both votes after rebroadcast, got %+v", proposerArtifacts)
	}
	if voterVote.BlockHash != proposerArtifacts.LatestCertificate.BlockHash {
		t.Fatalf("expected recovered voter vote for block %s, got %+v", proposerArtifacts.LatestCertificate.BlockHash, voterVote)
	}
}
func TestConsensusAutomationReplaysPendingActionsAfterRestart(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	proposerDataDir := t.TempDir()

	proposerServer := newTestServer(t, Config{
		DataDir:                      proposerDataDir,
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	voterServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		ValidatorPrivateKey:          encodedPrivateKey(t, voter.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	voterHTTP := httptest.NewServer(voterServer.Handler())
	defer voterHTTP.Close()

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := proposerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set proposer validators: %v", err)
	}
	if _, err := voterServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set voter validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "restart-replay")
	if _, err := proposerServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit proposer sender: %v", err)
	}
	if _, err := voterServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit voter sender: %v", err)
	}
	if _, err := proposerServer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		recovery := proposerServer.ledger.ConsensusRecovery()
		return proposerServer.ledger.HasVote(1, 0, proposer.address) && recovery.PendingActionCount == 2
	})

	proposerServer.Close()
	reopened, err := NewServerWithConfig(Config{
		DataDir:                      proposerDataDir,
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),
		PeerURLs:                     []string{voterHTTP.URL},
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        2 * time.Second,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})
	if err != nil {
		t.Fatalf("reopen proposer: %v", err)
	}
	defer reopened.Close()
	reopenedHTTP := httptest.NewServer(reopened.Handler())
	defer reopenedHTTP.Close()
	voterServer.config.PeerURLs = []string{reopenedHTTP.URL}

	waitFor(t, func() bool {
		return reopened.ledger.Status().Height == 1 && voterServer.ledger.Status().Height == 1
	})

	recovery := reopened.ledger.ConsensusRecovery()
	if recovery.NeedsReplay || recovery.PendingActionCount != 0 {
		t.Fatalf("expected replayed actions to complete after restart recovery, got %+v", recovery)
	}

	replayed := 0
	for _, action := range recovery.RecentActions {
		if (action.Type == ledger.ConsensusActionProposal || action.Type == ledger.ConsensusActionVote) && action.ReplayAttempts > 0 {
			replayed++
		}
	}
	if replayed < 2 {
		t.Fatalf("expected restarted proposer to replay persisted proposal and vote, got %+v", recovery.RecentActions)
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

func TestConsensusAutomationSelfProposesVotesAndCommitsCertifiedBlock(t *testing.T) {
	validator := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, validator.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: validator.address, VotingPower: 100, SelfStake: 100},
	}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "auto-single")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := server.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		return server.ledger.Status().Height == 1
	})

	artifacts := server.ledger.ConsensusArtifacts()
	if artifacts.LatestProposal == nil {
		t.Fatal("expected automated proposal to be recorded")
	}
	if artifacts.LatestCertificate == nil {
		t.Fatal("expected automated certificate to be recorded")
	}
	if artifacts.VoteCount != 1 {
		t.Fatalf("expected one automated vote, got %+v", artifacts)
	}
	if artifacts.LatestProposal.BlockHash != artifacts.LatestCertificate.BlockHash {
		t.Fatalf("expected certificate for proposal block %s, got %+v", artifacts.LatestProposal.BlockHash, artifacts.LatestCertificate)
	}
	if sender := server.ledger.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("unexpected sender state after automated commit: %+v", sender)
	}
}

func TestConsensusAutomationReplicatesProposalVotesAndCommitAcrossValidators(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)

	proposerServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, proposer.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})
	proposerHTTP := httptest.NewServer(proposerServer.Handler())
	defer proposerHTTP.Close()

	voterServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		ValidatorPrivateKey:          encodedPrivateKey(t, voter.privateKey),
		PeerURLs:                     []string{proposerHTTP.URL},
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	voterHTTP := httptest.NewServer(voterServer.Handler())
	defer voterHTTP.Close()

	proposerServer.config.PeerURLs = []string{voterHTTP.URL}

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := proposerServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set proposer validators: %v", err)
	}
	if _, err := voterServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set voter validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "auto-multi")
	if _, err := proposerServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit proposer sender: %v", err)
	}
	if _, err := voterServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit voter sender: %v", err)
	}
	if _, err := proposerServer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		return proposerServer.ledger.Status().Height == 1 && voterServer.ledger.Status().Height == 1
	})

	proposerArtifacts := proposerServer.ledger.ConsensusArtifacts()
	if proposerArtifacts.LatestCertificate == nil {
		t.Fatal("expected proposer certificate after automated quorum")
	}
	if proposerArtifacts.VoteCount != 2 {
		t.Fatalf("expected proposer to observe two votes, got %+v", proposerArtifacts)
	}
	voterArtifacts := voterServer.ledger.ConsensusArtifacts()
	if voterArtifacts.LatestCertificate == nil {
		t.Fatal("expected voter certificate after replicated votes")
	}
	if voterArtifacts.LatestProposal == nil {
		t.Fatal("expected voter to receive automated proposal")
	}
	if proposerArtifacts.LatestCertificate.BlockHash != voterArtifacts.LatestCertificate.BlockHash {
		t.Fatalf("expected matching certificates across validators, got proposer=%+v voter=%+v", proposerArtifacts.LatestCertificate, voterArtifacts.LatestCertificate)
	}
	if sender := voterServer.ledger.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("unexpected voter sender state after automated replication: %+v", sender)
	}
}

func TestConsensusAutomationReproposesStoredCandidateAfterRoundTimeout(t *testing.T) {
	roundZeroProposer := newConsensusSigner(t)
	roundOneProposer := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "node-b",
		ValidatorPrivateKey:       encodedPrivateKey(t, roundOneProposer.privateKey),
		BlockInterval:             0,
		ConsensusInterval:         20 * time.Millisecond,
		ConsensusRoundTimeout:     100 * time.Millisecond,
		SyncInterval:              0,
		MaxTransactionsPerBlock:   10,
		EnableBlockProduction:     true,
		EnableConsensusAutomation: true,
		EnablePeerSync:            false,
	})

	if _, err := server.ledger.SetValidators([]dpos.Validator{
		{Rank: 1, Address: roundZeroProposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: roundOneProposer.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	producedAt := time.Date(2026, time.March, 24, 13, 0, 0, 0, time.UTC)
	roundZeroProposal := signedConsensusProposal(t, roundZeroProposer, 1, 0, "", producedAt, []tx.Envelope{signedEnvelope(t, 25, 1, "round-zero-candidate")})
	if err := server.ledger.RecordProposal(roundZeroProposal); err != nil {
		t.Fatalf("record round-zero proposal: %v", err)
	}

	waitFor(t, func() bool {
		proposal, exists := server.ledger.ProposalAt(1, 1)
		return exists && proposal.Proposer == roundOneProposer.address
	})

	reproposal, exists := server.ledger.ProposalAt(1, 1)
	if !exists {
		t.Fatal("expected round-one reproposal to be recorded")
	}
	if reproposal.BlockHash != roundZeroProposal.BlockHash {
		t.Fatalf("expected reproposal block hash %s, got %s", roundZeroProposal.BlockHash, reproposal.BlockHash)
	}
	if reproposal.PreviousHash != roundZeroProposal.PreviousHash || !reproposal.ProducedAt.Equal(roundZeroProposal.ProducedAt) {
		t.Fatalf("expected reproposal to reuse stored candidate, got %+v", reproposal)
	}
}

func TestConsensusAutomationAdvancesRoundAndRotatesProposerAfterTimeout(t *testing.T) {
	roundZeroProposer := newConsensusSigner(t)
	roundOneProposer := newConsensusSigner(t)

	roundZeroServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-a",
		ValidatorPrivateKey:          encodedPrivateKey(t, roundZeroProposer.privateKey),
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        150 * time.Millisecond,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	roundZeroHTTP := httptest.NewServer(roundZeroServer.Handler())
	defer roundZeroHTTP.Close()

	roundOneServer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "node-b",
		ValidatorPrivateKey:          encodedPrivateKey(t, roundOneProposer.privateKey),
		PeerURLs:                     []string{roundZeroHTTP.URL},
		BlockInterval:                0,
		ConsensusInterval:            20 * time.Millisecond,
		ConsensusRoundTimeout:        150 * time.Millisecond,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnableConsensusAutomation:    true,
		EnablePeerSync:               false,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})
	roundOneHTTP := httptest.NewServer(roundOneServer.Handler())
	defer roundOneHTTP.Close()

	roundZeroServer.config.PeerURLs = []string{roundOneHTTP.URL}

	validators := []dpos.Validator{
		{Rank: 1, Address: roundZeroProposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: roundOneProposer.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := roundZeroServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set round-zero validators: %v", err)
	}
	if _, err := roundOneServer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set round-one validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "timeout-rotation")
	if _, err := roundZeroServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit round-zero sender: %v", err)
	}
	if _, err := roundOneServer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit round-one sender: %v", err)
	}
	if _, err := roundOneServer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	waitFor(t, func() bool {
		return roundZeroServer.ledger.Status().Height == 1 && roundOneServer.ledger.Status().Height == 1
	})

	roundOneArtifacts := roundOneServer.ledger.ConsensusArtifacts()
	if roundOneArtifacts.LatestProposal == nil || roundOneArtifacts.LatestProposal.Round != 1 {
		t.Fatalf("expected round-one proposal after timeout rotation, got %+v", roundOneArtifacts.LatestProposal)
	}
	if roundOneArtifacts.LatestCertificate == nil || roundOneArtifacts.LatestCertificate.Round != 1 {
		t.Fatalf("expected round-one certificate after timeout rotation, got %+v", roundOneArtifacts.LatestCertificate)
	}
	roundZeroArtifacts := roundZeroServer.ledger.ConsensusArtifacts()
	if roundZeroArtifacts.LatestCertificate == nil || roundZeroArtifacts.LatestCertificate.Round != 1 {
		t.Fatalf("expected replica round-one certificate after timeout rotation, got %+v", roundZeroArtifacts.LatestCertificate)
	}
	if sender := roundZeroServer.ledger.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("unexpected round-zero sender state after timeout rotation: %+v", sender)
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

	waitFor(t, func() bool {
		artifacts := peerServer.ledger.ConsensusArtifacts()
		return artifacts.LatestProposal != nil && artifacts.LatestProposal.BlockHash == templateResponse.Block.Hash
	})

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

	waitFor(t, func() bool {
		peers := replica.peerSnapshot()
		return len(peers) == 1 && peers[0].LastSnapshotRestoreAt != nil
	})
	peers := replica.peerSnapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer view after divergence repair, got %d", len(peers))
	}
	if peers[0].SyncState != "snapshot_restored" {
		t.Fatalf("expected snapshot_restored sync state after divergence repair, got %+v", peers[0])
	}
	if peers[0].LastSnapshotRestoreReason != "peer_diverged" {
		t.Fatalf("expected peer_diverged snapshot reason, got %+v", peers[0])
	}
	if peers[0].LastSnapshotRestoreHeight != producerBlock.Height || peers[0].LastSnapshotRestoreBlockHash != producerBlock.Hash {
		t.Fatalf("unexpected peer snapshot restore metadata %+v", peers[0])
	}
}

func TestPeerSyncHistoryPersistsAcrossServerRestart(t *testing.T) {
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

	producerEnvelope := signedEnvelope(t, 25, 1, "history-canonical")
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

	replicaEnvelope := signedEnvelope(t, 10, 1, "history-divergent")
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
		peers := replica.peerSnapshot()
		return len(peers) == 1 && len(peers[0].RecentIncidents) > 0
	})

	replica.Close()
	reopened, err := NewServerWithConfig(Config{
		DataDir:                 replicaDataDir,
		NodeID:                  "replica",
		PeerURLs:                []string{producerHTTP.URL},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})
	if err != nil {
		t.Fatalf("reopen replica: %v", err)
	}
	defer reopened.Close()

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	reopened.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200 after restart, got %d", statusRecorder.Code)
	}

	var statusResponse StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&statusResponse); err != nil {
		t.Fatalf("decode restarted status response: %v", err)
	}
	if len(statusResponse.PeerSyncHistory.Recent) == 0 {
		t.Fatal("expected persisted peer sync history after restart")
	}
	incident := statusResponse.PeerSyncHistory.Recent[0]
	if incident.PeerURL != producerHTTP.URL || incident.State != "snapshot_restored" || incident.Reason != "peer_diverged" {
		t.Fatalf("unexpected restarted peer sync history %+v", incident)
	}
	if incident.BlockHash != producerBlock.Hash {
		t.Fatalf("expected restarted incident block hash %s, got %+v", producerBlock.Hash, incident)
	}
	if statusResponse.PeerSyncSummary.IncidentCount != 1 || statusResponse.PeerSyncSummary.AffectedPeerCount != 1 || statusResponse.PeerSyncSummary.TotalOccurrences != 1 {
		t.Fatalf("unexpected restarted peer sync summary %+v", statusResponse.PeerSyncSummary)
	}
	if len(statusResponse.PeerSyncSummary.Peers) != 1 || statusResponse.PeerSyncSummary.Peers[0].PeerURL != producerHTTP.URL || statusResponse.PeerSyncSummary.Peers[0].LatestState != "snapshot_restored" {
		t.Fatalf("unexpected restarted peer sync summary peers %+v", statusResponse.PeerSyncSummary.Peers)
	}

	peersRequest := httptest.NewRequest(http.MethodGet, "/v1/peers", nil)
	peersRecorder := httptest.NewRecorder()
	reopened.Handler().ServeHTTP(peersRecorder, peersRequest)
	if peersRecorder.Code != http.StatusOK {
		t.Fatalf("expected peers 200 after restart, got %d", peersRecorder.Code)
	}

	var peersResponse PeersResponse
	if err := json.NewDecoder(peersRecorder.Body).Decode(&peersResponse); err != nil {
		t.Fatalf("decode restarted peers response: %v", err)
	}
	if len(peersResponse.Peers) != 1 {
		t.Fatalf("expected 1 configured peer after restart, got %+v", peersResponse.Peers)
	}
	if len(peersResponse.Peers[0].RecentIncidents) == 0 {
		t.Fatal("expected persisted per-peer incident history after restart")
	}
	if peersResponse.Peers[0].IncidentCount != 1 || peersResponse.Peers[0].IncidentOccurrences != 1 || peersResponse.Peers[0].LatestIncidentAt == nil {
		t.Fatalf("unexpected per-peer incident counters after restart %+v", peersResponse.Peers[0])
	}
	if peersResponse.Peers[0].RecentIncidents[0].State != "snapshot_restored" || peersResponse.Peers[0].RecentIncidents[0].Reason != "peer_diverged" {
		t.Fatalf("unexpected per-peer incident history after restart %+v", peersResponse.Peers[0].RecentIncidents)
	}
}

func TestStatusExposesPeerSyncSummaryAcrossPeers(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		PeerURLs:                []string{"http://peer-a.example", "http://peer-b.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	firstObservedAt := time.Date(2026, time.March, 24, 20, 0, 0, 0, time.UTC)
	secondObservedAt := firstObservedAt.Add(30 * time.Second)
	thirdObservedAt := firstObservedAt.Add(90 * time.Second)
	if err := server.ledger.RecordPeerSyncIncident(ledger.PeerSyncIncident{
		PeerURL:         "http://peer-a.example",
		State:           "unreachable",
		LocalHeight:     5,
		PeerHeight:      3,
		HeightDelta:     -2,
		ErrorMessage:    "dial tcp timeout",
		FirstObservedAt: firstObservedAt,
		LastObservedAt:  firstObservedAt,
	}); err != nil {
		t.Fatalf("record peer-a incident: %v", err)
	}
	if err := server.ledger.RecordPeerSyncIncident(ledger.PeerSyncIncident{
		PeerURL:         "http://peer-a.example",
		State:           "unreachable",
		LocalHeight:     5,
		PeerHeight:      3,
		HeightDelta:     -2,
		ErrorMessage:    "dial tcp timeout",
		FirstObservedAt: secondObservedAt,
		LastObservedAt:  secondObservedAt,
	}); err != nil {
		t.Fatalf("record repeated peer-a incident: %v", err)
	}
	if err := server.ledger.RecordPeerSyncIncident(ledger.PeerSyncIncident{
		PeerURL:         "http://peer-b.example",
		State:           "import_blocked",
		LocalHeight:     5,
		PeerHeight:      6,
		HeightDelta:     1,
		BlockHash:       consensusTestHash("peer-b-import-blocked"),
		ErrorCode:       "proposal_required",
		ErrorMessage:    "consensus proposal is required before block import",
		FirstObservedAt: thirdObservedAt,
		LastObservedAt:  thirdObservedAt,
	}); err != nil {
		t.Fatalf("record peer-b incident: %v", err)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", statusRecorder.Code)
	}

	var statusResponse StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&statusResponse); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if statusResponse.PeerSyncSummary.IncidentCount != 2 || statusResponse.PeerSyncSummary.AffectedPeerCount != 2 || statusResponse.PeerSyncSummary.TotalOccurrences != 3 {
		t.Fatalf("unexpected peer sync summary %+v", statusResponse.PeerSyncSummary)
	}
	if statusResponse.PeerSyncSummary.LatestObservedAt == nil || !statusResponse.PeerSyncSummary.LatestObservedAt.Equal(thirdObservedAt) {
		t.Fatalf("unexpected latest peer sync summary time %+v", statusResponse.PeerSyncSummary)
	}
	if len(statusResponse.PeerSyncSummary.States) != 2 || statusResponse.PeerSyncSummary.States[0].State != "unreachable" || statusResponse.PeerSyncSummary.States[0].TotalOccurrences != 2 {
		t.Fatalf("unexpected peer sync state summaries %+v", statusResponse.PeerSyncSummary.States)
	}
	if len(statusResponse.PeerSyncSummary.Reasons) != 1 || statusResponse.PeerSyncSummary.Reasons[0].Reason != "unknown" || statusResponse.PeerSyncSummary.Reasons[0].IncidentCount != 2 || statusResponse.PeerSyncSummary.Reasons[0].AffectedPeerCount != 2 || statusResponse.PeerSyncSummary.Reasons[0].TotalOccurrences != 3 {
		t.Fatalf("unexpected peer sync reason summaries %+v", statusResponse.PeerSyncSummary.Reasons)
	}
	if len(statusResponse.PeerSyncSummary.ErrorCodes) != 2 || statusResponse.PeerSyncSummary.ErrorCodes[0].ErrorCode != "unknown" || statusResponse.PeerSyncSummary.ErrorCodes[0].TotalOccurrences != 2 || statusResponse.PeerSyncSummary.ErrorCodes[1].ErrorCode != "proposal_required" || statusResponse.PeerSyncSummary.ErrorCodes[1].TotalOccurrences != 1 {
		t.Fatalf("unexpected peer sync error code summaries %+v", statusResponse.PeerSyncSummary.ErrorCodes)
	}
	if len(statusResponse.PeerSyncSummary.Peers) != 2 || statusResponse.PeerSyncSummary.Peers[0].PeerURL != "http://peer-b.example" || statusResponse.PeerSyncSummary.Peers[0].LatestState != "import_blocked" || statusResponse.PeerSyncSummary.Peers[0].LatestErrorCode != "proposal_required" {
		t.Fatalf("unexpected peer sync peer summaries %+v", statusResponse.PeerSyncSummary.Peers)
	}

	peersRequest := httptest.NewRequest(http.MethodGet, "/v1/peers", nil)
	peersRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(peersRecorder, peersRequest)
	if peersRecorder.Code != http.StatusOK {
		t.Fatalf("expected peers 200, got %d", peersRecorder.Code)
	}

	var peersResponse PeersResponse
	if err := json.NewDecoder(peersRecorder.Body).Decode(&peersResponse); err != nil {
		t.Fatalf("decode peers response: %v", err)
	}
	if len(peersResponse.Peers) != 2 {
		t.Fatalf("expected 2 peer views, got %+v", peersResponse.Peers)
	}
	if peersResponse.Peers[0].IncidentCount != 1 || peersResponse.Peers[0].IncidentOccurrences != 2 || peersResponse.Peers[0].LatestIncidentAt == nil {
		t.Fatalf("unexpected peer-a incident counters %+v", peersResponse.Peers[0])
	}
	if len(peersResponse.Peers[0].RecentIncidents) != 1 || peersResponse.Peers[0].RecentIncidents[0].Occurrences != 2 {
		t.Fatalf("unexpected peer-a incident history %+v", peersResponse.Peers[0].RecentIncidents)
	}
	if peersResponse.Peers[1].IncidentCount != 1 || peersResponse.Peers[1].IncidentOccurrences != 1 || peersResponse.Peers[1].RecentIncidents[0].State != "import_blocked" {
		t.Fatalf("unexpected peer-b incident data %+v", peersResponse.Peers[1])
	}
}

func TestHandleNodeHealthReportsReadyWhenSignalsAreClear(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "health-ok",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected health 200, got %d", recorder.Code)
	}

	var response HealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	if !response.Live || !response.Ready || response.Status != healthStatusOK {
		t.Fatalf("unexpected healthy response %+v", response)
	}
	if response.StructuredLogsEnabled {
		t.Fatalf("expected structured logs disabled by default %+v", response)
	}
	if check, ok := healthCheckByName(response.Checks, "recovery"); !ok || check.Status != healthCheckPass {
		t.Fatalf("expected passing recovery check, got %+v", response.Checks)
	}
	if check, ok := healthCheckByName(response.Checks, "peer_sync"); !ok || check.Status != healthCheckPass {
		t.Fatalf("expected passing peer_sync check, got %+v", response.Checks)
	}
}

func TestHandleNodeHealthWarnsOnRecentDiagnostics(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "health-warn",
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	server.recordConsensusDiagnostic("proposal_rejected", "api", ledger.ErrConsensusTemplateMismatch, 2, 0, consensusTestHash("health-warn"), "zph_validator_warn")

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected health warn to return 200, got %d", recorder.Code)
	}

	var response HealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode warning health response: %v", err)
	}
	if !response.Ready || response.Status != healthStatusWarn {
		t.Fatalf("expected ready warn health response, got %+v", response)
	}
	check, ok := healthCheckByName(response.Checks, "diagnostics")
	if !ok || check.Status != healthCheckWarn {
		t.Fatalf("expected warning diagnostics check, got %+v", response.Checks)
	}
}

func TestHandleNodeHealthFailsWhenRecoveryAndPeersAreBlocked(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "health-fail",
		PeerURLs:                     []string{"http://peer-a.example"},
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               true,
		RequireConsensusCertificates: true,
	})

	now := time.Now().UTC()
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionBlockImport,
		Height:     3,
		Round:      0,
		BlockHash:  consensusTestHash("health-import"),
		Validator:  "zph_validator_fail",
		RecordedAt: now,
		Status:     ledger.ConsensusActionPending,
		Note:       "waiting for peer block import after proposal_required",
	}); err != nil {
		t.Fatalf("record pending import action: %v", err)
	}
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     2,
		Reachable:  false,
		Admitted:   false,
		SyncState:  "unreachable",
		LastSeenAt: &now,
		Error:      "dial tcp timeout",
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/health", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected health failure 503, got %d", recorder.Code)
	}

	var response HealthResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode failing health response: %v", err)
	}
	if response.Ready || response.Status != healthStatusFail {
		t.Fatalf("expected failing health response, got %+v", response)
	}
	if check, ok := healthCheckByName(response.Checks, "recovery"); !ok || check.Status != healthCheckFail {
		t.Fatalf("expected failing recovery check, got %+v", response.Checks)
	}
	if check, ok := healthCheckByName(response.Checks, "peer_sync"); !ok || check.Status != healthCheckFail {
		t.Fatalf("expected failing peer_sync check, got %+v", response.Checks)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(metricsRecorder, metricsRequest)
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics status 200, got %d", metricsRecorder.Code)
	}
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_node_ready 0")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_health_check_status{check=\"recovery\",status=\"fail\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_health_check_status{check=\"peer_sync\",status=\"fail\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_alert_count 3")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_alert_active{code=\"consensus_recovery_backlog\",severity=\"critical\",component=\"recovery\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_alert_active{code=\"peer_sync_unavailable\",severity=\"critical\",component=\"peer_sync\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_alert_active{code=\"validator_set_missing\",severity=\"warning\",component=\"consensus\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_slo_objective_count 3")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_slo_status_count{status=\"breached\"} 3")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_slo_objective_status{objective=\"node_readiness\",status=\"breached\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"breached\"} 1")
	requirePrometheusLine(t, metricsRecorder.Body.String(), "zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"breached\"} 1")
}

func TestHandleAlertsExposeDerivedOperatorAlerts(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "alerts-node",
		PeerURLs:                     []string{"http://peer-a.example"},
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               true,
		RequireConsensusCertificates: true,
	})

	now := time.Now().UTC()
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionBlockImport,
		Height:     4,
		Round:      0,
		BlockHash:  consensusTestHash("alerts-import"),
		Validator:  "zph_validator_alerts",
		RecordedAt: now,
		Status:     ledger.ConsensusActionPending,
		Note:       "waiting for proposal before import",
	}); err != nil {
		t.Fatalf("record alert recovery action: %v", err)
	}
	if err := server.ledger.RecordConsensusDiagnostic(ledger.ConsensusDiagnostic{
		Kind:       "proposal_rejected",
		Code:       "template_mismatch",
		Message:    "proposal does not match local block template",
		Height:     4,
		Round:      0,
		BlockHash:  consensusTestHash("alerts-import"),
		Validator:  "zph_validator_alerts",
		Source:     "api",
		ObservedAt: now.Add(1 * time.Minute),
	}); err != nil {
		t.Fatalf("record alert diagnostic: %v", err)
	}
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     3,
		Reachable:  false,
		Admitted:   false,
		SyncState:  "unreachable",
		LastSeenAt: &now,
		Error:      "dial tcp timeout",
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/alerts", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected alerts status 200, got %d", recorder.Code)
	}

	var response AlertsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode alerts response: %v", err)
	}
	if response.Ready || response.Status != healthStatusFail {
		t.Fatalf("expected failing alert response, got %+v", response)
	}
	if response.AlertCount != 4 || response.CriticalCount != 2 || response.WarningCount != 2 {
		t.Fatalf("unexpected alert counts %+v", response)
	}
	if alert, ok := alertByCode(response.Alerts, "validator_set_missing"); !ok || alert.Severity != alertSeverityWarning {
		t.Fatalf("expected validator_set_missing warning, got %+v", response.Alerts)
	}
	if alert, ok := alertByCode(response.Alerts, "consensus_recovery_backlog"); !ok || alert.Severity != alertSeverityCritical || alert.ObservedAt == nil {
		t.Fatalf("expected recovery critical alert with observedAt, got %+v", response.Alerts)
	}
	if alert, ok := alertByCode(response.Alerts, "peer_sync_unavailable"); !ok || alert.Severity != alertSeverityCritical {
		t.Fatalf("expected peer sync critical alert, got %+v", response.Alerts)
	}
	if alert, ok := alertByCode(response.Alerts, "recent_consensus_diagnostics"); !ok || alert.Severity != alertSeverityWarning || alert.ObservedAt == nil {
		t.Fatalf("expected diagnostics warning alert with observedAt, got %+v", response.Alerts)
	}
}

func TestHandleAlertsExposePeerImportAndAdmissionWarnings(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "alerts-peer-detail",
		PeerURLs:                []string{"http://peer-a.example", "http://peer-b.example", "http://peer-c.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	now := time.Date(2026, time.March, 24, 23, 30, 0, 0, time.UTC)
	blockedAt := now.Add(1 * time.Minute)
	unadmittedAt := now.Add(2 * time.Minute)
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     4,
		Admitted:   true,
		Reachable:  true,
		SyncState:  "aligned",
		LastSeenAt: &now,
	})
	server.recordPeerView(PeerView{
		URL:                        "http://peer-b.example",
		Height:                     5,
		Admitted:                   true,
		Reachable:                  true,
		SyncState:                  "import_blocked",
		LastImportErrorCode:        "proposal_required",
		LastImportErrorMessage:     "consensus proposal is required before block import",
		LastImportFailureAt:        &blockedAt,
		LastImportFailureHeight:    5,
		LastImportFailureBlockHash: consensusTestHash("alerts-peer-import"),
	})
	server.recordPeerView(PeerView{
		URL:            "http://peer-c.example",
		Height:         5,
		Admitted:       false,
		Reachable:      true,
		SyncState:      "unadmitted",
		AdmissionError: "validator binding mismatch",
		LastSeenAt:     &unadmittedAt,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/alerts", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected alerts status 200, got %d", recorder.Code)
	}

	var response AlertsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode alerts response: %v", err)
	}
	if !response.Ready || response.Status != healthStatusWarn {
		t.Fatalf("expected warning alert response, got %+v", response)
	}
	if response.AlertCount != 3 || response.CriticalCount != 0 || response.WarningCount != 3 {
		t.Fatalf("unexpected peer detail alert counts %+v", response)
	}
	if alert, ok := alertByCode(response.Alerts, "peer_sync_degraded"); !ok || alert.Severity != alertSeverityWarning {
		t.Fatalf("expected peer_sync_degraded warning, got %+v", response.Alerts)
	}
	if alert, ok := alertByCode(response.Alerts, "peer_import_blocked"); !ok || alert.Severity != alertSeverityWarning || alert.ObservedAt == nil || !alert.ObservedAt.Equal(blockedAt) || !strings.Contains(alert.Detail, "errorCode=proposal_required") {
		t.Fatalf("expected peer_import_blocked warning with detail, got %+v", response.Alerts)
	}
	if alert, ok := alertByCode(response.Alerts, "peer_admission_blocked"); !ok || alert.Severity != alertSeverityWarning || alert.ObservedAt == nil || !alert.ObservedAt.Equal(unadmittedAt) || !strings.Contains(alert.Detail, "reason=validator binding mismatch") {
		t.Fatalf("expected peer_admission_blocked warning with detail, got %+v", response.Alerts)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(metricsRecorder, metricsRequest)
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics status 200, got %d", metricsRecorder.Code)
	}
	body := metricsRecorder.Body.String()
	requirePrometheusLine(t, body, "zephyr_alert_count 3")
	requirePrometheusLine(t, body, "zephyr_alert_count_by_severity{severity=\"warning\"} 3")
	requirePrometheusLine(t, body, "zephyr_alert_active{code=\"peer_import_blocked\",severity=\"warning\",component=\"peer_sync\"} 1")
	requirePrometheusLine(t, body, "zephyr_alert_active{code=\"peer_admission_blocked\",severity=\"warning\",component=\"peer_sync\"} 1")
}

func TestHandleSLOSummarizesDerivedObjectives(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "slo-node",
		PeerURLs:                     []string{"http://peer-a.example"},
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               true,
		RequireConsensusCertificates: true,
	})

	now := time.Now().UTC()
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionBlockImport,
		Height:     4,
		Round:      0,
		BlockHash:  consensusTestHash("slo-import"),
		Validator:  "zph_validator_slo",
		RecordedAt: now,
		Status:     ledger.ConsensusActionPending,
		Note:       "waiting for proposal before import",
	}); err != nil {
		t.Fatalf("record slo recovery action: %v", err)
	}
	if err := server.ledger.RecordConsensusDiagnostic(ledger.ConsensusDiagnostic{
		Kind:       "proposal_rejected",
		Code:       "template_mismatch",
		Message:    "proposal does not match local block template",
		Height:     4,
		Round:      0,
		BlockHash:  consensusTestHash("slo-import"),
		Validator:  "zph_validator_slo",
		Source:     "api",
		ObservedAt: now.Add(1 * time.Minute),
	}); err != nil {
		t.Fatalf("record slo diagnostic: %v", err)
	}
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     3,
		Reachable:  false,
		Admitted:   false,
		SyncState:  "unreachable",
		LastSeenAt: &now,
		Error:      "dial tcp timeout",
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/slo", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected slo status 200, got %d", recorder.Code)
	}

	var response SLOSummaryResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode slo response: %v", err)
	}
	if response.Ready || response.HealthStatus != healthStatusFail {
		t.Fatalf("expected failing slo response, got %+v", response)
	}
	if response.AlertCount != 4 || response.CriticalCount != 2 || response.WarningCount != 2 {
		t.Fatalf("unexpected slo alert counts %+v", response)
	}
	if response.ObjectiveCount != 3 || response.MeetingCount != 0 || response.AtRiskCount != 0 || response.BreachedCount != 3 || response.NotApplicableCount != 0 {
		t.Fatalf("unexpected slo objective counts %+v", response)
	}
	if objective, ok := sloObjectiveByName(response.Objectives, "node_readiness"); !ok || objective.Status != sloStatusBreached {
		t.Fatalf("expected breached node_readiness objective, got %+v", response.Objectives)
	}
	if objective, ok := sloObjectiveByName(response.Objectives, "consensus_continuity"); !ok || objective.Status != sloStatusBreached {
		t.Fatalf("expected breached consensus_continuity objective, got %+v", response.Objectives)
	}
	if objective, ok := sloObjectiveByName(response.Objectives, "peer_sync_continuity"); !ok || objective.Status != sloStatusBreached {
		t.Fatalf("expected breached peer_sync_continuity objective, got %+v", response.Objectives)
	}
}

func TestHandleAlertRulesExposeRecommendedBundles(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "alert-rules-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/alert-rules", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected alert-rules status 200, got %d", recorder.Code)
	}

	var response AlertRuleBundleResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode alert-rules response: %v", err)
	}
	if response.NodeID != "alert-rules-node" || response.PeerSyncEnabled {
		t.Fatalf("unexpected alert-rules identity/config %+v", response)
	}
	if response.RuleCount != 10 || response.EnabledRuleCount != 5 || response.DisabledRuleCount != 5 {
		t.Fatalf("unexpected alert-rule counts %+v", response)
	}
	if rule, ok := alertRuleByName(response.Groups, "ZephyrNodeNotReady"); !ok || !rule.Enabled || rule.Expression != "zephyr_node_ready == 0" {
		t.Fatalf("expected enabled ZephyrNodeNotReady rule, got %+v", response.Groups)
	}
	if rule, ok := alertRuleByName(response.Groups, "ZephyrPeerSyncContinuityBreached"); !ok || rule.Enabled || !strings.Contains(rule.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer-sync breach rule, got %+v", response.Groups)
	}
	if rule, ok := alertRuleByName(response.Groups, "ZephyrPeerImportBlocked"); !ok || rule.Enabled || !strings.Contains(rule.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer import rule, got %+v", response.Groups)
	}
}

func TestHandlePrometheusAlertRulesExportsEnabledRulesOnly(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "alert-rules-export-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/alert-rules/prometheus", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected prometheus alert-rules status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != prometheusAlertRuleContentType {
		t.Fatalf("expected alert-rule content type %q, got %q", prometheusAlertRuleContentType, recorder.Header().Get("Content-Type"))
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "groups:\n") {
		t.Fatalf("expected groups header in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "  - name: zephyr.peer_sync\n") {
		t.Fatalf("expected peer_sync rule group in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - alert: ZephyrPeerSyncContinuityBreached\n") {
		t.Fatalf("expected peer sync continuity alert in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "        expr: 'zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"breached\"} == 1'\n") {
		t.Fatalf("expected peer sync breach expression in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - alert: ZephyrConsensusRecoveryBacklog\n") {
		t.Fatalf("expected consensus recovery alert in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - alert: ZephyrPeerImportBlocked\n") {
		t.Fatalf("expected peer import blocked alert in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "        expr: 'zephyr_alert_active{code=\"peer_import_blocked\",severity=\"warning\",component=\"peer_sync\"} == 1'\n") {
		t.Fatalf("expected peer import blocked expression in prometheus alert rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - alert: ZephyrPeerAdmissionBlocked\n") {
		t.Fatalf("expected peer admission blocked alert in prometheus alert rules, got:\n%s", body)
	}
}
func TestHandleRecordingRulesExposeRecommendedBundles(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "recording-rules-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/recording-rules", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected recording-rules status 200, got %d", recorder.Code)
	}

	var response RecordingRuleBundleResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode recording-rules response: %v", err)
	}
	if response.NodeID != "recording-rules-node" || response.PeerSyncEnabled {
		t.Fatalf("unexpected recording-rules identity/config %+v", response)
	}
	if response.RuleCount != 11 || response.EnabledRuleCount != 8 || response.DisabledRuleCount != 3 {
		t.Fatalf("unexpected recording-rule counts %+v", response)
	}
	if rule, ok := recordingRuleByRecord(response.Groups, "zephyr:node_readiness:ready"); !ok || !rule.Enabled || rule.Expression != "zephyr_node_ready" {
		t.Fatalf("expected enabled zephyr:node_readiness:ready recording rule, got %+v", response.Groups)
	}
	if rule, ok := recordingRuleByRecord(response.Groups, "zephyr:peer_sync_continuity:breached"); !ok || rule.Enabled || !strings.Contains(rule.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer-sync breached recording rule, got %+v", response.Groups)
	}
}

func TestHandlePrometheusRecordingRulesExportsEnabledRulesOnly(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "recording-rules-export-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/recording-rules/prometheus", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected prometheus recording-rules status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != prometheusRecordingRuleContentType {
		t.Fatalf("expected recording-rule content type %q, got %q", prometheusRecordingRuleContentType, recorder.Header().Get("Content-Type"))
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "groups:\n") {
		t.Fatalf("expected groups header in prometheus recording rules, got:\n%s", body)
	}
	if !strings.Contains(body, "  - name: zephyr.peer_sync\n") {
		t.Fatalf("expected peer_sync rule group in prometheus recording rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - record: zephyr:peer_sync_continuity:breached\n") {
		t.Fatalf("expected peer sync breached recording rule in prometheus recording rules, got:\n%s", body)
	}
	if !strings.Contains(body, "        expr: 'zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"breached\"}'\n") {
		t.Fatalf("expected peer sync breached expression in prometheus recording rules, got:\n%s", body)
	}
	if !strings.Contains(body, "      - record: zephyr:consensus:recovery_backlog\n") {
		t.Fatalf("expected consensus recovery backlog recording rule in prometheus recording rules, got:\n%s", body)
	}
}

func TestHandleDashboardsExposeRecommendedBundles(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "dashboard-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/dashboards", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected dashboards status 200, got %d", recorder.Code)
	}

	var response DashboardBundleResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode dashboards response: %v", err)
	}
	if response.NodeID != "dashboard-node" || response.PeerSyncEnabled {
		t.Fatalf("unexpected dashboards identity/config %+v", response)
	}
	if response.DashboardCount != 3 || response.EnabledDashboardCount != 2 || response.DisabledDashboardCount != 1 {
		t.Fatalf("unexpected dashboard counts %+v", response)
	}
	if response.PanelCount != 16 || response.EnabledPanelCount != 10 || response.DisabledPanelCount != 6 {
		t.Fatalf("unexpected dashboard panel counts %+v", response)
	}
	if dashboard, ok := dashboardByName(response.Dashboards, "zephyr.overview"); !ok || !dashboard.Enabled {
		t.Fatalf("expected enabled overview dashboard, got %+v", response.Dashboards)
	} else if panel, ok := dashboardPanelByID(dashboard.Panels, "node_readiness"); !ok || !panel.Enabled || len(panel.RecordingRules) != 1 || panel.RecordingRules[0] != "zephyr:node_readiness:ready" {
		t.Fatalf("expected node_readiness panel to reference readiness recording rule, got %+v", dashboard.Panels)
	}
	if dashboard, ok := dashboardByName(response.Dashboards, "zephyr.peer_sync"); !ok || dashboard.Enabled || !strings.Contains(dashboard.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer-sync dashboard, got %+v", response.Dashboards)
	} else if panel, ok := dashboardPanelByID(dashboard.Panels, "peer_admitted_ratio"); !ok || panel.Enabled || !strings.Contains(panel.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer_admitted_ratio panel, got %+v", dashboard.Panels)
	}
	if dashboard, ok := dashboardByName(response.Dashboards, "zephyr.peer_sync"); !ok {
		t.Fatalf("expected peer-sync dashboard metadata, got %+v", response.Dashboards)
	} else if panel, ok := dashboardPanelByID(dashboard.Panels, "peer_incident_error_codes"); !ok || panel.Enabled || !strings.Contains(panel.DisabledReason, "disabled") {
		t.Fatalf("expected disabled peer_incident_error_codes panel, got %+v", dashboard.Panels)
	}
}

func TestHandleGrafanaDashboardsExportsEnabledDashboardsOnly(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "dashboard-export-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/dashboards/grafana", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected grafana dashboards status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != grafanaDashboardContentType {
		t.Fatalf("expected grafana dashboard content type %q, got %q", grafanaDashboardContentType, recorder.Header().Get("Content-Type"))
	}

	var response GrafanaDashboardBundleResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode grafana dashboards response: %v", err)
	}
	if response.NodeID != "dashboard-export-node" {
		t.Fatalf("unexpected grafana dashboard node %+v", response)
	}
	if response.DashboardCount != 3 || response.PanelCount != 16 {
		t.Fatalf("unexpected grafana dashboard counts %+v", response)
	}
	if dashboard, ok := grafanaDashboardByName(response.Dashboards, "zephyr.peer_sync"); !ok {
		t.Fatalf("expected peer-sync grafana dashboard, got %+v", response.Dashboards)
	} else {
		if dashboard.Filename != "zephyr-peer-sync.grafana-dashboard.json" {
			t.Fatalf("unexpected grafana dashboard filename %+v", dashboard)
		}
		if dashboard.Dashboard.UID != "zephyr-peer-sync" {
			t.Fatalf("unexpected grafana dashboard uid %+v", dashboard.Dashboard)
		}
		panel, ok := grafanaPanelByTitle(dashboard.Dashboard.Panels, "Peer sync continuity state")
		if !ok || panel.Type != "bargauge" {
			t.Fatalf("expected peer sync continuity bargauge panel, got %+v", dashboard.Dashboard.Panels)
		}
		if _, ok := grafanaTargetByExpression(panel.Targets, "zephyr:peer_sync_continuity:breached"); !ok {
			t.Fatalf("expected peer sync breach query in grafana panel, got %+v", panel.Targets)
		}
		panel, ok = grafanaPanelByTitle(dashboard.Dashboard.Panels, "Peer incident error codes")
		if !ok || panel.Type != "bargauge" {
			t.Fatalf("expected peer incident error codes bargauge panel, got %+v", dashboard.Dashboard.Panels)
		}
		if _, ok := grafanaTargetByExpression(panel.Targets, "zephyr_peer_sync_error_code_occurrence_count"); !ok {
			t.Fatalf("expected peer incident error code query in grafana panel, got %+v", panel.Targets)
		}
	}
	if dashboard, ok := grafanaDashboardByName(response.Dashboards, "zephyr.overview"); !ok {
		t.Fatalf("expected overview grafana dashboard, got %+v", response.Dashboards)
	} else {
		panel, ok := grafanaPanelByTitle(dashboard.Dashboard.Panels, "Node readiness")
		if !ok || panel.Type != "stat" {
			t.Fatalf("expected node readiness stat panel, got %+v", dashboard.Dashboard.Panels)
		}
		if _, ok := grafanaTargetByExpression(panel.Targets, "zephyr:node_readiness:ready"); !ok {
			t.Fatalf("expected node readiness query in grafana panel, got %+v", panel.Targets)
		}
	}
}
func TestMetricsExposeConsensusAndPeerObservabilityAggregates(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "metrics-node",
		ValidatorAddress:             "zph_validator_metrics",
		PeerURLs:                     []string{"http://peer-a.example", "http://peer-b.example", "http://peer-c.example"},
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               true,
		RequirePeerIdentity:          true,
		EnforceProposerSchedule:      true,
		RequireConsensusCertificates: true,
	})

	proposalHash := consensusTestHash("metrics-proposal")
	voteHash := consensusTestHash("metrics-vote")
	proposalRecordedAt := time.Date(2026, time.March, 24, 21, 0, 0, 0, time.UTC)
	replayedAt := proposalRecordedAt.Add(30 * time.Second)
	voteRecordedAt := proposalRecordedAt.Add(1 * time.Minute)
	roundAdvanceRecordedAt := proposalRecordedAt.Add(2 * time.Minute)
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionProposal,
		Height:     2,
		Round:      0,
		BlockHash:  proposalHash,
		Validator:  "zph_validator_metrics",
		RecordedAt: proposalRecordedAt,
	}); err != nil {
		t.Fatalf("record proposal action: %v", err)
	}
	if err := server.ledger.MarkConsensusActionReplayed(ledger.ConsensusActionProposal, 2, 0, proposalHash, "zph_validator_metrics", replayedAt); err != nil {
		t.Fatalf("mark proposal replayed: %v", err)
	}
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionVote,
		Height:     2,
		Round:      0,
		BlockHash:  voteHash,
		Validator:  "zph_validator_peer",
		RecordedAt: voteRecordedAt,
	}); err != nil {
		t.Fatalf("record vote action: %v", err)
	}
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:       ledger.ConsensusActionRoundAdvance,
		Height:     2,
		Round:      1,
		Validator:  "zph_validator_metrics",
		RecordedAt: roundAdvanceRecordedAt,
		Status:     ledger.ConsensusActionCompleted,
		Note:       "advanced after timeout",
	}); err != nil {
		t.Fatalf("record round-advance action: %v", err)
	}

	firstDiagnosticAt := proposalRecordedAt.Add(3 * time.Minute)
	secondDiagnosticAt := proposalRecordedAt.Add(4 * time.Minute)
	if err := server.ledger.RecordConsensusDiagnostic(ledger.ConsensusDiagnostic{
		Kind:       "vote_rejected",
		Code:       "stale_round",
		Message:    "vote round is older than the current active round",
		Height:     2,
		Round:      0,
		BlockHash:  voteHash,
		Validator:  "zph_validator_peer",
		Source:     "api",
		ObservedAt: firstDiagnosticAt,
	}); err != nil {
		t.Fatalf("record first diagnostic: %v", err)
	}
	if err := server.ledger.RecordConsensusDiagnostic(ledger.ConsensusDiagnostic{
		Kind:       "block_import_rejected",
		Code:       "proposal_required",
		Message:    "consensus proposal is required before block import",
		Height:     2,
		Round:      0,
		BlockHash:  proposalHash,
		Validator:  "zph_validator_metrics",
		Source:     "peer_sync",
		ObservedAt: secondDiagnosticAt,
	}); err != nil {
		t.Fatalf("record second diagnostic: %v", err)
	}

	firstPeerSeenAt := proposalRecordedAt.Add(5 * time.Minute)
	secondPeerSeenAt := proposalRecordedAt.Add(6 * time.Minute)
	blockedAt := proposalRecordedAt.Add(7 * time.Minute)
	unadmittedAt := proposalRecordedAt.Add(8 * time.Minute)
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     1,
		Admitted:   true,
		Reachable:  false,
		SyncState:  "unreachable",
		LastSeenAt: &firstPeerSeenAt,
		Error:      "dial tcp timeout",
	})
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     1,
		Admitted:   true,
		Reachable:  false,
		SyncState:  "unreachable",
		LastSeenAt: &secondPeerSeenAt,
		Error:      "dial tcp timeout",
	})
	server.recordPeerView(PeerView{
		URL:                        "http://peer-b.example",
		Height:                     2,
		Admitted:                   true,
		Reachable:                  true,
		SyncState:                  "import_blocked",
		LastImportErrorCode:        "proposal_required",
		LastImportErrorMessage:     "consensus proposal is required before block import",
		LastImportFailureAt:        &blockedAt,
		LastImportFailureHeight:    2,
		LastImportFailureBlockHash: proposalHash,
	})
	server.recordPeerView(PeerView{
		URL:            "http://peer-c.example",
		Height:         2,
		Admitted:       false,
		Reachable:      true,
		SyncState:      "unadmitted",
		AdmissionError: "validator binding mismatch",
		LastSeenAt:     &unadmittedAt,
	})

	request := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected metrics status 200, got %d", recorder.Code)
	}

	var response MetricsResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if response.GeneratedAt.IsZero() {
		t.Fatal("expected generatedAt in metrics response")
	}
	if response.NodeID != "metrics-node" || response.ValidatorAddress != "zph_validator_metrics" {
		t.Fatalf("unexpected metrics identity %+v", response)
	}
	if response.PeerCount != 3 || response.BlockProduction || !response.PeerSyncEnabled || !response.PeerIdentityRequired || !response.ProposerScheduleEnforced || !response.ConsensusCertificatesRequired {
		t.Fatalf("unexpected metrics config flags %+v", response)
	}
	if response.ConsensusActions.TotalCount != 3 || response.ConsensusActions.PendingCount != 2 || response.ConsensusActions.TotalReplayAttempts != 1 {
		t.Fatalf("unexpected consensus action metrics %+v", response.ConsensusActions)
	}
	if response.ConsensusActions.LatestCompletedAt == nil || !response.ConsensusActions.LatestCompletedAt.Equal(roundAdvanceRecordedAt) {
		t.Fatalf("unexpected consensus action completion time %+v", response.ConsensusActions)
	}
	actionTypes := make(map[string]int)
	for _, bucket := range response.ConsensusActions.ByType {
		actionTypes[bucket.Label] = bucket.Count
	}
	if actionTypes[ledger.ConsensusActionProposal] != 1 || actionTypes[ledger.ConsensusActionVote] != 1 || actionTypes[ledger.ConsensusActionRoundAdvance] != 1 {
		t.Fatalf("unexpected consensus action type buckets %+v", response.ConsensusActions.ByType)
	}
	if response.Diagnostics.TotalCount != 2 || response.Diagnostics.LatestObservedAt == nil || !response.Diagnostics.LatestObservedAt.Equal(secondDiagnosticAt) {
		t.Fatalf("unexpected diagnostic metrics %+v", response.Diagnostics)
	}
	diagnosticCodes := make(map[string]int)
	for _, bucket := range response.Diagnostics.ByCode {
		diagnosticCodes[bucket.Label] = bucket.Count
	}
	if diagnosticCodes["stale_round"] != 1 || diagnosticCodes["proposal_required"] != 1 {
		t.Fatalf("unexpected diagnostic code buckets %+v", response.Diagnostics.ByCode)
	}
	if response.PeerSyncSummary.IncidentCount != 3 || response.PeerSyncSummary.AffectedPeerCount != 3 || response.PeerSyncSummary.TotalOccurrences != 4 {
		t.Fatalf("unexpected peer sync summary %+v", response.PeerSyncSummary)
	}
	if response.PeerSyncSummary.LatestObservedAt == nil || !response.PeerSyncSummary.LatestObservedAt.Equal(unadmittedAt) {
		t.Fatalf("unexpected peer sync summary time %+v", response.PeerSyncSummary)
	}
	if len(response.PeerSyncSummary.States) == 0 || response.PeerSyncSummary.States[0].State != "unreachable" || response.PeerSyncSummary.States[0].TotalOccurrences != 2 {
		t.Fatalf("unexpected peer sync state summaries %+v", response.PeerSyncSummary.States)
	}
	if len(response.PeerSyncSummary.Reasons) != 2 || response.PeerSyncSummary.Reasons[0].Reason != "unknown" || response.PeerSyncSummary.Reasons[0].IncidentCount != 2 || response.PeerSyncSummary.Reasons[0].AffectedPeerCount != 2 || response.PeerSyncSummary.Reasons[0].TotalOccurrences != 3 || response.PeerSyncSummary.Reasons[1].Reason != "validator binding mismatch" || response.PeerSyncSummary.Reasons[1].TotalOccurrences != 1 {
		t.Fatalf("unexpected peer sync reason summaries %+v", response.PeerSyncSummary.Reasons)
	}
	if len(response.PeerSyncSummary.ErrorCodes) != 2 || response.PeerSyncSummary.ErrorCodes[0].ErrorCode != "unknown" || response.PeerSyncSummary.ErrorCodes[0].IncidentCount != 2 || response.PeerSyncSummary.ErrorCodes[0].AffectedPeerCount != 2 || response.PeerSyncSummary.ErrorCodes[0].TotalOccurrences != 3 || response.PeerSyncSummary.ErrorCodes[1].ErrorCode != "proposal_required" || response.PeerSyncSummary.ErrorCodes[1].TotalOccurrences != 1 {
		t.Fatalf("unexpected peer sync error code summaries %+v", response.PeerSyncSummary.ErrorCodes)
	}
	if response.PeerRuntime.ConfiguredPeerCount != 3 || response.PeerRuntime.ReachablePeerCount != 2 || response.PeerRuntime.UnreachablePeerCount != 1 || response.PeerRuntime.AdmittedPeerCount != 2 || response.PeerRuntime.UnadmittedPeerCount != 1 {
		t.Fatalf("unexpected peer runtime metrics %+v", response.PeerRuntime)
	}
	syncStates := make(map[string]int)
	for _, bucket := range response.PeerRuntime.BySyncState {
		syncStates[bucket.Label] = bucket.Count
	}
	if syncStates["unreachable"] != 1 || syncStates["import_blocked"] != 1 || syncStates["unadmitted"] != 1 {
		t.Fatalf("unexpected peer runtime sync-state buckets %+v", response.PeerRuntime.BySyncState)
	}
}

func TestPrometheusMetricsExportOperatorSignals(t *testing.T) {
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "prometheus-node",
		PeerURLs:                []string{"http://peer-a.example", "http://peer-b.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          true,
	})

	recordedAt := time.Date(2026, time.March, 24, 23, 0, 0, 0, time.UTC)
	if err := server.ledger.RecordConsensusAction(ledger.ConsensusAction{
		Type:        ledger.ConsensusActionProposal,
		Height:      2,
		Round:       0,
		BlockHash:   consensusTestHash("prometheus-proposal"),
		Validator:   "zph_validator_prometheus",
		RecordedAt:  recordedAt,
		Status:      ledger.ConsensusActionCompleted,
		CompletedAt: &recordedAt,
	}); err != nil {
		t.Fatalf("record prometheus proposal action: %v", err)
	}
	if err := server.ledger.RecordConsensusDiagnostic(ledger.ConsensusDiagnostic{
		Kind:       "proposal_rejected",
		Code:       "template_mismatch",
		Message:    "proposal does not match local block template",
		Height:     2,
		Round:      0,
		BlockHash:  consensusTestHash("prometheus-proposal"),
		Validator:  "zph_validator_prometheus",
		Source:     "api",
		ObservedAt: recordedAt.Add(1 * time.Minute),
	}); err != nil {
		t.Fatalf("record prometheus diagnostic: %v", err)
	}

	unreachableAt := recordedAt.Add(2 * time.Minute)
	blockedAt := recordedAt.Add(3 * time.Minute)
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     1,
		Admitted:   true,
		Reachable:  false,
		SyncState:  "unreachable",
		LastSeenAt: &unreachableAt,
		Error:      "dial tcp timeout",
	})
	server.recordPeerView(PeerView{
		URL:                        "http://peer-b.example",
		Height:                     2,
		Admitted:                   true,
		Reachable:                  true,
		SyncState:                  "import_blocked",
		LastImportErrorCode:        "proposal_required",
		LastImportErrorMessage:     "consensus proposal is required before block import",
		LastImportFailureAt:        &blockedAt,
		LastImportFailureHeight:    2,
		LastImportFailureBlockHash: consensusTestHash("prometheus-proposal"),
	})

	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	recorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected prometheus metrics status 200, got %d", recorder.Code)
	}
	if recorder.Header().Get("Content-Type") != prometheusContentType {
		t.Fatalf("expected prometheus content type %q, got %q", prometheusContentType, recorder.Header().Get("Content-Type"))
	}

	body := recorder.Body.String()
	requirePrometheusLine(t, body, "# HELP zephyr_node_ready Whether the node is currently ready according to /v1/health.")
	requirePrometheusLine(t, body, "zephyr_node_ready 1")
	requirePrometheusLine(t, body, "zephyr_health_status{status=\"warn\"} 1")
	requirePrometheusLine(t, body, "zephyr_health_check_status{check=\"diagnostics\",status=\"warn\"} 1")
	requirePrometheusLine(t, body, "zephyr_alert_count 3")
	requirePrometheusLine(t, body, "zephyr_alert_count_by_severity{severity=\"warning\"} 3")
	requirePrometheusLine(t, body, "zephyr_alert_active{code=\"peer_sync_degraded\",severity=\"warning\",component=\"peer_sync\"} 1")
	requirePrometheusLine(t, body, "zephyr_alert_active{code=\"peer_import_blocked\",severity=\"warning\",component=\"peer_sync\"} 1")
	requirePrometheusLine(t, body, "zephyr_alert_active{code=\"recent_consensus_diagnostics\",severity=\"warning\",component=\"consensus\"} 1")
	requirePrometheusLine(t, body, "zephyr_slo_objective_count 3")
	requirePrometheusLine(t, body, "zephyr_slo_status_count{status=\"at_risk\"} 3")
	requirePrometheusLine(t, body, "zephyr_slo_objective_status{objective=\"node_readiness\",status=\"at_risk\"} 1")
	requirePrometheusLine(t, body, "zephyr_slo_objective_status{objective=\"consensus_continuity\",status=\"at_risk\"} 1")
	requirePrometheusLine(t, body, "zephyr_slo_objective_status{objective=\"peer_sync_continuity\",status=\"at_risk\"} 1")
	requirePrometheusLine(t, body, "zephyr_consensus_action_by_type_count{type=\"proposal\"} 1")
	requirePrometheusLine(t, body, "zephyr_consensus_diagnostic_by_code_count{code=\"template_mismatch\"} 1")
	requirePrometheusLine(t, body, "zephyr_peer_runtime_by_sync_state_count{state=\"unreachable\"} 1")
	requirePrometheusLine(t, body, "zephyr_peer_runtime_by_sync_state_count{state=\"import_blocked\"} 1")
	requirePrometheusLine(t, body, "zephyr_peer_sync_occurrence_count 2")
	requirePrometheusLine(t, body, "zephyr_peer_sync_reason_occurrence_count{reason=\"unknown\"} 2")
	requirePrometheusLine(t, body, "zephyr_peer_sync_error_code_occurrence_count{code=\"proposal_required\"} 1")
	requirePrometheusLine(t, body, "zephyr_peer_sync_error_code_occurrence_count{code=\"unknown\"} 1")
}
func TestStructuredLogsEmitConsensusPeerAndRecoveryEvents(t *testing.T) {
	var logBuffer bytes.Buffer
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "structured-node",
		ValidatorAddress:        "zph_validator_logs",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
		EnableStructuredLogs:    true,
		StructuredLogWriter:     &logBuffer,
	})

	server.recordConsensusDiagnostic("proposal_rejected", "api", ledger.ErrConsensusTemplateMismatch, 3, 1, consensusTestHash("structured-diagnostic"), "zph_validator_peer")
	incidentObservedAt := time.Date(2026, time.March, 24, 22, 0, 0, 0, time.UTC)
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     2,
		Reachable:  false,
		SyncState:  "unreachable",
		LastSeenAt: &incidentObservedAt,
		Error:      "dial tcp timeout",
	})
	restoredAt := incidentObservedAt.Add(1 * time.Minute)
	snapshotBlockHash := consensusTestHash("structured-snapshot")
	server.recordSnapshotRestore("http://peer-a.example", ledger.Snapshot{
		Blocks: []ledger.Block{{Height: 1, Hash: consensusTestHash("structured-snapshot-previous")}, {Height: 2, Hash: snapshotBlockHash}},
	}, restoredAt)

	entries := decodeStructuredLogEntries(t, logBuffer.String())
	if len(entries) != 3 {
		t.Fatalf("expected 3 structured log entries, got %+v", entries)
	}

	diagnosticEntry := entries[0]
	if diagnosticEntry["component"] != "consensus" || diagnosticEntry["event"] != "diagnostic" {
		t.Fatalf("unexpected diagnostic structured log %+v", diagnosticEntry)
	}
	if diagnosticEntry["nodeId"] != "structured-node" || diagnosticEntry["validatorAddress"] != "zph_validator_logs" {
		t.Fatalf("unexpected diagnostic identity fields %+v", diagnosticEntry)
	}
	if diagnosticEntry["kind"] != "proposal_rejected" || diagnosticEntry["code"] != "template_mismatch" || diagnosticEntry["source"] != "api" {
		t.Fatalf("unexpected diagnostic fields %+v", diagnosticEntry)
	}
	if diagnosticEntry["timestamp"] == "" {
		t.Fatalf("expected diagnostic timestamp %+v", diagnosticEntry)
	}

	incidentEntry := entries[1]
	if incidentEntry["component"] != "peer_sync" || incidentEntry["event"] != "incident" {
		t.Fatalf("unexpected incident structured log %+v", incidentEntry)
	}
	if incidentEntry["peerUrl"] != "http://peer-a.example" || incidentEntry["state"] != "unreachable" || incidentEntry["errorMessage"] != "dial tcp timeout" {
		t.Fatalf("unexpected incident fields %+v", incidentEntry)
	}
	if incidentEntry["timestamp"] != incidentObservedAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected incident timestamp %+v", incidentEntry)
	}

	snapshotEntry := entries[2]
	if snapshotEntry["component"] != "recovery" || snapshotEntry["event"] != "snapshot_restore" {
		t.Fatalf("unexpected snapshot structured log %+v", snapshotEntry)
	}
	if snapshotEntry["peer"] != "http://peer-a.example" || snapshotEntry["blockHash"] != snapshotBlockHash {
		t.Fatalf("unexpected snapshot fields %+v", snapshotEntry)
	}
	if snapshotEntry["height"] != float64(2) || snapshotEntry["timestamp"] != restoredAt.Format(time.RFC3339Nano) {
		t.Fatalf("unexpected snapshot timing fields %+v", snapshotEntry)
	}

	metricsRequest := httptest.NewRequest(http.MethodGet, "/v1/metrics", nil)
	metricsRecorder := httptest.NewRecorder()
	server.Handler().ServeHTTP(metricsRecorder, metricsRequest)
	if metricsRecorder.Code != http.StatusOK {
		t.Fatalf("expected metrics 200, got %d", metricsRecorder.Code)
	}
	var metricsResponse MetricsResponse
	if err := json.NewDecoder(metricsRecorder.Body).Decode(&metricsResponse); err != nil {
		t.Fatalf("decode metrics response: %v", err)
	}
	if !metricsResponse.StructuredLogsEnabled {
		t.Fatalf("expected structuredLogsEnabled=true in metrics response %+v", metricsResponse)
	}
}

func TestStructuredLogsCanBeDisabled(t *testing.T) {
	var logBuffer bytes.Buffer
	server := newTestServer(t, Config{
		DataDir:                 t.TempDir(),
		NodeID:                  "plain-node",
		PeerURLs:                []string{"http://peer-a.example"},
		BlockInterval:           0,
		SyncInterval:            0,
		MaxTransactionsPerBlock: 10,
		EnableBlockProduction:   false,
		EnablePeerSync:          false,
		StructuredLogWriter:     &logBuffer,
	})

	server.recordConsensusDiagnostic("proposal_rejected", "api", ledger.ErrConsensusTemplateMismatch, 3, 1, consensusTestHash("disabled-structured-diagnostic"), "zph_validator_peer")
	observedAt := time.Date(2026, time.March, 24, 22, 5, 0, 0, time.UTC)
	server.recordPeerView(PeerView{
		URL:        "http://peer-a.example",
		Height:     1,
		Reachable:  false,
		SyncState:  "unreachable",
		LastSeenAt: &observedAt,
		Error:      "dial tcp timeout",
	})
	server.recordSnapshotRestore("http://peer-a.example", ledger.Snapshot{
		Blocks: []ledger.Block{{Height: 1, Hash: consensusTestHash("disabled-structured-snapshot")}},
	}, observedAt.Add(1*time.Minute))

	if strings.TrimSpace(logBuffer.String()) != "" {
		t.Fatalf("expected no structured logs when disabled, got %q", logBuffer.String())
	}
}
func TestHandleImportBlockRejectsAndExposesPendingImportRecovery(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}

	producer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	if _, err := producer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set producer validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "pending-import-recovery")
	if _, err := producer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}

	producedAt := time.Date(2026, time.March, 24, 18, 0, 0, 0, time.UTC)
	template, err := producer.ledger.BuildNextBlock(10, producedAt)
	if err != nil {
		t.Fatalf("build producer template: %v", err)
	}
	proposal := signedConsensusProposal(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions)
	if err := producer.ledger.RecordProposal(proposal); err != nil {
		t.Fatalf("record producer proposal: %v", err)
	}
	if _, _, err := producer.ledger.RecordVote(signedConsensusVote(t, proposer, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer vote A: %v", err)
	}
	if _, _, err := producer.ledger.RecordVote(signedConsensusVote(t, voter, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer vote B: %v", err)
	}
	block, err := producer.produceLocalBlock(producedAt)
	if err != nil {
		t.Fatalf("produce certified block: %v", err)
	}

	replica := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	if _, err := replica.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set replica validators: %v", err)
	}
	if _, err := replica.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit replica sender: %v", err)
	}

	body, err := json.Marshal(block)
	if err != nil {
		t.Fatalf("marshal block: %v", err)
	}
	request := httptest.NewRequest(http.MethodPost, "/v1/internal/blocks", bytes.NewReader(body))
	recorder := httptest.NewRecorder()
	replica.Handler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected import status 409, got %d", recorder.Code)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	replica.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status response 200, got %d", statusRecorder.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if response.Recovery.NeedsReplay || !response.Recovery.NeedsRecovery || response.Recovery.PendingActionCount != 1 || response.Recovery.PendingReplayCount != 0 || response.Recovery.PendingImportCount != 1 {
		t.Fatalf("unexpected recovery state after rejected import: %+v", response.Recovery)
	}
	if len(response.Recovery.PendingImportHeights) != 1 || response.Recovery.PendingImportHeights[0] != block.Height {
		t.Fatalf("unexpected pending import heights: %+v", response.Recovery.PendingImportHeights)
	}
	if len(response.Recovery.PendingActions) != 1 {
		t.Fatalf("expected one pending recovery action, got %+v", response.Recovery.PendingActions)
	}
	pending := response.Recovery.PendingActions[0]
	if pending.Type != ledger.ConsensusActionBlockImport || pending.Status != ledger.ConsensusActionPending || !strings.Contains(pending.Note, "proposal_required") {
		t.Fatalf("unexpected pending import action %+v", pending)
	}
	if len(response.Diagnostics.Recent) == 0 {
		t.Fatal("expected import diagnostics after rejected peer block")
	}
	diagnostic := response.Diagnostics.Recent[0]
	if diagnostic.Kind != "block_import_rejected" || diagnostic.Code != "proposal_required" || diagnostic.Source != "peer" {
		t.Fatalf("unexpected import diagnostic %+v", diagnostic)
	}
}

func TestPeerSyncConsensusImportFailureRestoresSnapshotAndRecordsRecoveryHistory(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}

	producer := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "producer",
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        true,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	if _, err := producer.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set producer validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "peer-sync-import-recovery")
	if _, err := producer.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}

	producedAt := time.Date(2026, time.March, 24, 18, 30, 0, 0, time.UTC)
	template, err := producer.ledger.BuildNextBlock(10, producedAt)
	if err != nil {
		t.Fatalf("build producer template: %v", err)
	}
	proposal := signedConsensusProposal(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions)
	if err := producer.ledger.RecordProposal(proposal); err != nil {
		t.Fatalf("record producer proposal: %v", err)
	}
	if _, _, err := producer.ledger.RecordVote(signedConsensusVote(t, proposer, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer vote A: %v", err)
	}
	if _, _, err := producer.ledger.RecordVote(signedConsensusVote(t, voter, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer vote B: %v", err)
	}
	block, err := producer.produceLocalBlock(producedAt)
	if err != nil {
		t.Fatalf("produce certified producer block: %v", err)
	}

	producerHTTP := httptest.NewServer(producer.Handler())
	defer producerHTTP.Close()

	replicaDataDir := t.TempDir()
	replicaSeed := newTestServer(t, Config{
		DataDir:                      replicaDataDir,
		NodeID:                       "replica-seed",
		BlockInterval:                0,
		SyncInterval:                 0,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
	})
	if _, err := replicaSeed.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set replica validators: %v", err)
	}
	if _, err := replicaSeed.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit replica sender: %v", err)
	}
	replicaSeed.Close()

	replica := newTestServer(t, Config{
		DataDir:                      replicaDataDir,
		NodeID:                       "replica",
		PeerURLs:                     []string{producerHTTP.URL},
		BlockInterval:                0,
		SyncInterval:                 50 * time.Millisecond,
		MaxTransactionsPerBlock:      10,
		EnableBlockProduction:        false,
		EnablePeerSync:               true,
		RequireConsensusCertificates: true,
	})

	waitFor(t, func() bool {
		return replica.ledger.Status().Height == 1
	})

	latest, ok := replica.ledger.LatestBlock()
	if !ok || latest.Hash != block.Hash {
		t.Fatalf("expected replica latest block %s after snapshot recovery, got %+v", block.Hash, latest)
	}

	statusRequest := httptest.NewRequest(http.MethodGet, "/v1/status", nil)
	statusRecorder := httptest.NewRecorder()
	replica.Handler().ServeHTTP(statusRecorder, statusRequest)
	if statusRecorder.Code != http.StatusOK {
		t.Fatalf("expected status response 200, got %d", statusRecorder.Code)
	}

	var response StatusResponse
	if err := json.NewDecoder(statusRecorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode status response: %v", err)
	}
	if response.Recovery.NeedsReplay || response.Recovery.NeedsRecovery || response.Recovery.PendingActionCount != 0 || response.Recovery.PendingImportCount != 0 {
		t.Fatalf("expected recovered status after peer snapshot restore, got %+v", response.Recovery)
	}
	if response.Recovery.LastSnapshotRestoreAt == nil || response.Recovery.LastSnapshotRestoreHeight != block.Height || response.Recovery.LastSnapshotRestoreBlockHash != block.Hash {
		t.Fatalf("expected snapshot restore metadata in recovery view, got %+v", response.Recovery)
	}
	sawCompletedImport := false
	sawSnapshotRestore := false
	for _, action := range response.Recovery.RecentActions {
		if action.Type == ledger.ConsensusActionBlockImport && action.Status == ledger.ConsensusActionCompleted {
			sawCompletedImport = true
		}
		if action.Type == ledger.ConsensusActionSnapshotSync && action.Status == ledger.ConsensusActionCompleted {
			sawSnapshotRestore = true
		}
	}
	if !sawCompletedImport || !sawSnapshotRestore {
		t.Fatalf("expected completed import and snapshot recovery actions, got %+v", response.Recovery.RecentActions)
	}
	if len(response.Diagnostics.Recent) == 0 {
		t.Fatal("expected preserved import diagnostic after peer snapshot restore")
	}
	diagnostic := response.Diagnostics.Recent[0]
	if diagnostic.Kind != "block_import_rejected" || diagnostic.Code != "proposal_required" || diagnostic.Source != "peer_sync" {
		t.Fatalf("unexpected peer-sync diagnostic %+v", diagnostic)
	}

	waitFor(t, func() bool {
		peers := replica.peerSnapshot()
		return len(peers) == 1 && peers[0].LastSnapshotRestoreAt != nil && peers[0].LastImportFailureAt != nil
	})
	peers := replica.peerSnapshot()
	if len(peers) != 1 {
		t.Fatalf("expected 1 peer view after import repair, got %d", len(peers))
	}
	if peers[0].SyncState != "snapshot_restored" {
		t.Fatalf("expected snapshot_restored peer sync state, got %+v", peers[0])
	}
	if peers[0].LastImportErrorCode != "proposal_required" || peers[0].LastImportFailureHeight != block.Height || peers[0].LastImportFailureBlockHash != block.Hash {
		t.Fatalf("unexpected peer import failure telemetry %+v", peers[0])
	}
	if peers[0].LastSnapshotRestoreReason != "import_repair" || peers[0].LastSnapshotRestoreHeight != block.Height || peers[0].LastSnapshotRestoreBlockHash != block.Hash {
		t.Fatalf("unexpected peer snapshot repair telemetry %+v", peers[0])
	}
}

func healthCheckByName(checks []HealthCheck, name string) (HealthCheck, bool) {
	for _, check := range checks {
		if check.Name == name {
			return check, true
		}
	}
	return HealthCheck{}, false
}

func alertByCode(alerts []Alert, code string) (Alert, bool) {
	for _, alert := range alerts {
		if alert.Code == code {
			return alert, true
		}
	}
	return Alert{}, false
}

func alertRuleByName(groups []AlertRuleGroup, name string) (AlertRule, bool) {
	for _, group := range groups {
		for _, rule := range group.Rules {
			if rule.Name == name {
				return rule, true
			}
		}
	}
	return AlertRule{}, false
}

func recordingRuleByRecord(groups []RecordingRuleGroup, record string) (RecordingRule, bool) {
	for _, group := range groups {
		for _, rule := range group.Rules {
			if rule.Record == record {
				return rule, true
			}
		}
	}
	return RecordingRule{}, false
}

func dashboardByName(dashboards []Dashboard, name string) (Dashboard, bool) {
	for _, dashboard := range dashboards {
		if dashboard.Name == name {
			return dashboard, true
		}
	}
	return Dashboard{}, false
}

func dashboardPanelByID(panels []DashboardPanel, id string) (DashboardPanel, bool) {
	for _, panel := range panels {
		if panel.ID == id {
			return panel, true
		}
	}
	return DashboardPanel{}, false
}

func grafanaDashboardByName(dashboards []GrafanaDashboardExport, name string) (GrafanaDashboardExport, bool) {
	for _, dashboard := range dashboards {
		if dashboard.Name == name {
			return dashboard, true
		}
	}
	return GrafanaDashboardExport{}, false
}

func grafanaPanelByTitle(panels []GrafanaPanel, title string) (GrafanaPanel, bool) {
	for _, panel := range panels {
		if panel.Title == title {
			return panel, true
		}
	}
	return GrafanaPanel{}, false
}

func grafanaTargetByExpression(targets []GrafanaTarget, expression string) (GrafanaTarget, bool) {
	for _, target := range targets {
		if target.Expr == expression {
			return target, true
		}
	}
	return GrafanaTarget{}, false
}
func sloObjectiveByName(objectives []SLOObjective, name string) (SLOObjective, bool) {
	for _, objective := range objectives {
		if objective.Name == name {
			return objective, true
		}
	}
	return SLOObjective{}, false
}

func requirePrometheusLine(t *testing.T, body string, line string) {
	t.Helper()

	if !strings.Contains(body, line+"\n") && !strings.HasSuffix(body, line) {
		t.Fatalf("expected prometheus output to contain line %q, got:\n%s", line, body)
	}
}

func decodeStructuredLogEntries(t *testing.T, raw string) []map[string]any {
	t.Helper()

	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	entries := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode structured log %q: %v", line, err)
		}
		entries = append(entries, entry)
	}
	return entries
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

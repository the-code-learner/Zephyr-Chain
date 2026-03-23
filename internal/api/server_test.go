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

	proposal := signedConsensusProposal(t, proposer, 1, 0, consensusTestHash("block-1"), "")
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

	proposal := signedConsensusProposal(t, proposer, 1, 0, consensusTestHash("peer-block-1"), "")
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
	if _, err := producer.produceLocalBlock(); err != nil {
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
	producerBlock, err := producer.produceLocalBlock()
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
	if _, err := replicaSeed.produceLocalBlock(); err != nil {
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

func signedConsensusProposal(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string, previousHash string) consensus.Proposal {
	t.Helper()

	proposal := consensus.Proposal{
		Height:       height,
		Round:        round,
		BlockHash:    blockHash,
		PreviousHash: previousHash,
		Proposer:     signer.address,
		PublicKey:    signer.publicKey,
	}
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

func consensusTestHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A receiver missing two consecutive committed heights cannot import the
// sender's latest block directly. On explicit HTTP 409, replay valid local
// signed evidence oldest-first with peer sync still disabled.
func TestCertifiedReplayRecoversTwoMissingHeightsWithoutPeerSync(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	producer := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "catchup-producer",
		ValidatorPrivateKey:   encodedPrivateKey(t, proposer.privateKey),
		EnableBlockProduction: true, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	replica := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "catchup-replica",
		ValidatorPrivateKey:   encodedPrivateKey(t, voter.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	receiver := httptest.NewServer(replica.Handler())
	defer receiver.Close()

	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	for _, server := range []*Server{producer, replica} {
		if _, err := server.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
			t.Fatalf("set validators: %v", err)
		}
	}

	// Both nodes must have exactly the same funded genesis-era account
	// state BEFORE either certified block is produced. Funding a future
	// sender only after block 1 on the producer would make the replica's
	// block-1 state root differ before any quorum can be verified.
	envelopes := []tx.Envelope{
		signedEnvelope(t, 25, 1, "catchup-signed-quorum-1"),
		signedEnvelope(t, 25, 1, "catchup-signed-quorum-2"),
	}
	for _, envelope := range envelopes {
		for _, server := range []*Server{producer, replica} {
			if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
				t.Fatalf("credit sender: %v", err)
			}
		}
	}
	var previousHash string
	blocks := make([]ledger.Block, 0, 2)
	for height := uint64(1); height <= 2; height++ {
		envelope := envelopes[height-1]
		if _, err := producer.ledger.Accept(envelope); err != nil {
			t.Fatalf("accept tx at height %d: %v", height, err)
		}
		scheduledSigner := proposer
		if height == 2 {
			scheduledSigner = voter
		}
		proposal := signedConsensusProposalForServer(t, producer, scheduledSigner, height, 0, previousHash, time.Now().UTC(), []tx.Envelope{envelope})
		if err := producer.ledger.RecordProposal(proposal); err != nil {
			t.Fatalf("record proposal at height %d: %v", height, err)
		}
		for _, signer := range []consensusSigner{proposer, voter} {
			vote := signedConsensusVote(t, signer, height, 0, proposal.BlockHash)
			if _, _, err := producer.ledger.RecordVote(vote); err != nil {
				t.Fatalf("record quorum at height %d: %v", height, err)
			}
		}
		block, err := producer.ledger.ProduceBlockWithOptions(10, proposal.ProducedAt, true)
		if err != nil {
			t.Fatalf("commit certified block height %d: %v", height, err)
		}
		previousHash = block.Hash
		blocks = append(blocks, block)
	}
	if got := replica.ledger.Status().Height; got != 0 {
		t.Fatalf("replica unexpectedly advanced before signed replay, height=%d", got)
	}
	transport, ok := producer.transport.(certifiedBlockDeliveryTransport)
	if !ok {
		t.Fatal("production transport missing certified block delivery")
	}
	evidence, ok := producer.ledger.CertifiedBlockEvidenceAt(2)
	if !ok {
		t.Fatal("missing durable latest signed QC evidence")
	}
	// An untrusted status source can claim height 1 and a conflicting
	// header while the replica is actually at height 0. Even if the
	// transport identity itself is valid, the status tip is not signed
	// finality; a conflicting ancestor must stop the replay before any
	// signed block history is streamed based on that claim.
	forgedStatus := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/status" {
			replica.Handler().ServeHTTP(w, r)
			return
		}
		recorder := httptest.NewRecorder()
		replica.Handler().ServeHTTP(recorder, r)
		if recorder.Code != http.StatusOK {
			t.Errorf("status request failed: %d", recorder.Code)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		var advertised StatusResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &advertised); err != nil {
			t.Errorf("parse peer status: %v", err)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		advertised.Status.Height = 1
		advertised.Status.LatestBlockHash = strings.Repeat("f", 64)
		writeJSON(w, http.StatusOK, advertised)
	}))
	defer forgedStatus.Close()
	err := producer.deliverCertifiedReplay(
		forgedStatus.URL, certifiedBlockDelivery{Block: blocks[1], Evidence: evidence}, transport,
	)
	if err == nil || !strings.Contains(err.Error(), "tip does not match") {
		t.Fatalf("expected fail-closed rejection of conflicting peer tip, got %v", err)
	}
	if got := replica.ledger.Status().Height; got != 0 {
		t.Fatalf("forged status caused unsafe replay, replica height=%d", got)
	}

	if err := producer.deliverCertifiedReplay(receiver.URL, certifiedBlockDelivery{Block: blocks[1], Evidence: evidence}, transport); err != nil {
		t.Fatalf("bounded certified multiheight catchup failed: %v", err)
	}
	if got := replica.ledger.Status().Height; got != 2 {
		t.Fatalf("replica did not recover 2 heights, height=%d", got)
	}
	for _, block := range blocks {
		if got, ok := replica.ledger.BlockAtHeight(block.Height); !ok || got.Hash != block.Hash {
			t.Fatalf("wrong replay block at height %d", block.Height)
		}
		if _, ok := replica.ledger.Certificate(block.Height, 0, block.Hash); !ok {
			t.Fatalf("missing locally verified QC at height %d", block.Height)
		}
	}
}

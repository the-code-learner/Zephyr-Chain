package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A producer that has both signed votes can finalize while a replica still
// has only its own 60%-power vote. The replica must verify the missing vote
// cryptographically before importing the certified block; a naked block may
// never bypass the receiver's local QC requirement.
func TestCertifiedBlockDeliveryRepairsReplicaMissingQuorum(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	producer := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "cert-producer",
		ValidatorPrivateKey:   encodedPrivateKey(t, proposer.privateKey),
		EnableBlockProduction: true, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	replica := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "cert-replica",
		ValidatorPrivateKey:   encodedPrivateKey(t, voter.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	// Drop one authenticated certified POST to model a transient partition
	// after the proposer has durably committed. No peer sync is available.
	var dropFirstCertified atomic.Bool
	var dropped atomic.Int32
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/internal/certified-blocks" && dropFirstCertified.CompareAndSwap(true, false) {
			dropped.Add(1)
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		replica.Handler().ServeHTTP(w, r)
	}))
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

	envelope := signedEnvelope(t, 25, 1, "certified-delivery-missing-vote")
	for _, server := range []*Server{producer, replica} {
		if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
			t.Fatalf("credit sender: %v", err)
		}
	}
	if _, err := producer.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}
	proposal := signedConsensusProposalForServer(t, producer, proposer, 1, 0, "", time.Now().UTC(), []tx.Envelope{envelope})
	for _, server := range []*Server{producer, replica} {
		if err := server.ledger.RecordProposal(proposal); err != nil {
			t.Fatalf("record proposal: %v", err)
		}
	}
	voteA := signedConsensusVote(t, proposer, 1, 0, proposal.BlockHash)
	voteB := signedConsensusVote(t, voter, 1, 0, proposal.BlockHash)
	if _, _, err := replica.ledger.RecordVote(voteA); err != nil {
		t.Fatalf("replica partial vote: %v", err)
	}
	for _, vote := range []consensus.Vote{voteA, voteB} {
		if _, _, err := producer.ledger.RecordVote(vote); err != nil {
			t.Fatalf("producer quorum vote: %v", err)
		}
	}
	block, err := producer.ledger.ProduceBlockWithOptions(10, proposal.ProducedAt, true)
	if err != nil {
		t.Fatalf("produce certified block: %v", err)
	}
	if err := replica.ledger.ImportBlockWithOptions(block, true); !errors.Is(err, ledger.ErrConsensusCertificateRequired) {
		t.Fatalf("bare certified block must fail without local QC, got %v", err)
	}
	evidence, ok := producer.ledger.CertifiedBlockEvidenceAt(block.Height)
	if !ok || len(evidence.Votes) != 2 {
		t.Fatalf("producer missing signed quorum evidence: %+v", evidence)
	}
	transport, ok := producer.transport.(certifiedBlockDeliveryTransport)
	if !ok {
		t.Fatal("production transport missing certified delivery")
	}
	delivery := certifiedBlockDelivery{Block: block, Evidence: evidence}
	tampered := delivery
	tampered.Evidence.Votes = append([]consensus.Vote(nil), evidence.Votes...)
	tampered.Evidence.Votes[0].Signature = "forged"
	if err := transport.PostCertifiedBlock(receiver.URL, tampered); err == nil {
		t.Fatal("forged signed vote must be rejected before import")
	}
	if got := replica.ledger.Status().Height; got != 0 {
		t.Fatalf("forged evidence advanced replica height to %d", got)
	}
	// Exercise the real post-commit broadcaster, not merely the optional
	// transport method. The replica has peer sync disabled and no QC yet.
	producer.setPeerURLs([]string{receiver.URL})
	dropFirstCertified.Store(true)
	producer.broadcastBlock(block)
	if got := replica.ledger.Status().Height; got != 0 || dropped.Load() != 1 {
		t.Fatalf("lost first delivery must not finalize replica: height=%d, dropped=%d", got, dropped.Load())
	}
	producer.maybeRebroadcastLatestCertifiedBlock()
	if got := replica.ledger.Status().Height; got != 0 {
		t.Fatalf("retry before backoff advanced replica to height=%d", got)
	}
	// Force the retry deadline to expire without a nondeterministic sleep.
	producer.certifiedReplayMu.Lock()
	replay := producer.certifiedReplay[receiver.URL]
	if replay.acknowledged || replay.blockHash != block.Hash {
		producer.certifiedReplayMu.Unlock()
		t.Fatalf("failed first delivery incorrectly acknowledged: %+v", replay)
	}
	replay.lastAttempt = time.Now().Add(-certifiedReplayInterval)
	producer.certifiedReplay[receiver.URL] = replay
	producer.certifiedReplayMu.Unlock()

	producer.maybeRebroadcastLatestCertifiedBlock()
	waitFor(t, func() bool { return replica.ledger.Status().Height == 1 })
	producer.certifiedReplayMu.Lock()
	replay = producer.certifiedReplay[receiver.URL]
	producer.certifiedReplayMu.Unlock()
	if !replay.acknowledged || replay.inFlight {
		t.Fatalf("successful signed retry must settle delivery state: %+v", replay)
	}
	if _, ok := replica.ledger.Certificate(1, 0, block.Hash); !ok {
		t.Fatal("replica missing independently reconstructed QC")
	}
	if err := transport.PostCertifiedBlock(receiver.URL, delivery); err != nil {
		t.Fatalf("repeated certified delivery should be idempotent: %v", err)
	}
	// The authenticated endpoint does not permit a client to assert
	// finality without the signed peer identity proof.
	unauthenticated := httptest.NewRequest(http.MethodPost, "/v1/internal/certified-blocks", nil)
	recorder := httptest.NewRecorder()
	replica.Handler().ServeHTTP(recorder, unauthenticated)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("unsigned certified delivery status=%d, want 403", recorder.Code)
	}
}

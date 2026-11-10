package api

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A test-only HTTP origin deliberately holds BOTH signed consensus POSTs.
// This is a reproducible failure-injection test, not a claim that the
// historical scheduled proposer rotation flake had this exact root cause.
func TestConsensusAutomationStalledPeerDoesNotBlockRoundRotation(t *testing.T) {
	local := newConsensusSigner(t)
	other := newConsensusSigner(t)
	s := newTestServer(t, Config{
		DataDir:                      t.TempDir(),
		NodeID:                       "stalled-peer-consensus",
		ValidatorPrivateKey:          encodedPrivateKey(t, local.privateKey),
		BlockInterval:                0,
		SyncInterval:                 0,
		EnableConsensusAutomation:    false,
		EnableBlockProduction:        false,
		EnablePeerSync:               false,
		RequireConsensusCertificates: true,
		ConsensusRoundTimeout:        100 * time.Millisecond,
	})
	validators := []dpos.Validator{
		{Rank: 1, Address: local.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: other.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := s.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatal(err)
	}
	envelope := signedEnvelope(t, 25, 1, "stalled-consensus-outbound")
	if _, err := s.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatal(err)
	}
	root, err := s.ledger.ExpectedStateRoot([]tx.Envelope{envelope})
	if err != nil {
		t.Fatal(err)
	}
	proposal := signedConsensusProposal(t, local, 1, 0, "", time.Now().UTC(), []tx.Envelope{envelope}, root)
	if err := s.ledger.RecordProposalWithAction(proposal, &ledger.ConsensusAction{
		Type: ledger.ConsensusActionProposal, Height: proposal.Height, Round: proposal.Round,
		BlockHash: proposal.BlockHash, Validator: proposal.Proposer,
	}); err != nil {
		t.Fatal(err)
	}
	vote := signedConsensusVote(t, local, 1, 0, proposal.BlockHash)
	if _, certificate, err := s.ledger.RecordVoteWithAction(vote, &ledger.ConsensusAction{
		Type: ledger.ConsensusActionVote, Height: vote.Height, Round: vote.Round,
		BlockHash: vote.BlockHash, Validator: vote.Voter,
	}); err != nil || certificate != nil {
		t.Fatalf("expected pending vote without quorum: certificate=%v err=%v", certificate, err)
	}

	proposalEntered := make(chan struct{})
	voteEntered := make(chan struct{})
	release := make(chan struct{})
	var proposalOnce, voteOnce, releaseOnce sync.Once
	var proposalRequests, voteRequests atomic.Int32
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/consensus/proposals":
			proposalRequests.Add(1)
			proposalOnce.Do(func() { close(proposalEntered) })
		case "/v1/consensus/votes":
			voteRequests.Add(1)
			voteOnce.Do(func() { close(voteEntered) })
		default:
			http.NotFound(w, r)
			return
		}
		// The real, signed POST remains in flight until the test releases it
		// or its client cancels. No network timeout is relaxed.
		select {
		case <-release:
			w.WriteHeader(http.StatusAccepted)
		case <-r.Context().Done():
		}
	}))
	var healthy *httptest.Server
	t.Cleanup(func() {
		releaseOnce.Do(func() { close(release) })
		s.Close()
		if healthy != nil {
			healthy.Close()
		}
		peer.Close()
	})
	s.setPeerURLs([]string{peer.URL})
	// Do not depend on scheduler timing for the first delivery: the
	// automation tick itself must always return despite stalled HTTP.
	tickDone := make(chan error, 1)
	go func() { tickDone <- s.runConsensusAutomation() }()
	select {
	case err := <-tickDone:
		if err != nil {
			t.Fatalf("first automation tick failed: %v", err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("consensus automation tick blocked on outbound signed peer POST")
	}
	for _, entry := range []struct {
		name string
		ch   <-chan struct{}
	}{
		{name: "proposal", ch: proposalEntered},
		{name: "vote", ch: voteEntered},
	} {
		select {
		case <-entry.ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("no scheduled signed %s POST reached the stalled peer", entry.name)
		}
	}

	// Additional ticks cannot create unbounded goroutines or duplicate
	// in-flight POSTs. The WAL must not count a rejected single-flight
	// scheduling attempt as another completed replay.
	for i := 0; i < 8; i++ {
		if err := s.runConsensusAutomation(); err != nil {
			t.Fatalf("tick %d blocked or failed while HTTP stalled: %v", i, err)
		}
	}
	if got := proposalRequests.Load(); got != 1 {
		t.Fatalf("unbounded proposal POSTs while peer stalled: %d", got)
	}
	if got := voteRequests.Load(); got != 1 {
		t.Fatalf("unbounded vote POSTs while peer stalled: %d", got)
	}
	for _, action := range s.ledger.ConsensusRecovery().RecentActions {
		if action.Height == 1 && (action.Type == ledger.ConsensusActionProposal || action.Type == ledger.ConsensusActionVote) &&
			action.ReplayAttempts > 1 {
			t.Fatalf("duplicate WAL replay count under stalled network: %+v", action)
		}
	}

	// Adding a healthy destination while the original peer is still
	// blocked must deliver BOTH signed messages without waiting five
	// seconds. A single global flight per message type would strand it.
	healthyProposal := make(chan struct{})
	healthyVote := make(chan struct{})
	var healthyProposalOnce, healthyVoteOnce sync.Once
	healthy = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(sourceSignatureHeader) == "" {
			http.Error(w, "missing signed peer proof", http.StatusForbidden)
			return
		}
		switch r.URL.Path {
		case "/v1/consensus/proposals":
			healthyProposalOnce.Do(func() { close(healthyProposal) })
		case "/v1/consensus/votes":
			healthyVoteOnce.Do(func() { close(healthyVote) })
		default:
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	s.setPeerURLs([]string{peer.URL, healthy.URL})
	if err := s.runConsensusAutomation(); err != nil {
		t.Fatalf("healthy peer could not be scheduled alongside a stalled peer: %v", err)
	}
	for _, entry := range []struct {
		name string
		ch   <-chan struct{}
	}{
		{name: "proposal", ch: healthyProposal},
		{name: "vote", ch: healthyVote},
	} {
		select {
		case <-entry.ch:
		case <-time.After(750 * time.Millisecond):
			t.Fatalf("healthy peer starved behind stalled %s delivery", entry.name)
		}
	}

	// A 100ms round timeout must still progress while both previous
	// round deliveries are blocked. This asserts scheduler liveness, not
	// a QC or an unauthorized commit.
	time.Sleep(150 * time.Millisecond)
	roundDone := make(chan error, 1)
	go func() { roundDone <- s.runConsensusAutomation() }()
	select {
	case err := <-roundDone:
		if err != nil {
			t.Fatalf("round advancement failed: %v", err)
		}
	case <-time.After(750 * time.Millisecond):
		t.Fatal("stalled network prevented proposer rotation after timeout")
	}
	if view := s.ledger.Consensus(); view.CurrentRound == 0 || view.NextHeight != 1 {
		t.Fatalf("stalled peer prevented round advancement: %+v", view)
	}
	if s.ledger.Status().Height != 0 {
		t.Fatal("a stalled peer without quorum unexpectedly finalized a block")
	}

	releaseOnce.Do(func() { close(release) })
	waitFor(t, func() bool {
		return s.consensusOutbound.idle()
	})
}

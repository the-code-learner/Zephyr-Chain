package api

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/ledger"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// After the local node advances its round, the previous signed proposal
// and vote remain durably recorded for recovery. Replaying either old
// message on every automation tick cannot help an active-round quorum and
// may produce endless peer HTTP 409 responses and WAL replay writes.
// This deliberately runs with automation disabled so the round boundary
// is deterministic rather than relying on scheduler timing.
func TestConsensusAutomationOnlyRebroadcastsActiveRoundArtifacts(t *testing.T) {
	roundZeroSigner := newConsensusSigner(t)
	roundOneSigner := newConsensusSigner(t)
	server := newTestServer(t, Config{
		DataDir:                   t.TempDir(),
		NodeID:                    "round-rebroadcast",
		ValidatorPrivateKey:       encodedPrivateKey(t, roundZeroSigner.privateKey),
		BlockInterval:             0,
		SyncInterval:              0,
		EnablePeerSync:            false,
		EnableConsensusAutomation: false,
	})
	validators := []dpos.Validator{
		{Rank: 1, Address: roundZeroSigner.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: roundOneSigner.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	if _, err := server.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatal(err)
	}
	envelope := signedEnvelope(t, 25, 1, "stale-round-rebroadcast")
	if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatal(err)
	}
	root, err := server.ledger.ExpectedStateRoot([]tx.Envelope{envelope})
	if err != nil {
		t.Fatal(err)
	}
	proposal0 := signedConsensusProposal(t, roundZeroSigner, 1, 0, "", time.Now().UTC(), []tx.Envelope{envelope}, root)
	if err := server.ledger.RecordProposalWithAction(proposal0, &ledger.ConsensusAction{
		Type: ledger.ConsensusActionProposal, Height: proposal0.Height, Round: proposal0.Round,
		BlockHash: proposal0.BlockHash, Validator: proposal0.Proposer,
	}); err != nil {
		t.Fatalf("persist local round-zero proposal: %v", err)
	}
	vote0 := signedConsensusVote(t, roundZeroSigner, 1, 0, proposal0.BlockHash)
	if _, certificate, err := server.ledger.RecordVoteWithAction(vote0, &ledger.ConsensusAction{
		Type: ledger.ConsensusActionVote, Height: vote0.Height, Round: vote0.Round,
		BlockHash: vote0.BlockHash, Validator: vote0.Voter,
	}); err != nil || certificate != nil {
		t.Fatalf("one of two votes unexpectedly finalized or failed: certificate=%v err=%v", certificate, err)
	}
	replayCount := func(kind string, round uint64) int {
		t.Helper()
		for _, action := range server.ledger.ConsensusRecovery().RecentActions {
			if action.Type == kind && action.Height == 1 && action.Round == round {
				return action.ReplayAttempts
			}
		}
		t.Fatalf("missing durable %s action at round %d", kind, round)
		return -1
	}

	// With no peers, a retry call cannot deliver anything and must not
	// rewrite the WAL just to record a fictitious network replay.
	for i := 0; i < 4; i++ {
		server.maybeRebroadcastProposal()
		server.maybeRebroadcastVote()
	}
	if replayCount(ledger.ConsensusActionProposal, 0) != 0 ||
		replayCount(ledger.ConsensusActionVote, 0) != 0 {
		t.Fatal("peerless node rewrote WAL for a nonexistent replay")
	}

	// Adding a peer later must preserve current-round retry/recovery.
	// This ephemeral sink is not a production validator or trust proof.
	peer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	// Drain tracked outbound tasks before shutting down the synthetic peer.
	t.Cleanup(func() { server.Close(); peer.Close() })
	server.setPeerURLs([]string{peer.URL})
	server.maybeRebroadcastProposal()
	server.maybeRebroadcastVote()
	if replayCount(ledger.ConsensusActionProposal, 0) != 1 ||
		replayCount(ledger.ConsensusActionVote, 0) != 1 {
		t.Fatal("new peer did not schedule current-round proposal/vote replay")
	}
	waitFor(t, func() bool {
		return !server.consensusOutbound.proposal.Load() && !server.consensusOutbound.vote.Load()
	})

	// Removing peers again pauses replay bookkeeping without affecting
	// the signed actions already stored for crash recovery.
	server.setPeerURLs(nil)
	for i := 0; i < 4; i++ {
		server.maybeRebroadcastProposal()
		server.maybeRebroadcastVote()
	}
	if replayCount(ledger.ConsensusActionProposal, 0) != 1 ||
		replayCount(ledger.ConsensusActionVote, 0) != 1 {
		t.Fatal("removing peers did not stop unnecessary WAL replay updates")
	}
	server.setPeerURLs([]string{peer.URL})

	advanced, err := server.ledger.AdvanceRound(time.Now().UTC())
	if err != nil || advanced.Round != 1 {
		t.Fatalf("failed to advance to the next proposer: %+v %v", advanced, err)
	}
	for i := 0; i < 4; i++ {
		server.maybeRebroadcastProposal()
		server.maybeRebroadcastVote()
	}
	if replayCount(ledger.ConsensusActionProposal, 0) != 1 ||
		replayCount(ledger.ConsensusActionVote, 0) != 1 {
		t.Fatal("obsolete round-zero message replayed and rewrote WAL after round advancement")
	}

	// Once the rotated proposer publishes a fresh round-one candidate, the
	// local voter must still be able to retry its new-round vote.
	proposal1 := signedConsensusProposal(t, roundOneSigner, 1, 1, "", proposal0.ProducedAt, []tx.Envelope{envelope}, root)
	if err := server.ledger.RecordProposal(proposal1); err != nil {
		t.Fatalf("record rotated proposer candidate: %v", err)
	}
	vote1 := signedConsensusVote(t, roundZeroSigner, 1, 1, proposal1.BlockHash)
	if _, certificate, err := server.ledger.RecordVoteWithAction(vote1, &ledger.ConsensusAction{
		Type: ledger.ConsensusActionVote, Height: vote1.Height, Round: vote1.Round,
		BlockHash: vote1.BlockHash, Validator: vote1.Voter,
	}); err != nil || certificate != nil {
		t.Fatalf("record rotated round vote: certificate=%v err=%v", certificate, err)
	}
	server.maybeRebroadcastProposal()
	server.maybeRebroadcastVote()
	if replayCount(ledger.ConsensusActionVote, 1) != 1 ||
		replayCount(ledger.ConsensusActionProposal, 0) != 1 ||
		replayCount(ledger.ConsensusActionVote, 0) != 1 {
		t.Fatal("fresh round-one replay blocked, or obsolete round-zero replay resurrected")
	}
	if server.ledger.Status().Height != 0 {
		t.Fatal("replay without quorum unexpectedly committed a block")
	}
}

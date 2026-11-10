package api

import (
	"testing"
	"time"
)

// TestLabSevenValidatorsLastProposerNeedsFollowingSweepForQuorum
// reproduces a deterministic *scheduler-sweep boundary*, not a timeout.
// With seven validators at height 1, round 6 selects validator index 6.
// A lab sweep runs validators 0..6 sequentially; the first six have
// already ticked when the last proposer broadcasts its signed proposal.
// All peers can receive that proposal and the proposer's vote, yet the
// quorum only forms when validators 0..5 get their NEXT automation tick.
// This must never be misreported as a QC safety failure, and the test
// does not alter any production BFT timer or the CI benchmark deadline.
func TestLabSevenValidatorsLastProposerNeedsFollowingSweepForQuorum(t *testing.T) {
	cluster := newLabCluster(t, 7)
	defer cluster.Close()

	transactions := cluster.prepareTransactions(1)
	cluster.fundTransactions(transactions)
	cluster.submitTransactions(transactions, 1)
	cluster.waitForMempools(len(transactions), 3*time.Second)

	// Set an identical higher round on every persisted consensus store.
	// At height 1, round 6, the scheduled proposer is the last node
	// (1-1+6)%7 == 6; advancing is a legal lab setup action, not an
	// artificial sleep or a relaxation of the production round timeout.
	for index, node := range cluster.nodes {
		for round := uint64(0); round < 6; round++ {
			if state, err := node.server.ledger.AdvanceRound(time.Now().UTC()); err != nil {
				t.Fatalf("advance validator %d to round %d: %v", index, round+1, err)
			} else if state.Round != round+1 || state.Height != 1 {
				t.Fatalf("validator %d unexpected advanced state: %+v", index, state)
			}
		}
		view := node.server.ledger.Consensus()
		if view.NextProposer != cluster.nodes[6].signer.address || view.CurrentRound != 6 {
			t.Fatalf("validator %d inconsistent last-node proposer: %+v", index, view)
		}
	}

	// The first six validator automation ticks have no proposal to vote
	// on, because node 6 has not run. Their local signer journals must
	// not invent an early vote or a certificate.
	for index := 0; index < 6; index++ {
		node := cluster.nodes[index]
		if err := node.server.runConsensusAutomation(); err != nil {
			t.Fatalf("pre-proposal tick %d: %v", index, err)
		}
		if _, exists := node.server.ledger.ProposalAt(1, 6); exists {
			t.Fatalf("validator %d saw proposal before scheduled proposer", index)
		}
		if node.server.ledger.HasVote(1, 6, node.signer.address) {
			t.Fatalf("validator %d signed before receiving round 6 proposal", index)
		}
	}

	// The last tick publishes a proposal and its own signed vote. HTTP
	// peers store both immediately, but their own automation tick has
	// already happened, so each node MUST still lack local vote/QC.
	if err := cluster.nodes[6].server.runConsensusAutomation(); err != nil {
		t.Fatalf("last-proposer tick: %v", err)
	}
	for index, node := range cluster.nodes {
		proposal, exists := node.server.ledger.ProposalAt(1, 6)
		if !exists || proposal.Proposer != cluster.nodes[6].signer.address {
			t.Fatalf("node %d did not receive last-proposer signed proposal", index)
		}
		voted := node.server.ledger.HasVote(1, 6, node.signer.address)
		if voted != (index == 6) {
			t.Fatalf("node %d own vote before following sweep=%t", index, voted)
		}
		tallies := node.server.ledger.VoteTalliesAt(1, 6)
		if len(tallies) != 1 || tallies[0].VoteCount != 1 || tallies[0].QuorumReached {
			t.Fatalf("node %d did not observe proposer-only tally: %+v", index, tallies)
		}
		if _, exists := node.server.ledger.Certificate(1, 6, proposal.BlockHash); exists {
			t.Fatalf("node %d formed QC with only one signed vote", index)
		}
		if height := node.server.ledger.Status().Height; height != 0 {
			t.Fatalf("node %d finalized without a quorum: height=%d", index, height)
		}
	}

	// The next *ordinary* sequential sweep lets the other validators
	// vote. Signatures are transported and aggregated using the normal
	// handlers: no synthetic votes, forged QC, bypass or extra timeouts.
	for index, node := range cluster.nodes {
		if err := node.server.runConsensusAutomation(); err != nil {
			t.Fatalf("following-sweep tick %d: %v", index, err)
		}
	}
	for index, node := range cluster.nodes {
		if !node.server.ledger.HasVote(1, 6, node.signer.address) {
			t.Fatalf("validator %d did not sign after next sweep", index)
		}
	}
	cluster.assertSameTip(cluster.allIndices(), 1)
}

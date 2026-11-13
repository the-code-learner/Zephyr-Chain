package api

import (
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A proposer can have a valid signed candidate in round 0 with only 60%
// voting power, then the same candidate finalizes in round 1 with 100%.
// CertifiedBlockEvidenceAt must return the *certified* round, not the first
// matching partial fragment. Otherwise retries and multiheight recovery
// advertise a block with evidence the receiving peer must reject.
func TestCertifiedBlockEvidenceSelectsQuorumRoundNotFirstPartialRound(t *testing.T) {
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	source := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "evidence-round-source",
		ValidatorPrivateKey:   encodedPrivateKey(t, first.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	receiver := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "evidence-round-target",
		ValidatorPrivateKey:   encodedPrivateKey(t, second.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	validators := []dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}
	for _, server := range []*Server{source, receiver} {
		if _, err := server.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
			t.Fatalf("set validator set: %v", err)
		}
	}
	envelope := signedEnvelope(t, 25, 1, "same-candidate-two-rounds")
	for _, server := range []*Server{source, receiver} {
		if _, err := server.ledger.Credit(envelope.From, 100); err != nil {
			t.Fatalf("credit sender: %v", err)
		}
	}
	if _, err := source.ledger.Accept(envelope); err != nil {
		t.Fatalf("accept transaction: %v", err)
	}
	producedAt := time.Now().UTC()
	txns := []tx.Envelope{envelope}
	partial := signedConsensusProposalForServer(t, source, first, 1, 0, "", producedAt, txns)
	if err := source.ledger.RecordProposal(partial); err != nil {
		t.Fatalf("record partial round proposal: %v", err)
	}
	if _, _, err := source.ledger.RecordVote(signedConsensusVote(t, first, 1, 0, partial.BlockHash)); err != nil {
		t.Fatalf("record partial 60-power vote: %v", err)
	}
	committedProposal := signedConsensusProposalForServer(t, source, second, 1, 1, "", producedAt, txns)
	if partial.BlockHash != committedProposal.BlockHash {
		t.Fatalf("rounds must propose identical candidate, hashes %s vs %s", partial.BlockHash, committedProposal.BlockHash)
	}
	if err := source.ledger.RecordProposal(committedProposal); err != nil {
		t.Fatalf("record certified round proposal: %v", err)
	}
	for _, signer := range []consensusSigner{first, second} {
		if _, _, err := source.ledger.RecordVote(signedConsensusVote(t, signer, 1, 1, committedProposal.BlockHash)); err != nil {
			t.Fatalf("record certified quorum vote: %v", err)
		}
	}
	block, err := source.ledger.ProduceBlockWithOptions(10, producedAt, true)
	if err != nil {
		t.Fatalf("produce certified block: %v", err)
	}
	if block.Hash != committedProposal.BlockHash {
		t.Fatalf("committed different candidate: %s", block.Hash)
	}
	fragments := source.ledger.CertifiedBlockEvidenceFragments(1)
	if len(fragments) < 2 || fragments[0].Proposal.Round != 0 || len(fragments[0].Votes) != 1 {
		t.Fatalf("expected earlier partial round before committed quorum: %+v", fragments)
	}
	selected, ok := source.ledger.CertifiedBlockEvidenceAt(1)
	if !ok || selected.Proposal.Round != 1 || len(selected.Votes) != 2 {
		t.Fatalf("selected partial instead of certified quorum: %+v, ok=%v", selected, ok)
	}
	if err := receiver.ledger.ImportBlockWithEvidence(block, selected); err != nil {
		t.Fatalf("receiver failed to verify signed certified round: %v", err)
	}
	if receiver.ledger.Status().Height != 1 {
		t.Fatalf("receiver never advanced after signed quorum delivery")
	}
	if _, ok := receiver.ledger.Certificate(1, 1, block.Hash); !ok {
		t.Fatalf("receiver did not reconstruct certified round-1 quorum")
	}
	// A legitimately signed but insufficient earlier round must remain
	// unacceptable on a separate fresh receiver; no fallback by sender.
	fresh := newTestServer(t, Config{
		DataDir: t.TempDir(), NodeID: "evidence-round-negative",
		ValidatorPrivateKey:   encodedPrivateKey(t, second.privateKey),
		EnableBlockProduction: false, EnablePeerSync: false,
		BlockInterval: 0, SyncInterval: 0, MaxTransactionsPerBlock: 10,
		RequireConsensusCertificates: true,
	})
	if _, err := fresh.ledger.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.ledger.Credit(envelope.From, 100); err != nil {
		t.Fatal(err)
	}
	if err := fresh.ledger.ImportBlockWithEvidence(block, fragments[0]); err == nil {
		t.Fatal("insufficient signed round must be rejected")
	}
	if fresh.ledger.Status().Height != 0 {
		t.Fatal("partial evidence altered recipient ledger height")
	}
}

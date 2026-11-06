package ledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

// A certified block and its completed block_commit action must be persisted
// by ONE atomic state update: neither can survive a failed write alone.
func TestCertifiedCommitActionAtomicOnWriteFailureAndRestart(t *testing.T) {
	dir := t.TempDir()
	store, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	validator := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: validator.address, VotingPower: 100, SelfStake: 100},
	}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatal(err)
	}
	envelope := signedEnvelope(t, 25, 1, "certified-atomic-commit")
	if _, err := store.Credit(envelope.From, 100); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, time.March, 25, 10, 0, 0, 0, time.UTC)
	proposal := signedProposalForStore(t, store, validator, 1, 0, "", at, []tx.Envelope{envelope})
	if err := store.RecordProposalWithAction(proposal, &ConsensusAction{
		Type: ConsensusActionProposal, Height: proposal.Height, Round: proposal.Round,
		BlockHash: proposal.BlockHash, Validator: proposal.Proposer, RecordedAt: proposal.ProposedAt,
	}); err != nil {
		t.Fatal(err)
	}
	vote := signedVoteWithSigner(t, validator, 1, 0, proposal.BlockHash)
	if _, _, err := store.RecordVoteWithAction(vote, &ConsensusAction{
		Type: ConsensusActionVote, Height: vote.Height, Round: vote.Round,
		BlockHash: vote.BlockHash, Validator: vote.Voter, RecordedAt: vote.VotedAt,
	}); err != nil {
		t.Fatal(err)
	}
	countCommits := func(s *Store) int {
		t.Helper()
		count := 0
		for _, action := range s.ConsensusRecovery().RecentActions {
			if action.Type != ConsensusActionBlockCommit {
				continue
			}
			count++
			if action.BlockHash != proposal.BlockHash ||
				action.Validator != validator.address || action.Round != proposal.Round ||
				action.Height != proposal.Height || action.Status != ConsensusActionCompleted {
				t.Fatalf("invalid persisted certified commit action: %+v", action)
			}
		}
		return count
	}
	// A preexisting directory at the state-file destination forces the
	// durable temp-file rename to fail, without modifying the actual state.
	blockedPath := filepath.Join(t.TempDir(), "state-target-is-directory")
	if err := os.Mkdir(blockedPath, 0700); err != nil {
		t.Fatal(err)
	}
	originalPath := store.statePath
	store.statePath = blockedPath
	_, writeErr := store.ProduceBlockWithOptions(10, at, true)
	store.statePath = originalPath
	if writeErr == nil {
		t.Fatal("certified commit succeeded despite failed atomic file replacement")
	}
	if store.Status().Height != 0 || countCommits(store) != 0 {
		t.Fatal("failed disk write exposed a block or completed block_commit action in memory")
	}
	reopened, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.Status().Height != 0 || countCommits(reopened) != 0 {
		t.Fatal("failed disk write left committed block/action on disk after restart")
	}
	pending := reopened.ConsensusRecovery()
	if pending.PendingActionCount != 2 {
		t.Fatalf("failed commit lost pending proposal/vote needed for recovery: %+v", pending)
	}
	block, err := reopened.ProduceBlockWithOptions(10, at, true)
	if err != nil || block.Hash != proposal.BlockHash {
		t.Fatalf("certified commit after recovery failed: %v", err)
	}
	if reopened.Status().Height != 1 || countCommits(reopened) != 1 {
		t.Fatal("committed block and completed block_commit action not visible together")
	}
	afterRestart, err := NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	if afterRestart.Status().Height != 1 || countCommits(afterRestart) != 1 ||
		afterRestart.ConsensusRecovery().PendingActionCount != 0 {
		t.Fatal("successful certified block/action did not survive restart atomically")
	}
	if sender := afterRestart.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("certified transaction state was not durable: %+v", sender)
	}
}

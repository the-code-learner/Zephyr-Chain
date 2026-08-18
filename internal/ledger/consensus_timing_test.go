package ledger

import (
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestRecordProposalUsesLocalObservationTimeForRoundStart(t *testing.T) {
	store := newTestStore(t)
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	transaction := signedEnvelope(t, 1, 1, "timing")
	proposal := signedFundedProposalForStore(t, store, second, 1, 1, "", time.Now().UTC(), []tx.Envelope{transaction})
	proposal.ProposedAt = time.Now().UTC().Add(24 * time.Hour)

	before := time.Now().UTC()
	if err := store.RecordProposal(proposal); err != nil {
		t.Fatalf("record higher-round proposal: %v", err)
	}
	after := time.Now().UTC()

	consensus := store.Consensus()
	if consensus.CurrentRound != 1 {
		t.Fatalf("expected round 1, got %d", consensus.CurrentRound)
	}
	if consensus.CurrentRoundStartedAt == nil {
		t.Fatal("expected round start time")
	}
	if consensus.CurrentRoundStartedAt.Before(before) || consensus.CurrentRoundStartedAt.After(after) {
		t.Fatalf("expected local observation time between %s and %s, got %s", before, after, consensus.CurrentRoundStartedAt)
	}
	if consensus.CurrentRoundStartedAt.Equal(proposal.ProposedAt) {
		t.Fatal("unsigned proposal timestamp must not control the local round timer")
	}
}

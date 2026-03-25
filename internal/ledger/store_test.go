package ledger

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestStoreAcceptReservesBalanceAndAdvancesPendingNonce(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}

	id, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 40,
		Nonce:  1,
	})
	if err != nil {
		t.Fatalf("expected transaction to be accepted, got %v", err)
	}

	if id == "" {
		t.Fatal("expected transaction id to be generated")
	}

	view := store.View("zph_sender")
	if view.AvailableBalance != 60 {
		t.Fatalf("expected available balance 60, got %d", view.AvailableBalance)
	}
	if view.NextNonce != 2 {
		t.Fatalf("expected next nonce 2, got %d", view.NextNonce)
	}
	if view.PendingTransactions != 1 {
		t.Fatalf("expected 1 pending transaction, got %d", view.PendingTransactions)
	}
}

func TestStoreAcceptRejectsDuplicateTransactions(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}

	envelope := tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 10,
		Nonce:  1,
	}

	if _, err := store.Accept(envelope); err != nil {
		t.Fatalf("expected first transaction to be accepted, got %v", err)
	}
	if _, err := store.Accept(envelope); !errors.Is(err, ErrDuplicateTransaction) {
		t.Fatalf("expected duplicate transaction error, got %v", err)
	}
}

func TestStoreAcceptRejectsNonceGapsAndInsufficientBalance(t *testing.T) {
	store := newTestStore(t)
	if _, err := store.Credit("zph_sender", 50); err != nil {
		t.Fatalf("credit account: %v", err)
	}

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 10,
		Nonce:  2,
	}); !errors.Is(err, ErrInvalidNonce) {
		t.Fatalf("expected invalid nonce error, got %v", err)
	}

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 40,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("expected first valid transaction to be accepted, got %v", err)
	}

	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 20,
		Nonce:  2,
	}); !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("expected insufficient balance error, got %v", err)
	}
}

func TestStoreCreditWithIDIsIdempotent(t *testing.T) {
	store := newTestStore(t)

	first, err := store.CreditWithID("fund-1", "zph_sender", 100)
	if err != nil {
		t.Fatalf("first credit: %v", err)
	}
	second, err := store.CreditWithID("fund-1", "zph_sender", 100)
	if err != nil {
		t.Fatalf("second credit: %v", err)
	}

	if first.Balance != 100 || second.Balance != 100 {
		t.Fatalf("expected idempotent funding balance 100, got first=%d second=%d", first.Balance, second.Balance)
	}
	if store.View("zph_sender").Balance != 100 {
		t.Fatalf("expected stored balance 100, got %d", store.View("zph_sender").Balance)
	}
}

func TestStorePersistsMempoolAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 25,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("accept transaction: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	if reopened.MempoolSize() != 1 {
		t.Fatalf("expected mempool size 1 after restart, got %d", reopened.MempoolSize())
	}

	view := reopened.View("zph_sender")
	if view.AvailableBalance != 75 {
		t.Fatalf("expected available balance 75 after restart, got %d", view.AvailableBalance)
	}
	if view.NextNonce != 2 {
		t.Fatalf("expected next nonce 2 after restart, got %d", view.NextNonce)
	}
}

func TestStoreProduceBlockCommitsTransactionsAndPersistsState(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit account: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{
		From:   "zph_sender",
		To:     "zph_receiver",
		Amount: 25,
		Nonce:  1,
	}); err != nil {
		t.Fatalf("accept transaction: %v", err)
	}

	block, err := store.ProduceBlock(10)
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}

	if block.Height != 1 {
		t.Fatalf("expected block height 1, got %d", block.Height)
	}
	if block.TransactionCount != 1 {
		t.Fatalf("expected block transaction count 1, got %d", block.TransactionCount)
	}
	if store.MempoolSize() != 0 {
		t.Fatalf("expected empty mempool after block production, got %d", store.MempoolSize())
	}

	sender := store.View("zph_sender")
	if sender.Balance != 75 || sender.Nonce != 1 || sender.PendingTransactions != 0 {
		t.Fatalf("unexpected sender state after block commit: %+v", sender)
	}

	receiver := store.View("zph_receiver")
	if receiver.Balance != 25 {
		t.Fatalf("expected receiver balance 25, got %d", receiver.Balance)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	latest, ok := reopened.LatestBlock()
	if !ok {
		t.Fatal("expected latest block after restart")
	}
	if latest.Hash != block.Hash {
		t.Fatalf("expected persisted block hash %s, got %s", block.Hash, latest.Hash)
	}

	reopenedSender := reopened.View("zph_sender")
	if reopenedSender.Balance != 75 || reopenedSender.Nonce != 1 {
		t.Fatalf("unexpected reopened sender state: %+v", reopenedSender)
	}
}

func TestStoreImportBlockCommitsValidRemoteBlock(t *testing.T) {
	producer := newTestStore(t)
	envelope := signedEnvelope(t, 25, 1, "replicated")
	if _, err := producer.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatalf("accept producer tx: %v", err)
	}

	block, err := producer.ProduceBlock(10)
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}

	replica := newTestStore(t)
	if _, err := replica.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit replica sender: %v", err)
	}
	if err := replica.ImportBlock(block); err != nil {
		t.Fatalf("import block: %v", err)
	}

	if replica.Status().Height != 1 {
		t.Fatalf("expected replica height 1, got %d", replica.Status().Height)
	}
	view := replica.View(envelope.From)
	if view.Balance != 75 || view.Nonce != 1 {
		t.Fatalf("unexpected imported sender state: %+v", view)
	}
	if replica.View(envelope.To).Balance != 25 {
		t.Fatalf("expected receiver balance 25, got %d", replica.View(envelope.To).Balance)
	}
}

func TestStoreSetValidatorsPersistsAcrossRestartAndUpdatesConsensus(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	snapshot, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: "zph_validator_a", VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
		{Rank: 2, Address: "zph_validator_b", VotingPower: 30, SelfStake: 20, DelegatedStake: 10},
	}, dpos.ElectionConfig{MaxValidators: 2, MinSelfStake: 10_000, MaxMissedBlocks: 50})
	if err != nil {
		t.Fatalf("set validators: %v", err)
	}

	if snapshot.Version != 1 {
		t.Fatalf("expected validator snapshot version 1, got %d", snapshot.Version)
	}
	if len(snapshot.Validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(snapshot.Validators))
	}

	consensus := store.Consensus()
	if consensus.CurrentHeight != 0 || consensus.NextHeight != 1 {
		t.Fatalf("unexpected initial consensus heights: %+v", consensus)
	}
	if consensus.ValidatorCount != 2 {
		t.Fatalf("expected validator count 2, got %d", consensus.ValidatorCount)
	}
	if consensus.TotalVotingPower != 70 {
		t.Fatalf("expected total voting power 70, got %d", consensus.TotalVotingPower)
	}
	if consensus.QuorumVotingPower != 47 {
		t.Fatalf("expected quorum voting power 47, got %d", consensus.QuorumVotingPower)
	}
	if consensus.NextProposer != "zph_validator_a" {
		t.Fatalf("expected proposer zph_validator_a, got %s", consensus.NextProposer)
	}

	if _, err := store.Credit("zph_sender", 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := store.Accept(tx.Envelope{From: "zph_sender", To: "zph_receiver", Amount: 10, Nonce: 1}); err != nil {
		t.Fatalf("accept tx: %v", err)
	}
	if _, err := store.ProduceBlock(10); err != nil {
		t.Fatalf("produce block: %v", err)
	}

	consensus = store.Consensus()
	if consensus.CurrentHeight != 1 || consensus.NextHeight != 2 {
		t.Fatalf("unexpected post-block consensus heights: %+v", consensus)
	}
	if consensus.NextProposer != "zph_validator_b" {
		t.Fatalf("expected proposer zph_validator_b after one block, got %s", consensus.NextProposer)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	reopenedSnapshot := reopened.ValidatorSet()
	if reopenedSnapshot.Version != 1 {
		t.Fatalf("expected reopened validator snapshot version 1, got %d", reopenedSnapshot.Version)
	}
	if len(reopenedSnapshot.Validators) != 2 {
		t.Fatalf("expected reopened validator count 2, got %d", len(reopenedSnapshot.Validators))
	}
	if reopened.Consensus().NextProposer != "zph_validator_b" {
		t.Fatalf("expected reopened proposer zph_validator_b, got %s", reopened.Consensus().NextProposer)
	}
}

func TestStoreSnapshotRestoreRehydratesState(t *testing.T) {
	producer := newTestStore(t)
	envelope := signedEnvelope(t, 25, 1, "snapshot")
	if _, err := producer.CreditWithID("fund-restore", envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatalf("accept producer tx: %v", err)
	}
	if _, err := producer.SetValidators([]dpos.Validator{
		{Rank: 1, Address: "zph_validator_a", VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: "zph_validator_b", VotingPower: 30, SelfStake: 20, DelegatedStake: 10},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}
	block, err := producer.ProduceBlock(10)
	if err != nil {
		t.Fatalf("produce block: %v", err)
	}

	replica := newTestStore(t)
	if err := replica.Restore(producer.Snapshot()); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}

	latest, ok := replica.LatestBlock()
	if !ok {
		t.Fatal("expected latest block after restore")
	}
	if latest.Hash != block.Hash {
		t.Fatalf("expected restored block hash %s, got %s", block.Hash, latest.Hash)
	}
	if replica.View(envelope.From).Balance != 75 {
		t.Fatalf("expected restored sender balance 75, got %d", replica.View(envelope.From).Balance)
	}

	validatorSnapshot := replica.ValidatorSet()
	if validatorSnapshot.Version != 1 {
		t.Fatalf("expected restored validator snapshot version 1, got %d", validatorSnapshot.Version)
	}
	if len(validatorSnapshot.Validators) != 2 {
		t.Fatalf("expected restored validator count 2, got %d", len(validatorSnapshot.Validators))
	}
	if replica.Consensus().NextProposer != "zph_validator_b" {
		t.Fatalf("expected restored proposer zph_validator_b, got %s", replica.Consensus().NextProposer)
	}

	if _, err := replica.CreditWithID("fund-restore", envelope.From, 25); err != nil {
		t.Fatalf("replay funding id: %v", err)
	}
	if replica.View(envelope.From).Balance != 75 {
		t.Fatalf("expected replayed funding id to stay idempotent, got %d", replica.View(envelope.From).Balance)
	}
}

func TestStoreRecordProposalVotesAndCertificatePersistAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedProposalWithSigner(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "block-1-tx")})
	if err := store.RecordProposal(proposal); err != nil {
		t.Fatalf("record proposal: %v", err)
	}

	firstVote := signedVoteWithSigner(t, proposer, 1, 0, proposal.BlockHash)
	tally, certificate, err := store.RecordVote(firstVote)
	if err != nil {
		t.Fatalf("record first vote: %v", err)
	}
	if certificate != nil {
		t.Fatal("expected no certificate after first vote")
	}
	if tally.QuorumReached {
		t.Fatal("expected quorum to remain unmet after first vote")
	}

	secondVote := signedVoteWithSigner(t, voter, 1, 0, proposal.BlockHash)
	tally, certificate, err = store.RecordVote(secondVote)
	if err != nil {
		t.Fatalf("record second vote: %v", err)
	}
	if certificate == nil {
		t.Fatal("expected certificate after quorum vote")
	}
	if !tally.QuorumReached {
		t.Fatal("expected quorum after second vote")
	}

	artifacts := store.ConsensusArtifacts()
	if artifacts.ProposalCount != 1 || artifacts.VoteCount != 2 || artifacts.CertificateCount != 1 {
		t.Fatalf("unexpected consensus artifact counts: %+v", artifacts)
	}
	if artifacts.LatestCertificate == nil || artifacts.LatestCertificate.BlockHash != proposal.BlockHash {
		t.Fatalf("expected latest certificate for block %s, got %+v", proposal.BlockHash, artifacts.LatestCertificate)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if reopened.ConsensusArtifacts().LatestCertificate == nil {
		t.Fatal("expected certificate after reopen")
	}

	replica := newTestStore(t)
	if err := replica.Restore(store.Snapshot()); err != nil {
		t.Fatalf("restore snapshot: %v", err)
	}
	if replica.ConsensusArtifacts().LatestCertificate == nil {
		t.Fatal("expected certificate after snapshot restore")
	}
}

func TestStoreRecordProposalRejectsUnexpectedProposer(t *testing.T) {
	store := newTestStore(t)
	proposer := newConsensusSigner(t)
	other := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 70, SelfStake: 50, DelegatedStake: 20},
		{Rank: 2, Address: other.address, VotingPower: 30, SelfStake: 20, DelegatedStake: 10},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedProposalWithSigner(t, other, 1, 0, "", time.Date(2026, time.March, 23, 9, 15, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "wrong-proposer-tx")})
	if err := store.RecordProposal(proposal); !errors.Is(err, ErrUnexpectedProposer) {
		t.Fatalf("expected unexpected proposer error, got %v", err)
	}
}

func TestStoreRecordProposalRejectsMissingTransactions(t *testing.T) {
	store := newTestStore(t)
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	proposal := signedProposalWithSigner(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 45, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "missing-proposal-body")})
	proposal.Transactions = nil
	if err := store.RecordProposal(proposal); !errors.Is(err, consensus.ErrMissingTransactions) {
		t.Fatalf("expected missing transactions error, got %v", err)
	}
}

func TestStoreAdvanceRoundRotatesScheduledProposerAndPersists(t *testing.T) {
	store := newTestStore(t)
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	startedAt := time.Date(2026, time.March, 24, 12, 0, 0, 0, time.UTC)
	roundState, err := store.EnsureRoundStarted(startedAt)
	if err != nil {
		t.Fatalf("ensure round started: %v", err)
	}
	if roundState.Round != 0 || !roundState.StartedAt.Equal(startedAt) {
		t.Fatalf("unexpected initial round state: %+v", roundState)
	}
	if consensus := store.Consensus(); consensus.CurrentRound != 0 || consensus.NextProposer != first.address {
		t.Fatalf("unexpected initial consensus view: %+v", consensus)
	}

	advancedAt := startedAt.Add(5 * time.Second)
	roundState, err = store.AdvanceRound(advancedAt)
	if err != nil {
		t.Fatalf("advance round: %v", err)
	}
	if roundState.Round != 1 || !roundState.StartedAt.Equal(advancedAt) {
		t.Fatalf("unexpected advanced round state: %+v", roundState)
	}
	if consensus := store.Consensus(); consensus.CurrentRound != 1 || consensus.NextProposer != second.address {
		t.Fatalf("unexpected rotated consensus view: %+v", consensus)
	}

	reopened, err := NewStore(store.DataDir())
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}
	if consensus := reopened.Consensus(); consensus.CurrentRound != 1 || consensus.NextProposer != second.address {
		t.Fatalf("unexpected reopened consensus view: %+v", consensus)
	}
}

func TestStoreRecordProposalAcceptsHigherRoundAndRejectsStaleRound(t *testing.T) {
	store := newTestStore(t)
	first := newConsensusSigner(t)
	second := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: first.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: second.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	producedAt := time.Date(2026, time.March, 24, 12, 30, 0, 0, time.UTC)
	roundOneProposal := signedProposalWithSigner(t, second, 1, 1, "", producedAt, []tx.Envelope{signedEnvelope(t, 5, 1, "round-one-proposal")})
	if err := store.RecordProposal(roundOneProposal); err != nil {
		t.Fatalf("record round-one proposal: %v", err)
	}
	if consensus := store.Consensus(); consensus.CurrentRound != 1 || consensus.NextProposer != second.address {
		t.Fatalf("unexpected consensus view after round-one proposal: %+v", consensus)
	}

	staleProposal := signedProposalWithSigner(t, first, 1, 0, "", producedAt.Add(-time.Second), []tx.Envelope{signedEnvelope(t, 5, 1, "stale-round-proposal")})
	if err := store.RecordProposal(staleProposal); !errors.Is(err, ErrConsensusRoundMismatch) {
		t.Fatalf("expected stale round mismatch error, got %v", err)
	}
}
func TestStoreProduceBlockWithConsensusRequiresProposalAndCertificate(t *testing.T) {
	store := newTestStore(t)
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "consensus-gated-produce")
	if _, err := store.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	if _, err := store.Accept(envelope); err != nil {
		t.Fatalf("accept tx: %v", err)
	}

	producedAt := time.Date(2026, time.March, 23, 10, 0, 0, 0, time.UTC)
	template, err := store.BuildNextBlock(10, producedAt)
	if err != nil {
		t.Fatalf("build next block: %v", err)
	}

	if _, err := store.ProduceBlockWithOptions(10, producedAt, true); !errors.Is(err, ErrConsensusProposalRequired) {
		t.Fatalf("expected proposal required error, got %v", err)
	}

	proposal := signedProposalWithSigner(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions)
	if err := store.RecordProposal(proposal); err != nil {
		t.Fatalf("record proposal: %v", err)
	}

	if _, err := store.ProduceBlockWithOptions(10, producedAt, true); !errors.Is(err, ErrConsensusCertificateRequired) {
		t.Fatalf("expected certificate required error, got %v", err)
	}

	if _, _, err := store.RecordVote(signedVoteWithSigner(t, proposer, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record proposer vote: %v", err)
	}
	if _, _, err := store.RecordVote(signedVoteWithSigner(t, voter, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record voter vote: %v", err)
	}

	if _, err := store.ProduceBlockWithOptions(10, producedAt.Add(time.Second), true); !errors.Is(err, ErrConsensusTemplateMismatch) {
		t.Fatalf("expected template mismatch error for mismatched producedAt, got %v", err)
	}

	block, err := store.ProduceBlockWithOptions(10, producedAt, true)
	if err != nil {
		t.Fatalf("produce consensus-gated block: %v", err)
	}
	if block.Hash != template.Hash {
		t.Fatalf("expected produced block hash %s, got %s", template.Hash, block.Hash)
	}
	if store.Status().Height != 1 {
		t.Fatalf("expected committed height 1, got %d", store.Status().Height)
	}
}

func TestStoreProduceCertifiedBlockFromProposalBodyWithoutMempool(t *testing.T) {
	store := newTestStore(t)
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "proposal-body-only")
	if _, err := store.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	producedAt := time.Date(2026, time.March, 23, 10, 30, 0, 0, time.UTC)
	proposal := signedProposalWithSigner(t, proposer, 1, 0, "", producedAt, []tx.Envelope{envelope})
	if err := store.RecordProposal(proposal); err != nil {
		t.Fatalf("record proposal: %v", err)
	}
	if _, _, err := store.RecordVote(signedVoteWithSigner(t, proposer, 1, 0, proposal.BlockHash)); err != nil {
		t.Fatalf("record proposer vote: %v", err)
	}
	if _, _, err := store.RecordVote(signedVoteWithSigner(t, voter, 1, 0, proposal.BlockHash)); err != nil {
		t.Fatalf("record voter vote: %v", err)
	}
	if store.MempoolSize() != 0 {
		t.Fatalf("expected empty mempool before certified production, got %d", store.MempoolSize())
	}

	block, err := store.ProduceBlockWithOptions(10, producedAt, true)
	if err != nil {
		t.Fatalf("produce certified block from proposal body: %v", err)
	}
	if block.Hash != proposal.BlockHash {
		t.Fatalf("expected produced block hash %s, got %s", proposal.BlockHash, block.Hash)
	}
	if store.Status().Height != 1 {
		t.Fatalf("expected committed height 1, got %d", store.Status().Height)
	}
	if sender := store.View(envelope.From); sender.Balance != 75 || sender.Nonce != 1 {
		t.Fatalf("unexpected sender state after proposal-body commit: %+v", sender)
	}
}

func TestStoreImportBlockWithConsensusRequiresProposalAndCertificate(t *testing.T) {
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	validators := []dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}

	producer := newTestStore(t)
	if _, err := producer.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set producer validators: %v", err)
	}
	envelope := signedEnvelope(t, 25, 1, "consensus-gated-import")
	if _, err := producer.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatalf("accept producer tx: %v", err)
	}

	producedAt := time.Date(2026, time.March, 23, 11, 0, 0, 0, time.UTC)
	template, err := producer.BuildNextBlock(10, producedAt)
	if err != nil {
		t.Fatalf("build producer template: %v", err)
	}
	proposal := signedProposalWithSigner(t, proposer, template.Height, 0, template.PreviousHash, template.ProducedAt, template.Transactions)
	if err := producer.RecordProposal(proposal); err != nil {
		t.Fatalf("record producer proposal: %v", err)
	}
	if _, _, err := producer.RecordVote(signedVoteWithSigner(t, proposer, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer proposer vote: %v", err)
	}
	if _, _, err := producer.RecordVote(signedVoteWithSigner(t, voter, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record producer voter vote: %v", err)
	}

	block, err := producer.ProduceBlockWithOptions(10, producedAt, true)
	if err != nil {
		t.Fatalf("produce source block: %v", err)
	}

	replica := newTestStore(t)
	if _, err := replica.SetValidators(validators, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set replica validators: %v", err)
	}
	if _, err := replica.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit replica sender: %v", err)
	}

	if err := replica.ImportBlockWithOptions(block, true); !errors.Is(err, ErrConsensusProposalRequired) {
		t.Fatalf("expected proposal required error, got %v", err)
	}
	if err := replica.RecordProposal(proposal); err != nil {
		t.Fatalf("record replica proposal: %v", err)
	}
	if err := replica.ImportBlockWithOptions(block, true); !errors.Is(err, ErrConsensusCertificateRequired) {
		t.Fatalf("expected certificate required error, got %v", err)
	}
	if _, _, err := replica.RecordVote(signedVoteWithSigner(t, proposer, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record replica proposer vote: %v", err)
	}
	if _, _, err := replica.RecordVote(signedVoteWithSigner(t, voter, template.Height, 0, template.Hash)); err != nil {
		t.Fatalf("record replica voter vote: %v", err)
	}
	mismatchedBlock := block
	mismatchedBlock.ProducedAt = block.ProducedAt.Add(time.Second)
	mismatchedBlock.Hash = consensus.BlockHash(mismatchedBlock.Height, mismatchedBlock.PreviousHash, mismatchedBlock.ProducedAt, mismatchedBlock.TransactionIDs)
	if err := replica.ImportBlockWithOptions(mismatchedBlock, true); !errors.Is(err, ErrConsensusTemplateMismatch) {
		t.Fatalf("expected template mismatch error for mismatched import, got %v", err)
	}
	if err := replica.ImportBlockWithOptions(block, true); err != nil {
		t.Fatalf("import certified block: %v", err)
	}
	if replica.Status().Height != 1 {
		t.Fatalf("expected replica height 1, got %d", replica.Status().Height)
	}
}

func TestStoreSetValidatorsClearsPendingConsensusArtifacts(t *testing.T) {
	store := newTestStore(t)
	proposer := newConsensusSigner(t)
	voter := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: proposer.address, VotingPower: 60, SelfStake: 40, DelegatedStake: 20},
		{Rank: 2, Address: voter.address, VotingPower: 40, SelfStake: 25, DelegatedStake: 15},
	}, dpos.ElectionConfig{MaxValidators: 2}); err != nil {
		t.Fatalf("set initial validators: %v", err)
	}

	proposal := signedProposalWithSigner(t, proposer, 1, 0, "", time.Date(2026, time.March, 23, 9, 30, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "pending-artifacts-tx")})
	if err := store.RecordProposalWithAction(proposal, &ConsensusAction{
		Type:       ConsensusActionProposal,
		Height:     proposal.Height,
		Round:      proposal.Round,
		BlockHash:  proposal.BlockHash,
		Validator:  proposal.Proposer,
		RecordedAt: proposal.ProposedAt,
		Note:       "pending proposal before validator update",
	}); err != nil {
		t.Fatalf("record proposal: %v", err)
	}
	vote := signedVoteWithSigner(t, proposer, 1, 0, proposal.BlockHash)
	if _, _, err := store.RecordVoteWithAction(vote, &ConsensusAction{
		Type:       ConsensusActionVote,
		Height:     vote.Height,
		Round:      vote.Round,
		BlockHash:  vote.BlockHash,
		Validator:  vote.Voter,
		RecordedAt: vote.VotedAt,
		Note:       "pending vote before validator update",
	}); err != nil {
		t.Fatalf("record vote: %v", err)
	}
	if store.ConsensusArtifacts().ProposalCount == 0 {
		t.Fatal("expected pending artifacts before validator update")
	}

	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: voter.address, VotingPower: 100, SelfStake: 60, DelegatedStake: 40},
	}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatalf("set updated validators: %v", err)
	}

	artifacts := store.ConsensusArtifacts()
	if artifacts.ProposalCount != 0 || artifacts.VoteCount != 0 || artifacts.CertificateCount != 0 {
		t.Fatalf("expected consensus artifacts to reset after validator update, got %+v", artifacts)
	}
	if recovery := store.ConsensusRecovery(); recovery.PendingActionCount != 0 || recovery.NeedsReplay {
		t.Fatalf("expected consensus recovery to reset after validator update, got %+v", recovery)
	}
}

func TestStoreConsensusRecoveryPersistsAcrossRestartAndCompletesOnCommit(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	validator := newConsensusSigner(t)
	if _, err := store.SetValidators([]dpos.Validator{
		{Rank: 1, Address: validator.address, VotingPower: 100, SelfStake: 100},
	}, dpos.ElectionConfig{MaxValidators: 1}); err != nil {
		t.Fatalf("set validators: %v", err)
	}

	envelope := signedEnvelope(t, 25, 1, "consensus-recovery")
	if _, err := store.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit sender: %v", err)
	}
	producedAt := time.Date(2026, time.March, 24, 16, 0, 0, 0, time.UTC)
	proposal := signedProposalWithSigner(t, validator, 1, 0, "", producedAt, []tx.Envelope{envelope})
	if err := store.RecordProposalWithAction(proposal, &ConsensusAction{
		Type:       ConsensusActionProposal,
		Height:     proposal.Height,
		Round:      proposal.Round,
		BlockHash:  proposal.BlockHash,
		Validator:  proposal.Proposer,
		RecordedAt: proposal.ProposedAt,
		Note:       "test local proposal",
	}); err != nil {
		t.Fatalf("record proposal with action: %v", err)
	}
	vote := signedVoteWithSigner(t, validator, 1, 0, proposal.BlockHash)
	if _, _, err := store.RecordVoteWithAction(vote, &ConsensusAction{
		Type:       ConsensusActionVote,
		Height:     vote.Height,
		Round:      vote.Round,
		BlockHash:  vote.BlockHash,
		Validator:  vote.Voter,
		RecordedAt: vote.VotedAt,
		Note:       "test local vote",
	}); err != nil {
		t.Fatalf("record vote with action: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	recovery := reopened.ConsensusRecovery()
	if !recovery.NeedsReplay || !recovery.NeedsRecovery || recovery.PendingActionCount != 2 || recovery.PendingReplayCount != 2 || recovery.PendingImportCount != 0 {
		t.Fatalf("expected two pending consensus actions after restart, got %+v", recovery)
	}

	if _, err := reopened.ProduceBlockWithOptions(10, producedAt, true); err != nil {
		t.Fatalf("produce certified block after restart: %v", err)
	}

	recovery = reopened.ConsensusRecovery()
	if recovery.NeedsReplay || recovery.NeedsRecovery || recovery.PendingActionCount != 0 || recovery.PendingReplayCount != 0 || recovery.PendingImportCount != 0 {
		t.Fatalf("expected no pending consensus actions after commit, got %+v", recovery)
	}

	completed := 0
	for _, action := range recovery.RecentActions {
		if (action.Type == ConsensusActionProposal || action.Type == ConsensusActionVote) && action.Status == ConsensusActionCompleted {
			completed++
		}
	}
	if completed < 2 {
		t.Fatalf("expected completed proposal and vote actions after commit, got %+v", recovery.RecentActions)
	}
}

func TestStoreRestoreFromPeerSnapshotPreservesLocalRecoveryAndDiagnostics(t *testing.T) {
	producer := newTestStore(t)
	envelope := signedEnvelope(t, 25, 1, "peer-snapshot-recovery")
	if _, err := producer.Credit(envelope.From, 100); err != nil {
		t.Fatalf("credit producer sender: %v", err)
	}
	if _, err := producer.Accept(envelope); err != nil {
		t.Fatalf("accept producer transaction: %v", err)
	}
	block, err := producer.ProduceBlock(10)
	if err != nil {
		t.Fatalf("produce producer block: %v", err)
	}

	replica := newTestStore(t)
	if err := replica.RecordConsensusAction(ConsensusAction{
		Type:      ConsensusActionBlockImport,
		Height:    block.Height,
		BlockHash: block.Hash,
		Note:      "waiting for peer block import after proposal_required",
	}); err != nil {
		t.Fatalf("record pending import action: %v", err)
	}
	if err := replica.RecordConsensusDiagnostic(ConsensusDiagnostic{
		Kind:       "block_import_rejected",
		Code:       "proposal_required",
		Message:    ErrConsensusProposalRequired.Error(),
		Height:     block.Height,
		BlockHash:  block.Hash,
		Source:     "peer_sync",
		ObservedAt: time.Date(2026, time.March, 24, 17, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record diagnostic: %v", err)
	}
	if err := replica.RecordPeerSyncIncident(PeerSyncIncident{
		PeerURL:         "http://producer.example",
		State:           "unreachable",
		LocalHeight:     0,
		PeerHeight:      block.Height,
		HeightDelta:     int64(block.Height),
		ErrorMessage:    "dial tcp timeout",
		FirstObservedAt: time.Date(2026, time.March, 24, 17, 1, 0, 0, time.UTC),
		LastObservedAt:  time.Date(2026, time.March, 24, 17, 1, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("record peer sync incident: %v", err)
	}

	restoredAt := time.Date(2026, time.March, 24, 17, 5, 0, 0, time.UTC)
	if err := replica.RestoreFromPeerSnapshot(producer.Snapshot(), restoredAt); err != nil {
		t.Fatalf("restore from peer snapshot: %v", err)
	}

	latest, ok := replica.LatestBlock()
	if !ok || latest.Hash != block.Hash {
		t.Fatalf("expected restored latest block %s, got %+v", block.Hash, latest)
	}
	if replica.View(envelope.From).Balance != 75 {
		t.Fatalf("expected restored sender balance 75, got %d", replica.View(envelope.From).Balance)
	}

	recovery := replica.ConsensusRecovery()
	if recovery.NeedsReplay || recovery.NeedsRecovery || recovery.PendingActionCount != 0 || recovery.PendingReplayCount != 0 || recovery.PendingImportCount != 0 {
		t.Fatalf("expected completed recovery state after peer snapshot restore, got %+v", recovery)
	}
	completedImport := false
	for _, action := range recovery.RecentActions {
		if action.Type == ConsensusActionBlockImport && action.Status == ConsensusActionCompleted {
			completedImport = true
			break
		}
	}
	if !completedImport {
		t.Fatalf("expected completed block import recovery action after peer snapshot restore, got %+v", recovery.RecentActions)
	}

	diagnostics := replica.ConsensusDiagnostics()
	if len(diagnostics.Recent) == 0 {
		t.Fatal("expected preserved diagnostics after peer snapshot restore")
	}
	if diagnostics.Recent[0].Kind != "block_import_rejected" || diagnostics.Recent[0].Code != "proposal_required" || diagnostics.Recent[0].Source != "peer_sync" {
		t.Fatalf("unexpected diagnostics after peer snapshot restore: %+v", diagnostics.Recent)
	}

	history := replica.PeerSyncHistory()
	if len(history.Recent) == 0 {
		t.Fatal("expected preserved peer sync history after peer snapshot restore")
	}
	if history.Recent[0].PeerURL != "http://producer.example" || history.Recent[0].State != "unreachable" {
		t.Fatalf("unexpected peer sync history after peer snapshot restore: %+v", history.Recent)
	}

	summary := replica.PeerSyncSummary()
	if summary.IncidentCount != 1 || summary.AffectedPeerCount != 1 || summary.TotalOccurrences != 1 {
		t.Fatalf("unexpected peer sync summary after peer snapshot restore: %+v", summary)
	}
	if len(summary.Peers) != 1 || summary.Peers[0].PeerURL != "http://producer.example" || summary.Peers[0].LatestState != "unreachable" {
		t.Fatalf("unexpected peer sync peer summary after peer snapshot restore: %+v", summary.Peers)
	}
}

func TestStorePeerSyncHistoryPersistsAcrossRestartAndMergesRepeatedIncidents(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	firstObservedAt := time.Date(2026, time.March, 24, 19, 0, 0, 0, time.UTC)
	secondObservedAt := firstObservedAt.Add(45 * time.Second)
	thirdObservedAt := firstObservedAt.Add(90 * time.Second)
	if err := store.RecordPeerSyncIncident(PeerSyncIncident{
		PeerURL:         "http://peer-a.example",
		State:           "unreachable",
		LocalHeight:     3,
		PeerHeight:      1,
		HeightDelta:     -2,
		ErrorMessage:    "dial tcp timeout",
		FirstObservedAt: firstObservedAt,
		LastObservedAt:  firstObservedAt,
	}); err != nil {
		t.Fatalf("record first peer incident: %v", err)
	}
	if err := store.RecordPeerSyncIncident(PeerSyncIncident{
		PeerURL:         "http://peer-a.example",
		State:           "unreachable",
		LocalHeight:     3,
		PeerHeight:      1,
		HeightDelta:     -2,
		ErrorMessage:    "dial tcp timeout",
		FirstObservedAt: secondObservedAt,
		LastObservedAt:  secondObservedAt,
	}); err != nil {
		t.Fatalf("record repeated peer incident: %v", err)
	}
	if err := store.RecordPeerSyncIncident(PeerSyncIncident{
		PeerURL:         "http://peer-b.example",
		State:           "snapshot_restored",
		Reason:          "peer_diverged",
		LocalHeight:     1,
		PeerHeight:      1,
		HeightDelta:     0,
		BlockHash:       testHash("peer-sync-history"),
		FirstObservedAt: thirdObservedAt,
		LastObservedAt:  thirdObservedAt,
	}); err != nil {
		t.Fatalf("record snapshot peer incident: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	history := reopened.PeerSyncHistory()
	if len(history.Recent) != 2 {
		t.Fatalf("expected 2 recent peer incidents, got %+v", history.Recent)
	}
	if history.Recent[0].PeerURL != "http://peer-b.example" || history.Recent[0].State != "snapshot_restored" || history.Recent[0].Reason != "peer_diverged" {
		t.Fatalf("unexpected newest peer incident %+v", history.Recent[0])
	}

	peerA := reopened.PeerSyncIncidents("http://peer-a.example", 5)
	if len(peerA) != 1 {
		t.Fatalf("expected 1 merged peer-a incident, got %+v", peerA)
	}
	if peerA[0].Occurrences != 2 {
		t.Fatalf("expected merged occurrence count 2, got %+v", peerA[0])
	}
	if !peerA[0].FirstObservedAt.Equal(firstObservedAt) || !peerA[0].LastObservedAt.Equal(secondObservedAt) {
		t.Fatalf("unexpected merged incident timestamps %+v", peerA[0])
	}

	summary := reopened.PeerSyncSummary()
	if summary.IncidentCount != 2 || summary.AffectedPeerCount != 2 || summary.TotalOccurrences != 3 {
		t.Fatalf("unexpected peer sync summary %+v", summary)
	}
	if summary.LatestObservedAt == nil || !summary.LatestObservedAt.Equal(thirdObservedAt) {
		t.Fatalf("unexpected latest observed time %+v", summary)
	}
	if len(summary.States) != 2 {
		t.Fatalf("expected two state summaries, got %+v", summary.States)
	}
	if summary.States[0].State != "unreachable" || summary.States[0].IncidentCount != 1 || summary.States[0].AffectedPeerCount != 1 || summary.States[0].TotalOccurrences != 2 {
		t.Fatalf("unexpected unreachable state summary %+v", summary.States[0])
	}
	if len(summary.Reasons) != 2 || summary.Reasons[0].Reason != "unknown" || summary.Reasons[0].IncidentCount != 1 || summary.Reasons[0].AffectedPeerCount != 1 || summary.Reasons[0].TotalOccurrences != 2 || summary.Reasons[1].Reason != "peer_diverged" || summary.Reasons[1].TotalOccurrences != 1 {
		t.Fatalf("unexpected reason summaries %+v", summary.Reasons)
	}
	if len(summary.ErrorCodes) != 1 || summary.ErrorCodes[0].ErrorCode != "unknown" || summary.ErrorCodes[0].IncidentCount != 2 || summary.ErrorCodes[0].AffectedPeerCount != 2 || summary.ErrorCodes[0].TotalOccurrences != 3 {
		t.Fatalf("unexpected error code summaries %+v", summary.ErrorCodes)
	}
	if len(summary.Peers) != 2 {
		t.Fatalf("expected two peer summaries, got %+v", summary.Peers)
	}
	if summary.Peers[0].PeerURL != "http://peer-b.example" || summary.Peers[0].LatestState != "snapshot_restored" || summary.Peers[0].LatestReason != "peer_diverged" {
		t.Fatalf("unexpected latest peer summary %+v", summary.Peers[0])
	}
	peerASummary := reopened.PeerSyncPeerSummary("http://peer-a.example")
	if peerASummary.IncidentCount != 1 || peerASummary.TotalOccurrences != 2 || peerASummary.LatestState != "unreachable" {
		t.Fatalf("unexpected peer-a summary %+v", peerASummary)
	}
}

func TestStoreConsensusMetricsPersistAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	proposalRecordedAt := time.Date(2026, time.March, 24, 20, 30, 0, 0, time.UTC)
	replayedAt := proposalRecordedAt.Add(30 * time.Second)
	voteRecordedAt := proposalRecordedAt.Add(1 * time.Minute)
	roundAdvanceRecordedAt := proposalRecordedAt.Add(2 * time.Minute)
	proposalHash := testHash("metrics-proposal")
	voteHash := testHash("metrics-vote")
	if err := store.RecordConsensusAction(ConsensusAction{
		Type:       ConsensusActionProposal,
		Height:     3,
		Round:      0,
		BlockHash:  proposalHash,
		Validator:  "zph_validator_a",
		RecordedAt: proposalRecordedAt,
	}); err != nil {
		t.Fatalf("record proposal action: %v", err)
	}
	if err := store.MarkConsensusActionReplayed(ConsensusActionProposal, 3, 0, proposalHash, "zph_validator_a", replayedAt); err != nil {
		t.Fatalf("mark proposal replayed: %v", err)
	}
	if err := store.RecordConsensusAction(ConsensusAction{
		Type:       ConsensusActionVote,
		Height:     3,
		Round:      0,
		BlockHash:  voteHash,
		Validator:  "zph_validator_b",
		RecordedAt: voteRecordedAt,
	}); err != nil {
		t.Fatalf("record vote action: %v", err)
	}
	if err := store.RecordConsensusAction(ConsensusAction{
		Type:       ConsensusActionRoundAdvance,
		Height:     3,
		Round:      1,
		Validator:  "zph_validator_a",
		RecordedAt: roundAdvanceRecordedAt,
		Status:     ConsensusActionCompleted,
		Note:       "advanced after timeout",
	}); err != nil {
		t.Fatalf("record round-advance action: %v", err)
	}

	firstDiagnosticAt := proposalRecordedAt.Add(3 * time.Minute)
	secondDiagnosticAt := proposalRecordedAt.Add(4 * time.Minute)
	thirdDiagnosticAt := proposalRecordedAt.Add(5 * time.Minute)
	if err := store.RecordConsensusDiagnostic(ConsensusDiagnostic{
		Kind:       "proposal_rejected",
		Code:       "template_mismatch",
		Message:    "proposal template does not match local block template",
		Height:     3,
		Round:      0,
		BlockHash:  proposalHash,
		Validator:  "zph_validator_a",
		Source:     "api",
		ObservedAt: firstDiagnosticAt,
	}); err != nil {
		t.Fatalf("record first diagnostic: %v", err)
	}
	if err := store.RecordConsensusDiagnostic(ConsensusDiagnostic{
		Kind:       "block_import_rejected",
		Code:       "proposal_required",
		Message:    "consensus proposal is required before block import",
		Height:     3,
		Round:      0,
		BlockHash:  proposalHash,
		Validator:  "zph_validator_b",
		Source:     "peer_sync",
		ObservedAt: secondDiagnosticAt,
	}); err != nil {
		t.Fatalf("record second diagnostic: %v", err)
	}
	if err := store.RecordConsensusDiagnostic(ConsensusDiagnostic{
		Kind:       "block_import_rejected",
		Code:       "proposal_required",
		Message:    "consensus proposal is required before block import",
		Height:     4,
		Round:      1,
		BlockHash:  voteHash,
		Validator:  "zph_validator_c",
		Source:     "peer_sync",
		ObservedAt: thirdDiagnosticAt,
	}); err != nil {
		t.Fatalf("record third diagnostic: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	actionMetrics := reopened.ConsensusActionMetrics()
	if actionMetrics.TotalCount != 3 || actionMetrics.PendingCount != 2 || actionMetrics.TotalReplayAttempts != 1 {
		t.Fatalf("unexpected action metrics %+v", actionMetrics)
	}
	if actionMetrics.LatestRecordedAt == nil || !actionMetrics.LatestRecordedAt.Equal(roundAdvanceRecordedAt) {
		t.Fatalf("unexpected latest action recorded time %+v", actionMetrics)
	}
	if actionMetrics.LatestCompletedAt == nil || !actionMetrics.LatestCompletedAt.Equal(roundAdvanceRecordedAt) {
		t.Fatalf("unexpected latest action completed time %+v", actionMetrics)
	}
	actionTypes := make(map[string]int)
	for _, bucket := range actionMetrics.ByType {
		actionTypes[bucket.Label] = bucket.Count
	}
	if actionTypes[ConsensusActionProposal] != 1 || actionTypes[ConsensusActionVote] != 1 || actionTypes[ConsensusActionRoundAdvance] != 1 {
		t.Fatalf("unexpected action type buckets %+v", actionMetrics.ByType)
	}
	actionStatuses := make(map[string]int)
	for _, bucket := range actionMetrics.ByStatus {
		actionStatuses[bucket.Label] = bucket.Count
	}
	if actionStatuses[ConsensusActionPending] != 2 || actionStatuses[ConsensusActionCompleted] != 1 {
		t.Fatalf("unexpected action status buckets %+v", actionMetrics.ByStatus)
	}

	diagnosticMetrics := reopened.ConsensusDiagnosticMetrics()
	if diagnosticMetrics.TotalCount != 3 {
		t.Fatalf("unexpected diagnostic metrics %+v", diagnosticMetrics)
	}
	if diagnosticMetrics.LatestObservedAt == nil || !diagnosticMetrics.LatestObservedAt.Equal(thirdDiagnosticAt) {
		t.Fatalf("unexpected latest diagnostic time %+v", diagnosticMetrics)
	}
	diagnosticKinds := make(map[string]int)
	for _, bucket := range diagnosticMetrics.ByKind {
		diagnosticKinds[bucket.Label] = bucket.Count
	}
	if diagnosticKinds["proposal_rejected"] != 1 || diagnosticKinds["block_import_rejected"] != 2 {
		t.Fatalf("unexpected diagnostic kind buckets %+v", diagnosticMetrics.ByKind)
	}
	diagnosticCodes := make(map[string]int)
	for _, bucket := range diagnosticMetrics.ByCode {
		diagnosticCodes[bucket.Label] = bucket.Count
	}
	if diagnosticCodes["template_mismatch"] != 1 || diagnosticCodes["proposal_required"] != 2 {
		t.Fatalf("unexpected diagnostic code buckets %+v", diagnosticMetrics.ByCode)
	}
	diagnosticSources := make(map[string]int)
	for _, bucket := range diagnosticMetrics.BySource {
		diagnosticSources[bucket.Label] = bucket.Count
	}
	if diagnosticSources["api"] != 1 || diagnosticSources["peer_sync"] != 2 {
		t.Fatalf("unexpected diagnostic source buckets %+v", diagnosticMetrics.BySource)
	}
}

func TestStoreChainThroughputMetricsPersistAcrossRestart(t *testing.T) {
	dataDir := t.TempDir()
	store, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	olderProducedAt := time.Date(2026, time.March, 25, 11, 50, 0, 0, time.UTC)
	recentProducedAt := time.Date(2026, time.March, 25, 11, 59, 30, 0, time.UTC)
	now := time.Date(2026, time.March, 25, 12, 0, 0, 0, time.UTC)

	for index := 0; index < 10; index++ {
		envelope := signedEnvelope(t, 1, 1, "throughput-older")
		if _, err := store.Credit(envelope.From, 5); err != nil {
			t.Fatalf("credit older sender %d: %v", index, err)
		}
		if _, err := store.Accept(envelope); err != nil {
			t.Fatalf("accept older transaction %d: %v", index, err)
		}
	}
	if _, err := store.ProduceBlockWithOptions(100, olderProducedAt, false); err != nil {
		t.Fatalf("produce older throughput block: %v", err)
	}

	for index := 0; index < 60; index++ {
		envelope := signedEnvelope(t, 1, 1, "throughput-recent")
		if _, err := store.Credit(envelope.From, 5); err != nil {
			t.Fatalf("credit recent sender %d: %v", index, err)
		}
		if _, err := store.Accept(envelope); err != nil {
			t.Fatalf("accept recent transaction %d: %v", index, err)
		}
	}
	if _, err := store.ProduceBlockWithOptions(100, recentProducedAt, false); err != nil {
		t.Fatalf("produce recent throughput block: %v", err)
	}

	reopened, err := NewStore(dataDir)
	if err != nil {
		t.Fatalf("reopen store: %v", err)
	}

	metrics := reopened.ChainThroughputMetrics(now)
	if metrics.TotalBlockCount != 2 || metrics.TotalTransactionCount != 70 {
		t.Fatalf("unexpected total throughput metrics %+v", metrics)
	}
	if metrics.LatestBlockAt == nil || !metrics.LatestBlockAt.Equal(recentProducedAt) {
		t.Fatalf("unexpected latest throughput block time %+v", metrics)
	}
	if metrics.LatestBlockIntervalSeconds != 570 {
		t.Fatalf("unexpected latest throughput block interval %+v", metrics)
	}
	if len(metrics.Windows) != 3 {
		t.Fatalf("expected 3 throughput windows, got %+v", metrics.Windows)
	}
	windows := make(map[string]ChainThroughputWindowView, len(metrics.Windows))
	for _, window := range metrics.Windows {
		windows[window.Window] = window
	}
	if window := windows["1m"]; window.BlockCount != 1 || window.TransactionCount != 60 || window.TransactionsPerSecond != 1 || window.AverageTransactionsPerBlock != 60 {
		t.Fatalf("unexpected 1m throughput window %+v", window)
	}
	if window := windows["5m"]; window.BlockCount != 1 || window.TransactionCount != 60 || window.AverageTransactionsPerBlock != 60 {
		t.Fatalf("unexpected 5m throughput window %+v", window)
	}
	if window := windows["15m"]; window.BlockCount != 2 || window.TransactionCount != 70 || window.AverageTransactionsPerBlock != 35 {
		t.Fatalf("unexpected 15m throughput window %+v", window)
	}
}

func newTestStore(t *testing.T) *Store {
	t.Helper()

	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	return store
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

func signedProposalWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) consensus.Proposal {
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

func signedVoteWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string) consensus.Vote {
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

func testHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

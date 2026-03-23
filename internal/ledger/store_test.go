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

	proposal := signedProposalWithSigner(t, proposer, 1, 0, testHash("block-1"), "")
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

	proposal := signedProposalWithSigner(t, other, 1, 0, testHash("wrong-proposer"), "")
	if err := store.RecordProposal(proposal); !errors.Is(err, ErrUnexpectedProposer) {
		t.Fatalf("expected unexpected proposer error, got %v", err)
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

func signedProposalWithSigner(t *testing.T, signer consensusSigner, height uint64, round uint64, blockHash string, previousHash string) consensus.Proposal {
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

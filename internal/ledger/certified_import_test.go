package ledger

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

type certifiedImportSigner struct {
	privateKey *ecdsa.PrivateKey
	publicKey  string
	address    string
}

func TestImportBlockWithEvidenceRequiresSignedQuorum(t *testing.T) {
	validators := make([]dpos.Validator, 0, 7)
	signers := make(map[string]certifiedImportSigner, 7)
	for index := 0; index < 7; index++ {
		signer := newCertifiedImportSigner(t)
		signers[signer.address] = signer
		validators = append(validators, dpos.Validator{
			Rank:        index + 1,
			Address:     signer.address,
			VotingPower: 10_000,
			SelfStake:   10_000,
		})
	}
	config := dpos.ElectionConfig{MaxValidators: 7, MinSelfStake: 1, MaxMissedBlocks: 100}

	source, err := NewStoreWithChainID(t.TempDir(), protocol.DefaultChainID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.SetValidators(validators, config); err != nil {
		t.Fatal(err)
	}

	txSigner := newCertifiedImportSigner(t)
	envelope := tx.Envelope{
		ChainID:   protocol.DefaultChainID,
		Domain:    protocol.TransactionDomain,
		From:      txSigner.address,
		To:        "zph_certified_receiver",
		Amount:    1,
		Nonce:     1,
		Memo:      "certified catch-up",
		PublicKey: txSigner.publicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature, err = tx.SignPayload(txSigner.privateKey, envelope.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := source.Credit(envelope.From, 10); err != nil {
		t.Fatal(err)
	}
	if _, err := source.Accept(envelope); err != nil {
		t.Fatal(err)
	}

	producedAt := time.Now().UTC()
	candidate, err := source.BuildNextBlock(100, producedAt)
	if err != nil {
		t.Fatal(err)
	}
	view := source.Consensus()
	proposer, ok := signers[view.NextProposer]
	if !ok {
		t.Fatalf("scheduled proposer %s has no signer", view.NextProposer)
	}
	proposal := consensus.Proposal{
		ChainID:        protocol.DefaultChainID,
		Domain:         protocol.ConsensusProposalDomain,
		Height:         candidate.Height,
		Round:          view.CurrentRound,
		BlockHash:      candidate.Hash,
		PreviousHash:   candidate.PreviousHash,
		StateRoot:      candidate.StateRoot,
		ProducedAt:     candidate.ProducedAt,
		TransactionIDs: append([]string(nil), candidate.TransactionIDs...),
		Transactions:   append([]tx.Envelope(nil), candidate.Transactions...),
		Proposer:       proposer.address,
		PublicKey:      proposer.publicKey,
		ProposedAt:     time.Now().UTC(),
	}
	proposal.Payload = proposal.CanonicalPayload()
	proposal.Signature, err = tx.SignPayload(proposer.privateKey, proposal.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := source.RecordProposal(proposal); err != nil {
		t.Fatal(err)
	}

	for index := 0; index < 5; index++ {
		validator := validators[index]
		signer := signers[validator.Address]
		vote := consensus.Vote{
			ChainID:   protocol.DefaultChainID,
			Domain:    protocol.ConsensusVoteDomain,
			Height:    proposal.Height,
			Round:     proposal.Round,
			BlockHash: proposal.BlockHash,
			Voter:     signer.address,
			PublicKey: signer.publicKey,
			VotedAt:   time.Now().UTC(),
		}
		vote.Payload = vote.CanonicalPayload()
		vote.Signature, err = tx.SignPayload(signer.privateKey, vote.Payload)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := source.RecordVote(vote); err != nil {
			t.Fatal(err)
		}
	}

	block, err := source.ProduceBlockWithOptions(100, candidate.ProducedAt, true)
	if err != nil {
		t.Fatal(err)
	}
	evidence, ok := source.CertifiedBlockEvidenceAt(block.Height)
	if !ok {
		t.Fatal("expected certified evidence for committed block")
	}
	if len(evidence.Votes) < 5 {
		t.Fatalf("expected at least 5 signed votes, got %d", len(evidence.Votes))
	}

	newTarget := func() *Store {
		t.Helper()
		store, err := NewStoreWithChainID(t.TempDir(), protocol.DefaultChainID)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.SetValidators(validators, config); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Credit(envelope.From, 10); err != nil {
			t.Fatal(err)
		}
		if _, err := store.Accept(envelope); err != nil {
			t.Fatal(err)
		}
		return store
	}

	insufficient := newTarget()
	fourVotes := evidence
	fourVotes.Votes = append([]consensus.Vote(nil), evidence.Votes[:4]...)
	if err := insufficient.ImportBlockWithEvidence(block, fourVotes); !errors.Is(err, ErrCertifiedEvidenceQuorum) {
		t.Fatalf("expected insufficient evidence quorum, got %v", err)
	}
	if height := insufficient.Status().Height; height != 0 {
		t.Fatalf("insufficient evidence mutated target height to %d", height)
	}

	localPlusRemote := newTarget()
	if err := localPlusRemote.RecordProposal(proposal); err != nil {
		t.Fatalf("record local recovery proposal: %v", err)
	}
	if _, _, err := localPlusRemote.RecordVote(evidence.Votes[0]); err != nil {
		t.Fatalf("record local recovery vote: %v", err)
	}
	fourRemoteVotes := evidence
	fourRemoteVotes.Votes = append([]consensus.Vote(nil), evidence.Votes[1:5]...)
	if err := localPlusRemote.ImportBlockWithEvidence(block, fourRemoteVotes); err != nil {
		t.Fatalf("expected local vote plus four remote votes to reach quorum: %v", err)
	}
	if height := localPlusRemote.Status().Height; height != 1 {
		t.Fatalf("expected local plus remote evidence to import height 1, got %d", height)
	}

	tampered := newTarget()
	badEvidence := evidence
	badEvidence.Votes = append([]consensus.Vote(nil), evidence.Votes...)
	badEvidence.Votes[0].Signature = base64.StdEncoding.EncodeToString(make([]byte, 64))
	if err := tampered.ImportBlockWithEvidence(block, badEvidence); !errors.Is(err, ErrCertifiedEvidenceInvalid) {
		t.Fatalf("expected invalid evidence error, got %v", err)
	}
	if height := tampered.Status().Height; height != 0 {
		t.Fatalf("invalid evidence mutated target height to %d", height)
	}

	valid := newTarget()
	if err := valid.ImportBlockWithEvidence(block, evidence); err != nil {
		t.Fatalf("import certified block: %v", err)
	}
	if height := valid.Status().Height; height != 1 {
		t.Fatalf("expected imported height 1, got %d", height)
	}
	if latest, ok := valid.LatestBlock(); !ok || latest.Hash != block.Hash {
		t.Fatalf("expected imported block %s, got %+v", block.Hash, latest)
	}
}

func newCertifiedImportSigner(t *testing.T) certifiedImportSigner {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	publicBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	publicKey := base64.StdEncoding.EncodeToString(publicBytes)
	address, err := tx.DeriveAddressFromPublicKey(publicKey)
	if err != nil {
		t.Fatal(err)
	}
	if address == "" {
		t.Fatal(fmt.Errorf("derived empty validator address"))
	}
	return certifiedImportSigner{privateKey: privateKey, publicKey: publicKey, address: address}
}

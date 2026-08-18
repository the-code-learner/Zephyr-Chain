package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestProposalValidateStaticAcceptsValidSignedProposal(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Date(2026, time.March, 23, 12, 0, 0, 123000000, time.UTC), []tx.Envelope{
		signedEnvelope(t, 5, 1, "tx-1"),
		signedEnvelope(t, 7, 1, "tx-2"),
	})
	if err := proposal.ValidateStatic(); err != nil {
		t.Fatalf("expected valid proposal, got %v", err)
	}
}

func TestProposalValidateStaticRejectsAddressMismatch(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "tx-1")})
	proposal.Proposer = "zph_not_the_real_proposer"
	if err := proposal.ValidateStatic(); err != ErrInvalidAddress {
		t.Fatalf("expected invalid address error, got %v", err)
	}
}

func TestProposalValidateStaticRejectsMissingTransactions(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "tx-1")})
	proposal.Transactions = nil
	if err := proposal.ValidateStatic(); err != ErrMissingTransactions {
		t.Fatalf("expected missing transactions error, got %v", err)
	}
}

func TestProposalValidateStaticRejectsTransactionMismatch(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "tx-1")})
	proposal.Transactions = []tx.Envelope{signedEnvelope(t, 5, 1, "other")}
	if err := proposal.ValidateStatic(); err != ErrTransactionMismatch {
		t.Fatalf("expected transaction mismatch error, got %v", err)
	}
}

func TestProposalValidateStaticRejectsHashMismatch(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Date(2026, time.March, 23, 12, 0, 0, 0, time.UTC), []tx.Envelope{signedEnvelope(t, 5, 1, "tx-1")})
	proposal.BlockHash = testHash("different-block")
	if err := proposal.ValidateStatic(); err != ErrHashMismatch {
		t.Fatalf("expected hash mismatch error, got %v", err)
	}
}

func TestProposalRejectsCrossChainReplay(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-2"), time.Now().UTC(), []tx.Envelope{signedEnvelope(t, 5, 1, "tx-1")})
	if err := proposal.ValidateForChain("zephyr-testnet-1"); !errors.Is(err, ErrInvalidChainID) {
		t.Fatalf("expected cross-chain proposal rejection, got %v", err)
	}
}

func TestVoteValidateStaticAcceptsValidSignedVote(t *testing.T) {
	vote := signedVote(t, 3, 1, testHash("block-3"))
	if err := vote.ValidateStatic(); err != nil {
		t.Fatalf("expected valid vote, got %v", err)
	}
}

func TestVoteValidateStaticRejectsPayloadMismatch(t *testing.T) {
	vote := signedVote(t, 3, 1, testHash("block-3"))
	vote.Payload = "{}"
	if err := vote.ValidateStatic(); err != ErrInvalidPayload {
		t.Fatalf("expected invalid payload error, got %v", err)
	}
}

func TestVoteRejectsWrongSigningDomain(t *testing.T) {
	vote := signedVote(t, 3, 1, testHash("block-3"))
	vote.Domain = protocol.ConsensusProposalDomain
	vote.Payload = vote.CanonicalPayload()
	if err := vote.ValidateStatic(); !errors.Is(err, ErrInvalidDomain) {
		t.Fatalf("expected cross-domain vote rejection, got %v", err)
	}
}

func signedProposal(t *testing.T, height uint64, round uint64, previousHash string, producedAt time.Time, transactions []tx.Envelope) Proposal {
	t.Helper()
	privateKey, encodedPublicKey, address := newSigner(t)
	transactionIDs := make([]string, 0, len(transactions))
	for _, envelope := range transactions {
		transactionIDs = append(transactionIDs, tx.ID(envelope))
	}
	proposal := Proposal{
		ChainID:        protocol.DefaultChainID,
		Domain:         protocol.ConsensusProposalDomain,
		Height:         height,
		Round:          round,
		PreviousHash:   previousHash,
		StateRoot:      testHash("state-root"),
		ProducedAt:     producedAt,
		TransactionIDs: append([]string(nil), transactionIDs...),
		Transactions:   append([]tx.Envelope(nil), transactions...),
		Proposer:       address,
		PublicKey:      encodedPublicKey,
	}
	proposal.BlockHash = proposal.CandidateHash()
	proposal.Payload = proposal.CanonicalPayload()
	var err error
	proposal.Signature, err = tx.SignPayload(privateKey, proposal.Payload)
	if err != nil {
		t.Fatalf("sign proposal: %v", err)
	}
	return proposal
}

func signedVote(t *testing.T, height uint64, round uint64, blockHash string) Vote {
	t.Helper()
	privateKey, encodedPublicKey, address := newSigner(t)
	vote := Vote{
		ChainID:   protocol.DefaultChainID,
		Domain:    protocol.ConsensusVoteDomain,
		Height:    height,
		Round:     round,
		BlockHash: blockHash,
		Voter:     address,
		PublicKey: encodedPublicKey,
	}
	vote.Payload = vote.CanonicalPayload()
	var err error
	vote.Signature, err = tx.SignPayload(privateKey, vote.Payload)
	if err != nil {
		t.Fatalf("sign vote: %v", err)
	}
	return vote
}

func signedEnvelope(t *testing.T, amount uint64, nonce uint64, memo string) tx.Envelope {
	t.Helper()
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate transaction key: %v", err)
	}
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(&privateKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal transaction public key: %v", err)
	}
	encodedPublicKey := base64.StdEncoding.EncodeToString(publicKeyBytes)
	address, err := tx.DeriveAddressFromPublicKey(encodedPublicKey)
	if err != nil {
		t.Fatalf("derive transaction address: %v", err)
	}

	envelope := tx.Envelope{
		ChainID:   protocol.DefaultChainID,
		Domain:    protocol.TransactionDomain,
		From:      address,
		To:        "zph_receiver",
		Amount:    amount,
		Nonce:     nonce,
		Memo:      memo,
		PublicKey: encodedPublicKey,
	}
	envelope.Payload = envelope.CanonicalPayload()
	envelope.Signature, err = tx.SignPayload(privateKey, envelope.Payload)
	if err != nil {
		t.Fatalf("sign transaction: %v", err)
	}
	return envelope
}

func newSigner(t *testing.T) (*ecdsa.PrivateKey, string, string) {
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
	return privateKey, encodedPublicKey, address
}

func testHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

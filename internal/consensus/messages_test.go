package consensus

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"math/big"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

func TestProposalValidateStaticAcceptsValidSignedProposal(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-3"), testHash("block-2"))

	if err := proposal.ValidateStatic(); err != nil {
		t.Fatalf("expected valid proposal, got %v", err)
	}
}

func TestProposalValidateStaticRejectsAddressMismatch(t *testing.T) {
	proposal := signedProposal(t, 3, 1, testHash("block-3"), testHash("block-2"))
	proposal.Proposer = "zph_not_the_real_proposer"

	if err := proposal.ValidateStatic(); err != ErrInvalidAddress {
		t.Fatalf("expected invalid address error, got %v", err)
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

func signedProposal(t *testing.T, height uint64, round uint64, blockHash string, previousHash string) Proposal {
	t.Helper()

	privateKey, encodedPublicKey, address := newSigner(t)
	proposal := Proposal{
		Height:       height,
		Round:        round,
		BlockHash:    blockHash,
		PreviousHash: previousHash,
		Proposer:     address,
		PublicKey:    encodedPublicKey,
	}
	proposal.Payload = proposal.CanonicalPayload()
	proposal.Signature = signPayload(t, privateKey, proposal.Payload)
	return proposal
}

func signedVote(t *testing.T, height uint64, round uint64, blockHash string) Vote {
	t.Helper()

	privateKey, encodedPublicKey, address := newSigner(t)
	vote := Vote{
		Height:    height,
		Round:     round,
		BlockHash: blockHash,
		Voter:     address,
		PublicKey: encodedPublicKey,
	}
	vote.Payload = vote.CanonicalPayload()
	vote.Signature = signPayload(t, privateKey, vote.Payload)
	return vote
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

func testHash(seed string) string {
	sum := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(sum[:])
}

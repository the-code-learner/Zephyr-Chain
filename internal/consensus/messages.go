package consensus

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/protocol"
	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrMissingFields              = errors.New("missing required consensus fields")
	ErrInvalidPayload             = errors.New("payload does not match canonical consensus message")
	ErrInvalidPublicKey           = errors.New("invalid public key")
	ErrInvalidAddress             = errors.New("signer address does not match public key")
	ErrInvalidSignature           = errors.New("invalid signature")
	ErrInvalidChainID             = errors.New("consensus message chain ID does not match local chain")
	ErrInvalidDomain              = errors.New("invalid consensus signing domain")
	ErrInvalidHash                = errors.New("invalid block hash")
	ErrInvalidStateRoot           = errors.New("invalid state root")
	ErrInvalidHeight              = errors.New("height must be greater than zero")
	ErrInvalidProducedAt          = errors.New("producedAt must be set")
	ErrInvalidTransactionID       = errors.New("invalid transaction ID")
	ErrMissingTransactions        = errors.New("proposal must include transactions for each transaction ID")
	ErrInvalidProposalTransaction = errors.New("proposal contains an invalid transaction")
	ErrTransactionMismatch        = errors.New("proposal transactions do not match transaction IDs")
	ErrHashMismatch               = errors.New("block hash does not match proposal fields")
)

type Proposal struct {
	ChainID        string        `json:"chainId"`
	Domain         string        `json:"domain"`
	Height         uint64        `json:"height"`
	Round          uint64        `json:"round"`
	BlockHash      string        `json:"blockHash"`
	PreviousHash   string        `json:"previousHash"`
	StateRoot      string        `json:"stateRoot"`
	ProducedAt     time.Time     `json:"producedAt"`
	TransactionIDs []string      `json:"transactionIds"`
	Transactions   []tx.Envelope `json:"transactions"`
	Proposer       string        `json:"proposer"`
	Payload        string        `json:"payload"`
	PublicKey      string        `json:"publicKey"`
	Signature      string        `json:"signature"`
	ProposedAt     time.Time     `json:"proposedAt"`
}

type Vote struct {
	ChainID   string    `json:"chainId"`
	Domain    string    `json:"domain"`
	Height    uint64    `json:"height"`
	Round     uint64    `json:"round"`
	BlockHash string    `json:"blockHash"`
	Voter     string    `json:"voter"`
	Payload   string    `json:"payload"`
	PublicKey string    `json:"publicKey"`
	Signature string    `json:"signature"`
	VotedAt   time.Time `json:"votedAt"`
}

type canonicalProposal struct {
	BlockHash      string   `json:"blockHash"`
	ChainID        string   `json:"chainId"`
	Domain         string   `json:"domain"`
	Height         uint64   `json:"height"`
	PreviousHash   string   `json:"previousHash"`
	ProducedAt     string   `json:"producedAt"`
	Proposer       string   `json:"proposer"`
	Round          uint64   `json:"round"`
	StateRoot      string   `json:"stateRoot"`
	TransactionIDs []string `json:"transactionIds"`
}

type canonicalVote struct {
	BlockHash string `json:"blockHash"`
	ChainID   string `json:"chainId"`
	Domain    string `json:"domain"`
	Height    uint64 `json:"height"`
	Round     uint64 `json:"round"`
	Voter     string `json:"voter"`
}

type canonicalBlock struct {
	ChainID        string   `json:"chainId"`
	Domain         string   `json:"domain"`
	Height         uint64   `json:"height"`
	PreviousHash   string   `json:"previousHash"`
	ProducedAt     string   `json:"producedAt"`
	StateRoot      string   `json:"stateRoot"`
	TransactionIDs []string `json:"transactionIds"`
}

func BlockHash(chainID string, height uint64, previousHash string, producedAt time.Time, stateRoot string, transactionIDs []string) string {
	payload, _ := json.Marshal(canonicalBlock{
		ChainID:        strings.TrimSpace(chainID),
		Domain:         protocol.BlockDomain,
		Height:         height,
		PreviousHash:   previousHash,
		ProducedAt:     producedAt.UTC().Format(time.RFC3339Nano),
		StateRoot:      stateRoot,
		TransactionIDs: append([]string(nil), transactionIDs...),
	})

	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func (p Proposal) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalProposal{
		BlockHash:      p.BlockHash,
		ChainID:        strings.TrimSpace(p.ChainID),
		Domain:         strings.TrimSpace(p.Domain),
		Height:         p.Height,
		PreviousHash:   p.PreviousHash,
		ProducedAt:     p.ProducedAt.UTC().Format(time.RFC3339Nano),
		Proposer:       p.Proposer,
		Round:          p.Round,
		StateRoot:      p.StateRoot,
		TransactionIDs: append([]string(nil), p.TransactionIDs...),
	})

	return string(payload)
}

func (p Proposal) CandidateHash() string {
	return BlockHash(p.ChainID, p.Height, p.PreviousHash, p.ProducedAt, p.StateRoot, p.TransactionIDs)
}

func (p Proposal) ValidateStatic() error {
	chainID := strings.TrimSpace(p.ChainID)
	domain := strings.TrimSpace(p.Domain)
	if chainID == "" || domain == "" || p.BlockHash == "" || p.StateRoot == "" || p.Proposer == "" || p.Payload == "" || p.PublicKey == "" || p.Signature == "" || len(p.TransactionIDs) == 0 {
		return ErrMissingFields
	}
	if err := protocol.ValidateChainID(chainID); err != nil {
		return ErrInvalidChainID
	}
	if domain != protocol.ConsensusProposalDomain {
		return ErrInvalidDomain
	}
	if p.Height == 0 {
		return ErrInvalidHeight
	}
	if p.ProducedAt.IsZero() {
		return ErrInvalidProducedAt
	}
	if err := validateHash(p.BlockHash, false); err != nil {
		return err
	}
	if err := validateHash(p.StateRoot, false); err != nil {
		return ErrInvalidStateRoot
	}
	if err := validateHash(p.PreviousHash, true); err != nil {
		return err
	}
	if err := validateTransactionIDs(p.TransactionIDs); err != nil {
		return err
	}
	if err := validateProposalTransactions(chainID, p.TransactionIDs, p.Transactions); err != nil {
		return err
	}
	if p.BlockHash != p.CandidateHash() {
		return ErrHashMismatch
	}

	address, err := tx.DeriveAddressFromPublicKey(p.PublicKey)
	if err != nil {
		if errors.Is(err, tx.ErrInvalidPublicKey) {
			return ErrInvalidPublicKey
		}
		return err
	}
	if address != p.Proposer {
		return ErrInvalidAddress
	}
	if p.Payload != p.CanonicalPayload() {
		return ErrInvalidPayload
	}
	if err := tx.VerifySignature(p.PublicKey, p.Payload, p.Signature); err != nil {
		switch {
		case errors.Is(err, tx.ErrInvalidPublicKey):
			return ErrInvalidPublicKey
		case errors.Is(err, tx.ErrInvalidSignature), errors.Is(err, tx.ErrNonCanonicalSignature):
			return ErrInvalidSignature
		default:
			return err
		}
	}

	return nil
}

func (p Proposal) ValidateForChain(expectedChainID string) error {
	if err := p.ValidateStatic(); err != nil {
		return err
	}
	expectedChainID = strings.TrimSpace(expectedChainID)
	if err := protocol.ValidateChainID(expectedChainID); err != nil || strings.TrimSpace(p.ChainID) != expectedChainID {
		return ErrInvalidChainID
	}
	return nil
}

func (v Vote) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalVote{
		BlockHash: v.BlockHash,
		ChainID:   strings.TrimSpace(v.ChainID),
		Domain:    strings.TrimSpace(v.Domain),
		Height:    v.Height,
		Round:     v.Round,
		Voter:     v.Voter,
	})

	return string(payload)
}

func (v Vote) ValidateStatic() error {
	chainID := strings.TrimSpace(v.ChainID)
	domain := strings.TrimSpace(v.Domain)
	if chainID == "" || domain == "" || v.BlockHash == "" || v.Voter == "" || v.Payload == "" || v.PublicKey == "" || v.Signature == "" {
		return ErrMissingFields
	}
	if err := protocol.ValidateChainID(chainID); err != nil {
		return ErrInvalidChainID
	}
	if domain != protocol.ConsensusVoteDomain {
		return ErrInvalidDomain
	}
	if v.Height == 0 {
		return ErrInvalidHeight
	}
	if err := validateHash(v.BlockHash, false); err != nil {
		return err
	}

	address, err := tx.DeriveAddressFromPublicKey(v.PublicKey)
	if err != nil {
		if errors.Is(err, tx.ErrInvalidPublicKey) {
			return ErrInvalidPublicKey
		}
		return err
	}
	if address != v.Voter {
		return ErrInvalidAddress
	}
	if v.Payload != v.CanonicalPayload() {
		return ErrInvalidPayload
	}
	if err := tx.VerifySignature(v.PublicKey, v.Payload, v.Signature); err != nil {
		switch {
		case errors.Is(err, tx.ErrInvalidPublicKey):
			return ErrInvalidPublicKey
		case errors.Is(err, tx.ErrInvalidSignature), errors.Is(err, tx.ErrNonCanonicalSignature):
			return ErrInvalidSignature
		default:
			return err
		}
	}

	return nil
}

func (v Vote) ValidateForChain(expectedChainID string) error {
	if err := v.ValidateStatic(); err != nil {
		return err
	}
	expectedChainID = strings.TrimSpace(expectedChainID)
	if err := protocol.ValidateChainID(expectedChainID); err != nil || strings.TrimSpace(v.ChainID) != expectedChainID {
		return ErrInvalidChainID
	}
	return nil
}

func validateHash(value string, allowEmpty bool) error {
	value = strings.TrimSpace(value)
	if value == "" {
		if allowEmpty {
			return nil
		}
		return ErrInvalidHash
	}
	if len(value) != 64 {
		return ErrInvalidHash
	}
	if _, err := hex.DecodeString(value); err != nil {
		return ErrInvalidHash
	}
	return nil
}

func validateTransactionIDs(transactionIDs []string) error {
	seen := make(map[string]struct{}, len(transactionIDs))
	for _, id := range transactionIDs {
		if err := validateHash(id, false); err != nil {
			return ErrInvalidTransactionID
		}
		if _, exists := seen[id]; exists {
			return ErrInvalidTransactionID
		}
		seen[id] = struct{}{}
	}
	return nil
}

func validateProposalTransactions(chainID string, transactionIDs []string, transactions []tx.Envelope) error {
	if len(transactions) == 0 {
		return ErrMissingTransactions
	}
	if len(transactions) != len(transactionIDs) {
		return ErrTransactionMismatch
	}
	for index, envelope := range transactions {
		if err := envelope.ValidateForChain(chainID); err != nil {
			return ErrInvalidProposalTransaction
		}
		if tx.ID(envelope) != transactionIDs[index] {
			return ErrTransactionMismatch
		}
	}
	return nil
}

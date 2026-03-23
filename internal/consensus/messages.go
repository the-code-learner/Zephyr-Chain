package consensus

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/tx"
)

var (
	ErrMissingFields    = errors.New("missing required consensus fields")
	ErrInvalidPayload   = errors.New("payload does not match canonical consensus message")
	ErrInvalidPublicKey = errors.New("invalid public key")
	ErrInvalidAddress   = errors.New("signer address does not match public key")
	ErrInvalidSignature = errors.New("invalid signature")
	ErrInvalidHash      = errors.New("invalid block hash")
	ErrInvalidHeight    = errors.New("height must be greater than zero")
)

type Proposal struct {
	Height       uint64    `json:"height"`
	Round        uint64    `json:"round"`
	BlockHash    string    `json:"blockHash"`
	PreviousHash string    `json:"previousHash"`
	Proposer     string    `json:"proposer"`
	Payload      string    `json:"payload"`
	PublicKey    string    `json:"publicKey"`
	Signature    string    `json:"signature"`
	ProposedAt   time.Time `json:"proposedAt"`
}

type Vote struct {
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
	BlockHash    string `json:"blockHash"`
	Height       uint64 `json:"height"`
	PreviousHash string `json:"previousHash"`
	Proposer     string `json:"proposer"`
	Round        uint64 `json:"round"`
}

type canonicalVote struct {
	BlockHash string `json:"blockHash"`
	Height    uint64 `json:"height"`
	Round     uint64 `json:"round"`
	Voter     string `json:"voter"`
}

func (p Proposal) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalProposal{
		BlockHash:    p.BlockHash,
		Height:       p.Height,
		PreviousHash: p.PreviousHash,
		Proposer:     p.Proposer,
		Round:        p.Round,
	})

	return string(payload)
}

func (p Proposal) ValidateStatic() error {
	if p.Height == 0 {
		return ErrInvalidHeight
	}
	if p.BlockHash == "" || p.Proposer == "" || p.Payload == "" || p.PublicKey == "" || p.Signature == "" {
		return ErrMissingFields
	}
	if err := validateHash(p.BlockHash, false); err != nil {
		return err
	}
	if err := validateHash(p.PreviousHash, true); err != nil {
		return err
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
		case errors.Is(err, tx.ErrInvalidSignature):
			return ErrInvalidSignature
		default:
			return err
		}
	}

	return nil
}

func (v Vote) CanonicalPayload() string {
	payload, _ := json.Marshal(canonicalVote{
		BlockHash: v.BlockHash,
		Height:    v.Height,
		Round:     v.Round,
		Voter:     v.Voter,
	})

	return string(payload)
}

func (v Vote) ValidateStatic() error {
	if v.Height == 0 {
		return ErrInvalidHeight
	}
	if v.BlockHash == "" || v.Voter == "" || v.Payload == "" || v.PublicKey == "" || v.Signature == "" {
		return ErrMissingFields
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
		case errors.Is(err, tx.ErrInvalidSignature):
			return ErrInvalidSignature
		default:
			return err
		}
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

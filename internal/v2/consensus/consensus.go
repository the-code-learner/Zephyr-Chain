package consensus

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"errors"
	"math/big"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrValidatorSet      = errors.New("invalid v2 validator set")
	ErrProposal          = errors.New("invalid v2 consensus proposal")
	ErrVote              = errors.New("invalid v2 consensus vote")
	ErrCertificate       = errors.New("invalid v2 quorum certificate")
	ErrInsufficientPower = errors.New("insufficient v2 voting power")
)

type Validator struct {
	ID        types.ValidatorID
	PublicKey []byte
	Power     uint64
}

type ValidatorSet struct {
	Network    types.NetworkID
	Validators []Validator
}

type Proposal struct {
	Header    sharding.GlobalHeader
	Round     uint64
	Proposer  types.ValidatorID
	PublicKey []byte
	Signature []byte
}

type Vote struct {
	Network    types.NetworkID
	Height     uint64
	Round      uint64
	HeaderHash types.Hash
	Voter      types.ValidatorID
	PublicKey  []byte
	Signature  []byte
}

type Certificate struct {
	Network    types.NetworkID
	Height     uint64
	Round      uint64
	HeaderHash types.Hash
	Votes      []Vote
}

func (s ValidatorSet) Validate() error {
	if types.IsZero32([32]byte(s.Network)) || len(s.Validators) == 0 {
		return ErrValidatorSet
	}
	seen := make(map[types.ValidatorID]struct{}, len(s.Validators))
	var total uint64
	for _, validator := range s.Validators {
		if validator.Power == 0 || len(validator.PublicKey) != 65 || types.ValidatorIDFromPublicKey(validator.PublicKey) != validator.ID {
			return ErrValidatorSet
		}
		x, y := elliptic.Unmarshal(elliptic.P256(), validator.PublicKey)
		if x == nil || y == nil {
			return ErrValidatorSet
		}
		if _, duplicate := seen[validator.ID]; duplicate {
			return ErrValidatorSet
		}
		seen[validator.ID] = struct{}{}
		if ^uint64(0)-total < validator.Power {
			return ErrValidatorSet
		}
		total += validator.Power
	}
	return nil
}

func (s ValidatorSet) TotalPower() (uint64, error) {
	if err := s.Validate(); err != nil {
		return 0, err
	}
	var total uint64
	for _, validator := range s.Validators {
		total += validator.Power
	}
	return total, nil
}

func QuorumPower(total uint64) uint64 {
	if total == 0 {
		return 0
	}
	q, r := total/3, total%3
	return q*2 + (r*2)/3 + 1
}

func (s ValidatorSet) Proposer(height, round uint64) (Validator, error) {
	total, err := s.TotalPower()
	if err != nil || height == 0 {
		return Validator{}, ErrValidatorSet
	}
	validators := append([]Validator(nil), s.Validators...)
	sort.Slice(validators, func(i, j int) bool { return bytes.Compare(validators[i].ID[:], validators[j].ID[:]) < 0 })
	slot := ((height - 1) % total + (round % total)) % total
	var cumulative uint64
	for _, validator := range validators {
		cumulative += validator.Power
		if slot < cumulative {
			return validator, nil
		}
	}
	return Validator{}, ErrValidatorSet
}

func HeaderConsensusHash(header sharding.GlobalHeader) types.Hash {
	copyHeader := header
	copyHeader.CertificateHash = types.Hash{}
	return types.Hash(codec.DomainHash("zephyr/global-header-consensus/v2", copyHeader.CanonicalBytes()))
}

func (p Proposal) SigningDigest() types.Hash {
	var w codec.Writer
	headerHash := HeaderConsensusHash(p.Header)
	w.Fixed(headerHash[:])
	w.U64(p.Round)
	w.Fixed(p.Proposer[:])
	return types.Hash(codec.DomainHash("zephyr/consensus/proposal/v2", w.BytesCopy()))
}

func (v Vote) SigningDigest() types.Hash {
	var w codec.Writer
	w.Fixed(v.Network[:])
	w.U64(v.Height)
	w.U64(v.Round)
	w.Fixed(v.HeaderHash[:])
	w.Fixed(v.Voter[:])
	return types.Hash(codec.DomainHash("zephyr/consensus/vote/v2", w.BytesCopy()))
}

func SignProposal(privateKey *ecdsa.PrivateKey, header sharding.GlobalHeader, round uint64) (Proposal, error) {
	publicKey, validatorID, err := signerIdentity(privateKey)
	if err != nil {
		return Proposal{}, err
	}
	proposal := Proposal{Header: header, Round: round, Proposer: validatorID, PublicKey: publicKey}
	proposal.Signature, err = signDigest(privateKey, proposal.SigningDigest())
	return proposal, err
}

func SignVote(privateKey *ecdsa.PrivateKey, network types.NetworkID, height, round uint64, headerHash types.Hash) (Vote, error) {
	publicKey, validatorID, err := signerIdentity(privateKey)
	if err != nil {
		return Vote{}, err
	}
	vote := Vote{Network: network, Height: height, Round: round, HeaderHash: headerHash, Voter: validatorID, PublicKey: publicKey}
	vote.Signature, err = signDigest(privateKey, vote.SigningDigest())
	return vote, err
}

func (s ValidatorSet) VerifyProposal(proposal Proposal) error {
	if err := s.Validate(); err != nil || proposal.Header.Network != s.Network || proposal.Header.Height == 0 || proposal.Header.CertificateHash != (types.Hash{}) {
		return ErrProposal
	}
	expected, err := s.Proposer(proposal.Header.Height, proposal.Round)
	if err != nil || expected.ID != proposal.Proposer || !bytes.Equal(expected.PublicKey, proposal.PublicKey) {
		return ErrProposal
	}
	if types.ValidatorIDFromPublicKey(proposal.PublicKey) != proposal.Proposer || verifyDigest(proposal.PublicKey, proposal.SigningDigest(), proposal.Signature) != nil {
		return ErrProposal
	}
	return nil
}

func (s ValidatorSet) VerifyVote(vote Vote) error {
	if vote.Network != s.Network || vote.Height == 0 || types.IsZero32([32]byte(vote.HeaderHash)) || types.ValidatorIDFromPublicKey(vote.PublicKey) != vote.Voter {
		return ErrVote
	}
	validator, ok := s.validator(vote.Voter)
	if !ok || !bytes.Equal(validator.PublicKey, vote.PublicKey) || verifyDigest(vote.PublicKey, vote.SigningDigest(), vote.Signature) != nil {
		return ErrVote
	}
	return nil
}

func (s ValidatorSet) BuildCertificate(proposal Proposal, votes []Vote) (Certificate, error) {
	if err := s.VerifyProposal(proposal); err != nil {
		return Certificate{}, err
	}
	certificate := Certificate{Network: s.Network, Height: proposal.Header.Height, Round: proposal.Round, HeaderHash: HeaderConsensusHash(proposal.Header), Votes: append([]Vote(nil), votes...)}
	if err := s.VerifyCertificate(certificate); err != nil {
		return Certificate{}, err
	}
	return certificate, nil
}

func (s ValidatorSet) VerifyCertificate(certificate Certificate) error {
	if err := s.Validate(); err != nil || certificate.Network != s.Network || certificate.Height == 0 || types.IsZero32([32]byte(certificate.HeaderHash)) {
		return ErrCertificate
	}
	total, _ := s.TotalPower()
	quorum := QuorumPower(total)
	seen := make(map[types.ValidatorID]struct{}, len(certificate.Votes))
	var power uint64
	for _, vote := range certificate.Votes {
		if vote.Network != certificate.Network || vote.Height != certificate.Height || vote.Round != certificate.Round || vote.HeaderHash != certificate.HeaderHash {
			return ErrCertificate
		}
		if _, duplicate := seen[vote.Voter]; duplicate {
			return ErrCertificate
		}
		seen[vote.Voter] = struct{}{}
		if err := s.VerifyVote(vote); err != nil {
			return ErrCertificate
		}
		validator, _ := s.validator(vote.Voter)
		power += validator.Power
	}
	if power < quorum {
		return ErrInsufficientPower
	}
	return nil
}

func (c Certificate) Hash() types.Hash {
	votes := append([]Vote(nil), c.Votes...)
	sort.Slice(votes, func(i, j int) bool { return bytes.Compare(votes[i].Voter[:], votes[j].Voter[:]) < 0 })
	var w codec.Writer
	w.Fixed(c.Network[:])
	w.U64(c.Height)
	w.U64(c.Round)
	w.Fixed(c.HeaderHash[:])
	w.U32(uint32(len(votes)))
	for _, vote := range votes {
		w.Fixed(vote.Voter[:])
		w.Bytes(vote.PublicKey)
		w.Bytes(vote.Signature)
	}
	return types.Hash(codec.DomainHash("zephyr/quorum-certificate/v2", w.BytesCopy()))
}

func (s ValidatorSet) validator(id types.ValidatorID) (Validator, bool) {
	for _, validator := range s.Validators {
		if validator.ID == id {
			return validator, true
		}
	}
	return Validator{}, false
}

func signerIdentity(privateKey *ecdsa.PrivateKey) ([]byte, types.ValidatorID, error) {
	if privateKey == nil || privateKey.Curve != elliptic.P256() {
		return nil, types.ValidatorID{}, ErrValidatorSet
	}
	publicKey := elliptic.Marshal(elliptic.P256(), privateKey.PublicKey.X, privateKey.PublicKey.Y)
	return publicKey, types.ValidatorIDFromPublicKey(publicKey), nil
}

func signDigest(privateKey *ecdsa.PrivateKey, digest types.Hash) ([]byte, error) {
	r, s, err := ecdsa.Sign(rand.Reader, privateKey, digest[:])
	if err != nil {
		return nil, err
	}
	if s.Cmp(halfOrder()) > 0 {
		s = new(big.Int).Sub(elliptic.P256().Params().N, s)
	}
	return append(pad32(r), pad32(s)...), nil
}

func verifyDigest(publicKey []byte, digest types.Hash, signature []byte) error {
	if len(publicKey) != 65 || len(signature) != 64 {
		return ErrVote
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return ErrVote
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	order := elliptic.P256().Params().N
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(order) >= 0 || s.Cmp(order) >= 0 || s.Cmp(halfOrder()) > 0 {
		return ErrVote
	}
	if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:], r, s) {
		return ErrVote
	}
	return nil
}

func halfOrder() *big.Int {
	return new(big.Int).Rsh(new(big.Int).Set(elliptic.P256().Params().N), 1)
}

func pad32(value *big.Int) []byte {
	raw := value.Bytes()
	out := make([]byte, 32)
	if len(raw) >= 32 {
		copy(out, raw[len(raw)-32:])
	} else {
		copy(out[32-len(raw):], raw)
	}
	return out
}

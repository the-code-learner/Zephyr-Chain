package consensus

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const MaxCertificateVotes = 4096

func (p Proposal) MarshalBinary() ([]byte, error) {
	if len(p.PublicKey) != 65 || len(p.Signature) != 64 || types.IsZero32([32]byte(p.Proposer)) {
		return nil, ErrProposal
	}
	var w codec.Writer
	w.Bytes(p.Header.CanonicalBytes())
	w.U64(p.Round)
	w.Fixed(p.Proposer[:])
	w.Bytes(p.PublicKey)
	w.Bytes(p.Signature)
	return w.BytesCopy(), nil
}

func ParseProposal(data []byte) (Proposal, error) {
	r := codec.NewReader(data)
	headerBytes, err := r.Bytes(512)
	if err != nil {
		return Proposal{}, ErrProposal
	}
	header, err := sharding.ParseGlobalHeader(headerBytes)
	if err != nil {
		return Proposal{}, ErrProposal
	}
	round, err := r.U64()
	if err != nil {
		return Proposal{}, ErrProposal
	}
	proposerBytes, err := r.Fixed(32)
	if err != nil {
		return Proposal{}, ErrProposal
	}
	publicKey, err := r.Bytes(65)
	if err != nil || len(publicKey) != 65 {
		return Proposal{}, ErrProposal
	}
	signature, err := r.Bytes(64)
	if err != nil || len(signature) != 64 || r.Done() != nil {
		return Proposal{}, ErrProposal
	}
	var proposer types.ValidatorID
	copy(proposer[:], proposerBytes)
	return Proposal{Header: header, Round: round, Proposer: proposer, PublicKey: publicKey, Signature: signature}, nil
}

func (v Vote) MarshalBinary() ([]byte, error) {
	if len(v.PublicKey) != 65 || len(v.Signature) != 64 || v.Height == 0 || types.IsZero32([32]byte(v.Network)) || types.IsZero32([32]byte(v.HeaderHash)) || types.IsZero32([32]byte(v.Voter)) {
		return nil, ErrVote
	}
	var w codec.Writer
	w.Fixed(v.Network[:])
	w.U64(v.Height)
	w.U64(v.Round)
	w.Fixed(v.HeaderHash[:])
	w.Fixed(v.Voter[:])
	w.Bytes(v.PublicKey)
	w.Bytes(v.Signature)
	return w.BytesCopy(), nil
}

func ParseVote(data []byte) (Vote, error) {
	r := codec.NewReader(data)
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return Vote{}, ErrVote
	}
	height, err := r.U64()
	if err != nil || height == 0 {
		return Vote{}, ErrVote
	}
	round, err := r.U64()
	if err != nil {
		return Vote{}, ErrVote
	}
	headerBytes, err := r.Fixed(32)
	if err != nil {
		return Vote{}, ErrVote
	}
	voterBytes, err := r.Fixed(32)
	if err != nil {
		return Vote{}, ErrVote
	}
	publicKey, err := r.Bytes(65)
	if err != nil || len(publicKey) != 65 {
		return Vote{}, ErrVote
	}
	signature, err := r.Bytes(64)
	if err != nil || len(signature) != 64 || r.Done() != nil {
		return Vote{}, ErrVote
	}
	var network types.NetworkID
	var headerHash types.Hash
	var voter types.ValidatorID
	copy(network[:], networkBytes)
	copy(headerHash[:], headerBytes)
	copy(voter[:], voterBytes)
	if types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(headerHash)) || types.IsZero32([32]byte(voter)) {
		return Vote{}, ErrVote
	}
	return Vote{Network: network, Height: height, Round: round, HeaderHash: headerHash, Voter: voter, PublicKey: publicKey, Signature: signature}, nil
}

func (c Certificate) MarshalBinary() ([]byte, error) {
	if len(c.Votes) == 0 || len(c.Votes) > MaxCertificateVotes || c.Height == 0 || types.IsZero32([32]byte(c.Network)) || types.IsZero32([32]byte(c.HeaderHash)) {
		return nil, ErrCertificate
	}
	var w codec.Writer
	w.Fixed(c.Network[:])
	w.U64(c.Height)
	w.U64(c.Round)
	w.Fixed(c.HeaderHash[:])
	w.U32(uint32(len(c.Votes)))
	for _, vote := range c.Votes {
		raw, err := vote.MarshalBinary()
		if err != nil {
			return nil, err
		}
		w.Bytes(raw)
	}
	return w.BytesCopy(), nil
}

func ParseCertificate(data []byte) (Certificate, error) {
	r := codec.NewReader(data)
	networkBytes, err := r.Fixed(32)
	if err != nil {
		return Certificate{}, ErrCertificate
	}
	height, err := r.U64()
	if err != nil || height == 0 {
		return Certificate{}, ErrCertificate
	}
	round, err := r.U64()
	if err != nil {
		return Certificate{}, ErrCertificate
	}
	headerBytes, err := r.Fixed(32)
	if err != nil {
		return Certificate{}, ErrCertificate
	}
	count, err := r.U32()
	if err != nil || count == 0 || count > MaxCertificateVotes {
		return Certificate{}, ErrCertificate
	}
	certificate := Certificate{Height: height, Round: round, Votes: make([]Vote, int(count))}
	copy(certificate.Network[:], networkBytes)
	copy(certificate.HeaderHash[:], headerBytes)
	for i := range certificate.Votes {
		voteBytes, err := r.Bytes(512)
		if err != nil {
			return Certificate{}, ErrCertificate
		}
		certificate.Votes[i], err = ParseVote(voteBytes)
		if err != nil {
			return Certificate{}, err
		}
	}
	if r.Done() != nil || types.IsZero32([32]byte(certificate.Network)) || types.IsZero32([32]byte(certificate.HeaderHash)) {
		return Certificate{}, ErrCertificate
	}
	return certificate, nil
}

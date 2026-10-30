package governance

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const Version uint16 = 1

type ActionKind uint16

const (
	ActionUnknown ActionKind = iota
	ActionProtocolParameter
	ActionValidatorTransition
	ActionEconomicActivation
)

var ErrGovernance = errors.New("invalid governance object")

func (k ActionKind) Valid() bool {
	switch k {
	case ActionProtocolParameter, ActionValidatorTransition, ActionEconomicActivation:
		return true
	default:
		return false
	}
}

// Proposal is a canonical commitment to a future governance action. ActionHash
// commits to action-specific canonical bytes defined by a later protocol layer;
// hashing a claim does not make that claim valid or authorize its execution.
type Proposal struct {
	Proposer      types.AccountID
	ActionKind    ActionKind
	ActionHash    types.Hash
	CreatedHeight uint64
	Nonce         types.Hash
}

func (p Proposal) Validate() error {
	if types.IsZero32([32]byte(p.Proposer)) ||
		!p.ActionKind.Valid() ||
		types.IsZero32([32]byte(p.ActionHash)) ||
		p.CreatedHeight == 0 ||
		types.IsZero32([32]byte(p.Nonce)) {
		return ErrGovernance
	}
	return nil
}

func (p Proposal) MarshalBinary() ([]byte, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(p.Proposer[:])
	w.U16(uint16(p.ActionKind))
	w.Fixed(p.ActionHash[:])
	w.U64(p.CreatedHeight)
	w.Fixed(p.Nonce[:])
	return w.BytesCopy(), nil
}

func ParseProposal(data []byte) (Proposal, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Proposal{}, ErrGovernance
	}
	proposerRaw, err := r.Fixed(32)
	if err != nil {
		return Proposal{}, ErrGovernance
	}
	kind, err := r.U16()
	if err != nil {
		return Proposal{}, ErrGovernance
	}
	actionRaw, err := r.Fixed(32)
	if err != nil {
		return Proposal{}, ErrGovernance
	}
	createdHeight, err := r.U64()
	if err != nil {
		return Proposal{}, ErrGovernance
	}
	nonceRaw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return Proposal{}, ErrGovernance
	}
	var proposer types.AccountID
	var actionHash types.Hash
	var nonce types.Hash
	copy(proposer[:], proposerRaw)
	copy(actionHash[:], actionRaw)
	copy(nonce[:], nonceRaw)
	out := Proposal{
		Proposer:      proposer,
		ActionKind:    ActionKind(kind),
		ActionHash:    actionHash,
		CreatedHeight: createdHeight,
		Nonce:         nonce,
	}
	if err := out.Validate(); err != nil {
		return Proposal{}, err
	}
	return out, nil
}

func (p Proposal) Hash() (types.Hash, error) {
	raw, err := p.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/governance/proposal/v1", raw)), nil
}

// Timelock commits a proposal to an independently verified authorization
// commitment and a block-height execution window. This type does not verify who
// is allowed to authorize an action; a future consensus layer must establish
// AuthorizationHash from protocol-verifiable finalized evidence before creating
// or consuming a timelock.
type Timelock struct {
	ProposalHash       types.Hash
	AuthorizationHash  types.Hash
	AuthorizedHeight   uint64
	ExecuteAfterHeight uint64
	ExpiresHeight      uint64
}

func (t Timelock) Validate() error {
	if types.IsZero32([32]byte(t.ProposalHash)) ||
		types.IsZero32([32]byte(t.AuthorizationHash)) ||
		t.AuthorizedHeight == 0 ||
		t.ExecuteAfterHeight <= t.AuthorizedHeight ||
		(t.ExpiresHeight != 0 && t.ExpiresHeight <= t.ExecuteAfterHeight) {
		return ErrGovernance
	}
	return nil
}

func (t Timelock) ValidateMinDelay(minDelayBlocks uint64) error {
	if err := t.Validate(); err != nil {
		return err
	}
	if t.ExecuteAfterHeight-t.AuthorizedHeight < minDelayBlocks {
		return ErrGovernance
	}
	return nil
}

func (t Timelock) ReadyAt(height uint64) bool {
	if height == 0 || t.Validate() != nil || height < t.ExecuteAfterHeight {
		return false
	}
	return t.ExpiresHeight == 0 || height < t.ExpiresHeight
}

func (t Timelock) ExpiredAt(height uint64) bool {
	return t.Validate() == nil && t.ExpiresHeight != 0 && height >= t.ExpiresHeight
}

func (t Timelock) MarshalBinary() ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(t.ProposalHash[:])
	w.Fixed(t.AuthorizationHash[:])
	w.U64(t.AuthorizedHeight)
	w.U64(t.ExecuteAfterHeight)
	w.U64(t.ExpiresHeight)
	return w.BytesCopy(), nil
}

func ParseTimelock(data []byte) (Timelock, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Timelock{}, ErrGovernance
	}
	proposalRaw, err := r.Fixed(32)
	if err != nil {
		return Timelock{}, ErrGovernance
	}
	authorizationRaw, err := r.Fixed(32)
	if err != nil {
		return Timelock{}, ErrGovernance
	}
	authorizedHeight, err := r.U64()
	if err != nil {
		return Timelock{}, ErrGovernance
	}
	executeAfterHeight, err := r.U64()
	if err != nil {
		return Timelock{}, ErrGovernance
	}
	expiresHeight, err := r.U64()
	if err != nil || r.Done() != nil {
		return Timelock{}, ErrGovernance
	}
	var proposalHash types.Hash
	var authorizationHash types.Hash
	copy(proposalHash[:], proposalRaw)
	copy(authorizationHash[:], authorizationRaw)
	out := Timelock{
		ProposalHash:       proposalHash,
		AuthorizationHash:  authorizationHash,
		AuthorizedHeight:   authorizedHeight,
		ExecuteAfterHeight: executeAfterHeight,
		ExpiresHeight:      expiresHeight,
	}
	if err := out.Validate(); err != nil {
		return Timelock{}, err
	}
	return out, nil
}

func (t Timelock) Hash() (types.Hash, error) {
	raw, err := t.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/governance/timelock/v1", raw)), nil
}

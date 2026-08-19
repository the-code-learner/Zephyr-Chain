package object

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type Kind uint16

const (
	KindUnknown Kind = iota
	KindCoin
	KindTokenDefinition
	KindContract
	KindContractState
	KindComputeOffer
	KindComputeJob
	KindComputeAssignment
	KindComputeResult
	KindSystem
)

const MaxObjectDataBytes = 1 << 20

var (
	ErrInvalidObject = errors.New("invalid protocol object")
	ErrInvalidCoin   = errors.New("invalid coin object")
)

type Object struct {
	ID      types.ObjectID
	Version uint64
	Owner   types.AccountID
	Kind    Kind
	Data    []byte
}

type OutputSpec struct {
	Owner types.AccountID
	Kind  Kind
	Data  []byte
}

func (o Object) Validate() error {
	if types.IsZero32([32]byte(o.ID)) || o.Version == 0 || o.Kind == KindUnknown || len(o.Data) > MaxObjectDataBytes {
		return ErrInvalidObject
	}
	return validateOwnerForKind(o.Owner, o.Kind)
}

func (o OutputSpec) Validate() error {
	if o.Kind == KindUnknown || len(o.Data) > MaxObjectDataBytes {
		return ErrInvalidObject
	}
	return validateOwnerForKind(o.Owner, o.Kind)
}

func validateOwnerForKind(owner types.AccountID, kind Kind) error {
	switch kind {
	case KindSystem:
		return nil
	default:
		if types.IsZero32([32]byte(owner)) {
			return ErrInvalidObject
		}
		return nil
	}
}

func (o Object) CanonicalBytes() []byte {
	var w codec.Writer
	w.Fixed(o.ID[:])
	w.U64(o.Version)
	w.Fixed(o.Owner[:])
	w.U16(uint16(o.Kind))
	w.Bytes(o.Data)
	return w.BytesCopy()
}

func ParseObject(data []byte) (Object, error) {
	r := codec.NewReader(data)
	idBytes, err := r.Fixed(32)
	if err != nil {
		return Object{}, ErrInvalidObject
	}
	version, err := r.U64()
	if err != nil {
		return Object{}, ErrInvalidObject
	}
	ownerBytes, err := r.Fixed(32)
	if err != nil {
		return Object{}, ErrInvalidObject
	}
	kind, err := r.U16()
	if err != nil {
		return Object{}, ErrInvalidObject
	}
	payload, err := r.Bytes(MaxObjectDataBytes)
	if err != nil {
		return Object{}, ErrInvalidObject
	}
	if err := r.Done(); err != nil {
		return Object{}, ErrInvalidObject
	}
	var id types.ObjectID
	var owner types.AccountID
	copy(id[:], idBytes)
	copy(owner[:], ownerBytes)
	out := Object{ID: id, Version: version, Owner: owner, Kind: Kind(kind), Data: payload}
	if err := out.Validate(); err != nil {
		return Object{}, err
	}
	return out, nil
}

func ParseOutputSpec(data []byte) (OutputSpec, error) {
	r := codec.NewReader(data)
	ownerBytes, err := r.Fixed(32)
	if err != nil {
		return OutputSpec{}, ErrInvalidObject
	}
	kind, err := r.U16()
	if err != nil {
		return OutputSpec{}, ErrInvalidObject
	}
	payload, err := r.Bytes(MaxObjectDataBytes)
	if err != nil {
		return OutputSpec{}, ErrInvalidObject
	}
	if err := r.Done(); err != nil {
		return OutputSpec{}, ErrInvalidObject
	}
	var owner types.AccountID
	copy(owner[:], ownerBytes)
	out := OutputSpec{Owner: owner, Kind: Kind(kind), Data: payload}
	if err := out.Validate(); err != nil {
		return OutputSpec{}, err
	}
	return out, nil
}

func (o Object) Hash() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/object/v2", o.CanonicalBytes()))
}

func (o OutputSpec) CanonicalBytes() []byte {
	var w codec.Writer
	w.Fixed(o.Owner[:])
	w.U16(uint16(o.Kind))
	w.Bytes(o.Data)
	return w.BytesCopy()
}

func (o OutputSpec) Hash() types.Hash {
	return types.Hash(codec.DomainHash("zephyr/output-spec/v2", o.CanonicalBytes()))
}

type Coin struct {
	Token  types.TokenID
	Amount uint64
}

func (c Coin) MarshalBinary() []byte {
	var w codec.Writer
	w.Fixed(c.Token[:])
	w.U64(c.Amount)
	return w.BytesCopy()
}

func ParseCoin(data []byte) (Coin, error) {
	r := codec.NewReader(data)
	tokenBytes, err := r.Fixed(32)
	if err != nil {
		return Coin{}, ErrInvalidCoin
	}
	amount, err := r.U64()
	if err != nil || amount == 0 {
		return Coin{}, ErrInvalidCoin
	}
	if err := r.Done(); err != nil {
		return Coin{}, ErrInvalidCoin
	}
	var token types.TokenID
	copy(token[:], tokenBytes)
	if types.IsZero32([32]byte(token)) {
		return Coin{}, ErrInvalidCoin
	}
	return Coin{Token: token, Amount: amount}, nil
}

func NewCoinOutput(owner types.AccountID, token types.TokenID, amount uint64) (OutputSpec, error) {
	if types.IsZero32([32]byte(owner)) || types.IsZero32([32]byte(token)) || amount == 0 {
		return OutputSpec{}, ErrInvalidCoin
	}
	out := OutputSpec{Owner: owner, Kind: KindCoin, Data: Coin{Token: token, Amount: amount}.MarshalBinary()}
	return out, out.Validate()
}

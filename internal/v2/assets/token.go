package assets

import (
	"errors"
	"math"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrInvalidTokenDefinition = errors.New("invalid token definition")

type SupplyPolicy uint8

const (
	// SupplyCapped is zero intentionally so existing v2 capped-token builders
	// remain valid during the clean break when MaxSupply is already provided.
	SupplyCapped SupplyPolicy = iota
	SupplyFixed
	SupplyMintable
	SupplyPolicyCount
)

type Definition struct {
	TokenID       types.TokenID
	Name          string
	Symbol        string
	Decimals      uint8
	SupplyPolicy  SupplyPolicy
	MaxSupply     uint64
	CurrentSupply uint64
	MintAuthority types.AccountID
	Burnable      bool
	Transferable  bool
}

type CreateToken struct {
	Name          string
	Symbol        string
	Decimals      uint8
	SupplyPolicy  SupplyPolicy
	MaxSupply     uint64
	InitialSupply uint64
	MintAuthority types.AccountID
	Burnable      bool
	Transferable  bool
}

type MintToken struct {
	DefinitionObject types.ObjectID
	Recipient        types.AccountID
	Amount           uint64
}

type BurnToken struct {
	DefinitionObject types.ObjectID
	Amount           uint64
}

func (c CreateToken) Validate() error {
	name := strings.TrimSpace(c.Name)
	symbol := strings.TrimSpace(c.Symbol)
	if name == "" || len(name) > 64 || symbol == "" || len(symbol) > 16 || c.Decimals > 18 || c.InitialSupply == 0 ||
		types.IsZero32([32]byte(c.MintAuthority)) {
		return ErrInvalidTokenDefinition
	}
	switch c.SupplyPolicy {
	case SupplyCapped:
		if c.MaxSupply == 0 || c.InitialSupply > c.MaxSupply {
			return ErrInvalidTokenDefinition
		}
	case SupplyFixed:
		if c.MaxSupply == 0 || c.InitialSupply != c.MaxSupply {
			return ErrInvalidTokenDefinition
		}
	case SupplyMintable:
		if c.MaxSupply != 0 {
			return ErrInvalidTokenDefinition
		}
	default:
		return ErrInvalidTokenDefinition
	}
	return nil
}

func (c CreateToken) MarshalBinary() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.String(strings.TrimSpace(c.Name))
	w.String(strings.TrimSpace(c.Symbol))
	w.U8(c.Decimals)
	w.U8(uint8(c.SupplyPolicy))
	w.U64(c.MaxSupply)
	w.U64(c.InitialSupply)
	w.Fixed(c.MintAuthority[:])
	w.Bool(c.Burnable)
	w.Bool(c.Transferable)
	return w.BytesCopy(), nil
}

func ParseCreateToken(data []byte) (CreateToken, error) {
	r := codec.NewReader(data)
	name, err := r.String(64)
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	symbol, err := r.String(16)
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	decimals, err := r.U8()
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	policy, err := r.U8()
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	maxSupply, err := r.U64()
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	initialSupply, err := r.U64()
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	authBytes, err := r.Fixed(32)
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	burnable, err := r.Bool()
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	transferable, err := r.Bool()
	if err != nil || r.Done() != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	var authority types.AccountID
	copy(authority[:], authBytes)
	out := CreateToken{
		Name: name, Symbol: symbol, Decimals: decimals, SupplyPolicy: SupplyPolicy(policy),
		MaxSupply: maxSupply, InitialSupply: initialSupply, MintAuthority: authority, Burnable: burnable, Transferable: transferable,
	}
	if err := out.Validate(); err != nil {
		return CreateToken{}, err
	}
	return out, nil
}

func (d Definition) Validate() error {
	if types.IsZero32([32]byte(d.TokenID)) || strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Symbol) == "" ||
		d.Decimals > 18 || types.IsZero32([32]byte(d.MintAuthority)) {
		return ErrInvalidTokenDefinition
	}
	switch d.SupplyPolicy {
	case SupplyCapped, SupplyFixed:
		if d.MaxSupply == 0 || d.CurrentSupply > d.MaxSupply {
			return ErrInvalidTokenDefinition
		}
	case SupplyMintable:
		if d.MaxSupply != 0 {
			return ErrInvalidTokenDefinition
		}
	default:
		return ErrInvalidTokenDefinition
	}
	return nil
}

func (d Definition) MarshalBinary() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.Fixed(d.TokenID[:])
	w.String(strings.TrimSpace(d.Name))
	w.String(strings.TrimSpace(d.Symbol))
	w.U8(d.Decimals)
	w.U8(uint8(d.SupplyPolicy))
	w.U64(d.MaxSupply)
	w.U64(d.CurrentSupply)
	w.Fixed(d.MintAuthority[:])
	w.Bool(d.Burnable)
	w.Bool(d.Transferable)
	return w.BytesCopy(), nil
}

func ParseDefinition(data []byte) (Definition, error) {
	r := codec.NewReader(data)
	tokenRaw, err := r.Fixed(32)
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	name, err := r.String(64)
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	symbol, err := r.String(16)
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	decimals, err := r.U8()
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	policy, err := r.U8()
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	maxSupply, err := r.U64()
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	currentSupply, err := r.U64()
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	authorityRaw, err := r.Fixed(32)
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	burnable, err := r.Bool()
	if err != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	transferable, err := r.Bool()
	if err != nil || r.Done() != nil {
		return Definition{}, ErrInvalidTokenDefinition
	}
	var token types.TokenID
	var authority types.AccountID
	copy(token[:], tokenRaw)
	copy(authority[:], authorityRaw)
	out := Definition{
		TokenID: token, Name: name, Symbol: symbol, Decimals: decimals, SupplyPolicy: SupplyPolicy(policy),
		MaxSupply: maxSupply, CurrentSupply: currentSupply, MintAuthority: authority, Burnable: burnable, Transferable: transferable,
	}
	if err := out.Validate(); err != nil {
		return Definition{}, err
	}
	return out, nil
}

func (d Definition) Mint(amount uint64) (Definition, error) {
	if err := d.Validate(); err != nil || amount == 0 || d.SupplyPolicy == SupplyFixed || math.MaxUint64-d.CurrentSupply < amount {
		return Definition{}, ErrInvalidTokenDefinition
	}
	next := d
	next.CurrentSupply += amount
	if d.SupplyPolicy == SupplyCapped && next.CurrentSupply > d.MaxSupply {
		return Definition{}, ErrInvalidTokenDefinition
	}
	return next, next.Validate()
}

func (d Definition) Burn(amount uint64) (Definition, error) {
	if err := d.Validate(); err != nil || !d.Burnable || amount == 0 || amount > d.CurrentSupply {
		return Definition{}, ErrInvalidTokenDefinition
	}
	next := d
	next.CurrentSupply -= amount
	return next, next.Validate()
}

func (m MintToken) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(m.DefinitionObject)) || types.IsZero32([32]byte(m.Recipient)) || m.Amount == 0 {
		return nil, ErrInvalidTokenDefinition
	}
	var w codec.Writer
	w.Fixed(m.DefinitionObject[:])
	w.Fixed(m.Recipient[:])
	w.U64(m.Amount)
	return w.BytesCopy(), nil
}

func ParseMintToken(data []byte) (MintToken, error) {
	if len(data) != 72 {
		return MintToken{}, ErrInvalidTokenDefinition
	}
	var out MintToken
	copy(out.DefinitionObject[:], data[:32])
	copy(out.Recipient[:], data[32:64])
	r := codec.NewReader(data[64:])
	amount, err := r.U64()
	if err != nil || r.Done() != nil {
		return MintToken{}, ErrInvalidTokenDefinition
	}
	out.Amount = amount
	if _, err := out.MarshalBinary(); err != nil {
		return MintToken{}, err
	}
	return out, nil
}

func (b BurnToken) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(b.DefinitionObject)) || b.Amount == 0 {
		return nil, ErrInvalidTokenDefinition
	}
	var w codec.Writer
	w.Fixed(b.DefinitionObject[:])
	w.U64(b.Amount)
	return w.BytesCopy(), nil
}

func ParseBurnToken(data []byte) (BurnToken, error) {
	if len(data) != 40 {
		return BurnToken{}, ErrInvalidTokenDefinition
	}
	var out BurnToken
	copy(out.DefinitionObject[:], data[:32])
	r := codec.NewReader(data[32:])
	amount, err := r.U64()
	if err != nil || r.Done() != nil {
		return BurnToken{}, ErrInvalidTokenDefinition
	}
	out.Amount = amount
	if _, err := out.MarshalBinary(); err != nil {
		return BurnToken{}, err
	}
	return out, nil
}

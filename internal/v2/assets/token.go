package assets

import (
	"errors"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var ErrInvalidTokenDefinition = errors.New("invalid token definition")

type Definition struct {
	TokenID       types.TokenID
	Name          string
	Symbol        string
	Decimals      uint8
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
	MaxSupply     uint64
	InitialSupply uint64
	MintAuthority types.AccountID
	Burnable      bool
	Transferable  bool
}

func (c CreateToken) Validate() error {
	name := strings.TrimSpace(c.Name)
	symbol := strings.TrimSpace(c.Symbol)
	if name == "" || len(name) > 64 || symbol == "" || len(symbol) > 16 || c.Decimals > 18 {
		return ErrInvalidTokenDefinition
	}
	if c.InitialSupply == 0 {
		return ErrInvalidTokenDefinition
	}
	if c.MaxSupply != 0 && c.InitialSupply > c.MaxSupply {
		return ErrInvalidTokenDefinition
	}
	if types.IsZero32([32]byte(c.MintAuthority)) {
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
	if err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	if err := r.Done(); err != nil {
		return CreateToken{}, ErrInvalidTokenDefinition
	}
	var authority types.AccountID
	copy(authority[:], authBytes)
	out := CreateToken{
		Name: name, Symbol: symbol, Decimals: decimals, MaxSupply: maxSupply,
		InitialSupply: initialSupply, MintAuthority: authority, Burnable: burnable, Transferable: transferable,
	}
	if err := out.Validate(); err != nil {
		return CreateToken{}, err
	}
	return out, nil
}

func (d Definition) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(d.TokenID)) || strings.TrimSpace(d.Name) == "" || strings.TrimSpace(d.Symbol) == "" ||
		d.Decimals > 18 || d.CurrentSupply == 0 || (d.MaxSupply != 0 && d.CurrentSupply > d.MaxSupply) ||
		types.IsZero32([32]byte(d.MintAuthority)) {
		return nil, ErrInvalidTokenDefinition
	}
	var w codec.Writer
	w.Fixed(d.TokenID[:])
	w.String(strings.TrimSpace(d.Name))
	w.String(strings.TrimSpace(d.Symbol))
	w.U8(d.Decimals)
	w.U64(d.MaxSupply)
	w.U64(d.CurrentSupply)
	w.Fixed(d.MintAuthority[:])
	w.Bool(d.Burnable)
	w.Bool(d.Transferable)
	return w.BytesCopy(), nil
}

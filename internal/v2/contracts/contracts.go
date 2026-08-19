package contracts

import (
	"bytes"
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	RuntimeWASMv1        = "wasm-v1"
	MaxModuleBytes       = 4 << 20
	MaxInitialStateBytes = 1 << 20
)

var (
	ErrInvalidModule     = errors.New("invalid deterministic wasm module")
	ErrInvalidDeployment = errors.New("invalid contract deployment")
	ErrFuelExhausted     = errors.New("contract fuel exhausted")
	ErrUndeclaredAccess  = errors.New("contract attempted undeclared state access")
)

type Deployment struct {
	Runtime          string
	Code             []byte
	ABI              uint16
	UpgradeAuthority types.AccountID
	InitialState     []byte
	MaxMemoryPages   uint32
}

func (d Deployment) Validate() error {
	if d.Runtime != RuntimeWASMv1 || d.ABI == 0 || len(d.Code) == 0 || len(d.Code) > MaxModuleBytes ||
		len(d.InitialState) > MaxInitialStateBytes || d.MaxMemoryPages == 0 ||
		types.IsZero32([32]byte(d.UpgradeAuthority)) {
		return ErrInvalidDeployment
	}
	if !ValidateWASMModule(d.Code) {
		return ErrInvalidModule
	}
	return nil
}

func (d Deployment) MarshalBinary() ([]byte, error) {
	if err := d.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.String(d.Runtime)
	w.Bytes(d.Code)
	w.U16(d.ABI)
	w.Fixed(d.UpgradeAuthority[:])
	w.Bytes(d.InitialState)
	w.U32(d.MaxMemoryPages)
	return w.BytesCopy(), nil
}

func ValidateWASMModule(code []byte) bool {
	if len(code) < 8 || len(code) > MaxModuleBytes {
		return false
	}
	return bytes.Equal(code[:4], []byte{0x00, 0x61, 0x73, 0x6d}) &&
		bytes.Equal(code[4:8], []byte{0x01, 0x00, 0x00, 0x00})
}

type Access struct {
	ObjectID types.ObjectID
	Write    bool
}

type Request struct {
	ContractID types.ContractID
	Entrypoint string
	Arguments  []byte
	Accesses   []Access
	FuelLimit  uint64
}

type Result struct {
	ReturnData []byte
	FuelUsed   uint64
	Writes     map[types.ObjectID][]byte
	Events     [][]byte
}

type Runtime interface {
	ValidateModule(code []byte) error
	Execute(request Request) (Result, error)
}

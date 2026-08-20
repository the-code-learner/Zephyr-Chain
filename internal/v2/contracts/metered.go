package contracts

import (
	"errors"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	MaxArgumentsBytes = 1 << 20
	MaxReturnBytes    = 1 << 20
	MaxEventBytes     = 64 << 10
	MaxEvents         = 1024
	MaxAccesses       = 4096
)

var (
	ErrInvalidRequest = errors.New("invalid deterministic contract request")
	ErrInvalidResult  = errors.New("invalid deterministic contract result")
)

// MeteredRuntime is the consensus guard around a concrete deterministic
// contract engine. The engine may change, but fuel, declared state access and
// bounded output remain consensus invariants outside the interpreter.
type MeteredRuntime struct {
	Inner Runtime
}

func (m MeteredRuntime) ValidateModule(code []byte) error {
	if m.Inner == nil {
		return ErrInvalidModule
	}
	return m.Inner.ValidateModule(code)
}

func (m MeteredRuntime) Execute(request Request) (Result, error) {
	if m.Inner == nil {
		return Result{}, ErrInvalidRequest
	}
	allowed, err := validateRequest(request)
	if err != nil {
		return Result{}, err
	}
	// Code is optional at this generic guard boundary so deterministic runtimes
	// backed by pre-registered/native modules can still use the same consensus
	// invariants. Runtimes that require code (Zephyr Script/WASM) reject an empty
	// module inside Execute/ValidateModule themselves.
	if len(request.Code) > 0 {
		if err := m.Inner.ValidateModule(request.Code); err != nil {
			return Result{}, err
		}
	}
	result, err := m.Inner.Execute(request)
	if err != nil {
		return Result{}, err
	}
	if result.FuelUsed > request.FuelLimit {
		return Result{}, ErrFuelExhausted
	}
	if len(result.ReturnData) > MaxReturnBytes || len(result.Events) > MaxEvents {
		return Result{}, ErrInvalidResult
	}
	for _, event := range result.Events {
		if len(event) > MaxEventBytes {
			return Result{}, ErrInvalidResult
		}
	}
	for id := range result.Writes {
		write, declared := allowed[id]
		if !declared || !write {
			return Result{}, ErrUndeclaredAccess
		}
	}
	return result, nil
}

func validateRequest(request Request) (map[types.ObjectID]bool, error) {
	if types.IsZero32([32]byte(request.ContractID)) || strings.TrimSpace(request.Entrypoint) == "" || len(request.Entrypoint) > 128 ||
		len(request.Code) > MaxModuleBytes || len(request.Arguments) > MaxArgumentsBytes || request.FuelLimit == 0 || len(request.Accesses) > MaxAccesses {
		return nil, ErrInvalidRequest
	}
	allowed := make(map[types.ObjectID]bool, len(request.Accesses))
	for _, access := range request.Accesses {
		if types.IsZero32([32]byte(access.ObjectID)) {
			return nil, ErrInvalidRequest
		}
		if _, duplicate := allowed[access.ObjectID]; duplicate {
			return nil, ErrInvalidRequest
		}
		allowed[access.ObjectID] = access.Write
	}
	for id := range request.ReadValues {
		if _, declared := allowed[id]; !declared {
			return nil, ErrUndeclaredAccess
		}
	}
	return allowed, nil
}

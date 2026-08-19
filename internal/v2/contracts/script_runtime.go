package contracts

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"go.starlark.net/starlark"
)

var ErrScriptRuntime = errors.New("zephyr script execution failed")

// ScriptRuntime is Zephyr's deterministic reference smart-contract runtime.
// It exposes no clock, randomness, filesystem, network or dynamic module load.
// Execution is bounded by Starlark's abstract step counter, which is used as
// deterministic fuel for consensus.
type ScriptRuntime struct{}

func (ScriptRuntime) ValidateModule(code []byte) error {
	if !ValidateScriptModule(code) {
		return ErrInvalidModule
	}
	thread := &starlark.Thread{Name: "zephyr-validate", Load: disabledLoad}
	thread.SetMaxExecutionSteps(1_000_000)
	if _, err := starlark.ExecFile(thread, "contract.star", string(code), validationPredeclared()); err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidModule, err)
	}
	return nil
}

func (ScriptRuntime) Execute(request Request) (Result, error) {
	if request.Runtime != RuntimeZephyrScriptV1 || !ValidateScriptModule(request.Code) {
		return Result{}, ErrInvalidModule
	}
	allowed := make(map[string]bool, len(request.Accesses))
	for _, access := range request.Accesses {
		allowed[access.ObjectID.String()] = access.Write
	}
	writes := make(map[types.ObjectID][]byte)
	events := make([][]byte, 0)

	predeclared := emptyPredeclared()
	predeclared["state_read"] = starlark.NewBuiltin("state_read", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var id string
		if err := starlark.UnpackArgs("state_read", args, kwargs, "id", &id); err != nil {
			return nil, err
		}
		normalized, rawID, err := parseObjectID(id)
		if err != nil {
			return nil, err
		}
		if _, ok := allowed[normalized]; !ok {
			return nil, ErrUndeclaredAccess
		}
		value, ok := request.ReadValues[rawID]
		if !ok {
			return starlark.None, nil
		}
		return starlark.Bytes(string(value)), nil
	})
	predeclared["state_write"] = starlark.NewBuiltin("state_write", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var id string
		var value starlark.Value
		if err := starlark.UnpackArgs("state_write", args, kwargs, "id", &id, "value", &value); err != nil {
			return nil, err
		}
		normalized, rawID, err := parseObjectID(id)
		if err != nil {
			return nil, err
		}
		if !allowed[normalized] {
			return nil, ErrUndeclaredAccess
		}
		bytes, err := valueBytes(value)
		if err != nil || len(bytes) > MaxReturnBytes {
			return nil, ErrInvalidResult
		}
		writes[rawID] = append([]byte(nil), bytes...)
		return starlark.None, nil
	})
	predeclared["emit"] = starlark.NewBuiltin("emit", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var value starlark.Value
		if err := starlark.UnpackArgs("emit", args, kwargs, "value", &value); err != nil {
			return nil, err
		}
		bytes, err := valueBytes(value)
		if err != nil || len(bytes) > MaxEventBytes || len(events) >= MaxEvents {
			return nil, ErrInvalidResult
		}
		events = append(events, append([]byte(nil), bytes...))
		return starlark.None, nil
	})
	predeclared["sha256"] = starlark.NewBuiltin("sha256", func(_ *starlark.Thread, _ *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
		var value starlark.Value
		if err := starlark.UnpackArgs("sha256", args, kwargs, "value", &value); err != nil {
			return nil, err
		}
		bytes, err := valueBytes(value)
		if err != nil {
			return nil, err
		}
		hash := sha256.Sum256(bytes)
		return starlark.Bytes(string(hash[:])), nil
	})

	thread := &starlark.Thread{Name: "zephyr-contract", Load: disabledLoad}
	thread.SetMaxExecutionSteps(request.FuelLimit)
	globals, err := starlark.ExecFile(thread, "contract.star", string(request.Code), predeclared)
	if err != nil {
		if thread.ExecutionSteps() >= request.FuelLimit {
			return Result{}, ErrFuelExhausted
		}
		return Result{}, fmt.Errorf("%w: %v", ErrScriptRuntime, err)
	}
	entry, ok := globals[request.Entrypoint]
	if !ok {
		return Result{}, fmt.Errorf("%w: missing entrypoint", ErrScriptRuntime)
	}
	callable, ok := entry.(starlark.Callable)
	if !ok {
		return Result{}, fmt.Errorf("%w: entrypoint is not callable", ErrScriptRuntime)
	}
	value, err := starlark.Call(thread, callable, starlark.Tuple{starlark.Bytes(string(request.Arguments))}, nil)
	if err != nil {
		if thread.ExecutionSteps() >= request.FuelLimit {
			return Result{}, ErrFuelExhausted
		}
		return Result{}, fmt.Errorf("%w: %v", ErrScriptRuntime, err)
	}
	returned, err := valueBytes(value)
	if value == starlark.None {
		returned = nil
		err = nil
	}
	if err != nil || len(returned) > MaxReturnBytes {
		return Result{}, ErrInvalidResult
	}
	outWrites := make(map[types.ObjectID][]byte, len(writes))
	for id, value := range writes {
		outWrites[id] = value
	}
	return Result{ReturnData: returned, FuelUsed: thread.ExecutionSteps(), Writes: outWrites, Events: events}, nil
}

func emptyPredeclared() starlark.StringDict { return starlark.StringDict{} }

func validationPredeclared() starlark.StringDict {
	stub := func(_ *starlark.Thread, _ *starlark.Builtin, _ starlark.Tuple, _ []starlark.Tuple) (starlark.Value, error) {
		return starlark.None, nil
	}
	return starlark.StringDict{
		"state_read":  starlark.NewBuiltin("state_read", stub),
		"state_write": starlark.NewBuiltin("state_write", stub),
		"emit":        starlark.NewBuiltin("emit", stub),
		"sha256":      starlark.NewBuiltin("sha256", stub),
	}
}

func disabledLoad(_ *starlark.Thread, module string) (starlark.StringDict, error) {
	return nil, fmt.Errorf("module loading disabled: %s", module)
}

func parseObjectID(value string) (string, types.ObjectID, error) {
	var id types.ObjectID
	normalized := strings.ToLower(strings.TrimSpace(value))
	raw, err := hex.DecodeString(normalized)
	if err != nil || len(raw) != len(id) {
		return "", id, ErrUndeclaredAccess
	}
	copy(id[:], raw)
	return normalized, id, nil
}

func valueBytes(value starlark.Value) ([]byte, error) {
	switch v := value.(type) {
	case starlark.Bytes:
		return []byte(string(v)), nil
	case starlark.String:
		return []byte(string(v)), nil
	default:
		return nil, ErrInvalidResult
	}
}

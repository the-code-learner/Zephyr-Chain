package contracts

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestScriptRuntimeDeterministicStateAndEvents(t *testing.T) {
	contract := types.ContractID(types.HashBytes("contract", []byte("script")))
	stateID := types.ObjectID(types.HashBytes("state", []byte("one")))
	code := []byte(`
def run(arg):
    before = state_read("` + stateID.String() + `")
    state_write("` + stateID.String() + `", arg)
    emit(sha256(arg))
    return before
`)
	request := Request{
		ContractID: contract, Runtime: RuntimeZephyrScriptV1, Code: code, Entrypoint: "run", Arguments: []byte("new"),
		Accesses: []Access{{ObjectID: stateID, Write: true}}, ReadValues: map[types.ObjectID][]byte{stateID: []byte("old")}, FuelLimit: 100_000,
	}
	runtime := MeteredRuntime{Inner: ScriptRuntime{}}
	first, err := runtime.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := runtime.Execute(request)
	if err != nil {
		t.Fatal(err)
	}
	if string(first.ReturnData) != "old" || string(first.Writes[stateID]) != "new" || len(first.Events) != 1 {
		t.Fatalf("unexpected result: %+v", first)
	}
	if first.FuelUsed != second.FuelUsed || string(first.ReturnData) != string(second.ReturnData) || string(first.Events[0]) != string(second.Events[0]) {
		t.Fatal("same contract request produced nondeterministic result")
	}
}

func TestScriptRuntimeRejectsUndeclaredWrite(t *testing.T) {
	contract := types.ContractID(types.HashBytes("contract", []byte("script-write")))
	stateID := types.ObjectID(types.HashBytes("state", []byte("write")))
	code := []byte(`
def run(arg):
    state_write("` + stateID.String() + `", arg)
    return arg
`)
	_, err := (MeteredRuntime{Inner: ScriptRuntime{}}).Execute(Request{
		ContractID: contract, Runtime: RuntimeZephyrScriptV1, Code: code, Entrypoint: "run", Arguments: []byte("x"),
		Accesses: []Access{{ObjectID: stateID, Write: false}}, FuelLimit: 100_000,
	})
	if !errors.Is(err, ErrUndeclaredAccess) {
		t.Fatalf("expected undeclared access error, got %v", err)
	}
}

func TestScriptRuntimeFuelExhaustion(t *testing.T) {
	contract := types.ContractID(types.HashBytes("contract", []byte("script-fuel")))
	code := []byte(`
def run(arg):
    x = 0
    for i in range(1000000):
        x += i
    return arg
`)
	_, err := (MeteredRuntime{Inner: ScriptRuntime{}}).Execute(Request{
		ContractID: contract, Runtime: RuntimeZephyrScriptV1, Code: code, Entrypoint: "run", Arguments: []byte("x"), FuelLimit: 100,
	})
	if !errors.Is(err, ErrFuelExhausted) {
		t.Fatalf("expected fuel exhaustion, got %v", err)
	}
}

func TestScriptRuntimeDisablesLoad(t *testing.T) {
	code := []byte("load(\"remote.star\", \"x\")\ndef run(arg):\n    return arg\n")
	if err := (ScriptRuntime{}).ValidateModule(code); err == nil {
		t.Fatal("expected module load to be rejected")
	}
}

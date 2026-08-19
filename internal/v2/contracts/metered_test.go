package contracts

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type fakeRuntime struct {
	result Result
}

func (f fakeRuntime) ValidateModule(code []byte) error        { return nil }
func (f fakeRuntime) Execute(request Request) (Result, error) { return f.result, nil }

func TestMeteredRuntimeEnforcesFuelAndAccess(t *testing.T) {
	contract := types.ContractID(types.HashBytes("contract", []byte("c")))
	writable := types.ObjectID(types.HashBytes("object", []byte("write")))
	request := Request{ContractID: contract, Entrypoint: "transfer", FuelLimit: 100, Accesses: []Access{{ObjectID: writable, Write: true}}}

	valid := MeteredRuntime{Inner: fakeRuntime{result: Result{FuelUsed: 80, Writes: map[types.ObjectID][]byte{writable: []byte("new")}}}}
	if _, err := valid.Execute(request); err != nil {
		t.Fatal(err)
	}

	overFuel := MeteredRuntime{Inner: fakeRuntime{result: Result{FuelUsed: 101}}}
	if _, err := overFuel.Execute(request); err != ErrFuelExhausted {
		t.Fatalf("expected fuel rejection, got %v", err)
	}

	undeclared := types.ObjectID(types.HashBytes("object", []byte("other")))
	badAccess := MeteredRuntime{Inner: fakeRuntime{result: Result{FuelUsed: 1, Writes: map[types.ObjectID][]byte{undeclared: []byte("x")}}}}
	if _, err := badAccess.Execute(request); err != ErrUndeclaredAccess {
		t.Fatalf("expected undeclared access rejection, got %v", err)
	}
}

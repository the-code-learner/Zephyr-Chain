package execution

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/contracts"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestBatchAllowsSharedReadOnlyContractObject(t *testing.T) {
	root := types.HashBytes("state", []byte("shared-read"))
	contractID := types.ObjectID(types.HashBytes("contract-object", []byte("shared")))
	stateA := types.ObjectID(types.HashBytes("state-object", []byte("a")))
	stateB := types.ObjectID(types.HashBytes("state-object", []byte("b")))
	feeA := types.ObjectID(types.HashBytes("coin", []byte("a")))
	feeB := types.ObjectID(types.HashBytes("coin", []byte("b")))

	makeTx := func(stateID, feeID types.ObjectID, salt byte) tx.Transaction {
		payload, err := (contracts.Call{ContractObject: contractID, Entrypoint: "run", FuelLimit: 100, Accesses: []contracts.Access{{ObjectID: stateID, Write: true}}}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		transaction := tx.Transaction{StateRoot: root, Inputs: []tx.InputRef{{ObjectID: contractID}, {ObjectID: stateID}, {ObjectID: feeID}}, Operations: []tx.Operation{{Kind: tx.OpContractCall, Payload: payload}}}
		transaction.Salt[0] = salt
		return transaction
	}
	if err := validateIndependentBatch([]tx.Transaction{makeTx(stateA, feeA, 1), makeTx(stateB, feeB, 2)}); err != nil {
		t.Fatalf("shared read-only contract should be parallelizable: %v", err)
	}
}

func TestBatchRejectsReadWriteContractStateConflict(t *testing.T) {
	root := types.HashBytes("state", []byte("rw-conflict"))
	contractID := types.ObjectID(types.HashBytes("contract-object", []byte("conflict")))
	shared := types.ObjectID(types.HashBytes("state-object", []byte("shared")))
	feeA := types.ObjectID(types.HashBytes("coin", []byte("ca")))
	feeB := types.ObjectID(types.HashBytes("coin", []byte("cb")))
	makeTx := func(write bool, feeID types.ObjectID, salt byte) tx.Transaction {
		payload, err := (contracts.Call{ContractObject: contractID, Entrypoint: "run", FuelLimit: 100, Accesses: []contracts.Access{{ObjectID: shared, Write: write}}}).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		transaction := tx.Transaction{StateRoot: root, Inputs: []tx.InputRef{{ObjectID: contractID}, {ObjectID: shared}, {ObjectID: feeID}}, Operations: []tx.Operation{{Kind: tx.OpContractCall, Payload: payload}}}
		transaction.Salt[0] = salt
		return transaction
	}
	if err := validateIndependentBatch([]tx.Transaction{makeTx(false, feeA, 1), makeTx(true, feeB, 2)}); err != ErrBatchConflict {
		t.Fatalf("expected read/write conflict, got %v", err)
	}
}

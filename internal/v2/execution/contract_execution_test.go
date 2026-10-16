package execution

import (
	"crypto/elliptic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/contracts"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestDeployAndCallZephyrScriptAreStateCommitted(t *testing.T) {
	key := makeKey(t)
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	owner := types.AccountIDFromPublicKey(pub)
	networkID := types.NetworkID(types.HashBytes("network", []byte("contract-execution")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	store := worldstate.NewMemory()
	feeID := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("contract-fee")), 0)
	feeSpec, _ := object.NewCoinOutput(owner, native, 10)
	feeObject := object.Object{ID: feeID, Version: 1, Owner: owner, Kind: object.KindCoin, Data: feeSpec.Data}
	root, err := store.Apply(nil, []object.Object{feeObject})
	if err != nil {
		t.Fatal(err)
	}
	feeWitness, feeProof, _ := store.Proof(feeID)
	feeHash := feeWitness.Hash()
	change, _ := object.NewCoinOutput(owner, native, 9)
	code := []byte("def run(id):\n    before = state_read(id)\n    state_write(id, \"new\")\n    emit(sha256(\"event\"))\n    return before\n")
	deployment := contracts.Deployment{Runtime: contracts.RuntimeZephyrScriptV1, Code: code, ABI: 1, UpgradeAuthority: owner, InitialState: []byte("old"), MaxMemoryPages: 16}
	payload, err := deployment.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	deploy := tx.Transaction{Version: tx.Version, Network: networkID, ShardID: 0, StateRoot: root,
		Inputs: []tx.InputRef{{ObjectID: feeID, Version: 1, ObjectHash: feeHash}}, Outputs: []object.OutputSpec{change},
		Operations: []tx.Operation{{Kind: tx.OpDeployContract, Payload: payload}}, Fee: 1,
		Witnesses: []tx.Witness{{Object: feeWitness, Proof: feeProof}}, ValidUntilHeight: 20}
	deploy.Salt[0] = 41
	if err := deploy.Sign(key); err != nil {
		t.Fatal(err)
	}
	engine := Engine{Network: networkID, NativeToken: native, ShardCount: 1}
	deployResult, err := engine.Execute(deploy)
	if err != nil {
		t.Fatal(err)
	}
	if len(deployResult.Created) != 3 {
		t.Fatalf("expected change+contract+state, got %d", len(deployResult.Created))
	}
	if _, err := store.Apply(deployResult.Consumed, deployResult.Created); err != nil {
		t.Fatal(err)
	}
	var contractObject, stateObject, callFee object.Object
	for _, item := range deployResult.Created {
		switch item.Kind {
		case object.KindContract:
			contractObject = item
		case object.KindContractState:
			stateObject = item
		case object.KindCoin:
			callFee = item
		}
	}
	if contractObject.ID == (types.ObjectID{}) || stateObject.ID == (types.ObjectID{}) {
		t.Fatal("deployment objects missing")
	}
	root = store.Root()
	objects := []object.Object{callFee, contractObject, stateObject}
	inputs := make([]tx.InputRef, 0, len(objects))
	witnesses := make([]tx.Witness, 0, len(objects))
	for _, item := range objects {
		proved, proof, ok := store.Proof(item.ID)
		if !ok {
			t.Fatal("missing call input proof")
		}
		h := proved.Hash()
		inputs = append(inputs, tx.InputRef{ObjectID: item.ID, Version: item.Version, ObjectHash: h})
		witnesses = append(witnesses, tx.Witness{Object: proved, Proof: proof})
	}
	callPayload, err := (contracts.Call{ContractObject: contractObject.ID, Entrypoint: "run", Arguments: []byte(stateObject.ID.String()), FuelLimit: 100_000, Accesses: []contracts.Access{{ObjectID: stateObject.ID, Write: true}}}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	change2, _ := object.NewCoinOutput(owner, native, 8)
	call := tx.Transaction{Version: tx.Version, Network: networkID, ShardID: 0, StateRoot: root,
		Inputs: inputs, Outputs: []object.OutputSpec{change2}, Operations: []tx.Operation{{Kind: tx.OpContractCall, Payload: callPayload}}, Fee: 1,
		Witnesses: witnesses, ValidUntilHeight: 30}
	call.Salt[0] = 42
	if err := call.Sign(key); err != nil {
		t.Fatal(err)
	}
	callResult, err := engine.Execute(call)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(callResult.Consumed, callResult.Created); err != nil {
		t.Fatal(err)
	}
	updated, _, ok := store.Proof(stateObject.ID)
	if !ok || updated.Version != 2 || string(updated.Data) != "new" {
		t.Fatalf("contract state not committed: %+v", updated)
	}
	if _, _, ok := store.Proof(contractObject.ID); !ok {
		t.Fatal("read-only contract object was consumed")
	}
	foundReceipt := false
	for _, item := range callResult.Created {
		if item.Kind == object.KindSystem {
			foundReceipt = true
		}
	}
	if !foundReceipt {
		t.Fatal("contract execution receipt not committed")
	}
}

package execution

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/assets"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func makeKey(t *testing.T) *ecdsa.PrivateKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func TestWalletProofToExecutionToNewRoot(t *testing.T) {
	aliceKey := makeKey(t)
	alicePub := elliptic.Marshal(elliptic.P256(), aliceKey.PublicKey.X, aliceKey.PublicKey.Y)
	alice := types.AccountIDFromPublicKey(alicePub)
	bob := types.AccountIDFromPublicKey([]byte("bob"))
	network := types.NetworkID(types.HashBytes("network", []byte("v2")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))

	store := worldstate.NewMemory()
	genesisTx := types.HashBytes("genesis", []byte("coin"))
	coinID := types.ObjectIDFromTransaction(genesisTx, 0)
	initialOut, err := object.NewCoinOutput(alice, native, 100)
	if err != nil {
		t.Fatal(err)
	}
	initial := object.Object{ID: coinID, Version: 1, Owner: alice, Kind: initialOut.Kind, Data: initialOut.Data}
	root, err := store.Apply(nil, []object.Object{initial})
	if err != nil {
		t.Fatal(err)
	}

	witnessObject, proof, ok := store.Proof(coinID)
	if !ok {
		t.Fatal("missing input")
	}
	inputHash := witnessObject.Hash()
	toBob, _ := object.NewCoinOutput(bob, native, 25)
	change, _ := object.NewCoinOutput(alice, native, 74)
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
		Inputs:     []tx.InputRef{{ObjectID: coinID, Version: 1, ObjectHash: inputHash}},
		Outputs:    []object.OutputSpec{toBob, change},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee:        1, ValidUntilHeight: 10,
		Witnesses: []tx.Witness{{Object: witnessObject, Proof: proof}},
	}
	transaction.Salt[0] = 7
	if err := transaction.Sign(aliceKey); err != nil {
		t.Fatal(err)
	}

	engine := Engine{Network: network, NativeToken: native, ShardCount: 1}
	result, err := engine.Execute(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Consumed) != 1 || len(result.Created) != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	newRoot, err := store.Apply(result.Consumed, result.Created)
	if err != nil {
		t.Fatal(err)
	}
	if newRoot == root {
		t.Fatal("state root did not change")
	}

	bobObject, bobProof, ok := store.Proof(result.Created[0].ID)
	if !ok {
		t.Fatal("bob output missing")
	}
	bobHash := bobObject.Hash()
	if !state.Verify(newRoot, types.Hash(bobObject.ID), bobHash[:], bobProof) {
		t.Fatal("recipient cannot verify resulting object")
	}
}

func TestNativeTokenCreation(t *testing.T) {
	key := makeKey(t)
	pub := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	alice := types.AccountIDFromPublicKey(pub)
	network := types.NetworkID(types.HashBytes("network", []byte("v2")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	store := worldstate.NewMemory()
	id := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("fee")), 0)
	feeCoinOut, _ := object.NewCoinOutput(alice, native, 10)
	feeCoin := object.Object{ID: id, Version: 1, Owner: alice, Kind: feeCoinOut.Kind, Data: feeCoinOut.Data}
	root, err := store.Apply(nil, []object.Object{feeCoin})
	if err != nil {
		t.Fatal(err)
	}
	witness, proof, _ := store.Proof(id)
	h := witness.Hash()
	change, _ := object.NewCoinOutput(alice, native, 9)

	create := assets.CreateToken{
		Name: "Example Token", Symbol: "EXM", Decimals: 6,
		MaxSupply: 1_000_000, InitialSupply: 500_000, MintAuthority: alice,
		Burnable: true, Transferable: true,
	}
	payload, err := create.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
		Inputs:     []tx.InputRef{{ObjectID: id, Version: 1, ObjectHash: h}},
		Outputs:    []object.OutputSpec{change},
		Operations: []tx.Operation{{Kind: tx.OpCreateToken, Payload: payload}},
		Fee:        1,
		Witnesses:  []tx.Witness{{Object: witness, Proof: proof}},
	}
	transaction.Salt[0] = 9
	if err := transaction.Sign(key); err != nil {
		t.Fatal(err)
	}
	result, err := (Engine{Network: network, NativeToken: native, ShardCount: 1}).Execute(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 3 {
		t.Fatalf("expected change + token definition + initial supply, got %d", len(result.Created))
	}
	if result.Created[1].Kind != object.KindTokenDefinition || result.Created[2].Kind != object.KindCoin {
		t.Fatalf("unexpected token creation objects: %v %v", result.Created[1].Kind, result.Created[2].Kind)
	}
}

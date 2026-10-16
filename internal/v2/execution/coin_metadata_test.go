package execution

import (
	"crypto/elliptic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestExecutorOverridesWalletCoinCreationHeight(t *testing.T) {
	key := makeKey(t)
	owner := types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y))
	network := types.NetworkID(types.HashBytes("network", []byte("coin-height")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	store := worldstate.NewMemory()
	seed := types.HashBytes("coin-height", []byte("seed"))
	inputID := types.ObjectIDForShard(seed, 0, 0)
	inputSpec, err := object.NewCoinOutputAtHeight(owner, native, 100, 50)
	if err != nil {
		t.Fatal(err)
	}
	input := object.Object{ID: inputID, Version: 1, Owner: owner, Kind: object.KindCoin, Data: inputSpec.Data}
	if _, err := store.Apply(nil, []object.Object{input}); err != nil {
		t.Fatal(err)
	}
	witness, proof, ok := store.Proof(inputID)
	if !ok {
		t.Fatal("missing input proof")
	}
	maliciousOutput, err := object.NewCoinOutputAtHeight(owner, native, 99, 1)
	if err != nil {
		t.Fatal(err)
	}
	transaction := tx.Transaction{
		Version: tx.Version, Network: network, ShardID: 0, StateRoot: store.Root(),
		Inputs:     []tx.InputRef{{ObjectID: inputID, Version: 1, ObjectHash: witness.Hash()}},
		Outputs:    []object.OutputSpec{maliciousOutput},
		Operations: []tx.Operation{{Kind: tx.OpTransfer}},
		Fee:        1,
		Witnesses:  []tx.Witness{{Object: witness, Proof: proof}},
	}
	transaction.Salt[0] = 1
	if err := transaction.Sign(key); err != nil {
		t.Fatal(err)
	}
	result, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 100}).Execute(transaction)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Created) != 1 {
		t.Fatalf("unexpected output count %d", len(result.Created))
	}
	coin, err := object.ParseCoin(result.Created[0].Data)
	if err != nil {
		t.Fatal(err)
	}
	if coin.CreatedHeight != 100 {
		t.Fatalf("wallet-controlled height survived execution: got %d want 100", coin.CreatedHeight)
	}
}

package execution

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestParallelBatchAppliesIndependentTransfers(t *testing.T) {
	aliceKey := makeKey(t)
	carolKey := makeKey(t)
	alicePub := elliptic.Marshal(elliptic.P256(), aliceKey.PublicKey.X, aliceKey.PublicKey.Y)
	carolPub := elliptic.Marshal(elliptic.P256(), carolKey.PublicKey.X, carolKey.PublicKey.Y)
	alice := types.AccountIDFromPublicKey(alicePub)
	carol := types.AccountIDFromPublicKey(carolPub)
	bob := types.AccountIDFromPublicKey([]byte("bob-parallel"))
	dave := types.AccountIDFromPublicKey([]byte("dave-parallel"))
	network := types.NetworkID(types.HashBytes("network", []byte("parallel")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))

	store := worldstate.NewMemory()
	coinAID := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("a")), 0)
	coinCID := types.ObjectIDFromTransaction(types.HashBytes("seed", []byte("c")), 0)
	coinAOut, _ := object.NewCoinOutput(alice, native, 100)
	coinCOut, _ := object.NewCoinOutput(carol, native, 200)
	coinA := object.Object{ID: coinAID, Version: 1, Owner: alice, Kind: coinAOut.Kind, Data: coinAOut.Data}
	coinC := object.Object{ID: coinCID, Version: 1, Owner: carol, Kind: coinCOut.Kind, Data: coinCOut.Data}
	root, err := store.Apply(nil, []object.Object{coinA, coinC})
	if err != nil {
		t.Fatal(err)
	}

	makeTransfer := func(key *ecdsa.PrivateKey, input object.Object, recipient types.AccountID, amount, change uint64, salt byte) tx.Transaction {
		proofObject, proof, ok := store.Proof(input.ID)
		if !ok {
			t.Fatal("missing input proof")
		}
		toRecipient, _ := object.NewCoinOutput(recipient, native, amount)
		toChange, _ := object.NewCoinOutput(input.Owner, native, change)
		h := proofObject.Hash()
		transaction := tx.Transaction{
			Version: tx.Version, Network: network, ShardID: 0, StateRoot: root,
			Inputs:  []tx.InputRef{{ObjectID: input.ID, Version: input.Version, ObjectHash: h}},
			Outputs: []object.OutputSpec{toRecipient, toChange}, Operations: []tx.Operation{{Kind: tx.OpTransfer}},
			Fee: 1, Witnesses: []tx.Witness{{Object: proofObject, Proof: proof}},
		}
		transaction.Salt[0] = salt
		if err := transaction.Sign(key); err != nil {
			t.Fatal(err)
		}
		return transaction
	}

	txA := makeTransfer(aliceKey, coinA, bob, 25, 74, 1)
	txC := makeTransfer(carolKey, coinC, dave, 50, 149, 2)
	newRoot, results, err := (BatchExecutor{Engine: Engine{Network: network, NativeToken: native, ShardCount: 1}, Workers: 2}).ApplyBatch(store, []tx.Transaction{txA, txC})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || newRoot == root {
		t.Fatalf("unexpected parallel result count/root")
	}
}

func TestParallelBatchRejectsSharedInput(t *testing.T) {
	root := types.HashBytes("root", []byte("same"))
	id := types.ObjectID(types.HashBytes("object", []byte("shared")))
	a := tx.Transaction{StateRoot: root, Inputs: []tx.InputRef{{ObjectID: id}}}
	b := tx.Transaction{StateRoot: root, Inputs: []tx.InputRef{{ObjectID: id}}}
	if err := validateIndependentBatch([]tx.Transaction{a, b}); err != ErrBatchConflict {
		t.Fatalf("expected batch conflict, got %v", err)
	}
}

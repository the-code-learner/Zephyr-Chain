package worldstate

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/state"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestObjectProofTracksIncrementalRoot(t *testing.T) {
	store := NewMemory()
	owner := types.AccountIDFromPublicKey([]byte("owner"))
	token := types.TokenID(types.HashBytes("token", []byte("zph")))
	txid := types.HashBytes("tx", []byte("seed"))
	id := types.ObjectIDFromTransaction(txid, 0)
	out, err := object.NewCoinOutput(owner, token, 10)
	if err != nil {
		t.Fatal(err)
	}
	o := object.Object{ID: id, Version: 1, Owner: out.Owner, Kind: out.Kind, Data: out.Data}
	root, err := store.Apply(nil, []object.Object{o})
	if err != nil {
		t.Fatal(err)
	}
	got, proof, ok := store.Proof(id)
	if !ok {
		t.Fatal("object missing")
	}
	h := got.Hash()
	if !state.Verify(root, types.Hash(id), h[:], proof) {
		t.Fatal("object proof failed")
	}
}

package tx

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestProofCarryingTransactionWireRoundTrip(t *testing.T) {
	privateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	network := types.NetworkID(types.HashBytes("network", []byte("test")))
	token := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	sender := types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), privateKey.PublicKey.X, privateKey.PublicKey.Y))
	store := worldstate.NewMemory()
	seed := types.HashBytes("seed", []byte("coin"))
	id := types.ObjectIDFromTransaction(seed, 0)
	coinOut, err := object.NewCoinOutput(sender, token, 100)
	if err != nil {
		t.Fatal(err)
	}
	coin := object.Object{ID: id, Version: 1, Owner: coinOut.Owner, Kind: coinOut.Kind, Data: coinOut.Data}
	root, err := store.Apply(nil, []object.Object{coin})
	if err != nil {
		t.Fatal(err)
	}
	got, proof, ok := store.Proof(id)
	if !ok {
		t.Fatal("missing coin")
	}
	h := got.Hash()
	recipient := types.AccountIDFromPublicKey([]byte("recipient"))
	out, err := object.NewCoinOutput(recipient, token, 99)
	if err != nil {
		t.Fatal(err)
	}

	transaction := Transaction{
		Version: Version, Network: network, ShardID: 0, StateRoot: root,
		Inputs:     []InputRef{{ObjectID: id, Version: 1, ObjectHash: h}},
		Outputs:    []object.OutputSpec{out},
		Operations: []Operation{{Kind: OpTransfer}},
		Fee:        1, ValidUntilHeight: 100,
		Witnesses: []Witness{{Object: got, Proof: proof}},
	}
	transaction.Salt[0] = 1
	if err := transaction.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	if err := transaction.ValidateStatic(); err != nil {
		t.Fatal(err)
	}
	if err := transaction.VerifyWitnesses(); err != nil {
		t.Fatal(err)
	}

	wire, err := transaction.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := ParseTransaction(wire)
	if err != nil {
		t.Fatal(err)
	}
	if decoded.ID() != transaction.ID() {
		t.Fatal("transaction ID changed after wire round-trip")
	}
	if err := decoded.VerifyWitnesses(); err != nil {
		t.Fatal(err)
	}
}

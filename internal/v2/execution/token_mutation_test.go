package execution

import (
	"crypto/elliptic"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/assets"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCustomTokenMintAndBurnLifecycle(t *testing.T) {
	aliceKey := makeKey(t)
	bobKey := makeKey(t)
	alice := types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), aliceKey.PublicKey.X, aliceKey.PublicKey.Y))
	bob := types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), bobKey.PublicKey.X, bobKey.PublicKey.Y))
	network := types.NetworkID(types.HashBytes("network", []byte("token-mutation")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	custom := types.TokenID(types.HashBytes("token", []byte("CUSTOM")))

	definition := assets.Definition{
		TokenID: custom, Name: "Custom", Symbol: "CUS", Decimals: 6,
		SupplyPolicy: assets.SupplyCapped, MaxSupply: 1_000, CurrentSupply: 500,
		MintAuthority: alice, Burnable: true, Transferable: true,
	}
	definitionRaw, err := definition.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	seed := types.HashBytes("token-mutation", []byte("objects"))
	definitionID := types.ObjectIDForShard(seed, 1, 0)
	aliceFeeID := types.ObjectIDForShard(seed, 2, 0)
	bobFeeID := types.ObjectIDForShard(seed, 3, 0)
	initialCoinID := types.ObjectIDForShard(seed, 4, 0)
	aliceFee, _ := object.NewCoinOutput(alice, native, 10)
	bobFee, _ := object.NewCoinOutput(bob, native, 10)
	initialCoin, _ := object.NewCoinOutput(alice, custom, 500)
	store := worldstate.NewMemory()
	_, err = store.Apply(nil, []object.Object{
		{ID: definitionID, Version: 1, Owner: alice, Kind: object.KindTokenDefinition, Data: definitionRaw},
		{ID: aliceFeeID, Version: 1, Owner: alice, Kind: object.KindCoin, Data: aliceFee.Data},
		{ID: bobFeeID, Version: 1, Owner: bob, Kind: object.KindCoin, Data: bobFee.Data},
		{ID: initialCoinID, Version: 1, Owner: alice, Kind: object.KindCoin, Data: initialCoin.Data},
	})
	if err != nil {
		t.Fatal(err)
	}

	mintPayload, _ := (assets.MintToken{DefinitionObject: definitionID, Recipient: bob, Amount: 100}).MarshalBinary()
	aliceChange, _ := object.NewCoinOutput(alice, native, 9)
	mint := transactionWithProofs(t, store, aliceKey, network, []types.ObjectID{definitionID, aliceFeeID}, []object.OutputSpec{aliceChange}, tx.Operation{Kind: tx.OpMintToken, Payload: mintPayload}, 1)
	engine := Engine{Network: network, NativeToken: native, ShardCount: 1}
	mintResult, err := engine.Execute(mint)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(mintResult.Consumed, mintResult.Created); err != nil {
		t.Fatal(err)
	}
	updatedDefinition, ok := store.GetObject(definitionID)
	if !ok {
		t.Fatal("mint removed token definition")
	}
	mintedDefinition, err := assets.ParseDefinition(updatedDefinition.Data)
	if err != nil || mintedDefinition.CurrentSupply != 600 || updatedDefinition.Version != 2 {
		t.Fatalf("unexpected definition after mint: %#v %v", mintedDefinition, err)
	}
	var bobMinted object.Object
	for _, created := range mintResult.Created {
		if created.Kind != object.KindCoin || created.Owner != bob {
			continue
		}
		coin, parseErr := object.ParseCoin(created.Data)
		if parseErr == nil && coin.Token == custom && coin.Amount == 100 {
			bobMinted = created
			break
		}
	}
	if types.IsZero32([32]byte(bobMinted.ID)) {
		t.Fatal("minted recipient coin missing")
	}

	burnPayload, _ := (assets.BurnToken{DefinitionObject: definitionID, Amount: 40}).MarshalBinary()
	bobNativeChange, _ := object.NewCoinOutput(bob, native, 9)
	bobTokenChange, _ := object.NewCoinOutput(bob, custom, 60)
	burn := transactionWithProofs(t, store, bobKey, network, []types.ObjectID{definitionID, bobFeeID, bobMinted.ID}, []object.OutputSpec{bobNativeChange, bobTokenChange}, tx.Operation{Kind: tx.OpBurnToken, Payload: burnPayload}, 1)
	burnResult, err := engine.Execute(burn)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(burnResult.Consumed, burnResult.Created); err != nil {
		t.Fatal(err)
	}
	updatedDefinition, ok = store.GetObject(definitionID)
	if !ok {
		t.Fatal("burn removed token definition")
	}
	burnedDefinition, err := assets.ParseDefinition(updatedDefinition.Data)
	if err != nil || burnedDefinition.CurrentSupply != 560 || updatedDefinition.Version != 3 {
		t.Fatalf("unexpected definition after burn: %#v %v", burnedDefinition, err)
	}
}

func TestFixedTokenCannotMint(t *testing.T) {
	definition := assets.Definition{SupplyPolicy: assets.SupplyFixed, MaxSupply: 100, CurrentSupply: 100}
	definition.TokenID[0] = 1
	definition.MintAuthority[0] = 2
	definition.Name = "Fixed"
	definition.Symbol = "FIX"
	if _, err := definition.Mint(1); err == nil {
		t.Fatal("fixed token unexpectedly minted")
	}
}

func transactionWithProofs(t *testing.T, store worldstate.Backend, key interface{ Public() any }, network types.NetworkID, ids []types.ObjectID, outputs []object.OutputSpec, operation tx.Operation, fee uint64) tx.Transaction {
	t.Helper()
	privateKey, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		t.Fatal("unexpected signing key")
	}
	transaction := tx.Transaction{Version: tx.Version, Network: network, ShardID: 0, StateRoot: store.Root(), Outputs: outputs, Operations: []tx.Operation{operation}, Fee: fee}
	for _, id := range ids {
		obj, proof, present := store.Proof(id)
		if !present {
			t.Fatalf("missing proof object %s", id.String())
		}
		transaction.Inputs = append(transaction.Inputs, tx.InputRef{ObjectID: id, Version: obj.Version, ObjectHash: obj.Hash()})
		transaction.Witnesses = append(transaction.Witnesses, tx.Witness{Object: obj, Proof: proof})
	}
	transaction.Salt[0] = byte(len(ids) + int(operation.Kind))
	if err := transaction.Sign(privateKey); err != nil {
		t.Fatal(err)
	}
	return transaction
}

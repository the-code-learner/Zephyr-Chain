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

func TestCustomTokenTransferRequiresTransferableDefinition(t *testing.T) {
	key := makeKey(t)
	sender := types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y))
	recipient := sender
	recipient[31] ^= 1
	network := types.NetworkID(types.HashBytes("network", []byte("token-policy")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	custom := types.TokenID(types.HashBytes("token", []byte("POLICY")))

	makeTransaction := func(transferable bool) (Engine, tx.Transaction) {
		definition := assets.Definition{
			TokenID: custom, Name: "Policy", Symbol: "PLC", SupplyPolicy: assets.SupplyFixed,
			MaxSupply: 100, CurrentSupply: 100, MintAuthority: sender, Burnable: true, Transferable: transferable,
		}
		definitionRaw, err := definition.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		seed := types.HashBytes("token-policy", []byte{byte(boolByte(transferable))})
		definitionID := types.ObjectIDForShard(seed, 1, 0)
		customID := types.ObjectIDForShard(seed, 2, 0)
		feeID := types.ObjectIDForShard(seed, 3, 0)
		customOut, _ := object.NewCoinOutput(sender, custom, 100)
		feeOut, _ := object.NewCoinOutput(sender, native, 10)
		store := worldstate.NewMemory()
		_, err = store.Apply(nil, []object.Object{
			{ID: definitionID, Version: 1, Owner: sender, Kind: object.KindTokenDefinition, Data: definitionRaw},
			{ID: customID, Version: 1, Owner: sender, Kind: object.KindCoin, Data: customOut.Data},
			{ID: feeID, Version: 1, Owner: sender, Kind: object.KindCoin, Data: feeOut.Data},
		})
		if err != nil {
			t.Fatal(err)
		}
		toRecipient, _ := object.NewCoinOutput(recipient, custom, 100)
		feeChange, _ := object.NewCoinOutput(sender, native, 9)
		transaction := transactionWithProofs(t, store, key, network, []types.ObjectID{customID, definitionID, feeID}, []object.OutputSpec{toRecipient, feeChange}, tx.Operation{Kind: tx.OpTransfer}, 1)
		return Engine{Network: network, NativeToken: native, ShardCount: 1}, transaction
	}

	engine, blocked := makeTransaction(false)
	if _, err := engine.Execute(blocked); err != ErrTokenPolicy {
		t.Fatalf("non-transferable token should be rejected, got %v", err)
	}
	engine, allowed := makeTransaction(true)
	result, err := engine.Execute(allowed)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Consumed) != 2 {
		t.Fatalf("definition witness must remain read-only; consumed=%d", len(result.Consumed))
	}
}

func TestCustomTokenCrossShardTransferRemainsActivationGated(t *testing.T) {
	var keyAccount types.AccountID
	key := makeKey(t)
	for attempts := 0; attempts < 64; attempts++ {
		keyAccount = types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y))
		if types.AccountShard(keyAccount, 2) == 0 {
			break
		}
		key = makeKey(t)
	}
	if types.AccountShard(keyAccount, 2) != 0 {
		t.Fatal("failed to generate shard-0 sender")
	}

	var recipient types.AccountID
	for attempts := 0; attempts < 64; attempts++ {
		recipientKey := makeKey(t)
		recipient = types.AccountIDFromPublicKey(elliptic.Marshal(elliptic.P256(), recipientKey.PublicKey.X, recipientKey.PublicKey.Y))
		if types.AccountShard(recipient, 2) == 1 {
			break
		}
	}
	if types.AccountShard(recipient, 2) != 1 {
		t.Fatal("failed to generate shard-1 recipient")
	}

	network := types.NetworkID(types.HashBytes("network", []byte("token-cross-shard")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	custom := types.TokenID(types.HashBytes("token", []byte("CUSTOM-X")))
	definition := assets.Definition{
		TokenID: custom, Name: "Cross", Symbol: "CRS", SupplyPolicy: assets.SupplyFixed,
		MaxSupply: 100, CurrentSupply: 100, MintAuthority: keyAccount, Transferable: true,
	}
	definitionRaw, _ := definition.MarshalBinary()
	seed := types.HashBytes("token-cross-shard", []byte("objects"))
	definitionID := types.ObjectIDForShard(seed, 1, 0)
	customID := types.ObjectIDForShard(seed, 2, 0)
	feeID := types.ObjectIDForShard(seed, 3, 0)
	customOut, _ := object.NewCoinOutput(keyAccount, custom, 100)
	feeOut, _ := object.NewCoinOutput(keyAccount, native, 10)
	store := worldstate.NewMemory()
	_, err := store.Apply(nil, []object.Object{
		{ID: definitionID, Version: 1, Owner: keyAccount, Kind: object.KindTokenDefinition, Data: definitionRaw},
		{ID: customID, Version: 1, Owner: keyAccount, Kind: object.KindCoin, Data: customOut.Data},
		{ID: feeID, Version: 1, Owner: keyAccount, Kind: object.KindCoin, Data: feeOut.Data},
	})
	if err != nil {
		t.Fatal(err)
	}
	remote, _ := object.NewCoinOutput(recipient, custom, 100)
	feeChange, _ := object.NewCoinOutput(keyAccount, native, 9)
	transaction := transactionWithProofs(t, store, key, network, []types.ObjectID{customID, definitionID, feeID}, []object.OutputSpec{remote, feeChange}, tx.Operation{Kind: tx.OpTransfer}, 1)
	_, err = (Engine{Network: network, NativeToken: native, ShardCount: 2}).Execute(transaction)
	if err != ErrTokenPolicy {
		t.Fatalf("custom token cross-shard transfer must remain gated, got %v", err)
	}
}

func boolByte(value bool) byte {
	if value {
		return 1
	}
	return 0
}

package execution

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestCrossShardLendingRejectsLateRepayment(t *testing.T) {
	const shardCount = uint32(2)
	network := types.NetworkID(types.HashBytes("network", []byte("cross-late-repay")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	_, lender := computeAccountOnShard(t, 0, shardCount)
	borrowerKey, borrower := computeAccountOnShard(t, 1, shardCount)
	store := worldstate.NewMemory()

	terms := lending.Terms{Lender: lender, Borrower: borrower, Principal: 50, Collateral: 30, TermRateBps: -1_000, MaturityHeight: 10}
	activationID := types.ObjectIDForShard(types.HashBytes("activation", []byte("late")), lending.CrossActivationObjectIndex, 1)
	acceptanceID := types.ObjectIDForShard(types.HashBytes("acceptance", []byte("late")), lending.CrossAcceptanceMessageIndex, 0)
	activation := lending.CrossActivation{AcceptanceObject: acceptanceID, LenderShard: 0, BorrowerShard: 1, AcceptedHeight: 3, Terms: terms}
	raw, err := activation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(nil, []object.Object{{ID: activationID, Version: 1, Owner: borrower, Kind: object.KindCrossLoanActivation, Data: raw}}); err != nil {
		t.Fatal(err)
	}
	coin := seedCrossLendingCoin(t, store, borrower, native, 50, 1, "late-repay")
	ref, _ := (lending.Ref{ObjectID: activationID}).MarshalBinary()
	transaction := computeSignedTx(t, store, borrowerKey, network, native, 1, []types.ObjectID{activationID, coin}, 4, tx.OpCrossLoanRepay, ref, 121)
	if _, err := (Engine{Network: network, NativeToken: native, ShardCount: shardCount, Height: 11}).Execute(transaction); !errors.Is(err, lending.ErrLendingState) {
		t.Fatalf("late repayment error = %v, want ErrLendingState", err)
	}
}

func TestCrossShardLendingDefaultAndOfferExpiryRequireGraceWindow(t *testing.T) {
	const shardCount = uint32(2)
	network := types.NetworkID(types.HashBytes("network", []byte("cross-grace")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lenderKey, lender := computeAccountOnShard(t, 0, shardCount)
	borrowerKey, borrower := computeAccountOnShard(t, 1, shardCount)
	terms := lending.Terms{Lender: lender, Borrower: borrower, Principal: 50, Collateral: 30, MaturityHeight: 10}

	t.Run("default", func(t *testing.T) {
		store := worldstate.NewMemory()
		activationID := types.ObjectIDForShard(types.HashBytes("activation", []byte("grace")), lending.CrossActivationObjectIndex, 1)
		acceptanceID := types.ObjectIDForShard(types.HashBytes("acceptance", []byte("grace")), lending.CrossAcceptanceMessageIndex, 0)
		acceptance := lending.CrossAcceptance{
			OfferObject:      types.ObjectIDForShard(types.HashBytes("offer", []byte("grace")), lending.CrossOfferMessageIndex, 1),
			ActivationObject: activationID, LenderShard: 0, BorrowerShard: 1, AcceptedHeight: 3, Terms: terms,
		}
		raw, err := acceptance.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Apply(nil, []object.Object{{ID: acceptanceID, Version: 1, Owner: lender, Kind: object.KindCrossLoanAcceptance, Data: raw}}); err != nil {
			t.Fatal(err)
		}
		feeCoin := seedCrossLendingCoin(t, store, lender, native, 10, 0, "default-fee")
		ref, _ := (lending.Ref{ObjectID: acceptanceID}).MarshalBinary()
		early := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{acceptanceID, feeCoin}, 9, tx.OpCrossLoanDefault, ref, 122)
		if _, err := (Engine{Network: network, NativeToken: native, ShardCount: shardCount, Height: 26}).Execute(early); !errors.Is(err, lending.ErrLendingState) {
			t.Fatalf("default at grace boundary error = %v, want ErrLendingState", err)
		}
		valid := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{acceptanceID, feeCoin}, 9, tx.OpCrossLoanDefault, ref, 123)
		result, err := (Engine{Network: network, NativeToken: native, ShardCount: shardCount, Height: 27}).Execute(valid)
		if err != nil {
			t.Fatal(err)
		}
		coinObjectByOwnerAmount(t, result.Created, lender, 30)
	})

	t.Run("offer expiry", func(t *testing.T) {
		store := worldstate.NewMemory()
		offerID := types.ObjectIDForShard(types.HashBytes("offer", []byte("expiry")), lending.CrossOfferMessageIndex, 1)
		offer := lending.CrossOffer{OfferObject: offerID, LenderShard: 0, BorrowerShard: 1, OfferedHeight: 1, Terms: terms}
		raw, err := offer.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.Apply(nil, []object.Object{{ID: offerID, Version: 1, Owner: borrower, Kind: object.KindCrossLoanOffer, Data: raw}}); err != nil {
			t.Fatal(err)
		}
		feeCoin := seedCrossLendingCoin(t, store, borrower, native, 10, 1, "expiry-fee")
		ref, _ := (lending.Ref{ObjectID: offerID}).MarshalBinary()
		early := computeSignedTx(t, store, borrowerKey, network, native, 1, []types.ObjectID{offerID, feeCoin}, 9, tx.OpCrossLoanExpire, ref, 124)
		if _, err := (Engine{Network: network, NativeToken: native, ShardCount: shardCount, Height: 26}).Execute(early); !errors.Is(err, lending.ErrLendingState) {
			t.Fatalf("expiry at grace boundary error = %v, want ErrLendingState", err)
		}
		valid := computeSignedTx(t, store, borrowerKey, network, native, 1, []types.ObjectID{offerID, feeCoin}, 9, tx.OpCrossLoanExpire, ref, 125)
		result, err := (Engine{Network: network, NativeToken: native, ShardCount: shardCount, Height: 27}).Execute(valid)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.Outbound) != 1 || result.Outbound[0].DestinationShard != 0 || result.Outbound[0].Output.Kind != object.KindCoin {
			t.Fatalf("principal return was not routed to lender shard: %+v", result.Outbound)
		}
		returned, err := object.ParseCoin(result.Outbound[0].Output.Data)
		if err != nil || returned.Amount != 50 {
			t.Fatalf("principal return = %+v err=%v", returned, err)
		}
	})
}

func seedCrossLendingCoin(t *testing.T, store *worldstate.Memory, owner types.AccountID, token types.TokenID, amount uint64, shard uint32, label string) types.ObjectID {
	t.Helper()
	spec, err := object.NewCoinOutputAtHeight(owner, token, amount, 1)
	if err != nil {
		t.Fatal(err)
	}
	id := types.ObjectIDForShard(types.HashBytes("cross-lending-seed", []byte(label)), 0, shard)
	if _, err := store.Apply(nil, []object.Object{{ID: id, Version: 1, Owner: owner, Kind: spec.Kind, Data: spec.Data}}); err != nil {
		t.Fatal(err)
	}
	return id
}

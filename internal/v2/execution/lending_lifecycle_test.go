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

func TestNativeLendingNegativeRateLifecycle(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("lending-negative")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lenderKey := makeKey(t)
	borrowerKey := makeKey(t)
	lender := accountForKey(lenderKey)
	borrower := accountForKey(borrowerKey)
	store := worldstate.NewMemory()

	lenderCoin := seedLendingCoin(t, store, lender, native, 1_000, "lender")
	borrowerCoin := seedLendingCoin(t, store, borrower, native, 500, "borrower")
	terms := lending.Terms{
		Lender:         lender,
		Borrower:       borrower,
		Principal:      300,
		Collateral:     150,
		TermRateBps:    -1_000,
		MaturityHeight: 50,
	}
	termsRaw, err := terms.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}

	offerTx := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{lenderCoin}, 699, tx.OpLoanOffer, termsRaw, 71)
	offerResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 1}).Execute(offerTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(offerResult.Consumed, offerResult.Created); err != nil {
		t.Fatal(err)
	}
	offer := objectOfKind(t, offerResult.Created, object.KindLoanOffer)
	lenderChange := coinObjectByOwnerAmount(t, offerResult.Created, lender, 699)

	refRaw, _ := (lending.Ref{ObjectID: offer.ID}).MarshalBinary()
	acceptTx := computeSignedTx(t, store, borrowerKey, network, native, 0, []types.ObjectID{offer.ID, borrowerCoin}, 349, tx.OpLoanAccept, refRaw, 72)
	acceptResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 10}).Execute(acceptTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(acceptResult.Consumed, acceptResult.Created); err != nil {
		t.Fatal(err)
	}
	loanObject := objectOfKind(t, acceptResult.Created, object.KindLoan)
	principalCoin := coinObjectByOwnerAmount(t, acceptResult.Created, borrower, 300)
	loan, err := lending.ParseLoan(loanObject.Data)
	if err != nil {
		t.Fatal(err)
	}
	if loan.Terms != terms || loan.AcceptedHeight != 10 {
		t.Fatalf("unexpected accepted loan: %+v", loan)
	}
	due, err := loan.Terms.RepaymentDue()
	if err != nil || due != 270 {
		t.Fatalf("negative-rate due = %d, err=%v; want 270", due, err)
	}

	loanRefRaw, _ := (lending.Ref{ObjectID: loanObject.ID}).MarshalBinary()
	repayTx := computeSignedTx(t, store, borrowerKey, network, native, 0, []types.ObjectID{loanObject.ID, principalCoin.ID}, 29, tx.OpLoanRepay, loanRefRaw, 73)
	repayResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 20}).Execute(repayTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(repayResult.Consumed, repayResult.Created); err != nil {
		t.Fatal(err)
	}

	coinObjectByOwnerAmount(t, repayResult.Created, lender, 270)
	coinObjectByOwnerAmount(t, repayResult.Created, borrower, 150)
	coinObjectByOwnerAmount(t, repayResult.Created, borrower, 29)
	if _, ok := store.GetObject(loanObject.ID); ok {
		t.Fatal("repaid loan object remained spendable")
	}

	// 699 lender change + 270 repayment = 969: lender funded the 30-ZPH
	// negative-rate subsidy and paid the 1-ZPH offer fee.
	lenderTotal := sumOwnerCoins(t, store, lender)
	if lenderTotal != 969 {
		t.Fatalf("lender final balance = %d, want 969", lenderTotal)
	}
	// 349 untouched accept change + 150 returned collateral + 29 repay change.
	borrowerTotal := sumOwnerCoins(t, store, borrower)
	if borrowerTotal != 528 {
		t.Fatalf("borrower final balance = %d, want 528", borrowerTotal)
	}
	_ = lenderChange
}

func TestNativeLendingDefaultOnlyAfterMaturity(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("lending-default")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lenderKey := makeKey(t)
	borrowerKey := makeKey(t)
	lender := accountForKey(lenderKey)
	borrower := accountForKey(borrowerKey)
	store := worldstate.NewMemory()

	lenderCoin := seedLendingCoin(t, store, lender, native, 1_000, "default-lender")
	borrowerCoin := seedLendingCoin(t, store, borrower, native, 500, "default-borrower")
	terms := lending.Terms{
		Lender:         lender,
		Borrower:       borrower,
		Principal:      300,
		Collateral:     200,
		TermRateBps:    500,
		MaturityHeight: 50,
	}
	raw, _ := terms.MarshalBinary()
	offerTx := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{lenderCoin}, 699, tx.OpLoanOffer, raw, 81)
	offerResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 1}).Execute(offerTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(offerResult.Consumed, offerResult.Created); err != nil {
		t.Fatal(err)
	}
	offer := objectOfKind(t, offerResult.Created, object.KindLoanOffer)
	lenderChange := coinObjectByOwnerAmount(t, offerResult.Created, lender, 699)

	refRaw, _ := (lending.Ref{ObjectID: offer.ID}).MarshalBinary()
	acceptTx := computeSignedTx(t, store, borrowerKey, network, native, 0, []types.ObjectID{offer.ID, borrowerCoin}, 299, tx.OpLoanAccept, refRaw, 82)
	acceptResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 10}).Execute(acceptTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(acceptResult.Consumed, acceptResult.Created); err != nil {
		t.Fatal(err)
	}
	loanObject := objectOfKind(t, acceptResult.Created, object.KindLoan)
	loanRef, _ := (lending.Ref{ObjectID: loanObject.ID}).MarshalBinary()

	premature := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{loanObject.ID, lenderChange.ID}, 698, tx.OpLoanDefault, loanRef, 83)
	if _, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 50}).Execute(premature); !errors.Is(err, lending.ErrLendingState) {
		t.Fatalf("default at maturity error = %v, want ErrLendingState", err)
	}

	defaultTx := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{loanObject.ID, lenderChange.ID}, 698, tx.OpLoanDefault, loanRef, 84)
	defaultResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 51}).Execute(defaultTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(defaultResult.Consumed, defaultResult.Created); err != nil {
		t.Fatal(err)
	}
	coinObjectByOwnerAmount(t, defaultResult.Created, lender, 200)
	if _, ok := store.GetObject(loanObject.ID); ok {
		t.Fatal("defaulted loan object remained spendable")
	}
}

func TestNativeLendingOfferCanBeCancelledByLender(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("lending-cancel")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lenderKey := makeKey(t)
	borrowerKey := makeKey(t)
	lender := accountForKey(lenderKey)
	borrower := accountForKey(borrowerKey)
	store := worldstate.NewMemory()

	lenderCoin := seedLendingCoin(t, store, lender, native, 500, "cancel-lender")
	terms := lending.Terms{Lender: lender, Borrower: borrower, Principal: 200, Collateral: 50, MaturityHeight: 100}
	raw, _ := terms.MarshalBinary()
	offerTx := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{lenderCoin}, 299, tx.OpLoanOffer, raw, 91)
	offerResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 1}).Execute(offerTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(offerResult.Consumed, offerResult.Created); err != nil {
		t.Fatal(err)
	}
	offer := objectOfKind(t, offerResult.Created, object.KindLoanOffer)
	change := coinObjectByOwnerAmount(t, offerResult.Created, lender, 299)

	refRaw, _ := (lending.Ref{ObjectID: offer.ID}).MarshalBinary()
	cancelTx := computeSignedTx(t, store, lenderKey, network, native, 0, []types.ObjectID{offer.ID, change.ID}, 298, tx.OpLoanCancel, refRaw, 92)
	cancelResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 2}).Execute(cancelTx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(cancelResult.Consumed, cancelResult.Created); err != nil {
		t.Fatal(err)
	}
	coinObjectByOwnerAmount(t, cancelResult.Created, lender, 200)
	if got := sumOwnerCoins(t, store, lender); got != 498 {
		t.Fatalf("lender balance after offer+cancel fees = %d, want 498", got)
	}
}

func accountForKey(key interface{ Public() any }) types.AccountID {
	panic("unreachable")
}

func seedLendingCoin(t *testing.T, store *worldstate.Memory, owner types.AccountID, token types.TokenID, amount uint64, label string) types.ObjectID {
	t.Helper()
	spec, err := object.NewCoinOutput(owner, token, amount)
	if err != nil {
		t.Fatal(err)
	}
	id := types.ObjectIDForShard(types.HashBytes("lending-seed", []byte(label)), 0, 0)
	if _, err := store.Apply(nil, []object.Object{{ID: id, Version: 1, Owner: owner, Kind: spec.Kind, Data: spec.Data}}); err != nil {
		t.Fatal(err)
	}
	return id
}

func coinObjectByOwnerAmount(t *testing.T, objects []object.Object, owner types.AccountID, amount uint64) object.Object {
	t.Helper()
	for _, item := range objects {
		if item.Kind != object.KindCoin || item.Owner != owner {
			continue
		}
		coin, err := object.ParseCoin(item.Data)
		if err == nil && coin.Amount == amount {
			return item
		}
	}
	t.Fatalf("coin owner=%s amount=%d not found", owner, amount)
	return object.Object{}
}

func sumOwnerCoins(t *testing.T, store *worldstate.Memory, owner types.AccountID) uint64 {
	t.Helper()
	var total uint64
	for _, item := range store.Objects() {
		if item.Kind != object.KindCoin || item.Owner != owner {
			continue
		}
		coin, err := object.ParseCoin(item.Data)
		if err != nil {
			t.Fatal(err)
		}
		total += coin.Amount
	}
	return total
}

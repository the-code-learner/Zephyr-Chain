package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/lending"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCrossLendingReceiptsCarryExactNativeValue(t *testing.T) {
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lender := types.AccountID(types.HashBytes("account", []byte("lender")))
	borrower := types.AccountID(types.HashBytes("account", []byte("borrower")))
	terms := lending.Terms{Lender: lender, Borrower: borrower, Principal: 50, Collateral: 30, TermRateBps: -1_000, MaturityHeight: 100}
	offerID := types.ObjectIDForShard(types.HashBytes("offer", []byte("1")), lending.CrossOfferMessageIndex, 1)
	offer := lending.CrossOffer{OfferObject: offerID, LenderShard: 0, BorrowerShard: 1, OfferedHeight: 10, Terms: terms}
	offerRaw, _ := offer.MarshalBinary()
	offerReceipt := crossLendingReceipt(t, 0, 1, 10, lending.CrossOfferMessageIndex,
		object.OutputSpec{Owner: borrower, Kind: object.KindCrossLoanOffer, Data: offerRaw})
	if amount, ok, err := finalizedReceiptNativeValue(offerReceipt, native); err != nil || !ok || amount != 50 {
		t.Fatalf("offer value = %d ok=%v err=%v", amount, ok, err)
	}

	activationID := types.ObjectIDForShard(types.HashBytes("accept", []byte("1")), lending.CrossActivationObjectIndex, 1)
	acceptance := lending.CrossAcceptance{OfferObject: offerID, ActivationObject: activationID, LenderShard: 0, BorrowerShard: 1, AcceptedHeight: 20, Terms: terms}
	acceptanceRaw, _ := acceptance.MarshalBinary()
	acceptanceReceipt := crossLendingReceipt(t, 1, 0, 20, lending.CrossAcceptanceMessageIndex,
		object.OutputSpec{Owner: lender, Kind: object.KindCrossLoanAcceptance, Data: acceptanceRaw})
	if amount, ok, err := finalizedReceiptNativeValue(acceptanceReceipt, native); err != nil || !ok || amount != 30 {
		t.Fatalf("acceptance value = %d ok=%v err=%v", amount, ok, err)
	}

	termsHash, _ := terms.Hash()
	acceptanceID := types.ObjectIDForShard(types.HashBytes("accept", []byte("1")), lending.CrossAcceptanceMessageIndex, 0)
	repayment := lending.CrossRepayment{
		AcceptanceObject: acceptanceID, ActivationObject: activationID,
		LenderShard: 0, BorrowerShard: 1, Amount: 45, PaidHeight: 50, TermsHash: termsHash,
	}
	repaymentRaw, _ := repayment.MarshalBinary()
	repaymentReceipt := crossLendingReceipt(t, 1, 0, 50, lending.CrossRepaymentMessageIndex,
		object.OutputSpec{Owner: lender, Kind: object.KindCrossLoanRepayment, Data: repaymentRaw})
	if amount, ok, err := finalizedReceiptNativeValue(repaymentReceipt, native); err != nil || !ok || amount != 45 {
		t.Fatalf("repayment value = %d ok=%v err=%v", amount, ok, err)
	}
}

func TestCrossLendingReceiptImportMovesSupplyAndBootstrapsLineage(t *testing.T) {
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	lender := types.AccountID(types.HashBytes("account", []byte("lender")))
	borrower := types.AccountID(types.HashBytes("account", []byte("borrower")))
	terms := lending.Terms{Lender: lender, Borrower: borrower, Principal: 50, Collateral: 30, MaturityHeight: 100}
	offerID := types.ObjectIDForShard(types.HashBytes("offer", []byte("supply")), lending.CrossOfferMessageIndex, 1)
	offer := lending.CrossOffer{OfferObject: offerID, LenderShard: 0, BorrowerShard: 1, OfferedHeight: 10, Terms: terms}
	raw, _ := offer.MarshalBinary()
	receipt := crossLendingReceipt(t, 0, 1, 10, lending.CrossOfferMessageIndex,
		object.OutputSpec{Owner: borrower, Kind: object.KindCrossLoanOffer, Data: raw})

	cfg := testCollectorConfig(2, native)
	cfg.InitialCirculatingSupply[0] = 100
	cfg.InitialCirculatingSupply[1] = 0
	collector, err := NewEpochCollector(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := collector.observeImportedReceipt(receipt); err != nil {
		t.Fatal(err)
	}
	source, _ := collector.CirculatingSupply(0)
	destination, _ := collector.CirculatingSupply(1)
	if source != 50 || destination != 50 {
		t.Fatalf("unexpected shard supply after lending receipt import: source=%d destination=%d", source, destination)
	}

	tracker := NewIdleCapitalTracker()
	applied, err := ApplyFinalizedIdleCapitalImport(tracker, native, receipt)
	if err != nil || !applied {
		t.Fatalf("lending receipt lineage import applied=%v err=%v", applied, err)
	}
	destinationObject, err := receipt.DestinationObject()
	if err != nil {
		t.Fatal(err)
	}
	lots, ok := tracker.ObjectLots(destinationObject.ID)
	if !ok || len(lots) != 1 || lots[0].Amount != 50 || lots[0].IdleSinceHeight != 10 {
		t.Fatalf("unexpected imported lending lineage: %#v", lots)
	}
}

func crossLendingReceipt(t *testing.T, source, destination uint32, height uint64, index uint32, output object.OutputSpec) sharding.CrossShardReceipt {
	t.Helper()
	receipt := sharding.CrossShardReceipt{
		SourceShard: source, DestinationShard: destination, SourceHeight: height,
		TransactionID: types.HashBytes("cross-lending-receipt", []byte{byte(source), byte(destination), byte(index)}),
		OutputIndex: index, Output: output, SourceStateRoot: types.HashBytes("state", []byte{byte(height)}),
	}
	if err := receipt.Validate(); err != nil {
		t.Fatal(err)
	}
	return receipt
}

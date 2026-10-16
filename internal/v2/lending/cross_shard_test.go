package lending

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCrossShardMessagesRoundTripAndBindTerms(t *testing.T) {
	terms := Terms{
		Lender:    types.AccountID(types.HashBytes("account", []byte("lender"))),
		Borrower:  types.AccountID(types.HashBytes("account", []byte("borrower"))),
		Principal: 100, Collateral: 40, TermRateBps: -500, MaturityHeight: 100,
	}
	offerID := types.ObjectIDForShard(types.HashBytes("tx", []byte("offer")), CrossOfferMessageIndex, 1)
	offer := CrossOffer{OfferObject: offerID, LenderShard: 0, BorrowerShard: 1, OfferedHeight: 10, Terms: terms}
	raw, err := offer.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedOffer, err := ParseCrossOffer(raw)
	if err != nil || parsedOffer != offer {
		t.Fatalf("offer round-trip mismatch: %+v %v", parsedOffer, err)
	}

	activationID := types.ObjectIDForShard(types.HashBytes("tx", []byte("accept")), CrossActivationObjectIndex, 1)
	acceptance := CrossAcceptance{
		OfferObject: offerID, ActivationObject: activationID,
		LenderShard: 0, BorrowerShard: 1, AcceptedHeight: 20, Terms: terms,
	}
	raw, err = acceptance.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedAcceptance, err := ParseCrossAcceptance(raw)
	if err != nil || parsedAcceptance != acceptance {
		t.Fatalf("acceptance round-trip mismatch: %+v %v", parsedAcceptance, err)
	}

	acceptanceID := types.ObjectIDForShard(types.HashBytes("tx", []byte("accept")), CrossAcceptanceMessageIndex, 0)
	activation := CrossActivation{
		AcceptanceObject: acceptanceID, LenderShard: 0, BorrowerShard: 1,
		AcceptedHeight: 20, Terms: terms,
	}
	raw, err = activation.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedActivation, err := ParseCrossActivation(raw)
	if err != nil || parsedActivation != activation {
		t.Fatalf("activation round-trip mismatch: %+v %v", parsedActivation, err)
	}

	termsHash, err := terms.Hash()
	if err != nil {
		t.Fatal(err)
	}
	repayment := CrossRepayment{
		AcceptanceObject: acceptanceID, ActivationObject: activationID,
		LenderShard: 0, BorrowerShard: 1, Amount: 95, PaidHeight: 50, TermsHash: termsHash,
	}
	raw, err = repayment.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsedRepayment, err := ParseCrossRepayment(raw)
	if err != nil || parsedRepayment != repayment {
		t.Fatalf("repayment round-trip mismatch: %+v %v", parsedRepayment, err)
	}

	changed := terms
	changed.Collateral++
	changedHash, err := changed.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if changedHash == termsHash || SameTerms(terms, changed) {
		t.Fatal("terms hash did not bind collateral")
	}
}

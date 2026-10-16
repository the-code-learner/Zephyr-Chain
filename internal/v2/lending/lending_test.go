package lending

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func lendingAccount(label string) types.AccountID {
	return types.AccountID(types.HashBytes("lending-account", []byte(label)))
}

func TestRepaymentDueSupportsPositiveZeroAndNegativeRates(t *testing.T) {
	base := Terms{
		Lender:         lendingAccount("lender"),
		Borrower:       lendingAccount("borrower"),
		Principal:      1_000,
		Collateral:     500,
		MaturityHeight: 100,
	}
	cases := []struct {
		name string
		rate int32
		want uint64
	}{
		{name: "positive", rate: 750, want: 1_075},
		{name: "zero", rate: 0, want: 1_000},
		{name: "negative", rate: -1_000, want: 900},
		{name: "negative truncates toward zero", rate: -333, want: 967},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			terms := base
			terms.TermRateBps = tc.rate
			got, err := terms.RepaymentDue()
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("repayment due = %d, want %d", got, tc.want)
			}
			raw, err := terms.MarshalBinary()
			if err != nil {
				t.Fatal(err)
			}
			parsed, err := ParseTerms(raw)
			if err != nil {
				t.Fatal(err)
			}
			if parsed != terms {
				t.Fatalf("round-trip mismatch: got %+v want %+v", parsed, terms)
			}
		})
	}
}

func TestTermsRejectZeroOrEconomicallyDegenerateRepayment(t *testing.T) {
	base := Terms{
		Lender:         lendingAccount("lender"),
		Borrower:       lendingAccount("borrower"),
		Principal:      100,
		MaturityHeight: 10,
	}
	for _, rate := range []int32{-10_000, -20_000, MaxPositiveRateBps + 1} {
		terms := base
		terms.TermRateBps = rate
		if err := terms.Validate(); !errors.Is(err, ErrLending) {
			t.Fatalf("rate %d error = %v, want ErrLending", rate, err)
		}
	}
}

func TestLoanCanonicalRoundTrip(t *testing.T) {
	terms := Terms{
		Lender:         lendingAccount("lender"),
		Borrower:       lendingAccount("borrower"),
		Principal:      500,
		Collateral:     250,
		TermRateBps:    -250,
		MaturityHeight: 200,
	}
	loan := Loan{
		OfferObject:    types.ObjectID(types.HashBytes("offer", []byte("1"))),
		Terms:          terms,
		AcceptedHeight: 20,
	}
	raw, err := loan.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseLoan(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed != loan {
		t.Fatalf("round-trip mismatch: got %+v want %+v", parsed, loan)
	}
}

package lending

import (
	"errors"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	Version            uint16 = 1
	MaxPositiveRateBps int32  = 100_000
	MinNegativeRateBps int32  = -9_999
)

var (
	ErrLending      = errors.New("invalid native lending object")
	ErrLendingState = errors.New("invalid native lending state transition")
)

type Terms struct {
	Lender         types.AccountID
	Borrower       types.AccountID
	Principal      uint64
	Collateral     uint64
	TermRateBps    int32
	MaturityHeight uint64
}

func (t Terms) Validate() error {
	if types.IsZero32([32]byte(t.Lender)) || types.IsZero32([32]byte(t.Borrower)) || t.Lender == t.Borrower {
		return ErrLending
	}
	if t.Principal == 0 || t.MaturityHeight == 0 {
		return ErrLending
	}
	if t.TermRateBps < MinNegativeRateBps || t.TermRateBps > MaxPositiveRateBps {
		return ErrLending
	}
	if _, err := t.RepaymentDue(); err != nil {
		return err
	}
	return nil
}

// RepaymentDue returns the exact ZPH principal settlement for the full bilateral
// term. TermRateBps is a fixed whole-contract rate, not an APR. Integer division
// truncates toward zero, including for negative rates.
func (t Terms) RepaymentDue() (uint64, error) {
	if t.Principal == 0 || t.TermRateBps < MinNegativeRateBps || t.TermRateBps > MaxPositiveRateBps {
		return 0, ErrLending
	}
	principal := new(big.Int).SetUint64(t.Principal)
	interest := new(big.Int).Mul(new(big.Int).Set(principal), big.NewInt(int64(t.TermRateBps)))
	interest.Quo(interest, big.NewInt(10_000))
	due := new(big.Int).Add(principal, interest)
	if due.Sign() <= 0 || due.BitLen() > 64 {
		return 0, ErrLending
	}
	return due.Uint64(), nil
}

func (t Terms) MarshalBinary() ([]byte, error) {
	if err := t.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(t.Lender[:])
	w.Fixed(t.Borrower[:])
	w.U64(t.Principal)
	w.U64(t.Collateral)
	w.U32(uint32(t.TermRateBps))
	w.U64(t.MaturityHeight)
	return w.BytesCopy(), nil
}

func ParseTerms(data []byte) (Terms, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Terms{}, ErrLending
	}
	lenderRaw, err := r.Fixed(32)
	if err != nil {
		return Terms{}, ErrLending
	}
	borrowerRaw, err := r.Fixed(32)
	if err != nil {
		return Terms{}, ErrLending
	}
	principal, err := r.U64()
	if err != nil {
		return Terms{}, ErrLending
	}
	collateral, err := r.U64()
	if err != nil {
		return Terms{}, ErrLending
	}
	rateRaw, err := r.U32()
	if err != nil {
		return Terms{}, ErrLending
	}
	maturity, err := r.U64()
	if err != nil || r.Done() != nil {
		return Terms{}, ErrLending
	}
	var lender, borrower types.AccountID
	copy(lender[:], lenderRaw)
	copy(borrower[:], borrowerRaw)
	out := Terms{
		Lender:         lender,
		Borrower:       borrower,
		Principal:      principal,
		Collateral:     collateral,
		TermRateBps:    int32(rateRaw),
		MaturityHeight: maturity,
	}
	if err := out.Validate(); err != nil {
		return Terms{}, err
	}
	return out, nil
}

type Loan struct {
	OfferObject    types.ObjectID
	Terms          Terms
	AcceptedHeight uint64
}

func (l Loan) Validate() error {
	if types.IsZero32([32]byte(l.OfferObject)) || l.AcceptedHeight == 0 {
		return ErrLending
	}
	if err := l.Terms.Validate(); err != nil {
		return err
	}
	if l.AcceptedHeight >= l.Terms.MaturityHeight {
		return ErrLending
	}
	return nil
}

func (l Loan) MarshalBinary() ([]byte, error) {
	if err := l.Validate(); err != nil {
		return nil, err
	}
	terms, err := l.Terms.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(l.OfferObject[:])
	w.Bytes(terms)
	w.U64(l.AcceptedHeight)
	return w.BytesCopy(), nil
}

func ParseLoan(data []byte) (Loan, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Loan{}, ErrLending
	}
	offerRaw, err := r.Fixed(32)
	if err != nil {
		return Loan{}, ErrLending
	}
	termsRaw, err := r.Bytes(256)
	if err != nil {
		return Loan{}, ErrLending
	}
	accepted, err := r.U64()
	if err != nil || r.Done() != nil {
		return Loan{}, ErrLending
	}
	terms, err := ParseTerms(termsRaw)
	if err != nil {
		return Loan{}, err
	}
	var offer types.ObjectID
	copy(offer[:], offerRaw)
	out := Loan{OfferObject: offer, Terms: terms, AcceptedHeight: accepted}
	if err := out.Validate(); err != nil {
		return Loan{}, err
	}
	return out, nil
}

type Ref struct {
	ObjectID types.ObjectID
}

func (r Ref) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(r.ObjectID)) {
		return nil, ErrLending
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(r.ObjectID[:])
	return w.BytesCopy(), nil
}

func ParseRef(data []byte) (Ref, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Ref{}, ErrLending
	}
	raw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return Ref{}, ErrLending
	}
	var id types.ObjectID
	copy(id[:], raw)
	if types.IsZero32([32]byte(id)) {
		return Ref{}, ErrLending
	}
	return Ref{ObjectID: id}, nil
}

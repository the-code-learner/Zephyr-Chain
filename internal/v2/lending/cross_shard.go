package lending

import (
	"bytes"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	CrossShardVersion uint16 = 1

	CrossOfferMessageIndex      uint32 = 0xD0000100
	CrossAcceptanceMessageIndex uint32 = 0xD0000101
	CrossPrincipalObjectIndex   uint32 = 0xD0000110
	CrossActivationObjectIndex  uint32 = 0xD0000111
	CrossRepaymentMessageIndex  uint32 = 0xD0000120
	CrossCollateralReturnIndex  uint32 = 0xD0000130
	CrossPrincipalReturnIndex   uint32 = 0xD0000131

	// CrossShardSettlementGraceBlocks is a deterministic receipt-import window.
	// It reduces ordinary import-latency races but is not a censorship-proof
	// fair-exchange mechanism.
	CrossShardSettlementGraceBlocks uint64 = 16
)

type CrossOffer struct {
	OfferObject   types.ObjectID
	LenderShard   uint32
	BorrowerShard uint32
	OfferedHeight uint64
	Terms         Terms
}

func (m CrossOffer) Validate() error {
	if types.IsZero32([32]byte(m.OfferObject)) || m.LenderShard == m.BorrowerShard ||
		m.OfferedHeight == 0 || m.OfferedHeight >= m.Terms.MaturityHeight || m.Terms.Validate() != nil {
		return ErrLending
	}
	return nil
}

func (m CrossOffer) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	terms, _ := m.Terms.MarshalBinary()
	var w codec.Writer
	w.U16(CrossShardVersion)
	w.Fixed(m.OfferObject[:])
	w.U32(m.LenderShard)
	w.U32(m.BorrowerShard)
	w.U64(m.OfferedHeight)
	w.Bytes(terms)
	return w.BytesCopy(), nil
}

func ParseCrossOffer(data []byte) (CrossOffer, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != CrossShardVersion {
		return CrossOffer{}, ErrLending
	}
	idRaw, err := r.Fixed(32)
	if err != nil {
		return CrossOffer{}, ErrLending
	}
	lenderShard, err := r.U32()
	if err != nil {
		return CrossOffer{}, ErrLending
	}
	borrowerShard, err := r.U32()
	if err != nil {
		return CrossOffer{}, ErrLending
	}
	offered, err := r.U64()
	if err != nil {
		return CrossOffer{}, ErrLending
	}
	termsRaw, err := r.Bytes(256)
	if err != nil || r.Done() != nil {
		return CrossOffer{}, ErrLending
	}
	terms, err := ParseTerms(termsRaw)
	if err != nil {
		return CrossOffer{}, err
	}
	var id types.ObjectID
	copy(id[:], idRaw)
	out := CrossOffer{OfferObject: id, LenderShard: lenderShard, BorrowerShard: borrowerShard, OfferedHeight: offered, Terms: terms}
	if err := out.Validate(); err != nil {
		return CrossOffer{}, err
	}
	return out, nil
}

type CrossAcceptance struct {
	OfferObject      types.ObjectID
	ActivationObject types.ObjectID
	LenderShard      uint32
	BorrowerShard    uint32
	AcceptedHeight   uint64
	Terms            Terms
}

func (m CrossAcceptance) Validate() error {
	if types.IsZero32([32]byte(m.OfferObject)) || types.IsZero32([32]byte(m.ActivationObject)) ||
		m.LenderShard == m.BorrowerShard || m.AcceptedHeight == 0 ||
		m.AcceptedHeight >= m.Terms.MaturityHeight || m.Terms.Validate() != nil {
		return ErrLending
	}
	return nil
}

func (m CrossAcceptance) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	terms, _ := m.Terms.MarshalBinary()
	var w codec.Writer
	w.U16(CrossShardVersion)
	w.Fixed(m.OfferObject[:])
	w.Fixed(m.ActivationObject[:])
	w.U32(m.LenderShard)
	w.U32(m.BorrowerShard)
	w.U64(m.AcceptedHeight)
	w.Bytes(terms)
	return w.BytesCopy(), nil
}

func ParseCrossAcceptance(data []byte) (CrossAcceptance, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != CrossShardVersion {
		return CrossAcceptance{}, ErrLending
	}
	offerRaw, err := r.Fixed(32)
	if err != nil {
		return CrossAcceptance{}, ErrLending
	}
	activationRaw, err := r.Fixed(32)
	if err != nil {
		return CrossAcceptance{}, ErrLending
	}
	lenderShard, err := r.U32()
	if err != nil {
		return CrossAcceptance{}, ErrLending
	}
	borrowerShard, err := r.U32()
	if err != nil {
		return CrossAcceptance{}, ErrLending
	}
	accepted, err := r.U64()
	if err != nil {
		return CrossAcceptance{}, ErrLending
	}
	termsRaw, err := r.Bytes(256)
	if err != nil || r.Done() != nil {
		return CrossAcceptance{}, ErrLending
	}
	terms, err := ParseTerms(termsRaw)
	if err != nil {
		return CrossAcceptance{}, err
	}
	var offer, activation types.ObjectID
	copy(offer[:], offerRaw)
	copy(activation[:], activationRaw)
	out := CrossAcceptance{
		OfferObject: offer, ActivationObject: activation,
		LenderShard: lenderShard, BorrowerShard: borrowerShard,
		AcceptedHeight: accepted, Terms: terms,
	}
	if err := out.Validate(); err != nil {
		return CrossAcceptance{}, err
	}
	return out, nil
}

type CrossActivation struct {
	AcceptanceObject types.ObjectID
	LenderShard      uint32
	BorrowerShard    uint32
	AcceptedHeight   uint64
	Terms            Terms
}

func (m CrossActivation) Validate() error {
	if types.IsZero32([32]byte(m.AcceptanceObject)) || m.LenderShard == m.BorrowerShard ||
		m.AcceptedHeight == 0 || m.AcceptedHeight >= m.Terms.MaturityHeight || m.Terms.Validate() != nil {
		return ErrLending
	}
	return nil
}

func (m CrossActivation) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	terms, _ := m.Terms.MarshalBinary()
	var w codec.Writer
	w.U16(CrossShardVersion)
	w.Fixed(m.AcceptanceObject[:])
	w.U32(m.LenderShard)
	w.U32(m.BorrowerShard)
	w.U64(m.AcceptedHeight)
	w.Bytes(terms)
	return w.BytesCopy(), nil
}

func ParseCrossActivation(data []byte) (CrossActivation, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != CrossShardVersion {
		return CrossActivation{}, ErrLending
	}
	acceptanceRaw, err := r.Fixed(32)
	if err != nil {
		return CrossActivation{}, ErrLending
	}
	lenderShard, err := r.U32()
	if err != nil {
		return CrossActivation{}, ErrLending
	}
	borrowerShard, err := r.U32()
	if err != nil {
		return CrossActivation{}, ErrLending
	}
	accepted, err := r.U64()
	if err != nil {
		return CrossActivation{}, ErrLending
	}
	termsRaw, err := r.Bytes(256)
	if err != nil || r.Done() != nil {
		return CrossActivation{}, ErrLending
	}
	terms, err := ParseTerms(termsRaw)
	if err != nil {
		return CrossActivation{}, err
	}
	var acceptance types.ObjectID
	copy(acceptance[:], acceptanceRaw)
	out := CrossActivation{
		AcceptanceObject: acceptance, LenderShard: lenderShard,
		BorrowerShard: borrowerShard, AcceptedHeight: accepted, Terms: terms,
	}
	if err := out.Validate(); err != nil {
		return CrossActivation{}, err
	}
	return out, nil
}

type CrossRepayment struct {
	AcceptanceObject types.ObjectID
	ActivationObject types.ObjectID
	LenderShard      uint32
	BorrowerShard    uint32
	Amount           uint64
	PaidHeight       uint64
	TermsHash        types.Hash
}

func (m CrossRepayment) Validate() error {
	if types.IsZero32([32]byte(m.AcceptanceObject)) || types.IsZero32([32]byte(m.ActivationObject)) ||
		m.LenderShard == m.BorrowerShard || m.Amount == 0 || m.PaidHeight == 0 ||
		types.IsZero32([32]byte(m.TermsHash)) {
		return ErrLending
	}
	return nil
}

func (m CrossRepayment) MarshalBinary() ([]byte, error) {
	if err := m.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(CrossShardVersion)
	w.Fixed(m.AcceptanceObject[:])
	w.Fixed(m.ActivationObject[:])
	w.U32(m.LenderShard)
	w.U32(m.BorrowerShard)
	w.U64(m.Amount)
	w.U64(m.PaidHeight)
	w.Fixed(m.TermsHash[:])
	return w.BytesCopy(), nil
}

func ParseCrossRepayment(data []byte) (CrossRepayment, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != CrossShardVersion {
		return CrossRepayment{}, ErrLending
	}
	acceptanceRaw, err := r.Fixed(32)
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	activationRaw, err := r.Fixed(32)
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	lenderShard, err := r.U32()
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	borrowerShard, err := r.U32()
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	amount, err := r.U64()
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	paidHeight, err := r.U64()
	if err != nil {
		return CrossRepayment{}, ErrLending
	}
	hashRaw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return CrossRepayment{}, ErrLending
	}
	var acceptance, activation types.ObjectID
	var termsHash types.Hash
	copy(acceptance[:], acceptanceRaw)
	copy(activation[:], activationRaw)
	copy(termsHash[:], hashRaw)
	out := CrossRepayment{
		AcceptanceObject: acceptance, ActivationObject: activation,
		LenderShard: lenderShard, BorrowerShard: borrowerShard,
		Amount: amount, PaidHeight: paidHeight, TermsHash: termsHash,
	}
	if err := out.Validate(); err != nil {
		return CrossRepayment{}, err
	}
	return out, nil
}

func (t Terms) Hash() (types.Hash, error) {
	raw, err := t.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/lending-terms/v1", raw)), nil
}

func SameTerms(a, b Terms) bool {
	left, err := a.MarshalBinary()
	if err != nil {
		return false
	}
	right, err := b.MarshalBinary()
	return err == nil && bytes.Equal(left, right)
}

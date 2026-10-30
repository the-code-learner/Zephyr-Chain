package devicecredential

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"errors"
	"math/big"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	Version uint16 = 1

	CapabilityVerifyHeaders uint32 = 1 << iota
	CapabilityRelay
	CapabilitySampleDA
	CapabilityExecuteRecent
	CapabilityServeCache

	SupportedCapabilities = CapabilityVerifyHeaders |
		CapabilityRelay |
		CapabilitySampleDA |
		CapabilityExecuteRecent |
		CapabilityServeCache

	ObjectIndex uint32 = 0xD1000000
)

var (
	ErrCredential = errors.New("invalid device credential")
	ErrProof      = errors.New("invalid device credential proof")
)

type Spec struct {
	DevicePublicKey []byte
	Capabilities    uint32
	ExpiresHeight   uint64
}

func (s Spec) Validate() error {
	if !validPublicKey(s.DevicePublicKey) || s.Capabilities == 0 || s.Capabilities&^SupportedCapabilities != 0 {
		return ErrCredential
	}
	return nil
}

func (s Spec) MarshalBinary() ([]byte, error) {
	if err := s.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Bytes(s.DevicePublicKey)
	w.U32(s.Capabilities)
	w.U64(s.ExpiresHeight)
	return w.BytesCopy(), nil
}

func ParseSpec(data []byte) (Spec, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Spec{}, ErrCredential
	}
	key, err := r.Bytes(65)
	if err != nil {
		return Spec{}, ErrCredential
	}
	capabilities, err := r.U32()
	if err != nil {
		return Spec{}, ErrCredential
	}
	expires, err := r.U64()
	if err != nil || r.Done() != nil {
		return Spec{}, ErrCredential
	}
	out := Spec{DevicePublicKey: key, Capabilities: capabilities, ExpiresHeight: expires}
	if err := out.Validate(); err != nil {
		return Spec{}, err
	}
	return out, nil
}

type Credential struct {
	Owner           types.AccountID
	DevicePublicKey []byte
	Capabilities    uint32
	IssuedHeight    uint64
	ExpiresHeight   uint64
	Sequence        uint64
}

func (c Credential) Validate() error {
	if types.IsZero32([32]byte(c.Owner)) || c.IssuedHeight == 0 || c.Sequence == 0 ||
		!validPublicKey(c.DevicePublicKey) || c.Capabilities == 0 || c.Capabilities&^SupportedCapabilities != 0 ||
		(c.ExpiresHeight != 0 && c.ExpiresHeight <= c.IssuedHeight) {
		return ErrCredential
	}
	return nil
}

func (c Credential) ActiveAt(height uint64) bool {
	if c.Validate() != nil || height < c.IssuedHeight {
		return false
	}
	return c.ExpiresHeight == 0 || height < c.ExpiresHeight
}

func (c Credential) HasCapability(capability uint32) bool {
	return capability != 0 && capability&^SupportedCapabilities == 0 && c.Capabilities&capability == capability
}

func (c Credential) MarshalBinary() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(c.Owner[:])
	w.Bytes(c.DevicePublicKey)
	w.U32(c.Capabilities)
	w.U64(c.IssuedHeight)
	w.U64(c.ExpiresHeight)
	w.U64(c.Sequence)
	return w.BytesCopy(), nil
}

func ParseCredential(data []byte) (Credential, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Credential{}, ErrCredential
	}
	ownerRaw, err := r.Fixed(32)
	if err != nil {
		return Credential{}, ErrCredential
	}
	key, err := r.Bytes(65)
	if err != nil {
		return Credential{}, ErrCredential
	}
	capabilities, err := r.U32()
	if err != nil {
		return Credential{}, ErrCredential
	}
	issued, err := r.U64()
	if err != nil {
		return Credential{}, ErrCredential
	}
	expires, err := r.U64()
	if err != nil {
		return Credential{}, ErrCredential
	}
	sequence, err := r.U64()
	if err != nil || r.Done() != nil {
		return Credential{}, ErrCredential
	}
	var owner types.AccountID
	copy(owner[:], ownerRaw)
	out := Credential{
		Owner: owner, DevicePublicKey: key, Capabilities: capabilities,
		IssuedHeight: issued, ExpiresHeight: expires, Sequence: sequence,
	}
	if err := out.Validate(); err != nil {
		return Credential{}, err
	}
	return out, nil
}

type Update struct {
	ObjectID types.ObjectID
	Spec     Spec
}

func (u Update) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(u.ObjectID)) {
		return nil, ErrCredential
	}
	spec, err := u.Spec.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(Version)
	w.Fixed(u.ObjectID[:])
	w.Bytes(spec)
	return w.BytesCopy(), nil
}

func ParseUpdate(data []byte) (Update, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != Version {
		return Update{}, ErrCredential
	}
	raw, err := r.Fixed(32)
	if err != nil {
		return Update{}, ErrCredential
	}
	specRaw, err := r.Bytes(256)
	if err != nil || r.Done() != nil {
		return Update{}, ErrCredential
	}
	var id types.ObjectID
	copy(id[:], raw)
	spec, err := ParseSpec(specRaw)
	if err != nil || types.IsZero32([32]byte(id)) {
		return Update{}, ErrCredential
	}
	return Update{ObjectID: id, Spec: spec}, nil
}

type Ref struct {
	ObjectID types.ObjectID
}

func (r Ref) MarshalBinary() ([]byte, error) {
	if types.IsZero32([32]byte(r.ObjectID)) {
		return nil, ErrCredential
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
		return Ref{}, ErrCredential
	}
	raw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return Ref{}, ErrCredential
	}
	var id types.ObjectID
	copy(id[:], raw)
	if types.IsZero32([32]byte(id)) {
		return Ref{}, ErrCredential
	}
	return Ref{ObjectID: id}, nil
}

func ChallengeDigest(credentialObject types.ObjectID, sequence uint64, challenge []byte) [32]byte {
	var w codec.Writer
	w.Fixed(credentialObject[:])
	w.U64(sequence)
	w.Bytes(challenge)
	return codec.DomainHash("zephyr/device-credential/challenge/v1", w.BytesCopy())
}

func VerifyChallenge(c Credential, credentialObject types.ObjectID, height uint64, challenge, signature []byte) error {
	if !c.ActiveAt(height) || types.IsZero32([32]byte(credentialObject)) || len(signature) != 64 || len(challenge) == 0 {
		return ErrProof
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), c.DevicePublicKey)
	if x == nil || y == nil {
		return ErrProof
	}
	r := new(big.Int).SetBytes(signature[:32])
	s := new(big.Int).SetBytes(signature[32:])
	order := elliptic.P256().Params().N
	if r.Sign() <= 0 || s.Sign() <= 0 || r.Cmp(order) >= 0 || s.Cmp(order) >= 0 {
		return ErrProof
	}
	half := new(big.Int).Rsh(new(big.Int).Set(order), 1)
	if s.Cmp(half) > 0 {
		return ErrProof
	}
	digest := ChallengeDigest(credentialObject, c.Sequence, challenge)
	if !ecdsa.Verify(&ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}, digest[:], r, s) {
		return ErrProof
	}
	return nil
}

func validPublicKey(publicKey []byte) bool {
	if len(publicKey) != 65 {
		return false
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	return x != nil && y != nil
}

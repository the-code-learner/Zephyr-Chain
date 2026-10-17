package citizen

import (
	"errors"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	DeviceChallengeVersion         uint16 = 1
	DeviceChallengeResponseVersion uint16 = 1
)

var ErrChallengeEvidence = errors.New("invalid Citizen device challenge evidence")

// DeviceChallenge is a canonical challenge bound to one finalized
// DeviceCredential version. It proves no timing, work or economic contribution
// by itself; freshness depends on how the challenge nonce/context were derived.
type DeviceChallenge struct {
	Network              types.NetworkID
	CredentialObject     types.ObjectID
	CredentialSequence   uint64
	CredentialHeight     uint64
	RequiredCapabilities uint32
	Nonce                types.Hash
	ContextHash          types.Hash
}

func (c DeviceChallenge) Validate() error {
	if types.IsZero32([32]byte(c.Network)) ||
		types.IsZero32([32]byte(c.CredentialObject)) ||
		c.CredentialSequence == 0 ||
		c.CredentialHeight == 0 ||
		c.RequiredCapabilities == 0 ||
		c.RequiredCapabilities&^devicecredential.SupportedCapabilities != 0 ||
		types.IsZero32([32]byte(c.Nonce)) ||
		types.IsZero32([32]byte(c.ContextHash)) {
		return ErrChallengeEvidence
	}
	return nil
}

func (c DeviceChallenge) MarshalBinary() ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(DeviceChallengeVersion)
	w.Fixed(c.Network[:])
	w.Fixed(c.CredentialObject[:])
	w.U64(c.CredentialSequence)
	w.U64(c.CredentialHeight)
	w.U32(c.RequiredCapabilities)
	w.Fixed(c.Nonce[:])
	w.Fixed(c.ContextHash[:])
	return w.BytesCopy(), nil
}

func ParseDeviceChallenge(data []byte) (DeviceChallenge, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != DeviceChallengeVersion {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	networkRaw, err := r.Fixed(32)
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	objectRaw, err := r.Fixed(32)
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	sequence, err := r.U64()
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	height, err := r.U64()
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	capabilities, err := r.U32()
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	nonceRaw, err := r.Fixed(32)
	if err != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	contextRaw, err := r.Fixed(32)
	if err != nil || r.Done() != nil {
		return DeviceChallenge{}, ErrChallengeEvidence
	}
	var network types.NetworkID
	var objectID types.ObjectID
	var nonce types.Hash
	var context types.Hash
	copy(network[:], networkRaw)
	copy(objectID[:], objectRaw)
	copy(nonce[:], nonceRaw)
	copy(context[:], contextRaw)
	out := DeviceChallenge{
		Network:              network,
		CredentialObject:     objectID,
		CredentialSequence:   sequence,
		CredentialHeight:     height,
		RequiredCapabilities: capabilities,
		Nonce:                nonce,
		ContextHash:          context,
	}
	if err := out.Validate(); err != nil {
		return DeviceChallenge{}, err
	}
	return out, nil
}

func (c DeviceChallenge) Hash() (types.Hash, error) {
	raw, err := c.MarshalBinary()
	if err != nil {
		return types.Hash{}, err
	}
	return types.Hash(codec.DomainHash("zephyr/citizen/device-challenge/v1", raw)), nil
}

type DeviceChallengeResponse struct {
	Challenge DeviceChallenge
	Signature []byte
}

func (r DeviceChallengeResponse) Validate() error {
	if err := r.Challenge.Validate(); err != nil || len(r.Signature) != 64 {
		return ErrChallengeEvidence
	}
	return nil
}

func (r DeviceChallengeResponse) MarshalBinary() ([]byte, error) {
	if err := r.Validate(); err != nil {
		return nil, err
	}
	challenge, err := r.Challenge.MarshalBinary()
	if err != nil {
		return nil, err
	}
	var w codec.Writer
	w.U16(DeviceChallengeResponseVersion)
	w.Bytes(challenge)
	w.Bytes(r.Signature)
	return w.BytesCopy(), nil
}

func ParseDeviceChallengeResponse(data []byte) (DeviceChallengeResponse, error) {
	r := codec.NewReader(data)
	version, err := r.U16()
	if err != nil || version != DeviceChallengeResponseVersion {
		return DeviceChallengeResponse{}, ErrChallengeEvidence
	}
	challengeRaw, err := r.Bytes(256)
	if err != nil {
		return DeviceChallengeResponse{}, ErrChallengeEvidence
	}
	signature, err := r.Bytes(64)
	if err != nil || len(signature) != 64 || r.Done() != nil {
		return DeviceChallengeResponse{}, ErrChallengeEvidence
	}
	challenge, err := ParseDeviceChallenge(challengeRaw)
	if err != nil {
		return DeviceChallengeResponse{}, err
	}
	out := DeviceChallengeResponse{Challenge: challenge, Signature: signature}
	if err := out.Validate(); err != nil {
		return DeviceChallengeResponse{}, err
	}
	return out, nil
}

// VerifiedDeviceChallengeEvidence records only facts established by finalized
// Zephyr credential state plus the device-key signature. It deliberately carries
// no contribution score, reward weight, governance weight or hardware claim.
type VerifiedDeviceChallengeEvidence struct {
	Network              types.NetworkID
	CredentialObject     types.ObjectID
	CredentialSequence   uint64
	CredentialHeight     uint64
	CredentialStateRoot  types.Hash
	Owner                types.AccountID
	RequiredCapabilities uint32
	ChallengeHash        types.Hash
	ContextHash          types.Hash
}

// VerifyDeviceChallenge re-parses the finalized credential object instead of
// trusting the exported Credential field supplied by the caller.
func VerifyDeviceChallenge(
	verified VerifiedDeviceCredential,
	response DeviceChallengeResponse,
) (VerifiedDeviceChallengeEvidence, error) {
	if err := response.Validate(); err != nil {
		return VerifiedDeviceChallengeEvidence{}, err
	}
	current, err := ParseVerifiedDeviceCredential(verified.Verified)
	if err != nil {
		return VerifiedDeviceChallengeEvidence{}, ErrChallengeEvidence
	}
	challenge := response.Challenge
	if current.Verified.Network != challenge.Network ||
		current.Verified.ObjectID != challenge.CredentialObject ||
		current.Verified.Height != challenge.CredentialHeight ||
		current.Credential.Sequence != challenge.CredentialSequence ||
		!current.Credential.HasCapability(challenge.RequiredCapabilities) {
		return VerifiedDeviceChallengeEvidence{}, ErrChallengeEvidence
	}
	challengeRaw, err := challenge.MarshalBinary()
	if err != nil {
		return VerifiedDeviceChallengeEvidence{}, err
	}
	if err := devicecredential.VerifyChallenge(
		current.Credential,
		challenge.CredentialObject,
		challenge.CredentialHeight,
		challengeRaw,
		response.Signature,
	); err != nil {
		return VerifiedDeviceChallengeEvidence{}, ErrChallengeEvidence
	}
	challengeHash, err := challenge.Hash()
	if err != nil {
		return VerifiedDeviceChallengeEvidence{}, err
	}
	return VerifiedDeviceChallengeEvidence{
		Network:              current.Verified.Network,
		CredentialObject:     current.Verified.ObjectID,
		CredentialSequence:   current.Credential.Sequence,
		CredentialHeight:     current.Verified.Height,
		CredentialStateRoot:  current.Verified.StateRoot,
		Owner:                current.Credential.Owner,
		RequiredCapabilities: challenge.RequiredCapabilities,
		ChallengeHash:        challengeHash,
		ContextHash:          challenge.ContextHash,
	}, nil
}

func VerifyDeviceChallengeBundleJSON(
	data []byte,
	anchor TrustAnchor,
	response DeviceChallengeResponse,
) (VerifiedDeviceChallengeEvidence, error) {
	verified, err := VerifyDeviceCredentialBundleJSON(data, anchor)
	if err != nil {
		return VerifiedDeviceChallengeEvidence{}, err
	}
	return VerifyDeviceChallenge(verified, response)
}

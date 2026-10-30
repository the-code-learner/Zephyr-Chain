package citizen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDeviceChallengeEvidenceBindsFinalizedCredentialVersionAndContext(t *testing.T) {
	key, verified := testVerifiedDeviceCredential(t, devicecredential.CapabilityVerifyHeaders|devicecredential.CapabilitySampleDA)
	challenge := DeviceChallenge{
		Network:              verified.Verified.Network,
		CredentialObject:     verified.Verified.ObjectID,
		CredentialSequence:   verified.Credential.Sequence,
		CredentialHeight:     verified.Verified.Height,
		RequiredCapabilities: devicecredential.CapabilityVerifyHeaders,
		Nonce:                types.HashBytes("citizen/challenge-nonce", []byte("nonce-1")),
		ContextHash:          types.HashBytes("citizen/challenge-context", []byte("header-verification-round-7")),
	}
	response := signDeviceChallenge(t, key, challenge)
	raw, err := response.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseDeviceChallengeResponse(raw)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := VerifyDeviceChallenge(verified, parsed)
	if err != nil {
		t.Fatal(err)
	}
	challengeHash, err := challenge.Hash()
	if err != nil {
		t.Fatal(err)
	}
	if evidence.Network != challenge.Network ||
		evidence.CredentialObject != challenge.CredentialObject ||
		evidence.CredentialSequence != challenge.CredentialSequence ||
		evidence.CredentialHeight != challenge.CredentialHeight ||
		evidence.CredentialStateRoot != verified.Verified.StateRoot ||
		evidence.Owner != verified.Credential.Owner ||
		evidence.RequiredCapabilities != challenge.RequiredCapabilities ||
		evidence.ChallengeHash != challengeHash ||
		evidence.ContextHash != challenge.ContextHash {
		t.Fatalf("unexpected verified challenge evidence: %+v", evidence)
	}
}

func TestDeviceChallengeEvidenceRejectsSubstitutionAndStaleCredentialBinding(t *testing.T) {
	key, verified := testVerifiedDeviceCredential(t, devicecredential.CapabilityVerifyHeaders)
	base := DeviceChallenge{
		Network:              verified.Verified.Network,
		CredentialObject:     verified.Verified.ObjectID,
		CredentialSequence:   verified.Credential.Sequence,
		CredentialHeight:     verified.Verified.Height,
		RequiredCapabilities: devicecredential.CapabilityVerifyHeaders,
		Nonce:                types.HashBytes("citizen/challenge-nonce", []byte("nonce-2")),
		ContextHash:          types.HashBytes("citizen/challenge-context", []byte("context-a")),
	}
	response := signDeviceChallenge(t, key, base)
	if _, err := VerifyDeviceChallenge(verified, response); err != nil {
		t.Fatal(err)
	}

	tampered := response
	tampered.Challenge.ContextHash = types.HashBytes("citizen/challenge-context", []byte("context-b"))
	if _, err := VerifyDeviceChallenge(verified, tampered); err == nil {
		t.Fatal("challenge context substitution was accepted")
	}

	tampered = response
	tampered.Challenge.CredentialSequence++
	if _, err := VerifyDeviceChallenge(verified, tampered); err == nil {
		t.Fatal("stale credential sequence substitution was accepted")
	}

	tampered = response
	tampered.Challenge.CredentialHeight++
	if _, err := VerifyDeviceChallenge(verified, tampered); err == nil {
		t.Fatal("different finalized credential height was accepted")
	}

	tampered = response
	tampered.Challenge.RequiredCapabilities = devicecredential.CapabilitySampleDA
	if _, err := VerifyDeviceChallenge(verified, tampered); err == nil {
		t.Fatal("ungranted capability was accepted")
	}
}

func TestDeviceChallengeEvidenceReparsesFinalizedCredentialObject(t *testing.T) {
	key, verified := testVerifiedDeviceCredential(t, devicecredential.CapabilityVerifyHeaders)
	forged := verified
	forged.Credential.Capabilities |= devicecredential.CapabilityRelay
	challenge := DeviceChallenge{
		Network:              verified.Verified.Network,
		CredentialObject:     verified.Verified.ObjectID,
		CredentialSequence:   verified.Credential.Sequence,
		CredentialHeight:     verified.Verified.Height,
		RequiredCapabilities: devicecredential.CapabilityRelay,
		Nonce:                types.HashBytes("citizen/challenge-nonce", []byte("nonce-3")),
		ContextHash:          types.HashBytes("citizen/challenge-context", []byte("relay-round")),
	}
	response := signDeviceChallenge(t, key, challenge)
	if _, err := VerifyDeviceChallenge(forged, response); err == nil {
		t.Fatal("caller-mutated credential metadata was trusted over finalized object bytes")
	}
}

func TestDeviceChallengeRejectsIncompleteOrUnsupportedClaims(t *testing.T) {
	_, verified := testVerifiedDeviceCredential(t, devicecredential.CapabilityVerifyHeaders)
	base := DeviceChallenge{
		Network:              verified.Verified.Network,
		CredentialObject:     verified.Verified.ObjectID,
		CredentialSequence:   verified.Credential.Sequence,
		CredentialHeight:     verified.Verified.Height,
		RequiredCapabilities: devicecredential.CapabilityVerifyHeaders,
		Nonce:                types.HashBytes("citizen/challenge-nonce", []byte("nonce-4")),
		ContextHash:          types.HashBytes("citizen/challenge-context", []byte("context")),
	}
	bad := base
	bad.RequiredCapabilities = devicecredential.SupportedCapabilities | (1 << 31)
	if bad.Validate() == nil {
		t.Fatal("unsupported challenge capability was accepted")
	}
	bad = base
	bad.Nonce = types.Hash{}
	if bad.Validate() == nil {
		t.Fatal("zero challenge nonce was accepted")
	}
	bad = base
	bad.ContextHash = types.Hash{}
	if bad.Validate() == nil {
		t.Fatal("zero challenge context was accepted")
	}
}

func testVerifiedDeviceCredential(t *testing.T, capabilities uint32) (*ecdsa.PrivateKey, VerifiedDeviceCredential) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	owner := types.AccountIDFromPublicKey([]byte("citizen-owner"))
	credential := devicecredential.Credential{
		Owner:           owner,
		DevicePublicKey: public,
		Capabilities:    capabilities,
		IssuedHeight:    20,
		ExpiresHeight:   200,
		Sequence:        4,
	}
	raw, err := credential.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	objectID := types.ObjectID(types.HashBytes("citizen/device-credential", []byte("object-1")))
	verified, err := ParseVerifiedDeviceCredential(VerifiedObject{
		Network:   types.NetworkID(types.HashBytes("citizen/network", []byte("test"))),
		Height:    80,
		ShardID:   1,
		ObjectID:  objectID,
		Present:   true,
		Object:    object.Object{ID: objectID, Version: 3, Owner: owner, Kind: object.KindDeviceCredential, Data: raw},
		StateRoot: types.HashBytes("citizen/state-root", []byte("height-80")),
	})
	if err != nil {
		t.Fatal(err)
	}
	return key, verified
}

func signDeviceChallenge(t *testing.T, key *ecdsa.PrivateKey, challenge DeviceChallenge) DeviceChallengeResponse {
	t.Helper()
	raw, err := challenge.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	digest := devicecredential.ChallengeDigest(challenge.CredentialObject, challenge.CredentialSequence, raw)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	s = citizenLowS(s)
	signature := append(citizenPad32(r), citizenPad32(s)...)
	return DeviceChallengeResponse{Challenge: challenge, Signature: signature}
}

func citizenLowS(s *big.Int) *big.Int {
	order := elliptic.P256().Params().N
	half := new(big.Int).Rsh(new(big.Int).Set(order), 1)
	if s.Cmp(half) > 0 {
		return new(big.Int).Sub(order, s)
	}
	return s
}

func citizenPad32(v *big.Int) []byte {
	out := make([]byte, 32)
	raw := v.Bytes()
	copy(out[32-len(raw):], raw)
	return out
}

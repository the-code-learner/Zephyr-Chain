package devicecredential

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"math/big"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCredentialRoundTripAndCapabilities(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	owner := types.AccountIDFromPublicKey([]byte("device-owner"))
	credential := Credential{
		Owner: owner, DevicePublicKey: public,
		Capabilities: CapabilityVerifyHeaders | CapabilitySampleDA,
		IssuedHeight: 10, ExpiresHeight: 100, Sequence: 3,
	}
	raw, err := credential.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := ParseCredential(raw)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Owner != credential.Owner || parsed.Capabilities != credential.Capabilities ||
		parsed.IssuedHeight != credential.IssuedHeight || parsed.ExpiresHeight != credential.ExpiresHeight ||
		parsed.Sequence != credential.Sequence || string(parsed.DevicePublicKey) != string(public) {
		t.Fatalf("round-trip mismatch: %+v", parsed)
	}
	if !parsed.ActiveAt(10) || !parsed.ActiveAt(99) || parsed.ActiveAt(100) {
		t.Fatal("unexpected credential activity window")
	}
	if !parsed.HasCapability(CapabilityVerifyHeaders) || parsed.HasCapability(CapabilityRelay) {
		t.Fatal("unexpected capability evaluation")
	}
}

func TestChallengeProofBindsObjectSequenceAndChallenge(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	credential := Credential{
		Owner:           types.AccountIDFromPublicKey([]byte("owner")),
		DevicePublicKey: public,
		Capabilities:    CapabilityVerifyHeaders,
		IssuedHeight:    1,
		Sequence:        2,
	}
	objectID := types.ObjectID(types.HashBytes("device-credential", []byte("1")))
	challenge := []byte("prove-current-device-control")
	digest := ChallengeDigest(objectID, credential.Sequence, challenge)
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	s = lowS(s)
	signature := append(pad32(r), pad32(s)...)
	if err := VerifyChallenge(credential, objectID, 20, challenge, signature); err != nil {
		t.Fatal(err)
	}
	if err := VerifyChallenge(credential, objectID, 20, []byte("different"), signature); err == nil {
		t.Fatal("challenge substitution was accepted")
	}
	rotated := credential
	rotated.Sequence++
	if err := VerifyChallenge(rotated, objectID, 20, challenge, signature); err == nil {
		t.Fatal("stale sequence signature was accepted")
	}
}

func TestCredentialRejectsUnsupportedCapabilitiesAndExpiredIssue(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := elliptic.Marshal(elliptic.P256(), key.PublicKey.X, key.PublicKey.Y)
	base := Credential{
		Owner: types.AccountIDFromPublicKey([]byte("owner")), DevicePublicKey: public,
		Capabilities: CapabilityVerifyHeaders, IssuedHeight: 10, Sequence: 1,
	}
	bad := base
	bad.Capabilities = SupportedCapabilities | (1 << 31)
	if bad.Validate() == nil {
		t.Fatal("unsupported capability accepted")
	}
	bad = base
	bad.ExpiresHeight = 10
	if bad.Validate() == nil {
		t.Fatal("non-forward expiry accepted")
	}
}

func lowS(s *big.Int) *big.Int {
	order := elliptic.P256().Params().N
	half := new(big.Int).Rsh(new(big.Int).Set(order), 1)
	if s.Cmp(half) > 0 {
		return new(big.Int).Sub(order, s)
	}
	return s
}

func pad32(v *big.Int) []byte {
	out := make([]byte, 32)
	raw := v.Bytes()
	copy(out[32-len(raw):], raw)
	return out
}

package citizen

import (
	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
)

type VerifiedDeviceCredential struct {
	Verified   VerifiedObject
	Credential devicecredential.Credential
}

// ParseVerifiedDeviceCredential accepts only an object whose membership or
// absence has already been checked against finalized Zephyr state. It makes no
// hardware-vendor, TEE or external attestation claim.
func ParseVerifiedDeviceCredential(verified VerifiedObject) (VerifiedDeviceCredential, error) {
	if !verified.Present || verified.Object.Kind != object.KindDeviceCredential ||
		verified.Object.ID != verified.ObjectID {
		return VerifiedDeviceCredential{}, ErrTrustedBundle
	}
	credential, err := devicecredential.ParseCredential(verified.Object.Data)
	if err != nil || credential.Owner != verified.Object.Owner || !credential.ActiveAt(verified.Height) {
		return VerifiedDeviceCredential{}, ErrTrustedBundle
	}
	return VerifiedDeviceCredential{Verified: verified, Credential: credential}, nil
}

func VerifyDeviceCredentialBundleJSON(data []byte, anchor TrustAnchor) (VerifiedDeviceCredential, error) {
	verified, err := VerifyObjectBundleJSON(data, anchor)
	if err != nil {
		return VerifiedDeviceCredential{}, err
	}
	return ParseVerifiedDeviceCredential(verified)
}

package execution

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/devicecredential"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestDeviceCredentialRegisterRotateAndRevoke(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("device-credential")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	ownerKey := makeKey(t)
	owner := accountForKey(ownerKey)
	store := worldstate.NewMemory()
	coinID := seedLendingCoin(t, store, owner, native, 100, "device-owner")

	deviceKey1 := makeKey(t)
	deviceKey2 := makeKey(t)
	spec1 := devicecredential.Spec{
		DevicePublicKey: elliptic.Marshal(elliptic.P256(), deviceKey1.PublicKey.X, deviceKey1.PublicKey.Y),
		Capabilities:    devicecredential.CapabilityVerifyHeaders | devicecredential.CapabilitySampleDA,
		ExpiresHeight:   100,
	}
	raw1, err := spec1.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	register := computeSignedTx(t, store, ownerKey, network, native, 0, []types.ObjectID{coinID}, 99, tx.OpDeviceCredentialRegister, raw1, 111)
	registerResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 10}).Execute(register)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(registerResult.Consumed, registerResult.Created); err != nil {
		t.Fatal(err)
	}
	credentialObject := objectOfKind(t, registerResult.Created, object.KindDeviceCredential)
	change := coinObjectByOwnerAmount(t, registerResult.Created, owner, 99)
	credential, err := devicecredential.ParseCredential(credentialObject.Data)
	if err != nil {
		t.Fatal(err)
	}
	if credential.Owner != owner || credential.Sequence != 1 || credential.IssuedHeight != 10 ||
		!credential.HasCapability(devicecredential.CapabilitySampleDA) {
		t.Fatalf("unexpected registered credential: %+v", credential)
	}

	spec2 := devicecredential.Spec{
		DevicePublicKey: elliptic.Marshal(elliptic.P256(), deviceKey2.PublicKey.X, deviceKey2.PublicKey.Y),
		Capabilities:    devicecredential.CapabilityVerifyHeaders | devicecredential.CapabilityRelay,
		ExpiresHeight:   200,
	}
	updateRaw, err := (devicecredential.Update{ObjectID: credentialObject.ID, Spec: spec2}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	update := computeSignedTx(t, store, ownerKey, network, native, 0, []types.ObjectID{credentialObject.ID, change.ID}, 98, tx.OpDeviceCredentialUpdate, updateRaw, 112)
	updateResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 20}).Execute(update)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(updateResult.Consumed, updateResult.Created); err != nil {
		t.Fatal(err)
	}
	rotated, ok := store.GetObject(credentialObject.ID)
	if !ok || rotated.Version != 2 || rotated.Kind != object.KindDeviceCredential {
		t.Fatalf("rotated credential object missing: %+v", rotated)
	}
	parsed, err := devicecredential.ParseCredential(rotated.Data)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Sequence != 2 || parsed.IssuedHeight != 20 ||
		string(parsed.DevicePublicKey) != string(spec2.DevicePublicKey) ||
		parsed.HasCapability(devicecredential.CapabilitySampleDA) ||
		!parsed.HasCapability(devicecredential.CapabilityRelay) {
		t.Fatalf("unexpected rotated credential: %+v", parsed)
	}
	change = coinObjectByOwnerAmount(t, updateResult.Created, owner, 98)

	attackerKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	attackerUpdate := computeSignedTx(t, store, attackerKey, network, native, 0, []types.ObjectID{rotated.ID}, 0, tx.OpDeviceCredentialUpdate, updateRaw, 113)
	if _, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 21}).Execute(attackerUpdate); err == nil {
		t.Fatal("non-owner credential update was accepted")
	}

	revokeRaw, err := (devicecredential.Ref{ObjectID: rotated.ID}).MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	revoke := computeSignedTx(t, store, ownerKey, network, native, 0, []types.ObjectID{rotated.ID, change.ID}, 97, tx.OpDeviceCredentialRevoke, revokeRaw, 114)
	revokeResult, err := (Engine{Network: network, NativeToken: native, ShardCount: 1, Height: 30}).Execute(revoke)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Apply(revokeResult.Consumed, revokeResult.Created); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.GetObject(rotated.ID); ok {
		t.Fatal("revoked device credential remained in state")
	}
}

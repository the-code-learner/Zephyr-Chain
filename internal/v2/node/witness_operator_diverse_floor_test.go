//go:build linux || darwin

package node

import (
	"crypto/ed25519"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestOperatorDiverseWitnessFloorRejectsKeyQuorumFromTwoOperators(t *testing.T) {
	network := types.NetworkID(types.HashBytes("operator-diverse", []byte("network")))
	root := types.HashBytes("operator-diverse", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 66, HeaderHash: types.Hash{7}}
	keys, private := witnessTestKeys()
	// Four actual keys belong to three *declared* operators. Since the
	// first two keys share an owner, the first three signatures reach
	// the 3-of-4 KEY threshold but only two OPERATOR identities.
	bindings := []WitnessOperatorKeyBinding{
		{OperatorID: "operator-a", PublicKey: append([]byte(nil), keys[0]...)},
		{OperatorID: "operator-a", PublicKey: append([]byte(nil), keys[1]...)},
		{OperatorID: "operator-b", PublicKey: append([]byte(nil), keys[2]...)},
		{OperatorID: "operator-c", PublicKey: append([]byte(nil), keys[3]...)},
	}
	twoOwners := signedWitnessTestBundle(t, cp, keys, private, 3)
	if verified, err := ParseSignedWitnessCheckpoint(twoOwners, keys, network, root); err != nil || verified == nil {
		t.Fatalf("test control: ordinary strict 3-of-4 key quorum did not verify: %v", err)
	}
	insufficient, err := NewOperatorDiverseWitnessFloor(witnessSignedFloorFixture{data: twoOwners}, bindings, network, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := insufficient.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("two operators passed a three-operator recovery gate: %v", err)
	}

	allOwners := signedWitnessTestBundle(t, cp, keys, private, 4)
	accepted, err := NewOperatorDiverseWitnessFloor(witnessSignedFloorFixture{data: allOwners}, bindings, network, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := accepted.AuthenticatedWitnessFloor()
	if err != nil || got != cp {
		t.Fatalf("four verified signatures across three owners rejected: %+v %v", got, err)
	}
	// A different valid 3-of-4 signature quorum can also be admitted,
	// provided it contains operators a, b AND c, not just two owners.
	digest, err := WitnessCheckpointDigest(cp)
	if err != nil {
		t.Fatal(err)
	}
	detached := make([]WitnessSignature, 0, 3)
	for _, i := range []int{0, 2, 3} {
		detached = append(detached, WitnessSignature{
			PublicKey: hex.EncodeToString(keys[i]),
			Signature: hex.EncodeToString(ed25519.Sign(private[i], digest[:])),
		})
	}
	payload, err := AssembleSignedWitnessCheckpoint(cp, keys, detached)
	if err != nil {
		t.Fatalf("three-operator detached quorum did not assemble: %v", err)
	}
	threeOwners, err := NewOperatorDiverseWitnessFloor(witnessSignedFloorFixture{data: payload}, bindings, network, root)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := threeOwners.AuthenticatedWitnessFloor(); err != nil || got != cp {
		t.Fatalf("three separate owners rejected: %+v %v", got, err)
	}
	// The policy is copied: later mutations by configuration code MUST NOT
	// change the pinned identities of an already constructed verifier.
	bindings[0].OperatorID = "operator-x"
	bindings[0].PublicKey[0] ^= 0xff
	if got, err := accepted.AuthenticatedWitnessFloor(); err != nil || got != cp {
		t.Fatalf("caller mutation changed a pinned operator trust policy: %+v %v", got, err)
	}
}

func TestOperatorDiverseWitnessFloorRejectsAmbiguousOrUntrustedKeyAssignments(t *testing.T) {
	network := types.NetworkID(types.HashBytes("operator-policy", []byte("network")))
	root := types.HashBytes("operator-policy", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 7, HeaderHash: types.Hash{8}}
	keys, private := witnessTestKeys()
	source := witnessSignedFloorFixture{data: signedWitnessTestBundle(t, cp, keys, private, 4)}
	good := []WitnessOperatorKeyBinding{
		{OperatorID: "operator-a", PublicKey: keys[0]},
		{OperatorID: "operator-a", PublicKey: keys[1]},
		{OperatorID: "operator-b", PublicKey: keys[2]},
		{OperatorID: "operator-c", PublicKey: keys[3]},
	}
	for _, tc := range []struct {
		name     string
		bindings []WitnessOperatorKeyBinding
		source   SignedWitnessFloorSource
	}{
		{"missing operator", good[:3], source},
		{"two operators only", []WitnessOperatorKeyBinding{
			{OperatorID: "operator-a", PublicKey: keys[0]},
			{OperatorID: "operator-a", PublicKey: keys[1]},
			{OperatorID: "operator-b", PublicKey: keys[2]},
			{OperatorID: "operator-b", PublicKey: keys[3]},
		}, source},
		{"duplicate key", []WitnessOperatorKeyBinding{
			good[0], good[1], good[2], {OperatorID: "operator-c", PublicKey: keys[0]},
		}, source},
		{"malformed key", []WitnessOperatorKeyBinding{
			good[0], good[1], good[2], {OperatorID: "operator-c", PublicKey: keys[3][:31]},
		}, source},
		{"uppercase operator", []WitnessOperatorKeyBinding{
			good[0], good[1], good[2], {OperatorID: "OPERATOR-C", PublicKey: keys[3]},
		}, source},
		{"whitespace operator", []WitnessOperatorKeyBinding{
			good[0], good[1], good[2], {OperatorID: "operator c", PublicKey: keys[3]},
		}, source},
		{"missing source", good, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewOperatorDiverseWitnessFloor(tc.source, tc.bindings, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("invalid operator custody mapping accepted: %v", err)
			}
		})
	}
	forged := append([]byte(nil), source.data...)
	forged[len(forged)-2] ^= 1
	f, err := NewOperatorDiverseWitnessFloor(witnessSignedFloorFixture{data: forged}, good, network, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("forged signatures bypassed operator diversity policy: %v", err)
	}
}

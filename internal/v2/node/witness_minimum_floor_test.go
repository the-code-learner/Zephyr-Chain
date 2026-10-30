//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestCertifiedWitnessFloorMinimumRejectsStaleQuorumAgreement(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-minimum", []byte("net")))
	root := types.HashBytes("witness-minimum", []byte("root"))
	keys, private := witnessTestKeys()
	minimum := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 40, HeaderHash: types.Hash{4}}
	signedSource := func(cp FinalityCheckpoint) WitnessExternalFloorVerifier {
		data := signedWitnessTestBundle(t, cp, keys, private, 3)
		f, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{data: data}, keys, network, root)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	old := minimum
	old.Height--
	// Both remote publishers agree on an authentic but obsolete height.
	staleSources, err := NewMultiSourceWitnessFloor(signedSource(old), signedSource(old))
	if err != nil {
		t.Fatal(err)
	}
	gate, err := NewCertifiedWitnessFloorMinimum(staleSources, minimum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("two stale signed sources bypassed authenticated floor: %v", err)
	}
	conflict := minimum
	conflict.HeaderHash[0]++
	gate, err = NewCertifiedWitnessFloorMinimum(signedSource(conflict), minimum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("same-height competing quorum accepted: %v", err)
	}
	gate, err = NewCertifiedWitnessFloorMinimum(signedSource(minimum), minimum)
	if err != nil {
		t.Fatal(err)
	}
	got, err := gate.AuthenticatedWitnessFloor()
	if err != nil || got != minimum {
		t.Fatalf("exact certified floor rejected: %+v %v", got, err)
	}
	higher := minimum
	higher.Height++
	gate, err = NewCertifiedWitnessFloorMinimum(signedSource(higher), minimum)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := gate.AuthenticatedWitnessFloor(); err != nil || got != higher {
		t.Fatalf("higher checkpoint unexpectedly rejected: %+v %v", got, err)
	}
	if _, err := NewCertifiedWitnessFloorMinimum(nil, minimum); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("missing remote accepted: %v", err)
	}
}

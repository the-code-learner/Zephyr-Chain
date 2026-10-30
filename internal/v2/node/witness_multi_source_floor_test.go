//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestMultiSourceWitnessFloorRejectsConflictAndMissingOperator(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-multi", []byte("network")))
	root := types.HashBytes("witness-multi", []byte("root"))
	keys, private := witnessTestKeys()
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 27, HeaderHash: types.Hash{3}}
	signed := signedWitnessTestBundle(t, cp, keys, private, 3)
	makeSource := func(data []byte) WitnessExternalFloorVerifier {
		f, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{data: data}, keys, network, root)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	a := makeSource(signed)
	b := makeSource(signed)
	m, err := NewMultiSourceWitnessFloor(a, b)
	if err != nil {
		t.Fatal(err)
	}
	got, err := m.AuthenticatedWitnessFloor()
	if err != nil || got != cp {
		t.Fatalf("two agreeing signed sources rejected: %+v %v", got, err)
	}
	conflict := cp
	conflict.HeaderHash[0]++
	conflicting := makeSource(signedWitnessTestBundle(t, conflict, keys, private, 3))
	m, err = NewMultiSourceWitnessFloor(a, conflicting)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("conflicting valid quorums accepted: %v", err)
	}
	later := cp
	later.Height++
	m, err = NewMultiSourceWitnessFloor(a, makeSource(signedWitnessTestBundle(t, later, keys, private, 3)))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("different height without ancestry proof accepted: %v", err)
	}
	m, err = NewMultiSourceWitnessFloor(a, makeSource([]byte("{}")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("unsigned operator response accepted: %v", err)
	}
	if _, err := NewMultiSourceWitnessFloor(a); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("single source accepted: %v", err)
	}
	if _, err := NewMultiSourceWitnessFloor(a, nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("missing source accepted: %v", err)
	}
}

//go:build linux || darwin

package node

import (
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type witnessSignedFloorFixture struct {
	data []byte
	err  error
}

func (f witnessSignedFloorFixture) SignedWitnessFloor() ([]byte, error) {
	return f.data, f.err
}

func TestQuorumAuthenticatedWitnessFloorRejectsUntrustedData(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-floor-quorum", []byte("net")))
	root := types.HashBytes("witness-floor-quorum", []byte("root"))
	keys, private := witnessTestKeys()
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 25, HeaderHash: types.Hash{6}}
	signed := signedWitnessTestBundle(t, cp, keys, private, 3)
	f, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{data: signed}, keys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.AuthenticatedWitnessFloor()
	if err != nil || got != cp {
		t.Fatalf("valid pinned quorum rejected: %+v %v", got, err)
	}
	keys[0][0] ^= 0xff // later caller mutation cannot alter pinned policy
	got, err = f.AuthenticatedWitnessFloor()
	if err != nil || got != cp {
		t.Fatalf("trust policy was not copied: %+v %v", got, err)
	}
	bad := append([]byte(nil), signed...)
	bad[len(bad)-2] ^= 1
	for _, payload := range [][]byte{nil, bad, []byte("{}")} {
		broken, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{data: payload}, f.trustedKeys, network, root)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := broken.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessCheckpoint) {
			t.Fatalf("untrusted bundle accepted: %v", err)
		}
	}
	unavailable, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{err: errors.New("offline")}, f.trustedKeys, network, root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := unavailable.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("unavailable source accepted: %v", err)
	}
	duplicate := [][]byte{f.trustedKeys[0], f.trustedKeys[0], f.trustedKeys[1]}
	if _, err := NewQuorumAuthenticatedWitnessFloor(witnessSignedFloorFixture{}, duplicate, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("duplicate trust key accepted: %v", err)
	}
	if _, err := NewQuorumAuthenticatedWitnessFloor(nil, f.trustedKeys, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil source accepted: %v", err)
	}
}

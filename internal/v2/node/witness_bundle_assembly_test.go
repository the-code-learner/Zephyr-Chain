//go:build linux || darwin

package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWitnessBundleAssemblyCanonicalDetachedQuorumAndAdversaries(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-bundle-assembly", []byte("network")))
	root := types.HashBytes("witness-bundle-assembly", []byte("validators"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 88, HeaderHash: types.Hash{9}}
	keys, private := witnessTestKeys()
	want := signedWitnessTestBundle(t, cp, keys, private, 3)
	var decoded SignedWitnessCheckpoint
	if err := json.Unmarshal(want, &decoded); err != nil {
		t.Fatal(err)
	}
	// Independently delivered signatures can arrive in arbitrary order.
	input := []WitnessSignature{
		decoded.Signatures[2], decoded.Signatures[0], decoded.Signatures[1],
	}
	original := append([]WitnessSignature(nil), input...)
	got, err := AssembleSignedWitnessCheckpoint(cp, keys, input)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("canonical publication differs from independently verified format: %v", err)
	}
	for i := range input {
		if input[i] != original[i] {
			t.Fatal("assembler mutated detached signature input")
		}
	}
	read, err := ParseSignedWitnessCheckpoint(got, keys, network, root)
	if err != nil || *read != cp {
		t.Fatalf("published bundle does not verify: %+v %v", read, err)
	}

	other := cp
	other.Height++
	conflict := cp
	conflict.HeaderHash[0]++
	foreign := cp
	foreign.Network = types.NetworkID(types.HashBytes("witness-bundle-assembly", []byte("foreign")))
	for _, tc := range []struct {
		name       string
		checkpoint FinalityCheckpoint
		keys       [][]byte
		sigs       []WitnessSignature
	}{
		{name: "insufficient quorum", checkpoint: cp, keys: keys, sigs: input[:2]},
		{name: "replayed same signer", checkpoint: cp, keys: keys, sigs: []WitnessSignature{input[0], input[0], input[1]}},
		{name: "unauthorized key", checkpoint: cp, keys: keys[1:], sigs: input},
		{name: "duplicate trust key", checkpoint: cp, keys: [][]byte{keys[0], keys[0], keys[1], keys[2]}, sigs: input},
		{name: "future height", checkpoint: other, keys: keys, sigs: input},
		{name: "conflicting header", checkpoint: conflict, keys: keys, sigs: input},
		{name: "foreign chain", checkpoint: foreign, keys: keys, sigs: input},
		{name: "missing signature", checkpoint: cp, keys: keys, sigs: []WitnessSignature{input[0], input[1], {PublicKey: input[2].PublicKey}}},
		{name: "zero checkpoint", checkpoint: FinalityCheckpoint{}, keys: keys, sigs: input},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := AssembleSignedWitnessCheckpoint(tc.checkpoint, tc.keys, tc.sigs); !errors.Is(err, ErrWitnessCheckpoint) {
				t.Fatalf("invalid detached publication was accepted: %v", err)
			}
		})
	}
}

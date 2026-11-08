//go:build linux || darwin

package node

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// These cases are deliberately encoded at the signed-envelope boundary.
// A different JSON parser must not interpret the same bytes as a different
// authorized checkpoint, even when the Ed25519 signatures are otherwise valid.
func TestWitnessCheckpointRejectsAmbiguousCanonicalEnvelopes(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-canonical", []byte("network")))
	root := types.HashBytes("witness-canonical", []byte("validators"))
	cp := FinalityCheckpoint{
		Network: network, ValidatorRoot: root, Height: 91,
		HeaderHash: types.HashBytes("witness-canonical", []byte("header")),
	}
	keys, private := witnessTestKeys()
	valid := signedWitnessTestBundle(t, cp, keys, private, 3)
	var envelope SignedWitnessCheckpoint
	if err := json.Unmarshal(valid, &envelope); err != nil {
		t.Fatal(err)
	}
	encode := func(e SignedWitnessCheckpoint) []byte {
		t.Helper()
		out, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	upperKey := envelope
	upperKey.Signatures = append([]WitnessSignature(nil), envelope.Signatures...)
	upperKey.Signatures[0].PublicKey = strings.ToUpper(upperKey.Signatures[0].PublicKey)
	upperSignature := envelope
	upperSignature.Signatures = append([]WitnessSignature(nil), envelope.Signatures...)
	upperSignature.Signatures[0].Signature = strings.ToUpper(upperSignature.Signatures[0].Signature)
	reordered := envelope
	reordered.Signatures = append([]WitnessSignature(nil), envelope.Signatures...)
	reordered.Signatures[0], reordered.Signatures[1] = reordered.Signatures[1], reordered.Signatures[0]
	duplicate := envelope
	duplicate.Signatures = append([]WitnessSignature(nil), envelope.Signatures...)
	duplicate.Signatures[1] = duplicate.Signatures[0]

	cases := map[string][]byte{
		"internal formatting":     bytes.Replace(valid, []byte(","), []byte(", "), 1),
		"trailing JSON value":     append(append([]byte(nil), valid...), []byte(" null")...),
		"duplicate top-level key": append([]byte("{\"height\":91,"), valid[1:]...),
		"uppercase public key":    encode(upperKey),
		"uppercase signature":     encode(upperSignature),
		"out-of-order signatures": encode(reordered),
		"duplicate signer":        encode(duplicate),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSignedWitnessCheckpoint(raw, keys, network, root); !errors.Is(err, ErrWitnessCheckpoint) {
				t.Fatalf("noncanonical or ambiguous envelope accepted: %v", err)
			}
		})
	}
	// Outer JSON whitespace is explicitly permitted by the parser; only the
	// trimmed envelope must be canonical.
	if got, err := ParseSignedWitnessCheckpoint(append([]byte(" \n"), valid...), keys, network, root); err != nil || got == nil || *got != cp {
		t.Fatalf("canonical control rejected: checkpoint=%+v err=%v", got, err)
	}
}

func TestWitnessCheckpointRejectsInvalidPinnedKeyPolicy(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-policy", []byte("network")))
	root := types.HashBytes("witness-policy", []byte("validators"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 1, HeaderHash: types.Hash{1}}
	keys, private := witnessTestKeys()
	valid := signedWitnessTestBundle(t, cp, keys, private, 3)
	cases := map[string][][]byte{
		"too few trusted signers": keys[:2],
		"empty trust policy":      nil,
		"duplicate pinned key":    {keys[0], keys[1], keys[1], keys[3]},
		"truncated public key":    {keys[0][:31], keys[1], keys[2], keys[3]},
	}
	for name, policy := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseSignedWitnessCheckpoint(valid, policy, network, root); !errors.Is(err, ErrWitnessCheckpoint) {
				t.Fatalf("invalid trust policy accepted: %v", err)
			}
		})
	}
}

//go:build linux || darwin

package node

import (
	"bytes"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// These regression cases pin the canonical trust boundary before a separate
// witness signing service is allowed to expose keys or publish attestations.
func TestWitnessBoundaryCanonicalAndQuorumAdversaries(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-boundary", []byte("network")))
	root := types.HashBytes("witness-boundary", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 9, HeaderHash: types.Hash{7}}
	keys, private := witnessTestKeys()
	valid := signedWitnessTestBundle(t, cp, keys, private, 3)
	reject := func(name string, data []byte, trusted [][]byte) {
		t.Helper()
		if _, err := ParseSignedWitnessCheckpoint(data, trusted, network, root); !errors.Is(err, ErrWitnessCheckpoint) {
			t.Errorf("%s: failed to reject: %v", name, err)
		}
	}
	reject("oversized envelope", bytes.Repeat([]byte(" "), (16<<10)+1), keys)
	reject("trailing document", append(append([]byte(nil), valid...), []byte(" true")...), keys)
	// The parser explicitly allows outer whitespace via bytes.TrimSpace.
	if _, err := ParseSignedWitnessCheckpoint(append([]byte(" "), valid...), keys, network, root); err != nil {
		t.Fatalf("outer whitespace should be accepted: %v", err)
	}
	reject("noncanonical interior whitespace", bytes.Replace(valid, []byte(","), []byte(", "), 1), keys)
	reject("truncated document", valid[:len(valid)-1], keys)
	reject("untrusted signer policy", valid, [][]byte{keys[0], keys[1], keys[2], keys[2]})
	var env SignedWitnessCheckpoint
	if err := json.Unmarshal(valid, &env); err != nil {
		t.Fatal(err)
	}
	env.Signatures[0], env.Signatures[1] = env.Signatures[1], env.Signatures[0]
	unsorted, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	reject("unsorted witnesses", unsorted, keys)

	// The maximum allowed committee has a strict 11-of-16 quorum.
	var committee [][]byte
	var private16 []ed25519.PrivateKey
	for i := 0; i < 16; i++ {
		seed := make([]byte, ed25519.SeedSize)
		seed[0], seed[1] = 99, byte(i)
		key := ed25519.NewKeyFromSeed(seed)
		private16 = append(private16, key)
		committee = append(committee, key.Public().(ed25519.PublicKey))
	}
	makeBundle := func(count int) []byte {
		t.Helper()
		digest, err := WitnessCheckpointDigest(cp)
		if err != nil {
			t.Fatal(err)
		}
		e := SignedWitnessCheckpoint{NetworkID: hex.EncodeToString(network[:]), ValidatorRoot: hex.EncodeToString(root[:]), Height: cp.Height, HeaderHash: hex.EncodeToString(cp.HeaderHash[:])}
		for i := 0; i < count; i++ {
			e.Signatures = append(e.Signatures, WitnessSignature{
				PublicKey: hex.EncodeToString(committee[i]),
				Signature: hex.EncodeToString(ed25519.Sign(private16[i], digest[:])),
			})
		}
		sort.Slice(e.Signatures, func(i, j int) bool { return e.Signatures[i].PublicKey < e.Signatures[j].PublicKey })
		b, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	reject("10 of 16", makeBundle(10), committee)
	if _, err := ParseSignedWitnessCheckpoint(makeBundle(11), committee, network, root); err != nil {
		t.Fatalf("valid 11 of 16 quorum rejected: %v", err)
	}
	seventeenth := make([]byte, 32)
	seventeenth[0] = 255
	reject("17 trusted keys", makeBundle(11), append(committee, seventeenth))
}

//go:build linux || darwin

package node

import (
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func signedWitnessOperatorInventoryTestFixture(
	t *testing.T,
	network types.NetworkID,
	root types.Hash,
	revision uint64,
) ([]WitnessHTTPSOperatorAttestation, [][]byte, []ed25519.PrivateKey) {
	t.Helper()
	pubs := witnessOperatorFloorTestProvisioning()
	attested := make([]WitnessHTTPSOperatorAttestation, len(pubs))
	keys := make([][]byte, len(pubs))
	private := make([]ed25519.PrivateKey, len(pubs))
	for i, publication := range pubs {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(101 + i)
		private[i] = ed25519.NewKeyFromSeed(seed)
		keys[i] = append([]byte(nil), private[i].Public().(ed25519.PublicKey)...)
		attested[i] = WitnessHTTPSOperatorAttestation{
			OperatorID: publication.OperatorID,
			Endpoint: publication.Endpoint,
			Revision: revision,
			OperatorPublicKey: append([]byte(nil), keys[i]...),
		}
		signWitnessOperatorInventoryTestEntry(t, &attested[i], private[i], network, root)
	}
	return attested, keys, private
}

func signWitnessOperatorInventoryTestEntry(
	t *testing.T, a *WitnessHTTPSOperatorAttestation,
	private ed25519.PrivateKey, network types.NetworkID, root types.Hash,
) {
	t.Helper()
	digest, err := WitnessHTTPSOperatorAttestationDigest(*a, network, root)
	if err != nil {
		t.Fatal(err)
	}
	a.Signature = ed25519.Sign(private, digest[:])
}

func cloneWitnessOperatorInventoryTestEntries(in []WitnessHTTPSOperatorAttestation) []WitnessHTTPSOperatorAttestation {
	out := append([]WitnessHTTPSOperatorAttestation(nil), in...)
	for i := range out {
		out[i].OperatorPublicKey = append([]byte(nil), in[i].OperatorPublicKey...)
		out[i].Signature = append([]byte(nil), in[i].Signature...)
	}
	return out
}

func TestAttestedMultiOperatorHTTPSWitnessFloorRequiresPinnedSignedRoster(t *testing.T) {
	network := types.NetworkID(types.HashBytes("signed-witness-operator-inventory", []byte("network")))
	root := types.HashBytes("signed-witness-operator-inventory", []byte("validator-root"))
	witnessKeys, _ := witnessTestKeys()
	const revision uint64 = 7
	signed, operatorKeys, operatorPrivates := signedWitnessOperatorInventoryTestFixture(t, network, root, revision)
	floor, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed, operatorKeys, witnessKeys, network, root, revision,
	)
	if err != nil || len(floor.sources) != 3 {
		t.Fatalf("properly signed independently pinned inventory rejected: floor=%v err=%v", floor, err)
	}

	tests := []struct {
		name string
		mutate func([]WitnessHTTPSOperatorAttestation)
	}{
		{"mutated endpoint", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].Endpoint = "https://evil.example.org/v1/checkpoint"
		}},
		{"mutated operator label", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].OperatorID = "operator-fake"
		}},
		{"mutated roster revision", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].Revision++
		}},
		{"mutated operator key", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].OperatorPublicKey[0] ^= 1
		}},
		{"mutated Ed25519 signature", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].Signature[0] ^= 1
		}},
		{"reused operator signature/key", func(a []WitnessHTTPSOperatorAttestation) {
			a[1] = a[0]
		}},
		{"missing operator signature", func(a []WitnessHTTPSOperatorAttestation) {
			a[0].Signature = nil
		}},
		{"different valid operator same DNS host", func(a []WitnessHTTPSOperatorAttestation) {
			a[1].Endpoint = "https://witness.north.example.org/other"
			signWitnessOperatorInventoryTestEntry(t, &a[1], operatorPrivates[1], network, root)
		}},
		{"same signed operator label different key", func(a []WitnessHTTPSOperatorAttestation) {
			a[1].OperatorID = a[0].OperatorID
			signWitnessOperatorInventoryTestEntry(t, &a[1], operatorPrivates[1], network, root)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := cloneWitnessOperatorInventoryTestEntries(signed)
			tc.mutate(entries)
			if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
				entries, operatorKeys, witnessKeys, network, root, revision,
			); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("accepted tampered/reused independently signed operator evidence: %v", err)
			}
		})
	}

	alternateNetwork := network
	alternateNetwork[0] ^= 1
	alternateRoot := root
	alternateRoot[0] ^= 1
	for _, tc := range []struct {
		name string
		network types.NetworkID
		root types.Hash
		revision uint64
	}{
		{name: "foreign genesis", network: alternateNetwork, root: root, revision: revision},
		{name: "foreign validator root", network: network, root: alternateRoot, revision: revision},
		{name: "old expected revision", network: network, root: root, revision: revision - 1},
		{name: "zero revision", network: network, root: root, revision: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
				signed, operatorKeys, witnessKeys, tc.network, tc.root, tc.revision,
			); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("accepted signed inventory replay across trust anchors: %v", err)
			}
		})
	}
	if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed[:2], operatorKeys[:2], witnessKeys, network, root, revision,
	); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted signed inventory from only two operators: %v", err)
	}
	if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed, operatorKeys[:2], witnessKeys, network, root, revision,
	); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted unpinned operator attestation: %v", err)
	}
	unknownPinned := append([][]byte(nil), operatorKeys...)
	unknownPinned[0] = make([]byte, ed25519.PublicKeySize)
	if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed, unknownPinned, witnessKeys, network, root, revision,
	); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted unpinned issuer key: %v", err)
	}
	duplicatePinned := append([][]byte(nil), operatorKeys...)
	duplicatePinned[1] = duplicatePinned[0]
	if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed, duplicatePinned, witnessKeys, network, root, revision,
	); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted repeated operator issuer key: %v", err)
	}
	// Distinct roles must not share the same Ed25519 identity key.
	overlappingWitnessKeys := append([][]byte(nil), witnessKeys...)
	overlappingWitnessKeys[0] = operatorKeys[0]
	if _, err := NewAttestedMultiOperatorHTTPSWitnessFloor(
		signed, operatorKeys, overlappingWitnessKeys, network, root, revision,
	); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted signing-role key reuse across operator and witness: %v", err)
	}
}

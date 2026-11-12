//go:build linux || darwin

package node

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestPinnedDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloorRejectsSameRevisionIssuerEquivocation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("pinned-online-delegation", []byte("network")))
	root := types.HashBytes("pinned-online-delegation", []byte("root"))
	witnessKeys, _ := witnessTestKeys()
	const revision uint64 = 42
	inventory, offlineKeys, offlinePrivate := signedWitnessOperatorInventoryTestFixture(t, network, root, revision)
	delegations := make([]WitnessHTTPSPublicationDelegation, len(inventory))
	pins := make([][]byte, len(inventory))
	for i, issuer := range inventory {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(224 + i)
		private := ed25519.NewKeyFromSeed(seed)
		pins[i] = append([]byte(nil), private.Public().(ed25519.PublicKey)...)
		delegations[i] = WitnessHTTPSPublicationDelegation{
			OperatorID:           issuer.OperatorID,
			Revision:             revision,
			PublicationPublicKey: append([]byte(nil), pins[i]...),
		}
		digest, err := WitnessHTTPSPublicationDelegationDigest(delegations[i], network, root)
		if err != nil {
			t.Fatal(err)
		}
		delegations[i].Signature = ed25519.Sign(offlinePrivate[i], digest[:])
	}
	copyDelegations := func() []WitnessHTTPSPublicationDelegation {
		result := make([]WitnessHTTPSPublicationDelegation, len(delegations))
		for i, d := range delegations {
			result[i] = d
			result[i].PublicationPublicKey = append([]byte(nil), d.PublicationPublicKey...)
			result[i].Signature = append([]byte(nil), d.Signature...)
		}
		return result
	}
	copyPins := func() [][]byte {
		c := make([][]byte, len(pins))
		for i := range pins {
			c[i] = append([]byte(nil), pins[i]...)
		}
		return c
	}
	verify := func(ds []WitnessHTTPSPublicationDelegation, expected [][]byte, n types.NetworkID, r types.Hash, rev uint64) (*MultiSourceWitnessFloor, error) {
		return NewPinnedDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
			inventory, offlineKeys, witnessKeys, ds, expected, n, r, rev,
		)
	}
	floor, err := verify(copyDelegations(), copyPins(), network, root, revision)
	if err != nil || floor == nil || len(floor.sources) != len(inventory) {
		t.Fatalf("authorized offline-signed and externally pinned online keys rejected: %v", err)
	}
	for i, raw := range floor.sources {
		q, ok := raw.(*QuorumAuthenticatedWitnessFloor)
		if !ok {
			t.Fatal("strict pin policy bypassed canonical witness quorum")
		}
		source, ok := q.source.(*HTTPWitnessFloorSource)
		if !ok || source.challengePolicy == nil ||
			!bytes.Equal(source.challengePolicy.publicationKey, pins[i]) {
			t.Fatal("strict publication key pin not used in HTTPS challenge")
		}
	}
	// An OFFLINE issuer can cryptographically sign TWO distinct online keys
	// at the SAME revision. The signature check alone would accept either;
	// a separately approved expected key pin denies the equivocation.
	substitute := copyDelegations()
	altSeed := make([]byte, ed25519.SeedSize)
	altSeed[0] = 239
	alt := ed25519.NewKeyFromSeed(altSeed).Public().(ed25519.PublicKey)
	substitute[1].PublicationPublicKey = append([]byte(nil), alt...)
	digest, err := WitnessHTTPSPublicationDelegationDigest(substitute[1], network, root)
	if err != nil {
		t.Fatal(err)
	}
	substitute[1].Signature = ed25519.Sign(offlinePrivate[1], digest[:])
	if _, err := NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
		inventory, offlineKeys, witnessKeys, substitute, network, root, revision,
	); err != nil {
		t.Fatalf("equivocation fixture must be a genuinely valid same-revision offline authorization: %v", err)
	}
	if _, err := verify(substitute, copyPins(), network, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("out-of-band pin failed to reject offline issuer equivocation: %v", err)
	}
	cases := []struct {
		name   string
		mutate func([]WitnessHTTPSPublicationDelegation, [][]byte)
	}{
		{"missing independent pins", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[0] = nil }},
		{"invalid pin length", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[0] = []byte{1} }},
		{"swapped expected pins", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[0], p[1] = p[1], p[0] }},
		{"substituted online pin", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[0][0] ^= 1 }},
		{"duplicate expected pins", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[1] = p[0] }},
		{"revoked online key by change of expected pin", func(_ []WitnessHTTPSPublicationDelegation, p [][]byte) { p[1] = append([]byte(nil), alt...) }},
		{"mutated signed delegation", func(d []WitnessHTTPSPublicationDelegation, _ [][]byte) { d[0].Signature[0] ^= 1 }},
		{"swapped signed delegations", func(d []WitnessHTTPSPublicationDelegation, _ [][]byte) { d[0], d[1] = d[1], d[0] }},
		{"different signed online key", func(d []WitnessHTTPSPublicationDelegation, _ [][]byte) { d[0].PublicationPublicKey[0] ^= 1 }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			ds, expected := copyDelegations(), copyPins()
			test.mutate(ds, expected)
			if _, err := verify(ds, expected, network, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("unauthorized signed online publisher accepted: %v", err)
			}
		})
	}
	for _, bad := range [][][]byte{nil, pins[:2], append(copyPins(), pins[0])} {
		if _, err := verify(copyDelegations(), bad, network, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("incomplete or oversized pin set accepted: %v", err)
		}
	}
	for _, n := range []types.NetworkID{types.NetworkID{9}, types.NetworkID{}} {
		if _, err := verify(copyDelegations(), copyPins(), n, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("foreign/missing NetworkID accepted: %v", err)
		}
	}
	if _, err := verify(copyDelegations(), copyPins(), network, types.Hash{9}, revision); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("foreign validator root accepted: %v", err)
	}
	for _, stale := range []uint64{0, revision - 1, revision + 1} {
		if _, err := verify(copyDelegations(), copyPins(), network, root, stale); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("unexpected revision %d accepted: %v", stale, err)
		}
	}
	// Constructor stores its own immutable copies of the online key pins.
	independentPins := copyPins()
	newFloor, err := verify(copyDelegations(), independentPins, network, root, revision)
	if err != nil {
		t.Fatal(err)
	}
	independentPins[0][0] ^= 1
	q := newFloor.sources[0].(*QuorumAuthenticatedWitnessFloor)
	src := q.source.(*HTTPWitnessFloorSource)
	if !bytes.Equal(src.challengePolicy.publicationKey, pins[0]) {
		t.Fatal("mutating provisioned pin changed live signer trust after constructor")
	}
}

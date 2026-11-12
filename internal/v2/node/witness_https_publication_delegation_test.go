//go:build linux || darwin

package node

import (
	"bytes"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloorRequiresOfflineAuthorization(t *testing.T) {
	network := types.NetworkID(types.HashBytes("signed-online-publication", []byte("network")))
	root := types.HashBytes("signed-online-publication", []byte("root"))
	witnessKeys, _ := witnessTestKeys()
	const revision uint64 = 17
	attested, offlineKeys, offlinePrivates := signedWitnessOperatorInventoryTestFixture(t, network, root, revision)
	delegations := make([]WitnessHTTPSPublicationDelegation, len(attested))
	for i, attestation := range attested {
		seed := make([]byte, ed25519.SeedSize)
		seed[0] = byte(191 + i)
		publicationPrivate := ed25519.NewKeyFromSeed(seed)
		delegations[i] = WitnessHTTPSPublicationDelegation{
			OperatorID: attestation.OperatorID,
			Revision: revision,
			PublicationPublicKey: append([]byte(nil), publicationPrivate.Public().(ed25519.PublicKey)...),
		}
		digest, err := WitnessHTTPSPublicationDelegationDigest(delegations[i], network, root)
		if err != nil {
			t.Fatal(err)
		}
		delegations[i].Signature = ed25519.Sign(offlinePrivates[i], digest[:])
	}
	check := func(in []WitnessHTTPSPublicationDelegation, n types.NetworkID, root types.Hash, expected uint64) error {
		_, err := NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
			attested, offlineKeys, witnessKeys, in, n, root, expected,
		)
		return err
	}
	floor, err := NewDelegatedChallengeAuthenticatedMultiOperatorHTTPSWitnessFloor(
		attested, offlineKeys, witnessKeys, delegations, network, root, revision,
	)
	if err != nil || floor == nil || len(floor.sources) != 3 {
		t.Fatalf("offline-signed publication delegation rejected: floor=%v err=%v", floor, err)
	}
	for i, raw := range floor.sources {
		quorum, ok := raw.(*QuorumAuthenticatedWitnessFloor)
		if !ok {
			t.Fatal("delegation bypassed pinned witness quorum")
		}
		source, ok := quorum.source.(*HTTPWitnessFloorSource)
		if !ok || source.challengePolicy == nil ||
			!bytes.Equal(source.challengePolicy.publicationKey, delegations[i].PublicationPublicKey) ||
			source.challengePolicy.operatorID != attested[i].OperatorID ||
			source.challengePolicy.revision != revision {
			t.Fatal("delegation was not bound to its signed operator")
		}
	}
	copyDelegations := func() []WitnessHTTPSPublicationDelegation {
		out := make([]WitnessHTTPSPublicationDelegation, len(delegations))
		for i, d := range delegations {
			out[i] = d
			out[i].PublicationPublicKey = append([]byte(nil), d.PublicationPublicKey...)
			out[i].Signature = append([]byte(nil), d.Signature...)
		}
		return out
	}
	cases := []struct {
		name string
		mutate func([]WitnessHTTPSPublicationDelegation)
	}{
		{"modified online key", func(d []WitnessHTTPSPublicationDelegation) { d[0].PublicationPublicKey[0] ^= 1 }},
		{"missing signature", func(d []WitnessHTTPSPublicationDelegation) { d[0].Signature = nil }},
		{"tampered signature", func(d []WitnessHTTPSPublicationDelegation) { d[0].Signature[0] ^= 1 }},
		{"signature copied from other operator", func(d []WitnessHTTPSPublicationDelegation) { d[0].Signature = d[1].Signature }},
		{"operator order swap", func(d []WitnessHTTPSPublicationDelegation) { d[0], d[1] = d[1], d[0] }},
		{"different operator identity", func(d []WitnessHTTPSPublicationDelegation) { d[0].OperatorID = "operator-other" }},
		{"different roster revision", func(d []WitnessHTTPSPublicationDelegation) { d[0].Revision++ }},
		{"one key duplicated with valid offline signatures", func(d []WitnessHTTPSPublicationDelegation) {
			d[1].PublicationPublicKey = append([]byte(nil), d[0].PublicationPublicKey...)
			digest, _ := WitnessHTTPSPublicationDelegationDigest(d[1], network, root)
			d[1].Signature = ed25519.Sign(offlinePrivates[1], digest[:])
		}},
		{"witness key reused with valid offline signature", func(d []WitnessHTTPSPublicationDelegation) {
			d[1].PublicationPublicKey = append([]byte(nil), witnessKeys[0]...)
			digest, _ := WitnessHTTPSPublicationDelegationDigest(d[1], network, root)
			d[1].Signature = ed25519.Sign(offlinePrivates[1], digest[:])
		}},
		{"inventory issuer reused with valid offline signature", func(d []WitnessHTTPSPublicationDelegation) {
			d[1].PublicationPublicKey = append([]byte(nil), offlineKeys[2]...)
			digest, _ := WitnessHTTPSPublicationDelegationDigest(d[1], network, root)
			d[1].Signature = ed25519.Sign(offlinePrivates[1], digest[:])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := copyDelegations()
			tc.mutate(in)
			if err := check(in, network, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("invalid online publication delegation accepted: %v", err)
			}
		})
	}
	otherNetwork := network
	otherNetwork[0] ^= 1
	otherRoot := root
	otherRoot[0] ^= 1
	for _, change := range []struct {
		network types.NetworkID
		root types.Hash
		revision uint64
	}{
		{otherNetwork, root, revision},
		{network, otherRoot, revision},
		{network, root, revision + 1},
		{network, root, 0},
	} {
		if err := check(copyDelegations(), change.network, change.root, change.revision); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("replayed delegation across pinned trust anchor accepted: %v", err)
		}
	}
	if err := check(copyDelegations()[:2], network, root, revision); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("partial operator delegation accepted: %v", err)
	}
}

func TestWitnessHTTPSPublicationDelegationDigestBindsKeyAndChain(t *testing.T) {
	network := types.NetworkID{1}
	root := types.Hash{2}
	d := WitnessHTTPSPublicationDelegation{OperatorID: "operator-a", Revision: 5, PublicationPublicKey: make([]byte, ed25519.PublicKeySize)}
	d.PublicationPublicKey[0] = 3
	original, err := WitnessHTTPSPublicationDelegationDigest(d, network, root)
	if err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*WitnessHTTPSPublicationDelegation){
		func(d *WitnessHTTPSPublicationDelegation) { d.OperatorID = "operator-b" },
		func(d *WitnessHTTPSPublicationDelegation) { d.Revision++ },
		func(d *WitnessHTTPSPublicationDelegation) { d.PublicationPublicKey[0] ^= 1 },
	} {
		modified := d
		modified.PublicationPublicKey = append([]byte(nil), d.PublicationPublicKey...)
		change(&modified)
		next, err := WitnessHTTPSPublicationDelegationDigest(modified, network, root)
		if err != nil || next == original {
			t.Fatalf("delegation digest omitted signed identity field: %v", err)
		}
	}
	if next, err := WitnessHTTPSPublicationDelegationDigest(d, types.NetworkID{4}, root); err != nil || next == original {
		t.Fatalf("delegation network not bound: %v", err)
	}
	if next, err := WitnessHTTPSPublicationDelegationDigest(d, network, types.Hash{4}); err != nil || next == original {
		t.Fatalf("delegation genesis root not bound: %v", err)
	}
	for _, invalid := range []WitnessHTTPSPublicationDelegation{
		{OperatorID: "bad!", Revision: 5, PublicationPublicKey: d.PublicationPublicKey},
		{OperatorID: "operator-a", Revision: 0, PublicationPublicKey: d.PublicationPublicKey},
		{OperatorID: "operator-a", Revision: 5, PublicationPublicKey: nil},
	} {
		if _, err := WitnessHTTPSPublicationDelegationDigest(invalid, network, root); !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("invalid delegation accepted: %+v", invalid)
		}
	}
}

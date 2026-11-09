//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// The in-process fixture does not implement durable remote custody.
// Only the exact signed wire-level evidence is exercised here.
type witnessAtomicCustodianFixture struct {
	authority ed25519.PrivateKey
	witness   ed25519.PrivateKey
	mutate    func(WitnessAtomicCustodyAttestation) WitnessAtomicCustodyAttestation
	calls     int
}

func (f *witnessAtomicCustodianFixture) ReserveAndSignWitnessCheckpoint(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessAtomicCustodyAttestation, error) {
	f.calls++
	if err := ctx.Err(); err != nil {
		return WitnessAtomicCustodyAttestation{}, err
	}
	cpDigest, err := WitnessCheckpointDigest(cp)
	if err != nil {
		return WitnessAtomicCustodyAttestation{}, err
	}
	a := WitnessAtomicCustodyAttestation{
		Checkpoint: cp, Challenge: challenge, Revision: 1,
		WitnessPublicKey: append([]byte(nil), f.witness.Public().(ed25519.PublicKey)...),
		WitnessSignature: ed25519.Sign(f.witness, cpDigest[:]),
	}
	digest, err := WitnessAtomicCustodyDigest(a)
	if err != nil {
		return WitnessAtomicCustodyAttestation{}, err
	}
	a.AuthoritySignature = ed25519.Sign(f.authority, digest[:])
	if f.mutate != nil {
		return f.mutate(a), nil
	}
	return a, nil
}

func witnessAtomicTestKeys() (ed25519.PrivateKey, ed25519.PrivateKey) {
	var a, w [ed25519.SeedSize]byte
	a[0], w[0] = 91, 92
	return ed25519.NewKeyFromSeed(a[:]), ed25519.NewKeyFromSeed(w[:])
}

func TestAtomicWitnessCustodySignatureBoundToGrant(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("atomic-witness", []byte("network")))
	root := types.HashBytes("atomic-witness", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 26, HeaderHash: types.Hash{5}}
	challenge := [32]byte{7}
	f := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	valid, err := f.ReserveAndSignWitnessCheckpoint(context.Background(), cp, challenge)
	if err != nil {
		t.Fatal(err)
	}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	if err := VerifyWitnessAtomicCustodyAttestation(valid, cp, challenge, keyA, keyW); err != nil {
		t.Fatalf("authentic atomic custody evidence rejected: %v", err)
	}
	for _, tc := range []struct {
		name      string
		mutate    func(*WitnessAtomicCustodyAttestation)
		check     FinalityCheckpoint
		challenge [32]byte
		keyA      []byte
		keyW      []byte
	}{
		{"replayed challenge", nil, cp, [32]byte{8}, keyA, keyW},
		{"foreign chain", nil, func() FinalityCheckpoint { c := cp; c.Network[0]++; return c }(), challenge, keyA, keyW},
		{"changed height", nil, func() FinalityCheckpoint { c := cp; c.Height++; return c }(), challenge, keyA, keyW},
		{"wrong authority", nil, cp, challenge, keyW, keyW},
		{"wrong witness", nil, cp, challenge, keyA, keyA},
		{"mutated revision", func(a *WitnessAtomicCustodyAttestation) { a.Revision++ }, cp, challenge, keyA, keyW},
		{"zero revision", func(a *WitnessAtomicCustodyAttestation) { a.Revision = 0 }, cp, challenge, keyA, keyW},
		{"mutated witness signature", func(a *WitnessAtomicCustodyAttestation) { a.WitnessSignature[0] ^= 1 }, cp, challenge, keyA, keyW},
		{"mutated authority signature", func(a *WitnessAtomicCustodyAttestation) { a.AuthoritySignature[0] ^= 1 }, cp, challenge, keyA, keyW},
		{"mutated claimed witness key", func(a *WitnessAtomicCustodyAttestation) { a.WitnessPublicKey[0] ^= 1 }, cp, challenge, keyA, keyW},
		{"zero challenge", func(a *WitnessAtomicCustodyAttestation) { a.Challenge = [32]byte{} }, cp, challenge, keyA, keyW},
		{"truncated witness signature", func(a *WitnessAtomicCustodyAttestation) { a.WitnessSignature = a.WitnessSignature[:20] }, cp, challenge, keyA, keyW},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := valid
			a.WitnessSignature = append([]byte(nil), valid.WitnessSignature...)
			a.AuthoritySignature = append([]byte(nil), valid.AuthoritySignature...)
			a.WitnessPublicKey = append([]byte(nil), valid.WitnessPublicKey...)
			if tc.mutate != nil {
				tc.mutate(&a)
			}
			if err := VerifyWitnessAtomicCustodyAttestation(a, tc.check, tc.challenge, tc.keyA, tc.keyW); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("forged custody evidence accepted: %v", err)
			}
		})
	}
}

func TestAtomicWitnessCustodyGuardDurableIntentAndFailClosed(t *testing.T) {
	authority, witness := witnessAtomicTestKeys()
	network := types.NetworkID(types.HashBytes("atomic-guard", []byte("network")))
	root := types.HashBytes("atomic-guard", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 81, HeaderHash: types.Hash{6}}
	j := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	f := &witnessAtomicCustodianFixture{authority: authority, witness: witness}
	keyA := authority.Public().(ed25519.PublicKey)
	keyW := witness.Public().(ed25519.PublicKey)
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, f, keyA, keyW); err != nil {
		t.Fatalf("valid independent remote signatures rejected: %v", err)
	}
	f.mutate = func(a WitnessAtomicCustodyAttestation) WitnessAtomicCustodyAttestation {
		a.WitnessSignature = append([]byte(nil), a.WitnessSignature...)
		a.WitnessSignature[0] ^= 1
		return a
	}
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(context.Background(), j, cp, f, keyA, keyW); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("fake witness signature accepted: %v", err)
	}
	stored, err := j.Current()
	if err != nil || stored != cp {
		t.Fatalf("malicious remote response erased locally fsynced intent: %+v %v", stored, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := f.calls
	if _, err := ReserveAndVerifyAtomicWitnessCheckpoint(ctx, j, cp, f, keyA, keyW); !errors.Is(err, ErrWitnessSigningIntent) || f.calls != before {
		t.Fatalf("canceled request reached external custodian: %v", err)
	}
}

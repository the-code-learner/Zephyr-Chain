//go:build linux || darwin

package node

import (
	"context"
	"crypto/ed25519"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This fixture models one serialized, remotely trusted authority shared by
// hosts with independent local journals. It is NOT an HSM or real multi-host
// globally durable storage, and must never be used as a production signer.
type witnessFenceAuthorityFixture struct {
	mu          sync.Mutex
	private     ed25519.PrivateKey
	highest     FinalityCheckpoint
	revision    uint64
	unavailable error
	change      func(WitnessFencingGrant) WitnessFencingGrant
	requests    int
}

func (f *witnessFenceAuthorityFixture) ReserveWitnessFence(ctx context.Context, cp FinalityCheckpoint, challenge [32]byte) (WitnessFencingGrant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	if ctx.Err() != nil {
		return WitnessFencingGrant{}, ctx.Err()
	}
	if f.unavailable != nil {
		return WitnessFencingGrant{}, f.unavailable
	}
	if f.revision > 0 &&
		(cp.Network != f.highest.Network || cp.ValidatorRoot != f.highest.ValidatorRoot ||
			cp.Height < f.highest.Height ||
			(cp.Height == f.highest.Height && cp.HeaderHash != f.highest.HeaderHash)) {
		return WitnessFencingGrant{}, ErrWitnessSigningIntent
	}
	if f.revision == 0 || cp.Height > f.highest.Height {
		f.highest = cp
		f.revision++
	}
	grant := WitnessFencingGrant{Checkpoint: cp, Challenge: challenge, Revision: f.revision}
	digest, err := WitnessFencingGrantDigest(grant)
	if err != nil {
		return WitnessFencingGrant{}, err
	}
	grant.Signature = ed25519.Sign(f.private, digest[:])
	if f.change != nil {
		grant = f.change(grant)
	}
	return grant, nil
}

func witnessFenceTestJournal(t *testing.T, cp FinalityCheckpoint, verifier WitnessFinalityVerifier) *WitnessSigningIntentJournal {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	j, err := OpenWitnessSigningIntentJournal(filepath.Join(dir, "witness-intent"), cp.Network, cp.ValidatorRoot, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	return j
}

func TestWitnessFenceGrantBindsChallengeRevisionAndChain(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-fence", []byte("network")))
	root := types.HashBytes("witness-fence", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 55, HeaderHash: types.Hash{8}}
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 22
	private := ed25519.NewKeyFromSeed(seed)
	key := private.Public().(ed25519.PublicKey)
	challenge := [32]byte{4, 5, 6}
	authority := &witnessFenceAuthorityFixture{private: private}
	grant, err := authority.ReserveWitnessFence(context.Background(), cp, challenge)
	if err != nil || VerifyWitnessFencingGrant(grant, cp, challenge, key) != nil {
		t.Fatalf("valid signed challenged remote receipt rejected: %v", err)
	}
	for _, tc := range []struct {
		name      string
		grant     WitnessFencingGrant
		cp        FinalityCheckpoint
		challenge [32]byte
		key       []byte
	}{
		{name: "replayed previous challenge", grant: grant, cp: cp, challenge: [32]byte{9}, key: key},
		{name: "wrong authority", grant: grant, cp: cp, challenge: challenge, key: make([]byte, 32)},
		{name: "empty authority key", grant: grant, cp: cp, challenge: challenge},
		{name: "empty signature", grant: func() WitnessFencingGrant { g := grant; g.Signature = nil; return g }(), cp: cp, challenge: challenge, key: key},
		{name: "changed remote revision", grant: func() WitnessFencingGrant { g := grant; g.Revision++; return g }(), cp: cp, challenge: challenge, key: key},
		{name: "zero remote revision", grant: func() WitnessFencingGrant { g := grant; g.Revision = 0; return g }(), cp: cp, challenge: challenge, key: key},
		{name: "wrong checkpoint height", grant: grant, cp: func() FinalityCheckpoint { c := cp; c.Height++; return c }(), challenge: challenge, key: key},
		{name: "wrong checkpoint hash", grant: grant, cp: func() FinalityCheckpoint { c := cp; c.HeaderHash[0]++; return c }(), challenge: challenge, key: key},
		{name: "wrong checkpoint network", grant: grant, cp: func() FinalityCheckpoint { c := cp; c.Network[0]++; return c }(), challenge: challenge, key: key},
		{name: "wrong validator root", grant: grant, cp: func() FinalityCheckpoint { c := cp; c.ValidatorRoot[0]++; return c }(), challenge: challenge, key: key},
		{name: "mutated signed challenge", grant: func() WitnessFencingGrant { g := grant; g.Challenge[0]++; return g }(), cp: cp, challenge: challenge, key: key},
		{name: "zero challenge", grant: grant, cp: cp, challenge: [32]byte{}, key: key},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := VerifyWitnessFencingGrant(tc.grant, tc.cp, tc.challenge, tc.key); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("forged/replayed grant accepted: %v", err)
			}
		})
	}
}

func TestWitnessFenceGuardRejectsUntrustedRemoteAndSurvivesRestart(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-fence-gate", []byte("network")))
	root := types.HashBytes("witness-fence-gate", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 78, HeaderHash: types.Hash{6}}
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 13
	private := ed25519.NewKeyFromSeed(seed)
	key := private.Public().(ed25519.PublicKey)
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "intent")
	j, err := OpenWitnessSigningIntentJournal(path, network, root, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	authority := &witnessFenceAuthorityFixture{private: private, unavailable: errors.New("remote outage")}
	if err := ReserveAndVerifyWitnessFence(context.Background(), j, cp, authority, key); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("remote outage accepted: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenWitnessSigningIntentJournal(path, network, root, false, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	stored, err := j.Current()
	if err != nil || stored != cp {
		t.Fatalf("outage lost durable local reservation: %+v %v", stored, err)
	}
	authority.unavailable = nil
	for _, tc := range []struct {
		name   string
		change func(WitnessFencingGrant) WitnessFencingGrant
	}{
		{name: "wrong challenge", change: func(g WitnessFencingGrant) WitnessFencingGrant { g.Challenge[0] ^= 1; return g }},
		{name: "changed checkpoint", change: func(g WitnessFencingGrant) WitnessFencingGrant { g.Checkpoint.HeaderHash[0] ^= 1; return g }},
		{name: "zero revision", change: func(g WitnessFencingGrant) WitnessFencingGrant { g.Revision = 0; return g }},
		{name: "invalid signature", change: func(g WitnessFencingGrant) WitnessFencingGrant { g.Signature[0] ^= 1; return g }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authority.change = tc.change
			if err := ReserveAndVerifyWitnessFence(context.Background(), j, cp, authority, key); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("malicious remote acknowledgement accepted: %v", err)
			}
		})
	}
	authority.change = nil
	if err := ReserveAndVerifyWitnessFence(context.Background(), j, cp, authority, key); err != nil {
		t.Fatalf("recovered authenticated remote fence rejected: %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	before := authority.requests
	if err := ReserveAndVerifyWitnessFence(canceled, j, cp, authority, key); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("canceled context accepted: %v", err)
	}
	if authority.requests != before {
		t.Fatal("remote contacted after cancellation")
	}
}

func TestWitnessFenceGuardSharedAuthorityRefusesConflictingHosts(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-fence-hosts", []byte("network")))
	root := types.HashBytes("witness-fence-hosts", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 21, HeaderHash: types.Hash{7}}
	conflict := cp
	conflict.HeaderHash[0]++
	seed := make([]byte, ed25519.SeedSize)
	seed[0] = 11
	private := ed25519.NewKeyFromSeed(seed)
	key := private.Public().(ed25519.PublicKey)
	authority := &witnessFenceAuthorityFixture{private: private}
	j1 := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: cp.HeaderHash}})
	j2 := witnessFenceTestJournal(t, cp, witnessIntentTestVerifier{allowed: map[uint64]types.Hash{cp.Height: conflict.HeaderHash}})
	var wg sync.WaitGroup
	errs := make([]error, 2)
	wg.Add(2)
	go func() {
		defer wg.Done()
		errs[0] = ReserveAndVerifyWitnessFence(context.Background(), j1, cp, authority, key)
	}()
	go func() {
		defer wg.Done()
		errs[1] = ReserveAndVerifyWitnessFence(context.Background(), j2, conflict, authority, key)
	}()
	wg.Wait()
	if (errs[0] == nil) == (errs[1] == nil) {
		t.Fatalf("fixture authority failed to fence conflicting hosts: %v %v", errs[0], errs[1])
	}
	for i, err := range errs {
		if err != nil && !errors.Is(err, ErrWitnessSigningIntent) {
			t.Fatalf("host %d: unexpected error %v", i, err)
		}
	}
	// This tests an in-process stand-in only: a real remote authority must
	// prove global linearizability and hardware key isolation across hosts.
}

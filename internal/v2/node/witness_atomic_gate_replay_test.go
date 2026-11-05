//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A local durable reservation is never sufficient authorization when an
// authenticated external witness floor is stale, divergent or foreign.
// This exercises recovery across process restarts, not multi-host fencing.
func TestWitnessAtomicGateRejectsStaleAndForeignRemoteFloorsAfterRestart(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-floor-replay", []byte("network")))
	root := types.HashBytes("witness-floor-replay", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 42, HeaderHash: types.Hash{4}}
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
	if err := j.ReserveFinalizedIntent(cp); err != nil {
		t.Fatal(err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = OpenWitnessSigningIntentJournal(path, network, root, false, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	stale := cp
	stale.Height--
	advanced := cp
	advanced.Height++
	divergent := cp
	divergent.HeaderHash[0]++
	foreign := cp
	foreign.Network = types.NetworkID(types.HashBytes("witness-floor-replay", []byte("foreign")))
	wrongRoot := cp
	wrongRoot.ValidatorRoot = types.HashBytes("witness-floor-replay", []byte("foreign-root"))
	zeroHash := cp
	zeroHash.HeaderHash = types.Hash{}
	for _, tc := range []struct {
		name string
		floor FinalityCheckpoint
	}{
		{name: "stale replay", floor: stale},
		{name: "unexpected future", floor: advanced},
		{name: "conflicting finalized hash", floor: divergent},
		{name: "foreign network", floor: foreign},
		{name: "foreign validator root", floor: wrongRoot},
		{name: "zero hash", floor: zeroHash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := ReserveAndReconcileWitnessIntent(j, cp, witnessFloorFixture{checkpoint: tc.floor}); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("untrusted floor authorized: %v", err)
			}
			stored, err := j.Current()
			if err != nil || stored != cp {
				t.Fatalf("untrusted floor changed durable local reservation: %+v %v", stored, err)
			}
		})
	}
	if err := ReserveAndReconcileWitnessIntent(j, cp, witnessFloorFixture{checkpoint: cp}); err != nil {
		t.Fatalf("matching authenticated floor could not recover: %v", err)
	}
}

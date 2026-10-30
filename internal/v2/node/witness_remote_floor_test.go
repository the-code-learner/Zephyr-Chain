//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type witnessFloorFixture struct {
	checkpoint FinalityCheckpoint
	err        error
}

func (f witnessFloorFixture) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.checkpoint, f.err
}

func TestWitnessRemoteFloorExactRecoveryGate(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-floor", []byte("network")))
	root := types.HashBytes("witness-floor", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 19, HeaderHash: types.Hash{7}}
	verifier := witnessIntentTestVerifier{allowed: map[uint64]types.Hash{19: cp.HeaderHash}}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "journal")
	j, err := OpenWitnessSigningIntentJournal(path, network, root, true, verifier)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("empty local journal accepted external floor: %v", err)
	}
	if err := j.ReserveFinalizedIntent(cp); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{checkpoint: cp}); err != nil {
		t.Fatalf("identical independent floor rejected: %v", err)
	}
	conflict := cp
	conflict.HeaderHash[0]++
	stale := cp
	stale.Height--
	future := cp
	future.Height++
	foreign := cp
	foreign.Network = types.NetworkID{1}
	for name, floor := range map[string]FinalityCheckpoint{
		"conflicting": conflict,
		"stale": stale,
		"future": future,
		"foreign": foreign,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{checkpoint: floor}); !errors.Is(err, ErrWitnessSigningIntent) {
				t.Fatalf("mismatched remote floor accepted: %v", err)
			}
		})
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{err: errors.New("remote unreachable")}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("unreachable floor accepted: %v", err)
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("missing floor accepted: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ReconcileWitnessIntentWithExternalFloor(j, witnessFloorFixture{checkpoint: cp}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("closed journal accepted: %v", err)
	}
}

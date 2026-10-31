//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestWitnessAtomicReserveRemoteGateCrashAndConflict(t *testing.T) {
	network := types.NetworkID(types.HashBytes("atomic-witness", []byte("network")))
	root := types.HashBytes("atomic-witness", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 34, HeaderHash: types.Hash{3}}
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
	unavailable := witnessFloorFixture{err: errors.New("external floor offline")}
	if err := ReserveAndReconcileWitnessIntent(j, cp, unavailable); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("outage authorized intent: %v", err)
	}
	// Remote outages do not undo a durable local intent.
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
		t.Fatalf("outage discarded durable reservation: %+v %v", stored, err)
	}
	conflict := cp
	conflict.HeaderHash[0]++
	if err := ReserveAndReconcileWitnessIntent(j, conflict, witnessFloorFixture{checkpoint: conflict}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("conflicting intent accepted: %v", err)
	}
	if err := ReserveAndReconcileWitnessIntent(j, cp, witnessFloorFixture{checkpoint: conflict}); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("conflicting remote floor accepted: %v", err)
	}
	if err := ReserveAndReconcileWitnessIntent(j, cp, witnessFloorFixture{checkpoint: cp}); err != nil {
		t.Fatalf("same durable reservation and authenticated remote floor rejected: %v", err)
	}
	if err := ReserveAndReconcileWitnessIntent(j, cp, nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil independent remote accepted: %v", err)
	}
}

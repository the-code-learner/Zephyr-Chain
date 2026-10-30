//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func TestPinnedFinalityCheckpointMatchesCertifiedHistoricalAncestor(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("history-external-anchor")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	r, genesisRoots, journalPath, historyPath := testCertifiedHistoryRuntime(t, network, native, root)
	var atTwo types.Hash
	for height := uint64(1); height <= 3; height++ {
		candidate, err := r.BuildCandidate(height, nil)
		if err != nil {
			t.Fatal(err)
		}
		commitSchedulerCandidate(t, r, key, validators, candidate)
		if height == 2 {
			atTwo = r.ParentHash
		}
	}
	if err := r.CloseCertifiedHistory(); err != nil {
		t.Fatal(err)
	}
	checkpoint := &FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 2, HeaderHash: atTwo}
	restarted, err := NewRuntime(network, native, root, r.States, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableCertifiedHistory(historyPath, genesisRoots, false); err != nil {
		t.Fatal(err)
	}
	defer restarted.CloseCertifiedHistory()
	if err := restarted.RecoverCertifiedHistoryWithCheckpoint(validators, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, checkpoint); err != nil {
		t.Fatalf("valid independently pinned ancestor rejected: %v", err)
	}
	if restarted.Height != 3 || restarted.ParentHash != r.ParentHash {
		t.Fatal("pinned verified checkpoint changed certified tip")
	}
}

func TestPinnedFinalityCheckpointRejectsMismatchAndForeignTrust(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("history-external-negative")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	r, genesisRoots, journalPath, historyPath := testCertifiedHistoryRuntime(t, network, native, root)
	for height := uint64(1); height <= 3; height++ {
		candidate, err := r.BuildCandidate(height, nil)
		if err != nil {
			t.Fatal(err)
		}
		commitSchedulerCandidate(t, r, key, validators, candidate)
	}
	if err := r.CloseCertifiedHistory(); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name       string
		checkpoint FinalityCheckpoint
	}{
		{"future", FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 4, HeaderHash: types.Hash{8}}},
		{"wrong_hash", FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 2, HeaderHash: types.Hash{7}}},
		{"foreign_network", FinalityCheckpoint{Network: types.NetworkID{5}, ValidatorRoot: root, Height: 2, HeaderHash: types.Hash{7}}},
		{"foreign_validator_root", FinalityCheckpoint{Network: network, ValidatorRoot: types.Hash{6}, Height: 2, HeaderHash: types.Hash{7}}},
		{"zero_height", FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 0, HeaderHash: types.Hash{7}}},
		{"zero_hash", FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			restarted, err := NewRuntime(network, native, root, r.States, 1)
			if err != nil {
				t.Fatal(err)
			}
			if err := restarted.EnableGlobalCommitJournal(journalPath); err != nil {
				t.Fatal(err)
			}
			if err := restarted.EnableCertifiedHistory(historyPath, genesisRoots, false); err != nil {
				t.Fatal(err)
			}
			defer restarted.CloseCertifiedHistory()
			err = restarted.RecoverCertifiedHistoryWithCheckpoint(validators, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, &tc.checkpoint)
			if !errors.Is(err, ErrFinalityCheckpoint) {
				t.Fatalf("unverified external checkpoint accepted or wrong rejection: %v", err)
			}
			if restarted.Height != 0 {
				t.Fatalf("rejected checkpoint modified finality anchor: %d", restarted.Height)
			}
		})
	}
}

// Even a fully coherent, QC-valid backup that has been rolled back to
// height two must fail when the operator pins independently verified height
// three. The node cannot infer freshness itself: external pin is essential.
func TestPinnedFinalityCheckpointDetectsOldValidJournalAndHistoryBackup(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("history-backup-rollback")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	r, genesisRoots, journalPath, historyPath := testCertifiedHistoryRuntime(t, network, native, root)
	for height := uint64(1); height <= 2; height++ {
		candidate, err := r.BuildCandidate(height, nil)
		if err != nil {
			t.Fatal(err)
		}
		commitSchedulerCandidate(t, r, key, validators, candidate)
	}
	earlierJournal, err := os.ReadFile(journalPath)
	if err != nil {
		t.Fatal(err)
	}
	earlierHistory, err := os.ReadFile(historyPath)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := r.BuildCandidate(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidate(t, r, key, validators, candidate)
	pinned := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 3, HeaderHash: r.ParentHash}
	if err := r.CloseCertifiedHistory(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journalPath, earlierJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(historyPath, earlierHistory, 0600); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewRuntime(network, native, root, r.States, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableGlobalCommitJournal(journalPath); err != nil {
		t.Fatal(err)
	}
	if err := restarted.EnableCertifiedHistory(historyPath, genesisRoots, false); err != nil {
		t.Fatal(err)
	}
	defer restarted.CloseCertifiedHistory()
	if err := restarted.RecoverCertifiedHistoryWithCheckpoint(validators, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}, &pinned); !errors.Is(err, ErrFinalityCheckpoint) {
		t.Fatalf("rollback of valid journal+history accepted: %v", err)
	}
}

// Checkpoint is a trust input, not a private mutable file inside the WAL
// tree. Its operational placement and freshness must be secured externally.
func TestPinnedFinalityCheckpointRejectsMissingOrInconsistentData(t *testing.T) {
	checkpoint := FinalityCheckpoint{Height: 1, Network: types.NetworkID{1}, ValidatorRoot: types.Hash{2}, HeaderHash: types.Hash{3}}
	if err := checkpoint.Validate(checkpoint.Network, checkpoint.ValidatorRoot); err != nil {
		t.Fatal(err)
	}
	if err := checkpoint.Validate(types.NetworkID{4}, checkpoint.ValidatorRoot); !errors.Is(err, ErrFinalityCheckpoint) {
		t.Fatal("wrong network permitted")
	}
	if err := checkpoint.Validate(checkpoint.Network, types.Hash{4}); !errors.Is(err, ErrFinalityCheckpoint) {
		t.Fatal("wrong validators permitted")
	}
}

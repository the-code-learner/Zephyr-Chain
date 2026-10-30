//go:build linux || darwin

package node

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

func privateAnchorTestPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	return filepath.Join(dir, "high-water.anchor")
}

func TestFinalityAnchorStoreRejectsRollbackEquivocationAndTornRecord(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("external-anchor-test")))
	root := types.Hash{4}
	path := privateAnchorTestPath(t)
	s, err := OpenFinalityAnchorStore(path, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFinalityAnchorStore(path, network, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("simultaneous external anchor writer accepted: %v", err)
	}
	c1 := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 1, HeaderHash: types.Hash{1}}
	c2 := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 2, HeaderHash: types.Hash{2}}
	if err := s.advance(c1); err != nil {
		t.Fatal(err)
	}
	if err := s.advance(c1); err != nil {
		t.Fatalf("idempotent anchor rejected: %v", err)
	}
	if err := s.advance(c2); err != nil {
		t.Fatal(err)
	}
	if err := s.advance(c1); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("lower checkpoint accepted: %v", err)
	}
	equivocation := c2
	equivocation.HeaderHash = types.Hash{9}
	if err := s.advance(equivocation); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("same-height equivocation accepted: %v", err)
	}
	latest, err := s.Current()
	if err != nil || latest.Height != 2 || latest.HeaderHash != c2.HeaderHash {
		t.Fatalf("anchor changed after invalid update: %+v %v", latest, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFinalityAnchorStore(path, network, root, false)
	if err != nil {
		t.Fatal(err)
	}
	persisted, err := reopened.Current()
	if err != nil || persisted.Height != 2 || persisted.HeaderHash != c2.HeaderHash {
		t.Fatal("reopened anchor lost high-water mark")
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw[:len(raw)-1], 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFinalityAnchorStore(path, network, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("torn external anchor record accepted: %v", err)
	}
}

func TestFinalityAnchorStoreRejectsForeignIdentityCorruptionAndSymlink(t *testing.T) {
	network := types.NetworkID{1}
	root := types.Hash{4}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0700); err != nil { t.Fatal(err) }
	path := filepath.Join(dir, "anchor")
	anchor, err := OpenFinalityAnchorStore(path, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := anchor.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFinalityAnchorStore(path, types.NetworkID{2}, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("foreign network allowed: %v", err)
	}
	if _, err := OpenFinalityAnchorStore(path, network, types.Hash{5}, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("foreign root allowed: %v", err)
	}
	link := filepath.Join(dir, "symlink")
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFinalityAnchorStore(link, network, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("symlink accepted: %v", err)
	}
	if _, err := OpenFinalityAnchorStore(filepath.Join(dir, "missing"), network, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("missing external store silently initialized: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data[0] ^= 0x01
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenFinalityAnchorStore(path, network, root, false); !errors.Is(err, ErrFinalityAnchor) {
		t.Fatalf("corrupted header accepted: %v", err)
	}
}

func TestExternalFinalityAnchorPreventsCoherentQCBackupRollback(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("external-anchor-history")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	r, roots, journal, history := testCertifiedHistoryRuntime(t, network, native, root)
	anchorPath := privateAnchorTestPath(t)
	anchor, err := OpenFinalityAnchorStore(anchorPath, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.EnableFinalityAnchorStore(anchor); err != nil {
		t.Fatal(err)
	}
	for height := uint64(1); height <= 2; height++ {
		candidate, err := r.BuildCandidate(height, nil)
		if err != nil {
			t.Fatal(err)
		}
		commitSchedulerCandidate(t, r, key, validators, candidate)
	}
	priorJournal, err := os.ReadFile(journal)
	if err != nil {
		t.Fatal(err)
	}
	priorHistory, err := os.ReadFile(history)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := r.BuildCandidate(3, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidate(t, r, key, validators, candidate)
	latest, err := anchor.Current()
	if err != nil || latest.Height != 3 || latest.HeaderHash != r.ParentHash {
		t.Fatal("committed tip was not persisted to external high-water mark")
	}
	if err := r.CloseCertifiedHistory(); err != nil {
		t.Fatal(err)
	}
	if err := anchor.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(journal, priorJournal, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(history, priorHistory, 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFinalityAnchorStore(anchorPath, network, root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := NewRuntime(network, native, root, r.States, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.EnableGlobalCommitJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := recovered.EnableCertifiedHistory(history, roots, false); err != nil {
		t.Fatal(err)
	}
	defer recovered.CloseCertifiedHistory()
	if err := recovered.EnableFinalityAnchorStore(reopened); err != nil {
		t.Fatal(err)
	}
	if err := recovered.RecoverCertifiedHistory(validators, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}); !errors.Is(err, ErrFinalityCheckpoint) {
		t.Fatalf("older QC-valid backup bypassed preserved independent anchor: %v", err)
	}
	if recovered.Height != 0 {
		t.Fatal("rejected rollback changed runtime finality anchor")
	}
}

func TestExternalFinalityAnchorFailureStopsCommitAndRecovers(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("external-anchor-powerloss")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	r, roots, journal, history := testCertifiedHistoryRuntime(t, network, native, root)
	path := privateAnchorTestPath(t)
	anchor, err := OpenFinalityAnchorStore(path, network, root, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.EnableFinalityAnchorStore(anchor); err != nil {
		t.Fatal(err)
	}
	if err := anchor.Close(); err != nil {
		t.Fatal(err)
	}
	candidate, err := r.BuildCandidate(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	commitSchedulerCandidateExpectError(t, r, key, validators, candidate)
	if !r.RecoveryRequired() || r.Height != 0 {
		t.Fatal("external anchor write failure did not fail closed")
	}
	if err := r.CloseCertifiedHistory(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenFinalityAnchorStore(path, network, root, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	recovered, err := NewRuntime(network, native, root, r.States, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := recovered.EnableGlobalCommitJournal(journal); err != nil {
		t.Fatal(err)
	}
	if err := recovered.EnableCertifiedHistory(history, roots, false); err != nil {
		t.Fatal(err)
	}
	defer recovered.CloseCertifiedHistory()
	if err := recovered.EnableFinalityAnchorStore(reopened); err != nil {
		t.Fatal(err)
	}
	if err := recovered.RecoverCertifiedHistory(validators, map[uint32]worldstate.Backend{0: worldstate.NewMemory()}); err != nil {
		t.Fatalf("certified tip failed to recover after anchor outage: %v", err)
	}
	latest, err := reopened.Current()
	if err != nil || latest.Height != 1 || latest.HeaderHash != recovered.ParentHash {
		t.Fatalf("recovery failed to catch external high-water mark up: %+v %v", latest, err)
	}
}

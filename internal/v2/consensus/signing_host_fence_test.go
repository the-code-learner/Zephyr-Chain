//go:build linux || darwin

package consensus

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// This helper runs in a REAL, separate operating-system process via the
// current Go test binary. The caller controls a different independent node
// data directory (working directory) but the same protected host custody
// root for one validator key.
func TestHostFencedSigningGuardSubprocessHelper(t *testing.T) {
	if os.Getenv("ZEPHYR_HOST_FENCE_TEST_HELPER") != "1" {
		return
	}
	root := os.Getenv("ZEPHYR_HOST_FENCE_TEST_ROOT")
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	switch os.Getenv("ZEPHYR_HOST_FENCE_TEST_MODE") {
	case "busy":
		if _, err := OpenHostFencedSigningGuard(root, network, validator); !errors.Is(err, ErrSigningGuardBusy) {
			t.Fatalf("different state-dir child obtained same validator key while parent holds lock: %v", err)
		}
	case "continue":
		guard, err := OpenHostFencedSigningGuard(root, network, validator)
		if err != nil {
			t.Fatal(err)
		}
		defer guard.Close()
		if err := guard.Authorize(network, validator, 10, 0, types.Hash{9}); !errors.Is(err, ErrSigningConflict) {
			t.Fatalf("fresh different-state-dir process double-signed: %v", err)
		}
		if err := guard.Authorize(network, validator, 9, 9, types.Hash{3}); !errors.Is(err, ErrSigningConflict) {
			t.Fatalf("fresh different-state-dir process signed older height: %v", err)
		}
		if err := guard.Authorize(network, validator, 11, 0, types.Hash{4}); err != nil {
			t.Fatalf("shared journal did not allow monotonic continuation: %v", err)
		}
	default:
		t.Fatal("unknown host fence helper mode")
	}
}

func TestHostFencedSigningGuardCrossProcessDifferentNodeStateDirs(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "host-custody")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	nodeA, nodeB := filepath.Join(base, "state-A"), filepath.Join(base, "state-B")
	if err := os.Mkdir(nodeA, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(nodeB, 0700); err != nil {
		t.Fatal(err)
	}
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	guard, err := OpenHostFencedSigningGuard(root, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(network, validator, 10, 0, types.Hash{3}); err != nil {
		t.Fatal(err)
	}
	runChild := func(dir, mode string) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestHostFencedSigningGuardSubprocessHelper$", "-test.v")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"ZEPHYR_HOST_FENCE_TEST_HELPER=1",
			"ZEPHYR_HOST_FENCE_TEST_ROOT="+root,
			"ZEPHYR_HOST_FENCE_TEST_MODE="+mode)
		output, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("independent %s subprocess failed: %v: %s", mode, err, string(output))
		}
	}
	runChild(nodeB, "busy")
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	runChild(nodeB, "continue")
	next, err := OpenHostFencedSigningGuard(root, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := next.Authorize(network, validator, 11, 0, types.Hash{9}); !errors.Is(err, ErrSigningConflict) {
		t.Fatalf("restarted original state-dir double-signed child committed height: %v", err)
	}
	if err := next.Authorize(network, validator, 12, 0, types.Hash{5}); err != nil {
		t.Fatalf("shared host journal rejected monotonic new height: %v", err)
	}
}

func TestHostFencedSigningGuardRejectsRootReplacementWithSameJournalInode(t *testing.T) {
	root := filepath.Join(t.TempDir(), "shared-custody")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	network, validator := types.NetworkID{1}, types.ValidatorID{2}
	guard, err := OpenHostFencedSigningGuard(root, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := guard.Authorize(network, validator, 3, 0, types.Hash{4}); err != nil {
		t.Fatal(err)
	}
	original, err := guard.file.Stat()
	if err != nil {
		t.Fatal(err)
	}
	originalRoot := root + ".displaced"
	if err := os.Rename(root, originalRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	subdir := filepath.Base(filepath.Dir(guard.file.Name()))
	if err := os.Rename(filepath.Join(originalRoot, subdir), filepath.Join(root, subdir)); err != nil {
		t.Fatal(err)
	}
	// The journal and key-subdirectory inodes are unchanged; only the
	// *original containing host custody root* is replaced.
	current, err := os.Lstat(guard.file.Name())
	if err != nil || !os.SameFile(original, current) {
		t.Fatalf("root swap fixture did not preserve original signer inode: %v", err)
	}
	if err := guard.Authorize(network, validator, 3, 0, types.Hash{4}); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("new custody root bypassed idempotent signer safety: %v", err)
	}
	if err := os.Rename(filepath.Join(root, subdir), filepath.Join(originalRoot, subdir)); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(root); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(originalRoot, root); err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(network, validator, 4, 0, types.Hash{5}); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("restored original root unpoisoned validator signer: %v", err)
	}
}

func TestHostFencedSigningGuardRejectsUnprovisionedRootAndKeepsKeyPathsSeparate(t *testing.T) {
	root := filepath.Join(t.TempDir(), "host-root")
	n, id := types.NetworkID{1}, types.ValidatorID{2}
	if _, err := OpenHostFencedSigningGuard(root, n, id); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("created unprovisioned private custody root: %v", err)
	}
	if _, err := os.Stat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("refused root nevertheless created: %v", err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	guard, err := OpenHostFencedSigningGuard(root, n, id)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := guard.Authorize(n, id, 1, 0, types.Hash{4}); err != nil {
		t.Fatal(err)
	}
	differentValidator, err := OpenHostFencedSigningGuard(root, n, types.ValidatorID{3})
	if err != nil {
		t.Fatalf("distinct validator mistakenly shared custody lock: %v", err)
	}
	defer differentValidator.Close()
	differentNetwork, err := OpenHostFencedSigningGuard(root, types.NetworkID{3}, id)
	if err != nil {
		t.Fatalf("distinct network mistakenly shared custody lock: %v", err)
	}
	defer differentNetwork.Close()
	if guard.file.Name() == differentValidator.file.Name() ||
		guard.file.Name() == differentNetwork.file.Name() ||
		differentValidator.file.Name() == differentNetwork.file.Name() {
		t.Fatal("distinct key/network mapped to same host custody journal")
	}
	if err := os.Chmod(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(n, id, 1, 0, types.Hash{4}); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("host root became public but validator kept signing: %v", err)
	}
}

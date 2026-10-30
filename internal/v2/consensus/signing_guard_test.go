//go:build linux || darwin

package consensus

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func testSigningJournalDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "signing")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestDurableSigningGuardRejectsConflictsAcrossRestarts(t *testing.T) {
	dir := testSigningJournalDir(t)
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	hashA := types.Hash{3}
	hashB := types.Hash{4}

	guard, err := OpenDurableSigningGuard(dir, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	if !guard.Matches(network, validator) || guard.Matches(types.NetworkID{9}, validator) {
		t.Fatal("unbound guard identity")
	}
	if _, err := OpenDurableSigningGuard(dir, network, validator); !errors.Is(err, ErrSigningGuardBusy) {
		t.Fatalf("second process was allowed to share signing journal: %v", err)
	}
	if err := guard.Authorize(network, validator, 10, 0, hashA); err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(network, validator, 10, 0, hashA); err != nil {
		t.Fatalf("idempotent intent rejected: %v", err)
	}
	if err := guard.Authorize(network, validator, 10, 0, hashB); !errors.Is(err, ErrSigningConflict) {
		t.Fatalf("double vote allowed: %v", err)
	}
	if err := guard.Authorize(network, validator, 9, 9, hashA); !errors.Is(err, ErrSigningConflict) {
		t.Fatalf("rollback signing allowed: %v", err)
	}
	if err := guard.Authorize(network, types.ValidatorID{8}, 10, 1, hashB); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("validator substitution allowed: %v", err)
	}
	if err := guard.Authorize(network, validator, 10, 1, hashB); err != nil {
		t.Fatalf("next round rejected: %v", err)
	}
	if err := guard.Authorize(network, validator, 11, 0, hashA); err != nil {
		t.Fatalf("next height rejected: %v", err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(network, validator, 12, 0, hashB); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("closed signer still authorized: %v", err)
	}
	reopened, err := OpenDurableSigningGuard(dir, network, validator)
	if err != nil {
		t.Fatalf("restart rejected valid journal: %v", err)
	}
	defer reopened.Close()
	if err := reopened.Authorize(network, validator, 11, 0, hashB); !errors.Is(err, ErrSigningConflict) {
		t.Fatalf("restart forgot prior target: %v", err)
	}
	if err := reopened.Authorize(network, validator, 10, 0, hashA); !errors.Is(err, ErrSigningConflict) {
		t.Fatalf("restart accepted rollback intent: %v", err)
	}
	if err := reopened.Authorize(network, validator, 11, 0, hashA); err != nil {
		t.Fatalf("same committed intent must be retryable: %v", err)
	}
	if err := reopened.Authorize(network, validator, 12, 0, hashB); err != nil {
		t.Fatalf("continued signing after restart failed: %v", err)
	}
}

func TestDurableSigningGuardCorruptionFailsClosed(t *testing.T) {
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	t.Run("truncated journal record", func(t *testing.T) {
		dir := testSigningJournalDir(t)
		g, err := OpenDurableSigningGuard(dir, network, validator)
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Authorize(network, validator, 1, 0, types.Hash{5}); err != nil {
			t.Fatal(err)
		}
		if err := g.Close(); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "consensus-signing.journal")
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write([]byte{1, 2, 3}); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if _, err := OpenDurableSigningGuard(dir, network, validator); !errors.Is(err, ErrSigningJournal) {
			t.Fatalf("partial intent ignored: %v", err)
		}
	})
	t.Run("tampered prior hash", func(t *testing.T) {
		dir := testSigningJournalDir(t)
		g, err := OpenDurableSigningGuard(dir, network, validator)
		if err != nil {
			t.Fatal(err)
		}
		if err := g.Authorize(network, validator, 1, 0, types.Hash{5}); err != nil {
			t.Fatal(err)
		}
		_ = g.Close()
		path := filepath.Join(dir, "consensus-signing.journal")
		f, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteAt([]byte{99}, signingJournalHeaderBytes+17); err != nil {
			t.Fatal(err)
		}
		_ = f.Close()
		if _, err := OpenDurableSigningGuard(dir, network, validator); !errors.Is(err, ErrSigningJournal) {
			t.Fatalf("tampered intent accepted: %v", err)
		}
	})
	t.Run("foreign network or validator", func(t *testing.T) {
		dir := testSigningJournalDir(t)
		g, err := OpenDurableSigningGuard(dir, network, validator)
		if err != nil {
			t.Fatal(err)
		}
		_ = g.Close()
		for _, id := range []struct {
			network types.NetworkID
			validator types.ValidatorID
		}{{types.NetworkID{3}, validator}, {network, types.ValidatorID{4}}} {
			if _, err := OpenDurableSigningGuard(dir, id.network, id.validator); !errors.Is(err, ErrSigningGuard) {
				t.Fatalf("foreign signer accepted: %v", err)
			}
		}
	})
	t.Run("insecure and symlink journal", func(t *testing.T) {
		dir := testSigningJournalDir(t)
		path := filepath.Join(dir, "consensus-signing.journal")
		if err := os.Symlink(filepath.Join(dir, "another"), path); err != nil {
			t.Skip(err)
		}
		if _, err := OpenDurableSigningGuard(dir, network, validator); err == nil {
			t.Fatal("symlink accepted")
		}
		_ = os.Remove(path)
		g, err := OpenDurableSigningGuard(dir, network, validator)
		if err != nil {
			t.Fatal(err)
		}
		_ = g.Close()
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, err := OpenDurableSigningGuard(dir, network, validator); err == nil {
			t.Fatal("exposed journal accepted")
		}
	})
}

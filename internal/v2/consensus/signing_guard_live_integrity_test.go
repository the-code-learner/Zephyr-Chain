//go:build linux || darwin

package consensus

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A flock protects only the original open inode. A process with write access
// can otherwise move that locked inode away, recreate an independent journal
// under the SAME name, and use a different validator process to sign conflicts.
// The live signing guard must refuse even an *idempotent* authorization if
// its original path/inode, directory, permission or cached journal tail drift.
func TestDurableSigningGuardRejectsLiveCustodyTamperingBeforeSigning(t *testing.T) {
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	first := types.Hash{3}
	second := types.Hash{4}
	for _, tc := range []struct {
		name   string
		tamper func(t *testing.T, dir, path string)
	}{
		{"journal pathname replaced", func(t *testing.T, _, path string) {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(path, path+".displaced"); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{"journal unlinked", func(t *testing.T, _, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
		{"duplicate hardlink", func(t *testing.T, _, path string) {
			if err := os.Link(path, path+".alias"); err != nil {
				t.Fatal(err)
			}
		}},
		{"journal made public", func(t *testing.T, _, path string) {
			if err := os.Chmod(path, 0644); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent made public", func(t *testing.T, dir, _ string) {
			if err := os.Chmod(dir, 0755); err != nil {
				t.Fatal(err)
			}
		}},
		{"parent inode replaced but anchor inode unchanged", func(t *testing.T, dir, path string) {
			displaced := dir + ".displaced"
			if err := os.Rename(dir, displaced); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(displaced, filepath.Base(path)), path); err != nil {
				t.Fatal(err)
			}
			// The locked journal is now back at the exact same pathname
			// and inode, but within a different custody directory.
		}},
		{"journal truncated", func(t *testing.T, _, path string) {
			if err := os.Truncate(path, signingJournalHeaderBytes); err != nil {
				t.Fatal(err)
			}
		}},
		{"journal append", func(t *testing.T, _, path string) {
			f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.Write([]byte{99}); err != nil {
				_ = f.Close()
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}
		}},
		{"journal header mutation", func(t *testing.T, _, path string) {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteAt([]byte{99}, 5)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}},
		{"last record mutation same size", func(t *testing.T, _, path string) {
			f, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			_, err = f.WriteAt([]byte{99}, signingJournalHeaderBytes+80)
			_ = f.Close()
			if err != nil {
				t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := testSigningJournalDir(t)
			path := filepath.Join(dir, "consensus-signing.journal")
			guard, err := OpenDurableSigningGuard(dir, network, validator)
			if err != nil {
				t.Fatal(err)
			}
			defer guard.Close()
			if err := guard.Authorize(network, validator, 9, 0, first); err != nil {
				t.Fatal(err)
			}
			tc.tamper(t, dir, path)
			if err := guard.Authorize(network, validator, 9, 0, first); err == nil {
				t.Fatal("idempotent signing intent bypassed live journal custody")
			}
			if err := guard.Authorize(network, validator, 10, 0, second); !errors.Is(err, ErrSigningGuard) {
				t.Fatalf("poisoned signer authorized new height: %v", err)
			}
			if guard.Matches(network, validator) {
				t.Fatal("poisoned signer still claims usable identity")
			}
		})
	}
}

func TestDurableSigningGuardPermanentPoisonAfterJournalAndParentRestore(t *testing.T) {
	dir := testSigningJournalDir(t)
	path := filepath.Join(dir, "consensus-signing.journal")
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	first := types.Hash{3}
	next := types.Hash{4}
	guard, err := OpenDurableSigningGuard(dir, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	defer guard.Close()
	if err := guard.Authorize(network, validator, 5, 0, first); err != nil {
		t.Fatal(err)
	}
	original, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	displaced := dir + ".displaced"
	if err := os.Rename(dir, displaced); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(displaced, "consensus-signing.journal"), path); err != nil {
		t.Fatal(err)
	}
	returned, err := os.Lstat(path)
	if err != nil || !os.SameFile(original, returned) {
		t.Fatalf("attack fixture did not preserve same locked journal inode: %v", err)
	}
	if err := guard.Authorize(network, validator, 6, 0, next); err == nil {
		t.Fatal("replaced parent allowed consensus signature")
	}
	if err := os.Rename(path, filepath.Join(displaced, "consensus-signing.journal")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(displaced, dir); err != nil {
		t.Fatal(err)
	}
	if err := guard.Authorize(network, validator, 6, 0, next); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("restored exact original parent/journal unpoisoned consensus guard: %v", err)
	}
	if guard.Matches(network, validator) {
		t.Fatal("poisoned journal was trusted after parent restored")
	}
}

func TestDurableSigningGuardRejectsPreexistingHardlinkAndRecoveryAfterCorruptLiveTail(t *testing.T) {
	dir := testSigningJournalDir(t)
	network := types.NetworkID{1}
	validator := types.ValidatorID{2}
	guard, err := OpenDurableSigningGuard(dir, network, validator)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "consensus-signing.journal")
	if err := guard.Authorize(network, validator, 1, 0, types.Hash{1}); err != nil {
		t.Fatal(err)
	}
	if err := guard.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Link(path, path+".alias"); err != nil {
		t.Fatal(err)
	}
	if _, err := OpenDurableSigningGuard(dir, network, validator); !errors.Is(err, ErrSigningGuard) {
		t.Fatalf("same journal with multiple hardlinks accepted at startup: %v", err)
	}
	if err := os.Remove(path + ".alias"); err != nil {
		t.Fatal(err)
	}
	second, err := OpenDurableSigningGuard(dir, network, validator)
	if err != nil {
		t.Fatalf("valid journal refused after hardlink removed: %v", err)
	}
	defer second.Close()
	if err := second.Authorize(network, validator, 2, 0, types.Hash{2}); err != nil {
		t.Fatalf("legitimate continuation after restart failed: %v", err)
	}
}

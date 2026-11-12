package consensus

import (
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// OpenHostFencedSigningGuard is the stricter same-host constructor for
// validator signing across INDEPENDENT node state directories. Every process
// holding one validator key on one host must use the SAME independently
// provisioned, owner-only custody root. A canonical subdirectory is derived
// exclusively from NetworkID and ValidatorID; the private consensus journal
// and OS exclusive file lock therefore stay shared across node data-dir
// migrations, simultaneous processes and application restarts.
//
// The custody root MUST already exist as a private, physically durable,
// independently backed-up/provisioned directory. The function refuses to
// create it implicitly from the untrusted node chain-state directory.
// Network and validator ID must be independently pinned; signing callers
// additionally validate the actual validator public key against the ID.
//
// This is a SAME-HOST best-effort OS/journal protection, NOT a remote HSM,
// globally linearizable key fence, cryptographic host identity or guard
// against an operator that configures a second root, copies the private key
// to another host or restores the entire host custody root from backup.
// Hardware/remote key custody and independent multi-host fault drills remain
// P0 mainnet release blockers.
func OpenHostFencedSigningGuard(root string, network types.NetworkID, validator types.ValidatorID) (*DurableSigningGuard, error) {
	if root == "" || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validator)) {
		return nil, ErrSigningGuard
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, ErrSigningGuard
	}
	absolute = filepath.Clean(absolute)
	identity, err := os.Lstat(absolute)
	if err != nil || !identity.IsDir() || identity.Mode().Perm()&0077 != 0 {
		return nil, ErrSigningGuard
	}
	keyDir := "validator-" + hex.EncodeToString(network[:]) + "-" + hex.EncodeToString(validator[:])
	guard, err := OpenDurableSigningGuard(filepath.Join(absolute, keyDir), network, validator)
	if err != nil {
		return nil, err
	}
	guard.mu.Lock()
	guard.hostRootPath = absolute
	guard.hostRootIdentity = identity
	err = guard.verifyLiveLocked()
	if err != nil {
		guard.poisoned = true
	}
	guard.mu.Unlock()
	if err != nil {
		_ = guard.Close()
		return nil, ErrSigningGuard
	}
	// Persist the new key-specific directory entry. Fsyncing only the
	// journal and key subdirectory does not durably establish its entry
	// in the parent custody root after a sudden power failure.
	dir, err := os.Open(absolute)
	if err != nil {
		_ = guard.Close()
		return nil, ErrSigningGuard
	}
	syncErr := dir.Sync()
	closeErr := dir.Close()
	if syncErr != nil || closeErr != nil {
		_ = guard.Close()
		return nil, ErrSigningGuard
	}
	guard.mu.Lock()
	err = guard.verifyLiveLocked()
	if err != nil {
		guard.poisoned = true
	}
	guard.mu.Unlock()
	if err != nil {
		_ = guard.Close()
		return nil, ErrSigningGuard
	}
	return guard, nil
}

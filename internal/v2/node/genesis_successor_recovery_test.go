package node

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

// A malicious disk editor can recompute the unkeyed journal checksum.
// Recovery must also validate the QC cryptographic signatures.
func TestGenesisSuccessorRecoveryRejectsForgedQCEvenWithValidJournalChecksum(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("first-certified-height")))
	native := types.TokenID(types.HashBytes("token", []byte("ZPH")))
	key, validators, root := schedulerValidatorSet(t, network)
	state := worldstate.NewMemory()
	genesisRoot := state.Root()
	runtime, err := NewRuntime(network, native, root, map[uint32]worldstate.Backend{0: state}, 1)
	if err != nil { t.Fatal(err) }
	path := filepath.Join(t.TempDir(), "global-commit.journal")
	if err := runtime.EnableGlobalCommitJournal(path); err != nil { t.Fatal(err) }
	candidate, err := runtime.BuildCandidate(1, nil)
	if err != nil { t.Fatal(err) }
	commitSchedulerCandidate(t, runtime, key, validators, candidate)
	intent, err := readGlobalCommitJournal(path)
	if err != nil { t.Fatal(err) }
	intent.Certificate.Votes[0].Signature[0] ^= 0x01
	// Re-encode using the *correct* unkeyed journal digest, not a trivial
	// bit-flip detectable only by the file checksum.
	if err := writeGlobalCommitJournal(path, intent); err != nil { t.Fatal(err) }
	restarted, err := NewRuntime(network, native, root, map[uint32]worldstate.Backend{0: state}, 1)
	if err != nil { t.Fatal(err) }
	if err := restarted.EnableGlobalCommitJournal(path); err != nil { t.Fatal(err) }
	if err := restarted.RecoverCertifiedGenesisSuccessor(validators, map[uint32]types.Hash{0: genesisRoot}); !errors.Is(err, ErrGlobalCommitJournal) {
		t.Fatalf("tampered QC accepted with valid checksum: %v", err)
	}
}

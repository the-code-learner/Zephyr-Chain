package consensus

import (
	"crypto/elliptic"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	signingJournalMagic             = "ZSG2"
	signingJournalVersion     uint8 = 1
	signingJournalHeaderBytes       = 69
	signingJournalRecordBytes       = 112
)

var (
	ErrSigningGuard            = errors.New("invalid or unavailable durable consensus signing guard")
	ErrSigningConflict         = errors.New("conflicting or stale consensus signing intent")
	ErrSigningJournal          = errors.New("corrupt or truncated consensus signing journal")
	ErrSigningGuardBusy        = errors.New("consensus signing journal already held by another writer")
	ErrSigningGuardUnsupported = errors.New("durable consensus signing guard unsupported on this operating system")
)

// DurableSigningGuard records the target consensus header hash before the
// validator signs either a proposal or a vote. The journal and its OS lock
// MUST be shared by every process holding this validator key. It is not
// safe to use independent copies of the same key with separate journals.
type DurableSigningGuard struct {
	mu               sync.Mutex
	file             *os.File
	parentIdentity   os.FileInfo
	hostRootPath     string
	hostRootIdentity os.FileInfo
	recordCount      uint64
	network          types.NetworkID
	validator        types.ValidatorID
	height           uint64
	round            uint64
	target           types.Hash
	lastRecordHash   types.Hash
	hasRecord        bool
	closed           bool
	poisoned         bool
}

// OpenDurableSigningGuard verifies the journal's network, validator and
// complete hash-chained history. It fails closed on partial records and
// never truncates or reinitializes a corrupt existing journal.
func OpenDurableSigningGuard(dir string, network types.NetworkID, validator types.ValidatorID) (guard *DurableSigningGuard, err error) {
	if dir == "" || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validator)) {
		return nil, ErrSigningGuard
	}
	// Bind the live journal to a stable absolute pathname even if an
	// embedding process later changes its working directory.
	canonicalDir, err := filepath.Abs(dir)
	if err != nil {
		return nil, ErrSigningGuard
	}
	dir = filepath.Clean(canonicalDir)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrSigningGuard
	}
	path := filepath.Join(dir, "consensus-signing.journal")
	file, created, err := openLockedSigningJournal(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = file.Close()
		}
	}()
	if !validSigningJournalFile(file) || !validSigningGuardParent(path, info) {
		return nil, ErrSigningGuard
	}
	guard = &DurableSigningGuard{file: file, parentIdentity: info, network: network, validator: validator}
	if created {
		var w codec.Writer
		w.Fixed([]byte(signingJournalMagic))
		w.U8(signingJournalVersion)
		w.Fixed(network[:])
		w.Fixed(validator[:])
		if _, err = file.Write(w.BytesCopy()); err != nil {
			return nil, err
		}
		if err = file.Sync(); err != nil {
			return nil, err
		}
		d, openErr := os.Open(dir)
		if openErr != nil {
			return nil, openErr
		}
		err = d.Sync()
		closeErr := d.Close()
		if err != nil {
			return nil, err
		}
		if closeErr != nil {
			return nil, closeErr
		}
	}
	if err = guard.replay(); err != nil {
		return nil, err
	}
	if err = guard.verifyLiveLocked(); err != nil {
		return nil, err
	}
	return guard, nil
}

func (g *DurableSigningGuard) replay() error {
	if _, err := g.file.Seek(0, io.SeekStart); err != nil {
		return err
	}
	var header [signingJournalHeaderBytes]byte
	if _, err := io.ReadFull(g.file, header[:]); err != nil {
		return ErrSigningJournal
	}
	if string(header[:4]) != signingJournalMagic || header[4] != signingJournalVersion {
		return ErrSigningJournal
	}
	var network types.NetworkID
	var validator types.ValidatorID
	copy(network[:], header[5:37])
	copy(validator[:], header[37:69])
	if network != g.network || validator != g.validator {
		return ErrSigningGuard
	}
	var previous types.Hash
	for {
		var raw [signingJournalRecordBytes]byte
		_, err := io.ReadFull(g.file, raw[:])
		if err == io.EOF {
			break
		}
		if err != nil {
			return ErrSigningJournal
		}
		r := codec.NewReader(raw[:])
		height, e1 := r.U64()
		round, e2 := r.U64()
		targetRaw, e3 := r.Fixed(32)
		prevRaw, e4 := r.Fixed(32)
		hashRaw, e5 := r.Fixed(32)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || r.Done() != nil || height == 0 {
			return ErrSigningJournal
		}
		var target, prev, hash types.Hash
		copy(target[:], targetRaw)
		copy(prev[:], prevRaw)
		copy(hash[:], hashRaw)
		if types.IsZero32([32]byte(target)) || prev != previous ||
			hash != types.Hash(codec.DomainHash("zephyr/consensus/signing-intent/v1", raw[:80])) {
			return ErrSigningJournal
		}
		// A signing key must never endorse different consensus header
		// hashes at one height, including across later rounds. Without a
		// separately proven BFT lock/unlock rule, two authentic quorums
		// for different same-height headers would be a safety violation.
		// Check this on FULL replay, not just new Authorize calls.
		if g.hasRecord && (height < g.height ||
			(height == g.height && (round <= g.round || target != g.target))) {
			return ErrSigningJournal
		}
		g.height, g.round, g.target, g.lastRecordHash, g.hasRecord = height, round, target, hash, true
		g.recordCount++
		previous = hash
	}
	_, err := g.file.Seek(0, io.SeekEnd)
	return err
}

// validSigningGuardParent rechecks the containing directory inode and mode.
// The same locked journal inode can be moved back into a *different* private
// directory under the original pathname. The original parent is therefore
// part of the local signer custody boundary.
func validSigningGuardParent(path string, pinned os.FileInfo) bool {
	if pinned == nil || !pinned.IsDir() || pinned.Mode().Perm()&0077 != 0 {
		return false
	}
	current, err := os.Lstat(filepath.Dir(path))
	return err == nil && current.IsDir() && current.Mode() == pinned.Mode() &&
		os.SameFile(current, pinned)
}

// verifyLiveLocked checks the current locked inode is still the *only*
// canonical link under the original owner-only directory, and that its
// header, exact append length and latest record match our cached state.
// Run even for idempotent retries. This is intentionally O(1) per signature
// rather than replaying an unbounded multi-year journal on every vote.
// Startup still replays/verifies the COMPLETE hash chain. In-place edits to
// historic records without changing the last record/size remain a privileged
// host tampering threat and require external nonrollbackable key custody.
func (g *DurableSigningGuard) verifyLiveLocked() error {
	if g.file == nil || !validSigningGuardParent(g.file.Name(), g.parentIdentity) ||
		!validSigningJournalFile(g.file) ||
		(g.hostRootPath != "" && !validSigningGuardParent(
			filepath.Join(g.hostRootPath, "_host_custody_root"), g.hostRootIdentity)) {
		return ErrSigningGuard
	}
	info, err := g.file.Stat()
	if err != nil || info.Size() < signingJournalHeaderBytes ||
		(info.Size()-signingJournalHeaderBytes)%signingJournalRecordBytes != 0 ||
		uint64((info.Size()-signingJournalHeaderBytes)/signingJournalRecordBytes) != g.recordCount {
		return ErrSigningJournal
	}
	var header [signingJournalHeaderBytes]byte
	if _, err := g.file.ReadAt(header[:], 0); err != nil ||
		string(header[:4]) != signingJournalMagic || header[4] != signingJournalVersion {
		return ErrSigningJournal
	}
	var network types.NetworkID
	var validator types.ValidatorID
	copy(network[:], header[5:37])
	copy(validator[:], header[37:69])
	if network != g.network || validator != g.validator {
		return ErrSigningGuard
	}
	if g.recordCount == 0 {
		if g.hasRecord {
			return ErrSigningJournal
		}
	} else {
		if !g.hasRecord {
			return ErrSigningJournal
		}
		var last [signingJournalRecordBytes]byte
		if _, err := g.file.ReadAt(last[:], info.Size()-signingJournalRecordBytes); err != nil {
			return ErrSigningJournal
		}
		r := codec.NewReader(last[:])
		height, e1 := r.U64()
		round, e2 := r.U64()
		targetRaw, e3 := r.Fixed(32)
		_, e4 := r.Fixed(32)
		hashRaw, e5 := r.Fixed(32)
		if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil ||
			r.Done() != nil || height != g.height || round != g.round {
			return ErrSigningJournal
		}
		var target, digest types.Hash
		copy(target[:], targetRaw)
		copy(digest[:], hashRaw)
		if target != g.target || digest != g.lastRecordHash ||
			digest != types.Hash(codec.DomainHash("zephyr/consensus/signing-intent/v1", last[:80])) {
			return ErrSigningJournal
		}
	}
	if !validSigningGuardParent(g.file.Name(), g.parentIdentity) ||
		!validSigningJournalFile(g.file) ||
		(g.hostRootPath != "" && !validSigningGuardParent(
			filepath.Join(g.hostRootPath, "_host_custody_root"), g.hostRootIdentity)) {
		return ErrSigningGuard
	}
	return nil
}

func (g *DurableSigningGuard) Matches(network types.NetworkID, validator types.ValidatorID) bool {
	if g == nil {
		return false
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.poisoned || g.network != network || g.validator != validator {
		return false
	}
	if err := g.verifyLiveLocked(); err != nil {
		g.poisoned = true
		return false
	}
	return true
}

// Authorize persists one monotonic signing intent to stable storage before
// returning success. Both proposals and votes for one (height, round) must
// target the same header; a replay of the same exact intent is permitted.
// Any record write/sync error poisons this instance until operator recovery.
func (g *DurableSigningGuard) Authorize(network types.NetworkID, validator types.ValidatorID, height, round uint64, target types.Hash) error {
	if g == nil {
		return ErrSigningGuard
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed || g.poisoned || g.network != network || g.validator != validator ||
		height == 0 || types.IsZero32([32]byte(target)) {
		return ErrSigningGuard
	}
	// Check path and live inode integrity BEFORE idempotent replay; a stale
	// locked descriptor must never authorize another signature.
	if err := g.verifyLiveLocked(); err != nil {
		g.poisoned = true
		return err
	}
	if g.hasRecord {
		if height < g.height || (height == g.height && round < g.round) {
			return ErrSigningConflict
		}
		if height == g.height && target != g.target {
			// Monotonic rounds alone do not certify that a validator
			// safely unlocked its old header. Until a verifiable unlock
			// protocol exists, remain locked on the first signed target
			// for the entire height, even after a process restart.
			return ErrSigningConflict
		}
		if height == g.height && round == g.round {
			return nil
		}
	}
	var w codec.Writer
	w.U64(height)
	w.U64(round)
	w.Fixed(target[:])
	w.Fixed(g.lastRecordHash[:])
	body := w.BytesCopy()
	digest := types.Hash(codec.DomainHash("zephyr/consensus/signing-intent/v1", body))
	w.Fixed(digest[:])
	encoded := w.BytesCopy()
	if len(encoded) != signingJournalRecordBytes {
		g.poisoned = true
		return ErrSigningJournal
	}
	n, err := g.file.Write(encoded)
	if err != nil || n != len(encoded) {
		g.poisoned = true
		return ErrSigningJournal
	}
	if err := g.file.Sync(); err != nil {
		g.poisoned = true
		return ErrSigningJournal
	}
	g.height, g.round, g.target, g.lastRecordHash, g.hasRecord = height, round, target, digest, true
	g.recordCount++
	if err := g.verifyLiveLocked(); err != nil {
		g.poisoned = true
		return err
	}
	return nil
}

func (g *DurableSigningGuard) Close() error {
	if g == nil {
		return nil
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.closed {
		return nil
	}
	g.closed = true
	return g.file.Close()
}

// A consensus key's identity is derived exclusively from its P-256 public
// key, not a caller-provided validator ID.
func SigningValidatorID(publicKey []byte) (types.ValidatorID, error) {
	if len(publicKey) != 65 {
		return types.ValidatorID{}, ErrSigningGuard
	}
	x, y := elliptic.Unmarshal(elliptic.P256(), publicKey)
	if x == nil || y == nil {
		return types.ValidatorID{}, ErrSigningGuard
	}
	return types.ValidatorIDFromPublicKey(publicKey), nil
}

package node

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
 "github.com/zephyr-chain/zephyr-chain/internal/v2/economics"
	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

const (
	certifiedHistoryMagic = "ZFH2"
	maxCertifiedHistoryBytes int64 = 512 << 20
	maxCertifiedHistoryRecords = 4096
)

var ErrCertifiedHistory = errors.New("invalid, truncated or non-contiguous v2 certified finality history")

// CertifiedHistory holds a strictly append-only, fsynced series of quorum-
// certified commit intents. Caller must retain this file together with the
// WAL and immutable genesis; this *does not* prevent full-directory rollback.
type CertifiedHistory struct {
	mu sync.Mutex
	file *os.File
	network types.NetworkID
	native types.TokenID
	validatorRoot types.Hash
	shards uint32
	roots map[uint32]types.Hash
	height uint64
	parent types.Hash
	lastDigest types.Hash
	count uint32
	closed bool
	poisoned bool
}

// openCertifiedHistory binds the history file to the independently pinned
// genesis roots and obtains an OS-level exclusive lock. Existing history is
// never re-created or silently truncated.
func openCertifiedHistory(path string, network types.NetworkID, native types.TokenID, validatorRoot types.Hash, genesisRoots map[uint32]types.Hash, create bool) (*CertifiedHistory, error) {
	if path == "" || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(native)) ||
		types.IsZero32([32]byte(validatorRoot)) || len(genesisRoots) == 0 || len(genesisRoots) > 16 {
		return nil, ErrCertifiedHistory
	}
	for shard := 0; shard < len(genesisRoots); shard++ {
		if types.IsZero32([32]byte(genesisRoots[uint32(shard)])) {
			return nil, ErrCertifiedHistory
		}
	}
	file, err := openLockedCertifiedHistory(path, create)
	if err != nil {
		return nil, err
	}
	fail := true
	defer func() { if fail { _ = file.Close() } }()
	h := &CertifiedHistory{
		file: file, network: network, native: native, validatorRoot: validatorRoot,
		shards: uint32(len(genesisRoots)), roots: cloneHistoryRoots(genesisRoots),
	}
	var w codec.Writer
	w.Fixed([]byte(certifiedHistoryMagic))
	w.Fixed(network[:])
	w.Fixed(native[:])
	w.Fixed(validatorRoot[:])
	w.U32(h.shards)
	for shard := uint32(0); shard < h.shards; shard++ { root := genesisRoots[shard]; w.Fixed(root[:]) }
	header := w.BytesCopy()
	if create {
		n, e := file.Write(header)
		if e != nil || n != len(header) || file.Sync() != nil {
			return nil, ErrCertifiedHistory
		}
		if err := syncHistoryDirectory(path); err != nil { return nil, err }
	} else {
		actual := make([]byte, len(header))
		if _, err := io.ReadFull(file, actual); err != nil || !bytes.Equal(actual, header) {
			return nil, ErrCertifiedHistory
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil { return nil, err }
	fail = false
	return h, nil
}

func cloneHistoryRoots(in map[uint32]types.Hash) map[uint32]types.Hash {
	out := make(map[uint32]types.Hash,len(in))
	for id, root := range in { out[id] = root }
	return out
}

func (h *CertifiedHistory) Close() error {
	if h == nil { return nil }
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed { return nil }
	h.closed = true
	return h.file.Close()
}

func (h *CertifiedHistory) checkIntent(intent globalCommitIntent, validators v2consensus.ValidatorSet) error {
	if intent.Validate() != nil || intent.Network != h.network || intent.NativeToken != h.native ||
		intent.ValidatorRoot != h.validatorRoot || intent.ShardCount != h.shards ||
		intent.Header.Height != h.height+1 || intent.PreHeight != h.height ||
		intent.PreParentHash != h.parent || intent.Header.ParentHash != h.parent ||
		intent.Header.EffectiveNextValidatorRoot() != h.validatorRoot ||
		intent.Header.CertificateHash != (types.Hash{}) {
		return ErrCertifiedHistory
	}
	root, err := validators.Root()
	if err != nil || root != h.validatorRoot || validators.Network != h.network ||
		validators.VerifyCertificate(intent.Certificate) != nil {
		return ErrCertifiedHistory
	}
	post := make(map[uint32]types.Hash, len(intent.Commitments))
	for _, c := range intent.Commitments { post[c.ShardID] = c.StateRoot }
	deltas := make(map[uint32]journalShardDelta,len(intent.Deltas))
	for _, d := range intent.Deltas { deltas[d.ShardID] = d }
	for shard := uint32(0); shard < h.shards; shard++ {
		old, ok := h.roots[shard]
		if !ok { return ErrCertifiedHistory }
		if d, changed := deltas[shard]; changed {
			if d.PreRoot != old || d.PostRoot != post[shard] { return ErrCertifiedHistory }
		} else if post[shard] != old { return ErrCertifiedHistory }
	}
	return nil
}

// Append stores the *certified* intended transition before any shard is
// mutated. Partial writes or fsync failures poison this writer and require
// explicit operator recovery; the file is never auto-truncated.
func (h *CertifiedHistory) Append(intent globalCommitIntent, validators v2consensus.ValidatorSet) error {
	if h == nil { return ErrCertifiedHistory }
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.poisoned || h.count >= maxCertifiedHistoryRecords || h.checkIntent(intent, validators) != nil { return ErrCertifiedHistory }
	intent.Status = globalCommitPreparing
	payload, err := intent.MarshalBinary()
	if err != nil || len(payload) == 0 || len(payload) > maxGlobalCommitJournalBytes { return ErrCertifiedHistory }
	var w codec.Writer
	w.Fixed(h.lastDigest[:])
	w.Fixed(payload)
	digest := types.Hash(codec.DomainHash("zephyr/finality-history-record/v2", w.BytesCopy()))
	var record codec.Writer
	record.U32(uint32(len(payload)))
	record.Fixed(payload)
	record.Fixed(digest[:])
	raw := record.BytesCopy()
	info, err := h.file.Stat()
	if err != nil || info.Size()+int64(len(raw)) > maxCertifiedHistoryBytes {
		h.poisoned = true
		return ErrCertifiedHistory
	}
	n, err := h.file.Write(raw)
	if err != nil || n != len(raw) {
		h.poisoned = true
		return ErrCertifiedHistory
	}
	if err := h.file.Sync(); err != nil {
		h.poisoned = true
		return ErrCertifiedHistory
	}
	for _, c := range intent.Commitments { h.roots[c.ShardID] = c.StateRoot }
	h.height, h.parent, h.lastDigest, h.count = intent.Header.Height, v2consensus.HeaderConsensusHash(intent.Header), digest, h.count+1
	return nil
}

// Replay re-verifies *every* QC and independently applies each journal
// delta to genesis-seeded in-memory state, then binds the terminal record to
// the latest on-disk global commit journal. It does not infer missing history
// from an RPC or permit validator-set changes.
func (h *CertifiedHistory) Replay(validators v2consensus.ValidatorSet, genesis map[uint32]worldstate.Backend, latestJournalPath string) (globalCommitIntent, error) {
	if h == nil || len(genesis) != int(h.shards) || latestJournalPath == "" { return globalCommitIntent{}, ErrCertifiedHistory }
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || h.poisoned || h.count != 0 || h.height != 0 { return globalCommitIntent{}, ErrCertifiedHistory }
	info, err := h.file.Stat()
	if err != nil || info.Size() > maxCertifiedHistoryBytes { return globalCommitIntent{}, ErrCertifiedHistory }
	offset := int64(4+32+32+32+4+32*h.shards)
	if _, err := h.file.Seek(offset, io.SeekStart); err != nil { return globalCommitIntent{}, err }
	var last globalCommitIntent
	var previous types.Hash
	for {
		var size [4]byte
		_, err := io.ReadFull(h.file, size[:])
		if err == io.EOF { break }
		if err != nil { return globalCommitIntent{}, ErrCertifiedHistory }
		length := uint32(size[0])<<24 | uint32(size[1])<<16 | uint32(size[2])<<8 | uint32(size[3])
		if length == 0 || length > maxGlobalCommitJournalBytes || h.count >= maxCertifiedHistoryRecords {
			return globalCommitIntent{}, ErrCertifiedHistory
		}
		raw := make([]byte,int(length))
		var digest types.Hash
		if _, err := io.ReadFull(h.file, raw); err != nil { return globalCommitIntent{}, ErrCertifiedHistory }
		if _, err := io.ReadFull(h.file,digest[:]); err != nil { return globalCommitIntent{}, ErrCertifiedHistory }
		var w codec.Writer
		w.Fixed(previous[:])
		w.Fixed(raw)
		if digest != types.Hash(codec.DomainHash("zephyr/finality-history-record/v2",w.BytesCopy())) { return globalCommitIntent{}, ErrCertifiedHistory }
		intent, err := parseGlobalCommitIntent(raw)
		if err != nil || intent.Status != globalCommitPreparing || h.checkIntent(intent,validators) != nil {
			return globalCommitIntent{}, ErrCertifiedHistory
		}
		deltas := make(map[uint32]journalShardDelta,len(intent.Deltas))
		for _, d := range intent.Deltas { deltas[d.ShardID] = d }
		for shard := uint32(0); shard < h.shards; shard++ {
			store, ok := genesis[shard]
			if !ok || store == nil || store.Root() != h.roots[shard] { return globalCommitIntent{}, ErrCertifiedHistory }
			if d, changed := deltas[shard]; changed {
				result, err := store.Apply(d.Consumed,d.Created)
				if err != nil || result != d.PostRoot { return globalCommitIntent{}, ErrCertifiedHistory }
			}
		}
		for _, c := range intent.Commitments { h.roots[c.ShardID] = c.StateRoot }
		h.height, h.parent, h.lastDigest, h.count = intent.Header.Height, v2consensus.HeaderConsensusHash(intent.Header), digest, h.count+1
		previous = digest
		last = intent
	}
	if h.count == 0 { return globalCommitIntent{}, ErrCertifiedHistory }
	// Global journal may be PREPARING (recoverable power loss) or
	// COMMITTED. Its full transition must match the final history event.
	current, err := readGlobalCommitJournal(latestJournalPath)
	if err != nil { return globalCommitIntent{}, ErrCertifiedHistory }
	current.Status = globalCommitPreparing
	latestRaw, err := current.MarshalBinary()
	if err != nil { return globalCommitIntent{}, ErrCertifiedHistory }
	historyRaw, err := last.MarshalBinary()
	if err != nil || !bytes.Equal(latestRaw,historyRaw) { return globalCommitIntent{}, ErrCertifiedHistory }
	if _, err := h.file.Seek(0,io.SeekEnd); err != nil { return globalCommitIntent{}, err }
	return last,nil
}

func (r *Runtime) EnableCertifiedHistory(path string, genesisRoots map[uint32]types.Hash, create bool) error {
	if r == nil { return ErrCertifiedHistory }
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.history != nil || r.globalCommitJournalPath == "" || r.Height != 0 ||
		r.ParentHash != (types.Hash{}) || len(genesisRoots) != int(r.ShardCount) {
		return ErrCertifiedHistory
	}
	h, err := openCertifiedHistory(path, r.Network, r.NativeToken, r.ValidatorRoot, genesisRoots, create)
	if err != nil { return err }
	r.history = h
	if !create { r.recoveryRequired = true }
	return nil
}

func (r *Runtime) CloseCertifiedHistory() error {
	if r == nil { return nil }
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.history == nil { return nil }
	// Keep the closed guard attached so the runtime cannot resume
	// committing without a history file after close.
	return r.history.Close()
}

func (r *Runtime) RecoverCertifiedHistory(validators v2consensus.ValidatorSet, genesis map[uint32]worldstate.Backend) error {
	if r == nil { return ErrCertifiedHistory }
	r.mu.Lock()
	h, path := r.history, r.globalCommitJournalPath
	r.mu.Unlock()
	if h == nil { return ErrCertifiedHistory }
	last, err := h.Replay(validators, genesis, path)
	if err != nil { return fmt.Errorf("%w: %v",ErrCertifiedHistory,err) }
	// The last on-disk PREPARING intent is idempotently reconciled with its
	// exact pre/post state. The full chain and genesis roots were verified
	// independently above; no prior block is silently trusted.
	if err := r.RecoverGlobalCommitJournal(validators,nil,economics.ShadowEpochEngineConfig{}); err != nil { return err }
	if r.Height != last.Header.Height || r.ParentHash != v2consensus.HeaderConsensusHash(last.Header) || r.RecoveryRequired() {
		return ErrCertifiedHistory
	}
	return nil
}

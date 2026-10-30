package node

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const (
	finalityAnchorMagic      = "ZFA2"
	finalityAnchorHeaderSize = 4 + 32 + 32
	finalityAnchorRecordSize = 8 + 32 + 32 + 32
	maxFinalityAnchorRecords = 4096
)

var ErrFinalityAnchor = errors.New("invalid, missing, non-monotonic or unavailable external finality anchor")

// FinalityAnchorStore is an independently placed, locally fsynced high-water
// mark for QC-verified finalized headers. It must live OUTSIDE the observer
// state directory and be retained in a distinct failure/backup domain.
//
// This local file is NOT rollback-proof if the *anchor file itself* can be
// restored to an older snapshot. No remote witness, TPM, HSM monotonic counter,
// independent operator, or authenticated checkpoint publisher is implied.
type FinalityAnchorStore struct {
	mu            sync.Mutex
	file          *os.File
	network       types.NetworkID
	validatorRoot types.Hash
	height        uint64
	hash          types.Hash
	lastDigest    types.Hash
	count         uint32
	closed        bool
	poisoned      bool
}

func OpenFinalityAnchorStore(path string, network types.NetworkID, validatorRoot types.Hash, create bool) (*FinalityAnchorStore, error) {
	if path == "" || types.IsZero32([32]byte(network)) || types.IsZero32([32]byte(validatorRoot)) {
		return nil, ErrFinalityAnchor
	}
	file, err := openLockedFinalityAnchor(path, create)
	if err != nil {
		return nil, err
	}
	success := false
	defer func() {
		if !success {
			_ = file.Close()
		}
	}()
	store := &FinalityAnchorStore{file: file, network: network, validatorRoot: validatorRoot}
	header := make([]byte, finalityAnchorHeaderSize)
	copy(header, finalityAnchorMagic)
	copy(header[4:36], network[:])
	copy(header[36:], validatorRoot[:])
	if create {
		n, writeErr := file.Write(header)
		if writeErr != nil || n != len(header) || file.Sync() != nil {
			return nil, ErrFinalityAnchor
		}
		if err := syncHistoryDirectory(path); err != nil {
			return nil, err
		}
	} else {
		read := make([]byte, len(header))
		if _, err := file.ReadAt(read, 0); err != nil {
			return nil, ErrFinalityAnchor
		}
		for i := range header {
			if header[i] != read[i] {
				return nil, ErrFinalityAnchor
			}
		}
		stat, err := file.Stat()
		if err != nil || stat.Size() < int64(len(header)) ||
			(stat.Size()-int64(len(header)))%finalityAnchorRecordSize != 0 ||
			(stat.Size()-int64(len(header)))/finalityAnchorRecordSize > maxFinalityAnchorRecords {
			return nil, ErrFinalityAnchor
		}
		if _, err := file.Seek(int64(len(header)), io.SeekStart); err != nil {
			return nil, err
		}
		var previous types.Hash
		for offset := int64(len(header)); offset < stat.Size(); offset += finalityAnchorRecordSize {
			var raw [finalityAnchorRecordSize]byte
			if _, err := io.ReadFull(file, raw[:]); err != nil {
				return nil, ErrFinalityAnchor
			}
			height := binary.BigEndian.Uint64(raw[0:8])
			var hash, oldDigest, digest types.Hash
			copy(hash[:], raw[8:40])
			copy(oldDigest[:], raw[40:72])
			copy(digest[:], raw[72:104])
			if height == 0 || height <= store.height || types.IsZero32([32]byte(hash)) ||
				oldDigest != previous || digest != anchorRecordDigest(network, validatorRoot, height, hash, oldDigest) {
				return nil, ErrFinalityAnchor
			}
			store.height, store.hash, store.lastDigest, store.count = height, hash, digest, store.count+1
			previous = digest
		}
	}
	if _, err := file.Seek(0, io.SeekEnd); err != nil {
		return nil, err
	}
	success = true
	return store, nil
}

func anchorRecordDigest(network types.NetworkID, validatorRoot types.Hash, height uint64, hash, prev types.Hash) types.Hash {
	var w codec.Writer
	w.Fixed(network[:])
	w.Fixed(validatorRoot[:])
	w.U64(height)
	w.Fixed(hash[:])
	w.Fixed(prev[:])
	return types.Hash(codec.DomainHash("zephyr/monotonic-finalized-anchor/v2", w.BytesCopy()))
}

// Current returns the durable minimum finalized checkpoint. The zero height
// indicates a newly provisioned store with no finalized blocks yet.
func (s *FinalityAnchorStore) Current() (FinalityCheckpoint, error) {
	if s == nil {
		return FinalityCheckpoint{}, ErrFinalityAnchor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.poisoned {
		return FinalityCheckpoint{}, ErrFinalityAnchor
	}
	return FinalityCheckpoint{Network: s.network, ValidatorRoot: s.validatorRoot, Height: s.height, HeaderHash: s.hash}, nil
}

// advance is called ONLY with a header authenticated through the normal
// QC-backed finality path. Forbids equivocation at equal height and rollback
// to a lower height. Never truncate or rewrite an older durable record.
func (s *FinalityAnchorStore) advance(checkpoint FinalityCheckpoint) error {
	if s == nil {
		return ErrFinalityAnchor
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || s.poisoned || checkpoint.Validate(s.network, s.validatorRoot) != nil {
		return ErrFinalityAnchor
	}
	if checkpoint.Height < s.height || (checkpoint.Height == s.height && checkpoint.HeaderHash != s.hash) {
		return ErrFinalityAnchor
	}
	if checkpoint.Height == s.height {
		return nil
	}
	if s.count >= maxFinalityAnchorRecords {
		return ErrFinalityAnchor
	}
	var raw [finalityAnchorRecordSize]byte
	binary.BigEndian.PutUint64(raw[0:8], checkpoint.Height)
	copy(raw[8:40], checkpoint.HeaderHash[:])
	copy(raw[40:72], s.lastDigest[:])
	digest := anchorRecordDigest(s.network, s.validatorRoot, checkpoint.Height, checkpoint.HeaderHash, s.lastDigest)
	copy(raw[72:104], digest[:])
	n, err := s.file.Write(raw[:])
	if err != nil || n != len(raw) {
		s.poisoned = true
		return ErrFinalityAnchor
	}
	if err := s.file.Sync(); err != nil {
		s.poisoned = true
		return ErrFinalityAnchor
	}
	s.height, s.hash, s.lastDigest, s.count = checkpoint.Height, checkpoint.HeaderHash, digest, s.count+1
	return nil
}

func (s *FinalityAnchorStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	return s.file.Close()
}

// EnableFinalityAnchorStore attaches an externally placed, exclusive high-water
// mark to a runtime BEFORE replay or networking. It never reopens a closed
// store, and requires the journal and certified history to be configured.
func (r *Runtime) EnableFinalityAnchorStore(store *FinalityAnchorStore) error {
	if r == nil || store == nil {
		return ErrFinalityAnchor
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.anchorStore != nil || r.history == nil || r.globalCommitJournalPath == "" ||
		r.Height != 0 || r.ParentHash != (types.Hash{}) {
		return ErrFinalityAnchor
	}
	cp, err := store.Current()
	if err != nil || cp.Network != r.Network || cp.ValidatorRoot != r.ValidatorRoot {
		return ErrFinalityAnchor
	}
	r.anchorStore = store
	return nil
}

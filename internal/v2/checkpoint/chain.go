package checkpoint

import (
	"errors"
	"sync"

	v2consensus "github.com/zephyr-chain/zephyr-chain/internal/v2/consensus"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/sharding"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

var (
	ErrCheckpointConfig     = errors.New("invalid checkpoint chain configuration")
	ErrCheckpointSequence   = errors.New("invalid checkpoint sequence")
	ErrCheckpointValidators = errors.New("checkpoint validator set mismatch")
	ErrCheckpointCert       = errors.New("invalid checkpoint certificate")
)

type Entry struct {
	Header      sharding.GlobalHeader
	Certificate v2consensus.Certificate
	Validators  v2consensus.ValidatorSet
}

type Chain struct {
	mu          sync.RWMutex
	network     types.NetworkID
	currentRoot types.Hash
	lastHeight  uint64
	lastHash    types.Hash
	entries     map[uint64]Entry
	sets        map[types.Hash]v2consensus.ValidatorSet
}

func New(network types.NetworkID, genesis v2consensus.ValidatorSet) (*Chain, error) {
	if types.IsZero32([32]byte(network)) || genesis.Network != network {
		return nil, ErrCheckpointConfig
	}
	root, err := genesis.Root()
	if err != nil || types.IsZero32([32]byte(root)) {
		return nil, ErrCheckpointConfig
	}
	return &Chain{
		network:     network,
		currentRoot: root,
		entries:     make(map[uint64]Entry),
		sets:        map[types.Hash]v2consensus.ValidatorSet{root: cloneSet(genesis)},
	}, nil
}

func (c *Chain) Network() types.NetworkID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.network
}

func (c *Chain) CurrentRoot() types.Hash {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.currentRoot
}

func (c *Chain) Height() uint64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.lastHeight
}

func (c *Chain) Append(header sharding.GlobalHeader, certificate v2consensus.Certificate, current v2consensus.ValidatorSet, next *v2consensus.ValidatorSet) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if header.Network != c.network || certificate.Network != c.network || current.Network != c.network {
		return ErrCheckpointSequence
	}
	if header.Height != c.lastHeight+1 || (c.lastHeight > 0 && header.ParentHash != c.lastHash) {
		return ErrCheckpointSequence
	}
	currentRoot, err := current.Root()
	if err != nil || currentRoot != c.currentRoot || header.ValidatorRoot != currentRoot {
		return ErrCheckpointValidators
	}
	if certificate.Height != header.Height || certificate.HeaderHash != v2consensus.HeaderConsensusHash(header) || header.CertificateHash != certificate.Hash() {
		return ErrCheckpointCert
	}
	if err := current.VerifyCertificate(certificate); err != nil {
		return ErrCheckpointCert
	}

	nextRoot := header.EffectiveNextValidatorRoot()
	if nextRoot != currentRoot {
		if next == nil || next.Network != c.network {
			return ErrCheckpointValidators
		}
		computed, err := next.Root()
		if err != nil || computed != nextRoot {
			return ErrCheckpointValidators
		}
		c.sets[nextRoot] = cloneSet(*next)
	} else if next != nil {
		computed, err := next.Root()
		if err != nil || computed != nextRoot {
			return ErrCheckpointValidators
		}
		c.sets[nextRoot] = cloneSet(*next)
	}

	c.sets[currentRoot] = cloneSet(current)
	c.entries[header.Height] = Entry{Header: header, Certificate: certificate, Validators: cloneSet(current)}
	c.currentRoot = nextRoot
	c.lastHeight = header.Height
	c.lastHash = v2consensus.HeaderConsensusHash(header)
	return nil
}

func (c *Chain) Entry(height uint64) (Entry, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	entry, ok := c.entries[height]
	if !ok {
		return Entry{}, false
	}
	entry.Validators = cloneSet(entry.Validators)
	entry.Certificate.Votes = append([]v2consensus.Vote(nil), entry.Certificate.Votes...)
	return entry, true
}

func (c *Chain) ValidatorSet(root types.Hash) (v2consensus.ValidatorSet, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	set, ok := c.sets[root]
	if !ok {
		return v2consensus.ValidatorSet{}, false
	}
	return cloneSet(set), true
}

func (c *Chain) VerifyEntry(height uint64) error {
	c.mu.RLock()
	entry, ok := c.entries[height]
	c.mu.RUnlock()
	if !ok {
		return ErrCheckpointSequence
	}
	root, err := entry.Validators.Root()
	if err != nil || root != entry.Header.ValidatorRoot {
		return ErrCheckpointValidators
	}
	if entry.Certificate.HeaderHash != v2consensus.HeaderConsensusHash(entry.Header) || entry.Header.CertificateHash != entry.Certificate.Hash() {
		return ErrCheckpointCert
	}
	if err := entry.Validators.VerifyCertificate(entry.Certificate); err != nil {
		return ErrCheckpointCert
	}
	return nil
}

func cloneSet(in v2consensus.ValidatorSet) v2consensus.ValidatorSet {
	out := v2consensus.ValidatorSet{Network: in.Network, Validators: make([]v2consensus.Validator, len(in.Validators))}
	for i, validator := range in.Validators {
		out.Validators[i] = validator
		out.Validators[i].PublicKey = append([]byte(nil), validator.PublicKey...)
	}
	return out
}

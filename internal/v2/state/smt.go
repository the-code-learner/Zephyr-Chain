package state

import (
	"bytes"
	"errors"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/codec"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

const Depth = 256

var (
	ErrInvalidProof       = errors.New("invalid sparse merkle proof")
	ErrProofValueMismatch = errors.New("proof existence does not match supplied value")
)

type nodeKey struct {
	Depth  uint16
	Prefix [32]byte
}

type Proof struct {
	Exists   bool
	Bitmap   [32]byte
	Siblings []types.Hash
}

type Tree struct {
	mu       sync.RWMutex
	values   map[types.Hash][]byte
	nodes    map[nodeKey]types.Hash
	defaults [Depth + 1]types.Hash
}

func NewTree() *Tree {
	t := &Tree{
		values: make(map[types.Hash][]byte),
		nodes:  make(map[nodeKey]types.Hash),
	}
	t.defaults[Depth] = types.Hash(codec.DomainHash("zephyr/smt/empty-leaf/v2", nil))
	for d := Depth - 1; d >= 0; d-- {
		t.defaults[d] = branchHash(t.defaults[d+1], t.defaults[d+1])
	}
	return t
}

func (t *Tree) Root() types.Hash {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.getNode(0, [32]byte{})
}

func (t *Tree) Get(key types.Hash) ([]byte, bool) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	v, ok := t.values[key]
	if !ok {
		return nil, false
	}
	out := append([]byte(nil), v...)
	return out, true
}

func (t *Tree) Update(key types.Hash, value []byte) types.Hash {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.updateLocked(key, value)
	return t.getNode(0, [32]byte{})
}

func (t *Tree) Apply(updates map[types.Hash][]byte) types.Hash {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, value := range updates {
		t.updateLocked(key, value)
	}
	return t.getNode(0, [32]byte{})
}

func (t *Tree) updateLocked(key types.Hash, value []byte) {
	rawKey := [32]byte(key)
	leafPrefix := prefixAtDepth(rawKey, Depth)
	leafNode := nodeKey{Depth: Depth, Prefix: leafPrefix}
	if value == nil {
		delete(t.values, key)
		delete(t.nodes, leafNode)
	} else {
		copyValue := append([]byte(nil), value...)
		t.values[key] = copyValue
		h := leafHash(key, copyValue)
		t.nodes[leafNode] = h
	}

	for depth := Depth - 1; depth >= 0; depth-- {
		bit := bitAt(rawKey, depth)
		childPrefix := prefixAtDepth(rawKey, depth+1)
		siblingPrefix := childPrefix
		toggleBit(&siblingPrefix, depth)
		child := t.getNode(depth+1, childPrefix)
		sibling := t.getNode(depth+1, siblingPrefix)

		var left, right types.Hash
		if bit == 0 {
			left, right = child, sibling
		} else {
			left, right = sibling, child
		}
		parent := branchHash(left, right)
		parentKey := nodeKey{Depth: uint16(depth), Prefix: prefixAtDepth(rawKey, depth)}
		if parent == t.defaults[depth] {
			delete(t.nodes, parentKey)
		} else {
			t.nodes[parentKey] = parent
		}
	}
}

func (t *Tree) Prove(key types.Hash) Proof {
	t.mu.RLock()
	defer t.mu.RUnlock()

	rawKey := [32]byte(key)
	_, exists := t.values[key]
	proof := Proof{Exists: exists}
	for i := 0; i < Depth; i++ {
		depth := Depth - i
		siblingPrefix := prefixAtDepth(rawKey, depth)
		toggleBit(&siblingPrefix, depth-1)
		sibling := t.getNode(depth, siblingPrefix)
		if sibling != t.defaults[depth] {
			setBitmapBit(&proof.Bitmap, i)
			proof.Siblings = append(proof.Siblings, sibling)
		}
	}
	return proof
}

func (p Proof) MarshalBinary() []byte {
	var w codec.Writer
	w.Bool(p.Exists)
	w.Fixed(p.Bitmap[:])
	w.U16(uint16(len(p.Siblings)))
	for _, sibling := range p.Siblings {
		w.Fixed(sibling[:])
	}
	return w.BytesCopy()
}

func ParseProof(data []byte) (Proof, error) {
	r := codec.NewReader(data)
	exists, err := r.Bool()
	if err != nil {
		return Proof{}, ErrInvalidProof
	}
	bitmap, err := r.Fixed(32)
	if err != nil {
		return Proof{}, ErrInvalidProof
	}
	count, err := r.U16()
	if err != nil || count > Depth {
		return Proof{}, ErrInvalidProof
	}
	proof := Proof{Exists: exists, Siblings: make([]types.Hash, int(count))}
	copy(proof.Bitmap[:], bitmap)
	for i := range proof.Siblings {
		raw, err := r.Fixed(32)
		if err != nil {
			return Proof{}, ErrInvalidProof
		}
		copy(proof.Siblings[i][:], raw)
	}
	if err := r.Done(); err != nil {
		return Proof{}, ErrInvalidProof
	}
	bits := 0
	for i := 0; i < Depth; i++ {
		if bitmapBit(proof.Bitmap, i) {
			bits++
		}
	}
	if bits != len(proof.Siblings) {
		return Proof{}, ErrInvalidProof
	}
	return proof, nil
}

func Verify(root, key types.Hash, value []byte, proof Proof) bool {
	if proof.Exists != (value != nil) {
		return false
	}
	defaults := defaultHashes()
	rawKey := [32]byte(key)
	var current types.Hash
	if proof.Exists {
		current = leafHash(key, value)
	} else {
		current = defaults[Depth]
	}

	siblingIndex := 0
	for i := 0; i < Depth; i++ {
		depth := Depth - i
		sibling := defaults[depth]
		if bitmapBit(proof.Bitmap, i) {
			if siblingIndex >= len(proof.Siblings) {
				return false
			}
			sibling = proof.Siblings[siblingIndex]
			siblingIndex++
		}

		bitIndex := depth - 1
		if bitAt(rawKey, bitIndex) == 0 {
			current = branchHash(current, sibling)
		} else {
			current = branchHash(sibling, current)
		}
	}
	return siblingIndex == len(proof.Siblings) && current == root
}

func (t *Tree) getNode(depth int, prefix [32]byte) types.Hash {
	if h, ok := t.nodes[nodeKey{Depth: uint16(depth), Prefix: prefixAtDepth(prefix, depth)}]; ok {
		return h
	}
	return t.defaults[depth]
}

func defaultHashes() [Depth + 1]types.Hash {
	var defaults [Depth + 1]types.Hash
	defaults[Depth] = types.Hash(codec.DomainHash("zephyr/smt/empty-leaf/v2", nil))
	for d := Depth - 1; d >= 0; d-- {
		defaults[d] = branchHash(defaults[d+1], defaults[d+1])
	}
	return defaults
}

func leafHash(key types.Hash, value []byte) types.Hash {
	var w codec.Writer
	w.Fixed(key[:])
	w.Bytes(value)
	return types.Hash(codec.DomainHash("zephyr/smt/leaf/v2", w.BytesCopy()))
}

func branchHash(left, right types.Hash) types.Hash {
	var w codec.Writer
	w.Fixed(left[:])
	w.Fixed(right[:])
	return types.Hash(codec.DomainHash("zephyr/smt/branch/v2", w.BytesCopy()))
}

func prefixAtDepth(key [32]byte, depth int) [32]byte {
	if depth <= 0 {
		return [32]byte{}
	}
	if depth >= Depth {
		return key
	}
	out := key
	fullBytes := depth / 8
	remainingBits := depth % 8
	if remainingBits == 0 {
		for i := fullBytes; i < len(out); i++ {
			out[i] = 0
		}
		return out
	}
	mask := byte(0xFF << (8 - remainingBits))
	out[fullBytes] &= mask
	for i := fullBytes + 1; i < len(out); i++ {
		out[i] = 0
	}
	return out
}

func bitAt(key [32]byte, bitIndex int) uint8 {
	byteIndex := bitIndex / 8
	shift := 7 - (bitIndex % 8)
	return (key[byteIndex] >> shift) & 1
}

func toggleBit(key *[32]byte, bitIndex int) {
	byteIndex := bitIndex / 8
	shift := 7 - (bitIndex % 8)
	key[byteIndex] ^= 1 << shift
}

func setBitmapBit(bitmap *[32]byte, index int) {
	byteIndex := index / 8
	shift := uint(index % 8)
	bitmap[byteIndex] |= 1 << shift
}

func bitmapBit(bitmap [32]byte, index int) bool {
	byteIndex := index / 8
	shift := uint(index % 8)
	return bitmap[byteIndex]&(1<<shift) != 0
}

func EqualProof(a, b Proof) bool {
	if a.Exists != b.Exists || a.Bitmap != b.Bitmap || len(a.Siblings) != len(b.Siblings) {
		return false
	}
	for i := range a.Siblings {
		if !bytes.Equal(a.Siblings[i][:], b.Siblings[i][:]) {
			return false
		}
	}
	return true
}

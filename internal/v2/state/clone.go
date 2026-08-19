package state

// Clone returns an independent copy of the sparse Merkle tree. It is used by
// proposal simulation so validators can calculate a candidate state root
// without mutating committed state before a quorum certificate exists.
func (t *Tree) Clone() *Tree {
	t.mu.RLock()
	defer t.mu.RUnlock()
	clone := &Tree{
		values:   make(map[types.Hash][]byte, len(t.values)),
		nodes:    make(map[nodeKey]types.Hash, len(t.nodes)),
		defaults: t.defaults,
	}
	for key, value := range t.values {
		clone.values[key] = append([]byte(nil), value...)
	}
	for key, value := range t.nodes {
		clone.nodes[key] = value
	}
	return clone
}

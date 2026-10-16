package state

import (
	"bytes"
	"sort"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Preview returns the state root that would result from applying updates while
// leaving the committed tree untouched. It stores only nodes on affected paths
// in an overlay instead of cloning the full tree, which keeps proposal
// simulation proportional to the changed working set rather than total state.
func (t *Tree) Preview(updates map[types.Hash][]byte) types.Hash {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if len(updates) == 0 {
		return t.getNode(0, [32]byte{})
	}

	keys := make([]types.Hash, 0, len(updates))
	for key := range updates {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return bytes.Compare(keys[i][:], keys[j][:]) < 0 })

	overlay := make(map[nodeKey]types.Hash, len(keys)*Depth/2)
	lookup := func(depth int, prefix [32]byte) types.Hash {
		key := nodeKey{Depth: uint16(depth), Prefix: prefixAtDepth(prefix, depth)}
		if hash, ok := overlay[key]; ok {
			return hash
		}
		if hash, ok := t.nodes[key]; ok {
			return hash
		}
		return t.defaults[depth]
	}

	for _, key := range keys {
		rawKey := [32]byte(key)
		leafKey := nodeKey{Depth: Depth, Prefix: rawKey}
		value := updates[key]
		if value == nil {
			overlay[leafKey] = t.defaults[Depth]
		} else {
			overlay[leafKey] = leafHash(key, value)
		}

		for depth := Depth - 1; depth >= 0; depth-- {
			bit := bitAt(rawKey, depth)
			childPrefix := prefixAtDepth(rawKey, depth+1)
			siblingPrefix := childPrefix
			toggleBit(&siblingPrefix, depth)
			child := lookup(depth+1, childPrefix)
			sibling := lookup(depth+1, siblingPrefix)
			var left, right types.Hash
			if bit == 0 {
				left, right = child, sibling
			} else {
				left, right = sibling, child
			}
			parentKey := nodeKey{Depth: uint16(depth), Prefix: prefixAtDepth(rawKey, depth)}
			overlay[parentKey] = branchHash(left, right)
		}
	}

	rootKey := nodeKey{Depth: 0, Prefix: [32]byte{}}
	if root, ok := overlay[rootKey]; ok {
		return root
	}
	return t.getNode(0, [32]byte{})
}

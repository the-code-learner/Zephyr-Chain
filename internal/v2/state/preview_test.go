package state

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestPreviewMatchesApplyWithoutMutatingCommittedTree(t *testing.T) {
	tree := NewTree()
	keyA := types.HashBytes("key", []byte("a"))
	keyB := types.HashBytes("key", []byte("b"))
	keyC := types.HashBytes("key", []byte("c"))
	keyD := types.HashBytes("key", []byte("d"))
	tree.Apply(map[types.Hash][]byte{
		keyA: []byte("one"),
		keyB: []byte("two"),
		keyC: []byte("three"),
	})
	before := tree.Root()
	updates := map[types.Hash][]byte{
		keyA: []byte("ONE"),
		keyB: nil,
		keyD: []byte("four"),
	}

	preview := tree.Preview(updates)
	if tree.Root() != before {
		t.Fatal("preview mutated committed sparse Merkle root")
	}
	if value, ok := tree.Get(keyA); !ok || string(value) != "one" {
		t.Fatal("preview mutated committed values")
	}

	clone := tree.Clone()
	applied := clone.Apply(updates)
	if preview != applied {
		t.Fatalf("preview root %s does not match applied root %s", preview, applied)
	}
	if preview == before {
		t.Fatal("preview did not reflect updates")
	}
}

func TestPreviewEmptyUpdatesReturnsCurrentRoot(t *testing.T) {
	tree := NewTree()
	key := types.HashBytes("key", []byte("only"))
	tree.Update(key, []byte("value"))
	if got := tree.Preview(nil); got != tree.Root() {
		t.Fatalf("empty preview changed root: %s != %s", got, tree.Root())
	}
}

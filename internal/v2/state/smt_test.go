package state

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func key(label string) types.Hash { return types.HashBytes("test/key", []byte(label)) }

func TestSparseMerkleProofInclusionAndAbsence(t *testing.T) {
	tree := NewTree()
	k1 := key("a")
	k2 := key("b")
	tree.Update(k1, []byte("one"))
	tree.Update(k2, []byte("two"))
	root := tree.Root()

	p1 := tree.Prove(k1)
	if !Verify(root, k1, []byte("one"), p1) {
		t.Fatal("inclusion proof failed")
	}
	if Verify(root, k1, []byte("tampered"), p1) {
		t.Fatal("tampered value verified")
	}

	k3 := key("missing")
	p3 := tree.Prove(k3)
	if !Verify(root, k3, nil, p3) {
		t.Fatal("absence proof failed")
	}
}

func TestSparseMerkleRootIsOrderIndependent(t *testing.T) {
	a := NewTree()
	b := NewTree()
	k1, k2 := key("a"), key("b")
	a.Update(k1, []byte("one"))
	a.Update(k2, []byte("two"))
	b.Update(k2, []byte("two"))
	b.Update(k1, []byte("one"))
	if a.Root() != b.Root() {
		t.Fatal("root depends on update order")
	}
}

func TestDeleteRestoresRoot(t *testing.T) {
	tree := NewTree()
	initial := tree.Root()
	k := key("a")
	tree.Update(k, []byte("one"))
	tree.Update(k, nil)
	if tree.Root() != initial {
		t.Fatal("delete did not restore empty root")
	}
}

func BenchmarkSparseMerkleUpdate(b *testing.B) {
	tree := NewTree()
	keys := make([]types.Hash, 1024)
	for i := range keys {
		keys[i] = types.HashBytes("bench/key", []byte{byte(i), byte(i >> 8)})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree.Update(keys[i%len(keys)], []byte{1, 2, 3, 4, 5, 6, 7, 8})
	}
}

func BenchmarkSparseMerkleProofVerify(b *testing.B) {
	tree := NewTree()
	k := key("bench")
	tree.Update(k, []byte("value"))
	root := tree.Root()
	proof := tree.Prove(k)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !Verify(root, k, []byte("value"), proof) {
			b.Fatal("proof failed")
		}
	}
}

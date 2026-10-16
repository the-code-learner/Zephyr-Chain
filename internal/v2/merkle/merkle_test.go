package merkle

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestProof(t *testing.T) {
	leaves := []types.Hash{
		Leaf("test", []byte("a")),
		Leaf("test", []byte("b")),
		Leaf("test", []byte("c")),
	}
	root := Root(leaves)
	for i, leaf := range leaves {
		proof, err := BuildProof(leaves, i)
		if err != nil {
			t.Fatal(err)
		}
		if !Verify(root, leaf, proof) {
			t.Fatalf("proof %d failed", i)
		}
		bad := types.HashBytes("bad", []byte{byte(i)})
		if Verify(root, bad, proof) {
			t.Fatalf("bad leaf %d verified", i)
		}
	}
}

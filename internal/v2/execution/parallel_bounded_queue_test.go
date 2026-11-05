package execution

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestBufferedMultiworkerBatchPreservesDeterministicErrors(t *testing.T) {
	network := types.NetworkID(types.HashBytes("network", []byte("bounded-worker-queue")))
	root := types.HashBytes("root", []byte("bounded-worker-queue"))
	engine := Engine{Network: network, ShardCount: 1}
	batch := make([]tx.Transaction, 12)
	for i := range batch {
		batch[i] = tx.Transaction{StateRoot: root}
		batch[i].Salt[0] = byte(i + 1)
	}
	_, expected := engine.Execute(batch[0])
	if expected == nil {
		t.Fatal("malformed transaction unexpectedly executed")
	}
	for _, workers := range []int{2, 4, 16} {
		results, err := (BatchExecutor{Engine: engine, Workers: workers}).ExecuteBatch(batch)
		if err == nil || err.Error() != expected.Error() || results != nil {
			t.Fatalf("workers=%d: expected first transaction error %v; got results=%v err=%v", workers, expected, results, err)
		}
		duplicate := []tx.Transaction{batch[0], batch[0]}
		if _, err := (BatchExecutor{Engine: engine, Workers: workers}).ExecuteBatch(duplicate); err != ErrBatchConflict {
			t.Fatalf("workers=%d: duplicate batch accepted: %v", workers, err)
		}
	}
}

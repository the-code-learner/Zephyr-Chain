package execution

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestV2SingleWorkerBatchFastPathFailClosed(t *testing.T) {
	engine := Engine{Network: types.NetworkID(types.HashBytes("network", []byte("single-worker"))), ShardCount: 1}
	batch := BatchExecutor{Engine: engine, Workers: 1}
	if results, err := batch.ExecuteBatch(nil); err != nil || results != nil {
		t.Fatalf("empty batch changed behavior: results=%v error=%v", results, err)
	}
	invalid := tx.Transaction{StateRoot: types.HashBytes("root", []byte("single-worker"))}
	_, directErr := engine.Execute(invalid)
	if directErr == nil {
		t.Fatal("invalid transaction unexpectedly succeeds")
	}
	results, err := batch.ExecuteBatch([]tx.Transaction{invalid})
	if err == nil || err.Error() != directErr.Error() || results != nil {
		t.Fatalf("single-worker execution diverged from engine: result=%v error=%v direct=%v", results, err, directErr)
	}
	if _, err := batch.ExecuteBatch([]tx.Transaction{invalid, invalid}); err != ErrBatchConflict {
		t.Fatalf("single-worker path bypassed duplicate transaction protection: %v", err)
	}
}

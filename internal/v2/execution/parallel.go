package execution

import (
	"errors"
	"runtime"
	"sync"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/tx"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/worldstate"
)

var (
	ErrBatchConflict  = errors.New("v2 batch contains conflicting transactions")
	ErrBatchStateRoot = errors.New("v2 batch does not target the current state root")
)

// BatchExecutor executes proof-carrying transactions concurrently only when
// their consumed object sets are disjoint. All transactions in one batch are
// anchored to the same pre-state root, so execution can be parallel and the
// resulting object delta can be committed atomically in deterministic order.
type BatchExecutor struct {
	Engine  Engine
	Workers int
}

func (b BatchExecutor) ExecuteBatch(transactions []tx.Transaction) ([]Result, error) {
	if len(transactions) == 0 {
		return nil, nil
	}
	if err := validateIndependentBatch(transactions); err != nil {
		return nil, err
	}
	workers := b.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(transactions) {
		workers = len(transactions)
	}
	if workers < 1 {
		workers = 1
	}

	results := make([]Result, len(transactions))
	errs := make([]error, len(transactions))
	jobs := make(chan int)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			for index := range jobs {
				results[index], errs[index] = b.Engine.Execute(transactions[index])
			}
		}()
	}
	for i := range transactions {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	for i := range errs {
		if errs[i] != nil {
			return nil, errs[i]
		}
	}
	return results, nil
}

func (b BatchExecutor) ApplyBatch(store worldstate.Backend, transactions []tx.Transaction) (types.Hash, []Result, error) {
	if len(transactions) == 0 {
		return store.Root(), nil, nil
	}
	if store.Root() != transactions[0].StateRoot {
		return store.Root(), nil, ErrBatchStateRoot
	}
	results, err := b.ExecuteBatch(transactions)
	if err != nil {
		return store.Root(), nil, err
	}
	consumed := make([]types.ObjectID, 0)
	created := make([]object.Object, 0)
	for _, result := range results {
		consumed = append(consumed, result.Consumed...)
		created = append(created, result.Created...)
	}
	root, err := store.Apply(consumed, created)
	if err != nil {
		return store.Root(), nil, err
	}
	return root, results, nil
}

func validateIndependentBatch(transactions []tx.Transaction) error {
	root := transactions[0].StateRoot
	seenInputs := make(map[types.ObjectID]struct{})
	seenTransactions := make(map[types.Hash]struct{})
	for _, transaction := range transactions {
		if transaction.StateRoot != root {
			return ErrBatchStateRoot
		}
		id := transaction.ID()
		if _, duplicate := seenTransactions[id]; duplicate {
			return ErrBatchConflict
		}
		seenTransactions[id] = struct{}{}
		for _, input := range transaction.Inputs {
			if _, conflict := seenInputs[input.ObjectID]; conflict {
				return ErrBatchConflict
			}
			seenInputs[input.ObjectID] = struct{}{}
		}
	}
	return nil
}

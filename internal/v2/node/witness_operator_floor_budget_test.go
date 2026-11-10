//go:build linux || darwin

package node

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// A context-aware witness fixture that stalls until the shared deadline,
// independent of real DNS/TLS or flaky timer-based peer scheduling.
type witnessBudgetVerifier struct {
	checkpoint FinalityCheckpoint
	stall      bool
	calls      atomic.Int32
}

func (s *witnessBudgetVerifier) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return s.AuthenticatedWitnessFloorContext(context.Background())
}

func (s *witnessBudgetVerifier) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	s.calls.Add(1)
	if s.stall {
		<-ctx.Done()
		return FinalityCheckpoint{}, ctx.Err()
	}
	return s.checkpoint, nil
}

func TestProvisionedMultiOperatorHTTPSWitnessFloorSharesOneReadDeadline(t *testing.T) {
	keys, _ := witnessTestKeys()
	network := types.NetworkID(types.HashBytes("witness-shared-budget", []byte("network")))
	root := types.HashBytes("witness-shared-budget", []byte("root"))
	floor, err := NewProvisionedMultiOperatorHTTPSWitnessFloor(
		witnessOperatorFloorTestProvisioning(), keys, network, root,
	)
	if err != nil {
		t.Fatal(err)
	}
	if floor.readTimeout != 5*time.Second {
		t.Fatalf("provisioned HTTPS aggregate deadline = %v, want 5s", floor.readTimeout)
	}
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 22, HeaderHash: types.Hash{7}}
	first := &witnessBudgetVerifier{checkpoint: cp}
	stalled := &witnessBudgetVerifier{checkpoint: cp, stall: true}
	third := &witnessBudgetVerifier{checkpoint: cp}
	floor.sources = []WitnessExternalFloorVerifier{first, stalled, third}
	// Shorten the configured budget only inside this deterministic fixture;
	// production always provisions the full 5s aggregate cap.
	floor.readTimeout = 20 * time.Millisecond
	start := time.Now()
	if _, err := floor.AuthenticatedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted expired second origin: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("aggregate timeout failed to bound stalled origin: %v", elapsed)
	}
	if first.calls.Load() != 1 || stalled.calls.Load() != 1 || third.calls.Load() != 0 {
		t.Fatalf("expired floor continued to later origins: first=%d blocked=%d third=%d",
			first.calls.Load(), stalled.calls.Load(), third.calls.Load())
	}
	if floor.observed {
		t.Fatal("expired multi-operator floor updated its high-water observation")
	}

	// An already stricter caller deadline must take precedence over the
	// provisioned aggregate cap; no later origin may be queried.
	first.calls.Store(0)
	stalled.calls.Store(0)
	third.calls.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := floor.AuthenticatedWitnessFloorContext(ctx); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("caller deadline was not enforced: %v", err)
	}
	if third.calls.Load() != 0 || floor.observed {
		t.Fatal("shorter caller deadline permitted later origin or high-water update")
	}
}

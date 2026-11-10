//go:build linux || darwin

package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Legacy 2+ operator provisioning must not turn N per-origin HTTP timeouts
// into a prolonged recovery/signing authorization attempt. The strict
// attested 3+ operator entry point remains the production recommendation.
func TestOperatorSeparatedWitnessFloorSharesOneReadDeadline(t *testing.T) {
	network := types.NetworkID(types.HashBytes("legacy-operator-budget", []byte("network")))
	root := types.HashBytes("legacy-operator-budget", []byte("root"))
	keys, _ := witnessTestKeys()
	policies := []WitnessPublisherPolicy{
		{Endpoint: "https://witness-a.example.org/v1/checkpoint", Keys: keys},
		{Endpoint: "https://witness-b.example.org/v1/checkpoint", Keys: keys},
		{Endpoint: "https://witness-c.example.org/v1/checkpoint", Keys: keys},
	}
	floor, err := NewOperatorSeparatedWitnessFloor(policies, network, root)
	if err != nil {
		t.Fatalf("valid legacy publisher inventory rejected: %v", err)
	}
	if floor.readTimeout != 5*time.Second {
		t.Fatalf("operator HTTPS floor total budget = %v, want five seconds", floor.readTimeout)
	}
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 29, HeaderHash: types.Hash{7}}
	first := &witnessBudgetVerifier{checkpoint: cp}
	stalled := &witnessBudgetVerifier{checkpoint: cp, stall: true}
	third := &witnessBudgetVerifier{checkpoint: cp}
	floor.sources = []WitnessExternalFloorVerifier{first, stalled, third}

	// A tiny test-only budget removes dependence on wall-clock 5s I/O.
	floor.readTimeout = 20 * time.Millisecond
	start := time.Now()
	if _, err := floor.AuthenticatedWitnessFloorContext(context.Background()); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted partial agreement after aggregate expiry: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("legacy multi-origin read exceeded test deadline: %v", elapsed)
	}
	if first.calls.Load() != 1 || stalled.calls.Load() != 1 || third.calls.Load() != 0 {
		t.Fatalf("expired read queried unexpected sources: first=%d stalled=%d third=%d",
			first.calls.Load(), stalled.calls.Load(), third.calls.Load())
	}
	if floor.observed {
		t.Fatal("expired legacy HTTPS floor persisted an unverified high-water mark")
	}

	// A shorter caller deadline must take priority over the overall cap.
	floor.readTimeout = time.Second
	first.calls.Store(0)
	stalled.calls.Store(0)
	third.calls.Store(0)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := floor.AuthenticatedWitnessFloorContext(ctx); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted read after caller cancellation: %v", err)
	}
	if third.calls.Load() != 0 || floor.observed {
		t.Fatal("caller deadline allowed a later origin or accepted a partial floor")
	}
}

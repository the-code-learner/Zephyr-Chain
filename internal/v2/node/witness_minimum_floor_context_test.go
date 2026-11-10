//go:build linux || darwin

package node

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

type minimumContextFloorFixture struct {
	calls int
	read  func(context.Context) (FinalityCheckpoint, error)
}

func (f *minimumContextFloorFixture) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.AuthenticatedWitnessFloorContext(context.Background())
}

func (f *minimumContextFloorFixture) AuthenticatedWitnessFloorContext(ctx context.Context) (FinalityCheckpoint, error) {
	f.calls++
	return f.read(ctx)
}

type minimumLegacyFloorFixture struct {
	read func() (FinalityCheckpoint, error)
}

func (f minimumLegacyFloorFixture) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	return f.read()
}

func TestCertifiedWitnessFloorMinimumPropagatesCancellation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("minimum-context", []byte("network")))
	root := types.HashBytes("minimum-context", []byte("root"))
	minimum := FinalityCheckpoint{
		Network: network, ValidatorRoot: root, Height: 17, HeaderHash: types.Hash{9},
	}
	remote := &minimumContextFloorFixture{read: func(ctx context.Context) (FinalityCheckpoint, error) {
		<-ctx.Done()
		return FinalityCheckpoint{}, ctx.Err()
	}}
	floor, err := NewCertifiedWitnessFloorMinimum(remote, minimum)
	if err != nil {
		t.Fatal(err)
	}

	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := floor.AuthenticatedWitnessFloorContext(cancelled); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("accepted previously canceled recovery request: %v", err)
	}
	if remote.calls != 0 {
		t.Fatalf("canceled request performed %d remote witness reads", remote.calls)
	}

	ctx, stop := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stop()
	start := time.Now()
	if _, err := floor.AuthenticatedWitnessFloorContext(ctx); !errors.Is(err, ErrWitnessSigningIntent) ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("remote deadline was lost through minimum adapter: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("minimum floor ignored caller deadline while waiting on remote witness")
	}
	if remote.calls != 1 {
		t.Fatalf("context-aware remote called %d times, expected one", remote.calls)
	}
	if _, err := floor.AuthenticatedWitnessFloorContext(nil); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("nil context was accepted: %v", err)
	}
}

func TestCertifiedWitnessFloorMinimumRejectsLateLegacyResponse(t *testing.T) {
	network := types.NetworkID(types.HashBytes("minimum-legacy", []byte("network")))
	root := types.HashBytes("minimum-legacy", []byte("root"))
	minimum := FinalityCheckpoint{
		Network: network, ValidatorRoot: root, Height: 18, HeaderHash: types.Hash{5},
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	legacy := minimumLegacyFloorFixture{read: func() (FinalityCheckpoint, error) {
		// Synchronous adapters cannot be interrupted in flight, but their
		// successful answer must not override an expired caller request.
		cancel()
		return minimum, nil
	}}
	floor, err := NewCertifiedWitnessFloorMinimum(legacy, minimum)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := floor.AuthenticatedWitnessFloorContext(ctx); !errors.Is(err, ErrWitnessSigningIntent) ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("accepted late response from legacy witness source: %v", err)
	}

	validRemote := &minimumContextFloorFixture{read: func(ctx context.Context) (FinalityCheckpoint, error) {
		return minimum, nil
	}}
	floor, err = NewCertifiedWitnessFloorMinimum(validRemote, minimum)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := floor.AuthenticatedWitnessFloorContext(context.Background()); err != nil || got != minimum {
		t.Fatalf("valid context-aware floor incorrectly rejected: got=%+v err=%v", got, err)
	}
}

package node

import (
	"errors"
	"sync"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

// Mutable fixtures model independently authenticated publisher adapters.
// They do not attest to real operator independence, network freshness or HSMs.
type changingWitnessFloorSource struct {
	mu         sync.Mutex
	checkpoint FinalityCheckpoint
}

func (s *changingWitnessFloorSource) AuthenticatedWitnessFloor() (FinalityCheckpoint, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.checkpoint, nil
}

func (s *changingWitnessFloorSource) set(cp FinalityCheckpoint) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checkpoint = cp
}

func TestMultiSourceWitnessFloorRejectsAgreedReplayAfterHigherObservation(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-observed", []byte("network")))
	root := types.HashBytes("witness-observed", []byte("root"))
	first := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 30, HeaderHash: types.Hash{1}}
	next := first
	next.Height = 31
	next.HeaderHash = types.Hash{2}
	a := &changingWitnessFloorSource{checkpoint: first}
	b := &changingWitnessFloorSource{checkpoint: first}
	floor, err := NewMultiSourceWitnessFloor(a, b)
	if err != nil {
		t.Fatal(err)
	}
	check := func(want FinalityCheckpoint) {
		t.Helper()
		got, err := floor.AuthenticatedWitnessFloor()
		if err != nil || got != want {
			t.Fatalf("got %+v, want %+v: %v", got, want, err)
		}
	}
	check(first)
	a.set(next)
	b.set(next)
	check(next)
	// A CDN rollback on both otherwise agreeing endpoints is a replay.
	a.set(first)
	b.set(first)
	if _, err := floor.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("previously seen lower height was accepted: %v", err)
	}
	a.set(next)
	b.set(first)
	if _, err := floor.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("split-brain publication was accepted: %v", err)
	}
	// A same-height conflicting signed hash cannot override the remembered
	// finalized checkpoint even when both publishers agree on it.
	conflict := next
	conflict.HeaderHash = types.Hash{3}
	a.set(conflict)
	b.set(conflict)
	if _, err := floor.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessEquivocation) {
		t.Fatalf("same-height conflicting hash was accepted: %v", err)
	}
	foreign := next
	foreign.Network = types.NetworkID(types.HashBytes("witness-observed", []byte("foreign")))
	a.set(foreign)
	b.set(foreign)
	if _, err := floor.AuthenticatedWitnessFloor(); !errors.Is(err, ErrWitnessSigningIntent) {
		t.Fatalf("foreign network was accepted after observation: %v", err)
	}
	a.set(next)
	b.set(next)
	check(next)
	advanced := next
	advanced.Height = 32
	advanced.HeaderHash = types.Hash{4}
	a.set(advanced)
	b.set(advanced)
	check(advanced)
}

func TestMultiSourceWitnessFloorConcurrentObservedReads(t *testing.T) {
	network := types.NetworkID(types.HashBytes("witness-observed", []byte("concurrent")))
	root := types.HashBytes("witness-observed", []byte("root"))
	cp := FinalityCheckpoint{Network: network, ValidatorRoot: root, Height: 3, HeaderHash: types.Hash{5}}
	a := &changingWitnessFloorSource{checkpoint: cp}
	b := &changingWitnessFloorSource{checkpoint: cp}
	floor, err := NewMultiSourceWitnessFloor(a, b)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < cap(errs); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := floor.AuthenticatedWitnessFloor()
			if err == nil && got != cp {
				err = ErrWitnessSigningIntent
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent witness read failed: %v", err)
		}
	}
}

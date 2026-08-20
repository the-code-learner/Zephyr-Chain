package economics

import (
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/v2/object"
	"github.com/zephyr-chain/zephyr-chain/internal/v2/types"
)

func TestAgeWeightedVelocityRewardsOlderCirculation(t *testing.T) {
	policy := VelocityPolicy{MinAgeBlocks: 10, FullWeightAgeBlocks: 100, MaxVelocityBps: 20_000}
	accumulator, err := NewVelocityAccumulator(policy)
	if err != nil {
		t.Fatal(err)
	}
	var token types.TokenID
	token[0] = 1
	if err := accumulator.ObserveCoin(object.Coin{Token: token, Amount: 500, CreatedHeight: 100}, 200); err != nil {
		t.Fatal(err)
	}
	snapshot, err := accumulator.Finalize(1_000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgeWeightedVelocityBps != 5_000 || snapshot.EligibleSpends != 1 {
		t.Fatalf("unexpected old-coin velocity: %#v", snapshot)
	}
}

func TestAgeWeightedVelocitySuppressesRapidSelfCycling(t *testing.T) {
	policy := VelocityPolicy{MinAgeBlocks: 10, FullWeightAgeBlocks: 100, MaxVelocityBps: 20_000}
	accumulator, err := NewVelocityAccumulator(policy)
	if err != nil {
		t.Fatal(err)
	}
	var token types.TokenID
	token[0] = 1
	for height := uint64(101); height <= 109; height++ {
		coin := object.Coin{Token: token, Amount: 1_000, CreatedHeight: height - 1}
		if err := accumulator.ObserveCoin(coin, height); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := accumulator.Finalize(1_000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgeWeightedVelocityBps != 0 || snapshot.FreshSpends != 9 {
		t.Fatalf("rapid cycling should have zero contribution under minimum age: %#v", snapshot)
	}
}

func TestAgeWeightedVelocityExcludesUnknownAgeAndRejectsImpossibleAge(t *testing.T) {
	policy := VelocityPolicy{MinAgeBlocks: 1, FullWeightAgeBlocks: 100, MaxVelocityBps: 20_000}
	accumulator, err := NewVelocityAccumulator(policy)
	if err != nil {
		t.Fatal(err)
	}
	var token types.TokenID
	token[0] = 1
	if err := accumulator.ObserveCoin(object.Coin{Token: token, Amount: 100}, 10); err != nil {
		t.Fatal(err)
	}
	if err := accumulator.ObserveCoin(object.Coin{Token: token, Amount: 100, CreatedHeight: 10}, 10); err != ErrVelocity {
		t.Fatalf("expected impossible same-height spend rejection, got %v", err)
	}
	snapshot, err := accumulator.Finalize(1_000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.UnknownAgeSpends != 1 {
		t.Fatalf("unknown-age spend should be tracked but excluded: %#v", snapshot)
	}
}

func TestAgeWeightedVelocityIsBounded(t *testing.T) {
	policy := VelocityPolicy{MinAgeBlocks: 1, FullWeightAgeBlocks: 1, MaxVelocityBps: 12_000}
	accumulator, err := NewVelocityAccumulator(policy)
	if err != nil {
		t.Fatal(err)
	}
	var token types.TokenID
	token[0] = 1
	for i := 0; i < 10; i++ {
		if err := accumulator.ObserveCoin(object.Coin{Token: token, Amount: 1_000, CreatedHeight: 1}, 2); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := accumulator.Finalize(1_000)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.AgeWeightedVelocityBps != 12_000 {
		t.Fatalf("velocity clamp failed: %#v", snapshot)
	}
}

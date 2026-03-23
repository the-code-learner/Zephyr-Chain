package dpos

import "testing"

func TestElectValidatorsRanksByVotingPower(t *testing.T) {
	service, err := NewService(ElectionConfig{
		MaxValidators:   2,
		MinSelfStake:    100,
		MaxMissedBlocks: 5,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	validators, err := service.ElectValidators(
		[]Candidate{
			{Address: "alice", SelfStake: 200, CommissionRate: 0.10},
			{Address: "bob", SelfStake: 300, CommissionRate: 0.12},
			{Address: "carol", SelfStake: 150, CommissionRate: 0.08},
		},
		[]Vote{
			{Delegator: "d1", Candidate: "alice", Amount: 500},
			{Delegator: "d2", Candidate: "bob", Amount: 100},
		},
	)
	if err != nil {
		t.Fatalf("unexpected error electing validators: %v", err)
	}

	if len(validators) != 2 {
		t.Fatalf("expected 2 validators, got %d", len(validators))
	}

	if validators[0].Address != "alice" {
		t.Fatalf("expected alice to rank first, got %s", validators[0].Address)
	}

	if validators[1].Address != "bob" {
		t.Fatalf("expected bob to rank second, got %s", validators[1].Address)
	}
}

func TestElectValidatorsFiltersIneligibleCandidates(t *testing.T) {
	service, err := NewService(ElectionConfig{
		MaxValidators:   5,
		MinSelfStake:    100,
		MaxMissedBlocks: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	validators, err := service.ElectValidators(
		[]Candidate{
			{Address: "alice", SelfStake: 90},
			{Address: "bob", SelfStake: 150, MissedBlocks: 12},
			{Address: "carol", SelfStake: 180, MissedBlocks: 3},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error electing validators: %v", err)
	}

	if len(validators) != 1 {
		t.Fatalf("expected 1 validator, got %d", len(validators))
	}

	if validators[0].Address != "carol" {
		t.Fatalf("expected carol to remain eligible, got %s", validators[0].Address)
	}
}

func TestElectValidatorsUsesDeterministicTieBreakers(t *testing.T) {
	service, err := NewService(ElectionConfig{
		MaxValidators:   3,
		MinSelfStake:    100,
		MaxMissedBlocks: 10,
	})
	if err != nil {
		t.Fatalf("unexpected error creating service: %v", err)
	}

	validators, err := service.ElectValidators(
		[]Candidate{
			{Address: "carol", SelfStake: 300, CommissionRate: 0.10},
			{Address: "alice", SelfStake: 300, CommissionRate: 0.08},
			{Address: "bob", SelfStake: 300, CommissionRate: 0.08},
		},
		nil,
	)
	if err != nil {
		t.Fatalf("unexpected error electing validators: %v", err)
	}

	if validators[0].Address != "alice" {
		t.Fatalf("expected alice first by address tie-breaker, got %s", validators[0].Address)
	}

	if validators[1].Address != "bob" {
		t.Fatalf("expected bob second by address tie-breaker, got %s", validators[1].Address)
	}

	if validators[2].Address != "carol" {
		t.Fatalf("expected carol third, got %s", validators[2].Address)
	}
}


package ledger

import (
	"math"
	"testing"

	"github.com/zephyr-chain/zephyr-chain/internal/dpos"
)

func TestAddUint64RejectsOverflow(t *testing.T) {
	if _, ok := addUint64(math.MaxUint64, 1); ok {
		t.Fatal("expected uint64 overflow to be rejected")
	}
	if value, ok := addUint64(math.MaxUint64-1, 1); !ok || value != math.MaxUint64 {
		t.Fatalf("expected exact max uint64 addition, got value=%d ok=%t", value, ok)
	}
}

func TestNextUint64RejectsExhaustedNonce(t *testing.T) {
	if _, ok := nextUint64(math.MaxUint64); ok {
		t.Fatal("expected max uint64 nonce to be exhausted")
	}
	if value, ok := nextUint64(math.MaxUint64 - 1); !ok || value != math.MaxUint64 {
		t.Fatalf("expected next nonce max uint64, got value=%d ok=%t", value, ok)
	}
}

func TestSumValidatorVotingPowerRejectsOverflow(t *testing.T) {
	validators := []dpos.Validator{
		{Address: "a", VotingPower: math.MaxUint64},
		{Address: "b", VotingPower: 1},
	}
	if _, ok := sumValidatorVotingPower(validators); ok {
		t.Fatal("expected validator voting power overflow to be rejected")
	}
}

func TestQuorumVotingPowerDoesNotOverflowAtMaxUint64(t *testing.T) {
	const expected uint64 = 12297829382473034411
	if quorum := quorumVotingPower(math.MaxUint64); quorum != expected {
		t.Fatalf("expected quorum %d, got %d", expected, quorum)
	}
}

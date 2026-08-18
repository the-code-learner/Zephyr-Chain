package ledger

import "math"

func addUint64(left uint64, right uint64) (uint64, bool) {
	if right > math.MaxUint64-left {
		return 0, false
	}
	return left + right, true
}

func nextUint64(value uint64) (uint64, bool) {
	if value == math.MaxUint64 {
		return 0, false
	}
	return value + 1, true
}

func sumValidatorVotingPower(validators []validatorVotingPowerView) (uint64, bool) {
	var total uint64
	for _, validator := range validators {
		next, ok := addUint64(total, validator.votingPower())
		if !ok {
			return 0, false
		}
		total = next
	}
	return total, true
}

type validatorVotingPowerView interface {
	votingPower() uint64
}

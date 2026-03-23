package dpos

type Candidate struct {
	Address        string  `json:"address"`
	SelfStake      uint64  `json:"selfStake"`
	CommissionRate float64 `json:"commissionRate"`
	MissedBlocks   uint64  `json:"missedBlocks"`
}

type Vote struct {
	Delegator string `json:"delegator"`
	Candidate string `json:"candidate"`
	Amount    uint64 `json:"amount"`
}

type Validator struct {
	Rank            int     `json:"rank"`
	Address         string  `json:"address"`
	VotingPower     uint64  `json:"votingPower"`
	SelfStake       uint64  `json:"selfStake"`
	DelegatedStake  uint64  `json:"delegatedStake"`
	CommissionRate  float64 `json:"commissionRate"`
	EligibilityNote string  `json:"eligibilityNote,omitempty"`
}

type ElectionConfig struct {
	MaxValidators  int    `json:"maxValidators"`
	MinSelfStake   uint64 `json:"minSelfStake"`
	MaxMissedBlocks uint64 `json:"maxMissedBlocks"`
}

func (c ElectionConfig) withDefaults() ElectionConfig {
	if c.MaxValidators <= 0 {
		c.MaxValidators = 21
	}

	if c.MinSelfStake == 0 {
		c.MinSelfStake = 10_000
	}

	if c.MaxMissedBlocks == 0 {
		c.MaxMissedBlocks = 50
	}

	return c
}


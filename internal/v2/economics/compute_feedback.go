package economics

import "errors"

var ErrComputeFeedback = errors.New("invalid Zephyr compute feedback policy")

type ComputeFeedbackMode uint8

const (
	ComputeFeedbackObserveOnly ComputeFeedbackMode = iota
	ComputeFeedbackRewardRouting
	ComputeFeedbackMonetaryBand
)

type ComputeFeedbackPolicy struct {
	Mode                       ComputeFeedbackMode
	BaseComputeRewardShareBps  uint32
	MinComputeRewardShareBps   uint32
	MaxComputeRewardShareBps   uint32
	RewardSensitivityBps       uint32
	MonetarySensitivityBps     uint32
	MaxInflationCorrectionBps  uint32
}

type ComputeFeedbackDecision struct {
	Shadow                        bool
	Mode                          ComputeFeedbackMode
	ScarcityScoreBps              int32
	ScarcityReliable              bool
	ComputeRewardShareBps         uint32
	SuggestedComputeIncentiveMint uint64
	InflationCorrectionBps        int32
	BaseTargetInflationBps        uint32
	SuggestedTargetInflationBps   uint32
	SuggestedNetIssuance          uint64
	SuggestedGrossMint            uint64
}

func DefaultComputeFeedbackPolicy(mode ComputeFeedbackMode) ComputeFeedbackPolicy {
	return ComputeFeedbackPolicy{
		Mode:                      mode,
		BaseComputeRewardShareBps: 1_000,
		MinComputeRewardShareBps:  0,
		MaxComputeRewardShareBps:  3_000,
		RewardSensitivityBps:      2_000,
		MonetarySensitivityBps:    25,
		MaxInflationCorrectionBps: 25,
	}
}

// EvaluateComputeFeedback simulates how ZCSI could affect incentive routing and,
// only in mode C, a narrow monetary band. It never mutates supply and all output
// remains shadow until governance/protocol activation gates are satisfied.
func EvaluateComputeFeedback(base MonetaryDecision, metrics MonetaryMetrics, monetary MonetaryPolicy, scarcity ComputeScarcitySnapshot, policy ComputeFeedbackPolicy) (ComputeFeedbackDecision, error) {
	if !base.Shadow || policy.Mode > ComputeFeedbackMonetaryBand || policy.BaseComputeRewardShareBps > BasisPoints ||
		policy.MinComputeRewardShareBps > policy.BaseComputeRewardShareBps || policy.BaseComputeRewardShareBps > policy.MaxComputeRewardShareBps ||
		policy.MaxComputeRewardShareBps > BasisPoints || policy.RewardSensitivityBps > BasisPoints ||
		policy.MonetarySensitivityBps > BasisPoints || policy.MaxInflationCorrectionBps > BasisPoints {
		return ComputeFeedbackDecision{}, ErrComputeFeedback
	}

	decision := ComputeFeedbackDecision{
		Shadow:                      true,
		Mode:                        policy.Mode,
		ScarcityScoreBps:            scarcity.ScoreBps,
		ScarcityReliable:            scarcity.Reliable,
		ComputeRewardShareBps:       policy.BaseComputeRewardShareBps,
		BaseTargetInflationBps:      base.TargetInflationBps,
		SuggestedTargetInflationBps: base.TargetInflationBps,
		SuggestedNetIssuance:        base.NetIssuanceTarget,
		SuggestedGrossMint:          base.GrossMintTarget,
	}
	if !scarcity.Reliable || policy.Mode == ComputeFeedbackObserveOnly {
		return decisionWithComputeBudget(decision), nil
	}

	rewardDelta := int64(scarcity.ScoreBps) * int64(policy.RewardSensitivityBps) / int64(BasisPoints)
	rewardShare := int64(policy.BaseComputeRewardShareBps) + rewardDelta
	if rewardShare < int64(policy.MinComputeRewardShareBps) {
		rewardShare = int64(policy.MinComputeRewardShareBps)
	}
	if rewardShare > int64(policy.MaxComputeRewardShareBps) {
		rewardShare = int64(policy.MaxComputeRewardShareBps)
	}
	decision.ComputeRewardShareBps = uint32(rewardShare)

	if policy.Mode != ComputeFeedbackMonetaryBand {
		return decisionWithComputeBudget(decision), nil
	}
	correction := int64(scarcity.ScoreBps) * int64(policy.MonetarySensitivityBps) / int64(BasisPoints)
	maxCorrection := int64(policy.MaxInflationCorrectionBps)
	if correction > maxCorrection {
		correction = maxCorrection
	}
	if correction < -maxCorrection {
		correction = -maxCorrection
	}
	decision.InflationCorrectionBps = int32(correction)
	decision.SuggestedTargetInflationBps = clampTarget(int64(base.TargetInflationBps)+correction, monetary.MinInflationBps, monetary.MaxInflationBps)
	net, err := epochIssuance(metrics.Supply, decision.SuggestedTargetInflationBps, monetary.EpochsPerYear)
	if err != nil {
		return ComputeFeedbackDecision{}, err
	}
	gross, err := addUint64(net, metrics.BurnedThisEpoch)
	if err != nil {
		return ComputeFeedbackDecision{}, err
	}
	decision.SuggestedNetIssuance = net
	decision.SuggestedGrossMint = gross
	return decisionWithComputeBudget(decision), nil
}

func decisionWithComputeBudget(decision ComputeFeedbackDecision) ComputeFeedbackDecision {
	budget, err := shareBps(decision.SuggestedNetIssuance, decision.ComputeRewardShareBps)
	if err == nil {
		decision.SuggestedComputeIncentiveMint = budget
	}
	return decision
}

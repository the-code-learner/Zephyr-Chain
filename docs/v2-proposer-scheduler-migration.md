# V2 proposer scheduling: consensus-safe migration proposal (issue #229)

Status: **DESIGN / NOT ENABLED / NO MAINNET GO**. The current selection rule in `ValidatorSet.Proposer` remains normative for existing V2 history. This note is not a vote to activate a different scheduler.

## Current rule and risks

For sorted validator identifiers and total power `T`, the code selects the owner of the cumulative-power ticket `((height-1)%T + round%T)%T`. A lab with every validator power=100 gives 100 consecutive round-zero heights to the first validator. The old two-height signed-QC lab proves two finalizations, **not** proposer rotation. This behavior is pinned by `TestV2LegacyProposerHundredHeightStreakIsConsensusCompatibility`.

**Additional arithmetic review:** the addition of two separately reduced `uint64` terms can wrap when `T` exceeds roughly half the `uint64` range. The resultant slot may differ from mathematical modular addition; all present implementations still agree, but the exact expression is part of existing consensus. Do not silently alter it on the active V2 network. Specify overflow-safe addition for a *new, explicitly versioned* scheduler.

## Proposed V3 schedule (candidate; not final)

Use a versioned, genesis-committed epoch scheduler with **256 proposal heights per epoch**, giving every validator an integer slot quota, rather than using raw stake units as consecutive block windows. Epoch arithmetic uses checked unsigned height/round handling and never allocates memory proportional to total stake. Define `e=(height-1)/256` and position `i=(height-1)%256`; heights must be >=1.

1. Validate nonzero canonical network ID, strictly positive voting powers, unique validator IDs, canonical P-256 public keys, and checked total power. Sort by binary validator ID ascending. Validator membership and power changes occur only at certified epoch transitions with an explicitly agreed activation height.
2. Compute each validator's `floor(256*power/T)` quota and remainder using exact **arbitrary-precision integer** multiplication/division (not `uint64` multiplication). Allocate leftover slots to largest fractional remainders, breaking equal remainders using a deterministic domain-separated hash of `network || genesis-scheduler-seed || epoch || validatorID` (and then ID lexicographic order). Quotas sum to exactly 256. Note: very small stakeholders can receive zero slots in an individual epoch; testing must cover long-horizon representation.
3. Interleave the integer quotas across the 256 positions with a **canonical deficit algorithm**: for every position, add each validator's epoch quota to its signed big-integer deficit, select the greatest deficit (hash+ID tie-break), and subtract 256 from the chosen deficit. Initialize deficits to zero each epoch. All arithmetic and tie-breaking must be specified byte-for-byte in an implementation-independent consensus test vector; no floating point, map-order iteration, or host-local random state.
4. For a round `r`, use the schedule slot at `(i + r%256)%256`, computed with a bounded addition. This is a *proposal candidate*, not proof that valid alternative proposals can be unlocked. Multi-round signing remains subject to #226 conservative same-height target lock and requires separately certified lock/unlock proofs before enabling timeout-driven proposer changes.
5. The seed must be committed in genesis/configuration and protocol-version metadata. A public fixed genesis seed gives fairness but **not unpredictable future leaders**. If unpredictable leader order becomes a requirement, design a separate bias-resistant beacon with proof of non-manipulability and unavailability fallback, and update the threat model first. Do not call the present deterministic schedule randomness.

This construction removes power-unit-derived 100-block windows for equal-stake lab validators and makes the schedule computationally bounded by `256*N` per epoch. **It does not guarantee a universal maximum leader streak for arbitrarily concentrated stake**, nor perfect distribution of sub-1/256 stake within one epoch; state quantitative fairness limits before activation. The deficit interleaver, tie-break strategy, and any optimizations require cross-implementation vectors and formal/adversarial review.

## Required before activation

- Implement an independent **opt-in** candidate scheduler (no call-site changes), golden vectors for 4/7 equal-power sets, weighted (1:2:4 and 1:99) sets, reordered validators, epoch boundaries and rounds, zero/duplicate members, max-`uint64` powers, near-overflow heights and round values, and deterministic repeat/restart. Compare exact quota and schedule against a separate reference implementation.
- Quantify quotas versus actual selection counts over many epochs, longest contiguous proposer streak, predictable targeted DoS, minority liveness and bias sensitivity; do **not** claim bounded streaks for >50% stake without proof.
- Commit an explicit new protocol version and activation height in signed genesis/upgrade configuration; bind the version to proposal validation and persisted headers, certified history replay and observer checks. Old pre-activation V2 QCs MUST retain the original proposer algorithm; no retroactive reinterpretation.
- Add cross-version transition, rollback/replay and mismatched-version rejection tests. A mixed-version set must not finalize conflicting branches. Keep quorum >2/3, durable signing locks and real 4/7-process lab checks unchanged.
- Resolve #226 lock/unlock and #211 multi-host independent validators, genuine network partitions and signing custody independently. Mainnet #42 remains **NO-GO**.

The design deliberately separates a candidate fairer leader assignment from actual production liveness and safety. No feature gate or test should enable it merely because the draft algorithm appears to rotate leaders.

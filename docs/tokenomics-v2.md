# Zephyr v2 Tokenomics — Protocol Economics and Purchasing-Power Target

Status: **design contract + shadow-mode implementation target**.

This document tracks the executable Protocol v2 economic foundation against the Zephyr Chain *Protocol Economics, Monetary Design, Governance, Lending and Consensus Incentives* working paper v0.1 dated 19 August 2026.

The working paper supersedes the earlier assumption that the monetary objective is a fixed annual ZPH supply-growth rate. The canonical objective is now expressed in Zephyr-native purchasing power. Supply remains endogenous and any monetary actuator stays shadow-only until explicit activation gates pass.

## 1. Canonical monetary objective

The intended long-run property is:

```text
PP_t = 1 / ZPPI_t
PP_(t+1) = 0.98 * PP_t
ZPPI_(t+1) / ZPPI_t = 1 / 0.98 ~= 1.020408163
```

Therefore an exact annual **-2.00% purchasing-power target** corresponds to approximately **+2.0408% annual ZPPI inflation**.

The protocol stores the canonical Q9 annual factor:

```text
TargetZPPIAnnualFactorQ9 = 1_020_408_163
```

This is not a fiat CPI target and does not require an exchange-price oracle. ZPPI is the price, in ZPH, of a versioned basket of competitively priced Zephyr-native services.

## 2. Separation of control loops

Zephyr keeps independent control signals so one metric cannot dominate monetary policy, resource pricing and consensus simultaneously.

```text
finalized native markets
  -> dynamic ZCU / service normalization
  -> ZCPI + DA/storage observations
  -> ZPPI
  -> slow ZAMP monetary evaluation

compute demand/supply/backlog/utilization
  -> ZCSI
  -> primarily reward routing / capacity incentives

coin age + productive coverage + concentration
  -> IdlePressure / future idle-capital levy

stake + reliability + diversity
  -> consensus SecurityWeight
```

The fast blockchain resource-fee loop remains separate from the slow monetary loop.

## 3. Dynamic ZCU reference

A fixed FLOP benchmark becomes obsolete as hardware and software improve. Protocol v2 therefore retains the multidimensional `WorkVector` accounting layer and adds a shadow dynamic reference for each compute work class.

A verified compute-slot observation contains benchmark-derived performance plus evidence-weighted availability history. Provider-declared peak performance is not an input.

```text
EffectiveSlotWeight =
    DeliveredSlotTime
  * AvailabilityEWMA
  * SuccessEWMA
  * Confidence

ZCU_ref(class, epoch) = weighted_median(verified slot performance)
```

The reference is smoothed with EWMA and bounded by a maximum per-epoch change. Thin classes fail closed as unreliable rather than inventing a reference.

Current implementation:

- `internal/v2/economics/zcu_reference.go`
- deterministic weighted median per work class;
- delivered-slot-time, availability, success and confidence weighting;
- Q9 fixed-point reference values;
- minimum verified-slot coverage gate;
- EWMA smoothing;
- per-epoch rate limit;
- no provider self-reported peak-performance field.

This is **shadow telemetry only**. It does not change live rewards or supply.

## 4. ZCPI and ZCSI

Existing Protocol v2 code already reconstructs verified paid compute observations from finalized compute state and builds:

- ZCPI: finalized paid ZPH per standardized verified compute unit, using per-class medians, EWMA, coverage and reliability;
- ZCSI: bounded compute scarcity from escrow-backed demand, verified supply, backlog, fulfillment, utilization and reliable price trend.

Unreliable price or capacity evidence fails closed. ZCSI is primarily a routing signal for compute incentives and must not become the monetary anchor.

## 5. ZPPI — Zephyr Purchasing Power Index

The initial basket emphasizes:

- compute;
- data availability;
- storage.

Gas/base fee should have low or zero basket weight because it is directly influenced by protocol policy and would introduce reflexive feedback.

The first executable reference uses a version-scoped **chain-weighted price-relative basket**. Each service is normalized to a reference price fixed for that basket version before aggregation, so heterogeneous raw service prices are not treated as equivalent units.

Current implementation:

- `internal/v2/economics/zppi.go`;
- Q9 component price relatives;
- compute / data-availability / storage components;
- version-supplied reference prices and weights;
- reliability-aware coverage;
- EWMA smoothing;
- canonical exact purchasing-power/ZPPI target conversion;
- fail-closed behavior when reliable basket coverage is too low.

A future benchmark/basket governance transition may use geometric aggregation after overlap/calibration. Basket changes must be versioned, delayed and measured across an overlap window.

## 6. ZAMP migration boundary

The branch already contains a deterministic shadow ZAMP evaluator that was built around the earlier 2% annual net-supply-growth center. That controller remains useful as a simulation/actuator baseline, but the **2% supply-growth center is no longer the normative monetary target**.

Until the ZPPI path is integrated end-to-end:

- the legacy supply-growth evaluator remains shadow-only;
- it must not be activated for live minting;
- new ZCU/ZPPI snapshots are measurement foundations, not supply commands;
- the next controller revision must use ZPPI target error as its primary monetary direction signal;
- ZCSI should mainly affect reward routing rather than broad issuance;
- mint, burn, reserve and reward-routing changes remain deterministic consensus state transitions behind explicit activation gates.

## 7. Idle capital and productive coverage — next P1 tranche

The working design defines:

```text
AgePressure(a) = 1 - exp(-k * a)
EffectiveIdle = AgePressure * (1 - ProductiveCoverage)
IdleLevyIntensity_i = Base * EffectiveIdle_i * CapitalPressure_i * GlobalIdlePressure
```

P1 implementation should add shadow-only:

- dormancy histograms;
- capital-lineage metadata for split objects;
- fragmentation/state carrying-cost simulation;
- productive-coverage accounting hooks;
- adversarial scenarios for 10/100/1,000/10,000-way wallet fragmentation.

No balance levy should activate in this tranche.

## 8. Native lending — P2

Native bilateral lending is the productive escape valve for idle capital. A loan becomes productive only after both sides accept terms and funding/collateral locks are atomically satisfied.

The future market includes:

- lender offers and borrower requests;
- counteroffers;
- duration/rate/collateral constraints;
- objective borrower history;
- negative rates;
- anti-wash credit that accrues over time rather than entirely at origination.

Negative rates are intentionally valid when the cost of lending is lower than the expected idle-capital pressure plus risk cost.

## 9. Activation discipline

Economic mechanisms progress through:

```text
Observe -> Shadow -> adversarial simulation -> bounded Active
```

No controller should jump directly from a paper design to live monetary state.

Minimum gates include:

1. all monetary metrics derived reproducibly from finalized consensus state;
2. authenticated benchmark/availability state for verified compute supply;
3. versioned basket and benchmark-era governance with overlap and timelocks;
4. long-horizon manipulation tests for self-cycling, wash lending, fragmentation and concentration;
5. bounded parameter changes and deterministic emergency fallback;
6. Citizen Node decoding/verification of economic state and history;
7. explicit protocol-version/activation-height transitions for mint, reward, fee split or levy changes.

## 10. Implementation order

The current working-paper roadmap is:

- **P0** keep CI/gofmt and Protocol v2 correctness gates green;
- **P1** dynamic ZCU + verified-slot accounting in shadow;
- **P1** ZPPI basket + exact -2% purchasing-power target simulation;
- **P1** dormancy histogram + lineage + fragmentation simulator;
- **P2** bilateral native lending;
- **P2** DeviceCredential abstraction;
- **P2** ZECI concentration telemetry;
- **P3** ContributionScore/Citizen challenge network, VRF committees and governance timelocks;
- **P4** Ethereum collateral light-client adapter and Bitcoin collateral research;
- **P5** incremental economic activation after long-running public simulation.

The engineering rule remains:

```text
measure first -> simulate second -> activate last
```

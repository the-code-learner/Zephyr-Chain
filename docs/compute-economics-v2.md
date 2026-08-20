# Zephyr v2 Compute Economics — ZCR, ZCPI and ZCSI

Status: **design contract + executable shadow-mode instrumentation**.

This document specifies how Zephyr measures compute work and how compute-market conditions may later influence protocol incentives without relying on external oracles.

The core rule is:

```text
measure first -> simulate second -> activate last
```

Nothing in this document authorizes live monetary minting. The current implementation is shadow-only.

## 1. Why one universal compute unit is not enough

CPU, GPU FP32, GPU FP64, tensor/AI, memory-heavy, rendering, storage and network workloads are not honestly comparable with one theoretical FLOP number.

Zephyr therefore models compute as a **normalized resource vector** plus a workload-specific logical unit definition.

The v2 resource vector currently includes:

```text
CPUUnits
GPUFP32Units
GPUFP64Units
TensorUnits
MemoryByteSeconds
VRAMByteSeconds
StorageBytes
NetworkBytes
```

A standardized workload specification binds:

```text
WorkloadHash
BenchmarkHash
WorkClass
normalized Units
WorkVector
version
```

The `BenchmarkHash` anchors the meaning of the unit. Provider self-reported peak performance is not sufficient.

## 2. Eligible compute observations

A compute observation may enter economic telemetry only when all of the following are true:

1. the workload is registered against a versioned benchmark/specification;
2. the job was funded with real on-chain escrow;
3. the job reached a verification-satisfied settled state;
4. the result commitment is finalized;
5. the provider payment is the amount actually settled on-chain.

Zephyr does **not** use advertised provider prices, unfilled offers or arbitrary self-reported capacity as a price observation.

## 3. ZCPI — Zephyr Compute Price Index

ZCPI answers:

> how many atomic ZPH units were actually paid for standardized, verified compute work?

For each eligible workload class:

```text
class price = settled ZPH / normalized verified work units
```

The reference implementation uses:

- integer/fixed-point Q9 prices;
- per-class medians;
- configurable basket weights;
- EWMA smoothing;
- minimum samples per class;
- basket coverage;
- an explicit `Reliable` flag.

If market coverage is too thin, ZCPI is marked unreliable and its price-trend signal is excluded from ZCSI.

ZCPI is an internal Zephyr compute-market index. It is not CPI and does not claim to measure fiat purchasing power or real-world inflation.

## 4. Why ZCPI alone must not control inflation

A higher ZPH price for compute can have multiple causes:

```text
compute supply became scarce
compute demand increased
ZPH purchasing power against compute changed
workload mix changed
```

Therefore `ZCPI up -> mint more ZPH` is not a safe rule.

Zephyr combines price information with observable demand/supply conditions in a separate **Zephyr Compute Scarcity Index (ZCSI)**.

## 5. ZCSI — Zephyr Compute Scarcity Index

ZCSI is a bounded signed score derived only from on-chain/consensus-reproducible compute-market metrics.

The reference inputs are:

```text
EscrowBackedDemandUnits
VerifiedSupplyUnits
BacklogUnits
FulfilledUnits
UtilizationBps
ComputePriceTrendBps
ComputeIndexReliable
```

Interpretation:

- positive ZCSI: standardized compute is relatively scarce;
- near-zero ZCSI: demand and verified capacity are broadly balanced;
- negative ZCSI: verified capacity is abundant relative to demand.

The score is bounded in basis points and uses integer arithmetic only.

## 6. What counts as demand

`EscrowBackedDemandUnits` must represent standardized work for jobs with real locked budget/escrow.

The following do **not** count as monetary demand:

- free API requests;
- un-funded job drafts;
- provider-created fake offers;
- arbitrary mempool messages;
- unregistered workload units.

This makes demand manipulation economically costly rather than free.

## 7. What counts as supply

`VerifiedSupplyUnits` must eventually be derived from capacity that is:

- benchmarked against an approved workload specification;
- bound to a provider identity;
- backed by collateral where required;
- recently demonstrated/available rather than permanently self-declared;
- normalized to the same workload units used by demand.

The current ZCSI implementation consumes this metric but does not yet define the final consensus transition that produces the capacity registry. Until that registry is authenticated, ZCSI remains shadow-only.

## 8. ZCSI components

The reference score combines five signals.

### Demand/supply pressure

Conceptually:

```text
(Demand - Supply) / Supply
```

bounded to a finite interval.

### ZCPI price trend

The smoothed ZCPI trend enters only if the compute index is reliable. If reliability is false, its weight is removed rather than treated as zero-price information.

### Backlog pressure

```text
Backlog / EscrowBackedDemand
```

A growing funded backlog indicates work waiting for capacity.

### Utilization pressure

```text
VerifiedUtilization - TargetUtilization
```

High persistent utilization supports a scarcity interpretation; low utilization pushes in the opposite direction.

### Fulfillment pressure

```text
1 - FulfilledWork / EscrowBackedDemand
```

A low fulfillment ratio indicates that funded standardized demand is not being satisfied.

## 9. Reliability gates

ZCSI has its own reliability gate in addition to ZCPI reliability.

The reference configuration requires minimum standardized demand and verified supply volumes. If these are not met:

```text
ZCSI.Reliable = false
```

An unreliable ZCSI is prohibited from moving either compute incentive routing or the monetary target in the shadow feedback evaluator.

## 10. Three feedback modes

The repository implements three **simulation-only** modes.

### Mode A — Observe only

```text
ZCSI -> telemetry
compute reward share -> unchanged
inflation target -> unchanged
```

This is the default and safest mode.

### Mode B — Reward routing

```text
ZCSI -> compute incentive share
inflation target -> unchanged
```

When verified compute is scarce, a larger fraction of the epoch's **net issuance budget** may be suggested for verified compute incentives. When capacity is abundant, that share may fall.

This changes distribution, not the total monetary target.

This is the leading candidate for first activation after sufficient devnet evidence.

### Mode C — Reward routing + narrow monetary band

```text
ZCSI -> compute incentive share
ZCSI -> small bounded inflation correction
```

Mode C may additionally suggest a very small correction around the ZAMP target. The correction is:

- bounded;
- integer-only;
- subject to ZAMP's overall min/max band;
- disabled when ZCSI is unreliable;
- shadow-only until long simulations show a clear stability benefit.

The compute feedback sensitivity is intentionally much smaller for total inflation than for reward routing.

## 11. Reference default parameters are test values

The checked-in defaults exist to make simulations reproducible. They are not mainnet constants.

The current reference configuration uses a 10,000-bps bounded ZCSI score and a weighted mix of demand/supply, price trend, backlog, utilization and fulfillment.

The current feedback reference starts with a compute reward share and permits a larger adjustment to **distribution** than to the total inflation target.

No parameter should become public-network policy without replay, sensitivity and adversarial simulation.

## 12. Anti-manipulation model

The main attacks to test are:

### Wash compute

An attacker controls both client and provider and creates fake jobs.

Mitigations to measure:

- real escrow/settlement cost;
- standardized verified work requirement;
- provider collateral/slashing;
- fee burn;
- median rather than mean pricing;
- EWMA smoothing;
- per-identity/provider concentration limits if required.

### Fake demand spam

Unfunded jobs do not count. Only escrow-backed standardized demand enters ZCSI.

### Fake supply

Advertised GPU/CPU capacity does not count merely because a provider claims it. Production supply accounting must be benchmark-backed and availability-aware.

### Price self-trading

ZCPI uses completed verified settlements, but a client/provider pair can still trade with itself at a cost. Median, minimum sample/coverage requirements, collateral, fees and future concentration metrics reduce leverage. Devnet simulation must explicitly measure the cost of moving the index.

### Workload-mix attack

Per-class ZCPI values are preserved. Basket weights and registry activation are versioned so one newly created workload class cannot silently redefine the whole compute economy.

## 13. Monetary relationship

ZCSI should influence **incentive routing before total issuance**.

The intended priority is:

```text
1. measure compute scarcity
2. adjust suggested compute incentive share
3. observe whether new capacity enters
4. only after evidence, test a small total-inflation correction
```

The protocol should not solve a GPU shortage by blindly printing currency. The goal is to direct incentives toward the scarce resource while preserving a stable monetary constitution.

## 14. Economics simulator

`cmd/zephyr-econ-sim` supports the base ZAMP shadow decision and optional compute-market inputs.

When `computeMarket` is present, `epoch` is required. The simulator returns:

```text
monetary
scarcity (ZCSI)
feedback (A/B/C shadow decision)
```

A representative input shape is:

```json
{
  "epoch": 42,
  "priorTargetBps": 200,
  "metrics": {
    "Supply": 1000000000,
    "CirculatingSupply": 900000000,
    "StakedSupply": 450000000,
    "ProtocolReserve": 100000000,
    "BurnedThisEpoch": 12000,
    "FinalizedOperations": 1000000,
    "ResourceUtilizationBps": 5000,
    "AgeWeightedVelocityBps": 5000,
    "ComputeIndexQ9": 7500000000,
    "ComputePriceTrendBps": 800,
    "ComputeIndexReliable": true
  },
  "computeMarket": {
    "EscrowBackedDemandUnits": 200000,
    "VerifiedSupplyUnits": 150000,
    "BacklogUnits": 30000,
    "FulfilledUnits": 170000,
    "UtilizationBps": 8500,
    "ComputePriceTrendBps": 800,
    "ComputeIndexReliable": true
  },
  "feedbackPolicy": {
    "Mode": 1,
    "BaseComputeRewardShareBps": 1000,
    "MinComputeRewardShareBps": 0,
    "MaxComputeRewardShareBps": 3000,
    "RewardSensitivityBps": 2000,
    "MonetarySensitivityBps": 25,
    "MaxInflationCorrectionBps": 25
  }
}
```

Mode values are currently:

```text
0 = observe only
1 = reward routing
2 = reward routing + narrow monetary band
```

These numeric values are experimental protocol-tooling values, not a public RPC stability promise.

## 15. Required simulation matrix

Before any non-zero compute feedback becomes live, replay at least:

- balanced demand/supply;
- AI demand boom;
- sudden provider loss;
- rapid GPU capacity entry;
- persistent overcapacity;
- ZCPI rising with stable demand/supply;
- ZCPI falling while backlog rises;
- thin/unreliable market coverage;
- wash compute between related accounts;
- provider concentration/cartel scenarios;
- job spam with and without escrow;
- compute-price shocks during low/high fee burn;
- validator/partition events while compute metrics are incomplete;
- recovery after prolonged scarcity;
- alternating scarcity/abundance designed to induce controller oscillation.

Compare all three modes on exactly the same epoch sequence.

## 16. Activation gates

Compute feedback remains shadow-only until:

1. workload registry changes are authenticated and delayed;
2. verified supply accounting is consensus-reproducible;
3. demand includes only escrow-backed standardized work;
4. ZCPI and ZCSI can be reproduced by Citizen Nodes from finalized state;
5. index manipulation cost is quantified;
6. feedback remains stable under adversarial oscillating workloads;
7. Mode B demonstrates that reward routing increases useful capacity without destabilizing ZPH;
8. Mode C, if ever considered, demonstrates a material benefit over Mode B;
9. governance changes to weights/sensitivities are bounded and delayed;
10. unreliable/missing compute metrics fail closed to zero feedback.

## 17. Current recommendation

Run Mode A on early devnet to collect data.

Then replay the same history under A/B/C.

If evidence supports activation, prefer **Mode B first**: keep the ~2% ZAMP monetary target structurally stable while allowing compute scarcity to redirect part of net issuance toward verified capacity.

Mode C should remain a later option, not the default assumption.

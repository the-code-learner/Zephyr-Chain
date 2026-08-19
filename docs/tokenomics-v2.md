# Zephyr v2 Tokenomics — Adaptive Monetary Policy

Status: **design contract + shadow-mode implementation target**.

This document defines the economic direction for ZPH in Protocol v2. It deliberately separates mechanisms that are already executable in the branch from mechanisms that must remain in shadow/simulation mode until devnet evidence demonstrates stability and manipulation resistance.

## Goals

Zephyr does not use a fixed maximum ZPH supply as its long-term monetary rule. The target is a deterministic, oracle-free adaptive policy with a long-run **net supply growth center near 2% per year**.

The policy must:

- be computable from consensus state only;
- be independently reproducible by validators, full nodes and Citizen Nodes;
- never depend on USD prices, CPI, exchanges, energy prices or other external oracles;
- distinguish fast fee-market control from slow monetary-policy control;
- use integer/fixed-point arithmetic only;
- rate-limit monetary changes;
- resist wash activity, fake offers and short-lived transaction spam;
- remain observable in shadow mode before it is allowed to mint or burn protocol supply.

A 2% target refers to **ZPH monetary supply growth**, not real-world purchasing-power inflation. Without an external price oracle Zephyr cannot claim to track CPI or any fiat purchasing-power index.

## 1. Two independent control loops

### Fast loop — every block

The fast loop prices scarce blockchain resources and can burn a base-fee component.

Conceptually:

```text
resource use
  -> block/shard utilization
  -> dynamic base fee
  -> fee burn + validator/reward component
```

Future resource pricing should account for:

- transaction base work;
- signature verification;
- witness/proof bytes;
- state reads/writes;
- contract fuel;
- data-availability bytes;
- cross-shard receipts;
- other consensus-critical resource units.

The fast loop may react to congestion quickly. It does **not** directly change the long-term monetary target.

### Slow loop — every monetary epoch

The slow loop is the **Zephyr Adaptive Monetary Policy (ZAMP)**.

It observes smoothed on-chain economic/security metrics and calculates the gross mint that would be required to hit the epoch net-issuance target after burn.

The branch currently implements this controller in **shadow mode only**.

```text
supply
burn
stake ratio
protocol reserve ratio
resource utilization
age-weighted velocity
finalized operations
compute-market telemetry
        |
        v
slow bounded controller
        |
        v
gross mint target
        |
        - burn already observed
        v
net supply target near 2% annualized
```

Shadow mode computes the decision but does not mutate supply.

## 2. Net inflation target

For an epoch, the base target is approximately:

```text
NetIssuanceTarget = Supply * TargetInflation / EpochsPerYear
```

where the default center is 200 basis points (2%).

If `B` ZPH were burned during the epoch and `N` is the desired positive net issuance, the gross mint target is:

```text
GrossMintTarget = N + B
```

therefore:

```text
GrossMintTarget - Burn = N
```

Burn and mint are separate accounting flows. A high burn rate does not automatically make the currency permanently deflationary if the monetary constitution targets a positive net supply rate.

## 3. Adaptive band and rate limit

The target is not intended to jump with short-term activity. The current shadow reference policy uses a center, a bounded range and a maximum movement per epoch.

The checked-in defaults are simulation parameters, **not public-mainnet constants**:

```text
center:          2.00%
shadow minimum:  1.50%
shadow maximum:  2.50%
max change:      1 basis point / epoch
```

These numbers exist so the controller can be tested. They require economic simulation before activation.

## 4. Oracle-free monetary signals

ZAMP can use only values committed by Zephyr consensus.

### Supply and burn

Directly known:

- total ZPH supply;
- circulating supply;
- ZPH burned by the fee mechanism;
- protocol-minted ZPH;
- protocol reserve.

### Security

Directly known:

- staked/bonded ZPH;
- validator voting power;
- collateral/slashing state.

The controller may gently increase incentives when staking/security coverage is below target and reduce them when coverage is comfortably above target.

### Network utilization

Do not use raw HTTP requests or mempool ingress. Use finalized consensus resource consumption.

A future resource-usage index should be derived from signed/finalized work such as state writes, proof bytes, fuel, DA bytes and receipt processing.

### Finalized operations

Operation counts are secondary signals only. They are not sufficient alone because an attacker can generate economically meaningless activity.

### Age-weighted monetary velocity

Simple transfer volume is wash-tradeable. The intended Zephyr velocity metric is based on native object history and gives more weight to value that remained unspent for meaningful time before moving.

Repeatedly cycling the same fresh coin object should therefore contribute far less than genuinely circulating older liquidity.

Velocity must be smoothed over long windows (for example EWMA/rolling epochs) before it can influence monetary policy.

## 5. Zephyr normalized compute work

There is no honest universal scalar that makes every CPU, GPU, AI training job, renderer and scientific workload directly equivalent.

Zephyr therefore uses a **resource vector**, not a fake universal FLOP count.

The current v2 model defines normalized dimensions including:

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

A standardized workload definition also carries:

- protocol work-spec version;
- workload class;
- normalized logical work units;
- workload hash;
- benchmark/specification hash;
- resource vector.

The benchmark hash anchors the meaning of the units. A provider cannot make its GPU appear more valuable merely by self-reporting a larger number.

## 6. Compute workload registry

Only protocol-approved work specifications are eligible for monetary telemetry.

A registry entry binds:

```text
WorkloadHash
  -> WorkClass
  -> normalized Units
  -> WorkVector
  -> BenchmarkHash
```

Conflicting definitions for the same workload hash are rejected.

The initial implementation is an executable reference registry. Before monetary activation the registry must become an authenticated protocol/governance state transition with explicit versioning and activation heights.

## 7. ZCPI — Zephyr Compute Price Index

ZCPI is an internal Zephyr compute-market price index. It is **not** a CPI and is not a claim about real-world inflation.

It answers a narrower question:

> how many atomic ZPH units were actually paid for standardized, verified compute work on Zephyr?

ZCPI deliberately excludes:

- advertised provider prices;
- unfilled offers;
- self-reported theoretical FLOPS;
- failed/unverified jobs;
- arbitrary unregistered workload units.

An eligible observation is generated only from:

```text
registered workload spec
+ finalized compute job
+ verification-satisfied result
+ actual on-chain provider payments
```

For each workload class:

```text
price = paid ZPH / normalized verified work units
```

The reference implementation uses fixed-point Q9 arithmetic, per-class medians and EWMA smoothing.

## 8. ZCPI basket, coverage and reliability

Different resource classes retain different prices. ZCPI may combine them into a weighted basket for telemetry, but it also reports each class separately.

A class enters an epoch index only when it has at least the configured minimum number of verified observations.

The index reports:

- price per class;
- sample count per class;
- weighted basket price;
- basket coverage in basis points;
- a `Reliable` flag;
- total accepted observations.

If too little of the configured basket has adequate data, the index remains unreliable and must not be used by monetary policy.

This prevents the chain from manufacturing a compute-price signal during thin markets.

## 9. Compute prices are telemetry-only in ZAMP v0

The branch deliberately records `ComputeIndexQ9`, compute-price trend and reliability in the shadow monetary decision **without allowing them to change the inflation target yet**.

This is intentional.

A rising ZPH price for compute can mean several different things:

- compute resources became scarce;
- demand for compute increased;
- ZPH purchasing power against compute fell;
- workload mix changed.

Without sufficient history it is unsafe to infer which cause dominates.

Activation requires simulation showing that a compute-price feedback term improves stability rather than creating a manipulable reflexive loop.

## 10. Native ZPH fee accounting

Current v2 transaction execution already requires native ZPH inputs to cover `outputs + Fee`.

Before public economic activation this must evolve into an explicit fee-accounting engine rather than an implicit 100% fee disappearance.

The intended structure is:

```text
transaction resource charge
       |
       +-- base-fee component -> burn
       |
       +-- execution/priority component -> validator/reward pool
       |
       +-- optional protocol component -> protocol reserve
```

Percentages and fee parameters must be integer basis points and simulation-backed.

## 11. Smart-contract gas

Contract execution already reports deterministic `FuelUsed` and enforces `FuelLimit`.

The economic fee engine should convert deterministic execution/resource consumption into ZPH cost, for example conceptually:

```text
contract fee =
    base transaction resource charge
  + FuelUsed * FuelPrice
  + state read/write charges
  + proof/data-availability charges
```

The wallet should sign a maximum acceptable resource/fee envelope. Validators must not be able to raise it after signing.

## 12. Compute payment is not blockchain gas

Heavy compute has two independent prices:

```text
provider payment
+ blockchain settlement fee
```

A 100 ZPH AI job does not imply 100 ZPH of gas. Validators settle commitments/proofs/results; they do not replay the expensive workload.

## 13. Native custom-token policy

Protocol-native custom assets retain independent supply policies.

The desired explicit policies are:

```text
FIXED
CAPPED
MINTABLE
```

Native mint/burn operations must update both coin objects and the authenticated `TokenDefinition.CurrentSupply` so Citizen Nodes can prove supply correctness.

Before public activation the executor must add explicit `MintToken` and `BurnToken` operations and enforce:

- mint authority;
- cap where applicable;
- irreversible fixed-supply policy;
- burn permission;
- transferability policy.

ZPH itself must not have a human mint authority. ZPH issuance is controlled only by the protocol monetary state machine after activation.

## 14. Testing strategy

### Unit/conformance tests

The branch includes deterministic tests for:

- normalized work-spec serialization;
- conflicting compute-registry definitions;
- deriving a compute observation only from a settled verified job;
- class medians and basket coverage;
- ZCPI reliability thresholds;
- compute price trend bounds;
- burn offset in the shadow monetary controller;
- bounded/rate-limited adaptive inflation target;
- compute telemetry having zero monetary influence in ZAMP v0.

### Monetary replay simulator

`cmd/zephyr-econ-sim` accepts a JSON epoch snapshot and prints the shadow decision.

Example shape:

```json
{
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
    "ComputePriceTrendBps": 250,
    "ComputeIndexReliable": true
  }
}
```

This enables historical replay, synthetic shocks and sensitivity analysis without changing consensus supply.

## 15. Activation gates

ZAMP remains shadow-only until all of the following are true:

1. explicit ZPH supply/burn/mint accounting exists in authenticated protocol state;
2. fee distribution is explicit and conserves supply exactly;
3. velocity is demonstrably resistant to cheap self-cycling;
4. all monetary metrics are reproducible from finalized state;
5. long simulations cover low/high usage, partitions, validator churn, spam and compute-market shocks;
6. parameter sensitivity does not create oscillation or runaway mint/burn behavior;
7. governance can change parameters only through bounded, delayed transitions;
8. Citizen Nodes can independently verify monetary state and epoch decisions;
9. ZCPI has sufficient real-market coverage before any non-zero monetary weight is considered;
10. an emergency safety rule can freeze adaptive corrections while preserving deterministic base issuance if metrics become unavailable or invalid.

## 16. Current policy boundary

The current branch implements **measurement and shadow decisions**, not live monetary issuance.

The design principle is:

```text
measure first
simulate second
activate last
```

This lets Zephyr use an adaptive oracle-free economy without turning monetary policy into an untested consensus experiment.

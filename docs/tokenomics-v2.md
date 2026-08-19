# Zephyr v2 Tokenomics — Adaptive Monetary Policy

Status: **design contract + shadow-mode implementation target**.

This document defines the economic direction for ZPH in Protocol v2. It deliberately separates mechanisms that are already executable in the branch from mechanisms that must remain in shadow/simulation mode until devnet evidence demonstrates stability and manipulation resistance.

Detailed companion specifications:

- `docs/compute-economics-v2.md` — normalized compute work, ZCPI, ZCSI and A/B/C feedback experiments;
- `docs/economic-state-v2.md` — consensus-stamped coin age, velocity, per-shard epoch accounting and authenticated shadow monetary state.

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
- remain observable in shadow mode before it is allowed to mint protocol supply.

A 2% target refers to **ZPH monetary supply growth**, not real-world purchasing-power inflation. Without an external price oracle Zephyr cannot claim to track CPI or any fiat purchasing-power index.

## 1. Two independent control loops

### Fast loop — every block

The fast loop prices scarce blockchain resources and can burn a base-fee component.

Conceptually:

```text
resource use
  -> block/shard utilization
  -> dynamic base fee
  -> fee burn + validator/reward + reserve components
```

The reference fee engine can price:

- transaction base work;
- signature verification;
- witness/proof bytes;
- state reads/writes;
- contract fuel;
- data-availability bytes;
- cross-shard receipts.

It also supports deterministic burn/validator/reserve splitting with exact value conservation. The current compatibility policy remains full fee burn until authenticated reward/reserve distribution is activated.

The fast loop may react to congestion quickly. It does **not** directly change the long-term monetary target.

### Slow loop — every monetary epoch

The slow loop is the **Zephyr Adaptive Monetary Policy (ZAMP)**.

It observes smoothed on-chain economic/security metrics and calculates the gross mint that would be required to hit the epoch net-issuance target after burn.

The branch implements this controller in **shadow mode only**.

```text
supply
burn
stake ratio
protocol reserve ratio
blockchain resource utilization
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

Shadow mode computes and can authenticate the decision but does not mutate live ZPH supply.

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

Directly knowable from protocol state/accounting:

- total ZPH supply;
- circulating supply;
- ZPH burned by the fee mechanism;
- protocol-minted ZPH after future activation;
- protocol reserve.

### Security

Directly known:

- staked/bonded ZPH;
- validator voting power;
- collateral/slashing state.

### Network utilization

Do not use raw HTTP requests or mempool ingress. Use finalized consensus resource consumption.

Blockchain resource utilization and compute-market utilization are separate signals and must not be conflated.

### Finalized operations

Operation counts are secondary signals only. They are not sufficient alone because an attacker can generate economically meaningless activity.

### Age-weighted monetary velocity

Simple transfer volume is wash-tradeable. Zephyr v2 coin objects now carry a consensus-stamped `CreatedHeight`.

New coin outputs are rewritten by deterministic execution with the candidate block height, so the wallet cannot choose an old timestamp to create fake monetary age.

The reference velocity accumulator uses:

```text
age = spendHeight - CreatedHeight
```

with configurable minimum age, full-weight age and maximum velocity bounds.

Rapidly recreating and cycling fresh coin objects therefore resets their age and can contribute zero below `MinAgeBlocks`.

Unknown/genesis age (`CreatedHeight = 0`) is tracked separately and excluded by the current reference policy.

## 5. Zephyr normalized compute work

There is no honest universal scalar that makes every CPU, GPU, AI training job, renderer and scientific workload directly equivalent.

Zephyr therefore uses a **resource vector**, not a fake universal FLOP count.

The current model includes:

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

A standardized workload definition carries protocol version, workload class, normalized units, workload hash, benchmark/specification hash and resource vector.

The benchmark hash anchors the meaning of the units. A provider cannot make its hardware appear more valuable merely by self-reporting a larger number.

## 6. Compute workload registry

Only standardized work specifications are eligible for monetary telemetry.

A registry entry binds:

```text
WorkloadHash
  -> WorkClass
  -> normalized Units
  -> WorkVector
  -> BenchmarkHash
```

Conflicting definitions for the same workload hash are rejected.

The current registry is a reference implementation. Before monetary activation it must become authenticated/governance-controlled state with delayed versioned activation.

## 7. ZCPI — Zephyr Compute Price Index

ZCPI is an internal Zephyr compute-market price index. It is **not** a CPI and is not a claim about real-world inflation.

It answers:

> how many atomic ZPH units were actually paid for standardized, verified compute work on Zephyr?

ZCPI excludes:

- advertised provider prices;
- unfilled offers;
- self-reported theoretical FLOPS;
- failed/unverified jobs;
- arbitrary unregistered workload units.

Eligible observations derive from:

```text
registered workload spec
+ finalized compute job
+ verification-satisfied result
+ actual on-chain provider payments
```

The reference implementation uses fixed-point Q9 arithmetic, per-class medians, EWMA smoothing, basket coverage and an explicit reliability flag.

## 8. ZCSI — Zephyr Compute Scarcity Index

ZCPI alone cannot safely drive inflation. A price increase can reflect scarcity, demand growth, ZPH purchasing-power movement or workload-mix changes.

Zephyr therefore separately computes **ZCSI**, a bounded scarcity score based on:

```text
escrow-backed standardized demand
verified standardized supply
funded backlog
fulfilled work
compute utilization
reliable ZCPI price trend
```

Only real escrow-backed standardized work counts as demand. Provider-advertised capacity does not become verified supply merely because it is claimed.

If ZCPI is unreliable, the price component is removed. If demand/supply coverage is too thin, ZCSI itself becomes unreliable.

An unreliable ZCSI is prohibited from changing either compute reward routing or the monetary target in the shadow evaluator.

## 9. Compute feedback experiments A/B/C

The branch implements three **shadow-only** modes.

### A — observe only

```text
ZCSI -> telemetry only
compute reward share -> unchanged
inflation target -> unchanged
```

### B — reward routing

```text
ZCSI -> suggested compute reward share
inflation target -> unchanged
```

This is the preferred first candidate if devnet evidence eventually justifies activation. Scarce verified compute can receive a larger share of an already-defined issuance budget without changing total issuance.

### C — reward routing + narrow monetary band

```text
ZCSI -> suggested compute reward share
ZCSI -> small bounded shadow inflation correction
```

The total-inflation sensitivity is deliberately much smaller than the reward-routing sensitivity.

Mode C must demonstrate a material stability/capacity benefit over Mode B before it is considered for activation.

The current active economic boundary remains equivalent to Mode A: **no compute signal changes live supply**.

## 10. Native ZPH fee accounting

V2 execution requires native ZPH inputs to cover outputs plus the signed fee.

The reference fee engine now supports:

```text
resource charge
       |
       +-- burn
       +-- validator/reward pool
       +-- protocol reserve
```

with integer basis-point splits and deterministic rounding that conserves every atomic unit.

Until state-backed distribution is activated, the compatibility policy preserves the current effective 100% fee burn.

## 11. Smart-contract gas

Contract execution reports deterministic `FuelUsed` and enforces `FuelLimit`.

The reference resource fee model can include:

```text
base transaction charge
+ signature work
+ witness bytes
+ state reads/writes
+ FuelUsed * FuelPrice
+ DA bytes
+ cross-shard receipts
```

Final production prices and the active base-fee controller remain simulation/benchmark decisions.

## 12. Compute payment is not blockchain gas

Heavy compute has two independent prices:

```text
provider payment
+ blockchain settlement fee
```

A 100 ZPH AI job does not imply 100 ZPH of gas. Validators settle commitments/proofs/results; they do not replay the expensive workload.

## 13. Native custom-token policy

Protocol-native custom assets now have explicit supply policies:

```text
FIXED
CAPPED
MINTABLE
```

The v2 executor implements:

- custom-token creation;
- `MintToken` with mint-authority and cap enforcement;
- `BurnToken` with burn-permission enforcement;
- authenticated `TokenDefinition.CurrentSupply` updates;
- `Transferable` enforcement;
- read-only token-definition policy witnesses for parallel normal transfers.

ZPH itself is excluded from user-authority mint/burn paths. Future ZPH issuance can occur only through an explicitly activated protocol monetary transition.

Custom-token cross-shard transfer/mint is deliberately gated until a globally verifiable token-policy proof/registry is available.

## 14. Economic epoch state

Zephyr must not create one global monetary object touched by every transaction; that would serialize execution.

The current foundation therefore uses:

```text
finalized execution
 -> per-shard epoch metrics
 -> deterministic epoch aggregate
 -> shadow MonetaryEpochState
```

Per-shard metrics cover:

- charged/burned/validator/reserve fees;
- finalized operations;
- chain resource used/capacity;
- shard circulating ZPH;
- age-weighted velocity;
- escrow-backed compute demand;
- verified compute supply;
- compute backlog/fulfillment.

Exact fee conservation is validated.

Global velocity is weighted by per-shard circulating ZPH rather than giving every shard equal influence.

Chain resource utilization and compute utilization are calculated separately.

The canonical aggregate hash is committed by a deterministic shadow `MonetaryEpochState` system object. Consecutive states bind `PreviousStateHash`.

The object can enter the normal Sparse-Merkle state root through consume/recreate semantics, so a future epoch-boundary runtime transition does not require another special consensus root.

The current transition builder returns a state delta for pre-QC simulation; it does not commit a store itself.

## 15. Testing and replay

The branch includes deterministic tests for:

- normalized work-spec serialization and registry conflicts;
- verified settlement observations;
- ZCPI medians, coverage, reliability and trend bounds;
- ZCSI demand/supply scarcity and reliability fail-closed behavior;
- A/B/C feedback separation;
- burn offset and rate-limited ZAMP target;
- fee split/resource quotation conservation;
- consensus override of wallet-provided coin creation height;
- old versus fresh age-weighted velocity;
- rapid self-cycling suppression;
- per-shard economic accounting and multi-shard aggregation;
- separation of chain and compute utilization;
- canonical shadow monetary state and previous-epoch binding;
- suggested Mode-C mint remaining shadow while `TotalSupply` is unchanged.

`cmd/zephyr-econ-sim` supports base ZAMP replay and optional compute-market/ZCSI A/B/C inputs. See `docs/examples/zephyr-econ-sim-compute.json`.

## 16. Activation gates

ZAMP remains shadow-only until all of the following are true:

1. per-shard economic metrics are derived automatically from finalized runtime execution rather than caller-provided summaries;
2. fee distribution is state-backed and conserves supply exactly;
3. verified compute supply comes from authenticated benchmark/availability state;
4. velocity is stress-tested against long-horizon self-cycling and capital-lock attacks;
5. all monetary metrics are reproducible from finalized state;
6. long simulations cover usage shocks, partitions, validator churn, spam, compute booms/busts and oscillating adversarial inputs;
7. parameter sensitivity does not create runaway or oscillatory issuance;
8. governance can change parameters only through bounded delayed transitions;
9. Citizen Nodes can decode and independently verify monetary state/history;
10. ZCPI/ZCSI have sufficient real-market coverage before Mode B or C is considered;
11. an emergency deterministic fallback can zero adaptive corrections when required metrics are unavailable/invalid;
12. live mint/reward/fee distribution has an explicit protocol-version/activation-height transition.

## 17. Current policy boundary

The branch implements **measurement, authenticated shadow state and shadow decisions**, not live monetary issuance.

The design principle remains:

```text
measure first
simulate second
activate last
```

This lets Zephyr develop an adaptive oracle-free economy without turning monetary policy into an untested consensus experiment.

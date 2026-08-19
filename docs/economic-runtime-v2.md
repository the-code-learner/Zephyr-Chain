# Zephyr v2 Finalized Economic Runtime

Status: **shadow/experimental implementation**.

This document describes the executable path that turns finalized Zephyr v2 activity into reproducible economic telemetry for ZCPI, ZCSI and ZAMP without activating live minting.

Companion documents:

- `docs/tokenomics-v2.md`
- `docs/compute-economics-v2.md`
- `docs/economic-state-v2.md`
- `docs/protocol-v2-implementation-status.md`

The governing rule remains:

```text
measure first -> simulate second -> activate last
```

## 1. Finality boundary

Economic telemetry is not allowed to advance from mempool admission, proposal construction, unfinalized votes, provider advertisements or RPC summaries.

The intended runtime path is:

```text
proof-carrying transactions
        -> deterministic execution
        -> proposal / votes / QC
        -> finalized global block
        -> FinalizedShardObservation
        -> EpochCollector
        -> ShardEpochMetrics
        -> EpochAggregate
        -> ZCPI
        -> ZCSI
        -> ZAMP shadow decision
        -> pending MonetaryEpochState
        -> normal Merkle state transition in a later finalized candidate
```

`EpochCollector` has an explicit preview boundary so a node can evaluate telemetry before durable state apply and promote the preview only when the corresponding finalized transition succeeds.

## 2. Native-supply attribution across shards

A cross-shard ZPH transfer must not make global supply temporarily disappear while its receipt is in flight.

The collector therefore treats per-shard native supply as an attribution ledger:

```text
source finalizes outbound receipt
    -> global circulating supply unchanged
    -> source attribution remains until import

destination finalizes receipt import
    -> source attribution -= amount
    -> destination attribution += amount
    -> global circulating supply unchanged
```

Fee burn remains a real reduction in circulating ZPH under the current compatibility fee policy.

This attribution rule is economic telemetry. Consensus double-spend protection remains the cross-shard receipt/anti-replay protocol.

## 3. Age-weighted velocity from finalized coins

Only consumed native coin witnesses that actually appear in the finalized execution result enter the velocity accumulator.

`CreatedHeight` is consensus-stamped by execution. Wallet timestamps are not trusted.

The existing minimum-age/full-weight policy therefore continues to make rapid fresh-coin cycling contribute little or zero weight.

## 4. Compute demand is stock-flow, not same-epoch volume

Long AI/scientific/rendering jobs may be posted in one epoch and settle in another. Zephyr therefore does not require `fulfilled <= new demand` inside a single epoch.

The v2 economic wire now uses the exact conservation rule:

```text
OpeningComputeBacklog
+ EscrowBackedComputeDemand
=
ComputeFulfilled
+ ComputeExpired
+ ComputeBacklog
```

where `ComputeBacklog` is the closing backlog carried into the next epoch.

This avoids declaring legitimate long-running jobs invalid merely because their creation and settlement occur in different economic epochs.

## 5. Standardized compute only

A compute job affects standardized ZCSI demand/backlog only when its `WorkloadHash` resolves to an active `WorkSpec` in the workload registry.

Unregistered workloads may still execute in the compute market, but they do not silently acquire a made-up normalization factor and do not enter standardized monetary telemetry.

The standardized job unit comes from the committed `WorkSpec`/`WorkVector`, not from provider-advertised peak FLOPS.

## 6. ZCPI from finalized settlement evidence

The compute execution path commits a canonical `SettlementReceipt` containing:

```text
JobID
ResultRoot
provider payments
refund
slashed collateral
slash reward
expiry flag
```

The economic replay path parses that receipt and recomputes the deterministic settlement from the pre-finalization `OnChainJob` witness.

A receipt is accepted for `VerifiedWork` only when the reconstructed settlement bytes match exactly.

For replicated-majority jobs this includes:

- strict-majority result selection;
- payment only to providers on the accepted result root;
- refund accounting;
- deterministic slashing of dissenting collateral;
- slash-reward accounting.

ZCPI then uses the ZPH actually paid for successfully verified standardized work. Offer prices, refunds and collateral movement are excluded from the compute price observation.

## 7. Authenticated compute supply is a separate reliability gate

A numeric capacity value is not enough to make ZCSI trustworthy.

Each shard metric carries:

```text
VerifiedComputeSupply
ComputeSupplyReliable
```

The numeric value can still be recorded for experiments, but `ComputeSupplyReliable=false` prevents ZCSI from becoming reliable regardless of how large the claimed capacity is.

The production path must eventually derive reliable supply from a consensus-reproducible benchmark/collateral/availability registry.

Until that exists, the safe runtime behavior is:

```text
capacity telemetry may exist
ZCSI reliability = false
ZCSI monetary influence = zero
```

## 8. ZCSI inputs

For an epoch, ZCSI can combine:

```text
active standardized demand
verified standardized supply
closing backlog
fulfilled work
compute utilization
reliable ZCPI trend
```

Active demand is:

```text
OpeningComputeBacklog + EscrowBackedComputeDemand
```

The ZCPI price component is ignored automatically when ZCPI coverage/reliability is insufficient.

The whole ZCSI signal is not marked reliable unless the compute-supply reliability gate and minimum demand/supply thresholds pass.

## 9. Shadow epoch engine

`ShadowEpochEngine` composes one closed economic epoch as:

```text
ShardEpochMetrics
        -> EpochAggregate
        -> BuildComputeIndex (ZCPI)
        -> ComputePriceTrendBps
        -> BuildComputeScarcity (ZCSI)
        -> BuildShadowMonetaryEpochState (ZAMP + compute feedback)
        -> ShadowMonetaryTransition
```

It follows a preview/accept model.

`PreviewCloseEpoch` produces:

- canonical aggregate;
- ZCPI snapshot;
- bounded price trend;
- ZCSI snapshot;
- ZAMP decision;
- compute-feedback decision A/B/C;
- pending `MonetaryEpochState`;
- consumed/created object delta.

It does **not** advance controller history.

`Accept` advances the controller only after the caller has finalized the exact monetary object through normal consensus/state finality.

## 10. Monetary state chaining

Each accepted shadow monetary object commits the prior accepted state through:

```text
PreviousStateHash
```

The engine rejects:

- skipped epochs;
- another network;
- a mismatched aggregate hash;
- a mismatched ZCPI snapshot;
- a broken prior-state hash.

This is designed so a Citizen Node can eventually verify the complete economic-controller history from Merkle proofs rather than trusting a dashboard or validator RPC.

## 11. Feedback modes remain shadow

The three compute-feedback modes remain:

```text
A — observe only
B — change suggested compute reward routing only
C — B plus a narrow bounded suggested inflation correction
```

No mode mints live ZPH in the current implementation.

A reliable ZCSI is mandatory before modes B/C can alter even the simulated reward routing/target.

Mode B remains the preferred first activation candidate if long-run testing eventually supports it.

## 12. Runtime resource utilization

The finalized collector includes a deterministic shadow resource-unit counter so chain utilization can be replayed without wall-clock measurements.

The current reference units account for bounded protocol work such as:

- finalized base transaction;
- transaction intent bytes;
- inputs/consumed/created objects;
- cross-shard outputs/imports;
- data-availability bytes.

This is **not** the final production gas schedule. It exists so simulations can test controller stability using a deterministic load signal while the final resource-pricing schedule remains an activation decision.

## 13. What is implemented now

Executable foundations now include:

- finalized-block economic collector;
- atomic collector preview/apply semantics;
- native supply attribution across shard receipt imports;
- compatibility full-fee-burn accounting;
- age-weighted finalized-spend velocity;
- compute backlog carry-over across epochs;
- compute expiry accounting;
- verified settlement-receipt reconstruction;
- finalized `VerifiedWork` extraction;
- independent authenticated-supply reliability bit;
- canonical v2 shard/aggregate economic metrics;
- previewable ZCPI/ZCSI/ZAMP shadow epoch engine;
- pending Merkle monetary-object delta generation;
- tests for atomic rejection, cross-shard supply conservation, multi-epoch compute backlog, verified settlement pricing and shadow-state chaining.

## 14. What is still required

Before monetary activation, Zephyr still needs:

1. node-runtime wiring from successful `Runtime.Commit` into the finalized collector;
2. automatic epoch-boundary scheduling;
3. insertion of the pending `MonetaryEpochState` into a normal candidate **before** its state root/QC is finalized;
4. authenticated total-supply, staking and protocol-reserve system objects rather than operator-supplied shadow inputs;
5. production benchmarked/collateralized compute-capacity registry;
6. governance-delayed workload-registry and economic-parameter changes;
7. final gas/resource pricing and active burn/validator/reserve distribution;
8. replay/oscillation/manipulation datasets over long devnet runs;
9. Citizen wallet economic-state decoder/history UI;
10. an explicit protocol version/height gate before any live issuance or reward redistribution.

The current invariant is therefore:

```text
finalized data -> reproducible shadow decision
shadow decision != permission to mint
```

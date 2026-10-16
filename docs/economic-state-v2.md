# Zephyr v2 Economic State and Epoch Accounting

Status: **shadow/experimental protocol foundation**.

This document defines how Zephyr can make ZAMP, ZCPI and ZCSI inputs reproducible from finalized chain state without creating a global per-transaction bottleneck.

It complements:

- `docs/tokenomics-v2.md`
- `docs/compute-economics-v2.md`
- `docs/protocol-v2.md`

The current implementation does **not** activate live ZPH minting.

## 1. No global monetary hot object per transaction

A single monetary object consumed by every transaction would serialize execution and work against Zephyr's parallel/object/sharded architecture.

Zephyr therefore separates:

```text
per-transaction execution
        -> per-shard block/epoch telemetry
        -> canonical ShardEpochMetrics
        -> epoch aggregation
        -> shadow MonetaryEpochState
```

Only the epoch boundary needs a global monetary-state transition.

## 2. Consensus-stamped coin age

The v2 `Coin` object includes:

```text
Token
Amount
CreatedHeight
```

`CreatedHeight` is not trusted from the wallet. During execution, every newly created coin output is rewritten with the candidate block height by the deterministic executor.

This applies to:

- native transfers;
- native fee/change outputs;
- token creation;
- custom-token mint;
- custom-token burn change;
- cross-shard outbound coin outputs.

A wallet may place any height in its proposed output bytes, but the executed object uses the consensus execution height.

Height `0` is reserved for genesis/unknown-age data and is excluded from age-weighted velocity until a policy explicitly defines otherwise.

## 3. Age-weighted velocity

Naive on-chain transfer volume is easy to manipulate by cycling the same funds repeatedly.

The reference velocity accumulator instead weights a finalized spend by the age of the consumed coin:

```text
age = spendHeight - CreatedHeight
```

with configurable:

```text
MinAgeBlocks
FullWeightAgeBlocks
MaxVelocityBps
```

For age below `MinAgeBlocks`, the contribution is zero.

Between minimum age and full-weight age, contribution increases with age.

Above full-weight age, the contribution saturates.

Conceptually:

```text
contribution = amount * boundedAgeWeight
```

The epoch value is normalized by circulating native supply.

The accumulator uses arbitrary-precision intermediate arithmetic and only converts to bounded consensus values at finalization.

### Rapid self-cycling

If a coin is spent and immediately recreated, the new output receives the current consensus height. Repeated rapid cycling therefore keeps resetting coin age and cannot repeatedly receive full velocity weight.

This does not make manipulation impossible; it makes the attacker sacrifice time/capital lockup rather than obtaining free volume by fast self-transfers.

## 4. Per-shard economic metrics

Each shard can produce a canonical `ShardEpochMetrics` record containing:

```text
Version
Epoch
ShardID
ChargedFees
BurnedFees
ValidatorFees
ReserveFees
FinalizedOperations
ResourceUsed
ResourceCapacity
CirculatingNativeSupply
AgeWeightedVelocityBps
EscrowBackedComputeDemand
VerifiedComputeSupply
ComputeBacklog
ComputeFulfilled
```

Validation requires exact fee conservation:

```text
ChargedFees = BurnedFees + ValidatorFees + ReserveFees
```

and rejects impossible compute accounting such as fulfilled work above verified capacity or backlog/fulfilled units that overlap beyond funded demand.

Each record has canonical binary bytes and a domain-separated hash.

## 5. Epoch aggregation

`AggregateEpochMetrics` combines unique shard records for the same epoch.

The aggregation is deterministic and overflow-safe.

It calculates two distinct utilization metrics:

### Chain resource utilization

```text
sum(ResourceUsed) / sum(ResourceCapacity)
```

This is a ZAMP/network signal.

### Compute utilization

```text
sum(ComputeFulfilled) / sum(VerifiedComputeSupply)
```

This is a ZCSI/compute-market signal.

They are deliberately separate. High blockchain congestion is not evidence of GPU scarcity, and idle blockspace is not evidence of excess compute capacity.

## 6. Multi-shard velocity weighting

Shard velocity is not averaged equally across shards.

The global epoch velocity is weighted by the native circulating supply represented by each shard:

```text
GlobalVelocity =
    sum(ShardCirculatingSupply * ShardVelocity)
    / sum(ShardCirculatingSupply)
```

A tiny shard therefore cannot move global monetary telemetry as much as a shard holding a large fraction of circulating ZPH merely by reporting an extreme velocity value.

## 7. Canonical epoch commitment

The aggregated economic record has canonical bytes and a domain-separated `EpochAggregate.Hash()`.

This hash is the bridge between high-throughput per-shard accounting and the global epoch monetary state.

The intended future finality path is:

```text
finalized shard metrics
      -> epoch aggregate hash
      -> MonetaryEpochState
      -> state root / global finality
```

The exact placement of the economics commitment in the final public `GlobalHeader` remains an activation decision and must not be changed without wallet/light-client conformance vectors.

## 8. Shadow MonetaryEpochState

The reference implementation defines a deterministic `MonetaryEpochState` system object.

It records:

```text
Network
Epoch
Shadow = true
TotalSupply
CirculatingSupply
StakedSupply
ProtocolReserve
ZAMPBaseTargetBps
SuggestedTargetBps
BaseFee
AggregateHash
ComputeIndexQ9
ComputeIndexReliable
ComputeScarcity score + reliability
FeedbackMode
ShadowGrossMintTarget
ShadowComputeIncentiveMint
InflationCorrection
PreviousStateHash
```

The system-object ID is deterministic for the network and the object version equals the epoch.

## 9. Shadow means no issuance side effect

`BuildShadowMonetaryEpochState` evaluates ZAMP and optional ZCSI feedback but does not mutate supply.

In Mode C it may record, for example:

```text
SuggestedTargetBps = 213
ShadowGrossMintTarget = X
```

while still recording:

```text
TotalSupply = observed pre-transition supply
Shadow = true
```

No transaction/executor path mints those suggested ZPH.

A future activation must introduce a separate, explicit consensus transition and activation height/version. Shadow records are not authorization to mint.

## 10. Epoch state chain

Every state after the first can commit:

```text
PreviousStateHash
```

The builder rejects a previous state from another network or a skipped/non-consecutive epoch.

This enables a Citizen Node or audit tool to replay the economic-controller history rather than trusting a current RPC's summary.

## 11. ZCPI/ZCSI feedback modes

The economic state can record the same three simulation modes defined in `docs/compute-economics-v2.md`:

```text
A: observe only
B: adjust compute reward routing only
C: adjust reward routing + narrow shadow inflation band
```

Mode B remains the preferred first candidate if devnet evidence eventually supports activation.

Mode C remains experimental.

## 12. Still required before live economics

This foundation does not yet complete:

- authenticated production derivation of verified compute capacity;
- final fee split and resource price constants/controller activation;
- live validator reward distribution;
- live protocol reserve credit/debit transitions;
- live ZPH mint/burn monetary transition;
- governance bounds and delayed parameter activation;
- economics commitment in the final light-client/global-header contract;
- long-run epoch replay datasets;
- adversarial money-velocity calibration;
- Citizen wallet UI for economic-state verification;
- economic recovery rules when compute telemetry is incomplete.

Until those gates pass:

```text
ZAMP = shadow
ZCSI feedback = shadow
suggested mint != minted supply
```

## 13. Testing strategy

The repository tests should continuously assert:

- wallet-provided coin age cannot survive execution unchanged;
- rapid fresh-coin cycling has little/zero velocity weight under the configured minimum age;
- old circulating coins receive higher bounded weight;
- per-shard fee accounting conserves every atomic ZPH unit;
- chain utilization and compute utilization are not conflated;
- shard velocity aggregation is supply-weighted;
- duplicate shard metrics are rejected;
- monetary epoch state round-trips canonically;
- epoch state binds the prior epoch hash;
- skipped epochs are rejected;
- Mode C can produce a suggestion without mutating `TotalSupply`.

The engineering rule remains:

```text
measure first -> simulate second -> activate last
```

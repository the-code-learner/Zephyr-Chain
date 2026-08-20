# Zephyr Protocol v2 — Implementation Status

This document tracks what the clean-break v2 branch **actually implements**, what exists only as a reference/experimental boundary, and what still requires production engineering.

Authoritative companion documents:

- `docs/protocol-v2.md` — architecture and clean-break contract;
- `docs/tokenomics-v2.md` — oracle-free ZPH monetary direction;
- `docs/compute-economics-v2.md` — ZCR/ZCPI/ZCSI compute economics;
- `docs/economic-state-v2.md` — coin age, velocity, epoch aggregation and shadow monetary state;
- `docs/protocol-v2-validator-trust.md` — validator trust/rotation.

Status legend:

- **Implemented** — executable code and tests exist on the v2 branch.
- **Integrated foundation** — correctness boundary exists, but production integration is incomplete.
- **Shadow/experimental** — executable measurement/simulation/state exists but cannot change live monetary policy.
- **Not production-complete** — must not be presented as a shipped public-network capability.

## Core identity, trust and wire protocol

**Implemented**

- canonical bounded binary codec for consensus-critical v2 data;
- typed network/account/node/validator/object/token/contract/job identities;
- genesis-derived `NetworkID`;
- independent account, node and validator identities;
- canonical low-S P-256 signing for proof-carrying transactions and validator consensus messages;
- binary proposal, vote and quorum-certificate wire formats;
- binary global-header, shard-commitment, cross-shard-receipt and Merkle-proof decoders;
- canonical Merkle commitment over validator ID, P-256 public key and integer voting power;
- proposal/runtime/import validation binds the exact validator root authorized by consensus;
- v2 genesis derives a `TrustAnchor { NetworkID, ValidatorRoot }` for Citizen wallets;
- QC-authorized `NextValidatorRoot` transitions and checkpoint-history foundations.

## Object state and persistence

**Implemented**

- proof-oriented object model;
- coin objects carrying `Token`, `Amount` and consensus-stamped `CreatedHeight`;
- 256-bit Sparse Merkle Tree with incremental updates;
- compressed inclusion/absence proofs;
- in-memory world-state backend;
- non-mutating copy-on-write state preview;
- durable v2 backend with append-only WAL, CRC32C records, monotonic sequence numbers, network binding, fsync, atomic checkpointing and replay;
- safe truncation of torn WAL tails;
- rejection of persisted state from another network.

`CreatedHeight` is overwritten by deterministic execution for newly created coin objects. Wallet-provided age metadata is therefore not trusted.

**Not production-complete**

- long-duration database growth/compaction benchmarks;
- large-state migration/repair tooling;
- production structured-KV/LSM backend comparison;
- archive/history indexing;
- further proof/state allocation reduction under large batches.

## Proof-carrying transactions, native assets and execution

**Implemented**

- P-256 signed proof-carrying transaction format;
- state-root-bound object witnesses;
- witness verification without requiring full-state trust;
- native ZPH/object transfers;
- custom token creation;
- explicit custom-token supply policies: fixed, capped and mintable;
- native custom-token mint operation with authority/cap enforcement;
- native custom-token burn operation with authenticated `CurrentSupply` update;
- `Transferable` enforcement from a token-definition witness;
- read-only token-definition sharing so independent transfers of one token can remain parallel;
- ZPH cannot be minted through user custom-token authority;
- deterministic conservation and fee checks;
- deterministic parallel batch executor;
- rejection of batches with shared writes, duplicate transactions or different pre-state roots;
- atomic merge of independent execution results;
- state-root simulation before consensus finality;
- permanent shard placement encoded into object IDs;
- contract deploy/call execution path;
- compute-market operations represented in consensus object execution.

**Activation guardrail**

Custom-token cross-shard transfers/mints remain rejected until Zephyr has a globally verifiable token-policy proof/registry path. ZPH cross-shard receipts remain supported.

**Not production-complete**

- authenticated cross-shard custom-token policy distribution;
- active fee distribution objects/accounting for burn/validator/reserve shares;
- finalized production resource-unit gas schedule.

The key invariant remains: candidate execution may calculate a future state root, but committed state is not mutated before a valid quorum certificate exists.

## Consensus and global finality

**Implemented**

- integer voting power and deterministic weighted proposer selection;
- domain-separated proposal/vote signatures;
- locally reconstructed `2/3+` quorum;
- duplicate-voter rejection;
- canonical quorum-certificate hash;
- validator-set root as proposal validity invariant;
- `GlobalHeader` consensus hash without certificate/hash circularity;
- validator rotation commitment via `NextValidatorRoot`;
- dedicated v2 seven-validator conformance and partition-stress gate.

Runtime path:

```text
proof-carrying transactions
        -> parallel execution
        -> state-root simulation
        -> shard commitments
        -> GlobalHeader
        -> proposal
        -> votes
        -> quorum certificate
        -> state commit
```

**Not production-complete**

- broader restart/proposer-death/Byzantine/wrong-chain matrix over production transport;
- governance-controlled validator-set activation policy;
- long-horizon checkpoint pruning/recovery rules.

## Sharding

**Implemented foundation**

- deterministic account routing;
- permanent shard placement in object IDs;
- per-shard state/data/receipt commitments;
- global shard-commitment root;
- remote ZPH outputs become finalized cross-shard receipts;
- receipt Merkle batches/inclusion proofs;
- destination import verifies source QC, validator root, shard proof and receipt proof;
- durable Merkle-state anti-replay marker;
- two-shard end-to-end finalization/import/replay-rejection test;
- hostile self-signed validator-set receipts rejected.

**Not production-complete**

- shard-aware gossip/recovery under sustained faults;
- reshard/split/merge and object migration;
- receipt-marker pruning/history retention;
- custom-token global policy proof;
- 4/16-shard controlled-hardware conformance/recovery/throughput evidence.

`shardCount = 1` remains the safe public activation value until those gates pass.

## Citizen Node and smartphone wallet

**Implemented**

- Go Citizen verifier for headers/state/shard/data proofs;
- battery/network-aware participation policy;
- self-verifiable light API;
- strict wallet verification of canonical headers, low-S P-256 votes, exact quorum, validator roots, shard commitments and Sparse-Merkle proofs;
- genesis/checkpoint trust anchor and validator-root advancement;
- exact `uint64` voting power through decimal JSON + JavaScript `BigInt`;
- wallet resource modes for header-only, relay, DA sampling/cache and opportunistic recent execution.

Because monetary state is a normal Merkle-authenticated system object, its bytes can already be proved through the same state-root path. A dedicated wallet monetary-state decoder/UI is not yet integrated.

**Not production-complete**

- Citizen status/control UI;
- iOS/Android lifecycle/background adapters;
- dedicated ZAMP/ZCSI state decoder/history view;
- multi-peer proof comparison/resumable cache/full relay integration;
- real-device RAM/battery/bandwidth measurements.

## Smart contracts

**Implemented foundation**

- versioned contract deployment/runtime boundary;
- deterministic metered Zephyr Script reference runtime;
- bounded module/request/output/event limits;
- execution-step/fuel limits;
- declared read/write object sets and no undeclared writes;
- no clock/random/filesystem/network/import nondeterminism in the reference runtime;
- contract deploy/call executor integration and receipts.

**Not production-complete**

- production deterministic WASM engine and audited fuel schedule;
- Rust SDK/ABI tooling;
- cross-machine WASM conformance corpus and long-running contract fuzzing.

## Native distributed compute market

**Implemented**

- provider offers and resource/capability requirements;
- collateral requirements;
- job posting with escrow/deadline;
- deterministic offer/job IDs;
- matching/assignment and multi-provider replicated verification;
- provider result submission;
- settlement, unused-escrow refund and expiry;
- objective replicated-majority slashing path;
- compute market object serialization and consensus transitions;
- verification-policy boundaries for deterministic, replicated, challenge, ZK, TEE, client-approved and hybrid evidence.

Heavy compute is provider-executed; validators verify compact settlement evidence rather than replaying expensive workloads.

### Compute economics — shadow/experimental

**Implemented**

- `WorkVector` resource representation instead of one fake universal FLOP scalar;
- workload classes and versioned `WorkSpec` bound to `WorkloadHash` + `BenchmarkHash`;
- conflicting registry definitions rejected;
- only finalized verification-satisfied settlements become `VerifiedWork` observations;
- offer prices and self-reported theoretical capacity excluded from ZCPI price observations;
- per-class medians, Q9 fixed-point pricing, EWMA, basket coverage and reliability;
- bounded compute-price trend;
- ZCSI combines escrow-backed standardized demand, verified supply, backlog, fulfillment, compute utilization and reliable ZCPI trend;
- unreliable ZCPI removes the price component rather than supplying fake information;
- ZCSI has independent minimum-demand/supply reliability gates;
- three simulator feedback modes:
  - A — observe only;
  - B — change suggested compute reward routing, not total inflation;
  - C — reward routing plus a narrow bounded shadow inflation correction;
- unreliable ZCSI cannot move either reward routing or monetary target.

**Not production-complete**

- authenticated/governance-delayed workload registry;
- consensus-reproducible benchmarked/collateralized compute-capacity registry;
- provider daemon/scheduler and input/output distribution protocol;
- concrete production ZK verifier integrations;
- concrete production TEE attestation integrations;
- confidential-data key exchange;
- reputation/concentration/anti-collusion policy;
- empirical ZCPI basket weights and thresholds.

## ZPH tokenomics and adaptive monetary policy

See `docs/tokenomics-v2.md`, `docs/compute-economics-v2.md` and `docs/economic-state-v2.md`.

### Shadow/experimental — implemented

- no fixed max-supply assumption in the v2 economic design;
- long-run net supply-growth center near 2% annually;
- bounded/rate-limited adaptive ZAMP target using only on-chain signals;
- deterministic burn-offset accounting;
- resource fee quotation and burn/validator/reserve split reference engine;
- compatibility fee policy preserves current full-fee burn until authenticated distribution is activated;
- age-weighted velocity accumulator;
- consensus-stamped coin creation height used as the age anchor;
- rapid fresh-coin cycling can be assigned zero contribution below `MinAgeBlocks`;
- age weight saturates at `FullWeightAgeBlocks` and the velocity metric is bounded;
- per-shard canonical `ShardEpochMetrics` for fees, operations, chain resources, circulating native supply, velocity and compute market telemetry;
- exact fee conservation checks in shard epoch metrics;
- deterministic multi-shard epoch aggregation;
- global velocity is weighted by per-shard circulating ZPH rather than equal-weighted by shard;
- blockchain resource utilization and compute utilization are kept separate;
- canonical epoch aggregate hash;
- deterministic network-scoped `MonetaryEpochState` system-object ID;
- canonical shadow monetary-state serialization/hash;
- `PreviousStateHash` chains consecutive economic epochs;
- QC-safe object delta builder returns a state transition without mutating the store itself;
- shadow monetary object can be included in a Merkle state root via normal consume/recreate semantics;
- Mode C records suggested issuance without mutating `TotalSupply`;
- `cmd/zephyr-econ-sim` supports ZAMP plus optional ZCSI A/B/C replay.

### Not production-complete / not active

- ZAMP does not mint live ZPH;
- no public economic parameter set is final;
- active validator/compute/reserve reward distribution is not enabled;
- resource fee prices/base-fee controller are not consensus-active;
- runtime derivation of per-shard epoch metrics from every finalized block still needs completion;
- verified compute supply still needs an authenticated availability/benchmark source;
- the epoch monetary transition is not yet scheduled automatically by the node runtime;
- governance bounds/delays and emergency fallback rules remain open;
- Citizen monetary-state decoding/history UI remains open;
- Mode B/C feedback remains shadow-only until long-run/manipulation testing.

## Data availability

**Implemented foundation**

- data roots and authenticated chunk/sample proofs;
- Reed-Solomon erasure-coded reconstruction;
- corrupted chunks rejected before reconstruction;
- bounded Citizen sampling mode.

**Not production-complete**

- final sampling confidence parameters;
- withholding-attack fault matrix;
- shard-aware data dissemination/repair;
- mobile bandwidth/storage measurements.

## Transport

**Implemented foundation**

- separate consensus, transaction-relay and light-proof protocol capabilities;
- libp2p node identity separated from account/validator keys;
- QUIC path with network-scoped protocol IDs, frame limits/deadlines and loopback tests.

**Not production-complete**

- discovery/bootstrap/NAT/mobile relay policy;
- shard-aware gossip topology;
- full fault-transport equivalence matrix over libp2p/QUIC.

## Performance gates

**Implemented**

- existing finalized-through-consensus v1 Lab remains mandatory;
- dedicated v2 seven-validator conformance/partition gate;
- v2 state/proof microbenchmarks;
- finalized v2 batch benchmark with 32 proof-carrying transfers, 7 validators and 1/4/8/16 execution workers;
- timed path includes witness/signature verification, execution/state-root simulation, proposal/votes, QC and committed transition.

Shared CI numbers are development signals only, never production-capacity claims.

## Activation gates

Before a public v2 devnet:

1. expand v2 fault coverage for restarts, proposer death, Byzantine/conflicting evidence and wrong-chain data;
2. complete long-running durable-state crash/recovery stress;
3. run Citizen verification against live nodes on real Android/iOS reference devices;
4. characterize one-shard performance on controlled hardware;
5. keep multi-shard public mode disabled until shard-aware recovery and 4/16-shard gates pass;
6. make production WASM deterministic/metered across machines;
7. complete production compute provider/evidence/dispute plumbing;
8. derive economic epoch metrics from finalized execution rather than external declarations;
9. authenticate verified compute capacity and registry activation;
10. keep ZAMP and ZCSI feedback shadow-only through replay, manipulation and oscillation tests;
11. activate any mint/reward/fee split only behind an explicit protocol version/height and governance bounds;
12. make genesis/checkpoint/operator upgrade and validator-rotation procedures explicit.

The engineering rules remain:

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
measure first  -> simulate second -> activate last
```

# Zephyr Protocol v2 — Implementation Status

This document tracks what the clean-break v2 branch **actually implements**, what is integrated only as a reference boundary, and what still requires production engineering. It complements `docs/protocol-v2.md`, which remains the architectural contract, and `docs/tokenomics-v2.md`, which defines the adaptive ZPH economic direction.

Status legend:

- **Implemented** — executable code and tests exist on the v2 branch.
- **Integrated foundation** — the protocol boundary and correctness rules exist, but a production backend/network/runtime is still to be selected or connected.
- **Shadow/experimental** — executable measurement/simulation exists but cannot yet change live consensus economics.
- **Not production-complete** — must not be presented as a shipped network capability yet.

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
- every accepted proposal must commit the exact validator-set root used to authorize it;
- runtime commit and cross-shard import reject validator sets that do not match the header's committed `ValidatorRoot`;
- v2 genesis derives a `TrustAnchor { NetworkID, ValidatorRoot }` for Citizen wallets;
- QC-authorized `NextValidatorRoot` transitions and checkpoint-history foundations.

## Object state and persistence

**Implemented**

- proof-oriented object/coin model;
- 256-bit Sparse Merkle Tree with incremental updates;
- compressed inclusion/absence proofs;
- in-memory world-state backend;
- non-mutating copy-on-write state preview;
- durable v2 backend with append-only WAL, CRC32C records, monotonic sequence numbers, network binding, fsync, atomic checkpointing and replay;
- safe truncation of a torn WAL tail after a crash;
- rejection of persisted state from a different network.

**Not production-complete**

- long-duration database growth/compaction benchmarks;
- large-state migration/repair tooling;
- production structured-KV/LSM backend comparison;
- archive/history indexing;
- further proof/state allocation reduction under large batches.

## Proof-carrying transactions and execution

**Implemented**

- P-256 signed proof-carrying transaction format;
- state-root-bound object witnesses;
- witness verification without requiring a full-state lookup for validity evidence;
- native ZPH/object transfers;
- protocol-native token creation;
- deterministic input/output conservation and fee checks;
- deterministic parallel batch executor;
- rejection of batches with shared consumed objects, duplicate transactions or different pre-state roots;
- atomic merge of independent transaction results;
- state-root simulation before consensus finality;
- permanent shard placement encoded into object IDs for multi-shard state;
- contract deploy/call execution path with metered deterministic reference runtime;
- compute-market operations represented in v2 consensus object execution.

**Not production-complete**

- explicit native token mint/burn operations and transfer-policy enforcement;
- explicit fee distribution object/state accounting (burn/validator/reserve split);
- finalized resource-unit gas schedule.

The key invariant remains enforced: candidate execution may calculate a future state root, but committed state is not mutated before a valid quorum certificate exists.

## Consensus and global finality

**Implemented**

- v2 validator set with integer voting power;
- deterministic weighted proposer selection;
- domain-separated proposal and vote signatures;
- locally reconstructed `2/3+` voting-power quorum;
- duplicate-voter rejection;
- canonical quorum-certificate hash;
- validator-set Merkle root as a proposal validity invariant;
- `GlobalHeader` consensus hash that avoids certificate/hash circularity;
- validator rotation commitment via `NextValidatorRoot`;
- dedicated v2 seven-validator conformance and partition-stress gate.

The runtime path remains:

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

- broader restart/proposer-death/Byzantine/wrong-chain fault matrix over the production transport;
- governance-controlled validator-set activation policy;
- long-horizon checkpoint pruning/recovery rules.

## Sharding

**Implemented foundation**

- deterministic account shard routing;
- permanent shard placement encoded in object IDs;
- per-shard state/data/receipt commitments;
- global shard-commitment root;
- remote outputs become finalized cross-shard receipts;
- receipt Merkle batches and inclusion proofs;
- destination import verifies source finality/QC, validator root, shard proof and receipt proof;
- durable Merkle-state anti-replay marker;
- two-shard end-to-end finalization/import/replay-rejection test;
- hostile self-signed foreign validator-set receipts are rejected.

**Not production-complete**

- shard-aware gossip/recovery under sustained faults;
- reshard/split/merge rules and object migration;
- receipt-marker pruning/history-retention policy;
- 4/16-shard conformance, recovery and controlled-hardware throughput evidence.

`shardCount = 1` remains the safe public activation value until those gates pass.

## Citizen Node and smartphone wallet

**Implemented**

- Go Citizen verifier for headers/state/shard/data proofs;
- battery/network-aware participation policy;
- self-verifiable light API (`/v2/light/status`, `/v2/light/object`);
- strict wallet verifier for canonical headers, low-S P-256 votes, exact `2/3+` quorum, validator roots, shard commitments and Sparse-Merkle proofs;
- genesis/checkpoint trust anchor and next-validator-root trust advancement;
- exact `uint64` validator power handling through decimal JSON + JavaScript `BigInt`;
- wallet resource-mode selection for header-only, relay, DA sampling/cache and opportunistic recent execution.

**Not production-complete**

- Vue Citizen status/control UI;
- iOS/Android native lifecycle/background adapters;
- multi-peer proof comparison, resumable cache and full peer relay integration;
- real-device RAM/battery/bandwidth measurements.

## Smart contracts

**Implemented foundation**

- versioned contract deployment/runtime boundary;
- deterministic metered Zephyr Script reference runtime;
- bounded module/request/output/event limits;
- execution-step/fuel limits;
- declared read/write object set and no undeclared writes;
- no clock/random/filesystem/network/import nondeterminism in the reference runtime;
- contract deploy/call executor integration and execution receipts.

**Not production-complete**

- production deterministic WASM engine and audited fuel schedule;
- Rust SDK/ABI tooling;
- cross-machine WASM conformance corpus and long-running contract fuzzing.

## Native distributed compute market

**Implemented**

- compute provider offers and resource/capability requirements;
- collateral requirements;
- job posting with escrow and deadline;
- deterministic offer/job IDs;
- matching/assignment and multi-provider replicated verification;
- provider result submission;
- settlement, unused-escrow refund and expiry;
- objective replicated-majority slashing path;
- compute market object serialization and consensus execution transitions;
- verification-policy boundaries for deterministic, replicated, challenge, ZK, TEE, client-approved and hybrid evidence.

Heavy compute is provider-executed; validators verify compact settlement evidence and do not replay expensive workloads.

**Shadow/experimental compute economics**

- normalized `WorkVector` rather than one fake universal FLOP scalar;
- workload classes and versioned `WorkSpec` bound to `WorkloadHash` + `BenchmarkHash`;
- registry rejects conflicting definitions for the same workload hash;
- only finalized, verification-satisfied settlements can become `VerifiedWork` observations;
- offer prices and provider self-reported capacity are excluded from ZCPI observations;
- deterministic per-class price medians, Q9 fixed-point arithmetic, EWMA, basket coverage and reliability flag;
- compute-price trend is bounded;
- ZCPI is telemetry-only for monetary policy v0.

**Not production-complete**

- authenticated/governance-controlled on-chain workload registry with activation heights;
- provider daemon/scheduler and input/output distribution protocol;
- concrete ZK verifier integrations;
- concrete TEE attestation integrations;
- confidential-data key exchange;
- compute reputation/anti-collusion policy;
- real ZCPI basket weights and minimum-sample thresholds based on observed market data.

## ZPH tokenomics and adaptive monetary policy

See `docs/tokenomics-v2.md`.

**Shadow/experimental**

- no fixed max-supply assumption in the v2 economic design;
- long-run net ZPH supply-growth center near 2% annually;
- bounded adaptive inflation target using only on-chain signals;
- reserve, staking, resource-utilization, age-weighted-velocity and finalized-operation signal inputs;
- burn-offset accounting: gross mint target equals desired net issuance plus observed burn;
- one-basis-point-per-epoch default shadow rate limit;
- ZCPI price/trend/reliability recorded as telemetry but intentionally given zero monetary influence in v0;
- `cmd/zephyr-econ-sim` replays epoch metrics and prints the deterministic shadow decision.

**Not production-complete / not active**

- ZAMP does not mint live ZPH yet;
- no public economic parameter set is claimed final;
- age-weighted velocity metric must be implemented from coin-object history and stress-tested against self-cycling;
- explicit fee split and resource-price controller must be state-backed;
- protocol reserve and total supply must become authenticated monetary state objects;
- governance bounds/delays and emergency fallback rules must be finalized;
- Citizen Node monetary-decision verification must be connected;
- compute price must remain telemetry-only until empirical causality/manipulation studies justify any weight.

## Data availability

**Implemented foundation**

- data roots and authenticated chunk/sample proof boundary;
- Reed-Solomon erasure-coded shard reconstruction path;
- rejection of corrupted chunks before reconstruction;
- Citizen participation mode for bounded sampling.

**Not production-complete**

- final sampling confidence parameters;
- withholding-attack matrix in the fault lab;
- shard-aware data dissemination and repair;
- mobile bandwidth/storage measurements.

## Transport

**Implemented foundation**

- consensus, transaction relay and light-proof retrieval are separate protocol capabilities;
- libp2p node identity is separate from account/validator keys;
- QUIC production transport path with network-scoped protocol IDs, frame limits/deadlines and loopback tests.

**Not production-complete**

- discovery/bootstrap/NAT traversal/mobile relay policy;
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

1. v2 multi-validator consensus must cover partitions, restarts, proposer death, conflicting/Byzantine evidence and wrong-chain data;
2. durable state/runtime metadata must survive crash/restart and longer stress runs;
3. Citizen verification must run against live nodes on real Android/iOS reference devices;
4. one-shard performance must be characterized on controlled hardware;
5. multi-shard mode must stay disabled until shard-aware recovery and 4/16-shard conformance pass;
6. production WASM must be deterministic and metered across machines;
7. real-value compute settlement needs production provider/evidence/dispute plumbing;
8. fee, supply, reserve and ZAMP accounting must be explicit authenticated state;
9. ZAMP must remain shadow-only through replay/simulation and manipulation testing;
10. genesis/checkpoint/operator upgrade and validator-rotation procedures must be explicit.

The engineering rule remains:

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
measure first  -> simulate second -> activate last
```

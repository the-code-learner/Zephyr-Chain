# Zephyr Protocol v2 — Current Executable Status

This snapshot complements the authoritative `docs/protocol-v2.md`. It exists so implementation progress cannot be confused with future design goals.

## Executable now on the v2 branch

### Consensus and trust

- canonical binary proposal, vote, quorum-certificate and `GlobalHeader` objects;
- low-S P-256 validator signatures;
- deterministic weighted proposer selection;
- exact `2/3+` integer voting-power quorum;
- duplicate-voter rejection;
- validator-set Merkle root over validator ID/public key/power;
- proposal rejection when the local authorized validator root differs from the header;
- pre-vote rejection when independently executed local state produces another candidate header;
- `GlobalHeader.ValidatorRoot` plus `GlobalHeader.NextValidatorRoot`;
- QC-backed validator-set transition: current committee authorizes the next committee for the following height;
- genesis-derived Citizen trust anchor (`NetworkID`, `ValidatorRoot`);
- dedicated 7-validator v2 Lab with happy path, 4/3 no-quorum partition/heal, 5/2 quorum/minority catch-up and conflicting-proposal rejection.

### State, proofs and persistence

- object/coin state model;
- 256-bit Sparse Merkle state;
- compressed inclusion/absence proofs;
- proof-carrying P-256 transactions;
- incremental state updates;
- copy-on-write non-mutating root preview before QC;
- preview-vs-real-apply equivalence tests;
- append-only network-bound WAL with CRC32C, sequence numbers and fsync;
- atomic checkpoint and crash-tail recovery;
- wrong-network persisted-state rejection;
- streamed canonical domain hashing and fixed-size branch hashing to reduce hot-path allocations.

### Execution, assets and sharding

- native ZPH/object transfers;
- protocol-native custom token creation;
- deterministic parallel execution of independent transactions;
- conflict rejection for shared inputs, duplicate transactions and mismatched pre-state roots;
- permanent shard placement encoded into object IDs;
- remote outputs become source-shard receipts rather than illegal local writes;
- receipt root committed in the source shard;
- destination import verifies source QC + authorized historical validator root + shard proof + receipt proof;
- destination import creates the deterministic destination object and a Merkle-state anti-replay marker;
- cross-shard receipt is spendable only from a later block because current-block transactions remain anchored to pre-state;
- tested two-shard transfer and durable replay rejection.

### Citizen wallet

- light proof API for finalized status and object proofs;
- Go Citizen verifier;
- browser cryptographic verifier using WebCrypto/BigInt;
- strict rotating-trust verifier in `apps/wallet/src/lib/v2CitizenTrusted.ts`;
- independent validator-root reconstruction, low-S P-256 QC verification, exact quorum, shard proof and Sparse-Merkle proof validation;
- returned `nextTrustAnchor` advances only after the current trusted committee finalizes the new root;
- battery/network-aware modes for verify-only, relay, bounded DA/cache and opportunistic execution.

### Smart-contract foundation

- WASM deployment/runtime abstraction;
- consensus-side metering guard;
- fuel limit;
- bounded args/result/events/access count;
- declared object read/write access;
- rejection of undeclared writes.

### Native compute-market foundation

- CPU/GPU/RAM/VRAM/storage/bandwidth/capability offers;
- collateral and pricing data;
- compute jobs with escrow/deadline;
- provider matching/assignment;
- replicated provider assignment;
- result submission, settlement, refund and expiry;
- deterministic, replicated, challenge, ZK-signal, TEE-attestation-signal, client-approval and hybrid verification policies.

Heavy jobs are provider-executed. Validator consensus settles compact evidence and never requires every validator to replay AI training, scientific computation, rendering or similar workloads.

### CI and measurement

- legacy v1 Consensus & Performance Lab remains a regression gate;
- dedicated `V2 Lab` workflow runs v2 conformance and repeated partition tests;
- finalized v2 32-transfer benchmark uses seven validators and 1/4/8/16 execution workers;
- client key generation/signing remains outside the timed consensus path;
- the pre-allocation-optimization shared-runner sample peaked around 8 workers and showed allocation/state-proof work mattered more than simply increasing worker count, motivating the current hash/preview optimizations.

## Deliberately not called production-complete yet

The following are still engineering gates rather than shipped claims:

- live process-facing v2 node/devnet wiring and durable runtime-height/round metadata;
- staking/governance policy for validator-set updates;
- full v2 fault transport with restart, proposer death, delay, duplicate/reorder and Byzantine cases;
- production libp2p/QUIC/mobile peer discovery and shard-aware gossip;
- 4/16-shard recovery and controlled-hardware scaling proof;
- reshard/split/merge policy;
- concrete deterministic production WASM engine, audited fuel schedule and Rust SDK;
- compute provider daemon, consensus-object escrow/slashing/disputes and concrete TEE/ZK integrations;
- production erasure coding/reconstruction and data-withholding fault tests;
- native Android/iOS lifecycle, resumable Citizen cache/relay and real-device battery/RAM/bandwidth measurements;
- controlled-hardware performance regression budgets;
- public v2 genesis/checkpoint/operator upgrade procedures.

## Activation rule

No subsystem is activated on a public Zephyr v2 network because it exists in code. It is activated only when its safety/liveness tests, restart/recovery tests and relevant performance/resource measurements pass.

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
```

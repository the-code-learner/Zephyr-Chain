# Zephyr Canonical Performance Profile — 2026-08-19

## Scope

This profile uses the canonical Consensus & Performance Lab benchmark on a GitHub-hosted Ubuntu 24.04 runner with Go 1.22.12 and an AMD EPYC 7763 CPU.

The measured workload uses 7 validators, real P-256 transaction validation, real HTTP peer replication, deterministic state execution/state root, quorum-certificate consensus and persisted committed blocks. The profiling run used 5 consecutive benchmark iterations with 32 finalized transfers per block.

This document records engineering evidence, not a production performance claim.

## Observed benchmark sample

The profiling run reported approximately:

- `38.52 finalized-tx/s`;
- `971.5 ms` p50 finality;
- `1.106 s` p95/p99 finality;
- `23,054 B` finalized block size;
- `15,071 B` measured protocol payload per finalized transaction;
- `244,576 B` persisted state per node after the five-iteration sample.

The lower TPS than shorter CI samples is useful: the sustained multi-block run increases persisted state and exposes costs that a one-block microbenchmark hides.

## CPU profile

The strongest CPU signal is persistence/serialization:

- `ledger.(*Store).writeState`: about **58.3% cumulative CPU**;
- `encoding/json.MarshalIndent`: about **54.5% cumulative CPU**;
- `encoding/json.appendIndent`: about **28.6% flat CPU**;
- `ledger.(*Store).Accept`: about **31.9% cumulative CPU**;
- P-256 `tx.VerifySignature`: about **10.1% cumulative CPU**.

The conclusion is that signature verification is material but is not the first bottleneck. Full-state JSON persistence currently costs substantially more CPU than P-256 verification.

## Allocation profile

The allocation profile reinforces the same result. Roughly 2.0 GB were allocated during the profiled process, with:

- `encoding/json.MarshalIndent`: about **40.8% flat allocations** and **73.8% cumulative** through its call tree;
- `ledger.(*Store).writeState`: about **71.4% cumulative allocations**;
- `encoding/json.Marshal`: about **20.4% flat allocations**;
- `bytes.growSlice`: about **11.6% flat allocations**;
- repeated cloning/snapshot construction also contributes materially as the persisted state grows.

The current persistence model serializes and atomically rewrites a broad `persistedState` structure after high-frequency operations such as transaction acceptance, funding and consensus vote recording. That design is excellent for simple correctness/restart guarantees but does not scale as the hot-path persistence architecture.

## Lock contention

The mutex profile identifies the ledger write lock as another direct consequence of the persistence model:

- `ledger.(*Store).Accept`: about **89.7% cumulative mutex delay**;
- `sync.(*Mutex).Unlock`: about **82.7% flat mutex delay**;
- `handleBroadcastTransaction`: about **98.8% cumulative through the affected request path**.

Parallel transaction ingress therefore serializes behind a state-wide critical section that also performs expensive cloning/serialization/persistence work.

## Blocking profile

The blocking profile shows two major classes:

1. ledger lock waiting during concurrent transaction acceptance;
2. synchronous HTTP transaction fan-out to peers.

Notable cumulative blocking signals include:

- HTTP client/send/round-trip paths around **35–37%**;
- `httpPeerTransport.postJSON` around **25%**;
- `Server.broadcastTransaction` around **23%**;
- ledger `Store.Accept` around **13%**.

This confirms that transaction-by-transaction synchronous replication will become a later networking/dissemination bottleneck, but the persistence/lock problem should be addressed first because it dominates both CPU/allocations and local contention.

## Evidence-backed optimization order

The first optimization sequence is therefore:

1. **Persistence hot path**
   - remove human-readable `MarshalIndent` from machine state persistence immediately;
   - stop treating full-state JSON rewrite as the long-term write path;
   - introduce a durable append/batch-oriented journal or structured state backend so mempool/vote/transaction mutations do not reserialize the whole node state;
   - preserve atomic committed checkpoints and restart validation.
2. **Ledger concurrency**
   - reduce the amount of work performed while holding the global store mutex;
   - separate validation/read preparation from the serialized commit section where deterministic safety allows;
   - move toward deterministic batch execution/state updates rather than one durable full-state rewrite per transaction.
3. **State commitment/storage architecture**
   - make state commitments incremental rather than rebuilding broad state structures as the chain grows;
   - introduce a backend abstraction suitable for structured key/value state and deterministic snapshots.
4. **Networking/dissemination**
   - replace synchronous transaction-by-transaction HTTP fan-out with batched/asynchronous dissemination semantics;
   - retain the HTTP transport as the reference implementation while adding libp2p/QUIC later.
5. **Signature verification parallelism**
   - parallelize P-256 verification once persistence/locking no longer masks its cost.

## Immediate next experiment

The first low-risk performance change should replace `json.MarshalIndent` with compact deterministic-equivalent JSON for `state.json` persistence and rerun the exact same 7-validator benchmark/profiles. This does not change consensus semantics or on-disk JSON meaning, but it directly tests the largest CPU/allocation signal.

If the expected improvement appears, the next architectural change should replace repeated full-state rewrites with a journal/checkpoint persistence layer. No storage engine should be selected until that benchmark establishes how much of the remaining cost is serialization, file I/O, state cloning and lock hold time.

# Zephyr Protocol v2 — Implementation Status

This document tracks what the clean-break v2 branch **actually implements**, what is integrated only as a reference boundary, and what still requires production engineering. It complements `docs/protocol-v2.md`, which remains the architectural contract.

Status legend:

- **Implemented** — executable code and tests exist on the v2 branch.
- **Integrated foundation** — the protocol boundary and correctness rules exist, but a production backend/network/runtime is still to be selected or connected.
- **Not production-complete** — must not be presented as a shipped network capability yet.

## Core identity and wire protocol

**Implemented**

- canonical bounded binary codec for consensus-critical v2 data;
- typed network/account/node/validator/object/token/contract/job identities;
- genesis-derived `NetworkID`;
- independent account, node and validator identities;
- canonical low-S P-256 signing for proof-carrying transactions and validator consensus messages;
- binary proposal, vote and quorum-certificate wire formats;
- binary global-header, shard-commitment, cross-shard-receipt and Merkle-proof decoders.

The browser/RPC surface may use JSON, but consensus does not depend on JSON canonicalization.

## Object state and persistence

**Implemented**

- proof-oriented object/coin model;
- 256-bit Sparse Merkle Tree with incremental updates;
- compressed inclusion/absence proofs;
- in-memory world-state backend;
- non-mutating state simulation through cloned Sparse Merkle state;
- durable v2 backend with append-only WAL, CRC32C records, monotonic sequence numbers, network binding, fsync, atomic checkpointing and replay;
- safe truncation of a torn WAL tail after a crash;
- rejection of persisted state from a different network.

**Not production-complete**

- long-duration database growth/compaction benchmarks;
- large-state migration/repair tooling;
- production structured-KV/LSM backend comparison;
- archive/history indexing.

The WAL/checkpoint backend removes the v1 requirement to serialize the complete node state for every mutation, but it is still a first durable backend rather than the final storage engine selection.

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
- state-root simulation before consensus finality.

The key invariant is enforced in code: candidate execution may calculate a future state root, but committed state is not mutated before a valid quorum certificate exists.

## Consensus and global finality

**Implemented**

- v2 validator set with integer voting power;
- deterministic weighted proposer selection;
- domain-separated proposal and vote signatures;
- locally reconstructed `2/3+` voting-power quorum;
- duplicate-voter rejection;
- canonical quorum-certificate hash;
- `GlobalHeader` consensus hash that avoids certificate/hash circularity;
- runtime path:

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

The existing v1 Consensus & Performance Lab remains a regression gate while v2-specific multi-node fault transport integration is expanded.

## Sharding

**Implemented foundation**

- deterministic shard router;
- per-shard state/data/receipt commitments;
- global shard-commitment root;
- `GlobalHeader` committing all active shard roots;
- cross-shard receipt format;
- receipt Merkle batches and inclusion proofs;
- proof that a source receipt belongs to a shard commitment that belongs to a finalized global header;
- destination-shard validation and in-memory anti-replay tracker;
- runtime capable of simulating/committing multiple shard state backends.

**Not production-complete**

- output/object placement rules for active multi-shard execution need final clean-break encoding before `shardCount > 1` is enabled;
- receipt-consumption anti-replay must move from the in-memory tracker into consensus-critical durable state;
- shard-aware gossip/recovery is not connected to production transport;
- reshard/split/merge rules are not activated;
- 4/16-shard conformance and throughput evidence is still required.

`shardCount = 1` remains the safe activation value until these conditions pass. Sharding is an optimization, not a prerequisite for correctness.

## Citizen Node and smartphone wallet

**Implemented**

- Go Citizen verifier for headers/state/shard/data proofs;
- battery/network-aware participation policy;
- self-verifiable light API:
  - `/v2/light/status`;
  - `/v2/light/object`;
- proof bundle contains canonical global header, quorum certificate, validator set, shard commitment and Merkle proof, object bytes and Sparse-Merkle proof;
- validator voting power is encoded as decimal text at the JSON boundary to preserve full `uint64` precision in JavaScript;
- `apps/wallet/src/lib/v2Citizen.ts` independently reconstructs:
  - v2 domain hashes;
  - validator identities;
  - low-S P-256 vote validation;
  - exact `2/3+` quorum with `BigInt`;
  - certificate hash;
  - shard-commitment inclusion;
  - object Sparse-Merkle inclusion/absence proof;
- wallet resource mode selection for header-only, relay, DA sampling/cache and opportunistic recent execution modes.

**Not production-complete**

- the Vue UI does not yet expose the Citizen status/control panel;
- current v1 node process does not yet mount a live v2 runtime/provider;
- iOS/Android native lifecycle/background adapters are not present;
- multi-peer proof comparison, resumable cache and peer relay are not connected yet;
- real-device RAM/battery/bandwidth measurements are still required.

No correctness claim may depend on an RPC response that the Citizen verifier cannot authenticate against finalized state.

## Smart contracts

**Integrated foundation**

- deterministic WASM deployment boundary;
- module magic/shape validation boundary;
- versioned contract deployment model;
- runtime interface independent from a concrete WASM engine;
- consensus guard enforcing:
  - fuel limit;
  - bounded arguments and return data;
  - bounded event count/size;
  - declared read/write object set;
  - no write outside a declared write-enabled object;
  - bounded state-access count.

**Not production-complete**

- production WASM interpreter/JIT selection;
- deterministic opcode/import policy;
- audited fuel schedule;
- contract deploy/call operations inside the main v2 transaction executor;
- Rust SDK/ABI tooling;
- contract conformance corpus.

A concrete WASM runtime will not be called production-ready until deterministic metering survives cross-machine conformance testing.

## Native distributed compute market

**Implemented state-machine foundation**

- compute provider offers;
- CPU/RAM/GPU/VRAM/storage/bandwidth/capability requirements;
- collateral requirements;
- job posting with escrow and deadline;
- deterministic offer/job IDs;
- matching and assignment;
- multi-provider assignment for replicated verification;
- provider result submission;
- settlement and unused-escrow refund;
- expiry;
- verification policies for:
  - deterministic replay;
  - replicated matching results;
  - challenge evidence;
  - zero-knowledge proof verification signal;
  - TEE attestation verification signal;
  - client approval;
  - hybrid evidence.

Heavy compute is provider-executed; validators verify settlement evidence and do not replay AI training, scientific simulations, rendering or other expensive workloads.

**Not production-complete**

- provider daemon/scheduler;
- input/output distribution protocol;
- on-chain object integration of market state transitions;
- actual collateral slashing/dispute arbitration;
- concrete ZK verifier integrations;
- concrete TEE attestation integrations;
- confidential-data key exchange;
- compute reputation and anti-collusion policy.

## Data availability

**Integrated foundation**

- chunk commitments;
- sample proof verification boundary;
- DA root in shard/global commitments;
- Citizen participation mode for bounded sampling.

**Not production-complete**

- production erasure code selection;
- reconstruction;
- sampling confidence parameters;
- withholding attacks in the fault lab;
- shard-aware dissemination;
- mobile bandwidth/storage measurements.

## Transport

**Integrated foundation**

- consensus transport, transaction relay and light-proof retrieval are separate logical interfaces;
- existing HTTP remains the reference/test transport boundary;
- the architecture permits libp2p/QUIC/WebTransport without changing consensus objects.

**Not production-complete**

- production libp2p/QUIC implementation;
- discovery/NAT traversal/mobile relay;
- shard-aware gossip;
- v2 fault-transport adapter covering the full existing Lab matrix.

## Performance gates

**Implemented**

- existing finalized-through-consensus v1 Lab remains mandatory;
- v2 state/proof microbenchmarks;
- v2 finalized batch benchmark with 32 proof-carrying transfers, 7 validators and 1/4/8/16 execution workers;
- the timed v2 path includes witness/signature verification, execution/state-root simulation, proposal/votes, quorum certificate and committed state transition;
- client workload setup/key generation/signing stays outside the timed consensus path, matching the Lab's canonical workload policy.

No numerical v2 TPS result from a shared CI runner is a production capacity claim.

## Activation gates

The clean break lets v2 replace prototype boundaries, but it does not remove the requirement to prove them. Before a public v2 devnet:

1. v2 multi-validator consensus must run through the fault-injection Lab, including partitions/restarts/conflicting evidence;
2. durable v2 state must survive crash/restart and longer stress runs;
3. Citizen verification must be exercised against a live v2 node from real Android/iOS reference devices;
4. one-shard finalized performance must be characterized on controlled hardware;
5. multi-shard mode must remain disabled until object placement, durable receipt anti-replay, recovery and 4/16-shard conformance pass;
6. contract execution must have a deterministic metered production runtime;
7. compute settlement must be consensus-state-backed before real value is escrowed;
8. genesis/checkpoint/operator upgrade procedures must be explicit.

The engineering rule remains:

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
```

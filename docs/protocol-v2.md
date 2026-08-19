# Zephyr Protocol v2 — Clean-Break Architecture

Status: **authoritative design target and implementation contract for the Zephyr v2 clean break**.

Protocol v1 remains the prototype/conformance reference. Protocol v2 intentionally does not preserve v1 transaction wire format, persisted-state layout, node-role coupling or account-state representation. We keep the security, consensus, recovery and benchmarking lessons while replacing boundaries that would otherwise become permanent scaling debt.

## Mission

Zephyr v2 has two equal scaling goals:

1. **Scale up when hardware is abundant.** The long-term target remains extremely high throughput, measured only as transactions finalized by validator consensus.
2. **Scale down without dying.** Payments, self-custody, verification and useful network participation must remain practical on commodity hardware, including a Citizen Node inside the Zephyr smartphone wallet.

Large datacenters may add capacity, but must not be structurally required for Zephyr to remain alive. The network may degrade in **throughput** when resources fall; it must never degrade in **safety**.

## Non-negotiable invariants

- Headline TPS means finalized transactions per second, never API ingress or mempool acceptance.
- No performance change is accepted if the Consensus & Performance Lab finds a safety or liveness regression.
- A Citizen Node must verify its own balances/payments, finalized headers, shard commitments, state proofs and sampled data without holding the full global state/history.
- Wallet-side work only helps network throughput when it produces evidence another participant can independently verify.
- Heavy compute (AI training, scientific workloads, rendering, etc.) is never replayed by every validator.
- Validators do not require GPUs or datacenter-class machines.
- Account, node and validator identities are separate.
- Consensus-critical encoding is canonical binary; JSON remains for RPC, diagnostics and human-facing configuration.
- Consensus-critical economics use integers/fixed point, not floating point.
- Sharding is protocol-native but activation is benchmark-driven. v2 starts safely with `shardCount = 1` unless evidence supports more.
- Adding shards must not linearly increase the minimum hardware requirement of a Citizen Node.
- Smart-contract execution is deterministic and metered.
- Heavy/private compute is provider-executed and blockchain-settled through commitments/proofs/attestations/replication/challenges as appropriate.

## Architecture

```text
                           ZEPHYR v2
                               |
                    +----------+----------+
                    |     GLOBAL FINALITY |
                    +----------+----------+
                               |
                    shard/object commitments
                               |
          +--------------------+--------------------+
          |                    |                    |
       Shard 0              Shard 1              Shard N
          |                    |                    |
     parallel exec        parallel exec        parallel exec
          |                    |                    |
          +--------------------+--------------------+
                               |
                        proofs / receipts
                               |
       +-----------------------+------------------------+
       |                       |                        |
   Validators              Full Nodes             Citizen Nodes
       |                       |                    smartphones
    consensus              full state                  |
    finality               proof serving          headers/proofs
    shard work             history                tx relay / DA
                                                    bounded cache
                                                    optional exec

                     +----------------------+
                     | Native Compute Market|
                     +----------------------+
                       off-chain CPU / GPU
                       on-chain settlement
```

Zephyr remains one network with one global notion of finality. Shards are execution/state partitions, not independent chains.

## 1. Genesis-derived network identity

A canonical v2 genesis contains protocol version, chain name, genesis time, initial/max shard count, native token, initial validator identities/voting power and initial allocations.

```text
networkId = H("zephyr/genesis/v2" || canonicalGenesis)
```

Genesis becomes the network identity and initial validator-set trust anchor. Nodes with different genesis data cannot silently claim the same network.

Reference: `internal/v2/genesis`.

## 2. Separate identities and roles

```text
Account identity    -> ownership and transaction authorization
Node identity       -> peer networking/authentication
Validator identity  -> consensus proposal/vote authority
```

A machine may expose one or more roles: Citizen Node, Full Node, Validator, Archive Node, Compute Provider. A full node does not need a validator key; a compute provider is not automatically a validator; a smartphone does not need permanent consensus availability.

References: `internal/v2/types`, `internal/v2/transport`.

## 3. Canonical binary protocol

Consensus objects use deterministic length-prefixed binary encoding with explicit versions, hard limits, big-endian integers and domain-separated hashing/signing. Consensus objects do not depend on JSON map/order behavior.

The first v2 proof-carrying transaction has a complete bounded binary marshal/parse round-trip.

Reference: `internal/v2/codec`.

## 4. Object state and native assets

The v2 execution primitive is a protocol object:

```text
Object
├── objectId
├── version
├── owner
├── kind
└── data
```

Initial kinds cover coins, token definitions, contracts, contract state, compute offers/jobs/assignments/results and system objects. Explicit object dependencies allow independent transactions to be scheduled in parallel.

For native payments the wallet still shows a normal balance; coin objects are an internal execution model. A transfer consumes coin objects and creates new ones while enforcing per-token conservation.

Token creation is protocol-native rather than requiring every token to reimplement a basic ERC-style ledger in bytecode. Token definitions include name, symbol, decimals, supply policy, mint authority, burnability and transferability. Custom logic can still be controlled by contracts later.

References: `internal/v2/object`, `internal/v2/assets`, `internal/v2/execution`.

## 5. Proof-native incremental state

The v1 performance profile identified full-state persistence/serialization as the first measured bottleneck. v2 therefore makes incremental, proof-oriented state a protocol requirement.

The reference engine is a 256-bit Sparse Merkle Tree over:

```text
objectId -> objectHash
```

It already provides deterministic roots, incremental path updates, inclusion proofs, absence proofs and compressed proofs that omit default siblings. The in-memory world-state backend defines the semantics; it is not the final durable database. Production storage will put the same state model over structured durable KV/WAL/checkpoint recovery.

Changing two objects must not require serializing the entire chain state.

References: `internal/v2/state`, `internal/v2/worldstate`.

## 6. Proof-Carrying Transactions

A v2 wallet does more than sign. It may package the exact state evidence needed to validate the objects it consumes.

```text
Wallet
  ├─ verifies finalized state root
  ├─ obtains object proofs
  ├─ declares inputs/outputs/operation
  ├─ signs canonical intent
  └─ attaches witnesses
             |
             v
      Proof-Carrying Transaction
             |
             v
Validator verifies signature + proofs + freshness/conflicts
             |
             v
        deterministic execution
```

The validator never trusts the wallet's claim; it independently verifies the cryptographic witness. This makes wallet work reusable while preserving consensus as the authority for uniqueness, ordering and finality.

The foundation includes P-256 low-S signing, genesis-derived network binding, state-root binding, explicit input object/version/hash, bounded witnesses, random salt and expiration height.

Reference: `internal/v2/tx`.

## 7. Parallel execution

Object inputs form an explicit conflict set. Transactions that touch disjoint objects can execute concurrently and then merge deterministically.

The first executor is deliberately simple and single-operation so we can establish correctness before a worker scheduler. The next execution milestone builds the deterministic conflict graph and benchmarks 1/4/8/16 workers on one shard before using sharding as a multiplier.

## 8. Shard-native, one-shard-first

The protocol contains shard IDs, shard commitments, a global commitment root, global headers and cross-shard receipt primitives from the beginning, but the first v2 chain may run one shard.

A global finalized header commits to shard roots and validator/data commitments. A Citizen Node can follow global headers while fetching only the shard/state proofs relevant to it.

Cross-shard movement follows:

```text
source shard consumes input
        |
        v
finalized receipt commitment
        |
        v
destination shard verifies receipt
        |
        v
creates destination output
```

Additional shards are activated only when 1/4/16-shard benchmarks show better finalized throughput/resource efficiency without worsening the Citizen Node minimum footprint. Receipt anti-replay and recovery must pass before multi-shard activation.

Reference: `internal/v2/sharding`.

## 9. Citizen Node inside Zephyr Wallet

A Citizen Node is not a passive RPC client. Depending on device conditions it can:

- verify finalized headers and quorum/finality evidence;
- verify object/state proofs for balances and payments;
- verify proof-carrying transactions;
- relay transactions through multiple peers;
- verify shard commitments;
- sample data availability;
- keep a bounded recent cache;
- optionally execute recent state while resources allow.

Participation is power-aware. Low battery can reduce the role to header verification; Wi-Fi/charging can enable sampling, cache serving and recent execution. Mobile availability is not assumed for consensus liveness.

Reference: `internal/v2/citizen`.

## 10. Data availability

Citizen Nodes should be able to contribute to availability without downloading all shard data. The foundation commits ordered chunks and verifies Merkle samples. It also defines an encoder boundary for a later erasure-code implementation.

Production DA still requires selection of an erasure code, reconstruction logic, sampling rules/confidence model, adversarial withholding tests and real mobile bandwidth/storage measurements. The current package is the proof contract, not a claim that production DA is finished.

Reference: `internal/v2/da`.

## 11. Deterministic smart contracts

Zephyr keeps smart contracts through a deterministic WebAssembly ABI. Rust is the first-class SDK target, not the only possible source language. Compatible toolchains may later include Zig, C/C++, TinyGo and others.

Consensus must standardize allowed WASM features, deterministic host calls, fuel/gas, memory/stack limits, state-access declarations, deterministic output/events and forbidden nondeterministic facilities.

The foundation validates a WASM v1 deployment envelope and defines the runtime interface. A production interpreter/metering engine is **not yet claimed complete**.

Reference: `internal/v2/contracts`.

## 12. Native distributed compute market

Heavy workloads remain outside consensus execution. The blockchain owns marketplace and settlement state; providers execute the work.

Native objects will cover provider offers, resource capabilities, price/collateral, jobs, assignments, escrow, result commitments, verification mode, challenges/disputes, settlement and reputation/slashing.

Target workloads include scientific/numerical computing, AI training/inference, video/3D rendering, compilation and data processing.

Verification is workload-specific. Supported modes are:

- deterministic re-execution;
- replicated execution;
- challenge-based verification;
- zero-knowledge/validity proof;
- TEE/remote attestation;
- client approval;
- hybrid combinations.

Confidential workloads keep private datasets/results off-chain where appropriate; Zephyr stores commitments, encrypted references, attestations/proofs and settlement state.

The foundation defines resource/offer/job/result data models and verification modes. Provider daemon, scheduling, escrow transitions, disputes and production TEE/ZK integrations are later milestones.

Reference: `internal/v2/compute`.

## 13. Transport boundaries

Consensus, transaction relay and light-proof retrieval are distinct logical capabilities. HTTP remains usable as a reference/test transport; future libp2p/QUIC/WebTransport implementations sit behind the same contracts. The Consensus & Performance Lab fault transport must be adapted to this boundary so correctness tests run independently of the production transport.

Reference: `internal/v2/transport`.

## 14. Performance has two axes

### Scale up — how fast can Zephyr finalize?

Measure finalized tx/s, p50/p95/p99 finality, validators, shards, batch size, CPU, allocations, memory, state-write bytes, network bytes/finalized tx, witness bytes, DA bytes and persisted state.

### Scale down — how small can a useful node be?

Measure Citizen Node resident memory, cache size, sync bandwidth, proof size/verification time, header verification, DA sample bytes/time, mobile CPU/battery duty cycle and startup/resume latency.

No phone or hardware budget becomes a production claim before measurement on real reference devices.

## 15. Security and consensus continuity

The existing Consensus & Performance Lab remains the gate. v2 must preserve or strengthen network/domain separation, low-S canonical signatures, quorum finality, pre-vote state-root verification, validator identity/signature validation, quorum-only recovery evidence, no single-peer snapshot trust, partition safety and recovery after quorum returns.

The new architecture changes **what consensus commits to**, not how much evidence is required for finality.

## 16. Clean-break compatibility policy

v2 intentionally breaks v1 compatibility for network identity, transaction wire/signing domain, object/state format, persistent backend, state-root calculation, node-role model, shard commitments and contract ABI.

It carries forward security invariants, consensus/fault lessons, performance methodology, recovery requirements, wallet self-custody and P-256 usability unless later benchmark/security evidence justifies changing it.

No public v2 network will silently accept v1 protocol objects.

## 17. Implementation sequence

### Foundation — implemented by this branch

- canonical binary codec and typed v2 identities;
- genesis-derived network ID;
- Sparse Merkle reference state and compressed proofs;
- object/coin model and native token definitions;
- signed proof-carrying transaction wire format;
- native transfer and token-creation reference executor;
- object world-state backend;
- shard routing, commitments/proofs, global header and receipt primitives;
- DA chunk/sample verification boundary;
- Citizen Node verifier and resource-aware participation policy;
- deterministic WASM deployment/runtime boundary;
- native compute-market data model and verification modes;
- separate consensus/transaction/light transport interfaces;
- unit tests and reference state/proof microbenchmarks.

### Integration

- add v2 genesis to the Consensus & Performance Lab;
- make current certified consensus finalize a v2 global header;
- run one-shard v2 transfers end to end;
- introduce durable structured KV persistence with crash/restart recovery;
- compare v1/v2 finalized TPS, finality, allocations and state-write cost.

### Mobile

- compact header/state-proof APIs;
- Citizen verifier inside Zephyr Wallet;
- bounded/resumable cache and multi-peer relay;
- real Android/iOS resource measurements;
- eliminate correctness dependence on any single RPC endpoint.

### Parallel execution

- deterministic conflict graph;
- parallel non-conflicting execution;
- deterministic merge;
- 1/4/8/16-worker benchmark on one shard.

### Sharding

- 4-shard Lab;
- cross-shard receipt consume and anti-replay;
- shard-aware gossip/recovery;
- 1/4/16-shard benchmarks;
- activate more shards only if evidence is positive.

### WASM

- select production deterministic runtime;
- validate imports/opcodes;
- define fuel schedule and memory limits;
- deploy/call state transitions;
- Rust SDK and deterministic conformance suite.

### Compute market

- offer/job/assignment/result state transitions;
- escrow/settlement and provider daemon;
- deterministic/replicated verification first;
- collateral/slashing/disputes;
- optional TEE/ZK backends and confidential workload flow.

### Data availability

- select erasure code and reconstruction;
- sampling/confidence rules;
- withholding/fault tests;
- mobile bandwidth/storage benchmarks;
- shard-aware DA propagation.

### Public devnet gate

No public v2 devnet until safety/liveness conformance, durable state recovery and Citizen Node correctness pass; one-shard performance is characterized; shard activation rules are defined; and genesis/checkpoint/operator/wallet upgrade procedures are explicit.

## Architectural north star

When simplicity, throughput and decentralization conflict, prefer designs that preserve independent verification and graceful hardware degradation, then recover throughput through parallelism, sharding and optional high-end providers.

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
```

Zephyr v2 is neither a "mobile blockchain" nor a "datacenter blockchain". It is intended to remain independently verifiable on small devices while scaling execution and data capacity when the network has more resources.

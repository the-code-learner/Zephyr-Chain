# Zephyr Protocol v2 — Clean-Break Architecture

Status: **authoritative design target and implementation contract for the Zephyr v2 clean break**.

Protocol v1 remains the prototype/conformance reference. Protocol v2 intentionally does not preserve v1 transaction wire format, persisted-state layout, node-role coupling or account-state representation. We keep the security, consensus, recovery and benchmarking lessons while replacing boundaries that would otherwise become permanent scaling debt.

For the executable status of each subsystem, see `docs/protocol-v2-implementation-status.md`. For committee trust/rotation, see `docs/protocol-v2-validator-trust.md`.

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
- A quorum certificate is valid only for the validator set committed by the already-trusted header chain.
- Cross-shard imports are asynchronous and cannot bypass source finality or durable anti-replay state.

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

The initial Citizen trust anchor is:

```text
TrustAnchor {
    NetworkID
    ValidatorRoot = Merkle(initial validator set)
}
```

## 2. Validator trust chain

Every validator set is committed by a canonical Merkle root over validator ID, P-256 public key and integer voting power.

Each `GlobalHeader` contains both:

```text
ValidatorRoot       // committee authorizing this header
NextValidatorRoot   // committee authorized for the next height
```

If `NextValidatorRoot` is zero, the committee remains unchanged. A committee cannot install itself: the current committee must first finalize the header committing the next root with the normal `2/3+` quorum.

Citizen wallets advance their local trust anchor only after verifying that QC. Cross-shard receipt import applies the same rule to historical source committees.

## 3. Canonical binary protocol

Consensus-critical objects use a bounded binary codec with explicit widths, length prefixes and versioned hash/signing domains. JSON is an RPC representation only.

Canonical binary objects include:

- transactions and witnesses;
- objects and asset definitions;
- shard commitments and receipts;
- GlobalHeader;
- proposals, votes and quorum certificates;
- Merkle/Sparse-Merkle proofs;
- genesis and validator commitments.

Protocol decoders reject trailing bytes, oversized fields and invalid versions.

## 4. Proof-oriented object state

Zephyr v2 replaces the v1 global account map as the consensus state primitive with versioned objects.

Core categories include:

```text
CoinObject
TokenDefinition
ContractObject
ComputeOffer / ComputeJob / ComputeResult state
SystemObject
```

User UX remains balance-oriented; wallets aggregate owned coin objects behind the scenes.

Objects have deterministic IDs. For multi-shard state, the object ID permanently encodes its assigned shard so changing the active shard count cannot silently relocate an existing object.

## 5. Sparse-Merkle state and proof-carrying transactions

Each shard maintains an incremental 256-bit Sparse Merkle state root. Inclusion and absence proofs are compressed by omitting default siblings.

A proof-carrying transaction identifies its pre-state root and carries authenticated object witnesses. A validator therefore verifies the same state evidence that the originating wallet could verify, rather than trusting wallet pre-validation.

```text
wallet
  -> verifies finalized state proof
  -> declares inputs / outputs / access set
  -> signs proof-carrying transaction
  -> network verifies witness + signature
  -> deterministic execution
```

Wallet-side work is useful because the evidence is independently reusable. Consensus remains responsible for uniqueness, ordering and finality.

## 6. Deterministic parallel execution

Transactions expose enough object dependencies to reject conflicting batches before concurrent execution. Independent transactions may execute in parallel; outputs merge in deterministic transaction order.

The execution gate rejects:

- duplicate transactions;
- duplicate/shared consumed objects in a parallel batch;
- mismatched pre-state roots;
- wrong-shard inputs;
- token conservation or fee violations;
- invalid witnesses/signatures.

Candidate execution computes a future root through a non-mutating state preview. Committed state changes only after a valid quorum certificate.

Performance work is profile-driven. The v2 benchmark measures 1/4/8/16 execution workers and reports finalized throughput/allocation behavior; adding goroutines is not considered scaling if proof/state allocation remains the bottleneck.

## 7. Sharding and cross-shard receipts

Sharding is native to the protocol but optional at activation. One shard is a fully valid Zephyr network.

Account routing determines where newly created account-owned outputs live. If an output targets another shard, the source shard does **not** write that object locally. Instead it creates a cross-shard receipt committed by the source shard's `ReceiptRoot`.

A destination import verifies:

1. source `GlobalHeader` and its authorized validator-set QC;
2. source shard commitment inclusion in the global root;
3. receipt inclusion in the source `ReceiptRoot`;
4. destination routing;
5. absence of the durable receipt marker.

The destination block then materializes both the destination object and a consensus-state anti-replay marker. Because normal transactions in that block are anchored to the destination pre-state root, an imported output becomes spendable only from a later block.

Sharding is enabled beyond one shard only after multi-shard conformance/recovery and throughput evidence demonstrate a net benefit.

## 8. GlobalHeader and finality

A v2 block-height finality commitment is a compact `GlobalHeader` over:

```text
version
network
height
parentHash
shardCommitmentRoot
validatorRoot
nextValidatorRoot
dataRoot
certificateHash
```

The consensus hash zeroes `certificateHash` to avoid circular signing. Proposal/vote signatures target this consensus hash. Once the quorum certificate forms, its hash is attached to the finalized header.

The header therefore ties together execution state, receipt availability, committee trust and global finality.

## 9. Citizen Node

The Zephyr Wallet includes a Citizen verifier rather than acting as a blind RPC client.

A proof bundle can carry:

```text
GlobalHeader
QuorumCertificate
ValidatorSet
ShardCommitment + proof
Object + SparseMerkle proof
```

The strict wallet verifier starts from a genesis/checkpoint trust anchor, independently reconstructs validator IDs/root/voting power, verifies low-S P-256 votes and `2/3+` quorum using exact integer arithmetic, then verifies shard/object proofs.

After a valid header, it may advance its local validator trust root to the QC-authorized `NextValidatorRoot`.

Participation remains resource-aware:

```text
battery low         -> headers/proofs only
wallet active       -> verify + relay
Wi-Fi               -> + DA sampling / bounded cache
Wi-Fi + charging    -> + opportunistic recent execution / serving
```

Mobile OS background availability is treated as opportunistic capacity, never as a consensus-liveness assumption.

## 10. Native tokens

Token definitions and coin objects are protocol-native so ordinary token transfers do not require executing general smart-contract bytecode.

Native asset state supports supply limits, mint authority, burn/transfer policy and deterministic token IDs. Custom smart contracts remain available when an asset needs logic beyond the native model.

## 11. Smart contracts

The contract protocol targets deterministic metered WASM through a versioned ABI. Rust is the first-class SDK target, not a protocol requirement; other modern languages may target the same deterministic WASM subset.

The runtime boundary already requires:

- bounded module/request/output sizes;
- deterministic imports/opcodes;
- fuel limits;
- memory/stack policy;
- declared object read/write access;
- no undeclared writes;
- bounded events.

The concrete production WASM engine and audited fuel schedule are selected only after cross-machine deterministic conformance and performance measurement.

## 12. Native distributed compute market

Heavy workloads are a native Zephyr market but run outside validator consensus execution.

Provider offers describe CPU, GPU, RAM, VRAM, storage, bandwidth, capabilities, verification modes, pricing and collateral. Jobs describe content-addressed inputs/workload, resource requirements, budget/escrow, deadline and verification policy.

Supported verification-policy primitives include:

- deterministic replay for suitable bounded jobs;
- replicated providers and matching result commitments;
- challenge evidence;
- ZK validity proof integration;
- TEE attestation integration;
- client approval;
- hybrid policies.

Validators settle compact evidence and payment state. They do not replay AI training, scientific simulations, rendering or other expensive compute merely to finalize payment.

Confidential workloads keep private datasets/results off-chain where appropriate; Zephyr stores commitments, encrypted references and settlement evidence.

## 13. Data availability

Shard/global commitments include data roots. Citizen Nodes can verify bounded samples rather than downloading global block data.

The checked-in foundation defines chunk/sample commitment verification; production erasure coding, reconstruction, confidence parameters and withholding fault tests remain activation gates.

The invariant is that increasing shard/data capacity must not linearly increase the minimum data requirement of every Citizen Node.

## 14. Transport boundaries

Consensus, transaction relay and light-proof retrieval are distinct capabilities. HTTP remains the reference/test transport. Production peer networking can move to libp2p/QUIC/WebTransport behind those interfaces without redefining consensus objects.

Shard-aware gossip, mobile relay/NAT traversal and the production transport implementation must pass the same conformance suite as the reference transport before public activation.

## 15. Durable state

The v2 durable backend uses an append-only network-bound WAL with checksums/sequence numbers/fsync plus atomic checkpoints and crash-tail recovery. This removes the v1 requirement to serialize the entire node state for every mutation.

It remains a reference durable backend while large-state benchmark evidence determines whether the final production backend should be a structured KV/LSM implementation.

## 16. Performance has two axes

### Scale up — how fast can Zephyr finalize?

Measure finalized tx/s, p50/p95/p99 finality, validators, shards, batch size, CPU, allocations, memory, state-write bytes, network bytes/finalized tx, witness bytes, DA bytes and persisted state.

### Scale down — how small can a useful node be?

Measure Citizen Node resident memory, cache size, sync bandwidth, proof size/verification time, header verification, DA sample bytes/time, mobile CPU/battery duty cycle and startup/resume latency.

No phone or hardware budget becomes a production claim before measurement on real reference devices.

## 17. Security and consensus continuity

The existing Consensus & Performance Lab remains a regression gate and v2 has its own seven-validator conformance suite.

V2 conformance covers certified happy path, 4/3 no-quorum partition, heal/recovery, 5/2 quorum/minority catch-up and conflicting proposal rejection. More restart/transport/Byzantine cases remain required before public devnet.

The architecture changes **what consensus commits to**, not how much evidence is required for finality.

## 18. Clean-break compatibility policy

V2 intentionally breaks v1 compatibility for network identity, transaction wire/signing domain, object/state format, persistent backend, state-root calculation, node-role model, shard commitments, validator trust chain and contract ABI.

It carries forward security invariants, consensus/fault lessons, performance methodology, recovery requirements, wallet self-custody and P-256 usability unless later benchmark/security evidence justifies changing them.

No public v2 network silently accepts v1 protocol objects or pre-transition experimental v2 wire objects.

## 19. Implementation sequence

### Executable foundation

Implemented on the v2 branch:

- canonical binary codec and typed identities;
- genesis-derived network/trust identity;
- validator-set roots and QC-backed committee transitions;
- Sparse-Merkle reference state and compressed proofs;
- proof-oriented object/coin model and native tokens;
- signed proof-carrying transaction wire format;
- deterministic parallel executor and non-mutating state preview;
- durable WAL/checkpoint state backend;
- v2 proposal/vote/QC and GlobalHeader finality path;
- permanent object shard placement;
- finalized cross-shard receipt import with durable anti-replay marker;
- Citizen light API and strict wallet cryptographic verifier;
- deterministic WASM metering/runtime boundary;
- native compute-market state-machine foundation;
- data-availability sample boundary;
- separate consensus/transaction/light transport interfaces;
- dedicated v2 seven-validator partition/conformance CI gate;
- finalized v2 batch benchmark.

### Next integration gates

- mount a live v2 runtime/genesis/light provider in the process-facing node path;
- persist runtime height/header/validator-transition metadata alongside shard state;
- expand v2 fault injection to restart, proposer death, Byzantine payloads, wrong-chain data and transport delay/reorder;
- profile/reduce proof/state allocation and compare controlled-hardware v1/v2 throughput;
- add shard-aware recovery/gossip and 4/16-shard conformance;
- implement trusted checkpoint history across long validator rotations;
- integrate a production deterministic WASM engine and Rust SDK;
- move compute-market transitions into consensus object execution and build provider daemon/escrow/dispute flows;
- select production erasure coding and implement DA reconstruction/withholding tests;
- connect libp2p/QUIC/mobile peer transport;
- embed Citizen lifecycle/cache/relay controls in the wallet UI/native shell;
- benchmark real Android/iOS devices and commodity validators.

### Public devnet gate

No public v2 devnet until safety/liveness conformance, durable state/runtime recovery and Citizen trust-chain correctness pass; one-shard performance is characterized; multi-shard activation rules are defined; contract execution is deterministic/metered; and genesis/checkpoint/operator/wallet upgrade procedures are explicit.

## Architectural north star

When simplicity, throughput and decentralization conflict, prefer designs that preserve independent verification and graceful hardware degradation, then recover throughput through parallelism, sharding and optional high-end providers.

```text
more hardware  -> more throughput
less hardware  -> less throughput
less hardware  -/-> weaker correctness
```

Zephyr v2 is neither a "mobile blockchain" nor a "datacenter blockchain". It is intended to remain independently verifiable on small devices while scaling execution and data capacity when the network has more resources.

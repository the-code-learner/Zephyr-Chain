# Zephyr Chain

Zephyr Chain is an open-source blockchain project exploring a clean-break Protocol v2 designed around two equal engineering goals:

1. **scale up** toward extremely high transaction throughput when hardware is abundant;
2. **scale down** so verification, payments, relay, data-availability participation and useful network activity remain practical on commodity hardware and eventually inside the Zephyr smartphone wallet.

Throughput may degrade when hardware is scarce. **Safety must not.**

The long-term mission is described in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). The executable v2 design is specified in [docs/protocol-v2.md](./docs/protocol-v2.md), and the exact implementation/non-claim boundary is tracked in [docs/protocol-v2-implementation-status.md](./docs/protocol-v2-implementation-status.md).

> Zephyr's aspirational performance target is **1,000,000 transactions per second finalized through consensus**. It is a benchmark target, not a claim about current capacity.

## Protocol v2 direction

Zephyr v2 is being built as a clean break rather than preserving experimental wire/state compatibility.

The architecture combines:

- genesis-derived network identity and canonical bounded binary consensus encoding;
- separate account, node and validator identities;
- proof-oriented object state backed by an incremental Sparse Merkle Tree;
- proof-carrying transactions and deterministic parallel execution;
- weighted validator consensus, quorum certificates and global finalized headers;
- shard-ready state/execution with finalized asynchronous cross-shard receipts;
- Citizen/light verification intended for resource-constrained and mobile participants;
- deterministic smart-contract execution with a production WASM/Rust path;
- native custom-token creation, fixed/capped/mintable supply policies, mint/burn and transferability rules;
- a native distributed-compute market for workloads such as scientific computing, AI, rendering and other provider-executed jobs;
- authenticated data-availability foundations and libp2p/QUIC networking foundations;
- an oracle-free economic measurement layer for ZPH, compute demand/supply and adaptive monetary-policy experiments.

The implementation rule is:

```text
measure first -> simulate second -> activate last
```

## What is implemented on the v2 branch

The current v2 development branch contains executable foundations and tests for:

- canonical v2 codec and genesis/network identity;
- P-256 proof-carrying transactions with low-S canonical signatures;
- object/coin state and Sparse-Merkle inclusion/absence proofs;
- durable WAL/checkpoint state persistence and non-mutating state simulation;
- deterministic parallel batch execution;
- v2 proposal/vote/QC consensus and validator-root transitions;
- cross-shard ZPH receipts with proof verification and durable anti-replay state;
- Citizen/light proof verification and wallet-side quorum/state-proof verification;
- custom tokens with fixed, capped and mintable policies plus native mint/burn;
- transferability policy enforcement without serializing independent token transfers on one hot policy object;
- deterministic metered reference smart-contract execution and contract deploy/call paths;
- compute offers, escrow, collateral, assignment, provider results, replicated-majority settlement and objective slashing foundations;
- standardized compute `WorkVector`/`WorkSpec` representation;
- finalized compute settlement receipts that can be reconstructed and verified from chain state;
- ZCPI compute-price measurement from successfully verified standardized work;
- ZCSI compute-scarcity measurement with independent capacity-reliability gates;
- age-weighted native-money velocity and canonical per-shard economic epoch metrics;
- ZAMP shadow monetary-policy evaluation centered near a long-run ~2% net-supply-growth target;
- finalized-block economic collection, cross-epoch compute backlog accounting and chained Merkle-authenticated shadow monetary state;
- automatic **shadow** economic epoch closure and insertion of the pending `MonetaryEpochState` into the next normal consensus candidate;
- Reed-Solomon data-availability reconstruction foundations;
- libp2p/QUIC transport foundations;
- v1 and v2 consensus/performance CI labs.

These are development/protocol foundations. They are not equivalent to a production public network.

## Citizen Node / mobile goal

A core Zephyr goal is to make the wallet a real network participant rather than a thin client that must trust a large RPC provider.

The intended Citizen Node can progressively perform:

```text
header + quorum verification
        -> account/object proof verification
        -> transaction relay
        -> data-availability sampling/cache
        -> opportunistic recent execution
```

Participation should adapt to battery, connectivity and device resources. Smartphones are not assumed to be always-on validators. Consensus liveness must remain possible on inexpensive commodity hardware even when mobile operating systems suspend background work.

Two benchmark axes are therefore first-class:

```text
How fast can Zephyr go?
How small can Zephyr run?
```

## Sharding and parallel execution

Zephyr v2 is shard-aware but does not assume that more shards are automatically better.

The safe activation value remains a single shard until controlled 4/16-shard tests demonstrate safety, recovery and useful scaling.

The intended model is:

```text
shard-local parallel execution
        -> shard state/data/receipt commitments
        -> global finalized commitment/QC
```

Cross-shard native payments use finalized one-time receipts. General synchronous atomic cross-shard smart-contract semantics are deliberately not a v0 requirement.

## Smart contracts

The repository includes a bounded deterministic reference runtime and contract deploy/call execution path. The production direction is deterministic metered **WASM**, with Rust-first tooling/SDK work planned around a stable ABI and cross-machine conformance tests.

Validators must never depend on nondeterministic clock, network, filesystem or host randomness during consensus-critical execution.

## Native distributed compute

Heavy compute is intentionally separated from deterministic validator execution.

Examples include:

- scientific/HPC workloads;
- AI inference or training;
- video/3D rendering;
- simulation and optimization;
- other provider-executed workloads whose result can be verified through an approved evidence policy.

The chain manages economic and verification state such as:

```text
job -> escrow -> assignment -> provider result -> verification -> settlement
```

Validators verify bounded settlement evidence rather than re-running arbitrarily expensive workloads.

Verification modes have explicit protocol boundaries for deterministic replication, replicated majority, challenge systems, ZK evidence, TEE evidence, client approval and future hybrids. Production ZK/TEE integrations are not yet claimed.

## Compute economics: ZCR, ZCPI and ZCSI

There is no honest single universal scalar that makes CPU, GPU tensor work, FP64 scientific work, memory, storage and network usage equivalent.

Zephyr therefore models compute as a normalized resource/work vector and only prices standardized workload classes whose definitions are committed by `WorkSpec`.

`ZCPI` measures the ZPH actually paid for finalized, verification-satisfied standardized work. It excludes provider-advertised offer prices and theoretical peak FLOPS.

`ZCSI` combines signals such as:

- escrow-backed standardized demand;
- verified standardized supply;
- opening/closing backlog;
- fulfilled work;
- compute utilization;
- reliable ZCPI trend.

Long jobs are accounted as stock-flow across epochs:

```text
opening backlog + new demand
=
fulfilled + expired + closing backlog
```

A numeric capacity value cannot make ZCSI trustworthy by itself. Compute supply has a separate reliability flag and remains monetarily inert until capacity can be derived from a consensus-reproducible benchmark/collateral/availability mechanism.

See [docs/compute-economics-v2.md](./docs/compute-economics-v2.md) and [docs/economic-runtime-v2.md](./docs/economic-runtime-v2.md).

## ZPH tokenomics — shadow mode

The v2 economic design does **not** assume a fixed maximum supply.

The research direction is an oracle-free burn/mint controller centered near approximately 2% long-run effective net supply growth, with bounded/rate-limited adjustments from on-chain signals such as:

- fee burn;
- staking/locked supply;
- protocol reserve;
- chain resource utilization;
- finalized operations;
- age-weighted money velocity;
- potentially reliable compute scarcity.

Compute feedback currently has three simulation modes:

```text
A — observe only
B — change suggested compute-reward routing only
C — B plus a narrow bounded shadow inflation correction
```

Mode B is the preferred first activation candidate if long-run devnet evidence supports it. **No current mode mints live ZPH.** Suggested issuance is stored only as shadow economic state for replay and analysis.

See [docs/tokenomics-v2.md](./docs/tokenomics-v2.md) and [docs/economic-state-v2.md](./docs/economic-state-v2.md).

## Consensus & Performance Lab

Zephyr defines throughput as transactions **finalized through validator consensus**, not HTTP ingress or mempool acceptance.

CI includes multi-validator conformance, partition/recovery stress, finalized-throughput sampling, signature verification baselines and v2 batch-scaling tests.

Shared GitHub runner numbers are development signals only and must not be presented as production capacity claims.

See [docs/performance-lab.md](./docs/performance-lab.md).

## Current non-claims

The repository must not currently be presented as having production-ready:

- live adaptive ZPH issuance or final monetary parameters;
- active validator/compute/reserve reward distribution;
- production gas/resource pricing;
- authenticated governance-controlled economic parameters;
- production benchmarked/collateralized compute-capacity registry;
- production ZK/TEE confidential-compute integrations;
- audited deterministic WASM engine and stable Rust SDK;
- mobile OS lifecycle/background integration with measured device budgets;
- production peer discovery/NAT/mobile relay/shard-aware gossip;
- dynamic resharding or public 4/16-shard activation;
- public-mainnet operational/security maturity.

The authoritative detailed list is [docs/protocol-v2-implementation-status.md](./docs/protocol-v2-implementation-status.md).

## Repository layout

```text
apps/wallet/              Vue reference wallet / Citizen verification work
cmd/node/                 legacy/current node entrypoint
cmd/compute-provider/     compute-provider tooling foundation
cmd/zephyr-econ-sim/      deterministic economic replay simulator
internal/v2/              clean-break Protocol v2 implementation
internal/v2/economics/    ZCPI, ZCSI, ZAMP, fee and epoch accounting
internal/v2/compute/      native distributed-compute market/state
internal/v2/node/         v2 candidate/finality runtime
internal/v2/worldstate/   proof-oriented in-memory/durable state
internal/v2/network/      v2 networking foundations
mobile/                   Citizen/mobile policy foundations
docs/                     protocol, economics, performance and roadmap docs
```

The pre-v2 packages remain in the repository because the existing Consensus & Performance Lab and hardening work are still valuable regression gates while v2 is developed.

## Development checks

From the repository root:

```bash
gofmt -w ./internal/v2 ./mobile ./cmd/zephyr-econ-sim
go vet ./...
go test ./...
```

Wallet:

```bash
cd apps/wallet
npm ci
npm run build
```

The GitHub workflows are the authoritative shared gate for the v1 Lab, V2 Lab, wallet build and dependency lock.

## Documentation

Start with:

- [Protocol v2 architecture](./docs/protocol-v2.md)
- [Protocol v2 implementation status](./docs/protocol-v2-implementation-status.md)
- [Validator trust model](./docs/protocol-v2-validator-trust.md)
- [Tokenomics v2](./docs/tokenomics-v2.md)
- [Compute economics v2](./docs/compute-economics-v2.md)
- [Economic state v2](./docs/economic-state-v2.md)
- [Finalized economic runtime](./docs/economic-runtime-v2.md)
- [Consensus & Performance Lab](./docs/performance-lab.md)
- [Roadmap](./docs/roadmap.md)
- [Project manifesto](./Zaphyr-chain_manifesto.md)

## License

Zephyr Chain is licensed under the **Apache License, Version 2.0**. See [LICENSE](./LICENSE).

The redistribution attribution notice is provided in [NOTICE](./NOTICE) and must be preserved as required by the Apache License 2.0.

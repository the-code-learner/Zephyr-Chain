# Zephyr Chain Protocol v2 — Economics & Tokenomics

> Status: Protocol v2 research and implementation. Economic mechanisms described here are **not live monetary policy** unless explicitly marked as activated. The activation discipline is **measure first → simulate second → activate last**.

## Design goals

Zephyr Chain Protocol v2 treats tokenomics as a measurable control system rather than a fixed-emission schedule. The monetary design is intended to align purchasing power, productive network capacity, state costs, compute-market incentives and governance without putting heavy economic computation in the consensus hot path.

The protocol does **not** target a fixed +2% annual supply increase. Instead, the long-term monetary objective is approximately **-2% annual purchasing-power drift for ZPH against a Zephyr-native basket**, represented as an annual ZPPI target factor of **1.020408163** (Q9: `1_020_408_163`). Supply is therefore endogenous: future issuance or contraction, if activated, must respond to measured conditions and bounded governance rules rather than a constant inflation target.

## Core economic indexes

### ZPPI — Zephyr Purchasing Power Index

ZPPI is the slow monetary-direction signal. Protocol v2 currently models a deterministic, versioned basket covering compute, data availability and storage with fixed-point price relatives, explicit reference values and weights, coverage/reliability checks, EWMA smoothing and bounded movement.

Purchasing power is represented as `PP = 1 / ZPPI`. ZPPI is designed to inform future monetary direction; it is not, by itself, an instruction to mint or burn supply.

### ZCSI — Zephyr Compute Supply Index

ZCSI is primarily a compute incentive and capacity-routing signal. It is intended to reflect **verified delivered work**, not theoretical FLOPS, advertised capacity or unverified provider claims.

### ZCU — Zephyr Compute Unit reference

The v2 implementation includes a dynamic ZCU reference derived from delivered-slot, availability, success and confidence observations using deterministic weighted aggregation, EWMA smoothing, bounded movement and fail-closed coverage rules.

Only verified/finalized compute-market evidence should feed production economic telemetry.

## Compute market separation

The compute market is asynchronous and keeps heavy work off-chain while using on-chain escrow, verification and settlement. Provider payment is separate from blockchain gas.

`WorkSpec` and `WorkRegistry` are versioned/canonical protocol objects. Only `VerifiedWork` finalized through consensus may feed trusted economic telemetry such as ZCPI/ZCSI/ZCU/ZPPI. Production benchmark authentication, governed registry activation and ZK/TEE verifier infrastructure remain future gates.

## Idle capital and productive-use telemetry

Protocol v2 includes a prospective `IdleCapitalTracker` as **shadow telemetry**. It tracks capital lineage rather than wallet counts so that ordinary transfers, self-transfers, change outputs, split/merge patterns or address fragmentation cannot fabricate economic activity or reset dormancy.

Key properties of the tracker design and current P1 implementation:

- capital lineage is amount-conserving and deterministic;
- `IdleSinceHeight` survives ordinary movement, split/merge and cross-shard transfer;
- productive use is an explicit verified hook, separate from generic transfers or contract calls;
- cross-shard transfer identity is exactly the consensus-canonical `CrossShardReceipt.Hash()`;
- destination object identity comes from the protocol-defined `CrossShardReceipt.DestinationObject()`;
- finalized transaction/result/witness evidence is used to derive native-capital movement;
- previously unseen historical capital is only prospectively bootstrapped when consensus-stamped age is available, with incomplete coverage exposed rather than hidden;
- the tracker is opt-in inside the finalized `EpochCollector` preview/apply path;
- runtime-produced canonical export receipts are supplied to economic observations only after the source state root is known;
- collector checkpoint version 2 carries idle-capital state and can still restore version 1 collector checkpoints;
- checkpoints are canonical, bounded and fail closed on malformed state;
- replay and invalid/non-conserving transitions are rejected atomically.

The tracker does **not** currently impose an idle levy and does not mutate supply. Productive-coverage updates are not granted for generic transfers or arbitrary contract calls; future hooks must be tied to narrow, verifiable productive events.

## State carrying cost

Protocol v2 includes deterministic measurement of state carrying cost and fragmentation pressure. The purpose is to quantify long-lived state/resource externalities before proposing any economic charge. Measurement and public simulation precede any activation decision.

## Monetary activation gates

No idle levy, monetary minting, burn policy or reward-routing change becomes live merely because telemetry exists. Activation requires explicit protocol/governance gates, sufficient observation windows, deterministic simulations and safety review.

The required sequence is:

1. **Measure** — collect deterministic, finalized evidence with explicit coverage/reliability.
2. **Simulate** — run public scenarios over long windows, including adversarial and fragmented-capital cases.
3. **Activate** — only through an explicit bounded protocol/governance decision after the evidence is sufficient.

## Governance direction

Protocol v2 is designed for explicit versioning, bounded parameters and delayed/timelocked activation. Later roadmap stages include contribution/citizen evidence, VRF committees and governance objects. These remain developmental and must not be interpreted as production-ready governance claims.

## Lending roadmap

The economics roadmap includes bilateral native lending, including the ability to represent negative rates where economically justified, followed by device credentials and additional economic indexes. These features belong to later Protocol v2 phases and are not yet live.

## Consensus incentives and benchmark discipline

Economic design must not weaken safety, liveness or verifiability. Performance claims are valid only for **finalized-through-consensus** transactions. Execution-only throughput is not chain TPS.

The v2 benchmark framework is being extended to report:

- finalized TPS over a measured window;
- warm-up finalization separately from measured finalization;
- p50/p95/p99 finality latency;
- shard/validator counts, transactions per block, block cadence and cross-shard workload ratio;
- commit SHA and machine/environment metadata;
- rejected transactions and errors;
- explicit invalidation when safety or liveness failures occur;
- machine-readable JSON output.

The **1,000,000 TPS finalized-through-consensus** figure is a long-term aspirational north-star, **not a benchmark already achieved and not a production-capacity claim**.

## Current Protocol v2 implementation status

Implemented in the current P1 economics branch:

- deterministic dynamic ZCU reference;
- ZPPI compute/DA/storage basket with Q9 arithmetic and reliability/coverage gates;
- capital-lot lineage primitives, dormancy histograms and productive-coverage measurement;
- deterministic state carrying-cost estimator;
- adversarial fragmentation and lineage edge-case coverage;
- prospective IdleCapitalTracker with canonical local/cross-shard identity and finalized-evidence derivation;
- opt-in finalized `EpochCollector` integration with atomic preview/apply semantics;
- versioned collector checkpoint/restart support for idle-capital state;
- finalized-through-consensus benchmark report core and tests for V2 Lab integration.

Still gated / not production-ready:

- live monetary mint/burn or idle levy;
- final reward-routing parameters;
- automatic productive-coverage credit beyond explicitly approved and verifiable hooks;
- long-horizon public economic simulations and manipulation-resistance evidence;
- production benchmark/capacity authentication;
- wiring the report core to real V2 Lab finality/QC sampling and publishing new measured chain-TPS results;
- production ZK/TEE verification infrastructure;
- governed production WorkRegistry;
- production Rust/WASM contract stack;
- any claim that the 1M TPS north-star has been reached.

## Source of economic design

This documentation reflects **Zephyr Chain — Protocol Economics, Monetary Design, Governance, Lending and Consensus Incentives, working whitepaper v0.1 (19 Aug 2026)** together with the current Protocol v2 P1 implementation state.

# Zephyr Chain Roadmap

## Goal

Build Zephyr into a production-capable network with:

- validator-driven consensus instead of single-node block production
- deterministic Rust-first WASM execution for on-chain logic
- a separate confidential compute market for private or heavy workloads
- operator tooling, observability, and recovery paths strong enough for public testnet and mainnet operations

## Current Status

As of this iteration, the repository has:

- durable ledger state for accounts, mempool, committed blocks, snapshots, validator snapshots, proposals, votes, and quorum certificates
- an explicit peer transport abstraction with the current implementation running over HTTP devnet replication
- durable validator-set snapshots with versioning and restart-safe persistence
- derived consensus metadata including total voting power, quorum target, and next scheduled proposer
- signed proposal and vote messages validated with Zephyr addresses plus P-256 signatures
- durable quorum certificates built when vote power crosses the `>2/3` threshold
- deterministic next-block template generation for the current mempool and tip state
- optional certificate-gated local block commit and remote block import behind `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`
- a browser wallet that can create accounts, sign locally, and submit transactions

What it still does not have:

- authenticated validator networking
- proposal dissemination that lets validators verify more than a block hash alone
- round timeout and re-proposal handling
- restart-safe round recovery and operator evidence tooling
- on-chain staking/governance-driven validator updates
- WASM contracts, fee metering, or compute markets
- production operations tooling

## Historical Roadmap Review

The earlier broad roadmap from commit `c00d110` was useful as a starting point, but it is no longer the right production plan for this repository.

Still applicable from that early plan:

- lightweight wallet-first UX
- strong emphasis on scalability and developer reach
- staged delivery instead of pretending the full protocol arrives at once

Now superseded by the current manifesto and code direction:

- `Tendermint` integration is not the current path; the codebase is building its own consensus stack incrementally in Go
- `Solidity` and broad `EVM compatibility` are no longer the target; the current execution plan is deterministic WASM with Rust-first tooling
- `Sharding` and `DAG exploration` are not the immediate next milestones; the urgent gap is validator agreement, authenticated networking, and production hardening on a single-chain execution path first

In short: the old roadmap is historically informative, but the production roadmap below is the one that still applies.

## Near-Term Roadmap

These steps are the most detailed because they are closest to implementation and carry the most design risk.

### Phase 1: Consensus Foundation

Status:

- DPoS ranking exists
- validator snapshots are durable
- proposer scheduling is visible and can be enforced locally
- signed proposals and validator votes are durable artifacts
- quorum certificates are derived and persisted when vote power crosses quorum
- nodes can optionally require a matching proposal and certificate before local block commit or remote block import
- the current proposal/certificate path is still an operator-driven dev flow, not a full round engine

Next steps:

1. Bind validator identity to network identity so a node can prove which validator it represents.
2. Carry enough proposal payload or block-template commitment data for validators to verify what they are certifying, not just the final hash.
3. Add round timeout handling, proposer rotation within a round sequence, and re-proposal flows.
4. Persist round state, evidence, and operator-facing recovery data for restart-safe recovery.
5. Add deterministic integration tests for certified happy path, missing certificate, conflicting proposals, restart during a round, and recovery from partial quorum.

Exit criteria:

- a block is considered committed because validators agreed on a well-defined proposal, not because one local node wrote it first
- nodes can restart and resume without silently losing consensus-critical state
- operators can distinguish proposal failure, quorum failure, and transport failure from observable state

### Phase 2: Networking And State Sync Hardening

Status:

- a transport abstraction now exists
- the active transport is still static peer URLs over HTTP
- certified block checks can already run over that abstraction
- behind nodes can fetch blocks or restore full snapshots
- sync is convenient, but not trust-minimized or production-safe

Next steps:

1. Replace static peer configuration with authenticated peer discovery over libp2p.
2. Bind transport identity, validator identity, and peer admission rules together.
3. Add transport-level authentication, duplicate suppression, and replay-safe message handling.
4. Separate dev snapshot restore from production state sync so operators can choose explicit trust models.
5. Add checkpointing, snapshot metadata, and verification hooks for state transfer.
6. Add structured logs, metrics, and health surfaces for validator, sync, and transport operations.

Exit criteria:

- nodes can join, recover, and observe the network without relying on ad hoc static replication alone
- operators can reason about sync health, peer identity, and consensus message flow in production

### Phase 3: Staking, Validator Lifecycle, And Governance Control Plane

Status:

- validator sets are currently injected through `POST /v1/election`
- that is good for development, but not a production control plane

Next steps:

1. Move validator-set changes behind explicit state transitions instead of ad hoc API writes.
2. Add staking, delegation, validator registration, and validator rotation flows.
3. Add missed-block accounting, evidence handling, and slashing rules.
4. Add governance or protocol-defined authority over election parameters.

Exit criteria:

- validator membership and voting power come from chain state
- operators and delegators can reason about validator lifecycle without out-of-band coordination

## Mid-Term Roadmap

These steps are important, but they depend on the consensus foundation above being stable first.

### Phase 4: Deterministic WASM Execution

Goals:

- deterministic on-chain WASM runtime
- Rust-first contract tooling
- explicit gas and fee accounting for instructions, memory, storage, and emitted messages
- execution receipts and error surfaces suitable for operators and developers

### Phase 5: Node And Operator Hardening

Goals:

- configuration validation and safer defaults
- rate limits, resource limits, and anti-abuse controls
- write-ahead logging and crash recovery for consensus-critical paths
- metrics, tracing, dashboards, and incident-friendly logs
- release automation, reproducible builds, and upgrade runbooks

## Long-Term Roadmap

These phases should stay broad until the lower-level protocol is stable.

### Phase 6: Confidential Compute Marketplace

Broad direction:

- package off-chain jobs separately from on-chain contracts
- add worker registry, attestation verification, bidding, escrow, settlement, and slashing
- settle payments on-chain with the Zephyr native token
- keep privacy-oriented execution separate from consensus-critical state transitions

### Phase 7: Public Testnet To Mainnet Readiness

Broad direction:

- public testnet launch criteria
- validator onboarding and incident runbooks
- upgrade strategy and rollback planning
- monitoring, alerts, and SLOs for operators
- staged path from devnet to public testnet to mainnet

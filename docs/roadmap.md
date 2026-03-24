# Zephyr Chain Roadmap

## Goal

Build Zephyr into a production-capable network with:

- validator-driven consensus instead of single-node block production
- deterministic Rust-first WASM execution for on-chain logic
- a separate confidential compute market for private or heavy workloads
- operator tooling, observability, and recovery paths strong enough for public testnet and mainnet operations

## Current Status

As of this iteration, the repository has:

- durable ledger state for accounts, mempool, committed blocks, snapshots, validator snapshots, active round state, proposals, votes, and quorum certificates
- an explicit peer transport abstraction with the current implementation running over HTTP devnet replication
- durable validator-set snapshots with versioning and restart-safe persistence
- derived consensus metadata including total voting power, quorum target, active round, round start time, and the currently scheduled proposer
- signed proposal and vote messages validated with Zephyr addresses plus P-256 signatures
- proposals that commit to deterministic template fields: `previousHash`, `producedAt`, ordered `transactionIds`, the full `transactions` body, and the derived `blockHash`
- optional certificate-gated local block commit and remote block import behind `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`
- certificate-gated local commit that can replay the stored proposal body instead of depending on the local mempool alone
- signed validator transport-identity proofs derived from `ZEPHYR_VALIDATOR_PRIVATE_KEY` and surfaced through status plus peer verification views
- optional strict peer admission behind `ZEPHYR_REQUIRE_PEER_IDENTITY`
- optional peer-to-validator binding behind `ZEPHYR_PEER_VALIDATORS`
- admitted-peer gating for background sync and outgoing replication on the current HTTP transport
- a first timeout-driven automation slice behind `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION`, `ZEPHYR_CONSENSUS_INTERVAL`, and `ZEPHYR_CONSENSUS_ROUND_TIMEOUT`
- scheduled proposer self-proposal, active-validator auto-vote, timeout-driven round advance, proposer rotation, stored-candidate reproposal, and proposer-side certified auto-commit on the current devnet path
- in-order automated proposal and vote dissemination on the validator path to avoid the vote-before-proposal race
- operator-facing `roundEvidence` on status, consensus, and block-template endpoints, including leading tally, quorum remaining, pending replay rounds, and warning flags
- per-height `roundHistory` on status, consensus, and block-template endpoints so operators can inspect prior and active rounds for the pending height side by side
- `blockReadiness` on status, consensus, and block-template endpoints so operators can see whether the local template matches stored proposals and certificates for the pending height
- latest local proposal and latest local vote rebroadcast for the pending height during delayed peer recovery
- a bounded local consensus-action WAL with pending/completed status, replay-attempt metadata, and restart-safe persistence
- bounded recent consensus diagnostics for rejected proposal, vote, commit, and import paths, including explicit `template_mismatch`
- a browser wallet that can create accounts, sign locally, and submit transactions

What it still does not have:

- authenticated peer discovery and replay-safe transport over libp2p
- broader recovery and transport-facing incident evidence beyond the current local round history, block readiness, warnings, and rejection history
- broader recovery coverage beyond the current local proposal/vote WAL path
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
- proposals now commit to concrete template fields, not only a loose block hash
- proposals now carry the full candidate transaction body, not only IDs
- quorum certificates are derived and persisted when vote power crosses quorum
- nodes can optionally require a matching proposal and certificate before local block commit or remote block import
- certificate-gated local commit can replay the stored proposal body without needing the same candidate in the local mempool
- validator nodes can now prove which validator they represent over the current transport and nodes can enforce that proof plus per-peer validator binding when configured
- active round height, round number, and round start time are now durable consensus state
- valid higher-round proposals and votes can move a node onto the newer round instead of being rejected just because the local timer had not fired yet
- a first timeout-driven engine now exists: the scheduled proposer can self-propose, active validators can auto-vote, timeout can rotate the proposer, the next proposer can reuse the latest stored candidate body, and the proposer can auto-commit after quorum when certificate enforcement is enabled
- proposal and vote broadcasts on the automation path are now sent in-order to avoid vote-before-proposal races on the happy path
- the current automation path now has delayed-link proposal and vote recovery, richer round evidence, per-height round history, block readiness, bounded rejection diagnostics, and a restart-safe local proposal/vote WAL, but it still lacks broader recovery coverage and deeper peer-import evidence

Next steps:

1. Extend the new `blockReadiness`, `roundHistory`, `roundEvidence`, and `diagnostics` surfaces into explicit peer-import and broader recovery diagnosis flows.
2. Extend the current local proposal and vote WAL into broader consensus recovery coverage where more in-flight actions can resume safely after restart.
3. Add deterministic multi-node integration tests for certified happy path, conflicting proposals, timeout and re-proposal, restart during a round, rejection diagnostics, and recovery from partial quorum.
4. Keep tightening the transport-backed consensus loop so proposal and vote recovery remain correct when peers reconnect after advancing rounds.
5. Carry the same operator evidence into structured logs and metrics once the transport and recovery paths are more complete.

Exit criteria:

- a block is considered committed because validators agreed on a well-defined proposal, not because one local node wrote it first
- nodes can restart and resume without silently losing consensus-critical state
- operators can distinguish proposal failure, quorum failure, template mismatch, timeout, and transport failure from observable state

### Phase 2: Networking And State Sync Hardening

Status:

- a transport abstraction now exists
- the active transport is still static peer URLs over HTTP
- validator nodes can attach signed identity proofs to replicated requests and expose the same proof through status
- peer views can verify that proof, enforce strict peer admission, and pin configured peers to expected validator identities
- admitted-peer policy already gates current HTTP sync and replication behavior
- proposal, vote, and certified-block replication already ride over that abstraction
- the timeout-driven automation slice already uses that transport for proposal and vote dissemination
- behind nodes can fetch blocks or restore full snapshots
- sync is convenient, but not trust-minimized or production-safe

Next steps:

1. Replace static peer configuration with authenticated peer discovery over libp2p while preserving validator-binding semantics.
2. Add transport-level duplicate suppression, replay-safe message handling, and explicit message identifiers for consensus artifacts.
3. Separate dev snapshot restore from production state sync so operators can choose explicit trust models.
4. Add checkpointing, snapshot metadata, and verification hooks for state transfer.
5. Add structured logs, metrics, and health surfaces for validator, sync, admission, transport, and automation operations.

Exit criteria:

- nodes can join, recover, and observe the network without relying on ad hoc static replication alone
- operators can reason about sync health, peer identity, admission policy, and consensus message flow in production

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





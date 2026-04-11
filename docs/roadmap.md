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
- explicit pending `block_import` recovery actions plus durable `snapshot_restore` history for peer-import repair and snapshot catch-up
- peer views that now expose `syncState`, `heightDelta`, last import failure, last snapshot-restore metadata, last replication-failure metadata, durable per-peer `recentIncidents` history, derived incident counters per configured peer, and restart-safe backfill of that telemetry from retained incidents
- status, consensus, and block-template endpoints that now expose durable `peerSyncHistory` plus derived `peerSyncSummary` so operators can correlate recent peer incidents across the node and see affected-peer totals by dominant failure state, reason, and error code
- a machine-readable `GET /v1/metrics` surface that rolls up consensus-action counts, rejection-diagnostic buckets, durable peer-sync summary state by peer, state, reason, and error code, live peer runtime counts by sync state, committed-chain throughput windows for recent TPS baselining, and a typed settlement queue-drain view with lag plus thresholds
- optional structured JSON event logs for consensus diagnostics, peer-sync incidents, and snapshot-restore recovery behind `ZEPHYR_ENABLE_STRUCTURED_LOGS`
- an operator-facing `GET /v1/health` readiness surface that derives pass, warn, and fail checks from validator-set availability, recovery backlog, consensus warnings, settlement queue-drain lag under automatic block production, peer runtime, and recent diagnostics, with settlement detail now carrying the current worst-case drain forecast
- a Prometheus-style `GET /metrics` export adapter that projects the same readiness, consensus, diagnostic, recovery, peer, and chain-throughput signals into scrape-friendly text metrics, including per-peer retained incident counts, latest observation timestamps, recent TPS gauges, and settlement queue-drain lag, threshold, utilization-ratio, warn-normalized drain-estimate-pressure, backlog-drain-estimate, and explicit max drain gauges
- an operator-facing `GET /v1/alerts` surface that turns the current readiness, recovery, diagnostics, throughput, and peer-sync state into derived warning and critical alerts, including settlement-throughput reduced or stalled warnings for queued transaction drain with peak drain-forecast detail plus targeted peer import, peer admission, peer replication, and peer snapshot-restore warnings from retained peer incidents
- an operator-facing `GET /v1/slo` surface that projects those same signals into SLO-oriented objective states for readiness, consensus continuity, peer-sync continuity, and settlement throughput, with settlement objective detail preserving the peak drain forecast context from the lower layers
- recommended alert-rule bundle exports through JSON `GET /v1/alert-rules` and Prometheus-oriented `GET /v1/alert-rules/prometheus`, now including settlement-throughput at-risk and stalled rules for automatic block production plus a targeted peer snapshot-restore rule for peer-repair incidents
- recommended recording-rule bundle exports through JSON `GET /v1/recording-rules` and Prometheus-oriented `GET /v1/recording-rules/prometheus`, now including settlement-throughput state rollups plus normalized queue-drain utilization, projected queue-drain pressure, max projected queue-drain pressure, queue-drain estimate, and max queue-drain estimate rollups for queue-drain dashboards, peer-sync rollups for per-peer incident pressure, horizon-aware incident pressure, snapshot-repair pressure, and snapshot-repair-by-reason pressure, canonical `1m`, `5m`, and `15m` TPS rollups for the overview dashboard, and runtime-aware disabled reasons when settlement monitoring or peer sync is not applicable
- recommended dashboard bundle exports through JSON `GET /v1/dashboards` and Grafana-oriented `GET /v1/dashboards/grafana`, including overview throughput, settlement-state, raw queue-drain-lag, queue-drain-utilization, queue-drain-pressure, queue-drain-estimate, and worst-case queue-drain-time panels plus peer incident drill-down, peer incident horizon comparison, and snapshot-repair-pressure panels, with runtime-aware disabled reasons in JSON and enabled-only Grafana export
- a bounded local consensus-action WAL with pending/completed status, replay-attempt metadata, restart-safe persistence, explicit proposer-side `block_commit` history, import-recovery plus snapshot-restore history, and durable peer-sync incident history
- bounded recent consensus diagnostics for rejected proposal, vote, commit, and import paths, including explicit `template_mismatch` and peer-sync import failures
- a browser wallet that can create accounts, sign locally, and submit transactions

What it still does not have:

- authenticated peer discovery and replay-safe transport over libp2p
- broader recovery, richer dashboard packages beyond the current dashboard bundle and Grafana export, broader transport-facing incident evidence beyond the current replication-blocked peer incidents, and stronger historical fidelity beyond the current bounded peer-summary horizons (`5m`, `15m`, `1h`, `6h`, `24h`) layered on top of local round history, block readiness, warnings, durable peer-sync history, JSON metrics, Prometheus `GET /metrics`, `GET /v1/health`, `GET /v1/alerts`, `GET /v1/slo`, `GET /v1/alert-rules`, `GET /v1/alert-rules/prometheus`, `GET /v1/recording-rules`, `GET /v1/recording-rules/prometheus`, `GET /v1/dashboards`, `GET /v1/dashboards/grafana`, structured event logs, import backlog, snapshot-restore history, and rejection history with only bounded retention so far
- broader recovery coverage beyond the current local proposal/vote/block-commit history plus peer-import and snapshot-recovery path
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
- the current automation path now has delayed-link proposal and vote recovery, richer round evidence, per-height round history, block readiness, import-aware recovery state, durable peer-sync history, derived peer-sync summary, bounded rejection diagnostics, a machine-readable `GET /v1/metrics` surface with settlement alert metadata, normalized queue-drain utilization ratios, recent backlog-drain estimates, per-estimate warn utilization ratios, an explicit peak drain-estimate summary, and fixed peer-summary horizons for `5m`, `15m`, `1h`, `6h`, and `24h`, Prometheus `GET /metrics` including explicit max drain-estimate gauges plus peer horizon gauges, operator-facing `GET /v1/health` including settlement queue-drain checks and peer horizon detail, derived `GET /v1/alerts` with settlement-throughput plus peer import, admission, replication, the aggregate snapshot-restore alert, and repair-path-specific snapshot-restore diagnostics, derived `GET /v1/slo`, recommended `GET /v1/alert-rules` including repair-path snapshot-restore rules, exported `GET /v1/alert-rules/prometheus`, recommended `GET /v1/recording-rules` including canonical settlement drain-estimate, max drain-estimate, projected-pressure, and max projected-pressure rollups plus peer incident pressure by horizon, peer snapshot-restore pressure, and peer snapshot-restore pressure by reason, exported `GET /v1/recording-rules/prometheus`, recommended `GET /v1/dashboards` with rule-backed estimated queue-drain pressure, worst-case projected-pressure, worst-case drain-time, time panels, peer incident pressure horizons, peer snapshot-restore pressure, and peer snapshot-restore reasons, exported `GET /v1/dashboards/grafana`, structured JSON event logs, and a restart-safe local proposal, vote, and certified block-commit history plus snapshot-repair history, but it still lacks broader recovery coverage and stronger history than bounded retained-window summaries

Next steps:

1. Extend the new `blockReadiness`, `roundHistory`, `roundEvidence`, `recovery`, `diagnostics`, `peerSyncHistory`, `peerSyncSummary`, per-peer `recentIncidents`, `GET /v1/health`, `GET /v1/alerts`, `GET /v1/slo`, `GET /v1/alert-rules`, `GET /v1/alert-rules/prometheus`, `GET /v1/recording-rules`, `GET /v1/recording-rules/prometheus`, `GET /v1/dashboards`, and `GET /v1/dashboards/grafana` into deeper peer-import, divergence, longer-horizon multi-peer diagnosis, and production incident flows.
   Progress now: targeted peer snapshot-restore evidence is exposed end to end through the aggregate `peer_snapshot_restored` alert, the repair-path-specific alert codes `peer_snapshot_restore_divergence`, `peer_snapshot_restore_import_repair`, and `peer_snapshot_restore_fetch_fallback`, `ZephyrPeerSnapshotRestore`, the repair-path-specific rule exports `ZephyrPeerSnapshotRestoreDivergence`, `ZephyrPeerSnapshotRestoreImportRepair`, and `ZephyrPeerSnapshotRestoreFetchFallback`, the canonical rollups `zephyr:peer_sync:snapshot_restore_pressure`, `zephyr:peer_sync:snapshot_restore_pressure_by_reason`, `zephyr:peer_sync:snapshot_restore_pressure_by_peer`, and `zephyr:peer_sync:snapshot_restore_age_by_peer`, the new horizon-aware peer-summary views on `GET /v1/status`, `GET /v1/metrics`, `GET /metrics`, and `GET /v1/health` with retained windows `5m`, `15m`, `1h`, `6h`, and `24h`, the per-peer snapshot-repair metadata gauges `zephyr_peer_snapshot_restore_last_height`, `zephyr_peer_snapshot_restore_last_observed_at_seconds`, and `zephyr_peer_snapshot_restore_age_seconds`, the canonical horizon rollup `zephyr:peer_sync:incident_pressure_by_horizon`, the new aggregate-plus-split snapshot-repair related-alert metadata in `GET /v1/recording-rules`, `GET /v1/recording-rules/prometheus`, and `GET /v1/dashboards`, and the `Peer incident pressure horizons`, `Peer snapshot restore pressure by peer`, `Peer snapshot restore heights`, `Peer snapshot restore age`, `Peer snapshot restore pressure`, plus `Peer snapshot restore reasons` dashboard surfaces.
   Immediate substeps now:
   1. decide whether the current retained-window horizons should stay `LastObservedAt`-based summaries now that `6h` and `24h` exist, or graduate into true history-backed rollups;
   2. decide whether the new repair-path-specific alert codes should remain additive next to the aggregate `peer_snapshot_restored` signal now that alerts, recording-rule metadata, and dashboard metadata all expose both views together, or whether the aggregate code should later be downgraded or deprecated with an explicit compatibility plan;
   3. decide whether the new per-peer repair age surface is enough for operator drill-down or whether fresh-vs-stale repair should also grow thresholds or alerting, preferably starting from a low-cardinality aggregate signal before adding any peer-level thresholding.
2. Extend the current local proposal, vote, and certified block-commit WAL plus import-repair history into broader consensus recovery coverage where more in-flight actions can resume safely after restart.
3. Add deterministic multi-node integration tests for certified happy path, conflicting proposals, timeout and re-proposal, restart during a round, rejection diagnostics, and recovery from partial quorum.
4. Keep tightening the transport-backed consensus loop so proposal and vote recovery remain correct when peers reconnect after advancing rounds.
5. Tune the new alert-rule, recording-rule, and dashboard bundles, extend the Grafana export, and extend the current structured logs, JSON metrics, Prometheus `GET /metrics`, `GET /v1/alerts`, `GET /v1/slo`, and readiness surfaces into wider aggregation beyond the current bounded peer-history window.

Exit criteria:

- a block is considered committed because validators agreed on a well-defined proposal, not because one local node wrote it first
- nodes can restart and resume without silently losing consensus-critical state
- operators can distinguish proposal failure, quorum failure, template mismatch, timeout, and transport failure from observable state

### Phase 2: Networking And State Sync Hardening

Status:

- a transport abstraction now exists
- the active transport is still static peer URLs over HTTP
- validator nodes can attach signed identity proofs to replicated requests and expose the same proof through status
- peer views can verify that proof, enforce strict peer admission, pin configured peers to expected validator identities, and expose per-peer sync, repair, and replication-failure telemetry plus durable peer-incident history, restart-safe backfill, and counters
- admitted-peer policy already gates current HTTP sync and replication behavior
- proposal, vote, and certified-block replication already ride over that abstraction
- failed outgoing proposal, vote, and block dissemination now lands in durable `replication_blocked` peer incidents with reason and error-code rollups
- a first machine-readable `GET /v1/metrics` surface already exposes current transport and consensus observability as JSON for operator tooling and future export adapters, including structured settlement alert metadata, normalized queue-drain utilization ratios, recent backlog-drain estimates, per-estimate warn utilization ratios, and an explicit peak drain-estimate summary
- `GET /metrics` now exposes those same operator signals through a Prometheus-style text exporter for standard scraping stacks
- `GET /v1/health` now condenses those same runtime signals into a pass, warn, or fail readiness surface for operators and automation, including peer-incident horizon detail in `peer_sync`
- `GET /v1/alerts` now exposes derived warning and critical alerts for polling dashboards, operators, and automation, including targeted peer import, admission, replication, and snapshot-restore diagnostics
- `GET /v1/slo` now exposes SLO-oriented objective summaries on top of those same signals for dashboards, operators, and automation
- `GET /v1/alert-rules` and `GET /v1/alert-rules/prometheus` now turn those same metrics and objectives into recommended monitoring bundles for JSON and Prometheus-oriented workflows, including a targeted peer snapshot-restore rule plus repair-path-specific divergence, import-repair, and fetch-fallback rules in the peer-sync group
- `GET /v1/recording-rules` and `GET /v1/recording-rules/prometheus` now turn those same metrics and objectives into recommended dashboard and aggregation rollups for JSON and Prometheus-oriented workflows, including settlement-throughput state rollups, normalized queue-drain utilization, projected queue-drain pressure, max projected queue-drain pressure, and queue-drain estimate rollups, per-peer incident-pressure and incident-pressure-by-horizon rollups, per-peer snapshot-repair pressure and age rollups, a snapshot-repair pressure rollup, and a snapshot-repair-by-reason rollup for peer-sync drill-down, runtime-aware disabled reasons when a producing or synced role is absent, and aggregate-plus-split snapshot-repair related-alert metadata for compatibility-safe downstream joins
- `GET /v1/dashboards` and `GET /v1/dashboards/grafana` now turn those same metrics, rollups, and objectives into recommended operator dashboard bundles and Grafana-oriented export, including settlement-throughput state, raw queue-drain lag, normalized queue-drain utilization, rule-backed estimated queue-drain pressure, a rule-backed worst-case projected-pressure stat, rule-backed estimated queue-drain time, and recent TPS panels in the overview bundle plus peer incident reason, error-code, per-peer pressure, peer incident horizons, per-peer snapshot-repair pressure, per-peer snapshot-repair height, per-peer snapshot-repair age, snapshot-repair-pressure, and snapshot-repair-reasons diagnosis in the peer-sync bundle, with JSON metadata preserved even when some panels are runtime-disabled and the peer-sync panels now carrying aggregate-plus-split snapshot-repair alert-code metadata
- optional structured JSON event logs already expose consensus diagnostics, peer incidents, and snapshot recovery as line-oriented runtime events
- the timeout-driven automation slice already uses that transport for proposal and vote dissemination
- behind nodes can fetch blocks or restore full snapshots
- sync is convenient, but not trust-minimized or production-safe

Next steps:

1. Replace static peer configuration with authenticated peer discovery over libp2p while preserving validator-binding semantics.
2. Add transport-level duplicate suppression, replay-safe message handling, and explicit message identifiers for consensus artifacts.
3. Separate dev snapshot restore from production state sync so operators can choose explicit trust models.
4. Add checkpointing, snapshot metadata, and verification hooks for state transfer.
5. Extend the current JSON metrics surface, Prometheus `GET /metrics`, `GET /v1/health`, `GET /v1/alerts`, `GET /v1/slo`, `GET /v1/alert-rules`, `GET /v1/alert-rules/prometheus`, `GET /v1/recording-rules`, `GET /v1/recording-rules/prometheus`, `GET /v1/dashboards`, `GET /v1/dashboards/grafana`, and structured event logs into broader dashboard packages, multi-peer incident aggregation, and export adapters for validator, sync, admission, transport, automation, and repair operations.
   Progress now: repair-path-specific snapshot-restore rollups, per-peer repair drill-down panels including age-by-peer, recommended alert rules, horizon-aware peer-summary views, and aggregate-plus-split snapshot-repair related-alert metadata already live under the existing peer-sync bundle, so the next slice can stay focused on richer history and policy decisions instead of creating a new dashboard family.
   Immediate observability slice after this step:
   1. evaluate whether the current retained-window horizons should remain bounded `LastObservedAt` summaries now that `6h` and `24h` are live, or shift to true history-backed rollups while preserving JSON metadata and clean Grafana export;
   2. decide whether `peer_snapshot_restored` should remain as the compatibility aggregate now that repair-path alert codes are live and the bundle metadata carries both aggregate and split mappings, or whether downstream surfaces should eventually pivot fully to the split codes;
   3. extend the same alignment discipline into structured logs and any future export adapters so downstream operators do not need custom joins once they leave the JSON bundle surfaces.

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
- metrics, alerts, SLO summaries, readiness probes, tracing, dashboards, and incident-friendly logs
- release automation, reproducible builds, and upgrade runbooks

## Long-Term Roadmap

These phases should stay broad until the lower-level protocol is stable.

### Phase 6: Confidential Compute Marketplace

Broad direction:

- package off-chain jobs separately from on-chain contracts
- add worker registry, attestation verification, bidding, escrow, settlement, and slashing
- settle payments on-chain with the Zephyr native token
- keep privacy-oriented execution separate from consensus-critical state transitions
- scale confidential compute first through partitioned worker pools or execution lanes with batched on-chain settlement rather than assuming immediate full consensus sharding
- treat full sharded consensus as a later research option to evaluate only if measured throughput shows the single-chain settlement path becoming the bottleneck

### Phase 7: Public Testnet To Mainnet Readiness

Broad direction:

- public testnet launch criteria
- validator onboarding and incident runbooks
- upgrade strategy and rollback planning
- monitoring, alerts, and SLOs for operators
- staged path from devnet to public testnet to mainnet







































































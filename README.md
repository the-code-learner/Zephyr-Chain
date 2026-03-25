# Zephyr Chain

Zephyr Chain is an early-stage blockchain node and wallet stack focused on a production path toward validator-driven consensus, deterministic WASM execution, and a confidential compute marketplace.

The long-term product vision lives in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). Application and use-case framing lives in [docs/applications.md](./docs/applications.md). Academic paper materials are maintained locally under `paper/` and kept private via `.gitignore`. This `README` stays practical: what works now, what changed in the latest iteration, and what comes next.

## Current Status

Implemented today:

- Go HTTP node entrypoint in `cmd/node`
- DPoS election primitives and tests in `internal/dpos`
- transaction envelope validation in `internal/tx`
- durable accounts, mempool, committed blocks, restart-safe state, and snapshot restore in `internal/ledger`
- durable validator-set snapshots with versioning, proposer scheduling, and quorum summaries in `internal/ledger`
- durable consensus round state with restart-safe height, round, and round-start tracking in `internal/ledger`
- durable signed consensus proposals, validator votes, and quorum certificates in `internal/ledger`
- self-contained consensus proposals that carry the full candidate transaction body plus deterministic template fields
- automatic single-node block production plus manual dev block production
- optional proposer-schedule enforcement for block production when a validator set and local validator address are configured
- optional certificate-gated local block commit and remote block import when consensus enforcement is enabled
- certificate-gated local commit can replay a stored certified proposal body even when the local mempool no longer has that candidate
- optional consensus automation for scheduled self-proposal, validator auto-vote, timeout-driven round advance, proposer rotation, and certified proposer auto-commit on the current devnet path
- transport-backed peer replication in `internal/api` with the current implementation running over HTTP
- signed validator transport-identity proofs in status responses and peer verification views when a validator private key is configured
- optional strict peer-admission enforcement and peer-to-validator binding on the current HTTP transport
- peer status tracking, peer admission state, per-peer sync telemetry, block fetch by height, block import, snapshot-based catch-up, and consensus artifact replication for admitted peers
- consensus visibility endpoints for status, validator snapshots, active round inspection, proposer schedule inspection, latest consensus artifacts, and next-block template preview
- operator-facing observability endpoints for readiness, alerts, SLO summaries, alert-rule exports, recording-rule exports, dashboard bundles, Grafana dashboard export, JSON metrics, Prometheus metrics, and structured logs
- Vue wallet in `apps/wallet`
- wallet account generation, import/export, local signing, account inspection, faucet funding, and transaction broadcast

Implemented in this iteration:

- failed outgoing proposal, vote, and block dissemination now lands in durable `replication_blocked` peer incidents with artifact-specific `reason` labels and transport-oriented error-code rollups
- `GET /v1/alerts` now separates general peer-sync degradation from targeted `peer_import_blocked`, `peer_admission_blocked`, and `peer_replication_blocked` warnings built from durable peer incident rollups
- `GET /v1/alert-rules` and `GET /v1/alert-rules/prometheus` now export matching peer-import, peer-admission, and peer-replication diagnostic rules for scrape-based monitoring stacks
- `GET /metrics` now exports retained peer incident counts and latest observation timestamps per peer with the latest state, reason, and error-code labels attached for scrape-based drill-down
- `GET /v1/recording-rules` and `GET /v1/recording-rules/prometheus` now export a canonical per-peer incident-pressure rollup so downstream dashboards can reuse that peer view without rewriting PromQL
- `GET /v1/dashboards` and `GET /v1/dashboards/grafana` now expose peer incident reason panels plus a per-peer incident pressure panel built on that recording rule alongside state and error-code rollups so dissemination failures are visible in the peer-sync bundle
- `GET /v1/metrics` now includes `chainThroughput` totals plus rolling `1m`, `5m`, and `15m` windows for committed blocks, committed transactions, average transactions per block, and recent TPS baselining, along with a `settlementThroughput` view carrying raw queue-drain lag, latest commit age, and warn or fail thresholds
- `GET /metrics` now also exports committed-block, committed-transaction, latest-block-interval, and rolling throughput gauges plus settlement queue-drain gauges such as `zephyr_settlement_queue_drain_lag_seconds` and `zephyr_settlement_queue_drain_threshold_seconds` so Prometheus-style monitoring can track both recent TPS and settlement pressure without re-deriving chain history
- `GET /v1/recording-rules` and `GET /v1/recording-rules/prometheus` now additionally export canonical `zephyr:chain:transactions_per_second_1m`, `zephyr:chain:transactions_per_second_5m`, and `zephyr:chain:transactions_per_second_15m` rollups for dashboard reuse
- `GET /v1/dashboards` and `GET /v1/dashboards/grafana` now add a `Recent transaction throughput` overview panel built on those rollups so operators can baseline recent TPS alongside readiness and peer health
- `GET /v1/health` now includes a `settlement_throughput` check that watches queued transaction drain against the configured automatic block interval when block production is enabled
- `GET /v1/alerts` and `GET /v1/slo` now derive `settlement_throughput_reduced`, `settlement_throughput_stalled`, and the `settlement_throughput` objective so slow or stalled queue drain becomes first-class operator evidence
- `GET /v1/alert-rules` and `GET /v1/alert-rules/prometheus` now export `ZephyrSettlementThroughputAtRisk` and `ZephyrSettlementThroughputStalled` so the same queue-drain signal can be promoted into Prometheus-based alerting
- `GET /v1/recording-rules` and `GET /v1/recording-rules/prometheus` now additionally export canonical `zephyr:settlement_throughput:at_risk` and `zephyr:settlement_throughput:breached` rollups for queue-drain dashboards and fleet summaries
- `GET /v1/dashboards` and `GET /v1/dashboards/grafana` now add both a `Settlement throughput state` overview panel built on those rollups and a raw `Settlement queue-drain lag` panel built on the settlement gauges so operators can see queue-drain pressure next to recent TPS baselines
- `GET /v1/peers` now backfills the latest import, snapshot-repair, and replication-failure telemetry from durable peer incidents so operator context survives restart before the next live sync pass
- successful local certified commits now record a durable `block_commit` consensus action so recovery history and action metrics cover the full proposer path from proposal and vote through commit
- focused tests now cover peer import, admission, and replication alerts, per-peer Prometheus incident metrics, restart-safe per-peer telemetry reconstruction, JSON and Prometheus throughput metrics including raw settlement-lag gauges, throughput health or alert or SLO projections, alert-rule export, dashboard export, and durable `block_commit` history across the operator surfaces

Planned but not implemented yet:

- authenticated peer discovery and replay-safe transport over libp2p on top of the new HTTP admission and binding policy
- broader consensus recovery coverage plus richer dashboard packages, longer-horizon aggregation, and export adapters beyond the current local proposal, vote, block-commit, peer-import, snapshot-recovery, JSON metrics, Prometheus text export, alert-rule bundles, recording-rule bundles, dashboard bundles, Grafana dashboard export, derived readiness, alerts, SLO summaries, structured event logs, durable peer-sync history, and derived peer-sync summary surfaces
- on-chain staking and governance-driven validator updates instead of ad hoc election API writes
- deterministic WASM smart-contract runtime with native fee metering
- confidential compute marketplace for encrypted off-chain jobs paid in native tokens, with partitioned worker-lane scaling ahead of any full consensus-sharding step
- production observability, recovery tooling, and public testnet operations

## Repository Layout

- `cmd/node`: node process entrypoint and environment-based runtime configuration
- `internal/api`: HTTP handlers, peer replication, consensus surface, transport abstraction, sync loops, automation loop, and status endpoints
- `internal/consensus`: signed proposal and vote message primitives
- `internal/dpos`: candidate, vote, validator, and election service logic
- `internal/ledger`: persisted accounts, mempool entries, committed blocks, validator snapshots, round state, consensus artifacts, snapshots, and commit/import logic
- `internal/tx`: transaction envelope validation, address derivation, and signature verification
- `apps/wallet`: reference light wallet built with Vue 3, Vite, and Tailwind CSS
- `docs/`: architecture, API, usage, roadmap, and applications guides
- `paper/`: private local academic paper workspace, draft manuscript materials, and evaluation planning notes kept out of git via `.gitignore`
- `var/`: default local runtime state directory for the node, ignored by git

## Prerequisites

- Go 1.22 or newer
- Node.js
- npm

PowerShell note: if your shell blocks `npm`, use `npm.cmd` instead.

## Quick Start

### 1. Run one node

From the repository root:

```powershell
go run ./cmd/node
```

By default the node:

- listens on `:8080`
- stores durable state in `var/node`
- produces blocks every `15s` when transactions are queued
- runs the consensus automation ticker every `1s`, but automation stays off until `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION=true`
- uses a `5s` consensus round timeout once automation is enabled
- runs peer sync only if `ZEPHYR_PEERS` is configured
- exposes consensus status even before a validator set has been elected

Useful probes:

```powershell
Invoke-RestMethod http://localhost:8080/health
curl.exe -i http://localhost:8080/v1/health
Invoke-RestMethod http://localhost:8080/v1/alerts
Invoke-RestMethod http://localhost:8080/v1/slo
Invoke-RestMethod http://localhost:8080/v1/alert-rules
Invoke-RestMethod http://localhost:8080/v1/recording-rules
Invoke-RestMethod http://localhost:8080/v1/dashboards
Invoke-RestMethod http://localhost:8080/v1/status
curl.exe http://localhost:8080/metrics
curl.exe http://localhost:8080/v1/alert-rules/prometheus
curl.exe http://localhost:8080/v1/recording-rules/prometheus
curl.exe http://localhost:8080/v1/dashboards/grafana
```

### 2. Run the wallet

In a second terminal:

```powershell
cd apps/wallet
npm install
npm run dev
```

If PowerShell execution policy blocks `npm`, run:

```powershell
cd apps/wallet
npm.cmd install
npm.cmd run dev
```

Vite serves the wallet on `http://localhost:5173` by default.

### 3. Run a two-node local devnet

Node A, producer:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_HTTP_ADDR=":8080"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="true"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
go run ./cmd/node
```

Node B, replica:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_HTTP_ADDR=":8081"
$env:ZEPHYR_DATA_DIR="var/devnet-b"
$env:ZEPHYR_PEERS="http://localhost:8080"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="false"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
go run ./cmd/node
```

Use the wallet against Node A. Node B will follow through transaction, block, snapshot, and consensus-artifact sync from admitted peers.

### 4. Enable certificate-gated commit/import

For production-style consensus enforcement on the current devnet flow, run a node with:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

Then use:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/dev/block-template
Invoke-RestMethod http://localhost:8080/v1/consensus
```

`GET /v1/status` exposes the node's signed transport identity when `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured. The template response gives you the exact `height`, `previousHash`, `producedAt`, full `transactions`, ordered `transactionIds`, and `blockHash` that a signed proposal must certify. Once a matching quorum certificate exists, `POST /v1/dev/produce-block` can commit that exact block candidate from the stored proposal body.

### 5. Enable autonomous certified consensus on a devnet

Initial round-0 proposer:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_HTTP_ADDR=":8080"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key-a>"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="true"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
$env:ZEPHYR_ENABLE_CONSENSUS_AUTOMATION="true"
$env:ZEPHYR_CONSENSUS_INTERVAL="250ms"
$env:ZEPHYR_CONSENSUS_ROUND_TIMEOUT="2s"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

Second validator:

```powershell
$env:ZEPHYR_NODE_ID="node-b"
$env:ZEPHYR_HTTP_ADDR=":8081"
$env:ZEPHYR_DATA_DIR="var/devnet-b"
$env:ZEPHYR_PEERS="http://localhost:8080"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key-b>"
$env:ZEPHYR_ENABLE_BLOCK_PRODUCTION="true"
$env:ZEPHYR_ENABLE_PEER_SYNC="true"
$env:ZEPHYR_ENABLE_CONSENSUS_AUTOMATION="true"
$env:ZEPHYR_CONSENSUS_INTERVAL="250ms"
$env:ZEPHYR_CONSENSUS_ROUND_TIMEOUT="2s"
$env:ZEPHYR_REQUIRE_PEER_IDENTITY="true"
$env:ZEPHYR_PEER_VALIDATORS="http://localhost:8080=zph_validator_a"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

With an active validator set and queued transactions, the scheduled proposer self-builds and signs the next proposal, active validators auto-vote, and the scheduled proposer auto-commits as soon as a matching quorum certificate exists. If the scheduled proposer stalls past the round timeout, the node advances the round, rotates the proposer, and the new proposer can reuse the latest stored candidate body for that same height.

## Runtime Configuration

### Node

- `ZEPHYR_HTTP_ADDR`: HTTP bind address for the Go node
- `ZEPHYR_NODE_ID`: human-readable node identifier used in peer replication headers and status output
- `ZEPHYR_VALIDATOR_ADDRESS`: chain-level validator address used for proposer-schedule enforcement and status reporting
- `ZEPHYR_VALIDATOR_PRIVATE_KEY`: base64-encoded PKCS#8 P-256 private key used to derive and sign the node's validator transport identity plus automated proposal and vote messages
- `ZEPHYR_DATA_DIR`: local directory used for durable node state
- `ZEPHYR_PEERS`: comma-separated peer base URLs such as `http://localhost:8081,http://localhost:8082`
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval such as `15s`
- `ZEPHYR_CONSENSUS_INTERVAL`: automation ticker interval such as `250ms` or `1s`
- `ZEPHYR_CONSENSUS_ROUND_TIMEOUT`: timeout window for the active round before automation advances to the next round
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval such as `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per produced block
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true` or `false`
- `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION`: `true` or `false`; when enabled, active validators automatically propose, vote, and advance rounds on the current devnet path
- `ZEPHYR_ENABLE_PEER_SYNC`: `true` or `false`
- `ZEPHYR_ENABLE_STRUCTURED_LOGS`: `true` or `false`; when enabled, the node emits newline-delimited JSON event logs for diagnostics, peer incidents, and snapshot recovery
- `ZEPHYR_REQUIRE_PEER_IDENTITY`: `true` or `false`; when enabled, replicated peer POST requests must include a valid signed transport identity
- `ZEPHYR_PEER_VALIDATORS`: comma-separated `<peer-url>=<validator-address>` bindings used to pin configured peers to expected validators
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: `true` or `false`; when enabled and a validator set exists, only the scheduled proposer for the active round may produce the next block locally
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: `true` or `false`; when enabled and a validator set exists, local block commit and remote block import require a matching proposal and quorum certificate

Default values:

- `ZEPHYR_HTTP_ADDR`: `:8080`
- `ZEPHYR_NODE_ID`: `node-local`
- `ZEPHYR_VALIDATOR_ADDRESS`: empty
- `ZEPHYR_VALIDATOR_PRIVATE_KEY`: empty
- `ZEPHYR_DATA_DIR`: `var/node`
- `ZEPHYR_PEERS`: empty
- `ZEPHYR_BLOCK_INTERVAL`: `15s`
- `ZEPHYR_CONSENSUS_INTERVAL`: `1s`
- `ZEPHYR_CONSENSUS_ROUND_TIMEOUT`: `5s`
- `ZEPHYR_SYNC_INTERVAL`: `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true`
- `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION`: `false`
- `ZEPHYR_ENABLE_PEER_SYNC`: `true`
- `ZEPHYR_ENABLE_STRUCTURED_LOGS`: `false`
- `ZEPHYR_REQUIRE_PEER_IDENTITY`: `false`
- `ZEPHYR_PEER_VALIDATORS`: empty
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: `false`
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: `false`

### Wallet

- `VITE_ZEPHYR_API_BASE`: base URL used by the wallet for node API calls
- default: `http://localhost:8080`

Example `.env.local` inside `apps/wallet`:

```env
VITE_ZEPHYR_API_BASE=http://localhost:8080
```

## How The MVP Works

1. The wallet generates an ECDSA P-256 keypair in the browser using Web Crypto.
2. It derives a Zephyr-style address from the SHA-256 hash of the exported public key.
3. The wallet stores the private key, public key, and address in browser `localStorage`.
4. The wallet can inspect node-side account state and use a dev faucet for local funding.
5. The wallet signs a canonical transaction payload locally and sends the signed envelope to the node.
6. The node validates the payload, address, signature, nonce, and available balance before persisting the transaction in the durable mempool.
7. A block-producing node can build a deterministic next-block template from the current mempool and latest chain tip.
8. DPoS elections persist a durable validator snapshot with versioning, voting-power totals, and next-proposer scheduling metadata.
9. Consensus state now persists the active height, round, and round start time separately from blocks and validator snapshots.
10. Operators can still submit signed proposals for the active round's concrete block template, including the exact `previousHash`, `producedAt`, full `transactions`, ordered `transactionIds`, and derived `blockHash`.
11. Validators can verify those proposal transactions directly from the proposal body instead of depending on local mempool convergence alone.
12. If consensus automation is enabled, the scheduled proposer for the active round can build that same template, sign a proposal, persist it, and disseminate it without an operator POST.
13. Active validators with automation enabled can sign and replicate a vote for the current known proposal.
14. If the active round times out, the node advances the round, rotates the scheduled proposer, and can accept higher-round messages from peers even if its own timer had not fired yet.
15. A new higher-round proposer can reuse the latest stored candidate body for that height instead of depending only on local mempool state.
16. Once vote power crosses quorum, the node stores a durable commit certificate artifact for that height and round.
17. If proposer-schedule enforcement is enabled, a node can refuse to produce a block unless its configured validator address matches the scheduled proposer for the active round.
18. If consensus-certificate enforcement is enabled, local block commit and remote block import both require a proposal and quorum certificate for the exact block template being committed.
19. A scheduled proposer with automation enabled and certificate enforcement turned on can auto-commit immediately from the stored certified proposal body.
20. A consensus-gated local commit can replay the stored certified proposal body even when the local mempool does not contain that candidate anymore.
21. If a validator private key is configured, the node derives a signed transport identity for its validator address and exposes that proof in runtime status.
22. Configured peer nodes receive transactions, self-contained consensus proposals, votes, and blocks over the current transport implementation, verify peer identity proofs when available, enforce admission and validator binding when configured, import blocks when possible, and fall back to snapshot restore when they need catch-up.

## Current Limitations

- the current multi-node layer is still HTTP-based under the new transport abstraction, not libp2p networking
- peer admission and validator pinning can now be enforced over the current HTTP transport, but peer discovery is still static configuration rather than libp2p
- the round engine now supports timeout-driven proposer rotation, latest-artifact rebroadcast after link recovery, richer `roundEvidence`, per-height `roundHistory`, `blockReadiness`, import-aware `recovery`, durable peer-sync incident history, bounded rejection diagnostics, machine-readable `GET /v1/metrics`, Prometheus-style `GET /metrics`, derived `GET /v1/health`, derived `GET /v1/alerts` including peer import, admission, and replication warnings, derived `GET /v1/slo`, recommended `GET /v1/alert-rules`, exported `GET /v1/alert-rules/prometheus`, recommended `GET /v1/recording-rules`, exported `GET /v1/recording-rules/prometheus`, recommended `GET /v1/dashboards`, exported `GET /v1/dashboards/grafana`, and local consensus-action history across restart for proposal, vote, round-advance, block-commit, import, and snapshot-repair events, but broader recovery tooling is still missing
- crash recovery now persists active round metadata plus a bounded local consensus-action WAL, and peer snapshot restore preserves local recovery, diagnostics, and peer-sync incident history, but replay coverage is still centered on local proposal, vote, certified block-commit, and import-repair paths rather than the full consensus lifecycle
- DPoS elections still happen through an API call, not an on-chain staking/governance flow
- snapshot restore is a state catch-up mechanism, not a trust-minimized proof-based sync protocol
- WASM smart-contract execution is planned, but not implemented yet
- confidential compute jobs, worker attestation, escrow, and settlement are planned, but not implemented yet
- wallet private keys are stored unencrypted in browser `localStorage`

Because of these limitations, the current MVP should still be treated as a development prototype, not a production blockchain network.

## Roadmap

The production roadmap now lives in [docs/roadmap.md](./docs/roadmap.md).

Short version:

1. Move the new enforced HTTP peer-admission and validator-binding policy toward authenticated libp2p discovery plus replay-safe transport behavior.
2. Extend the new `blockReadiness`, `roundHistory`, `roundEvidence`, `recovery`, `diagnostics`, `peerSyncHistory`, `peerSyncSummary`, per-peer `recentIncidents`, `GET /v1/metrics`, `GET /metrics`, `GET /v1/health`, `GET /v1/alerts`, `GET /v1/slo`, `GET /v1/alert-rules`, `GET /v1/alert-rules/prometheus`, `GET /v1/recording-rules`, `GET /v1/recording-rules/prometheus`, `GET /v1/dashboards`, `GET /v1/dashboards/grafana`, and structured event logs into deeper recovery, longer-horizon incident retention, richer exported metrics, broader dashboard coverage, and production incident tooling.
3. Move validator lifecycle changes behind staking, delegation, slashing, and governance state transitions.
4. Add deterministic WASM execution, native fee metering, and the confidential compute lane.
5. Add production observability, recovery tooling, and public testnet operations.

## Documentation

- [docs/architecture.md](./docs/architecture.md)
- [docs/api.md](./docs/api.md)
- [docs/usage.md](./docs/usage.md)
- [docs/roadmap.md](./docs/roadmap.md)
- [docs/applications.md](./docs/applications.md)
- [paper/README.md](./paper/README.md)
- [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md)

## License

Zephyr Chain is licensed under the MIT License. See [LICENSE](./LICENSE).






































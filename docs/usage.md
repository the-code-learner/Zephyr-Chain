# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you five practical local development flows:

- a single-node flow where one Go node persists chain state, funds test accounts, validates transactions, and commits blocks
- a small multi-node devnet flow where one node produces blocks and other configured nodes follow through transport-backed replication and sync
- a scheduling flow where you elect a validator set, inspect the derived active round and scheduled proposer, and optionally enforce that schedule for local block production
- a manual certificate-gated consensus flow where you build a concrete next-block template, submit signed proposals and votes for the active round, and commit only after a quorum certificate exists
- an automated certificate-gated devnet flow where the scheduled proposer self-proposes, active validators auto-vote, timeout can rotate the proposer, and the next proposer can reuse the stored candidate body for the same height

The browser wallet can create a local account, export and import it, inspect node-side account state, sign a transaction, and send it to the node.

Deterministic WASM smart contracts and a confidential compute marketplace are planned next phases, not part of the current runnable workflow. Product-oriented applications and target use cases are outlined in [docs/applications.md](./applications.md).

## Run A Single Node

From the repository root:

```powershell
go run ./cmd/node
```

Sanity-check the node with:

```powershell
Invoke-RestMethod http://localhost:8080/health
curl.exe -i http://localhost:8080/v1/health
Invoke-RestMethod http://localhost:8080/v1/alerts
Invoke-RestMethod http://localhost:8080/v1/slo
Invoke-RestMethod http://localhost:8080/v1/alert-rules
Invoke-RestMethod http://localhost:8080/v1/recording-rules
Invoke-RestMethod http://localhost:8080/v1/dashboards
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/consensus
curl.exe http://localhost:8080/metrics
curl.exe http://localhost:8080/v1/alert-rules/prometheus
curl.exe http://localhost:8080/v1/recording-rules/prometheus
curl.exe http://localhost:8080/v1/dashboards/grafana
```

## Inspect Node Readiness

`/health` tells you whether the process is responding. `/v1/health` tells you whether the node is actually ready based on validator-set expectations, recovery backlog, consensus warnings, peer-sync condition, and recent diagnostics. `/v1/slo` tells you how those same signals roll up into operator-facing objectives for readiness, consensus continuity, and peer-sync continuity.

```powershell
curl.exe -i http://localhost:8080/health
curl.exe -i http://localhost:8080/v1/health
Invoke-RestMethod http://localhost:8080/v1/alerts
Invoke-RestMethod http://localhost:8080/v1/slo
Invoke-RestMethod http://localhost:8080/v1/metrics
curl.exe http://localhost:8080/metrics
```

What to expect:

- `/health` returns `200` as long as the API loop is alive
- `/v1/health` returns `200` when only `pass` or `warn` checks exist and `503` when at least one `fail` check is active
- `checks` currently cover `api`, `validator_set`, `recovery`, `consensus`, `peer_sync`, and `diagnostics`
- `/v1/alerts` turns those same operator signals into a derived critical or warning alert set for polling dashboards and automation, including targeted `peer_import_blocked`, `peer_admission_blocked`, and `peer_replication_blocked` warnings when retained peer incidents point to those fault classes
- `/v1/slo` groups them into objective states so operators can see whether readiness, consensus continuity, or peer sync continuity is meeting, at risk, breached, or not applicable
- `/metrics` exports the alert, health, and SLO state as Prometheus-style gauges such as `zephyr_node_ready`, `zephyr_health_check_status`, `zephyr_alert_active`, and `zephyr_slo_objective_status`, plus peer-incident gauges such as `zephyr_peer_sync_reason_occurrence_count`, `zephyr_peer_sync_error_code_occurrence_count`, per-peer retained-incident gauges like `zephyr_peer_sync_peer_occurrence_count`, and chain throughput gauges such as `zephyr_chain_total_committed_transaction_count` and `zephyr_chain_window_transactions_per_second`, while `/v1/metrics` keeps the structured JSON view including `chainThroughput` windows for `1m`, `5m`, and `15m`
- `/v1/alert-rules` keeps the structured recommended alert bundle, while `/v1/alert-rules/prometheus` exports the enabled subset as Prometheus-rule YAML for scrape-based alerting stacks
- `/v1/recording-rules` keeps the structured recommended recording bundle, while `/v1/recording-rules/prometheus` exports the enabled subset as Prometheus recording-rule YAML for dashboard and aggregation stacks, including canonical recent-TPS rollups
- `/v1/dashboards` keeps the structured recommended dashboard bundle, while `/v1/dashboards/grafana` exports the enabled subset as Grafana-oriented JSON built on the current recording rules and metrics, including the overview throughput panel
- use `/v1/health` together with `/v1/alerts`, `/v1/slo`, `/metrics`, `/v1/metrics`, `GET /v1/status`, `/v1/dashboards`, and structured logs when you need both a quick readiness gate and deeper incident context

## Export Recommended Alert Rules

Once you have a node running, you can export the recommended monitoring bundles directly from the API:

```powershell
Invoke-RestMethod http://localhost:8080/v1/alert-rules
curl.exe http://localhost:8080/v1/alert-rules/prometheus
```

What to expect:

- `/v1/alert-rules` returns readiness, consensus, and peer-sync rule groups with expressions, severities, source metrics, and disabled reasons when a rule is not applicable to the current node configuration; the peer-sync group now includes continuity rules plus targeted peer import, peer admission, and peer replication diagnostics
- `/v1/alert-rules/prometheus` exports only the enabled subset as Prometheus-rule YAML so you can drop it into a standard scrape-plus-alert workflow without hand-translating expressions
- treat the bundle as a production-oriented starting point rather than a final policy set; tune durations, severities, and escalation paths for your deployment

## Export Recommended Recording Rules

Once you have a node running, you can export the recommended aggregation rollups directly from the API:

```powershell
Invoke-RestMethod http://localhost:8080/v1/recording-rules
curl.exe http://localhost:8080/v1/recording-rules/prometheus
```

What to expect:

- `/v1/recording-rules` returns readiness, consensus, peer-sync, and operator-summary recording-rule groups with stable `record` names, expressions, source metrics, and disabled reasons when a rule is not applicable to the current node configuration; the peer-sync group includes the per-peer incident-pressure rollup `zephyr:peer_sync:incident_pressure_by_peer`, and the operator-summary group includes `zephyr:chain:transactions_per_second_1m`, `zephyr:chain:transactions_per_second_5m`, and `zephyr:chain:transactions_per_second_15m`
- `/v1/recording-rules/prometheus` exports only the enabled subset as Prometheus recording-rule YAML so you can drop it into a standard scrape-plus-dashboard workflow without hand-translating expressions
- use these rollups as the default dashboard query layer on top of `/metrics`, then import or adapt the higher-level dashboard bundles for your deployment

## Export Recommended Dashboards

Once you have a node running, you can export the recommended dashboard bundles directly from the API:

```powershell
Invoke-RestMethod http://localhost:8080/v1/dashboards
curl.exe http://localhost:8080/v1/dashboards/grafana
```

What to expect:

- `/v1/dashboards` returns overview, consensus-and-recovery, and peer-sync dashboard bundles with stable panel IDs, PromQL queries, source endpoints, related recording rules, related alert codes, and disabled reasons when a dashboard or panel is not applicable to the current node configuration; the overview bundle now includes a `Recent transaction throughput` panel built on `zephyr:chain:transactions_per_second_1m`, `zephyr:chain:transactions_per_second_5m`, and `zephyr:chain:transactions_per_second_15m`, while the peer-sync bundle includes incident-by-state, incident-by-reason, incident-by-error-code, and per-peer incident-pressure panels tied to the peer import, admission, and replication alerts, with the per-peer panel built on `zephyr:peer_sync:incident_pressure_by_peer`
- `/v1/dashboards/grafana` exports only the enabled dashboards and panels as Grafana-oriented JSON so you can import a starting Zephyr dashboard set after wiring a Prometheus data source to `/metrics`
- treat the bundle as a production-oriented starting point rather than a final layout; tune datasource selection, labels, thresholds, and panel arrangement for your deployment

## Run A Two-Node Devnet

Use separate terminals and separate data directories.

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

What to expect:

- Node A accepts wallet transactions and can produce blocks
- Node B polls peer status on `ZEPHYR_SYNC_INTERVAL`
- transactions, faucet credits, proposals, votes, and blocks are replicated over the current transport implementation
- if validator private keys are configured, `GET /v1/status` exposes a signed identity proof, `peerSyncSummary`, and `GET /v1/peers` shows verification, admission state, per-peer sync telemetry, restart-safe import, snapshot, and replication-failure context, derived incident counters, and durable `recentIncidents` history for configured peers
- if Node B starts late or misses a block import, it can recover from Node A's snapshot

## Inspect Validator Scheduling

1. Start a node with a validator address or validator private key.
2. Submit an election, then inspect:

```powershell
Invoke-RestMethod http://localhost:8080/v1/validators
Invoke-RestMethod http://localhost:8080/v1/consensus
```

You should see:

- a validator snapshot version that increases when the election result is replaced
- the persisted validator list and normalized election config
- `totalVotingPower`, `quorumVotingPower`, `currentRound`, `currentRoundStartedAt`, and `nextProposer`

## Inspect Signed Validator Identity

If you want the node to prove which validator it represents over the current HTTP transport, start it with a validator private key:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
go run ./cmd/node
```

Then inspect:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/peers
```

## Enforce Peer Admission

If you want the current HTTP devnet to fail closed on unsigned or mismatched peers, start the node with:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_PEERS="http://localhost:8081"
$env:ZEPHYR_REQUIRE_PEER_IDENTITY="true"
$env:ZEPHYR_PEER_VALIDATORS="http://localhost:8081=zph_validator_b"
go run ./cmd/node
```

What to expect:

- `GET /v1/status` reports `peerIdentityRequired=true`
- `GET /v1/peers` shows `expectedValidator`, `admitted`, `admissionError`, `syncState`, `heightDelta`, per-peer `incidentCount`, `incidentOccurrences`, `latestIncidentAt`, the latest import, snapshot-repair, and replication-failure metadata, and durable `recentIncidents` history for each configured peer
- background sync and outgoing replication use only admitted peers under this policy
- replicated peer POST requests without a valid identity, or from validators outside the configured binding allowlist, are rejected with `403`

## Enable Structured Event Logs

If you want incident-friendly JSON logs while keeping the existing HTTP API surfaces, start the node with:

```powershell
$env:ZEPHYR_ENABLE_STRUCTURED_LOGS="true"
go run ./cmd/node
```

What to expect:

- consensus diagnostics, peer-sync incidents, and snapshot-restore recovery events are emitted as newline-delimited JSON
- `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/metrics` report `structuredLogsEnabled=true`
- the logs are designed to pair with `GET /v1/metrics`, `diagnostics`, and `peerSyncSummary` rather than replace those durable views

## Run A Manual Certificate-Gated Commit Flow

This is the lowest-level way to exercise the current certified block path.

1. Start a node with proposer scheduling and certificate enforcement enabled:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

2. Make sure a validator set already exists.
3. Queue at least one transaction in the mempool.
4. Fetch the concrete next block candidate and the active round:

```powershell
Invoke-RestMethod http://localhost:8080/v1/dev/block-template
Invoke-RestMethod http://localhost:8080/v1/consensus
```

5. Build a signed proposal whose `height`, `round`, `previousHash`, `producedAt`, full `transactions`, ordered `transactionIds`, and `blockHash` match that template exactly.
6. POST the proposal to `/v1/consensus/proposals`.
7. Submit validator votes to `/v1/consensus/votes` until a quorum certificate exists for that same `height`, `round`, and `blockHash`.
8. Commit that exact block template by reusing the returned `producedAt` timestamp:

```powershell
$body = @{ producedAt = "2026-03-24T13:00:00Z" } | ConvertTo-Json
Invoke-RestMethod http://localhost:8080/v1/dev/produce-block -Method Post -ContentType 'application/json' -Body $body
```

## Run An Automated Certified Devnet

This is the closest current path to a production-style validator flow, but it is still a first-pass timeout-driven round engine.

1. Start the initial round-0 proposer:

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

2. Start another active validator:

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

3. Submit an election that makes both validators active and puts Node A first in the proposer schedule.
4. Queue at least one transaction on the current proposer.
5. Inspect the live state:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/consensus
Invoke-RestMethod http://localhost:8081/v1/consensus
```

Expected behavior:

- startup fails if automation is enabled without `ZEPHYR_VALIDATOR_PRIVATE_KEY`
- the scheduled proposer builds the next block template and persists a self-contained proposal automatically
- active validators persist and replicate votes automatically for that proposal
- once quorum is observed, the proposer commits from the stored certified proposal body without requiring `POST /v1/dev/produce-block`
- if the active proposer stalls past `ZEPHYR_CONSENSUS_ROUND_TIMEOUT`, the node advances `currentRound`, rotates `nextProposer`, and the new proposer can reuse the latest stored candidate body for that same height
- admitted peers replicate the proposal, vote, certificate, and committed block over the current HTTP transport, and failed outgoing proposal, vote, or block dissemination is retained as durable `replication_blocked` peer evidence
- `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template` now expose `roundEvidence` so operators can see the active round deadline, proposal presence, leading vote power, quorum remaining, replay backlog, warnings, and certificate state
- those same responses now expose `roundHistory`, which shows the pending height across prior and active rounds so operators can inspect proposer rotation and stalled rounds side by side
- those same responses now expose `blockReadiness`, which shows whether the local template matches stored proposals and certificates and whether commit or import can proceed from stored certified artifacts
- those same responses now expose `recovery`, which shows pending replayable local proposal or vote actions, pending import backlog, and recent replay/completion plus local certified `block_commit` and snapshot-restore metadata from the broader local consensus recovery surface
- those same responses now expose `diagnostics`, which show recent rejected proposal, vote, commit, or import actions with stable error codes
- those same responses now expose `peerSyncHistory`, which keeps recent cross-peer sync incidents and retained `replication_blocked` dissemination failures visible even after restart
- those same responses now expose `peerSyncSummary`, which rolls those incidents up by peer, state, reason, and error code so operators can see the dominant network problem quickly
- `GET /v1/metrics` now provides the same durable peer summary alongside machine-readable consensus-action, diagnostic, peer-incident reason and error-code counters, and live peer-runtime counters for dashboards or automation
- if `ZEPHYR_ENABLE_STRUCTURED_LOGS=true`, the node also emits newline-delimited JSON logs for diagnostics, peer incidents, and snapshot recovery as those events happen
- if a peer link drops and later returns, validators keep rebroadcasting their latest local proposal or vote for the pending height until the matching certificate exists
- if a validator restarts mid-round after persisting a local proposal or vote, the node can replay that pending action from the persisted recovery state

## Troubleshooting

### Proposal Or Vote Submission Returns An Error

- confirm a validator set already exists through `GET /v1/validators`
- confirm the signer address is part of that validator set
- confirm the proposal height matches the node's `nextHeight` in `GET /v1/consensus`
- confirm the proposal round matches the node's `currentRound` in `GET /v1/consensus`, unless you are intentionally advancing to a higher round
- confirm the proposal signer matches `nextProposer`
- confirm the proposal `previousHash` matches the current chain tip
- confirm the proposal `producedAt`, full `transactions`, and ordered `transactionIds` come from the same `GET /v1/dev/block-template` response as `blockHash`
- confirm votes reference the same `blockHash`, `height`, and `round` as a known proposal
- confirm the signed payload still matches the visible request fields exactly
- inspect `diagnostics` in `GET /v1/status` or `GET /v1/consensus` after a rejection; common codes now include `unexpected_proposer`, `stale_round`, `conflicting_proposal`, `unknown_proposal`, and `template_mismatch`

### Consensus Automation Does Not Fire

- confirm the node started with `ZEPHYR_VALIDATOR_PRIVATE_KEY`; startup rejects automation without it
- confirm the local validator is part of the active validator set shown by `GET /v1/validators`
- confirm `GET /v1/consensus` reports the expected validator in `nextProposer`
- confirm the proposer node still has `ZEPHYR_ENABLE_BLOCK_PRODUCTION=true`
- confirm `GET /v1/status` or `GET /v1/consensus` shows `consensusAutomationEnabled=true`
- confirm `ZEPHYR_CONSENSUS_ROUND_TIMEOUT` is long enough for proposal and vote dissemination in your local setup
- confirm there is at least one queued transaction or a previously stored proposal body when you expect automatic proposal generation
- inspect `roundEvidence` in `GET /v1/status`, `GET /v1/consensus`, or `GET /v1/dev/block-template` to see whether the node is waiting for a proposal, collecting votes, timed out, waiting for reproposal, or already certified
- use `leadingVotePower`, `quorumRemaining`, `pendingReplayRounds`, and `warnings` inside `roundEvidence` to separate partial quorum, timeout, replay backlog, and proposer-schedule problems
- inspect `roundHistory` in those same responses to compare round-0, round-1, and later proposer attempts for the pending height without losing visibility into earlier rounds
- inspect `blockReadiness` in those same responses to see whether the current local template matches a stored proposal, whether a matching certificate exists, and whether a certified stored proposal is already ready for commit or import
- inspect `recovery` in those same responses to see whether the node still has pending replayable local proposal or vote actions, blocked peer-import heights, or a recent snapshot restore after a restart or dropped peer link
- inspect `diagnostics` in those same responses to see whether recent failures were caused by stale rounds, unexpected proposers, missing proposals, template mismatch, missing certificates, or other rejected consensus actions
- inspect `GET /v1/metrics` when you want machine-readable totals for pending replay, diagnostic code frequency, and live peer sync-state distribution during the incident
- inspect `GET /metrics` when you want those same health, recovery, peer, and alert signals in Prometheus-compatible text for scrape-based dashboards or alerts
- inspect `GET /v1/alerts` when you want the current derived critical and warning alerts without reconstructing them from raw health or metric data
- inspect `GET /v1/slo` when you want the same incident evidence projected into compact objective states for readiness, consensus continuity, and peer sync continuity
- inspect `GET /v1/alert-rules` or `GET /v1/alert-rules/prometheus` when you are wiring alert managers or alert rule files and want the bundle Zephyr currently recommends, including peer import, peer admission, and peer replication diagnostics built from retained incident state
- enable `ZEPHYR_ENABLE_STRUCTURED_LOGS=true` when you want those same incident transitions as newline-delimited JSON in the node logs
- inspect `curl.exe -i http://localhost:8080/v1/health` to separate a live node from a ready one; `503` usually means recovery backlog or peer-sync availability has escalated into a hard failure, while `warn` highlights degraded but still serving conditions
- remember the current engine now supports timeout-driven proposer rotation, latest-artifact rebroadcast after peer recovery, restart-safe local proposal or vote replay, pending import recovery, snapshot-restore history, durable peer-incident history, cross-peer `peerSyncSummary`, machine-readable `/v1/metrics`, Prometheus-style `/metrics`, derived `/v1/health`, derived `/v1/alerts`, derived `/v1/slo`, recommended alert-rule bundles, recommended recording-rule bundles, recommended `/v1/dashboards`, exported `/v1/dashboards/grafana`, structured event logs, per-height round history, block readiness inspection, and bounded rejection diagnostics, but broader recovery coverage plus broader dashboard coverage and export adapters are still limited

### Peer Sync Falls Back To Snapshot Restore

- inspect `GET /v1/peers` first; `syncState=snapshot_restored` tells you a peer-specific repair happened, `lastSnapshotRestoreReason` distinguishes `peer_diverged`, `import_repair`, and `fetch_fallback`, `lastReplicationFailureReason` shows whether outgoing dissemination most recently failed on a proposal, vote, or block, and durable incidents plus the per-peer counters keep that story available after restart
- inspect `lastImportErrorCode`, `lastImportFailureHeight`, and `lastImportFailureBlockHash` on that peer view when the repair was triggered by a rejected block import
- inspect `peerSyncSummary` in `GET /v1/status` or `GET /v1/consensus` to see whether the issue is isolated to one peer or part of a broader pattern such as repeated `unreachable` incidents, admission failures, `replication_blocked` proposal or vote churn, or `proposal_required` import blocks across several peers
- inspect `GET /v1/metrics` to compare that durable summary with the live `peerRuntime.bySyncState` distribution, then use Prometheus-facing peer-level gauges such as `zephyr_peer_sync_peer_occurrence_count` plus the reason or error-code rollups when some peers have already recovered and others are still failing
- inspect `diagnostics` in `GET /v1/status` or `GET /v1/consensus`; if the latest `block_import_rejected` entry has `source=peer_sync`, the node hit a block-import problem during background sync before falling back to snapshot restore
- inspect `recovery.pendingImportCount` and `recovery.pendingImportHeights` to see whether the node is still blocked on a peer-import path or whether that backlog has already been cleared
- inspect `recovery.lastSnapshotRestoreAt`, `recovery.lastSnapshotRestoreHeight`, and `recovery.lastSnapshotRestoreBlockHash` to confirm that snapshot repair actually ran and which chain tip it restored
- inspect `recovery.recentActions` for a completed `block_commit` action when you want durable evidence that the local proposer finished a certified commit, or for a completed `block_import` action followed by a completed `snapshot_restore` action when you are debugging catch-up or divergence repair
- remember that peer snapshot restore now preserves the local node's own recovery, diagnostic, and peer-sync incident history, so post-incident inspection stays on the repairing node instead of inheriting the peer's local WAL context

### Peer Identity Verification Or Admission Fails

- confirm the peer validator node is started with `ZEPHYR_VALIDATOR_PRIVATE_KEY`
- confirm the private key is a base64-encoded PKCS#8 P-256 key
- confirm `GET /v1/status` on the remote node includes an `identity` object
- confirm `GET /v1/peers` shows the expected `validatorAddress`, then read `identityError`, `admissionError`, and `syncState` for the exact failure mode
- if you enable `ZEPHYR_REQUIRE_PEER_IDENTITY`, peer-originated replicated POST requests without a valid identity are rejected with `403`
- if you configure `ZEPHYR_PEER_VALIDATORS`, confirm the bound `<peer-url>=<validator-address>` pair matches what the peer proves in `GET /v1/status`

### Certified Block Production Is Rejected

- confirm `GET /v1/consensus` shows a non-empty validator set
- confirm the active `currentRound` and `nextProposer` match the proposal and certificate you expect to commit
- confirm `GET /v1/dev/block-template` and your proposal use the same `blockHash`, `previousHash`, `producedAt`, full `transactions`, and `transactionIds`
- confirm `GET /v1/consensus` shows a latest certificate for that same `height`, `round`, and `blockHash`
- confirm you replay `POST /v1/dev/produce-block` with the same `producedAt` used by the certified template when you are using the manual path
- inspect `blockReadiness` in `GET /v1/status`, `GET /v1/consensus`, or `GET /v1/dev/block-template`; common warnings now include `proposal_missing`, `local_template_mismatch`, `certificate_missing`, and `certified_proposal_differs_from_local_template`
- inspect `diagnostics` in `GET /v1/status` or `GET /v1/consensus`; common commit-side codes now include `proposal_required`, `template_mismatch`, `certificate_required`, and `not_scheduled_proposer`
- disable `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES` only if you intentionally want a looser local dev flow

## Recommended Local Demo Flow

1. Start Node A.
2. Start Node B as a replica or second validator.
3. Start the Vue wallet against Node A.
4. Create a wallet.
5. Fund the wallet with the local dev faucet.
6. Sign and broadcast a sample transaction.
7. Produce a block on Node A or enable automated certified consensus if you already have validator keys.
8. Inspect `GET /v1/status`, `GET /v1/peers`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}` on both nodes.
9. If validator private keys are configured, confirm peer identity verification succeeds in `GET /v1/peers`.
10. Submit a validator election and inspect `GET /v1/validators` plus `GET /v1/consensus`.
11. Either submit a matching proposal and validator votes manually, or let the automated proposer and validators handle the active round.
12. If the active proposer stalls, watch `currentRound` advance and `nextProposer` rotate.
13. Inspect the resulting block, vote tallies, and certificate on both nodes.
14. Optionally restart a node and confirm the validator snapshot, round state, consensus artifacts, and `recovery` state survived.
15. If the restarted node had a pending local proposal or vote, confirm the action is replayed and later marked completed once the block finalizes.







































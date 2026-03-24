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
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/consensus
```

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
- if validator private keys are configured, `GET /v1/status` exposes a signed identity proof, `peerSyncSummary`, and `GET /v1/peers` shows verification, admission state, per-peer sync telemetry, derived incident counters, and durable `recentIncidents` history for configured peers
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
- `GET /v1/peers` shows `expectedValidator`, `admitted`, `admissionError`, `syncState`, `heightDelta`, per-peer `incidentCount`, `incidentOccurrences`, `latestIncidentAt`, the latest import or snapshot-repair metadata, and durable `recentIncidents` history for each configured peer
- background sync and outgoing replication use only admitted peers under this policy
- replicated peer POST requests without a valid identity, or from validators outside the configured binding allowlist, are rejected with `403`

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
- admitted peers replicate the proposal, vote, certificate, and committed block over the current HTTP transport
- `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template` now expose `roundEvidence` so operators can see the active round deadline, proposal presence, leading vote power, quorum remaining, replay backlog, warnings, and certificate state
- those same responses now expose `roundHistory`, which shows the pending height across prior and active rounds so operators can inspect proposer rotation and stalled rounds side by side
- those same responses now expose `blockReadiness`, which shows whether the local template matches stored proposals and certificates and whether commit or import can proceed from stored certified artifacts
- those same responses now expose `recovery`, which shows pending replayable local proposal or vote actions, pending import backlog, and recent replay/completion plus snapshot-restore metadata from the broader local consensus recovery surface
- those same responses now expose `diagnostics`, which show recent rejected proposal, vote, commit, or import actions with stable error codes
- those same responses now expose `peerSyncHistory`, which keeps recent cross-peer sync incidents visible even after restart
- those same responses now expose `peerSyncSummary`, which rolls those incidents up by peer and state so operators can see the dominant network problem quickly
- `GET /v1/metrics` now provides the same durable peer summary alongside machine-readable consensus-action, diagnostic, and live peer-runtime counters for dashboards or automation
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
- remember the current engine now supports timeout-driven proposer rotation, latest-artifact rebroadcast after peer recovery, restart-safe local proposal or vote replay, pending import recovery, snapshot-restore history, durable peer-incident history, cross-peer `peerSyncSummary`, machine-readable `/v1/metrics`, per-height round history, block readiness inspection, and bounded rejection diagnostics, but broader recovery coverage plus structured logs are still limited

### Peer Sync Falls Back To Snapshot Restore

- inspect `GET /v1/peers` first; `syncState=snapshot_restored` tells you a peer-specific repair happened, `lastSnapshotRestoreReason` distinguishes `peer_diverged`, `import_repair`, and `fetch_fallback`, and `recentIncidents` plus the per-peer counters keep that story available after restart
- inspect `lastImportErrorCode`, `lastImportFailureHeight`, and `lastImportFailureBlockHash` on that peer view when the repair was triggered by a rejected block import
- inspect `peerSyncSummary` in `GET /v1/status` or `GET /v1/consensus` to see whether the issue is isolated to one peer or part of a broader state like repeated `unreachable`, `import_blocked`, or `snapshot_restored` incidents across several peers
- inspect `GET /v1/metrics` to compare that durable summary with the live `peerRuntime.bySyncState` distribution when some peers have already recovered and others are still failing
- inspect `diagnostics` in `GET /v1/status` or `GET /v1/consensus`; if the latest `block_import_rejected` entry has `source=peer_sync`, the node hit a block-import problem during background sync before falling back to snapshot restore
- inspect `recovery.pendingImportCount` and `recovery.pendingImportHeights` to see whether the node is still blocked on a peer-import path or whether that backlog has already been cleared
- inspect `recovery.lastSnapshotRestoreAt`, `recovery.lastSnapshotRestoreHeight`, and `recovery.lastSnapshotRestoreBlockHash` to confirm that snapshot repair actually ran and which chain tip it restored
- inspect `recovery.recentActions` for a completed `block_import` action followed by a completed `snapshot_restore` action when you are debugging catch-up or divergence repair
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













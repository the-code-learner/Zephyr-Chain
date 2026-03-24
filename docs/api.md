# Zephyr Chain MVP API

## Base URL

The default local base URL is:

```text
http://localhost:8080
```

Change it with `ZEPHYR_HTTP_ADDR` when starting the node.

## Node Runtime Configuration

- `ZEPHYR_HTTP_ADDR`: HTTP bind address, default `:8080`
- `ZEPHYR_NODE_ID`: node identifier, default `node-local`
- `ZEPHYR_VALIDATOR_ADDRESS`: local validator address for proposer-schedule enforcement and status output, default empty
- `ZEPHYR_VALIDATOR_PRIVATE_KEY`: base64-encoded PKCS#8 P-256 private key used to derive and sign the node transport identity plus automated proposal and vote messages, default empty
- `ZEPHYR_DATA_DIR`: durable node state directory, default `var/node`
- `ZEPHYR_PEERS`: comma-separated peer base URLs, default empty
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval, default `15s`
- `ZEPHYR_CONSENSUS_INTERVAL`: consensus automation ticker interval, default `1s`
- `ZEPHYR_CONSENSUS_ROUND_TIMEOUT`: active round timeout before automation advances to the next round, default `5s`
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval, default `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per block, default `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: enable local block production, default `true`
- `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION`: enable the current timeout-driven automation loop, default `false`
- `ZEPHYR_ENABLE_PEER_SYNC`: enable background peer sync, default `true`
- `ZEPHYR_ENABLE_STRUCTURED_LOGS`: emit newline-delimited JSON event logs for diagnostics, peer incidents, and snapshot recovery, default `false`
- `ZEPHYR_REQUIRE_PEER_IDENTITY`: when `true`, replicated peer POST requests must include a valid signed transport identity, default `false`
- `ZEPHYR_PEER_VALIDATORS`: comma-separated `<peer-url>=<validator-address>` bindings used to pin configured peers to expected validators, default empty
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: when `true`, only the scheduled proposer for the active round may produce the next block once a validator set exists, default `false`
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: when `true`, local block commit and remote block import require a matching proposal and quorum certificate, default `false`

Notes:

- startup rejects `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION=true` unless `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured
- if `ZEPHYR_VALIDATOR_ADDRESS` is also set, startup rejects mismatches between that address and the private key-derived address

## Core Consensus Types

### Proposal

```json
{
  "height": 1,
  "round": 1,
  "blockHash": "<64-hex-block-hash>",
  "previousHash": "",
  "producedAt": "2026-03-24T13:00:00Z",
  "transactionIds": [
    "<64-hex-transaction-id>"
  ],
  "transactions": [
    {
      "from": "zph_sender_a",
      "to": "zph_receiver",
      "amount": 5,
      "nonce": 1,
      "memo": "tx-1",
      "payload": "<canonical-transaction-payload>",
      "publicKey": "<base64-spki-public-key>",
      "signature": "<base64-signature>"
    }
  ],
  "proposer": "zph_validator_b",
  "payload": "{\"blockHash\":\"<64-hex-block-hash>\",\"height\":1,\"previousHash\":\"\",\"producedAt\":\"2026-03-24T13:00:00Z\",\"proposer\":\"zph_validator_b\",\"round\":1,\"transactionIds\":[\"<64-hex-transaction-id>\"]}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "proposedAt": "2026-03-24T13:00:01Z"
}
```

Current meaning:

- `blockHash` must be the derived hash of `height`, `previousHash`, `producedAt`, and ordered `transactionIds`
- `transactions` must be present and must match `transactionIds` in the same order
- the proposer signs that full template commitment, not just a standalone hash string
- the scheduled proposer is derived from both `height` and `round`
- validators can verify the candidate directly from the proposal body without relying on local mempool convergence alone

### Vote

```json
{
  "height": 1,
  "round": 1,
  "blockHash": "<64-hex-block-hash>",
  "voter": "zph_validator_a",
  "payload": "{\"blockHash\":\"<64-hex-block-hash>\",\"height\":1,\"round\":1,\"voter\":\"zph_validator_a\"}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "votedAt": "2026-03-24T13:00:02Z"
}
```

### CommitCertificate

```json
{
  "height": 1,
  "round": 1,
  "blockHash": "<64-hex-block-hash>",
  "votingPower": 43000,
  "quorumVotingPower": 28667,
  "voterCount": 2,
  "voters": ["zph_validator_a", "zph_validator_b"],
  "createdAt": "2026-03-24T13:00:03Z"
}
```

### TransportIdentity

```json
{
  "nodeId": "node-a",
  "validatorAddress": "zph_validator_a",
  "payload": "{\"nodeId\":\"node-a\",\"signedAt\":\"2026-03-24T09:15:00Z\",\"validatorAddress\":\"zph_validator_a\"}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "signedAt": "2026-03-24T09:15:00Z"
}
```

### RoundEvidence

`roundEvidence` is a derived operator-facing view exposed by `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template`.

Current fields include:

- `height`, `round`, `nextProposer`, `startedAt`, `deadlineAt`, and `timedOut`
- `quorumVotingPower`, the target voting power required for certification in the active round
- `state`, which is currently one of `no_validator_set`, `idle`, `waiting_for_proposal`, `waiting_for_reproposal`, `collecting_votes`, or `certified`
- `proposalPresent`, `proposalBlockHash`, and `proposalProposer` for the active round
- `latestKnownProposalRound` and `latestKnownProposalBlockHash` when the node has seen a newer stored proposal for the same height than the currently active round
- `voteTallies` for the active round
- `leadingVoteBlockHash`, `leadingVotePower`, `leadingVoteCount`, `quorumRemaining`, and `partialQuorum` so operators can see whether a round is converging or stalled below quorum
- `localVotePresent` and `localVoteBlockHash` for the local validator when configured
- `pendingReplayCount` and `pendingReplayRounds` to show whether the local node still has replayable actions for the active height
- `certificatePresent` and `certificateBlockHash` when the active round already has a matching quorum certificate
- `warnings`, currently drawn from `timeout_elapsed`, `partial_quorum`, `reproposal_pending`, `replay_pending`, and `proposal_not_from_scheduled_proposer`

### ConsensusRoundHistoryView

`roundHistory` is a derived per-height view exposed by `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template`.

Current fields include:

- `height`, which is currently the pending `nextHeight`
- `rounds`, sorted by round number for that height
- each round entry exposes `round`, `active`, `startedAt`, `scheduledProposer`, `proposalPresent`, `proposalBlockHash`, `proposalProposer`, `voteTallies`, `certificatePresent`, and `certificateBlockHash`

Current behavior:

- the active round is always included for the pending height, even if that round has no stored proposal yet
- prior rounds remain visible when the node advances after timeout or accepts higher-round messages
- operators can compare round-0 and round-1 proposal, vote, and certificate state directly without reconstructing it from logs or diagnostics

### BlockReadiness

`blockReadiness` is a derived next-block readiness view exposed by `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template`.

Current fields include:

- `height`, the current pending `nextHeight`
- `localTemplateAvailable`, `localTemplateBlockHash`, `localTemplateProducedAt`, and `localTemplateTransactionCount`
- `storedProposalCount` and `certifiedProposalCount`
- `matchingLocalProposalRound` and `matchingLocalCertificate`
- `readyToCommitLocalTemplate`, `readyToCommitStoredProposal`, and `readyToImportCertifiedBlock`
- `latestCertifiedRound`, `latestCertifiedBlockHash`, and `latestCertifiedProducedAt`
- `warnings`, currently drawn from `proposal_missing`, `local_template_mismatch`, `certificate_missing`, `certified_proposal_differs_from_local_template`, and `certified_proposal_available_without_local_template`

Current behavior:

- when no proposal exists yet, the view shows whether the node can build a local candidate and warns with `proposal_missing`
- when a proposal exists but lacks quorum, the view shows the matching round but warns with `certificate_missing`
- when a certified proposal exists for the pending height, the view shows whether the current local template still matches it and whether commit or peer import can proceed from stored artifacts
- wrong `producedAt` or wrong imported block attempts now surface as `template_mismatch` in diagnostics instead of looking like a missing proposal

### ConsensusRecoveryView

`recovery` is the durable local consensus-action recovery view exposed by `GET /v1/status`, `GET /v1/consensus`, `GET /v1/dev/block-template`, and local proposal or vote submissions.

Current fields include:

- `pendingActionCount`, `pendingReplayCount`, `pendingImportCount`, `pendingImportHeights`, `needsReplay`, and `needsRecovery`
- `lastSnapshotRestoreAt`, `lastSnapshotRestoreHeight`, and `lastSnapshotRestoreBlockHash`
- `pendingActions`, which now list replayable local actions plus pending import-repair actions that still need follow-up
- `recentActions`, which show the latest local consensus actions with `status`, `replayAttempts`, `lastReplayAt`, and `completedAt`

Current behavior:

- locally authored proposals and votes for the configured validator are persisted into this WAL view
- timeout-driven round advance is also recorded for operator history
- recoverable peer block-import failures now append a pending `block_import` action so operators can see blocked import heights directly in the recovery view
- when automation rebroadcasts a stored local proposal or vote, the matching action updates `replayAttempts` and `lastReplayAt`
- when a block is committed locally or imported for that height, pending proposal, vote, and import-repair actions for that height are marked completed
- when peer sync falls back to snapshot restore, the node preserves its own recovery and diagnostic history, completes any blocked import actions through the restored height, and records a completed `snapshot_restore` action with the restored height and latest block hash

### ConsensusDiagnosticsView

`diagnostics` is a bounded recent rejection-history view exposed by `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template`.

Current fields include:

- `recent`, newest first
- each diagnostic exposes `kind`, `code`, `message`, `height`, `round`, `blockHash`, `validator`, `source`, and `observedAt`

Current behavior:

- rejected proposal submissions append a `proposal_rejected` diagnostic
- rejected vote submissions append a `vote_rejected` diagnostic
- rejected local commit attempts append a `block_commit_rejected` diagnostic
- rejected peer block imports append a `block_import_rejected` diagnostic
- background peer-sync import failures append the same `block_import_rejected` diagnostic with `source` set to `peer_sync` before snapshot fallback
- `code` is a stable operator-facing category such as `unexpected_proposer`, `stale_round`, `conflicting_proposal`, `conflicting_vote`, `proposal_required`, `template_mismatch`, `certificate_required`, or `not_scheduled_proposer`

### PeerSyncHistoryView

`peerSyncHistory` is a bounded durable peer-sync incident view exposed by `GET /v1/status`, `GET /v1/consensus`, and `GET /v1/dev/block-template`.

Current fields include:

- `recent`, newest first
- each incident exposes `peerUrl`, `state`, `reason`, `localHeight`, `peerHeight`, `heightDelta`, `blockHash`, `errorCode`, `errorMessage`, `firstObservedAt`, `lastObservedAt`, and `occurrences`

Current behavior:

- repeated incidents for the same peer and same incident shape are merged into one record with a higher `occurrences` count instead of growing the history indefinitely
- the history survives restart because it is stored in the durable ledger state
- peer snapshot restore preserves the local node's own peer-sync incident history instead of replacing it with the repairing peer's local context

## Consensus Endpoints

### PeerSyncSummaryView

`peerSyncSummary` is a bounded derived cross-peer incident summary exposed by `GET /v1/status`, `GET /v1/consensus`, `GET /v1/dev/block-template`, and `GET /v1/metrics`.

Current fields include:

- `incidentCount`, `affectedPeerCount`, `totalOccurrences`, and `latestObservedAt`
- `states`, where each entry exposes `state`, `incidentCount`, `affectedPeerCount`, `totalOccurrences`, and `latestObservedAt`
- `peers`, where each entry exposes `peerUrl`, `incidentCount`, `totalOccurrences`, `latestState`, `latestReason`, `latestErrorCode`, `latestBlockHash`, and `latestObservedAt`

Current behavior:

- repeated incidents for one peer increase `totalOccurrences` without inflating the distinct `incidentCount`
- the summary is derived from the durable peer-sync incident history, so it survives restart and peer snapshot repair
- state summaries are sorted by dominant total occurrences and peer summaries are sorted by latest observation time

### GET /v1/consensus

Returns the durable validator snapshot, the latest consensus artifacts, and the derived consensus summary for the next height.

Current behavior:

- the response includes `consensusAutomationEnabled`, `structuredLogsEnabled`, `proposerScheduleEnforced`, and `consensusCertificatesRequired`
- `validatorSet` exposes the durable validator snapshot
- `artifacts` exposes the latest stored proposal, votes, and certificate
- `consensus` now includes `currentRound` and `currentRoundStartedAt` in addition to `nextHeight`, `nextProposer`, total voting power, and quorum target
- `nextProposer` reflects the active round, not only the next height
- `roundEvidence` exposes the round deadline, proposal presence, vote tallies, leading vote, quorum remaining, replay backlog, warnings, local vote, and certificate state for operator inspection
- `roundHistory` exposes the pending height across rounds so operators can compare prior and active proposer attempts side by side
- `blockReadiness` exposes whether the current local template matches stored proposals and certificates for the pending height
- `recovery` exposes the local consensus-action WAL, including pending replayable actions, pending import backlog, and recent replay, completion, plus snapshot-restore metadata
- `diagnostics` exposes recent rejected proposal, vote, commit, and import events
- `peerSyncHistory` exposes recent durable cross-peer sync incidents
- `peerSyncSummary` exposes the derived cross-peer totals for those incidents

### POST /v1/consensus/proposals

Validates and persists a signed proposal for the next block height.

Current behavior:

- the proposer must be part of the active validator set
- the proposer must match the scheduled proposer for that height and round
- `previousHash` must match the current chain tip for that height
- `blockHash` must match the proposal's `producedAt` plus ordered `transactionIds`
- `transactions` must be present and must match `transactionIds` in the same order
- the node rejects stale lower-round proposals after it has already moved forward
- a valid higher-round proposal can advance the local active round when needed
- the proposal is stored durably and replicated to admitted peers
- when automation is enabled, the scheduled proposer uses the same validation path internally before broadcasting the proposal
- when the proposal is authored by the node's configured local validator, the node persists a local recovery action for restart replay
- when automation is enabled and a peer link comes back, the proposer can rebroadcast its latest stored proposal for the pending height until a matching certificate exists
- rejected proposals are appended to the diagnostic history exposed by status and consensus surfaces

### POST /v1/consensus/votes

Validates and persists a signed validator vote for a known proposal.

Current behavior:

- the voter must be part of the active validator set
- the vote must target a known proposal for that height and round
- duplicate same-block votes from the same validator are idempotent
- the node rejects stale lower-round votes after it has already moved forward
- a valid higher-round vote can advance the local active round when needed, as long as the referenced proposal is known
- if the accumulated vote power reaches quorum, the node stores a commit certificate artifact
- when certificate enforcement is enabled, that certificate can unlock local commit and remote import for the matching block hash
- when automation is enabled, active validators use the same validation path internally before broadcasting their vote
- when the vote is authored by the node's configured local validator, the node persists a local recovery action for restart replay
- when automation is enabled and a peer link comes back, validators can rebroadcast their latest stored vote for the pending height until the matching certificate exists
- rejected votes are appended to the diagnostic history exposed by status and consensus surfaces

## Runtime And Ledger Endpoints

### GET /health

Returns a simple liveness response.

### GET /metrics

Returns a Prometheus-compatible text export derived from the same durable and live operator signals exposed by `GET /v1/metrics` and `GET /v1/health`.

Current behavior:

- the response uses `text/plain; version=0.0.4; charset=utf-8`
- the endpoint keeps returning `200` while the HTTP API is alive; readiness is exported through `zephyr_node_ready`, `zephyr_health_status`, and `zephyr_health_check_status` instead of surfacing failure as HTTP `503`
- the current metric families cover node flags, chain height and mempool size, consensus height and round state, recovery backlog, retained consensus action history, retained consensus diagnostic buckets, live peer runtime counts, and durable peer-sync incident summaries
- `GET /v1/metrics` remains the structured JSON surface for automation that wants typed objects, while `GET /metrics` is the scrape-friendly adapter for Prometheus-style monitoring stacks

### GET /v1/health

Returns a derived readiness response built from durable ledger state plus the latest live peer-runtime view.

Current behavior:

- `/health` remains a simple liveness probe, while `/v1/health` is the richer readiness surface for operators and automation
- the top-level response includes `generatedAt`, node identity, peer count, runtime flags, `live`, `ready`, `status`, ordered `checks`, and flattened `warnings`
- `status` is currently one of `ok`, `warn`, or `fail`, while each check uses `pass`, `warn`, or `fail`
- the current checks are `api`, `validator_set`, `recovery`, `consensus`, `peer_sync`, and `diagnostics`
- `warn` checks keep the node live and ready but surface degraded conditions such as recent diagnostics, early peer observation, or consensus warnings
- `fail` checks set `ready=false` and return HTTP `503`; the current hard-fail cases are recovery backlog and peer-sync availability failures when peer sync is enabled
- `warnings` is a flattened operator-facing list built from the active warn or fail checks so dashboards do not need to re-derive short incident summaries

### GET /v1/status

Returns the local runtime status for the current node, including consensus summary and whether proposer or certificate enforcement is enabled.

Current behavior:

- the response includes `consensusAutomationEnabled` and `structuredLogsEnabled`
- the embedded `consensus` view now exposes `currentRound`, `currentRoundStartedAt`, and the active-round `nextProposer`
- `roundEvidence` exposes the active round deadline, state, vote tallies, leading vote, quorum remaining, replay backlog, warnings, proposal presence, local vote, and certificate visibility for operators
- `roundHistory` exposes the pending height across rounds so operators can inspect round-0, round-1, and later attempts together
- `blockReadiness` exposes whether the local template is ready to commit and whether a certified stored proposal is already ready for commit or import
- `recovery` exposes pending replayable local actions, pending import backlog, and recent replay/completion plus snapshot-restore metadata from the local consensus-action WAL
- `diagnostics` exposes recent rejected proposal, vote, commit, and import events
- `peerSyncHistory` exposes a durable recent history of cross-peer sync incidents, including repeated failures merged by occurrence count
- `peerSyncSummary` exposes affected-peer totals, dominant states, and the latest incident summary across peers
- `GET /v1/metrics` offers a machine-readable roll-up of that durable summary plus live peer runtime counts
- `GET /metrics` offers a Prometheus-style text projection of the same operator signals for scrape-based monitoring and alerting
- `GET /v1/health` offers a pass, warn, or fail readiness summary derived from the same durable and live operator signals; unlike `/health`, it can return HTTP `503` when fail checks are active
- when `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured, the response includes an `identity` object with a signed transport proof for the local validator
- `peerIdentityRequired` is `true` when strict peer admission or explicit peer-validator binding is enabled

### GET /v1/metrics

Returns a machine-readable observability snapshot built from durable ledger state plus the latest live peer runtime views.

Current behavior:

- the top-level response includes `generatedAt`, node identity, runtime flags including `structuredLogsEnabled`, and embedded `status`, `consensus`, and `recovery` summaries
- `consensusActions` rolls up the durable local WAL and recovery actions into `totalCount`, `pendingCount`, `totalReplayAttempts`, latest record or completion times, and `byType` or `byStatus` buckets
- `diagnostics` rolls up the bounded rejection history into `totalCount`, `latestObservedAt`, and `byKind`, `byCode`, or `bySource` buckets
- `peerSyncSummary` reuses the durable cross-peer incident summary also exposed by status, consensus, and block-template responses
- `peerRuntime` reflects the current configured peer set and live `syncState` distribution, including reachable or admitted counts versus unreachable or unadmitted counts
- unlike `peerSyncSummary`, `peerRuntime` is derived from the latest in-memory peer view and may reset on process restart until peers are seen again
- `GET /metrics` reuses these same rollups in Prometheus-compatible text form, including readiness gauges such as `zephyr_node_ready` and `zephyr_health_check_status`

### Structured Event Logs

When `ZEPHYR_ENABLE_STRUCTURED_LOGS=true`, the node emits newline-delimited JSON event logs alongside the existing text startup log.

Current behavior:

- every entry includes `timestamp`, `level`, `component`, `event`, `nodeId`, and optional `validatorAddress`
- consensus diagnostic entries use `component=consensus` and `event=diagnostic`, then add `kind`, `code`, `message`, `height`, `round`, `blockHash`, `validator`, `source`, and `observedAt`
- peer incident entries use `component=peer_sync` and `event=incident`, then add `peerUrl`, `state`, `reason`, `localHeight`, `peerHeight`, `heightDelta`, `blockHash`, `errorCode`, `errorMessage`, `firstObservedAt`, `lastObservedAt`, and `occurrences`
- snapshot restore entries use `component=recovery` and `event=snapshot_restore`, then add `peer`, `height`, `blockHash`, and `restoredAt`
- the current structured-log surface is intentionally narrow: it focuses on consensus rejection, peer incident, and snapshot-repair paths so operators can correlate the same events exposed by `diagnostics`, `peerSyncHistory`, `GET /v1/metrics`, `GET /metrics`, and the higher-level readiness summaries from `GET /v1/health`

### GET /v1/peers

Returns the latest known view of configured peers.

Current behavior:

- each peer view includes the remote `validatorAddress` when advertised
- `expectedValidator`, `admitted`, and `admissionError` show the local admission policy and whether the peer passed it
- `identityPresent`, `identityVerified`, and `identityError` show whether the peer exposed a signed transport identity and whether local verification succeeded
- `heightDelta` and `syncState` show whether the peer is aligned, ahead, behind, divergent, unadmitted, unreachable, blocked on import, or was recently repaired through snapshot restore
- `lastSyncAttemptAt` and `lastSyncSuccessAt` show the last peer-sync attempt and completion times for that peer
- `lastImportErrorCode`, `lastImportErrorMessage`, `lastImportFailureAt`, `lastImportFailureHeight`, and `lastImportFailureBlockHash` show the most recent import-side failure observed while syncing from that peer
- `lastSnapshotRestoreAt`, `lastSnapshotRestoreHeight`, `lastSnapshotRestoreBlockHash`, and `lastSnapshotRestoreReason` show the latest snapshot-based repair event for that peer, with reasons currently drawn from `fetch_fallback`, `import_repair`, and `peer_diverged`
- `incidentCount`, `incidentOccurrences`, and `latestIncidentAt` expose the derived per-peer counters from the durable incident history
- `recentIncidents` exposes the durable per-peer incident history the node kept on disk, including state, reason, local and peer heights, block hash, error details, first and last observation time, and merged occurrence count
- when strict peer admission or peer binding is enabled, background sync and outgoing replication use only admitted peers

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, persists it durably in the ledger, increments the validator-set version, resets pending proposal, vote, and certificate artifacts, and resets the active round to height `nextHeight`, round `0`.

### GET /v1/validators

Returns the latest durable validator snapshot produced by `POST /v1/election`.

### GET /v1/accounts/{address}

Returns the current persisted account view for the requested address.

### GET /v1/blocks/latest

Returns the latest committed local block.

### GET /v1/blocks/{height}

Returns a committed block by exact height.

### POST /v1/dev/faucet

Credits a local account for development and testing.

### POST /v1/transactions

Accepts a signed transaction envelope and queues it in the node's persisted mempool after validation.

### GET /v1/dev/block-template

Builds and returns the deterministic next block candidate from the current mempool and chain tip.

Current behavior:

- the response includes the exact `blockHash`, `previousHash`, `producedAt`, full `transactions`, and ordered `transactionIds` validators should certify
- operators can use that data directly when constructing a signed self-contained proposal
- the response also includes the current consensus summary, `roundEvidence`, `roundHistory`, `blockReadiness`, `recovery`, `diagnostics`, `peerSyncHistory`, `peerSyncSummary`, and latest durable artifacts for operator context

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool or a stored certified proposal.

Behavior:

- with no JSON body, the node uses the current time as the block timestamp for ungated local production
- you may send `{ "producedAt": "<RFC3339 timestamp>" }` to target a specific previously fetched block template or a specific stored certified proposal
- if proposer-schedule enforcement is enabled, the endpoint returns `409` when the local validator is not the scheduled proposer for the active round
- if certificate enforcement is enabled, the endpoint returns `409` unless the resulting block exactly matches a stored proposal template and quorum certificate
- when certificate enforcement is enabled and a matching certified proposal exists, the node can commit from the stored proposal body even if the local mempool no longer contains those transactions
- when automation is enabled, the scheduled proposer may reach the same commit path without an operator POST as soon as quorum exists for its current round proposal
- rejected local commit attempts are appended to the diagnostic history exposed by status and consensus surfaces
- wrong `producedAt` for an otherwise known certified proposal now reports `template_mismatch` instead of `proposal_required`

## Internal Node-To-Node Endpoints

These endpoints are used by the current devnet sync layer. They exist for node replication, not wallet clients.

When `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured, replicated POST requests carry these signed source headers:

- `X-Zephyr-Source-Node`
- `X-Zephyr-Source-Validator`
- `X-Zephyr-Source-Identity-Payload`
- `X-Zephyr-Source-Public-Key`
- `X-Zephyr-Source-Signature`
- `X-Zephyr-Source-Signed-At`

Current behavior:

- if signed transport-identity headers are present, they must be complete and valid or the request is rejected with `400`
- when `ZEPHYR_REQUIRE_PEER_IDENTITY=true`, replicated peer POST requests must include a valid signed transport identity or they are rejected with `403`
- when `ZEPHYR_PEER_VALIDATORS` is configured, replicated peer POST requests are also rejected with `403` unless the proven validator belongs to the configured peer-binding allowlist
- proposal, vote, and block dissemination for the current automation flow use these same admitted peer paths
- the automation path now sends proposals before votes to avoid vote-before-proposal races on the happy path
- the automation loop also rebroadcasts the latest stored proposal and latest stored local vote for the pending height until a matching certificate exists, which helps delayed peers recover on the current HTTP devnet
- `GET /v1/peers` now shows whether a given peer most recently aligned normally, fell back to snapshot restore, or triggered an import-side repair path during sync, and `recentIncidents` keeps that peer history visible after restart

### POST /v1/internal/blocks

Imports a committed block from another node.

If certificate enforcement is enabled on the receiving node, the imported block must match a stored proposal template and quorum certificate or the import is rejected.

Rejected imports are appended to the diagnostic history exposed by status and consensus surfaces.

If proposals exist for that height but the imported block does not match any stored proposal template, the rejection now reports `template_mismatch`.

### GET /v1/internal/snapshot

Returns the current durable node snapshot used for catch-up restore.

When another node applies this snapshot through peer sync, it preserves its own local recovery, diagnostic, peer-sync incident history, and derived peer-sync summary context instead of replacing that operator context with the peer's local WAL or diagnostics.






















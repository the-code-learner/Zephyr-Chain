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
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval, default `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per block, default `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: enable local block production, default `true`
- `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION`: enable the current round-0 automation loop, default `false`
- `ZEPHYR_ENABLE_PEER_SYNC`: enable background peer sync, default `true`
- `ZEPHYR_REQUIRE_PEER_IDENTITY`: when `true`, replicated peer POST requests must include a valid signed transport identity, default `false`
- `ZEPHYR_PEER_VALIDATORS`: comma-separated `<peer-url>=<validator-address>` bindings used to pin configured peers to expected validators, default empty
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: when `true`, only the scheduled proposer may produce the next block once a validator set exists, default `false`
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: when `true`, local block commit and remote block import require a matching proposal and quorum certificate, default `false`

Notes:

- startup rejects `ZEPHYR_ENABLE_CONSENSUS_AUTOMATION=true` unless `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured
- if `ZEPHYR_VALIDATOR_ADDRESS` is also set, startup rejects mismatches between that address and the private key-derived address

## Core Consensus Types

### Proposal

```json
{
  "height": 1,
  "round": 0,
  "blockHash": "<64-hex-block-hash>",
  "previousHash": "",
  "producedAt": "2026-03-23T15:32:00Z",
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
  "proposer": "zph_validator_a",
  "payload": "{\"blockHash\":\"<64-hex-block-hash>\",\"height\":1,\"previousHash\":\"\",\"producedAt\":\"2026-03-23T15:32:00Z\",\"proposer\":\"zph_validator_a\",\"round\":0,\"transactionIds\":[\"<64-hex-transaction-id>\"]}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "proposedAt": "2026-03-23T15:32:02Z"
}
```

Current meaning:

- `blockHash` must be the derived hash of `height`, `previousHash`, `producedAt`, and ordered `transactionIds`
- `transactions` must be present and must match `transactionIds` in the same order
- the proposer signs that full template commitment, not just a standalone hash string
- validators can verify the candidate directly from the proposal body without relying on local mempool convergence alone

### Vote

```json
{
  "height": 1,
  "round": 0,
  "blockHash": "<64-hex-block-hash>",
  "voter": "zph_validator_b",
  "payload": "{\"blockHash\":\"<64-hex-block-hash>\",\"height\":1,\"round\":0,\"voter\":\"zph_validator_b\"}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "votedAt": "2026-03-23T15:32:05Z"
}
```

### CommitCertificate

```json
{
  "height": 1,
  "round": 0,
  "blockHash": "<64-hex-block-hash>",
  "votingPower": 43000,
  "quorumVotingPower": 28667,
  "voterCount": 2,
  "voters": ["zph_validator_a", "zph_validator_b"],
  "createdAt": "2026-03-23T15:32:06Z"
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

Current meaning:

- this proof exists when `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured
- peers verify the proof when reading `GET /v1/status`, and validator nodes attach the same proof to replicated POST requests
- when `ZEPHYR_REQUIRE_PEER_IDENTITY=true`, replicated peer POST requests without a valid proof are rejected with `403`
- when `ZEPHYR_PEER_VALIDATORS` is configured, the receiving node also checks that the proven validator belongs to its configured peer-binding allowlist

## Consensus Endpoints

### GET /v1/consensus

Returns the durable validator snapshot, the latest consensus artifacts, and the derived consensus summary for the next height.

Current behavior:

- the response includes `consensusAutomationEnabled`, `proposerScheduleEnforced`, and `consensusCertificatesRequired`
- `validatorSet` exposes the durable validator snapshot
- `artifacts` exposes the latest stored proposal, votes, and certificate
- `consensus` exposes the derived summary for `nextHeight`, `nextProposer`, total voting power, and quorum target

### POST /v1/consensus/proposals

Validates and persists a signed proposal for the next block height.

Current behavior:

- the proposer must be part of the active validator set
- the proposer must match the scheduled proposer for that height
- `previousHash` must match the current chain tip for that height
- `blockHash` must match the proposal's `producedAt` plus ordered `transactionIds`
- `transactions` must be present and must match `transactionIds` in the same order
- the proposal is stored durably and replicated to admitted peers when strict peer admission is enabled
- the proposal becomes part of the block-gating path when certificate enforcement is enabled
- when automation is enabled, the scheduled proposer uses the same validation path internally before broadcasting the proposal

### POST /v1/consensus/votes

Validates and persists a signed validator vote for a known proposal.

Current behavior:

- the voter must be part of the active validator set
- the vote must target a known proposal for that height and round
- duplicate same-block votes from the same validator are idempotent
- if the accumulated vote power reaches quorum, the node stores a commit certificate artifact
- when certificate enforcement is enabled, that certificate can unlock local commit and remote import for the matching block hash
- when automation is enabled, active validators use the same validation path internally before broadcasting their vote

## Runtime And Ledger Endpoints

### GET /health

Returns a simple liveness response.

### GET /v1/status

Returns the local runtime status for the current node, including consensus summary and whether proposer or certificate enforcement is enabled.

Current behavior:

- the response includes `consensusAutomationEnabled`
- when `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured, the response includes an `identity` object with a signed transport proof for the local validator
- `peerIdentityRequired` is `true` when strict peer admission or explicit peer-validator binding is enabled
- if `ZEPHYR_VALIDATOR_ADDRESS` is also configured, startup rejects mismatches between the configured address and the private key-derived address

### GET /v1/peers

Returns the latest known view of configured peers.

Current behavior:

- each peer view includes the remote `validatorAddress` when advertised
- `expectedValidator`, `admitted`, and `admissionError` show the local admission policy and whether the peer passed it
- `identityPresent`, `identityVerified`, and `identityError` show whether the peer exposed a signed transport identity and whether local verification succeeded
- when strict peer admission or peer binding is enabled, background sync and outgoing replication use only admitted peers

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, persists it durably in the ledger, increments the validator-set version, and resets pending proposal, vote, and certificate artifacts.

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
- the response also includes the current consensus summary and latest durable artifacts for operator context

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool or a stored certified proposal.

Behavior:

- with no JSON body, the node uses the current time as the block timestamp for ungated local production
- you may send `{ "producedAt": "<RFC3339 timestamp>" }` to target a specific previously fetched block template or a specific stored certified proposal
- if proposer-schedule enforcement is enabled, the endpoint returns `409` when the local validator is not the scheduled proposer for the next height
- if certificate enforcement is enabled, the endpoint returns `409` unless the resulting block exactly matches a stored proposal template and quorum certificate
- when certificate enforcement is enabled and a matching certified proposal exists, the node can commit from the stored proposal body even if the local mempool no longer contains those transactions
- when automation is enabled, the scheduled proposer may reach the same commit path without an operator POST as soon as quorum exists for its current proposal

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

### POST /v1/internal/blocks

Imports a committed block from another node.

If certificate enforcement is enabled on the receiving node, the imported block must match a stored proposal template and quorum certificate or the import is rejected.

### GET /v1/internal/snapshot

Returns the current durable node snapshot used for catch-up restore.

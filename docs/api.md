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
- `ZEPHYR_VALIDATOR_ADDRESS`: local validator address for proposer-schedule enforcement, default empty
- `ZEPHYR_DATA_DIR`: durable node state directory, default `var/node`
- `ZEPHYR_PEERS`: comma-separated peer base URLs, default empty
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval, default `15s`
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval, default `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per block, default `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: enable local block production, default `true`
- `ZEPHYR_ENABLE_PEER_SYNC`: enable background peer sync, default `true`
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: when `true`, only the scheduled proposer may produce the next block once a validator set exists, default `false`
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: when `true`, local block commit and remote block import require a matching proposal and quorum certificate, default `false`

## Core Consensus Types

### Proposal

```json
{
  "height": 1,
  "round": 0,
  "blockHash": "<64-hex-block-hash>",
  "previousHash": "",
  "proposer": "zph_validator_a",
  "payload": "{\"blockHash\":\"<64-hex-block-hash>\",\"height\":1,\"previousHash\":\"\",\"proposer\":\"zph_validator_a\",\"round\":0}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>",
  "proposedAt": "2026-03-23T15:32:00Z"
}
```

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

### ConsensusArtifactsView

```json
{
  "latestProposal": {
    "height": 1,
    "round": 0,
    "blockHash": "<64-hex-block-hash>",
    "previousHash": "",
    "proposer": "zph_validator_a",
    "payload": "...",
    "publicKey": "<base64-spki-public-key>",
    "signature": "<base64-signature>",
    "proposedAt": "2026-03-23T15:32:00Z"
  },
  "latestCertificate": {
    "height": 1,
    "round": 0,
    "blockHash": "<64-hex-block-hash>",
    "votingPower": 43000,
    "quorumVotingPower": 28667,
    "voterCount": 2,
    "voters": ["zph_validator_a", "zph_validator_b"],
    "createdAt": "2026-03-23T15:32:06Z"
  },
  "voteTallies": [
    {
      "height": 1,
      "round": 0,
      "blockHash": "<64-hex-block-hash>",
      "voteCount": 2,
      "votingPower": 43000,
      "quorumReached": true
    }
  ],
  "proposalCount": 1,
  "voteCount": 2,
  "certificateCount": 1
}
```

### ConsensusView

```json
{
  "currentHeight": 0,
  "nextHeight": 1,
  "validatorSetVersion": 1,
  "validatorSetUpdatedAt": "2026-03-23T15:31:30Z",
  "validatorCount": 2,
  "totalVotingPower": 43000,
  "quorumVotingPower": 28667,
  "nextProposer": "zph_validator_a"
}
```

## Consensus Endpoints

### GET /v1/consensus

Returns the durable validator snapshot, the latest consensus artifacts, and the derived consensus summary for the next height.

### POST /v1/consensus/proposals

Validates and persists a signed proposal for the next block height.

Current behavior:

- the proposer must be part of the active validator set
- the proposer must match the scheduled proposer for that height
- `previousHash` must match the current chain tip for that height
- the proposal is stored durably and replicated to configured peers
- the proposal becomes part of the block-gating path when certificate enforcement is enabled

### POST /v1/consensus/votes

Validates and persists a signed validator vote for a known proposal.

Current behavior:

- the voter must be part of the active validator set
- the vote must target a known proposal for that height and round
- duplicate same-block votes from the same validator are idempotent
- if the accumulated vote power reaches quorum, the node stores a commit certificate artifact
- when certificate enforcement is enabled, that certificate can unlock local commit and remote import for the matching block hash

## Runtime And Ledger Endpoints

### GET /health

Returns a simple liveness response.

### GET /v1/status

Returns the local runtime status for the current node, including consensus summary and whether proposer or certificate enforcement is enabled.

### GET /v1/peers

Returns the latest known view of configured peers.

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, persists it durably in the ledger, increments the validator-set version, and resets pending proposal/vote/certificate artifacts.

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

- the response includes the exact block hash and `producedAt` timestamp for that candidate
- operators can use that data to construct a matching proposal and gather votes
- the response also includes the current consensus summary and latest durable artifacts for operator context

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool.

Behavior:

- with no JSON body, the node uses the current time as the block timestamp
- you may send `{ "producedAt": "<RFC3339 timestamp>" }` to reproduce a previously fetched block template
- if proposer-schedule enforcement is enabled, the endpoint returns `409` when the local validator is not the scheduled proposer for the next height
- if certificate enforcement is enabled, the endpoint returns `409` unless the resulting block hash matches a stored proposal and quorum certificate

## Internal Node-To-Node Endpoints

These endpoints are used by the current devnet sync layer. They exist for node replication, not wallet clients.

### POST /v1/internal/blocks

Imports a committed block from another node.

If certificate enforcement is enabled on the receiving node, the imported block must match a stored proposal and quorum certificate or the import is rejected.

### GET /v1/internal/snapshot

Returns the current durable node snapshot used for catch-up restore.

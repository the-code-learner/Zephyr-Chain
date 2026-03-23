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

### ConsensusVote

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

### VoteTally

```json
{
  "height": 1,
  "round": 0,
  "blockHash": "<64-hex-block-hash>",
  "voteCount": 2,
  "votingPower": 43000,
  "quorumReached": true
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
- the proposal is stored durably and replicated to configured peers
- accepting a proposal does not yet commit a block

### POST /v1/consensus/votes

Validates and persists a signed validator vote for a known proposal.

Current behavior:

- the voter must be part of the active validator set
- the vote must target a known proposal for that height and round
- duplicate same-block votes from the same validator are idempotent
- if the accumulated vote power reaches quorum, the node stores a commit certificate artifact
- accepting votes does not yet force block commit; that is a later roadmap step

## Existing Runtime And Ledger Endpoints

### GET /health

Returns a simple liveness response.

### GET /v1/status

Returns the local runtime status for the current node, including consensus summary.

### GET /v1/peers

Returns the latest known view of configured peers.

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, persists it durably in the ledger, increments the validator-set version, and returns the resulting consensus summary.

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

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool.

If proposer-schedule enforcement is enabled and a validator set exists, the endpoint returns `409` when the local validator is not the scheduled proposer for the next height.

## Internal Node-To-Node Endpoints

These endpoints are used by the current devnet sync layer. They exist for node replication, not wallet clients.

### POST /v1/internal/blocks

Imports a committed block from another node.

### GET /v1/internal/snapshot

Returns the current durable node snapshot used for catch-up restore.

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
- `ZEPHYR_DATA_DIR`: durable node state directory, default `var/node`
- `ZEPHYR_PEERS`: comma-separated peer base URLs, default empty
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval, default `15s`
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval, default `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per block, default `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: enable local block production, default `true`
- `ZEPHYR_ENABLE_PEER_SYNC`: enable background peer sync, default `true`

## Types

### Candidate

```json
{
  "address": "validator-a",
  "selfStake": 20000,
  "commissionRate": 0.1,
  "missedBlocks": 2
}
```

### Vote

```json
{
  "delegator": "delegator-1",
  "candidate": "validator-a",
  "amount": 5000
}
```

### Validator

```json
{
  "rank": 1,
  "address": "validator-a",
  "votingPower": 25000,
  "selfStake": 20000,
  "delegatedStake": 5000,
  "commissionRate": 0.1,
  "eligibilityNote": ""
}
```

### ElectionConfig

```json
{
  "maxValidators": 21,
  "minSelfStake": 10000,
  "maxMissedBlocks": 50
}
```

If a numeric field is `0`, the service applies its built-in default for that field.

### BroadcastTransactionRequest

```json
{
  "from": "zph_sender",
  "to": "zph_receiver",
  "amount": 1,
  "nonce": 1,
  "memo": "Genesis wallet test transfer",
  "payload": "{\"amount\":1,\"from\":\"zph_sender\",\"memo\":\"Genesis wallet test transfer\",\"nonce\":1,\"to\":\"zph_receiver\"}",
  "publicKey": "<base64-spki-public-key>",
  "signature": "<base64-signature>"
}
```

Current node behavior:

- `payload` must match the canonical transaction JSON exactly
- `from` must match the address derived from `publicKey`
- the node verifies the P-256 signature over `payload`
- the node enforces duplicate detection, next-nonce rules, and available-balance checks before mempool admission
- accepted transactions are persisted in local node state until they are committed into a block
- when peers are configured, locally accepted transactions are also forwarded to them over HTTP

### BroadcastTransactionResponse

```json
{
  "id": "<transaction-hash>",
  "accepted": true,
  "queuedAt": "2026-03-23T15:30:00Z",
  "mempoolSize": 1
}
```

### AccountView

```json
{
  "address": "zph_sender",
  "balance": 100,
  "availableBalance": 75,
  "nonce": 0,
  "nextNonce": 2,
  "pendingTransactions": 1
}
```

### StatusView

```json
{
  "height": 1,
  "latestBlockHash": "<block-hash>",
  "latestBlockAt": "2026-03-23T15:31:00Z",
  "mempoolSize": 0
}
```

### StatusResponse

```json
{
  "nodeId": "node-a",
  "peerCount": 1,
  "blockProduction": true,
  "peerSyncEnabled": true,
  "status": {
    "height": 1,
    "latestBlockHash": "<block-hash>",
    "latestBlockAt": "2026-03-23T15:31:00Z",
    "mempoolSize": 0
  }
}
```

### PeerView

```json
{
  "url": "http://localhost:8081",
  "nodeId": "node-b",
  "height": 1,
  "latestBlockHash": "<block-hash>",
  "mempoolSize": 0,
  "blockProduction": false,
  "lastSeenAt": "2026-03-23T15:31:05Z",
  "reachable": true,
  "error": ""
}
```

### Block

```json
{
  "height": 1,
  "hash": "<block-hash>",
  "previousHash": "",
  "producedAt": "2026-03-23T15:31:00Z",
  "transactionCount": 1,
  "transactionIds": ["<transaction-hash>"],
  "transactions": [
    {
      "from": "zph_sender",
      "to": "zph_receiver",
      "amount": 25,
      "nonce": 1,
      "memo": "Genesis wallet test transfer",
      "payload": "...",
      "publicKey": "<base64-spki-public-key>",
      "signature": "<base64-signature>"
    }
  ]
}
```

### FaucetRequest

```json
{
  "requestId": "fund-node-a-123456789",
  "address": "zph_sender",
  "amount": 100
}
```

`requestId` is optional for client calls. The node uses it internally to make replicated faucet credits idempotent across peers.

## Endpoints

### GET /health

Returns a simple liveness response.

```bash
curl http://localhost:8080/health
```

### GET /v1/status

Returns the local runtime status for the current node.

```bash
curl http://localhost:8080/v1/status
```

### GET /v1/peers

Returns the latest known view of configured peers.

```bash
curl http://localhost:8080/v1/peers
```

If no peer sync has happened yet, peers may appear with only their configured URL.

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, then stores the result as the current local validator snapshot.

### GET /v1/validators

Returns the latest validator snapshot produced by `POST /v1/election`.

### GET /v1/accounts/{address}

Returns the current persisted account view for the requested address.

```bash
curl http://localhost:8080/v1/accounts/zph_sender
```

### GET /v1/blocks/latest

Returns the latest committed local block.

```bash
curl http://localhost:8080/v1/blocks/latest
```

If no block has been committed yet, the endpoint returns `404`.

### GET /v1/blocks/{height}

Returns a committed block by exact height.

```bash
curl http://localhost:8080/v1/blocks/1
```

### POST /v1/dev/faucet

Credits a local account for development and testing.

```bash
curl -X POST http://localhost:8080/v1/dev/faucet \
  -H "Content-Type: application/json" \
  -d '{
    "address": "zph_sender",
    "amount": 100
  }'
```

When peers are configured, the node forwards the same funding request to them with an internal source header so replicated credits stay idempotent.

### POST /v1/transactions

Accepts a signed transaction envelope and queues it in the node's persisted mempool after validation.

```bash
curl -X POST http://localhost:8080/v1/transactions \
  -H "Content-Type: application/json" \
  -d '{
    "from": "zph_sender",
    "to": "zph_receiver",
    "amount": 25,
    "nonce": 1,
    "memo": "Genesis wallet test transfer",
    "payload": "{\"amount\":25,\"from\":\"zph_sender\",\"memo\":\"Genesis wallet test transfer\",\"nonce\":1,\"to\":\"zph_receiver\"}",
    "publicKey": "<base64-spki-public-key>",
    "signature": "<base64-signature>"
  }'
```

Meaning:

- `accepted` means the transaction passed validation and was queued in the local durable mempool
- when peers are configured, the node also schedules replication to them
- it still does not imply validator agreement or finality

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool.

```bash
curl -X POST http://localhost:8080/v1/dev/produce-block
```

If block production is disabled on that node, the endpoint returns `409`.

## Internal Node-To-Node Endpoints

These endpoints are used by the current HTTP devnet sync layer. They exist for node replication, not wallet clients.

### POST /v1/internal/blocks

Imports a committed block from another node.

### GET /v1/internal/snapshot

Returns the current durable node snapshot used for catch-up restore.

## PowerShell Example

The following example funds an account, submits a transaction, forces a block on one node, and inspects peer state from PowerShell:

```powershell
$faucet = @{ address = "zph_sender"; amount = 100 } | ConvertTo-Json
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/dev/faucet" -ContentType "application/json" -Body $faucet

$tx = @{
  from = "zph_sender"
  to = "zph_receiver"
  amount = 25
  nonce = 1
  memo = "Genesis wallet test transfer"
  payload = '{"amount":25,"from":"zph_sender","memo":"Genesis wallet test transfer","nonce":1,"to":"zph_receiver"}'
  publicKey = "<base64-spki-public-key>"
  signature = "<base64-signature>"
} | ConvertTo-Json

Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/transactions" -ContentType "application/json" -Body $tx
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/dev/produce-block"
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/blocks/latest"
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/peers"
```

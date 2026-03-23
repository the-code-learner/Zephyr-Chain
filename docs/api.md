# Zephyr Chain MVP API

## Base URL

The default local base URL is:

```text
http://localhost:8080
```

You can change it with `ZEPHYR_HTTP_ADDR` when starting the node.

## Node Runtime Configuration

- `ZEPHYR_HTTP_ADDR`: HTTP bind address, default `:8080`
- `ZEPHYR_DATA_DIR`: durable node state directory, default `var/node`
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval, default `15s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per block, default `100`

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
- accepted transactions are persisted in the local node state until they are committed into a block

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

- `balance`: current committed account balance
- `availableBalance`: balance left after reserving amounts for pending mempool transactions
- `nonce`: latest committed nonce in account state
- `nextNonce`: next nonce expected for the sender
- `pendingTransactions`: number of accepted mempool transactions reserved for this account

### StatusView

```json
{
  "height": 1,
  "latestBlockHash": "<block-hash>",
  "latestBlockAt": "2026-03-23T15:31:00Z",
  "mempoolSize": 0
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
  "address": "zph_sender",
  "amount": 100
}
```

This is a development-only funding helper for the current MVP.

## Endpoints

### GET /health

Returns a simple liveness response.

```bash
curl http://localhost:8080/health
```

### GET /v1/status

Returns the local chain status for the current node.

```bash
curl http://localhost:8080/v1/status
```

Response:

```json
{
  "status": {
    "height": 1,
    "latestBlockHash": "<block-hash>",
    "latestBlockAt": "2026-03-23T15:31:00Z",
    "mempoolSize": 0
  }
}
```

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

### POST /v1/transactions

Accepts a signed transaction envelope and queues it in the node's persisted local mempool after validation.

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

- `accepted` means the transaction passed validation and was queued in the node's persisted local mempool
- it does not imply peer replication or network finality yet

### POST /v1/dev/produce-block

Forces immediate block production from the current local mempool.

```bash
curl -X POST http://localhost:8080/v1/dev/produce-block
```

This endpoint is intended for local development and tests. In normal operation, the node also produces blocks automatically on `ZEPHYR_BLOCK_INTERVAL` when the mempool is non-empty.

## PowerShell Example

The following example funds an account, submits a transaction, and forces a block locally from PowerShell:

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
```

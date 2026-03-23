# Zephyr Chain MVP API

## Base URL

The default local base URL is:

```text
http://localhost:8080
```

You can change it with `ZEPHYR_HTTP_ADDR` when starting the node.

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
- accepted transactions are still queued only in memory

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

- `balance`: currently funded account balance
- `availableBalance`: balance left after reserving amounts for pending mempool transactions
- `nonce`: latest committed nonce in account state
- `nextNonce`: next nonce expected for the sender
- `pendingTransactions`: number of accepted mempool transactions reserved for this account

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

Response:

```json
{
  "status": "ok",
  "service": "zephyr-node-api"
}
```

### POST /v1/election

Calculates a validator set from the provided candidates, votes, and config, then stores the result as the current in-memory snapshot.

```bash
curl -X POST http://localhost:8080/v1/election \
  -H "Content-Type: application/json" \
  -d '{
    "candidates": [
      { "address": "alice", "selfStake": 20000, "commissionRate": 0.10, "missedBlocks": 1 },
      { "address": "bob", "selfStake": 15000, "commissionRate": 0.08, "missedBlocks": 2 }
    ],
    "votes": [
      { "delegator": "d1", "candidate": "alice", "amount": 5000 },
      { "delegator": "d2", "candidate": "bob", "amount": 9000 }
    ],
    "config": {
      "maxValidators": 21,
      "minSelfStake": 10000,
      "maxMissedBlocks": 50
    }
  }'
```

Each new election replaces the previous in-memory validator snapshot.

### GET /v1/validators

Returns the latest validator snapshot produced by `POST /v1/election`.

```bash
curl http://localhost:8080/v1/validators
```

If no election has been submitted since process start, the array is empty.

### GET /v1/accounts/{address}

Returns the current in-memory account view for the requested address.

```bash
curl http://localhost:8080/v1/accounts/zph_sender
```

Response:

```json
{
  "account": {
    "address": "zph_sender",
    "balance": 100,
    "availableBalance": 75,
    "nonce": 0,
    "nextNonce": 2,
    "pendingTransactions": 1
  }
}
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

Response:

```json
{
  "account": {
    "address": "zph_sender",
    "balance": 100,
    "availableBalance": 100,
    "nonce": 0,
    "nextNonce": 1,
    "pendingTransactions": 0
  }
}
```

### POST /v1/transactions

Accepts a signed transaction envelope and queues it in the node's in-memory mempool after validation.

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

Response:

```json
{
  "id": "7f0e5d5d3f7cf8f2f6b7c7c2fcb00f2a0f4dce8f0e615d4db5b8d80c2c0c1111",
  "accepted": true,
  "queuedAt": "2026-03-23T15:30:00Z",
  "mempoolSize": 1
}
```

Meaning:

- `accepted` means the transaction passed current validation and was queued in the node's in-memory mempool
- it still does not imply execution, inclusion in a block, or finality

## PowerShell Example

The following example funds an account and then submits a transaction from PowerShell:

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
```

# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you two practical local development flows:

- a single-node flow where one Go node persists chain state, funds test accounts, validates transactions, and commits blocks
- a small multi-node devnet flow where one node produces blocks and other configured nodes follow through HTTP-based replication and sync

The browser wallet can create a local account, export and import it, inspect node-side account state, sign a transaction, and send it to the node.

Deterministic WASM smart contracts and a confidential compute marketplace are planned next phases, not part of the current runnable workflow.

## Prerequisites

- Go 1.22 or newer
- Node.js
- npm

Check your tools:

```powershell
go version
node -v
npm -v
```

If PowerShell blocks `npm`, use `npm.cmd -v` and run `npm.cmd` for the later commands.

## Run A Single Node

From the repository root:

```powershell
go run ./cmd/node
```

Expected behavior:

- the process starts an HTTP server
- default bind address is `:8080`
- default data directory is `var/node`
- default automatic block interval is `15s`
- the node exposes `/health`, `/v1/status`, `/v1/peers`, `/v1/election`, `/v1/validators`, `/v1/accounts/{address}`, `/v1/blocks/latest`, `/v1/blocks/{height}`, `/v1/dev/faucet`, `/v1/dev/produce-block`, and `/v1/transactions`

To change the bind address, node identity, data directory, or block interval:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_HTTP_ADDR=":9090"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_BLOCK_INTERVAL="5s"
$env:ZEPHYR_MAX_TXS_PER_BLOCK="25"
go run ./cmd/node
```

Sanity-check the node with:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/v1/status
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

- Node A accepts wallet transactions and produces blocks
- Node B polls peer status on `ZEPHYR_SYNC_INTERVAL`
- transactions, faucet credits, and blocks are replicated over HTTP
- if Node B starts late or misses a block import, it can recover from Node A's snapshot

Inspect the live devnet:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/peers
Invoke-RestMethod http://localhost:8081/v1/status
Invoke-RestMethod http://localhost:8081/v1/peers
```

## Run The Wallet

Open another terminal:

```powershell
cd apps/wallet
npm install
npm run dev
```

If needed:

```powershell
cd apps/wallet
npm.cmd install
npm.cmd run dev
```

By default the wallet expects the node at `http://localhost:8080`.

To point the wallet somewhere else, create `apps/wallet/.env.local`:

```env
VITE_ZEPHYR_API_BASE=http://localhost:8080
```

Then restart the Vite dev server.

## Wallet Walkthrough

### Create A Wallet

1. Open the wallet in your browser.
2. Click `Create wallet`.
3. The app generates an ECDSA P-256 keypair in the browser.
4. The account is saved locally and the `from` field is pre-filled.

### Fund And Inspect The Wallet

1. Make sure the node is running and the wallet shows node health as online.
2. Click `Fund account` to credit the local wallet through the node's dev faucet.
3. Use `Refresh node state` to pull the latest account view from the node.
4. Use `Use next nonce` if you want to align the form with the node's expected next nonce.

### Sign And Broadcast A Transaction

1. Fill in `To`, `Amount`, `Nonce`, and `Memo`.
2. Confirm the `From` address matches your loaded wallet.
3. Click `Sign locally`.
4. Review the generated signed envelope in the read-only payload area.
5. Click `Broadcast`.

What happens under the hood:

- the wallet creates a canonical payload by sorting transaction keys
- the payload is signed locally with the stored private key
- the signed envelope is sent to `POST /v1/transactions`
- the node validates the canonical payload, verifies the P-256 signature, checks nonce and available balance, and persists the transaction in the local mempool
- if peers are configured, the node forwards the transaction to them
- the producer later commits the transaction into a block automatically on the configured interval, or immediately if you force block production through the dev endpoint
- replica nodes import the new block or catch up from snapshot if necessary

Important:

- broadcast success means the transaction entered the local durable mempool
- replication is devnet synchronization, not consensus finality
- this is still a prototype network layer, not the final peer-to-peer transport

## Force A Local Block

For deterministic local testing, you can force immediate block production on a producer node:

```powershell
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/dev/produce-block"
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/blocks/latest"
```

This is useful when you want to confirm account balances and nonces after commit without waiting for the automatic interval.

## Verify A Replicated Block

After broadcasting and producing a block on Node A, compare both nodes:

```powershell
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/blocks/latest"
Invoke-RestMethod -Method Get -Uri "http://localhost:8081/v1/blocks/latest"
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/accounts/<sender-address>"
Invoke-RestMethod -Method Get -Uri "http://localhost:8081/v1/accounts/<sender-address>"
```

You should see matching block hashes and matching committed balances/nonces after sync completes.

## Wallet Backup JSON

The backup JSON is a portable serialization of the stored wallet object. It currently includes both public and private key material.

Use it only for local development. In the current MVP:

- backups are not encrypted
- anyone with the JSON can control the wallet
- imported backups fully replace the active local wallet

## Troubleshooting

### `go` Is Not Recognized

- install Go 1.22 or newer
- reopen your terminal after installation
- run `go version` again

### Node Health Shows Offline

- confirm the node process is still running
- confirm the bind address matches the wallet's API base URL
- open `http://localhost:8080/health` directly or use `Invoke-RestMethod`

### Peer State Does Not Sync

- confirm both nodes have each other in `ZEPHYR_PEERS`
- confirm the replica has `ZEPHYR_ENABLE_PEER_SYNC="true"`
- confirm producer and replica use different `ZEPHYR_DATA_DIR` values
- check `GET /v1/peers` for reachability and last error details

### Runtime State Is Not Where You Expect

- confirm `ZEPHYR_DATA_DIR` points to the intended local directory
- remember the default durable state location is `var/node`
- remove a data directory only if you intentionally want to discard local chain state

### Transaction Broadcast Returns An Error

- make sure `From`, `To`, `publicKey`, and `signature` are present
- sign the transaction before clicking `Broadcast`
- fund the sender account first if the node reports insufficient balance
- use the node's suggested nonce if the current one is rejected
- make sure the visible transaction fields still match the signed payload if you change anything after signing

## Recommended Local Demo Flow

1. Start Node A.
2. Start Node B as a replica.
3. Start the Vue wallet against Node A.
4. Create a wallet.
5. Fund the wallet with the local dev faucet.
6. Refresh account state and confirm the suggested nonce.
7. Sign a sample transaction.
8. Broadcast it to Node A.
9. Wait for automatic block production or call `POST /v1/dev/produce-block`.
10. Inspect `GET /v1/status`, `GET /v1/peers`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}` on both nodes.

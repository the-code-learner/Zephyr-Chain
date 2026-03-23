# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you two local development components:

- a Go node API that can rank validators, persist local chain state, fund test accounts, inspect account state, queue validated transactions, and commit single-node blocks
- a browser wallet that can create a local account, export and import it, inspect node-side account state, sign a transaction, and send it to the node

Deterministic WASM smart contracts and a confidential compute marketplace are planned next phases, not part of the current local MVP workflow.

This guide explains how to run both parts together.

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

## Run The Node

From the repository root:

```powershell
go run ./cmd/node
```

Expected behavior:

- the process starts an HTTP server
- default bind address is `:8080`
- default data directory is `var/node`
- default automatic block interval is `15s`
- the node exposes `/health`, `/v1/status`, `/v1/election`, `/v1/validators`, `/v1/accounts/{address}`, `/v1/blocks/latest`, `/v1/dev/faucet`, `/v1/dev/produce-block`, and `/v1/transactions`

To change the bind address, data directory, or block interval:

```powershell
$env:ZEPHYR_HTTP_ADDR=":9090"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_BLOCK_INTERVAL="5s"
$env:ZEPHYR_MAX_TXS_PER_BLOCK="25"
go run ./cmd/node
```

You can sanity-check the node with:

```powershell
Invoke-RestMethod http://localhost:8080/health
Invoke-RestMethod http://localhost:8080/v1/status
```

## Run The Wallet

Open a second terminal:

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
- the node later commits the transaction into a block automatically on the configured interval, or immediately if you force block production through the dev endpoint

Important:

- broadcast success means the transaction entered the local node mempool
- block commitment is a separate step from broadcast acceptance
- this is still single-node local execution, not network finality

## Force A Local Block

For deterministic local testing, you can force immediate block production:

```powershell
Invoke-RestMethod -Method Post -Uri "http://localhost:8080/v1/dev/produce-block"
Invoke-RestMethod -Method Get -Uri "http://localhost:8080/v1/blocks/latest"
```

This is useful when you want to confirm account balances and nonces after commit without waiting for the automatic interval.

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

### Runtime State Is Not Where You Expect

- confirm `ZEPHYR_DATA_DIR` points to the intended local directory
- remember the default durable state location is `var/node`
- remove the data directory only if you intentionally want to discard local chain state

### Transaction Broadcast Returns An Error

- make sure `From`, `To`, `publicKey`, and `signature` are present
- sign the transaction before clicking `Broadcast`
- fund the sender account first if the node reports insufficient balance
- use the node's suggested nonce if the current one is rejected
- make sure the visible transaction fields still match the signed payload if you change anything after signing

## Recommended Local Demo Flow

1. Start the Go node.
2. Start the Vue wallet.
3. Create a wallet.
4. Fund the wallet with the local dev faucet.
5. Refresh account state and confirm the suggested nonce.
6. Sign a sample transaction.
7. Broadcast it to the local node.
8. Wait for automatic block production or call `POST /v1/dev/produce-block`.
9. Inspect `GET /v1/status`, `GET /v1/blocks/latest`, and `GET /v1/accounts/{address}`.

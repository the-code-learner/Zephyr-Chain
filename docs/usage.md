# Zephyr Chain MVP Usage Guide

## What This MVP Does

The current repository gives you two local development components:

- a Go node API that can rank validators, fund local test accounts, inspect account state, and queue validated transactions in memory
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
- the node exposes `/health`, `/v1/election`, `/v1/validators`, `/v1/accounts/{address}`, `/v1/dev/faucet`, and `/v1/transactions`

To change the bind address:

```powershell
$env:ZEPHYR_HTTP_ADDR=":9090"
go run ./cmd/node
```

You can sanity-check the node with:

```powershell
Invoke-RestMethod http://localhost:8080/health
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

What is stored:

- address
- creation timestamp
- exported public key JWK
- exported private key JWK
- base64-encoded SPKI public key

Where it is stored:

- browser `localStorage`
- storage key: `zephyr.wallet.account`

### Import A Wallet

1. Paste a previously exported backup JSON into the `Wallet backup JSON` field.
2. Click `Import backup`.
3. The wallet is saved into local storage and becomes the active account.

### Clear A Wallet

1. Click `Clear local wallet`.
2. The app removes the stored wallet from browser `localStorage`.

## Sign And Broadcast A Transaction

1. Make sure the node is running and the wallet shows node health as online.
2. If this is a fresh account, use the wallet's `Fund account` control or call `POST /v1/dev/faucet` first.
3. Fill in `To`, `Amount`, `Nonce`, and `Memo`.
4. Confirm the `From` address matches your loaded wallet.
5. Use `Use next nonce` if you want to copy the node's suggested nonce into the form.
6. Click `Sign locally`.
7. Review the generated signed envelope in the read-only payload area.
8. Click `Broadcast`.

What happens under the hood:

- the wallet creates a canonical payload by sorting transaction keys
- the payload is signed locally with the stored private key
- the signed envelope is sent to `POST /v1/transactions`
- the node validates the canonical payload, verifies the P-256 signature, checks nonce and available balance, then appends the request to the in-memory mempool and returns an ID

Important:

- broadcast success only means the node queued the transaction in memory
- it does not mean the transaction executed or finalized

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

### Wallet Cannot Reach The Node

- confirm `VITE_ZEPHYR_API_BASE` points to the right host and port
- restart the Vite dev server after changing `.env.local`
- make sure browser and node are using the same local network target

### PowerShell Blocks `npm`

- use `npm.cmd install` instead of `npm install`
- use `npm.cmd run dev` instead of `npm run dev`

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
8. Optionally call `POST /v1/election` and inspect `GET /v1/validators`.

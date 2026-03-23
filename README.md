# Zephyr Chain

Zephyr Chain is an early-stage blockchain MVP with a Go node, a browser wallet, durable local state, and a first multi-node devnet replication layer.

The long-term product vision lives in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). This `README` stays practical: what works now, how to run it, and what should be built next.

## Current MVP

Implemented today:

- Go HTTP node entrypoint in `cmd/node`
- DPoS election primitives and tests in `internal/dpos`
- transaction envelope validation in `internal/tx`
- durable accounts, mempool, committed blocks, and restart-safe state in `internal/ledger`
- automatic single-node block production plus manual dev block production
- static multi-node devnet replication over HTTP in `internal/api`
- peer status tracking, block fetch by height, block import, and snapshot-based catch-up for late joiners
- Vue wallet in `apps/wallet`
- wallet account generation, import/export, local signing, account inspection, faucet funding, and transaction broadcast

Planned but not implemented yet:

- libp2p-based networking and authenticated peer discovery
- validator-coordinated block proposal and acknowledgment rules
- deterministic WASM smart-contract runtime
- confidential compute marketplace for encrypted off-chain jobs paid in native tokens

## Planned Execution Model

- On-chain contracts will use deterministic WASM execution with Rust-first tooling.
- Contract pricing will use Zephyr-native metering for instruction budget, memory growth, storage access, and emitted messages.
- Heavy or private workloads will run through a separate confidential compute market anchored and settled on-chain.
- Native tokens will pay for both contract execution and off-chain compute jobs, but through different pricing mechanisms.

## Repository Layout

- `cmd/node`: node process entrypoint and environment-based runtime configuration
- `internal/api`: HTTP handlers, peer replication, sync loops, and status surface
- `internal/dpos`: candidate, vote, validator, and election service logic
- `internal/ledger`: persisted accounts, mempool entries, blocks, snapshots, and commit/import logic
- `internal/tx`: transaction envelope validation, address derivation, and signature verification
- `apps/wallet`: reference light wallet built with Vue 3, Vite, and Tailwind CSS
- `docs/`: architecture, API, and usage guides
- `var/`: default local runtime state directory for the node, ignored by git

## Prerequisites

- Go 1.22 or newer
- Node.js
- npm

PowerShell note: if your shell blocks `npm`, use `npm.cmd` instead.

## Quick Start

### 1. Run one node

From the repository root:

```powershell
go run ./cmd/node
```

By default the node:

- listens on `:8080`
- stores durable state in `var/node`
- produces blocks every `15s` when transactions are queued
- runs peer sync only if `ZEPHYR_PEERS` is configured

### 2. Run the wallet

In a second terminal:

```powershell
cd apps/wallet
npm install
npm run dev
```

If PowerShell execution policy blocks `npm`, run:

```powershell
cd apps/wallet
npm.cmd install
npm.cmd run dev
```

Vite serves the wallet on `http://localhost:5173` by default.

### 3. Run a two-node local devnet

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

Use the wallet against Node A. Node B will follow through transaction, block, and snapshot sync.

## Runtime Configuration

### Node

- `ZEPHYR_HTTP_ADDR`: HTTP bind address for the Go node
- `ZEPHYR_NODE_ID`: human-readable node identifier used in peer replication headers and status output
- `ZEPHYR_DATA_DIR`: local directory used for durable node state
- `ZEPHYR_PEERS`: comma-separated peer base URLs such as `http://localhost:8081,http://localhost:8082`
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval such as `15s`
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval such as `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per produced block
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true` or `false`
- `ZEPHYR_ENABLE_PEER_SYNC`: `true` or `false`

Default values:

- `ZEPHYR_HTTP_ADDR`: `:8080`
- `ZEPHYR_NODE_ID`: `node-local`
- `ZEPHYR_DATA_DIR`: `var/node`
- `ZEPHYR_PEERS`: empty
- `ZEPHYR_BLOCK_INTERVAL`: `15s`
- `ZEPHYR_SYNC_INTERVAL`: `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true`
- `ZEPHYR_ENABLE_PEER_SYNC`: `true`

### Wallet

- `VITE_ZEPHYR_API_BASE`: base URL used by the wallet for node API calls
- default: `http://localhost:8080`

Example `.env.local` inside `apps/wallet`:

```env
VITE_ZEPHYR_API_BASE=http://localhost:8080
```

## How The MVP Works

1. The wallet generates an ECDSA P-256 keypair in the browser using Web Crypto.
2. It derives a Zephyr-style address from the SHA-256 hash of the exported public key.
3. The wallet stores the private key, public key, and address in browser `localStorage`.
4. The wallet can inspect node-side account state and use a dev faucet for local funding.
5. The wallet signs a canonical transaction payload locally and sends the signed envelope to the node.
6. The node validates the payload, address, signature, nonce, and available balance before persisting the transaction in the durable mempool.
7. A block-producing node commits pending transactions into a durable local block and updates balances and nonces.
8. Configured peer nodes receive transactions and blocks over HTTP, import them when possible, and fall back to snapshot restore when they need catch-up.
9. DPoS elections are still calculated on demand through the DPoS service and stored as a local validator snapshot.

## Current Limitations

- the current multi-node layer is static HTTP replication, not libp2p networking
- block production is still effectively single-producer in this devnet slice
- there is no validator acknowledgment or Byzantine consensus yet
- DPoS elections are still API-level calculations, not a live validator round
- snapshot restore is a simple state catch-up mechanism, not a trust-minimized sync protocol
- WASM smart-contract execution is planned, but not implemented yet
- confidential compute jobs, worker attestation, escrow, and settlement are planned, but not implemented yet
- wallet private keys are stored unencrypted in browser `localStorage`

Because of these limitations, the current MVP should still be treated as a local development prototype, not a production blockchain network.

## Documentation

- [docs/architecture.md](./docs/architecture.md)
- [docs/api.md](./docs/api.md)
- [docs/usage.md](./docs/usage.md)
- [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md)

## Next Development Steps

1. Replace the current static HTTP replication with libp2p transport and authenticated peer discovery.
2. Introduce validator-aware block proposal, acknowledgment, and commit rules instead of single-node production.
3. Add deterministic WASM smart-contract execution.
4. Add contract metering and native-fee accounting for WASM execution.
5. Introduce worker registry, attestation verification, and bid marketplace flows.
6. Add async confidential compute jobs with escrow, settlement, and slashing.
7. Improve wallet UX for network selection, peer visibility, block history, job creation, result retrieval, and fee/payment history.

## License

Zephyr Chain is licensed under the MIT License. See [LICENSE](./LICENSE).

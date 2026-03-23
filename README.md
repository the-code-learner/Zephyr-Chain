# Zephyr Chain

Zephyr Chain is an early-stage blockchain MVP focused on a Go node API, a browser wallet, and a progressively more realistic single-node execution path.

The long-term vision for Zephyr Chain lives in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). This `README` documents what is implemented now, how to run it, and where to extend it next.

## Current MVP

Implemented today:

- Go HTTP node entrypoint in `cmd/node`
- DPoS election primitives and tests in `internal/dpos`
- transaction envelope validation in `internal/tx`
- durable local ledger, mempool, and block state in `internal/ledger`
- persisted JSON state under a configurable node data directory
- single-node block production with committed balances and nonces
- HTTP endpoints for health, status, accounts, faucet funding, transaction submission, latest block, and manual local block production
- Vue wallet in `apps/wallet`
- wallet account generation, import/export, local signing, account inspection, and transaction broadcast
- wallet-side account refresh, suggested nonce helpers, and local faucet integration for easier testing

Planned but not implemented yet:

- peer-to-peer networking and validator coordination
- sharding and DAG research
- deterministic WASM smart-contract runtime
- confidential compute marketplace for encrypted off-chain jobs paid in native tokens

## Planned Execution Model

- On-chain contracts will use deterministic WASM execution with Rust-first tooling.
- Contract pricing will use Zephyr-native metering for instruction budget, memory growth, storage access, and emitted messages.
- Heavy or private workloads will run through a separate confidential compute market anchored and settled on-chain.
- Native tokens will pay for both contract execution and off-chain compute jobs, but through different pricing mechanisms.

## Repository Layout

- `cmd/node`: node process entrypoint and environment-based runtime configuration
- `internal/api`: HTTP handlers, status surface, and single-node block-production loop
- `internal/dpos`: candidate, vote, validator, and election service logic
- `internal/ledger`: persisted accounts, mempool entries, blocks, and commit logic
- `internal/tx`: transaction envelope validation, address derivation, and signature verification
- `apps/wallet`: reference light wallet built with Vue 3, Vite, and Tailwind CSS
- `docs/`: architecture, API, and local usage guides
- `var/`: default local runtime state directory for the node, ignored by git

## Prerequisites

- Go 1.22 or newer
- Node.js
- npm

PowerShell note: if your shell blocks `npm`, use `npm.cmd` instead.

## Quick Start

### 1. Run the node API

From the repository root:

```powershell
go run ./cmd/node
```

The node listens on `:8080` by default, stores local runtime state in `var/node`, and produces blocks every `15s` when there are pending transactions.

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

## Runtime Configuration

### Node

- `ZEPHYR_HTTP_ADDR`: HTTP bind address for the Go node
- `ZEPHYR_DATA_DIR`: local directory used for durable node state
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval such as `15s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per produced block

Default values:

- `ZEPHYR_HTTP_ADDR`: `:8080`
- `ZEPHYR_DATA_DIR`: `var/node`
- `ZEPHYR_BLOCK_INTERVAL`: `15s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: `100`

Example:

```powershell
$env:ZEPHYR_HTTP_ADDR=":9090"
$env:ZEPHYR_DATA_DIR="var/devnet-a"
$env:ZEPHYR_BLOCK_INTERVAL="5s"
$env:ZEPHYR_MAX_TXS_PER_BLOCK="25"
go run ./cmd/node
```

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
3. The private key, public key, and address are stored in browser `localStorage`.
4. The wallet can inspect the current node-side account state and use a dev faucet for local funding.
5. The wallet signs a canonical transaction payload locally and sends the signed envelope to the node.
6. The node validates the payload, address, signature, balance, and nonce rules before accepting the transaction into the persisted local mempool.
7. The node produces blocks on a timer, commits pending transactions into durable local chain state, updates balances and nonces, and exposes the latest block through the API.
8. Validator elections are still calculated on demand through the DPoS service and stored as the latest local validator snapshot.

## Current Limitations

- block production is still single-node and not coordinated by a peer network
- DPoS elections are still API-level calculations, not a live validator consensus round
- the runtime state is durable on one node only; there is no replication or sync yet
- there is no peer networking or consensus communication layer yet
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

1. Add peer-to-peer networking, block propagation, and validator coordination.
2. Introduce deterministic WASM smart-contract execution.
3. Add contract metering and native-fee accounting for WASM execution.
4. Introduce worker registry, attestation verification, and bid marketplace flows.
5. Add async confidential compute jobs with escrow, settlement, and slashing.
6. Improve wallet UX for network selection, block visibility, job creation, budget setting, result retrieval, and fee/payment history.

## License

Zephyr Chain is licensed under the MIT License. See [LICENSE](./LICENSE).

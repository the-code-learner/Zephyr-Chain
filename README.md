# Zephyr Chain

Zephyr Chain is an early-stage blockchain MVP focused on two Phase 1 capabilities:

- a Go node API with in-memory Delegated Proof-of-Stake (DPoS) election logic
- a Vue 3 wallet that creates browser-side accounts, signs transactions locally, and broadcasts them to the node API

The long-term vision for Zephyr Chain lives in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). This `README` documents what is implemented now, how to run it, and where to extend it next.

## Current MVP

Implemented today:

- Go HTTP node entrypoint in `cmd/node`
- in-memory API server in `internal/api`
- DPoS election primitives and tests in `internal/dpos`
- Vue wallet in `apps/wallet`
- wallet account generation, import/export, local signing, account inspection, and transaction broadcast
- server-side canonical payload verification, signature validation, nonce checks, balance checks, and duplicate detection
- dev/test account funding and account inspection endpoints
- wallet-side account refresh, suggested nonce helpers, and local faucet integration for easier testing

Planned but not implemented yet:

- persistent chain state and block production
- peer-to-peer networking
- sharding and DAG research
- deterministic WASM smart-contract runtime
- confidential compute marketplace for encrypted off-chain jobs paid in native tokens

## Planned Execution Model

- On-chain contracts will use deterministic WASM execution with Rust-first tooling.
- Contract pricing will use Zephyr-native metering for instruction budget, memory growth, storage access, and emitted messages.
- Heavy or private workloads will run through a separate confidential compute market anchored and settled on-chain.
- Native tokens will pay for both contract execution and off-chain compute jobs, but through different pricing mechanisms.

## Repository Layout

- `cmd/node`: node process entrypoint
- `internal/api`: HTTP handlers for health, election, validator snapshot, account inspection, faucet funding, and transaction submission
- `internal/dpos`: candidate, vote, validator, and election service logic
- `internal/ledger`: in-memory account, pending balance, and mempool reservation logic
- `internal/tx`: transaction envelope validation, address derivation, and signature verification
- `apps/wallet`: reference light wallet built with Vue 3, Vite, and Tailwind CSS
- `Zaphyr-chain_manifesto.md`: project vision and long-term direction
- `docs/architecture.md`: MVP architecture and data flow
- `docs/api.md`: current HTTP API and JSON shapes
- `docs/usage.md`: local setup and operator walkthrough

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

The node listens on `:8080` by default.

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
- default: `:8080`

Example:

```powershell
$env:ZEPHYR_HTTP_ADDR=":9090"
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
6. The node validates the payload, address, signature, balance, and nonce rules before accepting the transaction into an in-memory mempool and returning a generated transaction ID.
7. Validator elections are calculated on demand through the DPoS service and stored as the latest in-memory validator snapshot.

## Current Limitations

- all state is in memory and is lost on restart
- account state, mempool reservations, and dev funding are still single-node and in-memory only
- there is no block production or finality mechanism yet
- there is no peer networking or consensus communication layer yet
- WASM smart-contract execution is planned, but not implemented yet
- confidential compute jobs, worker attestation, escrow, and settlement are planned, but not implemented yet
- wallet private keys are stored unencrypted in browser `localStorage`

Because of these limitations, the current MVP should be treated as a local development prototype, not a production blockchain network.

## Documentation

- [docs/architecture.md](./docs/architecture.md)
- [docs/api.md](./docs/api.md)
- [docs/usage.md](./docs/usage.md)
- [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md)

## Next Development Steps

1. Persist account, mempool, and chain state instead of keeping them only in memory.
2. Add peer-to-peer networking, block propagation, and validator coordination.
3. Introduce deterministic WASM smart-contract execution.
4. Add contract metering and native-fee accounting for WASM execution.
5. Introduce worker registry, attestation verification, and bid marketplace flows.
6. Add async confidential compute jobs with escrow, settlement, and slashing.
7. Improve wallet UX for network selection, job creation, budget setting, result retrieval, and fee/payment history.

## License

Zephyr Chain is licensed under the MIT License. See [LICENSE](./LICENSE).

# Zephyr Chain

Zephyr Chain is an early-stage blockchain node and wallet stack focused on a production path toward validator-driven consensus, deterministic WASM execution, and a confidential compute marketplace.

The long-term product vision lives in [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md). This `README` stays practical: what works now, what changed in the latest iteration, and what comes next.

## Current Status

Implemented today:

- Go HTTP node entrypoint in `cmd/node`
- DPoS election primitives and tests in `internal/dpos`
- transaction envelope validation in `internal/tx`
- durable accounts, mempool, committed blocks, restart-safe state, and snapshot restore in `internal/ledger`
- durable validator-set snapshots with versioning, proposer scheduling, and quorum summaries in `internal/ledger`
- durable signed consensus proposals, validator votes, and quorum certificates in `internal/ledger`
- automatic single-node block production plus manual dev block production
- optional proposer-schedule enforcement for block production when a validator set and local validator address are configured
- optional certificate-gated local block commit and remote block import when consensus enforcement is enabled
- transport-backed peer replication in `internal/api` with the current implementation running over HTTP
- peer status tracking, block fetch by height, block import, snapshot-based catch-up, and consensus artifact replication for late joiners
- signed validator transport-identity proofs in status responses and peer verification views when a validator private key is configured
- consensus visibility endpoints for status, validator snapshots, proposer schedule inspection, latest consensus artifacts, and next-block template preview
- Vue wallet in `apps/wallet`
- wallet account generation, import/export, local signing, account inspection, faucet funding, and transaction broadcast

Implemented in this iteration:

- validator nodes can derive and expose a signed transport identity from `ZEPHYR_VALIDATOR_PRIVATE_KEY`, and the node derives or validates the configured validator address from that key
- `GET /v1/status` now exposes that signed validator identity, and `GET /v1/peers` reports whether a configured peer's identity proof was present and verified
- transport-backed replication now attaches signed source-identity headers for validator nodes, and malformed proofs are rejected on replicated POST paths
- focused tests now cover signed status exposure, peer-side identity verification, mismatched validator-key startup rejection, and invalid transport-signature rejection

Planned but not implemented yet:

- strict peer admission and peer discovery over libp2p on top of the new signed transport-identity proof
- proposal dissemination that carries enough data for validators to verify a candidate without relying on local mempool mirroring alone
- round timeout handling, re-proposal rules, and consensus write-ahead recovery
- on-chain staking and governance-driven validator updates instead of ad hoc election API writes
- deterministic WASM smart-contract runtime with native fee metering
- confidential compute marketplace for encrypted off-chain jobs paid in native tokens
- production observability, recovery tooling, and public testnet operations

## Repository Layout

- `cmd/node`: node process entrypoint and environment-based runtime configuration
- `internal/api`: HTTP handlers, peer replication, consensus surface, transport abstraction, sync loops, and status endpoints
- `internal/consensus`: signed proposal and vote message primitives
- `internal/dpos`: candidate, vote, validator, and election service logic
- `internal/ledger`: persisted accounts, mempool entries, committed blocks, validator snapshots, consensus artifacts, snapshots, and commit/import logic
- `internal/tx`: transaction envelope validation, address derivation, and signature verification
- `apps/wallet`: reference light wallet built with Vue 3, Vite, and Tailwind CSS
- `docs/`: architecture, API, usage, and roadmap guides
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
- exposes consensus status even before a validator set has been elected

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

Use the wallet against Node A. Node B will follow through transaction, block, snapshot, and consensus-artifact sync.

### 4. Enable certificate-gated commit/import

For production-style consensus enforcement on the current devnet flow, run a node with:

```powershell
$env:ZEPHYR_NODE_ID="node-a"
$env:ZEPHYR_VALIDATOR_ADDRESS="zph_validator_a"
$env:ZEPHYR_VALIDATOR_PRIVATE_KEY="<base64-pkcs8-p256-private-key>"
$env:ZEPHYR_ENFORCE_PROPOSER_SCHEDULE="true"
$env:ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES="true"
go run ./cmd/node
```

Then use:

```powershell
Invoke-RestMethod http://localhost:8080/v1/status
Invoke-RestMethod http://localhost:8080/v1/dev/block-template
Invoke-RestMethod http://localhost:8080/v1/consensus
```

`GET /v1/status` now exposes the node's signed transport identity when `ZEPHYR_VALIDATOR_PRIVATE_KEY` is configured. The template response gives you the exact `height`, `previousHash`, `producedAt`, `transactionIds`, and `blockHash` that a signed proposal must certify. Once a matching quorum certificate exists, `POST /v1/dev/produce-block` can commit that exact block candidate.

## Runtime Configuration

### Node

- `ZEPHYR_HTTP_ADDR`: HTTP bind address for the Go node
- `ZEPHYR_NODE_ID`: human-readable node identifier used in peer replication headers and status output
- `ZEPHYR_VALIDATOR_ADDRESS`: chain-level validator address used for proposer-schedule enforcement and status reporting
- `ZEPHYR_VALIDATOR_PRIVATE_KEY`: base64-encoded PKCS#8 P-256 private key used to derive and sign the node's validator transport identity
- `ZEPHYR_DATA_DIR`: local directory used for durable node state
- `ZEPHYR_PEERS`: comma-separated peer base URLs such as `http://localhost:8081,http://localhost:8082`
- `ZEPHYR_BLOCK_INTERVAL`: automatic block-production interval such as `15s`
- `ZEPHYR_SYNC_INTERVAL`: peer poll/sync interval such as `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: maximum committed transactions per produced block
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true` or `false`
- `ZEPHYR_ENABLE_PEER_SYNC`: `true` or `false`
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: `true` or `false`; when enabled and a validator set exists, only the scheduled proposer may produce the next block locally
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: `true` or `false`; when enabled and a validator set exists, local block commit and remote block import require a matching proposal and quorum certificate

Default values:

- `ZEPHYR_HTTP_ADDR`: `:8080`
- `ZEPHYR_NODE_ID`: `node-local`
- `ZEPHYR_VALIDATOR_ADDRESS`: empty
- `ZEPHYR_VALIDATOR_PRIVATE_KEY`: empty
- `ZEPHYR_DATA_DIR`: `var/node`
- `ZEPHYR_PEERS`: empty
- `ZEPHYR_BLOCK_INTERVAL`: `15s`
- `ZEPHYR_SYNC_INTERVAL`: `5s`
- `ZEPHYR_MAX_TXS_PER_BLOCK`: `100`
- `ZEPHYR_ENABLE_BLOCK_PRODUCTION`: `true`
- `ZEPHYR_ENABLE_PEER_SYNC`: `true`
- `ZEPHYR_ENFORCE_PROPOSER_SCHEDULE`: `false`
- `ZEPHYR_REQUIRE_CONSENSUS_CERTIFICATES`: `false`

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
7. A block-producing node can build a deterministic next-block template from the current mempool and latest chain tip.
8. DPoS elections persist a durable validator snapshot with versioning, voting-power totals, and next-proposer scheduling metadata.
9. Operators can submit signed proposals for that concrete block template, including the exact `previousHash`, `producedAt`, ordered `transactionIds`, and derived `blockHash`.
10. Validators submit signed votes for the certified `blockHash`, and once vote power crosses quorum the node stores a durable commit certificate artifact for that height and round.
11. If proposer-schedule enforcement is enabled, a node can refuse to produce a block unless its configured validator address matches the scheduled proposer for the next height.
12. If consensus-certificate enforcement is enabled, local block commit and remote block import both require a proposal and quorum certificate for the exact block template being committed.
13. If a validator private key is configured, the node derives a signed transport identity for its validator address and exposes that proof in runtime status.
14. Configured peer nodes receive transactions, consensus artifacts, and blocks over the current transport implementation, verify peer identity proofs when available, import blocks when possible, and fall back to snapshot restore when they need catch-up.

## Current Limitations

- the current multi-node layer is still HTTP-based under the new transport abstraction, not libp2p networking
- validator nodes can now prove identity over the current transport, but peer admission and discovery do not enforce that proof yet
- the current proposal flow signs ordered transaction IDs and `producedAt`, but it still does not distribute a fuller self-contained proposal body or autonomous round engine
- round timeout, round change, and crash-recovery behavior are not implemented yet
- DPoS elections still happen through an API call, not an on-chain staking/governance flow
- snapshot restore is a state catch-up mechanism, not a trust-minimized proof-based sync protocol
- WASM smart-contract execution is planned, but not implemented yet
- confidential compute jobs, worker attestation, escrow, and settlement are planned, but not implemented yet
- wallet private keys are stored unencrypted in browser `localStorage`

Because of these limitations, the current MVP should still be treated as a development prototype, not a production blockchain network.

## Roadmap

The production roadmap now lives in [docs/roadmap.md](./docs/roadmap.md).

Short version:

1. Turn the new signed validator transport identity into strict peer admission rules and move the transport abstraction from HTTP-only behavior toward authenticated libp2p networking.
2. Extend the certified flow from template commitment into fuller proposal dissemination, timeout handling, and restart-safe round recovery.
3. Move validator lifecycle changes behind staking, delegation, slashing, and governance state transitions.
4. Add deterministic WASM execution, native fee metering, and the confidential compute lane.
5. Add production observability, recovery tooling, and public testnet operations.

## Documentation

- [docs/architecture.md](./docs/architecture.md)
- [docs/api.md](./docs/api.md)
- [docs/usage.md](./docs/usage.md)
- [docs/roadmap.md](./docs/roadmap.md)
- [Zaphyr-chain_manifesto.md](./Zaphyr-chain_manifesto.md)

## License

Zephyr Chain is licensed under the MIT License. See [LICENSE](./LICENSE).


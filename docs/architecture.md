# Zephyr Chain MVP Architecture

## Overview

The current MVP is a three-part local development system:

- a Go node API that validates transactions, persists chain state, produces blocks, and replicates state to configured peers
- a durable ledger that stores accounts, mempool entries, committed blocks, and restart-safe metadata on disk
- a Vue wallet that runs in the browser and acts as a light client

The current data flow is:

`wallet UI -> wallet signing logic -> node HTTP API -> durable mempool -> local block production -> durable block/account state -> static peer replication over HTTP`

This is the first multi-node devnet slice. It is not libp2p and it is not consensus yet.

## Components

### Wallet

The reference wallet lives in `apps/wallet` and is responsible for:

- generating an ECDSA P-256 keypair through the Web Crypto API
- exporting the key material as JWK plus SPKI public key bytes
- deriving a Zephyr address from the SHA-256 hash of the public key
- storing the wallet backup JSON in browser `localStorage`
- creating a canonical transaction payload
- signing the payload locally
- calling the node API for health, account inspection, faucet funding, and transaction broadcast

The wallet is still a light client. It does not run consensus logic or store full chain state.

### Node API

The node entrypoint lives in `cmd/node/main.go` and starts an HTTP server from `internal/api`.

The API layer currently handles:

- liveness through `GET /health`
- runtime status through `GET /v1/status`
- peer visibility through `GET /v1/peers`
- validator election inputs through `POST /v1/election`
- the latest local validator snapshot through `GET /v1/validators`
- persisted account state through `GET /v1/accounts/{address}`
- signed transaction envelopes through `POST /v1/transactions`
- committed blocks through `GET /v1/blocks/latest` and `GET /v1/blocks/{height}`
- development funding through `POST /v1/dev/faucet`
- manual local block production through `POST /v1/dev/produce-block`
- internal node sync through `POST /v1/internal/blocks` and `GET /v1/internal/snapshot`

### Durable Ledger

The durable local state lives in `internal/ledger` and is persisted as JSON under the configured node data directory.

The store currently persists:

- account balances and committed nonces
- queued mempool entries
- committed blocks
- known committed transaction IDs
- applied faucet request IDs used for idempotent peer funding replication

On startup, the node reloads this state and rebuilds pending balance and nonce reservations from the persisted mempool.

### Static Peer Replication Layer

The current multi-node layer is intentionally simple.

Each node can be configured with a fixed list of peer base URLs through `ZEPHYR_PEERS`. When enabled:

- accepted transactions are forwarded to peers over HTTP
- dev faucet credits are forwarded with a request ID so duplicate credits are ignored safely
- locally produced blocks are posted to peers for import
- a background sync loop polls peer status on `ZEPHYR_SYNC_INTERVAL`
- if a node is behind, it fetches missing blocks by height
- if block import fails or the node detects divergent state at the same height, it falls back to a full snapshot restore

This gives the project a workable devnet replication path without yet claiming full peer-to-peer networking or consensus safety.

## DPoS Election Flow

The DPoS service lives in `internal/dpos` and currently models a deterministic ranking algorithm rather than a full consensus engine.

Inputs:

- `Candidate`: validator candidate metadata and self stake
- `Vote`: delegated stake from delegators to candidates
- `ElectionConfig`: election limits and eligibility thresholds

Default election behavior:

- `MaxValidators`: `21` when the provided value is `0`
- `MinSelfStake`: `10000` when the provided value is `0`
- `MaxMissedBlocks`: `50` when the provided value is `0`

Election steps:

1. Index candidates by address.
2. Sum delegated stake for votes that target known candidates.
3. Reject candidates whose `SelfStake` is below `MinSelfStake`.
4. Reject candidates whose `MissedBlocks` exceeds `MaxMissedBlocks`.
5. Compute `VotingPower = SelfStake + DelegatedStake`.
6. Sort validators deterministically by higher `VotingPower`, higher `SelfStake`, lower `CommissionRate`, then lexicographically smaller `Address`.
7. Trim the result to `MaxValidators`.
8. Assign 1-based `Rank` values.

The API still stores the latest election result locally and independently from block production.

## Transaction Lifecycle

1. The wallet creates a keypair in the browser.
2. It derives an address in the form `zph_<40 hex chars>`.
3. The wallet stores the full backup object under the browser key `zephyr.wallet.account`.
4. The user fills in `from`, `to`, `amount`, `nonce`, and `memo`.
5. The wallet creates a canonical JSON payload by sorting object keys before serialization.
6. The wallet signs that payload with ECDSA using SHA-256.
7. The wallet sends a `BroadcastTransactionRequest` to `POST /v1/transactions`.
8. The node validates the canonical payload, checks that the public key maps to the sender address, verifies the signature, enforces nonce and balance rules, and persists the transaction in the local mempool.
9. If peers are configured, the node forwards the accepted transaction to them.
10. A block-producing node later selects queued transactions, applies balance and nonce updates, commits a new block, removes committed transactions from the mempool, and persists the new head state.
11. Peer nodes import the new block directly or catch up from snapshot if needed.

Important current behavior:

- `accepted: true` means the transaction was queued in the local durable mempool
- replication is best-effort devnet behavior, not validator finality
- snapshot restore is a convenience sync mechanism, not a trust-minimized state proof system

## Block Production And Sync Path

The current block and sync path is intentionally narrow:

- one node can be configured as the active producer while others disable local production
- the producer creates blocks from mempool order up to `ZEPHYR_MAX_TXS_PER_BLOCK`
- each block stores height, previous hash, produced time, transaction IDs, and full transaction envelopes
- peers import blocks only if height, previous hash, transaction IDs, hashes, signatures, balances, and nonces all line up
- a behind node fetches missing blocks by height
- if block-by-block import cannot reconcile the state, the node restores from a peer snapshot

This is enough to prove durable replication and recovery, but it is still not a validator-driven commit protocol.

## Planned WASM Contract Layer

Zephyr's planned on-chain execution model is a deterministic WASM runtime for consensus-critical smart contracts and state transitions.

The future contract model will preserve:

- deterministic WASM execution across nodes
- Rust-first contract tooling and packaging
- explicit resource accounting for instruction budget, memory growth, storage reads and writes, and emitted events or messages
- chain-defined execution pricing instead of EVM-specific compatibility requirements

## Planned Confidential Compute Marketplace

Zephyr's planned distributed compute layer is separate from on-chain contract execution.

The future compute model will provide:

- packaged WASM compute jobs with encrypted inputs
- TEE-attested workers for confidential execution
- async job submission, matching, execution, and settlement
- native-token payment for off-chain compute through worker bids plus a protocol fee

## Security Model And Prototype Caveats

This MVP intentionally favors clarity over production safety.

- Private keys are stored unencrypted in browser `localStorage`.
- Peer replication currently uses static HTTP configuration, not authenticated libp2p networking.
- Block production is still single-producer in practice for the current devnet slice.
- There is no validator acknowledgment, slashing, or Byzantine fault tolerance yet.
- Snapshot restore trusts the peer it restores from.
- DPoS election output is still an API-level calculation, not a live consensus round.
- WASM contracts and confidential compute are planned architecture targets, not implemented runtime features.

For those reasons, the current implementation is suitable for local development, API experimentation, and early distributed-state testing, but not for real funds or public deployment.

# Zephyr Chain MVP Architecture

## Overview

The current MVP is a two-part local development system:

- a Go node API that validates transactions, persists local chain state, and produces single-node blocks
- a Vue wallet that runs entirely in the browser and acts as a light client

The data flow is:

`wallet UI -> wallet signing logic -> node HTTP API -> persisted mempool -> local block production -> persisted account and block state`

There is still no peer network or distributed consensus. Deterministic WASM contracts and a confidential compute marketplace are planned later phases.

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

The wallet is a light client in the sense that it never runs consensus logic or stores full chain state.

### Node API

The node entrypoint lives in `cmd/node/main.go` and starts an HTTP server from `internal/api`.

The API layer is currently responsible for:

- reporting liveness through `GET /health`
- exposing local chain status through `GET /v1/status`
- accepting validator election inputs through `POST /v1/election`
- exposing the latest computed validator snapshot through `GET /v1/validators`
- exposing persisted account state through `GET /v1/accounts/{address}`
- accepting signed transaction envelopes through `POST /v1/transactions`
- exposing the latest committed block through `GET /v1/blocks/latest`
- funding development accounts through `POST /v1/dev/faucet`
- forcing local block production through `POST /v1/dev/produce-block`

### Durable Ledger

The durable local state lives in `internal/ledger` and is persisted as JSON under the configured node data directory.

The store currently persists:

- account balances and committed nonces
- queued mempool entries
- committed blocks
- known committed transaction IDs

On startup, the node reloads this state and rebuilds pending balance and nonce reservations from the persisted mempool.

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
9. The block producer later selects queued transactions, applies balance and nonce updates, commits a new block, removes committed transactions from the mempool, and persists the new head state.

Important current behavior:

- `accepted: true` means the transaction was queued in the persisted local mempool
- it does not mean peer replication, distributed finality, or validator agreement yet

## Block Production Path

The current block-production path is intentionally simple and single-node:

- the node keeps a persisted local mempool in arrival order
- an automatic block loop runs on `ZEPHYR_BLOCK_INTERVAL` when there are pending transactions
- a development endpoint can force immediate block production for local testing
- block production applies sender debits, receiver credits, and committed nonce advancement
- each committed block stores height, previous hash, produced time, transaction IDs, and full transaction envelopes

This is the first real commit path for Zephyr, but it is not consensus yet. It is a durable local chain, not a distributed blockchain network.

## Planned WASM Contract Layer

Zephyr's planned on-chain execution model is a deterministic WASM runtime for consensus-critical smart contracts and state transitions.

The future contract model will preserve:

- deterministic WASM execution across nodes
- Rust-first contract tooling and packaging
- explicit resource accounting for instruction budget, memory growth, storage reads and writes, and emitted events or messages
- chain-defined execution pricing instead of EVM-specific opcode or gas compatibility requirements

## Planned Confidential Compute Marketplace

Zephyr's planned distributed compute layer is separate from on-chain contract execution.

The future compute model will provide:

- packaged WASM compute jobs with encrypted inputs
- TEE-attested workers for confidential execution
- async job submission, matching, execution, and settlement
- native-token payment for off-chain compute through worker bids plus a protocol fee

## Security Model And Prototype Caveats

This MVP intentionally favors simplicity over production safety.

- Private keys are stored unencrypted in browser `localStorage`.
- Runtime state is durable on one node, but it is not replicated or consensus-validated.
- Block production is still single-node and local only.
- DPoS election output is still an API-level calculation, not a live consensus round.
- WASM contracts and confidential compute are planned architecture targets, not implemented runtime features in the current MVP.

For those reasons, the current implementation is suitable for local development, API experimentation, and early architecture discussions, but not for real funds or public deployment.

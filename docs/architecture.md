# Zephyr Chain MVP Architecture

## Overview

The current MVP is a two-part local development system:

- a Go node API that exposes health, validator election, validator snapshot, and transaction submission endpoints
- a Vue wallet that runs entirely in the browser and acts as a light client

The data flow is:

`wallet UI -> wallet signing logic -> node HTTP API -> in-memory validator snapshot / in-memory mempool`

There is no persistent database, peer network, block production, or on-chain execution layer yet. Deterministic WASM contracts and a confidential compute marketplace are planned later phases.

## Components

### Wallet

The reference wallet lives in `apps/wallet` and is responsible for:

- generating an ECDSA P-256 keypair through the Web Crypto API
- exporting the key material as JWK plus SPKI public key bytes
- deriving a Zephyr address from the SHA-256 hash of the public key
- storing the wallet backup JSON in browser `localStorage`
- creating a canonical transaction payload
- signing the payload locally
- sending the signed transaction envelope to the node API

The wallet is a light client in the sense that it never runs consensus logic or stores full chain state.

### Node API

The node entrypoint lives in `cmd/node/main.go` and starts an HTTP server from `internal/api`.

The current API layer is responsible for:

- reporting liveness through `GET /health`
- accepting validator election inputs through `POST /v1/election`
- exposing the latest computed validator snapshot through `GET /v1/validators`
- accepting signed transaction envelopes through `POST /v1/transactions`

The server keeps two in-memory data structures:

- the latest validator election result
- the submitted transaction mempool

These are process-local and disappear when the node restarts.

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
6. Sort validators deterministically by:
   - higher `VotingPower`
   - higher `SelfStake`
   - lower `CommissionRate`
   - lexicographically smaller `Address`
7. Trim the result to `MaxValidators`.
8. Assign 1-based `Rank` values.

The API stores the latest election result in memory so it can be read back through `GET /v1/validators`.

## Transaction Lifecycle

The current transaction flow is a wallet-to-mempool prototype.

1. The wallet creates a keypair in the browser.
2. It derives an address in the form `zph_<40 hex chars>`.
3. The wallet stores the full backup object under the browser key `zephyr.wallet.account`.
4. The user fills in `from`, `to`, `amount`, `nonce`, and `memo`.
5. The wallet creates a canonical JSON payload by sorting object keys before serialization.
6. The wallet signs that payload with ECDSA using SHA-256.
7. The wallet sends a `BroadcastTransactionRequest` to `POST /v1/transactions`.
8. The node validates the canonical payload, checks that the public key maps to the sender address, verifies the signature, enforces nonce and balance rules, and then appends the request to the in-memory mempool.
9. The node returns a SHA-256 hash of the full request body as the transaction ID.

Important current behavior:

- `accepted: true` means the transaction was queued in the node's in-memory mempool
- it does not mean the transaction was included in a block or finalized on-chain

## Planned WASM Contract Layer

Zephyr's planned on-chain execution model is a deterministic WASM runtime for consensus-critical smart contracts and state transitions.

The future contract model will preserve:

- deterministic WASM execution across nodes
- Rust-first contract tooling and packaging
- explicit resource accounting for instruction budget, memory growth, storage reads and writes, and emitted events or messages
- chain-defined execution pricing instead of EVM-specific opcode or gas compatibility requirements

This means contract logic that must be replayed and verified by every validator stays on-chain, deterministic, and intentionally constrained.

## Planned Confidential Compute Marketplace

Zephyr's planned distributed compute layer is separate from on-chain contract execution.

The future compute model will provide:

- packaged WASM compute jobs with encrypted inputs
- TEE-attested workers for confidential execution
- async job submission, matching, execution, and settlement
- native-token payment for off-chain compute through worker bids plus a protocol fee

This separation allows Zephyr to keep the on-chain VM deterministic while still offering access to encrypted execution power for workloads that are too heavy, too private, or too hardware-specific for consensus-layer execution.

## Planned Compute Job Lifecycle

The first marketplace design is:

1. A requester submits a job manifest, encrypted payload, resource limits, and maximum budget.
2. The protocol matches the job to the lowest-priced eligible attested worker.
3. The requester escrows native tokens on-chain before execution starts.
4. The selected worker executes the WASM workload inside an attested TEE.
5. The worker returns encrypted output, a result digest, a receipt, and an attestation reference.
6. The protocol releases payment to the worker, takes a protocol fee, and refunds unused escrow.

First-version privacy rules:

- input is encrypted to the selected worker's TEE
- output is encrypted for the requester only
- on-chain records contain job status, result digest, attestation reference, fees, and settlement outcome
- contracts may observe job status, receipts, and result digests, but not requester-private plaintext outputs

## Planned Execution And Pricing Model

Zephyr will use different pricing models for the two execution layers.

For on-chain WASM contracts, pricing is deterministic and chain-defined based on:

- instruction budget
- memory growth
- storage reads and writes
- emitted events and messages

For confidential compute jobs, pricing is marketplace-based and native-token denominated:

- workers advertise bid prices
- the protocol selects the lowest-priced eligible attested worker
- the requester funds the job through escrow
- successful settlement pays the worker and protocol
- failure, timeout, or invalid delivery can trigger refund and slashing flows

This keeps a clean distinction between:

- public, replayable WASM contract execution
- off-chain, async, requester-private confidential compute
- chain-native contract fees
- chain-native compute-market payments

## Security Model And Prototype Caveats

This MVP intentionally favors simplicity over production safety.

- Private keys are stored unencrypted in browser `localStorage`.
- The node now verifies canonical payloads and P-256 signatures, but transaction state is still single-node and in-memory.
- Nonce, balance, and duplicate checks now exist for mempool admission, but there is still no durable chain state or finalized settlement.
- The mempool is an in-memory slice, not a durable or validated transaction pool.
- The validator election output is an API-level calculation, not a live consensus round.
- WASM contracts and confidential compute are planned architecture targets, not implemented runtime features in the current MVP.

For those reasons, the current implementation is suitable for local development, API experimentation, and early architecture discussions, but not for real funds or public deployment.


# Zephyr Chain MVP Architecture

## Overview

The current MVP is a five-part local development system:

- a Go node API that validates transactions, persists chain state, produces blocks, and replicates state to configured peers
- a durable ledger that stores accounts, mempool entries, committed blocks, validator snapshots, proposals, votes, and restart-safe metadata on disk
- a DPoS election module that ranks validators deterministically from candidate and vote inputs
- a consensus message layer that validates signed proposals and votes
- a Vue wallet that runs in the browser and acts as a light client

The current data flow is:

`wallet UI -> wallet signing logic -> node HTTP API -> durable mempool -> local block production -> durable block/account state -> transport-backed peer replication`

The current consensus-artifact flow is:

`validator election -> durable validator snapshot -> signed proposal -> signed votes -> quorum certificate artifact`

This is still a development-stage system. It has more consensus structure than before, but it is not validator finality yet.

## Components

### Node API

The node entrypoint lives in `cmd/node/main.go` and starts an HTTP server from `internal/api`.

The API layer now handles:

- liveness through `GET /health`
- runtime status through `GET /v1/status`
- peer visibility through `GET /v1/peers`
- consensus visibility through `GET /v1/consensus`
- validator election inputs through `POST /v1/election`
- the latest durable validator snapshot through `GET /v1/validators`
- signed proposals through `POST /v1/consensus/proposals`
- signed validator votes through `POST /v1/consensus/votes`
- persisted account state through `GET /v1/accounts/{address}`
- signed transaction envelopes through `POST /v1/transactions`
- committed blocks through `GET /v1/blocks/latest` and `GET /v1/blocks/{height}`
- development funding through `POST /v1/dev/faucet`
- manual local block production through `POST /v1/dev/produce-block`
- internal node sync through `POST /v1/internal/blocks` and `GET /v1/internal/snapshot`

### Peer Transport Layer

The current multi-node layer is now hidden behind a transport abstraction.

Today the concrete implementation still uses static HTTP peer URLs, but the rest of the server no longer depends directly on raw HTTP calls for peer replication. The transport currently carries:

- accepted transactions
- dev faucet credits
- committed blocks
- signed proposals
- signed votes
- status fetches
- block fetches by height
- snapshot fetches for catch-up restore

This is an important production-preparation step because it gives the codebase a seam where authenticated libp2p networking can later replace the HTTP implementation.

### Durable Ledger

The durable local state lives in `internal/ledger` and is persisted as JSON under the configured node data directory.

The store currently persists:

- account balances and committed nonces
- queued mempool entries
- committed blocks
- known committed transaction IDs
- applied faucet request IDs used for idempotent peer funding replication
- the active validator snapshot selected by the latest election call
- versioned validator metadata and update time
- durable signed proposals
- durable signed validator votes with frozen voting power at record time
- durable quorum certificates built from vote power

On startup, the node reloads this state and rebuilds pending balance and nonce reservations from the persisted mempool. Validator and consensus artifacts also survive restart and snapshot restore.

### Consensus Message Layer

The `internal/consensus` package introduces signed consensus messages.

Current message types:

- `Proposal`: signed by the scheduled proposer for a height and round
- `Vote`: signed by a validator for a proposed block hash at a height and round

Current validation rules:

- the signer address must match the submitted public key
- the signature must verify with P-256 over the canonical payload
- the proposal or vote must target the node's next block height
- the proposer must match the scheduled proposer for that height
- the voter must belong to the active validator set
- votes must reference a known proposal

When a vote set for a block hash reaches the `>2/3` voting-power threshold, the node persists a quorum certificate artifact.

Important caveat: these are durable consensus artifacts, but the current node still does not use them to gate block commit or block import. That remains the next major production step.

## Current Production Gap

The repository now has enough structure to model consensus rounds, but not enough to claim finality:

- local block production can still commit without a certificate
- remote block import still validates block structure and ledger state, not certificate-backed agreement
- validator identity is not authenticated at the network layer
- there is no timeout, round-change, or crash-recovery protocol yet

That is why the project has moved from replicated prototype to consensus-preparation prototype, but it is not yet a production blockchain.

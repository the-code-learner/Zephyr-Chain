# Zephyr Protocol Security Model

This document defines the protocol-security invariants implemented for issues #2, #3, #4, and #5. The current protocol is a clean break: there is no active devnet or deployed state that requires legacy message compatibility.

## Network identity

The default local-development network identifier is `zephyr-devnet-1`. Public testnets and production networks must use distinct, stable chain IDs through `ZEPHYR_CHAIN_ID`.

`chainId` is mandatory in security-critical protocol messages. A verifier never fills in a missing chain ID on behalf of a received message. Nodes reject transactions, consensus messages, blocks, peer request proofs, and snapshots that target a different chain.

The persisted node state also records its chain ID. A newly initialized data directory is bound to the configured chain immediately, and reopening that directory with a different chain ID fails closed. Persisted blocks are checked against the same chain identity before state is loaded, preventing accidental hybrid histories when operators reuse a data directory.

## Versioned domains

Protocol objects are separated by explicit versioned domains:

- `zephyr/transaction/v1`
- `zephyr/block/v1`
- `zephyr/consensus/proposal/v1`
- `zephyr/consensus/vote/v1`
- `zephyr/transport/identity/v1`
- `zephyr/transport/request/v1`
- `zephyr/state/v1`
- `zephyr/snapshot/v1`

The relevant domain and chain ID are part of each canonical signed or hashed representation. A signature or commitment from one message type or network is therefore not valid in another domain or network.

## Canonical P-256 signatures and transaction identity

Zephyr uses raw 64-byte P-256 ECDSA signatures encoded as base64 (`r || s`). Every Zephyr signer normalizes `s` to low-S and verification rejects high-S signatures.

A transaction ID hashes the canonical transaction identity, including chain ID, signing domain, sender, recipient, amount, nonce, memo, and public key. Signature bytes are deliberately excluded, so equivalent ECDSA encodings cannot produce distinct transaction IDs.

The browser wallet reads the chain ID from the connected node before signing and refuses to sign while the node network identity is unavailable.

## Chain-bound blocks and state roots

Every block contains `chainId` and `stateRoot`. Both values are part of the canonical block hash.

The state root is a deterministic `zephyr/state/v1` commitment over consensus-critical committed account state, the active validator snapshot, and the sorted set of already-applied funding request IDs. Including funding IDs preserves faucet/funding idempotency across quorum snapshot recovery: a previously applied request cannot become valid again merely because a node restored state from peers. Volatile node-local telemetry, pending diagnostics, and local timing metadata are excluded.

Before recording or voting for a proposal, a validator deterministically executes the proposal transactions against its committed state and recomputes the expected state root. A proposal whose signed root does not match local execution is rejected. This makes the quorum certificate commit to both the transaction body and its resulting state.

## Request-bound peer authentication

The signed identity exposed in node status is a discovery/admission proof only. It does not authorize an internal request.

Every authenticated peer request binds the validator identity to:

- chain ID and `zephyr/transport/request/v1`
- HTTP method
- canonical request path and query
- SHA-256 request-body hash
- node and validator identity
- a cryptographically random nonce
- a bounded timestamp

The receiver verifies the complete proof before accepting internal state-changing or snapshot operations. Used nonces are stored in an owner-only replay database with bounded expiry. Replay of the same proof, reuse on a different endpoint/body, and reuse after node restart are rejected.

Request-body hashing is bounded by the same 1 MiB process limit even when the server is embedded through its internal handler, so authentication cannot be used to bypass the public HTTP memory limit.

All `/v1/internal/*` endpoints require a signed peer request proof even when the server is embedded through the internal handler rather than the process-facing public wrapper.

## Snapshot trust and quorum

Peer authentication proves who sent a snapshot; it does not prove that the snapshot is correct. Snapshot restore therefore requires independent state validation and validator quorum.

A snapshot proof is bound to the local chain ID, `zephyr/snapshot/v1`, committed height, latest block hash, state commitment, and validator-set version. The node validates block continuity and hashes, chain-bound transaction IDs and signatures, committed account state, applied funding IDs, validator voting-power arithmetic, and the latest committed state root before considering a proof.

The node uses its locally trusted validator set as the recovery trust anchor. Matching snapshot proofs from distinct validators are accumulated by voting power and restore is allowed only after the normal 2/3+ quorum threshold is reached. A single authenticated validator below quorum cannot replace local state.

A node with no trusted validator set cannot bootstrap itself from an arbitrary peer snapshot. Initial bootstrap must instead establish trust from genesis/checkpoint configuration or validated block synchronization before quorum snapshot recovery becomes available.

After a quorum-approved restore, the node restores only verified committed state, including committed funding-idempotency metadata. Remote mempool contents, pending round state, pending proposals/votes, and remote diagnostics are not blindly adopted; local volatile state is restarted from a clean recovery boundary.

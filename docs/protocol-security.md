# Zephyr Protocol Security Model

This document defines the security decisions for the protocol-hardening work tracked in issues #2, #3, #4, and #5.

## Network identity

The default development network identifier is `zephyr-devnet-1`. Production and public test networks must use distinct stable chain IDs. Nodes reject security-critical messages whose chain ID differs from their configured chain ID.

## Versioned signing domains

Every signed object is domain separated. The first protocol version uses:

- `zephyr/transaction/v1`
- `zephyr/consensus/proposal/v1`
- `zephyr/consensus/vote/v1`
- `zephyr/transport/identity/v1`
- `zephyr/transport/request/v1`
- `zephyr/snapshot/v1`

The domain and chain ID are part of the canonical payload. A signature from one message type or chain is invalid in another domain or chain.

## Canonical P-256 signatures

Zephyr uses raw 64-byte P-256 ECDSA signatures encoded as base64 (`r || s`). Signers normalize `s` to low-S and validators reject high-S signatures. Transaction IDs do not include signature bytes; they hash canonical transaction identity plus the public key, so equivalent ECDSA representations cannot create distinct transaction identities.

## Request-bound peer authentication

Peer authorization proofs bind the validator identity to the exact HTTP method, canonical request path, SHA-256 request-body hash, chain ID, nonce, and timestamp. Nodes reject reused nonces inside the accepted replay window. Replay state is persisted with bounded expiry so a restart does not reopen the replay window.

Signed status identity remains a liveness/discovery proof and is not sufficient to authorize a state-changing peer request.

## Snapshot trust

A snapshot is accepted only when it is bound to the local chain ID, height, latest block hash, validator-set version, and a deterministic state commitment. The snapshot commitment covers consensus-critical ledger state and excludes local-only diagnostics/telemetry.

Restore validation must fail closed before replacing known-good state. At minimum it validates block continuity and hashes, transaction IDs and signatures for the local chain, account/nonces/balance invariants, mempool invariants, validator voting-power arithmetic, and consensus certificate/proposal/vote consistency.

A signed snapshot from one peer proves provenance, not correctness; state commitment and invariant validation are required independently of peer identity.

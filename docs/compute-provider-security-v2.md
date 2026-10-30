# Protocol v2 — Compute provider transport identity and admission

## Scope and production boundary

The `cmd/compute-provider` executable is an **experimental**, restricted
compute worker. It is not a production job settlement participant. Its
`sha256` and `identity` executors are reference capabilities, not a
general scientific/AI compute runtime.

The previously unauthenticated public compute listener could accept a
validly encoded compute request from **any** connected peer. A libp2p
remote peer ID authenticates transport identity; it does not prove the
peer's job assignment, payment, or authorization. This tranche defaults
to *no execution* without an operator-controlled allowlist and explicit
acknowledgment of unverified jobs.

### Required environment

```sh
export ZEPHYR_NETWORK_ID='<64-character hex v2 NetworkID from an audited genesis>'
export ZEPHYR_PROVIDER_STORAGE='/private/operator/storage'
export ZEPHYR_PROVIDER_LISTEN='/ip4/127.0.0.1/udp/9901/quic-v1'
export ZEPHYR_PROVIDER_EXPERIMENTAL_EXECUTION='true'
export ZEPHYR_PROVIDER_ALLOWED_PEERS='<trusted libp2p peer ID>[,<another peer ID>]'
go run ./cmd/compute-provider
```

Keep the listener on loopback or behind network-level restrictions until
independent authorization, DoS defenses and metering are complete.
The explicit `EXPERIMENTAL_EXECUTION=true` opt-in **does not change
consensus**. A missing/invalid/empty/duplicate peer list, or any
unlisted remote peer, fails closed.

### Durable network-scoped P2P identity

A fresh key is generated once, cryptographically random Ed25519, and
stored under `ZEPHYR_PROVIDER_STORAGE/transport-identity.key`. Future
process starts recover the same libp2p PeerID. The key file encodes a
version and the exact v2 NetworkID; replaying it under another network
is rejected. Invalid files, excessive permissions, symlinks and
non-Ed25519 key types are rejected rather than silently replaced.

The private-key file uses owner-only `0600` permissions and the data
directory must be owner-only. **Protect and back up this key offline.**
Never share it with RPC clients, commit it, email it or use the same
key in multiple deployments. Changing or deleting it changes
the public PeerID and invalidates existing allowlist relationships.

Identity persistence is *not* a validator consensus key manager and
does not authorize votes, VRF proofs or payment claims.

### Remaining requirements before public network use

- Verify finalized compute assignment and provider identity from
  canonical consensus/settlement state, not an RPC summary.
- Authenticate request/job/WorkSpec and input commitments against
  exactly the authorized finalized workload.
- Enforce replay prevention, resource quotas, durable execution
  receipts and capacity/error-accounting per peer and per job.
- Apply bounded admission and request concurrency, storage quotas,
  CPU/memory isolation, restart recovery and audit logging.
- Isolate dangerous executors in a hardened deterministic sandbox.
- Provision peer identity using secure rotation/backup, without
  shared credentials or conflicting concurrent processes.
- Run externally reviewed fuzzing, adversarial network tests,
  multi-host failover and independent security audit.

**No trusted oracles**; **measure → simulate → activate**. Neither
transport allowlists nor experimental results are economic proof.

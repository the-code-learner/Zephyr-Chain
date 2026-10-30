# M1 / P0 — QC-certified multi-height observer history and recovery

**Status: bounded devnet observer, NOT production mainnet.**

`cmd/v2-observer` provides a non-voting Protocol v2 process with
independently pinned genesis NetworkID and validator root, durable
shard WALs, persistent QUIC PeerID and a conservatively authenticated
local finality history. The process does not possess or sign with a
validator private key.

## Starting a new observer

Verify genesis through an independent trust channel, then supply both
explicit 64-character hex commitments:

```sh
go run ./cmd/v2-observer \
  -genesis /private/genesis.json \
  -expected-network-id '<independent-64-hex>' \
  -expected-validator-root '<independent-64-hex>' \
  -data-dir /private/observer-data \
  -listen /ip4/127.0.0.1/udp/0/quic-v1
```

Use operator-pinned full `/p2p/<peer-ID>` transport addresses for
experimental connectivity. Transport authentication does not replace
cryptographic header+QC verification. The default listener is local.

## New durable history safety boundary

The observer creates a private, exclusive, **append-only, fsynced**
`certified-history.log` bound to independently computed genesis
roots, network, native token, and original validator root. For each
certified commit:

1. The existing global commit journal records the quorum-certified
   PREPARING transition durably.
2. The new history appends and fsyncs that entire intent, with a
   domain-separated hash chain, **before any shard state writes**.
3. Shard WALs apply the finalized delta and the existing global
   commit journal transitions to COMMITTED.

A crash during history append, a torn record or journal/history
disagreement **fails closed**. No partial history is truncated.

At observer restart, before opening the P2P listener, the process:
- checks the original fsynced single-session marker and owner-only
  file modes;
- re-derives exact canonical genesis roots independently in memory;
- verifies each strictly consecutive height, parent block hash, all
  predecessor/postdecessor shard roots, validator-set root and
  cryptographic QC signatures from the genesis to the final record;
- independently reapplies the historical shard object deltas to
  genesis in-memory state, rejecting inconsistent object transitions;
- requires the final history event to exactly match the latest global
  commit journal, whether PREPARING or COMMITTED;
- reconciles the last journal with current WAL shard state using the
  existing idempotent certified recovery, and restores the verified
  runtime Height/ParentHash.

**This is local authenticated history replay, not independent proof
that a remote peer or backup gave us the latest chain.** It does not
independently re-execute original transaction scripts for each past
commit: the historical state roots are QC-authorized, and recorded
state deltas are reapplied and verified. It does not implement
production snapshot compaction or archive sync.

### Deliberate fail-closed limits

- Maximum **4096 records** and **512 MiB** per history file in this
  experimental implementation. Exceeding the limit stops new
  finalized commits rather than silently discarding proofs.
- **Validator-set transitions remain unsupported** in this recovery
  path. A future implementation must prove the QC-backed next-root
  chain and preserve each activated validator set.
- Deleting, replacing or restoring an **older valid copy of all
  history, shard state and global journal files together** cannot be
  detected by this local log. Mainnet requires a separately secured
  monotonic finalized checkpoint/anti-rollback anchor or independently
  verified newer peer proof before resuming a consensus signer.
- This startup is strictly **non-voting** and does not allow recovery
  to unlock a consensus validator private key.
- Lock semantics are supported only on Linux/macOS local filesystems;
  unsupported systems fail closed.
- A prior PR44/45 data directory that lacks this history log cannot
  be silently upgraded from a single latest commit: operators must
  treat it as needing explicit verified state migration, not
  erase the session marker or trust a single RPC snapshot.

## Verification

```sh
go test ./internal/v2/node ./internal/v2/bootstrap ./cmd/v2-observer
go test -race ./internal/v2/node -run '^TestCertifiedHistory' -count=1
bash scripts/verify-v2.sh full
```

Regression tests include multiple consecutive certified empty
blocks, state object changes, forged intermediate QC with a
recomputed valid file checksum, truncated/deleted history, mismatched
final journal and incorrect state roots. Correctly replayed historical
events are **not** validation of independent multi-host consensus,
long-running production liveness or safe backup restore.

**Public mainnet remains NO-GO.** Independent security audit,
monotonic cross-host anti-rollback, distributed signer key custody,
Byzantine multi-operator validation, deterministic production WASM,
mobile DA, governance and economics remain gated.

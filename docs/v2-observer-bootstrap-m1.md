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

## Independent minimum-finality checkpoint (rollback guard)

A second, independent anchor is now supported when restarting an
**already initialized** non-voting observer. The operator supplies **both**
a minimum finalized height and the corresponding canonical **consensus
header hash** from an independent, QC-verified source. The program
verifies that exact pair occurred in its own QC-validated local
genesis-to-tip history before it reconciles any on-disk state:

```sh
go run ./cmd/v2-observer \
  -genesis /private/genesis.json \
  -expected-network-id '<independent-genesis-network-id>' \
  -expected-validator-root '<independent-genesis-validator-root>' \
  -data-dir /private/existing-observer-data \
  -trusted-finalized-height 123 \
  -trusted-finalized-hash '<independently-verified-consensus-header-hash-at-height-123>'
```

This is a **minimum ancestor** pin, not a claim the local tip is current.
The history must contain the exact certified height and hash even when
the tip is higher. A coherent but older restored backup ending below
that height, or a different fork at that height, **fails closed**.
The pin must be obtained and retained independently of the observer's
WAL, history, journal, data directory, and any untrusted RPC. In
particular the same machine's rolled-back file, or a single unaudited
peer reporting a hash, is **not** an independent trust source. A
future mechanism should persist a monotonic minimum through a
separately secured device/service with authenticated updates and
multi-peer quorum-backed proofs.

The CLI requires the height/hash pair; malformed, missing, zero and
foreign-network pins are rejected. The startup JSON explicitly reports
`finalizedHeight`, `finalizedHeaderHash`,
`trustedFinalizedHeight`, and `rollbackGuard`.
Without the pair, the program marks itself **UNANCHORED** and remains
suitable only for an experimental devnet.

Importantly, a brand-new empty observer has no certified history to
prove an existing external finalized height. It **refuses startup**
with an external positive-height pin rather than silently starting at
genesis; safe state-sync/bootstrap from certified snapshots is a
separate unfinished release gate. Operators must refresh their
externally protected minimum pin as finality progresses. An old,
authentic pin protects only up to its own height and **does not prove
freshness, detect rollbacks above it, or authorize a consensus signer**.

## Multi-witness signed minimum finalized checkpoint (experimental)

An observer restart can now require a compact **canonical JSON** checkpoint
attested by a supermajority of **independently pinned Ed25519 witness keys**.
The witness keys must be obtained using a separate trusted release/ceremony
channel, NOT from the checkpoint JSON, the node state, or an untrusted RPC.
The policy requires 3–16 different pinned keys, more than two-thirds
signatures (e.g. 3-of-4), one signature per known key, sorted ascending by
lowercase hex public key. Every included signature must verify; an unknown,
duplicate, malformed, or corrupt signer causes refusal.

Canonical JSON fields in exact serialization order:

```json
{"networkId":"<64-lowercase-hex>","validatorRoot":"<64-lowercase-hex>","height":42,"headerHash":"<64-lowercase-hex>","signatures":[{"publicKey":"<64-lowercase-hex>","signature":"<128-lowercase-hex>"}]}
```

This is a **format illustration**, not a valid signed checkpoint. Produce the
bytes with a canonical JSON encoder in the specified field order and without
extraneous whitespace, duplications or additional fields. Each Ed25519 witness
signs the **32-byte digest** returned by
`node.WitnessCheckpointDigest(FinalityCheckpoint)`, which is
`codec.DomainHash("zephyr/external-finalized-witness-checkpoint/v1", writerBytes)`;
`writerBytes` is exactly the 32-byte NetworkID, 32-byte original genesis
validator root, big-endian uint64 height and 32-byte consensus header hash.
Never sign arbitrary JSON bytes or a different digest.

For an already initialized observer with certified history covering the
signed height:

```sh
go run ./cmd/v2-observer \
  -genesis /private/genesis.json \
  -expected-network-id '<independently-reviewed-network-id>' \
  -expected-validator-root '<independently-reviewed-validator-root>' \
  -data-dir /private/observer-data \
  -witness-checkpoint-json /independent/witness-bundle.json \
  -witness-public-keys '<key1-hex>,<key2-hex>,<key3-hex>,<key4-hex>'
```

Optionally combine with `-external-anchor-file
/private/trusted-finality-anchor/minimum-finalized.log`. Both separately
provided checkpoints MUST be verified as exact QC-certified historical
ancestors independently; they need not be at the same height. A mismatch,
missing history or forged signature fails closed **before the network
listener opens**. A fresh empty observer intentionally refuses a positive
externally witnessed checkpoint until production-grade authenticated state
sync from that height exists.

**Crucial limits:** this verifies signed message provenance, not that
witnesses are deployed independently, follow the correct finalized chain,
remain online, hold uncompromised keys, have a fresh view, or keep their own
checkpoint counter monotonic. A stale but honestly signed checkpoint cannot
prove the most recent finalized block. The actual public-mainnet GO gate
requires separately deployed authenticated witness operators, published
witness key ceremony/rotation, secured monotonic storage and verified
freshness/availability drills; witness attestations are not a substitute
for native validator QCs or an independent protocol audit.

## Separately held monotonic high-water ledger (experimental)

A more automated local anti-rollback guard is available on **Linux/macOS**
with the optional `-external-anchor-file` flag. Provision a dedicated
**operator-controlled 0700 directory outside the node's data directory**.
Its file must remain in a **separate backup/failure domain** from all
observer state/WAL/history snapshots. For example:

```sh
mkdir -p /private/trusted-finality-anchor
chmod 700 /private/trusted-finality-anchor

go run ./cmd/v2-observer \
  -genesis /private/genesis.json \
  -expected-network-id '<independently-reviewed-network-id>' \
  -expected-validator-root '<independently-reviewed-validator-root>' \
  -data-dir /private/observer-data \
  -external-anchor-file /private/trusted-finality-anchor/minimum-finalized.log
```

During **first startup**, the guard creates an exclusive, 0600
identity-bound file with no finalized height. After each commit passes
the normal QC checks and its shard WALs and global journal reach the
COMMITTED state, the node appends the exact finalized height and
consensus header hash with a domain-separated hash link and **fsyncs
the external ledger before returning successful finality**. Failure
to update the ledger is a mandatory **recovery-required stop**, never
an unanchored success. Repeating a known height with a different hash
or lowering the recorded height is rejected. Concurrent writers,
symlinks, insecure parent permissions and torn or altered records
are refused rather than repaired silently.

On restart the program **requires** the previously created external
file, replays and verifies every recorded QC and state delta from
genesis, proves the last external high-water checkpoint is an exact
historical ancestor, reconciles WAL state and only then advances the
external ledger if needed. Deleting the file after an earlier session,
putting it inside the state directory or restoring a locally coherent
but older node backup below its protected high-water mark prevents
startup. Existing v2 observer directories are **not** silently
migrated: provisioning a high-water mark for already finalized
history needs an explicit separately verified ceremony.

A separate operator-supplied `-trusted-finalized-height`/
`-trusted-finalized-hash` pair is an alternative mode. The prototype
does not allow mixing that pair with the high-water file, because
both independently held commitments must be checked rather than
blindly choosing one. The ledger has a conservative **4096-record**
limit; exhaustion fails closed until independently verified
compaction is implemented.

**This is NOT an HSM/TPM or remotely witnessed monotonic register.**
The separate file's integrity can be checked for accidental truncation
and unauthorized edits, but anyone with the ability to replace or
roll back the guard itself together with the node state can still
replay an older, internally consistent history. Filesystem
append/locking and ordinary `fsync` do not imply write-once storage
or rollback resistance against a privileged attacker. Before public
mainnet GO, a truly **independent authenticated and monotonic**
operator/remote-witness/hardware anchor, freshness policy,
quorum-backed publication, and failover drills are still mandatory.

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

The external minimum checkpoint is a **trust assertion** supplied by
the operator, not a signed checkpoint protocol. Release GO additionally
requires authenticated monotonic checkpoint publication and storage,
independent verification of the validator-root transitions, robust
multi-host recovery, real validator key custody and external audits.

**Public mainnet remains NO-GO.** Independent security audit,
monotonic cross-host anti-rollback, distributed signer key custody,
Byzantine multi-operator validation, deterministic production WASM,
mobile DA, governance and economics remain gated.

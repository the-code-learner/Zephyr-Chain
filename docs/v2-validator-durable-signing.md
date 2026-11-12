# Protocol v2 — Durable validator signing-intent guard (P0)

## Implemented safety boundary

`consensus.OpenDurableSigningGuard(dir, network, validator)` maintains
one exclusive, append-only, fsynced journal for a validator identity.
Each durable record commits to the target global-header consensus hash,
height, round and previous record checksum in a domain-separated hash
chain. A record is **synced before a signature is produced**.

`node.NewProtectedService` uses the guard for **both proposals and
votes**. Ordinary `node.NewService` is retained for the existing
ephemeral in-memory conformance/devnet test harness, and is **not**
suitable by itself as a production validator constructor.

The guard:
- rejects a different target for the same height and round;
- rejects returning to an earlier height or round, even after restart;
- allows retrying the same exact intent, and increasing height/round;
- validates the complete journal and network/validator identity on
  reopen; rejects truncation, tampering, foreign network/key, bad mode
  or symlink and does not automatically rebuild/repair records;
- obtains a non-blocking kernel file lock before accessing the journal,
  preventing simultaneous processes using the **same path**;
- refuses to sign after any journal write/sync failure until reviewed
  recovery (fail-closed);
- releases the OS lock at close/crash, but **never unlinks** the journal.
- pins the canonical absolute journal pathname, original owner-only
  containing directory inode and the **single-link** locked journal inode;
  validates file/directory identity, private modes, exact on-disk record count,
  header and latest record hash before every signing intent (including
  idempotent retries) and after every durable write; a mismatch permanently
  poisons the live signer instead of allowing the next vote;
- refuses live unlink, rename/replacement, extra hardlink, parent directory
  replacement (even when the exact same journal inode moves back), truncated
  or appended records and last record/header corruption. A reopened guard
  still verifies the complete hash-chained history. Live per-signing checks
  intentionally avoid O(chain-height) replay costs, so earlier in-place
  changes preserving journal size and last record need full startup verification
  and independently protected storage to detect.


Linux and macOS have OS locking; unsupported systems fail closed.

## Operator integration

Configure a secure, independent signer-journal directory that is
**outside** a rollback-prone chain-state directory:

```go
guard, err := consensus.OpenDurableSigningGuard(signerDir, networkID, validatorID)
if err != nil { return err }
defer guard.Close()

service, err := node.NewProtectedService(runtime, validators, validatorKey, transport, guard)
if err != nil { return err }
```

The caller must use the **same** journal directory for every process
holding the same consensus key, including after restarts, and must
never start unguarded signing paths. Never copy an old journal from
backup while reusing the key. State rollback does not imply signer
rollback. Invalid/torn journal recovery requires a separately audited
procedure or retiring the affected validator key, **not truncation**.

## Strict same-host custody across separate node data directories (opt-in)

`consensus.OpenHostFencedSigningGuard(custodyRoot, networkID, validatorID)`
is the recommended stricter local constructor when running different node
instances, migrating state directories, or testing independent validator
processes on the **SAME host**. All processes holding the same consensus
key MUST receive the **same pre-provisioned owner-only host custody root**,
separate from chain state; it is never created implicitly by the validator
code. The path under this root is derived solely from full 32-byte NetworkID
and full 32-byte ValidatorID, rather than a caller-selected node data dir.
As a result, two local processes starting from unrelated node state dirs
use the exact same journal and inode flock; simultaneous access by the
second process fails closed and later restarts reuse the durable intent
history. The constructor also fsyncs the root directory after creating
the per-key subdirectory, and pins the original root directory inode/mode.
Replacing that root, even after moving the same key journal inode and
subdirectory back under its original pathname, invalidates the active
guard permanently.

For example:

```go
// custodyRoot is an existing, private, independently protected path.
// nodeDataDir may change across restarts and is NOT used for key custody.
guard, err := consensus.OpenHostFencedSigningGuard(custodyRoot, networkID, validatorID)
if err != nil { return err }
defer guard.Close()
service, err := node.NewProtectedService(runtime, validators, validatorKey, transport, guard)
if err != nil { return err }
```

The security regression launches the actual test executable in a *separate
OS process* with a different state/working directory while the first
process holds the same key. It verifies denial of concurrent custody,
recovery of the exact signed intent and refusal to double-sign after
the original process closes. It also injects root replacement with the
exact same journal inode and verifies permanent poisoning.

This is a **same-host conditional invariant**, not globally enforced.
A deployment choosing a second custody root, an attacker cloning a key
onto another host or restoring the host root and journal from an older
backup can bypass this local-only lock. A single real consensus signing
key still needs independent globally fenced HSM/remote custody and
audited multi-host migration, crash and cold-restore evidence for mainnet.
Synthetic independent *processes* are not evidence of independent *hosts*.
**Public mainnet remains NO-GO.**

## Production safety limitations

The journal does **not** solve the case where copies of the validator
private key are run on **different computers, disks or journal
directories**. Neither local `flock` nor hash-chained records provides
distributed single-signer consensus or protection against restoring
an old valid journal snapshot. It is not proof of finalized state or
proper validator-set authorization.

A production deployment still requires:

1. A cryptographically authenticated global key-ownership/signer
   policy (HSM or independently audited remote threshold/exclusive
   signer) so there cannot be two active signer copies.
2. A real multi-process v2 validator executable that always constructs
   `NewProtectedService`, refuses unsafe fallback, and binds the
   signer to a verified genesis/trust anchor.
3. Verified finalized checkpoint recovery, validator-set transitions,
   untrusted-peer isolation and crash/restart consensus tests across
   independently hosted validators.
4. Adversarial fault-injection of lost/partial writes, power failure,
   backup restoration and signer rotation with documented remediation.
5. Independent consensus-security review prior to public mainnet.

**Mainnet status: NO-GO.** A green CI run does not prove that duplicated
keys can never double-sign. No trusted oracle, VRF, governance or
economic activation is introduced in this tranche.

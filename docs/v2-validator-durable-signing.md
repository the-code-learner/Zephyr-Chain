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

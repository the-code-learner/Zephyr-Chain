# Protocol v2 — Durable worldstate exclusive-writer protection

A Protocol v2 validator must never let two independent processes
append to the same on-disk state WAL, even if both claim the same network.
Concurrent writers can diverge their memory roots, overwrite checkpoint
history and produce irrecoverable ledger corruption.

## State-directory lock

`worldstate.OpenDisk` acquires an exclusive, non-blocking OS file lock
on `state.writer.lock` **before** reading the checkpoint or replaying
the WAL. A second process opening the same directory fails with
`ErrPersistenceBusy`; it never receives a shared write handle.

- The durable writer retains the lock until `Disk.Close()`.
- Kernel flock automatically releases on process exit, including
  crashes. A restart does **not** require unlinking a stale lock file.
- The lock file is intentionally **never removed**: removing an
  active file can let two processes lock different inodes with the
  same pathname.
- The lock file is owner-only `0600`; symlink targets and improperly
  permissioned lock files are rejected.
- Failures during checkpoint loading, network validation, or WAL
  replay release the lock before returning.
- `Disk.Close` releases the lock even if WAL sync reports an error.

### Supported platforms

This tranche provides audited-by-test kernel advisory `flock`
protection for Linux and macOS. On other operating systems durable
opening returns `ErrPersistenceUnsupported` rather than silently
opening an unlocked database. Read-only state verifiers remain
conceptually separate from a durable writer.

Do not use a network filesystem with unreliable or disabled advisory
locking. Operator deployment must use a local filesystem that honors
same-inode file locks across processes, with private directory and
backup permissions.

## What this does NOT solve

This is only the **single writer** process safety invariant. It
does not provide:

- finalized consensus-height/checkpoint restoration in a live
  multi-process validator executable;
- protection against two validators signing conflicting votes with
  the same consensus key on *different* state directories;
- distributed storage consensus or off-host checkpoint authentication;
- on-disk encryption, secure key storage, data migration or pruning;
- chain-level rollback protection, provable crash recovery or
  independent multi-host persistence audits.

Those remain mainnet release blockers. Never use this lock as an excuse
to launch a live validator without a reproducible external recovery
and fork-safety evaluation.

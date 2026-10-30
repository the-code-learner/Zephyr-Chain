# P0 witness intent storage — engineering boundary

Status: **prototype / NOT production signer / mainnet NO-GO**. Tracks #55 and #42.

## Implemented

The `WitnessSigningIntentJournal` is a non-key-bearing API. A caller must supply a `WitnessFinalityVerifier` and a separately provisioned absolute-path journal. A reservation invokes that verifier before the existing `FinalityAnchorStore` writes and fsyncs an immutable height/hash record. The store rejects same-height conflicts and decreasing heights, persists across restart, detects torn tails, binds to NetworkID and original validator root, and uses a local Linux/macOS file lock. It holds no private keys, issues no signatures, and cannot act as a signer by itself.

The verifier interface alone is **not** a proof. Passing a verifier that merely compares caller assertions to a mutable RPC response defeats the intended boundary. The test verifier is a fixture and may never be used for production signer authorization.

## Mandatory production dependencies (blocking)

1. Implement an actual independent genesis-pinned QC history verifier as the only production signer input; recheck validator transitions and monotonic certified ancestry. Do not expose a public arbitrary `ReserveFinalizedIntent` route in a live signing service.
2. Extend fencing to distinct hosts/disks with one globally exclusive, attested hardware/remote signing authority per witness key. Local `flock` does not prevent cross-host double-signing.
3. Preserve an externally authenticated durable minimum height/hash and refuse operation after backups/snapshots are restored unless independent reconciliation confirms the floor. The append-only journal can be rolled back if its underlying storage can be reverted.
4. Atomically couple pre-sign reservation with the *actual* HSM signature and its commitment semantics; exercise power-cut and ambiguous remote-sign response tests. Reserve-before-sign alone does not establish an end-to-end exactly-once guarantee.
5. Independent witness operators, out-of-band pinned keys, authenticated publication, rotation, availability, stale checkpoint rejection, QC-backed recovery, multi-host fault injection and external audit remain mandatory.

## Test gates

`go test ./internal/v2/node -run 'TestWitnessIntent|TestWitnessEvidenceSet|TestWitnessQuorumConsistency' -count=1`

The full exact-head CI, V2 Lab, V2 Dependency Lock, V2 Devnet Gate and V2 Security Regression checks must all pass before merging. A green CI gate does not satisfy #42 public-mainnet launch criteria.

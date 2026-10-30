# Witness signer deployment boundary (P0 #55)

This is a **NO-GO** design and adversarial validation record, not a deployed witness signer.

## Existing verified baseline

The observer accepts canonical bounded `SignedWitnessCheckpoint` bundles only against a 3–16-key out-of-band pinned key policy and a strict >2/3 Ed25519 quorum. It verifies both the witness claim and its separate durable high-water mark against locally replayed QC-certified ancestry. The new witness-boundary regression also covers 11/16 quorum, malformed serialization, and signer ordering.

## What a production witness signer MUST do before any key is provisioned

1. Operate as an isolated non-voting service with a private immutable network and initial validator-root pin. Never sign height/hash received solely from a network peer, RPC, operator command, or unverified cache.
2. Independently replay a locally checked genesis-to-tip QC history, including validator-set transitions, before deriving any signable finalized checkpoint. Incomplete, conflicting or future history must fail closed.
3. Before the first signature, durably persist and fsync signer intent to an exclusive per-key journal in a failure domain with anti-rollback controls. Equal height with different hash and decreasing height MUST refuse. Append-only hashes by themselves cannot detect a rolled-back snapshot.
4. Maintain exactly one signing authority per key across **all** hosts, not merely one flock-protected directory; use HSM/threshold signer or attested remotely fenced signing with clearly reviewed semantics.
5. Publish authenticated canonical bundles from independently administered origins with retention, equivocation reporting, key rotation and replay/freshness policy. A timestamp alone never demonstrates canonical chain finality.
6. Reconcile a restored signer with independently authenticated durable monotonic state, and halt/alert rather than resume if an external floor cannot be verified.

## Adversarial acceptance matrix

- 3-of-4 and 11-of-16 bundles accepted; below-threshold, malformed, duplicate, unknown and unsorted signer records rejected.
- Conflicting hash at identical height, rollback to older snapshots, journal truncation and power loss during pre-sign sync cause no conflicting signatures.
- Two independent processes/hosts trying the same key at once: at most one permitted signing authority. A local file lock does not satisfy this criterion.
- Up to one-third witness outages cannot manufacture an invalid quorum. Partition/reconciliation and stale origin/CDN replay must produce an explicit pause where no safe fresh history is available.
- Genesis mismatch, incorrect validator transition, forged QC and restored unverified state fail closed.
- Publish run logs and external reviewer approvals on #55; then re-evaluate #42. Never infer production GO from CI or synthetic single-host tests.

## Current change scope

This tranche adds adversarial **verification tests only**; it deliberately does not create an API that signs arbitrary caller-supplied height/hash. Such an API would be a signature oracle and would regress the trust boundary. Signer implementation remains blocked pending a QC-verified input boundary and cross-host fencing design.

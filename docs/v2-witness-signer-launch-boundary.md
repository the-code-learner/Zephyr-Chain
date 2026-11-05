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

## Detached quorum publication assembly (software only)

`node.AssembleSignedWitnessCheckpoint` accepts a finalized checkpoint and
**already-produced** detached witness signatures; it does not store or access
private keys, does not operate a signer or publish over a network, and must
never sign a caller-provided RPC claim. It canonicalizes signer ordering and
emits a version-bound, network/genesis-root-bound JSON bundle only after the
existing strict pinned-key verifier approves the complete >2/3 quorum.
Insufficient, duplicate, unknown or forged signatures cause an error.
Consumers must repeat pinned-key signature and independent QC-history checks.

The input checkpoint MUST have been independently derived from QC-certified
genesis-anchored finality by each signing operator, and the signing custody
MUST be protected by a globally linearizable, rollback-proof remote
reservation/fencing authority before any production private key use.
An assembled bundle proves signature provenance only. It does not prove the
quorum is separately operated, that the attestations are live/fresh, or that
a publisher origin is independent. The proof of that deployment and its
cross-host crash/partition/audit evidence remains tracked in #55 and #54.

## Current change scope

This tranche adds adversarial **verification tests only**; it deliberately does not create an API that signs arbitrary caller-supplied height/hash. Such an API would be a signature oracle and would regress the trust boundary. Signer implementation remains blocked pending a QC-verified input boundary and cross-host fencing design.

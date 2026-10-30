# Protocol v2 — P3.8 VRF key-schedule evidence and beacon readiness

P3.8 introduces **non-activating key-schedule evidence** for RFC 9381
VRF use. It does not create a live key registry, randomness beacon or
consensus/governance/validator committee authority.

It builds on:

- P3.3 verification-only ECVRF-P256-SHA256-TAI;
- P3.4 deterministic unweighted committee selection;
- the existing QC-backed finalized validator-root trust chain.

## Historical epoch commitment

`committee.VRFKeyEpoch` canonically commits to:

- exact Zephyr `NetworkID`;
- producer `AccountID` (an identity **claim**, not automatically an
  authorized validator/producer);
- canonical compressed P-256 public key (33 bytes);
- sequence, starting at 1;
- claimed registration height and activation height;
- optional first inactive height (exclusive, zero if open-ended);
- previous epoch hash for every sequence after the first.

Each record has a domain-separated `vrf-key-epoch/v1` hash.
`VRFKeyScheduleCommitment` hashes the complete, sorted, unique sequence,
its record bytes, the version and a caller-supplied nonzero
`minLeadBlocks` bound.

A complete history must have no sequence gaps, duplicate or reused keys,
cross-network/cross-producer entries, overlapping activity windows,
reordering-dependent hashes, inconsistent predecessor hashes or multiple
open-ended active epochs.

Explicit gaps without an active key are allowed: **unavailability must not
be silently interpreted as proof of withholding**, and never produces
substitute randomness. Rotation may therefore cause temporary
unavailability rather than an implicit fallback.

## Anti-grinding timing guardrail

For each claimed registration:

`ActiveFromHeight > RegistrationHeight`

and

`ActiveFromHeight - RegistrationHeight >= minLeadBlocks`.

The bound is **supplied by the caller** and is part of the schedule
commitment. P3.8 does not select a protocol-wide delay value, assert that
the claimed registration is authentic or claim that a delay on its own
guarantees beacon unpredictability. A real anti-grinding proof must
establish that the key was irrevocably committed in *finalized Zephyr
state* before the relevant future randomness/input could be known, and
must define registration, rotation, retirement, revocation and network
upgrade behavior at the consensus layer.

`CheckVRFKeyScheduleAtInput` checks that the selected key's public-key
commitment matches `committee.Input.VRFKeyHash`, the network matches,
the key is active at the claimed finalized anchor height, and the
history satisfies the timing guardrail.

It **does not authenticate** any record or the finalized anchor. The
caller must independently reconstruct state/registration evidence
with QC-backed finality, authorized historical validator roots and exact
network-scoped Merkle proofs. RPC responses, admin signatures and
multisig assertions are not adequate evidence.

The current API intentionally does not claim that the `AccountID`
controls the key. That requires separately authenticated registration
operations; possession of a valid VRF proof alone does not confer
validator or governance authority.

## Verified proof observation

`ObserveScheduledVRFProof` verifies:

1. the complete schedule and its explicit delay parameter;
2. the exact active epoch/producer/network and VRF public-key binding;
3. the existing P3.4 canonical selection-input hash;
4. the exact RFC 9381 proof and output.

Only on success does it return commitments to the schedule, active
epoch, committee input, proof and verified output.

There is no unverified-output constructor and no interpretation of a
missing proof as an all-zero VRF output. No scalar contribution or
economic score is derived.

An observation **is not** a beacon, producer entitlement, finality
certificate, cryptographic proof of continuous availability or
committee authorization.

## Beacon/fallback design decision (not implemented)

Before live randomness or committee authority can be activated, the
protocol must specify and adversarially test all of the following:

1. **Producer authorization:** which finalized protocol object/root
   authorizes each dedicated VRF key, how owners prove key control,
   and how registration/rotation/revocation changes are finalized.
   External attestations cannot substitute for protocol evidence.
2. **Precommit cutoff:** a fixed activation delay, exact registration
   cutoff relative to an *unpredictable future beacon seed*, deterministic
   handling of late registrations, state reorganizations and upgrades,
   and prevention of cheap multi-key grinding/producer Sybil attacks.
3. **Round source:** one or multiple producers, their selection rule and
   the independently verifiable entropy protocol. A finalized header,
   proposer order, QC hash or ECDSA signature is NOT intrinsically a
   VRF beacon.
4. **Missing proofs and withholding:** which exact finalized evidence
   defines a deadline, absent/malformed/late proofs, timeout boundaries,
   whether omission is punishable with on-chain evidence and whether
   such punishment is objectively provable. Lack of receipt is not
   objective proof that a producer deliberately withheld.
5. **Fallback and liveness:** either an independently secure,
   deterministic, verifiable fallback protocol or a fail-closed stalled
   beacon. Do not silently reuse a previous round's output, hash an
   arbitrary header, or let an administrator supply randomness.
6. **Bias and replay resistance:** analyze producer abort/withholding,
   last-revealer attacks, committee capture, collusion, stale keys,
   duplicate proofs, cross-network, cross-purpose and cross-round
   replay, and adaptive participation.
7. **Authority integration:** bind any finalized beacon output to
   explicitly verified eligibility/validator roots, maintain the
   existing QC-backed `NextValidatorRoot` transition, and prove
   consensus safety/liveness under sustained network faults.

These are **unresolved policy and protocol choices**, not parameters
selected implicitly by this PR. No consensus or economic transition
reads the schedule or observation types.

## Scope and verification

The Go tests cover canonical ordering, full-chain rotation, the exact
exclusive retirement boundary, key reuse, network/producer substitution,
incorrect predecessor commitments, short lead times, malformed P-256
keys, proof substitution, pre-activation, cross-network and replay.

This is a deterministic verification/evidence layer suitable for
further simulations, not a production randomness beacon.

The project rule remains: **measure → simulate → activate**.

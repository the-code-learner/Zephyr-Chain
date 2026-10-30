# Protocol v2 — VRF committee selection foundations

P3.4 adds canonical public committee-selection inputs, eligibility-set
commitments and deterministic unweighted selection from a verified RFC 9381
VRF proof.

This tranche is still non-activating. It does not decide who is eligible, who
may hold a VRF key, how randomness producers are chosen, or what authority a
selected committee receives.

## Canonical selection input

A committee `Input` commits to:

- Zephyr network;
- finalized anchor height;
- finalized-header hash commitment;
- exact eligibility-set root;
- VRF public-key commitment;
- purpose hash;
- round;
- committee size.

The canonical input is domain-hashed before being passed as the RFC 9381 VRF
alpha value.

Changing the anchor, eligible set, VRF key, purpose, round or committee size
therefore changes the VRF input.

## VRF key commitment

`VRFKeyHash` commits to the compressed P-256 VRF public key.

`VerifyProof` requires the supplied public key to match that commitment and
then verifies the RFC 9381 proof through the P3.3 verifier.

This prevents substituting a different key under the same committee input, but
it does **not** prove that the committed key is authorized by Zephyr.

A future consensus layer must establish VRF-key authorization from finalized
protocol state. In particular, a key selected after the selection input is
known could be ground for a favorable output; key registration/rotation policy
must prevent that before activation.

## Eligibility commitment

`EligibilityRoot` is computed over the sorted exact set of unique non-zero
32-byte candidate identifiers.

The root is independent of caller ordering. Duplicate or zero candidates are
rejected.

`SelectUnweighted` recomputes the root from the supplied candidates and
requires exact equality with the committed root before selection.

This package does not determine whether a candidate should be eligible.
Eligibility must eventually be reconstructed from finalized, independently
verifiable Zephyr state.

## Deterministic unweighted selection

Each candidate receives a canonical rank commitment over:

- the committee input hash;
- the verified VRF output;
- the candidate identifier.

Candidates are sorted by rank, with candidate ID as a deterministic tie-break,
and the first `CommitteeSize` candidates are returned.

This avoids modulo-based index selection and caller-order dependence.

The selector is intentionally **unweighted**. It does not read stake,
ContributionScore, DeviceCredential presence, ZECI, balances, lending state or
any other implicit power source.

If a future protocol needs weighted committees, the weight source and sampling
algorithm require a separate proof-first specification and tests.

## Verified-proof type

The selector does not accept arbitrary random bytes. Outside the committee
package, a `VerifiedProof` is obtained only through `VerifyProof`, which:

1. validates the canonical input;
2. checks the VRF public-key commitment;
3. hashes the canonical input;
4. verifies the RFC 9381 P-256 proof;
5. records the verified output and proof/input commitments.

This is a software boundary, not an authorization boundary. Consensus
integration must still prove that the input commitments themselves correspond
to finalized authorized state.

## Withholding and beacon boundary

A single VRF proof has uniqueness for a fixed key/input, but a producer can
still withhold its proof.

P3.4 does not claim to solve:

- producer withholding;
- key grinding before authorization;
- multi-producer beacon combination;
- liveness under missing randomness producers;
- bias resistance of a live beacon;
- committee replacement/fallback rules.

Those semantics must be explicit before committee selection can become
consensus-critical.

## Finality and authority boundary

`FinalizedHeight` and `FinalizedHeaderHash` are committed fields, not
self-authenticating finality proofs.

No transaction, object transition, validator-root change, governance vote or
economic path consumes the selected committee in P3.4.

The existing QC-backed validator-root trust chain remains unchanged.

## Non-activation

P3.4 adds no:

- VRF-key registry or rotation transaction;
- randomness-beacon state;
- live committee scheduling;
- validator power or proposer changes;
- governance voting weight;
- ContributionScore;
- reward routing;
- minting, levy, fee split or supply mutation;
- trusted oracle or external randomness feed.

The next live step, if pursued, must bind these commitments to finalized
protocol state and specify producer authorization plus withholding/failure
semantics before any selected committee can carry authority.

# Protocol v2 — RFC 9381 VRF verification foundations

P3.3 adds a verification-only implementation of the RFC 9381
`ECVRF-P256-SHA256-TAI` ciphersuite.

This tranche deliberately stops before randomness-beacon or committee
activation.

## Ciphersuite

The implementation fixes the RFC 9381 suite byte to `0x01` and uses:

- NIST P-256;
- SHA-256;
- compressed 33-byte public keys and curve points;
- 16-byte VRF challenge values;
- 32-byte response scalars;
- 81-byte proofs;
- 32-byte VRF outputs.

Public keys and proof points are decoded on P-256 and the public key is
validated before verification. The response scalar must be below the curve
order.

The implementation follows the RFC verification equation:

`U = s*B - c*Y`

`V = s*H - c*Gamma`

and returns the VRF output only after the recomputed challenge matches the
proof challenge.

## Conformance evidence

The test suite pins the implementation to RFC 9381 Appendix B.1 examples 10
and 11, including their expected VRF outputs. It also checks the encoded curve
point from example 10.

Tests reject:

- input substitution;
- proof mutation;
- public-key substitution;
- malformed key/proof lengths;
- over-bounded protocol inputs;
- response scalars outside the P-256 group order.

The implementation uses only the Go standard-library P-256 and SHA-256
primitives; no new cryptographic dependency is introduced.

## Public-input boundary

RFC 9381 notes that try-and-increment encoding has input-dependent running
time. Protocol committee-selection inputs are intended to be public finalized
data, never secrets. This tranche therefore does not expose the construction
as a privacy-preserving VRF for secret inputs.

The low-level verifier also bounds alpha length so an untrusted caller cannot
force repeated hashing over an arbitrarily large input.

## What verification proves

For a supplied public key and exact input, a valid proof establishes the
RFC 9381 VRF relationship and its unique pseudorandom output.

That does **not** establish that:

- the public key is an authorized Zephyr VRF key;
- the key holder was entitled to produce a beacon value;
- the producer could not withhold a valid output;
- a randomness beacon is bias-free under missing participants;
- an eligible committee set is authentic;
- the output has been finalized by Zephyr;
- any selected committee has governance, validator or economic authority.

A VRF proof is cryptographic evidence, not authorization.

## No QC-hash shortcut

Protocol v2 proposer scheduling remains deterministic weighted round-robin.
ECDSA signatures, quorum-certificate hashes and ordinary finalized header
hashes are not relabeled as VRF outputs.

They may eventually be inputs to a separately specified randomness protocol,
but they do not become unpredictable/verifiable-random merely because they
are finalized.

## Before committee activation

A future stacked tranche must define and verify at least:

1. a finalized authorization rule for dedicated VRF public keys;
2. a canonical public input bound to a finalized chain anchor, purpose and
   exact eligibility-set commitment;
3. withholding/failure semantics for the randomness producer or producers;
4. deterministic committee selection from verified output;
5. replay/domain separation across epochs, rounds and committee purposes;
6. interaction with the existing QC-backed validator-root trust chain;
7. adversarial tests for missing, stale, substituted and equivocated evidence.

If a multi-producer or threshold beacon is chosen, its combination rule must
also be consensus-verifiable. An external randomness service, API, RPC summary
or administrator assertion is not acceptable.

## Non-activation

P3.3 adds no:

- VRF key registry;
- randomness beacon state;
- committee membership;
- validator power;
- governance weight;
- ContributionScore;
- reward or monetary activation;
- transaction operation or execution hook consuming VRF output.

The purpose is to establish a standards-pinned verification primitive before
any protocol authority can depend on it.

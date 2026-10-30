# Protocol v2 — Citizen challenge evidence

P3.1 adds a canonical challenge/response envelope for Citizen devices that are already bound to an account by a finalized `DeviceCredential`.

This tranche is intentionally proof-first. It does **not** create a ContributionScore, reward weight, validator weight, governance weight, personhood claim or hardware-attestation claim.

## What is verified

A `DeviceChallenge` is canonically bound to:

- Zephyr network;
- finalized DeviceCredential ObjectID;
- credential sequence;
- exact finalized credential height;
- required DeviceCredential capability bitset;
- a non-zero challenge nonce;
- a non-zero context hash.

The device signs the canonical challenge bytes through the existing DeviceCredential challenge-signature rule.

Verification re-parses the finalized credential object bytes instead of trusting caller-supplied credential metadata. It then requires:

- the challenge network to match finalized state;
- the challenge ObjectID to match the finalized DeviceCredential;
- the challenge sequence to match the finalized credential sequence;
- the challenge height to match the finalized object proof height;
- all challenged capabilities to be present on the finalized credential;
- a valid low-S P-256 signature from the key bound to that credential version.

The resulting `VerifiedDeviceChallengeEvidence` records the finalized state root, owner, credential identity/version, challenged capabilities, challenge hash and context hash.

## What this does not prove

A valid response proves only that the device controlling the finalized credential key signed the specified challenge.

It does **not** by itself prove:

- that the response happened at a particular chain height or wall-clock time;
- that the device performed useful work;
- that a capability was actually exercised;
- hardware identity, vendor identity, TEE integrity, location or personhood;
- economic contribution;
- entitlement to rewards, governance power or validator weight.

Challenge freshness is therefore only as strong as the source of the nonce and context. An off-chain verifier can use a fresh local nonce for an interactive session, but that is not consensus evidence of timing or work.

## Replay and substitution boundary

The signature commits to the full canonical challenge, including network, credential ObjectID, sequence, finalized height, capability requirements, nonce and context hash.

Changing any of those fields invalidates the signature or finalized-state binding.

A future protocol that consumes challenge evidence must also define how challenge identifiers are issued and deduplicated. Reusing multiple valid signatures over the same challenge must never create multiple contribution credits.

## Relationship to ContributionScore

P3.1 deliberately stops before defining a composite ContributionScore.

DeviceCredential presence is authorization metadata, not contribution. A successful challenge is key-control evidence, not productive-use evidence.

Any later score or eligibility system must consume only narrowly defined finalized/verifiable evidence, keep unavailable evidence explicit, and be simulated before activation. No reward, governance or monetary path is added here.

## Next proof layer

A later P3 tranche can derive challenge selection from protocol-verifiable randomness or finalized committee state, for example a VRF/committee construction whose seed and membership are independently checkable by validators.

That later layer must define:

- deterministic challenge assignment;
- replay/deduplication rules;
- finalized inclusion or receipt evidence for responses if chain timing matters;
- committee selection and failure handling;
- explicit separation between liveness/key-control evidence and economic contribution.

The no-trusted-oracle rule remains unchanged.

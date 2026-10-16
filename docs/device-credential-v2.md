# Protocol v2 — DeviceCredential

DeviceCredential binds an account-controlled device public key to explicit protocol capabilities in finalized Zephyr state.

It is deliberately narrow. A DeviceCredential proves:

- the account owner authorized the current device public key;
- the credential is present in finalized Zephyr state;
- the credential version/sequence has not been revoked or replaced;
- a device that answers a challenge with the bound P-256 key controls that key.

It does **not** prove hardware identity, manufacturer identity, TEE integrity, geographic location, personhood, or any other fact that Zephyr cannot verify itself.

## Lifecycle

- Register: the account owner creates a credential with a P-256 public key, capability bitset and optional expiry height.
- Update/rotate: the owner consumes the current credential version, keeps the same ObjectID, increments object version and credential sequence, and may rotate the key/capabilities/expiry.
- Revoke: the owner consumes the credential without creating a replacement.

Challenge signatures bind:

- credential ObjectID;
- current sequence;
- challenge bytes.

A signature from an older sequence is invalid after rotation.

## Capabilities

Current capability flags are:

- verify finalized headers;
- relay;
- sample data availability;
- execute recent state;
- serve cache.

Capabilities are authorization metadata, not proof that the device actually performed work. Economic credit must come from separate finalized/verifiable evidence.

## Citizen verification

Citizen/light clients first verify the normal finalized object bundle: validator certificate, shard commitment, Merkle proof and object proof. Only then is the DeviceCredential parsed and checked for ownership and activity at the finalized height.

This keeps the rule: verify finalized state; do not trust an RPC assertion.

## Trust boundary

DeviceCredential is self-controlled by the account. There is no privileged issuer in this tranche and no external attestation oracle. Future hardware attestations, if ever introduced, must have a verifier whose evidence is independently checkable by the protocol; a signed vendor statement alone is not enough for consensus-critical meaning.

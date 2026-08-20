# Zephyr Protocol v2 reference foundation

This directory contains the clean-break Protocol v2 reference types and proof-oriented execution primitives.

It is intentionally isolated from the current v1 runtime while the Consensus & Performance Lab is used to compare behavior and performance. The goal is to migrate only after the v2 path proves safety, recovery and measurable performance/resource advantages.

Packages:

- `codec`: bounded canonical binary encoding primitives.
- `types`: typed network/account/node/validator/object/token/contract/job identifiers.
- `genesis`: canonical genesis and genesis-derived network identity.
- `state`: incremental Sparse Merkle Tree with compressed inclusion/absence proofs.
- `worldstate`: proof-native object state backend contract and reference in-memory backend.
- `object`: object and coin model.
- `tx`: signed proof-carrying v2 transactions and canonical wire format.
- `execution`: native transfer and token-creation reference executor.
- `assets`: protocol-native token definition model.
- `merkle`: ordered Merkle commitment/proof utility.
- `sharding`: shard routing, global/shard commitments and cross-shard receipt primitives.
- `da`: data-availability chunk commitments and sample verification.
- `citizen`: mobile/light verifier and resource-aware participation policy.
- `contracts`: deterministic WASM deployment/runtime boundary.
- `compute`: native distributed-compute market objects and verification modes.
- `transport`: separated consensus, transaction and light-proof transport capabilities.

The authoritative architecture and implementation sequence is documented in `docs/protocol-v2.md`.

Do not interpret the presence of an interface or protocol type as a production-complete feature. In particular, production WASM execution, erasure coding, dynamic sharding, durable v2 storage, mobile OS integration and compute-provider execution/settlement are explicit later integration milestones.

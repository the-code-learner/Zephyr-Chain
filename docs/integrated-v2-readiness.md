# Zephyr Chain — Integrated v2 development readiness (October 2026)

## Scope of this integration

This integration merges the previously stacked **P2 #19–#22**, **P3
#23–#28**, **P3.7 #37** and **P3.8 #38** development tranches plus a
local verification gate. It is a substantially more complete, testable
development baseline, **not a public mainnet release**.

The initial v2 foundation already had deterministic proposal/vote/QC
consensus, QC-backed validator root transitions, P-256 proof-carrying
transactions, persistent state/WAL/checkpoints, Merkle proofs,
asynchronous cross-shard native receipts, Citizen verification, custom
tokens, reference metered smart contracts, compute job/settlement
foundations and shadow economics.

Newly integrated evidence/protocol foundations include:

- same-shard and explicit cross-shard **bilateral native ZPH lending**;
- on-chain **DeviceCredential** version/rotation/revocation foundations
  and Citizen **signed key-control challenge** verification;
- finalized **ZECI shadow telemetry**, with independent economic evidence
  availability rather than a fake composite score;
- governance **proposal/timelock commitments**, without votes or execution;
- RFC 9381 **P-256 VRF proof verification** and deterministic,
  *unweighted* committee *selection evidence*;
- separate Citizen/economic contribution **shadow evidence manifest**;
- finalized **paid provider attribution** and canonical complete
  provider-evidence-set commitments;
- historical VRF **key schedule and timing-bound evidence** with
  fail-closed stale/missing proof semantics.

These are deliberately **non-activating** unless explicitly stated.
Their presence does not automatically mint ZPH, assign validator
power, enable government execution, change reward routing or run a
live randomness beacon.

## Verify from a clean checkout

Tooling: Go version in `go.mod` (Go 1.26.0 at integration time),
Node 24, npm corresponding to Node, and a host able to run loopback
QUIC/libp2p tests. No paid services or separate servers are required.

Run:

```sh
bash scripts/verify-v2.sh quick
bash scripts/verify-v2.sh full
```

The `quick` gate runs Go vet/tests, lockfile-based wallet install,
strict high/critical npm audit, TypeScript and Vite build.

The `full` gate adds repeated quorum-fault/recovery tests and 7- and
20-validator real-network-stack ephemeral devnet correctness tests.
The devnet topology is **one local OS host**; it is not a geographically
distributed network or production throughput benchmark. Each command
fails closed; no test failure is hidden or waived.

The protected `main` branch additionally requires green GitHub
Actions checks on the **exact integration PR merge SHA**:
`CI`, `V2 Lab`, `V2 Dependency Lock`, `V2 Devnet Gate`.
A passing check on a previous PR head does not certify a later
different head or retargeted merge ref.

## Current safety boundary

- No trusted oracles: consensus-critical evidence must be
  independently reproducible from authenticated chain state,
  finalized QC/headers, canonical proofs and deterministic execution.
- DeviceCredential establishes account-authorized key control, not
  a unique human/hardware identity, useful work or economic power.
- VRF key schedule records remain *claims* until a future protocol
  authenticates registration from finalized state and defines anti-key-
  grinding, beacon producer authorization, withholding and fallback.
  VRF outputs are not yet live committee randomness or authority.
- No canonical scalar ContributionScore/normalization or weight
  has been approved. Provider payments are evidence, not a score.
- Governance object and timelock hashes do not authorize execution;
  the QC-backed validator-root trust chain is unchanged.
- Economics uses **measure -> simulate -> activate**. No live
  monetary issuance, idle levy or network-wide reward/rate changes.
- 1,000,000 finalized-through-consensus TPS remains an aspirational
  research goal. Neither unit tests nor same-runner devnet results
  establish that throughput.

## Not yet production-ready

**P0:** independent cryptographic/protocol security audit, real
multi-host authenticated fault/restart experiments, public
genesis/validator upgrade and release/recovery procedures.

**P1:** deterministic production WASM engine and Rust SDK;
production compute provider process and dispute verification
mechanisms; shard-aware gossip, 4/16-shard controlled-hardware
scaling and recovery.

**P2:** real-device Citizen caching/relay/DA-battery measurements;
finalized VRF-key authorization and resilient beacon semantics;
governance voting/authorization/execution; economic contribution
policy design with reproducible adversarial simulation.

Do not activate an incomplete protocol feature merely because
its code is now present on `main`.

## Regression or rollback

If exact-head protected checks fail, stop the integration and fix
the candidate on its own branch; never bypass the ruleset. If
a post-merge issue is found, use a reviewed revert PR with the
same checks; do not force-push `main` or rewrite finalized
history. Record any resulting implementation/policy changes in
the project's Postmaster continuity memory.

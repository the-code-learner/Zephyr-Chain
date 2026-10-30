# Zephyr Chain — Mainnet security and release gates

**Status: PUBLIC MAINNET NOT APPROVED.** This document is a proof and
engineering acceptance plan, not a claim that the goals below are complete.
A successful PR build or a one-host devnet does not constitute a secure
decentralized production network.

## Non-negotiable invariants

- **Safety dominates throughput:** fail closed when quorum, finalized
  validator roots, authentication or deterministic execution cannot be
  verified. Do not treat a missed progress deadline as permission to
  rewrite consensus history or adopt unverified snapshots.
- **No trusted price, compute, identity or randomness oracles** for
  consensus-critical transitions. Publishing an assertion on-chain
  does not make its underlying fact verifiable.
- **Citizen-first verification:** a low-resource user must be able to
  verify finalized headers, quorum certificates, validator transitions
  and state/receipt proofs without trusting one RPC.
- **Economic activation is separate from measurement:** do not enable
  monetary issuance, idle levies, contribution weights or reward
  multipliers merely because telemetry or shadow state exists.
- **1,000,000 TPS finalized through consensus** remains an experimental
  target. Only reproducible, multi-node adversarial benchmarks with
  valid QC and confirmed application state may be used as evidence.

## Evidence required before launch

| Area | Currently available foundation | Missing production evidence / acceptance criterion |
| --- | --- | --- |
| Genesis and native supply | Canonical NetworkID, validator-root derivation, supply-overflow guard, offline pinned-anchor audit | Public independently reviewed genesis, reproducible allocation audit, owner/validator key ceremony, signed distributed fingerprints |
| Consensus and BFT finality | P-256 low-S signatures, proposal/vote/QC, validator-root transition, journaled protected signing-service constructor, 7/20 validator same-host gate | Independent adversarial consensus audit, >=3 independent hosts/providers, message partitions, equivocation, crash/restart, key rotation and long-duration liveness |
| State, WAL and recovery | Canonical Sparse Merkle proofs, WAL/checkpoint and replay, Linux/macOS exclusive same-directory writer lock | Independent monotonic finalized checkpoint/anti-rollback anchor, multi-height validator-root transitions, production state migration, power-loss recovery, cross-directory validator-key double-sign protection, compaction and archival recovery drills |
| Peer networking | QUIC/libp2p network-scoped protocols; durable provider PeerID | Secure validator transport identity persistence, authenticated peer admission quotas, replay-aware gossip, abuse limits, timeouts, independent-host recovery |
| Execution and transfers | Deterministic state execution, native ZPH, custom tokens and proof-carrying transactions | Full adversarial transaction fuzzing, bounded mempool, replay semantics, resource budgeting, gas accounting and cross-version compatibility tests |
| Sharding | Finalized asynchronous receipt/proof import and replay protection | Shard-aware multi-host sync, controlled 4/16 shard fault tests, validator-set transitions across shards and long-range proof pruning |
| Wallet / Citizen | Local header, QC, Merkle and challenge verification | iOS/Android resource/battery/relay/DA tests, multi-peer comparison, trustworthy update and recovery, anti-phishing key UX, reproducible signed releases |
| Smart contracts | Deterministic metered reference runtime | Production deterministic WASM, compiler/ABI, syscall/resource sandbox, gas/upgrade contracts and external security audit |
| Distributed compute | Offers, escrow, assignments, receipts and settlement primitives | Finalized provider-job authentication, provider isolation, execution metering, anti-DoS quotas, dispute proofs, SLA and economically secure incentives |
| Identity / DeviceCredential | Canonical credential lifecycle and Citizen key-control evidence | Hardware/user identity semantics must remain explicit, never equate one credential to one human, no silent economic eligibility |
| VRF / random beacon | RFC 9381 verification, unweighted selection and historical key-schedule evidence | On-chain finalized key authorization, anti-grinding cutoff, withholding/last-revealer proof model, unbiased multi-producer randomness and deterministic liveness fallback |
| Governance | Proposal/timelock hash foundations | Canonical validator-authorized voting, conflict resolution, quorum proof, security timelock, rollback/upgrade rules without overriding existing QC root |
| ZPH economics | ZECI/ZCSI/ZPPI simulations and shadow economic state | Canonical verifiable production parameters, replay-safe epoch activation, invariant-preserving simulation and audited accounting, explicit community/governance approval |
| Mobile and accessibility | Reference wallet, Citizen light proofs | Independent low-end mobile hardware validation, offline/verifier-first UX, light proof transport coverage and signed builds |
| Deployment and operations | CI, v2 lab, one-host devnet, pinned non-voting observer bootstrap with bounded QC-certified multiheight history replay and operator health/alert foundations | Production runbooks, off-host encrypted recovery/backup, telemetry/privacy review, fire drills, security contact, incident response and SLOs |

## Required external evidence

A release candidate must provide reproducible artefacts for:

1. Independent cryptographic, protocol, runtime and smart-contract
   security review, plus documented remediation.
2. A public, deterministic genesis review with independent NetworkID
   and validator-root agreement before any validator starts.
3. Multi-host, multi-operator and multi-provider consensus/finality
   fault injection with sustained partitions and crash recovery.
4. Threat-modelled resource exhaustion and untrusted-peer admission
   tests for P2P, RPC, compute and DA paths.
5. Data migration and rollback/recovery drills where the signed
   finalized chain is never replaced by a remote unverified snapshot.
6. Controlled hardware throughput studies across shard counts,
   including CPU/memory/bandwidth cost, latency p50/p95/p99, finality
   verification and observer/Citizen correctness.
7. Signed, reproducible release binaries, SBOM, dependency advisories,
   secure operator key handling, emergency patch and rotation policy.

A blocker cannot be waived by a product launch date, a trusted
administrator signature or an external RPC assertion.

## Launch decision

The engineering gate is **NO-GO** until every unresolved security
assumption above is independently tested and documented. New security
tranches should reduce this list through executable evidence, not
merely mark checkboxes. Production activation should be a separate,
reviewed governance/release action after the protocol has demonstrated
safe recovery and no trusted-oracle dependencies.

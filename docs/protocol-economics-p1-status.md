# Protocol Economics P1 Status

Working paper reference: 19 August 2026, v0.1.

This tranche is **shadow-only**. It does not activate live monetary supply changes, an idle-capital levy, reward-routing changes or production benchmark claims.

## Implemented in this branch

- dynamic per-work-class ZCU references from verified slot evidence;
- deterministic weighted median, EWMA smoothing and per-epoch rate limiting;
- ZPPI price-relative basket for compute, data availability and storage;
- reliability/coverage gates for thin economic data;
- canonical annual ZPPI factor `1_020_408_163`, derived from the ~2% annual purchasing-power objective;
- tokenomics documentation migrated away from a fixed 2% supply-growth objective;
- capital-lot lineage primitives, dormancy histograms and productive-coverage measurement;
- deterministic state carrying-cost estimation and fragmentation-pressure simulation;
- prospective `IdleCapitalTracker` with amount-conserving lineage, canonical checkpoints, local/cross-shard targets and explicit productive-use hooks;
- adversarial/edge coverage for repeated self-cycling, payment/change outputs, mixed-age merge, fragmentation, duplicate materialization and malformed/invalid transition handling;
- finalized-evidence lineage derivation from transaction/result/witness data without heuristic transfer identifiers;
- canonical cross-shard tracker identity using `CrossShardReceipt.Hash()` and protocol `DestinationObject()` identity;
- opt-in `IdleCapitalTracker` integration inside the finalized `EpochCollector` preview/apply path;
- canonical export receipts passed from the runtime into finalized economic observations after the source state root is known;
- collector checkpoint version 2 carrying idle-capital state while retaining restore support for version 1 checkpoints;
- restart/canonical checkpoint and local/cross-shard collector integration tests;
- finalized compute productive-evidence derivation that verifies the referenced consumed job object and canonical settlement receipt, reconstructs exact/strict-majority settlement, isolates paid compute escrow from collateral/refunds, and computes overflow-safe `paid / escrow` coverage in basis points;
- stable shadow component identities for owner escrow and each provider collateral position, keyed independently of compute-job object versions so lineage updates cannot conflate productive spend with collateral;
- finalized compute capital-lineage routing through job creation, offer/accept collateral locking, assignment ingest, settlement and expiry;
- deterministic settlement legs that preserve escrow/refund/payment and per-provider collateral return/slash lineage even when execution merges payment and collateral into the same native coin;
- exact paid-escrow productive reset at finalized settlement, while refund and collateral preserve their prior idle age; expired compute receives no productive reset;
- tracked cross-shard compute coin receipts materialize from canonical receipt identity even when the execution coin has no CreatedHeight stamp, because the tracker already carries authoritative lineage age;
- productive-evidence/component/lineage tests for exact replicated settlement, expiry, collector integration, stable escrow/collateral identities, duplicate-provider rejection, tampered receipts, generic-transfer exclusion and maximum-`uint64` capital values;
- restart/fault coverage for compute capital with byte-canonical collector checkpoint/restore over a larger tracker state, pending cross-shard compute settlement lineage, post-restore import continuation, truncated/trailing checkpoint rejection and rollback proving temporary settlement legs cannot leak after a failed finalized transition;
- finalized-through-consensus benchmark report core with warm-up separation, measured finalized TPS, p50/p95/p99 finality, environment/config metadata, JSON output and safety/liveness invalidation;
- V2 Lab wiring that derives small machine-readable reports from real single-process candidate build -> signed proposal -> quorum QC -> finalized commit across seven runtimes, with warm-up submissions excluded from the measured window;
- reusable `FinalityHarness` with deterministic pre-measurement identities, independent per-validator/per-shard world states, exact cross-shard-ratio validation, canonical receipt proofs/imports, destination receipt-marker verification and an import-only drain outside the measured window;
- CI coverage for 1-shard native transfer, 2-shard 50% cross-shard receipt/import and a 4-shard cross-shard smoke scenario;
- `cmd/v2-finality-matrix` plus a manually triggered `V2 Finality Matrix` workflow covering 1/2/4/8 shards and 0/10/25/50% cross-shard configurations with JSON artifact output;
- benchmark methodology documented in `docs/finality-benchmarking-v2.md`, including the explicit single-process topology boundary and publication gate.

The pre-existing supply-growth controller remains a legacy **shadow simulator** until the ZPPI path is integrated into the monetary epoch path. ZCSI remains primarily a compute incentive/capacity-routing signal rather than the monetary anchor.

## Productive-capital boundary

A compute job object can contain both the owner's escrow and provider collateral, while settlement outputs can aggregate provider payment and collateral return into the same native coin. Marking the whole job object productive would therefore over-credit collateral.

P1 now routes those economic subcomponents through deterministic shadow identities in `IdleCapitalTracker`. At a canonical finalized settlement, only the exact paid portion of escrow receives a productive age reset; unused escrow refunds and provider collateral retain their prior lineage age. Deterministic settlement legs keep those histories separate until they are merged into the actual settlement coin. Expiry returns capital without a productive reset. Generic transfers and arbitrary contract calls remain ineligible for productive-use credit.

## Activation discipline

```text
measure first -> simulate second -> activate last
```

No live mint, burn policy, levy or reward-routing change is implied by the presence of telemetry or simulation code.

## Remaining P1 work

- execute and review the manually triggered 1/2/4/8-shard finalized-through-consensus matrix and retain its machine-readable artifact; the matrix infrastructure is implemented, but no production-capacity claim follows from its single-process topology;
- run long-horizon public/adversarial economic simulations before proposing any monetary or idle-capital activation;
- keep CI/manual single-process finality outputs as instrumentation only; publish no capacity claim until representative distributed finalized-through-consensus runs have documented hardware, topology, workload and safety/liveness results.

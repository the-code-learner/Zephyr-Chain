# Evaluation Plan

## 1. Functional Consensus Scenarios

- Certified happy path across two or more validators.
- Timeout-driven proposer rotation and reproposal.
- Conflicting proposal rejection.
- Partial quorum and stalled round behavior.

## 2. Recovery And Restart Scenarios

- Restart with pending local proposal or vote actions.
- Rejected import leading to pending recovery state.
- Snapshot-assisted peer repair with local diagnostic preservation.
- Multi-peer divergence cases once the implementation supports them.

## 3. Observability Validation

- Verify `/v1/health` and `/v1/alerts` under ready, degraded, and failing scenarios.
- Verify `/v1/metrics` and `/metrics` remain consistent for the same incident.
- Verify structured logs correlate with diagnostics, peer incidents, recovery, and alert state.

## 4. Performance And Overhead

- Transaction validation latency.
- Block-template construction latency.
- Proposal and vote persistence overhead.
- Snapshot restore and peer repair latency.
- Observability overhead from metrics and structured logs.

## 5. Artifact Plan For The Paper

- Tables summarizing implemented versus future features.
- Sequence diagrams for proposal, vote, commit, restart, and peer repair flows.
- Example alert and metric excerpts for failure diagnosis.
- Reproducible local devnet scenarios based on integration tests.
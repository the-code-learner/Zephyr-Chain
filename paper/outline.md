# Manuscript Outline

## 1. Introduction

- Problem statement: many blockchain prototypes under-specify recovery and operator evidence during the path from prototype to production.
- Zephyr thesis: consensus, recovery, and observability should be designed together.
- Contributions summary.

## 2. System Goals And Scope

- Validator-driven consensus on a single-chain path.
- Deterministic WASM and confidential compute as later phases, not current claims.
- Explicit non-goals for the current paper: no claim of public-mainnet readiness, no claim of mature staking economics yet.

## 3. Architecture Overview

- Node API, durable ledger, consensus message layer, automation loop, peer replication, wallet.
- Figure idea: end-to-end data path from wallet submission to certified commit and peer repair.

## 4. Consensus And Agreement Model

- Durable validator snapshots and proposer scheduling.
- Self-contained proposals, votes, and quorum certificates.
- Timeout-driven round advance and proposer rotation.
- Certificate-gated commit and import.
- Figure idea: proposal-vote-certificate-commit pipeline across two validators.

## 5. Recovery And Peer Repair

- Consensus action WAL.
- Pending replay, blocked import, and snapshot restore handling.
- Peer-sync incident history and derived summaries.
- Figure idea: stalled import leading to snapshot repair and preserved local diagnostic context.

## 6. Operator Observability

- Status, consensus, round evidence, block readiness, diagnostics, and peer history.
- Readiness via `/v1/health`.
- Derived alerts via `/v1/alerts`.
- JSON metrics via `/v1/metrics`, Prometheus metrics via `/metrics`, and structured logs.
- Figure idea: observability stack layered from raw artifacts to alerts.

## 7. Evaluation Plan

- Functional correctness scenarios.
- Recovery and restart experiments.
- Operator observability validation.
- Performance and overhead measurements appropriate for the current implementation stage.

## 8. Limitations And Future Work

- HTTP transport still in place instead of authenticated `libp2p`.
- Validator lifecycle still off-chain.
- Deterministic WASM and confidential compute not implemented yet.
- Public-testnet and production operations still future phases.

## 9. Conclusion

- Zephyr as a production-oriented research artifact centered on evidence-rich consensus hardening.
# Implementation Status For Paper Claims

## Claims The Paper Can Make Today

- Zephyr persists validator snapshots, consensus round state, proposals, votes, quorum certificates, consensus recovery actions, diagnostics, and peer-sync incident history on disk.
- Zephyr supports self-contained proposals that carry the full candidate transaction body and deterministic template fields.
- Zephyr can require matching proposals and certificates before local block commit or remote block import.
- Zephyr includes a timeout-driven automation slice with proposer rotation, stored-candidate reproposal, and proposer-side auto-commit on the current devnet path.
- Zephyr exposes signed validator transport identity, strict peer admission, peer-to-validator binding, peer incident history, and snapshot-assisted catch-up over the current HTTP transport.
- Zephyr includes operator-facing status, readiness, alert, JSON metric, Prometheus metric, and structured-log surfaces.

## Claims The Paper Must Keep As Future Work

- Authenticated `libp2p` discovery and transport.
- On-chain staking, delegation, governance, and slashing.
- Deterministic WASM contract execution.
- Confidential compute job orchestration, attestation, escrow, and settlement.
- Public testnet and mainnet operations.

## Suggested Evidence Map

- Consensus persistence and recovery: `internal/ledger`
- Proposal and vote primitives: `internal/consensus`
- API, alerts, readiness, metrics, peer admission, and automation: `internal/api`
- Runtime wiring: `cmd/node/main.go`
- Product and roadmap framing: `README.md`, `docs/roadmap.md`, `docs/architecture.md`, `docs/api.md`

## Guardrails For Writing

- Distinguish clearly between implemented behavior, roadmap direction, and research hypotheses.
- Use the paper to explain why observability and recovery are first-class protocol concerns in Zephyr.
- Avoid presenting Zephyr as a finished public-chain protocol; present it as a staged production-hardening architecture.
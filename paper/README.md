# Zephyr Chain Paper Workspace

This directory holds working material for an academic paper about Zephyr Chain. It should be maintained alongside the code so the manuscript never claims capabilities that the repository does not actually implement.

## Current Paper Focus

The current paper angle is:

- validator-driven consensus hardening on a single-chain execution path
- restart-safe recovery and peer-repair mechanics for a development-to-production transition
- operator-facing observability through status, readiness, alerts, JSON metrics, Prometheus metrics, and structured logs
- a forward-looking path toward deterministic WASM execution and a confidential compute marketplace

## Files

- `abstract.md`: working abstract for the paper
- `outline.md`: section-by-section manuscript outline and figure ideas
- `implementation-status.md`: what the paper can claim today versus what must remain future work
- `evaluation-plan.md`: experiments, benchmarks, and evidence the paper should gather

## Maintenance Rules

- Update these files whenever consensus, networking, recovery, or observability changes materially.
- Do not describe `libp2p`, on-chain staking, deterministic WASM contracts, or confidential compute as implemented until the code and tests actually exist.
- Keep the implementation claims aligned with `README.md`, `docs/roadmap.md`, `docs/architecture.md`, and the code under `internal/api`, `internal/ledger`, and `internal/consensus`.
- Treat this folder as the manuscript control plane: when the roadmap changes, update the paper framing too.
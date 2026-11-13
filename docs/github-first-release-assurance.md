# GitHub-first release assurance — internal audit and container isolation

This track is coordinated by [issue #208](https://github.com/the-code-learner/Zephyr-Chain/issues/208), under the non-waivable [mainnet NO-GO gate](mainnet-security-release-gates.md).

## Targets

1. **Containerized single-runner evidence (current):** `.github/workflows/v2-docker-devnet-matrix.yml` runs the existing v2 transaction/vote/finality test with **4, 7 and 20** validator fixtures. Each matrix job is an isolated GitHub-hosted VM, but **each fixture runs multiple validators inside one Go test process inside one Docker container on one host**. This is not independently operated host evidence, nor a measured distributed TPS claim.
2. **Multi-process single-host evidence (next):** provision independent v2 node processes in containers, authenticated transport and separate state/signer directories; inject partitions, restart and equivocation, preserve full signed-QC proof chain.
3. **Truly cross-runner network evidence (next):** a secure overlay linking independent GitHub Actions runners, with explicit host identity, signed connectivity, real v2 node binaries, scheduler and reproducible failure injection. Merely selecting `matrix` is not enough to network hosts together.
4. **Long-duration/independent custody:** prove if GitHub runners can fulfill the required uptime, nonrollbackable signer/key custody and multiple independently administered provider requirements. If not, document precisely which acceptance check is blocked, then seek free-tier MCP-accessible resources. Never treat a free-tier account as independent if it shares operator custody/control.

## Run and evidence

Start the `V2 Docker Devnet Matrix` workflow by `workflow_dispatch` or by changing the listed protocol/test files in a PR. Each job runs the exact checkout SHA inside `golang:1.26.6` with `GOTOOLCHAIN=local`, rootless container process, dropped capabilities and read-only source mount.

The uploaded artifact includes environment/runner metadata, image digest and test stdout. An unsuccessful `go test` fails the job through `set -o pipefail`; `if: always()` preserves partial artifacts on error. Missing logs and expired artifacts are not safety evidence. Existing protected PR and main CI gates remain separate and mandatory.

## Security review target

The project's assistant-led review is an **internal engineering assessment, not an independent audit**. Record each finding with severity, concrete exploit path, exact commit, reproducible negative test, remediation and post-merge result. Fuzz/race/static analysis and code review cannot replace an independent observer/operator asserting an external security property. Unproven launch claims remain **NO-GO**.

## Cost and escalation policy

Prefer repository-provided GitHub Actions resources and bounded test workloads. No paid deployment, additional secrets, provider sign-up or upgrade may be performed without explicit user authorization. The first request for an external connection must name the exact unmet proof requirement, the failed GitHub-only attempt and an eligible free-tier MCP option.

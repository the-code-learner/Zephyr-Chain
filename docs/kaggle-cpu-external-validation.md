# Kaggle CPU-only external execution gate

Purpose: measure **actual Kaggle free-tier CPU/RAM allocation** and run an **independent offline Python** reference check of BFT quorum arithmetic and candidate proposer epoch quotas. It is **not** real cross-host Zephyr validator consensus: the Kaggle script does not launch Zephyr Go processes, verify live QUIC QC traffic or provide external key custody. Mainnet #42 remains NO-GO.

## Security and resource boundaries

- Only `KAGGLE_API_TOKEN` is used from GitHub repository secrets. Never commit the token or include it in logs/artifacts. Authentication uses the documented Kaggle token environment mechanism.
- `kernel-metadata.json` is generated on the GitHub runner, never committed. Kaggle run is **private**, `enable_gpu=false`, `enable_tpu=false`, `machine_shape=""` and `enable_internet=false`. The CLI invocation does **not** pass `--accelerator`.
- A CPU-only device check fails closed if an accelerator device appears in the runtime. No GPU/TPU workloads or libraries are invoked. Kaggle free-tier actual CPU affinity and cgroup memory limit are measured rather than assuming 30 GB.
- The Kaggle runtime receives the **Python probe only**, not the GitHub secret, private validator keys, complete repo or private data. Evidence includes resource counts and mathematical tests, never credentials.
- The workflow runs on protected `main` when its probe/workflow changes, or by manual dispatch. It is intentionally **not** a required consensus/mainnet gate until execution and reliability are proven.
- Poll interval is 180 seconds (maximum six status checks), and the remote script run timeout is 900 seconds, keeping free-tier consumption bounded. This is an experimental optional check, not a long-running hosting service.

## Authentication and Kaggle username

GitHub repository secret `KAGGLE_API_TOKEN` already supplied by operator. The workflow tries to read the profile owner from Kaggle's own `kaggle kernels init` output. If unavailable, it fails clearly before submitting any job, requesting nonsecret repository **Actions variable** `KAGGLE_USERNAME` (or a manual-dispatch input). GitHub username is **not assumed** to equal Kaggle username. The kernel owner identifier must match the token's authorized Kaggle account.

## Acceptance

- GitHub workflow metadata must show no GPU/TPU accelerator and internet disabled. A private Kaggle CPU run must successfully produce `zephyr_kaggle_cpu_evidence.json`.
- Downloaded evidence must independently prove `schema=zephyr-kaggle-cpu-probe/v1`, `passed=true`, `accelerator_device_present=false`, measured CPU affinity, effective cgroup RAM (possibly unavailable and explicitly null) and exhaustive power-subset arithmetic for 4/7 equal-stake plus 1:2:4 weighted sets.
- Record exact source SHA and GitHub action URL in issue #211/#229; never treat this proof as a real 7-process consensus test on a different provider.
- Only then consider a second phase with pinned and hashed Zephyr binaries delivered via a controlled mechanism. Any subsequent real external-node work requires network/key-custody safeguards and independent QC verification, not just more simulations.

Official docs: https://github.com/Kaggle/kaggle-cli/blob/main/docs/kernels_metadata.md and https://github.com/Kaggle/kaggle-cli/blob/main/docs/kernels.md .

# Security Policy

Zephyr Chain is pre-production software. Do not use the current node, wallet, dev faucet, or consensus implementation to custody assets of real-world value.

## Current security posture

- The process-facing node handler disables `/v1/dev/*` endpoints by default. Local development that needs the faucet, block-template, or manual block-production endpoints must opt in with `ZEPHYR_ENABLE_DEV_ENDPOINTS=true`.
- Internal snapshot requests require a peer source header and follow the configured peer-identity policy. Validator deployments should configure `ZEPHYR_VALIDATOR_PRIVATE_KEY`, enable `ZEPHYR_REQUIRE_PEER_IDENTITY=true`, and pin peers with `ZEPHYR_PEER_VALIDATORS` where practical.
- The node entrypoint restricts its data directory to owner access and keeps `state.json` owner-readable/writable only on operating systems that implement POSIX-style permission bits.
- The browser wallet encrypts private-key material at rest with a passphrase-derived AES-256-GCM key. The passphrase is processed locally and is not sent to the node.
- A browser wallet remains exposed to browser-origin threats while unlocked. XSS, malicious extensions, compromised dependencies, or a compromised browser profile can still access in-memory key material. Hardware-backed signing and stronger key isolation remain future production work.

## Development endpoints

The following routes are development-only and should stay disabled on public deployments:

- `POST /v1/dev/faucet`
- `GET /v1/dev/block-template`
- `POST /v1/dev/produce-block`

Enable them only for an isolated development environment:

```text
ZEPHYR_ENABLE_DEV_ENDPOINTS=true
```

## Reporting a vulnerability

Please avoid publishing exploitable details in a public issue. Use the repository's private vulnerability-reporting or security-advisory flow when available. If no private reporting channel is enabled, open a minimal issue asking the maintainer for a private contact channel without including exploit details.

When reporting, include the affected commit, reproduction conditions, expected impact, and whether the issue affects consensus safety, key custody, peer authentication, state integrity, or denial-of-service resistance.

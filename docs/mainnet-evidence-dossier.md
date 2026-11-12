# Mainnet evidence dossier — offline integrity preflight

**PUBLIC MAINNET: NO-GO.** This tool prepares immutable files for independent release review; it does **not** validate the claims in those files or grant mainnet authorization. No fake audit reports, operator signatures, simulated HSM counters, or self-attested independence may substitute for third-party evidence.

## Evidence manifest

A JSON object with `schemaVersion: 1` and a `gates` object containing **all** required areas from `scripts/check-mainnet-evidence.py`. Each gate is a nonempty list of distinct file references:

```json
{
  "schemaVersion": 1,
  "gates": {
    "genesis_and_supply": [
      {
        "path": "genesis/independent-review.pdf",
        "sha256": "REPLACE_WITH_64_LOWERCASE_HEX_SHA256_OF_REVIEWED_FILE"
      }
    ]
  }
}
```

The snippet above is illustrative and **intentionally invalid/incomplete**. All required gates are enforced by code, including multi-host BFT, remote validator signing custody, independent witness operators, global monotonicity/freshness, crash recovery, external cryptography and WASM audits, compute settlement, actual QC-verified multishard throughput, mobile/reproducible binaries, VRF/governance/economics and incident drills.

Store the complete dossier's original reviewed bytes in a directory outside the repository. Do not commit raw private key material, secret audit findings, operator credentials, or identifying infrastructure access tokens. Name paths relative to the evidence root and calculate the hash of each actual file.

Run:

```sh
python3 scripts/check-mainnet-evidence.py /secure/dossier/manifest.json --root /secure/dossier
```

The preflight rejects missing gate categories, missing/mutated files, duplicate file references, unsafe/escaping paths, invalid SHA-256 and empty entries. All JSON objects are parsed strictly: repeated member keys at the manifest, gate, or artifact level are rejected rather than silently using the last value. It also rejects multiply hard-linked artifact inodes, including aliases outside the dossier: two filenames do not constitute two independent pieces of evidence. Store each reviewed artifact as a separate, single-linked regular file within the evidence root. This check is a local preflight, not a defense against a privileged operator mutating files concurrently or replacing the entire directory during inspection. It streams file hashes and never silently treats a synthetic fixture, GitHub Actions green check or self-declared operator ID as externally verified safety.

**Even a successful preflight prints `MAINNET: NO-GO`.** A different organization must independently check evidence authenticity, real operator separation and legal/control independence, HSM monotonic reserve/sign across failover, attack drill reproduction, audit findings and remediations, release governance and final sign-offs under [mainnet-security-release-gates.md](mainnet-security-release-gates.md) and GitHub issues #42, #54, #55. Only that explicitly reviewed decision can authorize production launch.

Run synthetic regressions with `python3 -m unittest discover -s scripts -p 'test_check_mainnet_evidence.py' -v`. Synthetic tests prove the preflight rejects malformed dossiers, **not** that production criteria have been met.

#!/usr/bin/env python3
"""Offline integrity preflight for a mainnet review dossier; NEVER grants GO.

The manifest is self-asserted metadata. Hash verification proves that the
reviewer received the named files unchanged, not that their claims are true,
that operators are independent, or that audits/approval have occurred.
"""
import argparse
import hashlib
import json
import pathlib
import sys

REQUIRED_GATES = (
    "genesis_and_supply",
    "consensus_byzantine_multihost",
    "validator_remote_signing_custody",
    "witness_independent_operators",
    "witness_global_monotonicity_and_freshness",
    "adversarial_recovery_and_migrations",
    "protocol_and_crypto_external_audit",
    "wasm_runtime_and_contract_audit",
    "compute_isolation_and_settlement",
    "multishard_finality_and_throughput",
    "citizen_mobile_and_reproducible_builds",
    "vrf_governance_and_economics",
    "release_operations_and_incident_drills",
)


def inspect(manifest_path: pathlib.Path, root: pathlib.Path) -> list[str]:
    errors = []
    try:
        doc = json.loads(manifest_path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError) as exc:
        return [f"manifest unreadable: {exc}"]
    if not isinstance(doc, dict) or set(doc) != {"schemaVersion", "gates"} or type(doc["schemaVersion"]) is not int or doc["schemaVersion"] != 1:
        return ["manifest must contain exactly schemaVersion=1 and gates"]
    gates = doc["gates"]
    if not isinstance(gates, dict):
        return ["gates must be an object"]
    expected = set(REQUIRED_GATES)
    errors.extend(f"missing gate: {gate}" for gate in sorted(expected - set(gates)))
    errors.extend(f"unknown gate: {gate}" for gate in sorted(set(gates) - expected))
    root = root.resolve()
    used = set()
    for gate in REQUIRED_GATES:
        if gate not in gates:
            continue
        artifacts = gates[gate]
        if not isinstance(artifacts, list) or not artifacts:
            errors.append(f"{gate}: at least one immutable evidence artifact is required")
            continue
        for index, item in enumerate(artifacts):
            prefix = f"{gate}[{index}]"
            if not isinstance(item, dict) or set(item) != {"path", "sha256"}:
                errors.append(f"{prefix}: require exactly path and sha256")
                continue
            value, digest = item["path"], item["sha256"]
            if not isinstance(value, str) or not value or not isinstance(digest, str) or len(digest) != 64 or any(c not in "0123456789abcdef" for c in digest):
                errors.append(f"{prefix}: invalid relative path or lowercase SHA-256")
                continue
            relative = pathlib.PurePosixPath(value)
            if relative.is_absolute() or any(part in ("", ".", "..") for part in value.split("/")) or "\\" in value or relative.as_posix() != value:
                errors.append(f"{prefix}: noncanonical or unsafe evidence path")
                continue
            target = root / value
            try:
                resolved = target.resolve(strict=True)
                if not resolved.is_relative_to(root) or not resolved.is_file():
                    raise ValueError("file outside evidence root or not regular")
                if resolved in used:
                    raise ValueError("one artifact reused across evidence requirements")
                used.add(resolved)
                hasher = hashlib.sha256()
                with resolved.open("rb") as stream:
                    for chunk in iter(lambda: stream.read(1024 * 1024), b""):
                        hasher.update(chunk)
                if hasher.hexdigest() != digest:
                    raise ValueError("evidence SHA-256 mismatch")
            except (OSError, ValueError) as exc:
                errors.append(f"{prefix}: {exc}")
    return errors


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=pathlib.Path)
    parser.add_argument("--root", type=pathlib.Path, required=True, help="local directory containing evidence bytes")
    args = parser.parse_args()
    errors = inspect(args.manifest, args.root)
    print("MAINNET: NO-GO — offline evidence integrity is NOT launch authorization")
    if errors:
        for issue in errors:
            print(f"REJECT: {issue}", file=sys.stderr)
        return 1
    print("Dossier file integrity verified. Independent audit, operator identity, actual distributed deployment, security sign-off and release governance MUST still be verified separately.")
    return 0


if __name__ == "__main__":
    sys.exit(main())

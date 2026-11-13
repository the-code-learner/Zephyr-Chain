#!/usr/bin/env python3
"""Offline integrity preflight for a mainnet review dossier; NEVER grants GO.

The manifest is self-asserted metadata. Hash verification proves that the
reviewer received the named files unchanged, not that their claims are true,
that operators are independent, or that audits/approval have occurred.
"""
import argparse
import contextlib
import hashlib
import json
import os
import pathlib
import stat
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


def reject_duplicate_json_members(pairs: list[tuple[str, object]]) -> dict:
    """Reject ambiguous duplicate JSON keys at every object nesting level."""
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError(f"duplicate JSON key: {key}")
        result[key] = value
    return result



def hash_open_evidence(root: pathlib.Path, relative: pathlib.PurePosixPath) -> tuple[str, tuple[int, int]]:
    """Read the same regular inode that was validated, without following links.

    Every path component is opened relative to the already opened directory,
    with O_NOFOLLOW, rather than re-resolving the name between a stat and
    read. The final descriptor is checked before AND after hashing. This
    protects against symlink swaps, not a privileged in-place writer or a
    snapshot/custody rollback by the machine owner.
    """
    if not hasattr(os, "O_NOFOLLOW") or not hasattr(os, "O_DIRECTORY"):
        raise ValueError("secure descriptor-relative evidence reads unavailable")
    with contextlib.ExitStack() as stack:
        root_fd = os.open(root, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        stack.callback(os.close, root_fd)
        directory_fd = root_fd
        for part in relative.parts[:-1]:
            component = os.stat(part, dir_fd=directory_fd, follow_symlinks=False)
            if stat.S_ISLNK(component.st_mode):
                raise ValueError("symlink evidence path component")
            next_fd = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=directory_fd)
            stack.callback(os.close, next_fd)
            directory_fd = next_fd
        filename = relative.parts[-1]
        leaf = os.stat(filename, dir_fd=directory_fd, follow_symlinks=False)
        if stat.S_ISLNK(leaf.st_mode):
            raise ValueError("symlink evidence path component")
        # O_NONBLOCK prevents a malicious FIFO replacement from hanging the
        # verifier before the descriptor can be rejected as non-regular.
        fd = os.open(filename, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=directory_fd)
        stack.callback(os.close, fd)
        before = os.fstat(fd)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError("file outside evidence root or not regular")
        if before.st_nlink != 1:
            raise ValueError("hard-linked evidence is not independently immutable")
        hasher = hashlib.sha256()
        while True:
            chunk = os.read(fd, 1024 * 1024)
            if not chunk:
                break
            hasher.update(chunk)
        after = os.fstat(fd)
        if (before.st_dev, before.st_ino, before.st_size, before.st_mtime_ns, before.st_ctime_ns) != (
            after.st_dev, after.st_ino, after.st_size, after.st_mtime_ns, after.st_ctime_ns
        ) or after.st_nlink != 1:
            raise ValueError("evidence file modified during digest")
        return hasher.hexdigest(), (before.st_dev, before.st_ino)


def inspect(manifest_path: pathlib.Path, root: pathlib.Path) -> list[str]:
    errors = []
    try:
        doc = json.loads(
            manifest_path.read_text(encoding="utf-8"),
            object_pairs_hook=reject_duplicate_json_members,
        )
    except (OSError, UnicodeError, ValueError) as exc:
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
            try:
                computed, inode = hash_open_evidence(root, relative)
                if inode in used:
                    raise ValueError("one artifact inode reused across evidence requirements")
                used.add(inode)
                if computed != digest:
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

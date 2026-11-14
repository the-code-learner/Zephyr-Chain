#!/usr/bin/env python3
"""Run the actual statically linked Zephyr Go consensus test binary on Kaggle CPU.

The binary is built and SHA-256 pinned by GitHub Actions, transferred through
a private Kaggle dataset. This is a test of code execution on an external CPU,
NOT a live independent validator, remote key custody, or a cross-host QC lab.
"""
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import time

DATA_DIR = Path("/kaggle/input/zephyr-chain-offline-consensus-cpu")
OUTPUT_DIR = Path("/kaggle/working")
BINARY_NAME = "zephyr_consensus.test"
TEST_PATTERN = (
    "^TestV2LegacyProposerHundredHeightStreakIsConsensusCompatibility$"
    "|^TestCandidateEpochProportionalQuotasAndPermutation$"
    "|^TestCandidateEpochBoundariesAndRoundModulo$"
    "|^TestCandidateEpochNeverChangesV2Proposer$"
    "|^TestV2QuorumCertificateRequiresTwoThirdsPlus$"
    "|^TestV2CertificateRejectsDuplicateVote$"
    "|^TestV2ProposalRejectsUncommittedValidatorRoot$"
    "|^TestDurableSigningGuardRejectsConflictsAcrossRestarts$"
    "|^TestDurableSigningGuardCrossRoundHeaderLockSurvivesRestarts$"
    "|^TestDurableSigningGuardReplayRejectsValidHashChainWithCrossRoundConflict$"
    "|^TestDurableSigningGuardCorruptionFailsClosed$"
    "|^TestV2IndexedCertificateVerificationRejectsTampering$"
)


def main():
    if platform.system() != "Linux" or platform.machine() not in ("x86_64", "AMD64"):
        raise RuntimeError("Unexpected Kaggle CPU architecture")
    if any(any(Path("/dev").glob(name)) for name in ("nvidia[0-9]*", "nvidiactl", "nvidia-uvm")):
        raise RuntimeError("GPU device detected, refusing CPU-only job")
    # The private Kaggle source can be mounted beneath a generated directory
    # rather than the assumed dataset slug. Discover only inside /kaggle/input,
    # never download network code or run unpinned executable contents.
    candidates = [DATA_DIR] if DATA_DIR.is_dir() else []
    root = Path("/kaggle/input")
    if root.is_dir():
        candidates += sorted({
            p.parent for p in root.rglob("expected_sha256.txt")
            if "zephyr-chain-offline-consensus-cpu" in p.parts
        })
    accepted = []
    for folder in candidates:
        binary_file = folder / BINARY_NAME
        digest_file = folder / "expected_sha256.txt"
        source_file_candidate = folder / "source_sha.txt"
        if binary_file.is_file() and digest_file.is_file() and source_file_candidate.is_file():
            accepted.append((binary_file, digest_file, source_file_candidate))
    # More than one eligible bundle is ambiguous: never silently choose one.
    if len(set(accepted)) != 1:
        mounted = sorted(p.name for p in root.iterdir())[:30] if root.is_dir() else []
        raise RuntimeError(
            "Trusted private dataset mount unresolved: "
            f"eligible_bundles={len(set(accepted))}; mounted_roots={mounted}"
        )
    binary, expected_file, source_file = accepted[0]
    expected = expected_file.read_text(encoding="utf-8").strip()
    actual = hashlib.sha256(binary.read_bytes()).hexdigest()
    if len(expected) != 64 or actual != expected:
        raise RuntimeError("Go test binary SHA-256 mismatch")
    source_sha = source_file.read_text(encoding="utf-8").strip()
    if len(source_sha) != 40 or not all(c in "0123456789abcdef" for c in source_sha):
        raise RuntimeError("Invalid pinned Git source SHA")
    executable = OUTPUT_DIR / BINARY_NAME
    shutil.copyfile(binary, executable)
    executable.chmod(0o700)
    started = time.monotonic()
    # Capture only test results; test filters are deliberately narrow and do
    # not run any validator private-key fixture or network-based lab here.
    result = subprocess.run(
        [str(executable), "-test.run", TEST_PATTERN, "-test.v",
         "-test.count=1", "-test.timeout=150s"],
        cwd=str(OUTPUT_DIR), capture_output=True, text=True, timeout=170, check=False
    )
    elapsed = time.monotonic() - started
    lines = result.stdout.splitlines()
    passed = [line for line in lines if line.startswith("--- PASS:")]
    failed = [line for line in lines if line.startswith("--- FAIL:")]
    out = {
        "schema": "zephyr-kaggle-offline-go-consensus/v1",
        "scope": "real Zephyr Go consensus unit tests on Kaggle CPU; no live remote quorum",
        "source_sha": source_sha,
        "binary_sha256": actual,
        "cpu_architecture": platform.machine(),
        "accelerator_requested": False,
        "exit_code": result.returncode,
        "passed_test_cases": len(passed),
        "failed_test_cases": len(failed),
        "elapsed_seconds": round(elapsed, 3),
        "accepted": result.returncode == 0 and len(passed) >= 12 and not failed,
        "timestamp_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }
    (OUTPUT_DIR / "zephyr_kaggle_go_consensus.json").write_text(
        json.dumps(out, sort_keys=True, indent=2) + "\n", encoding="utf-8")
    # Avoid leaking private paths or binary data in logs.
    print("Zephyr Go consensus test exit:", result.returncode)
    print("Passed test cases:", len(passed), "failed:", len(failed))
    print("Source SHA:", source_sha)
    if not out["accepted"]:
        print("Test error details (bounded):", result.stderr[-1000:])
        raise RuntimeError("Remote Go consensus regression failed")


if __name__ == "__main__":
    main()

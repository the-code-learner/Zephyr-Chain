#!/usr/bin/env python3
"""Offline, CPU-only independent infrastructure and BFT arithmetic probe.

This does not execute Zephyr Go validators or prove cross-host QC finality.
Only nonsecret observations are written to /kaggle/working.
"""
import hashlib
import json
import math
import os
from pathlib import Path
import platform
import sys
import time


def read_first(path):
    try:
        return Path(path).read_text(encoding="utf-8").strip()
    except (FileNotFoundError, PermissionError, OSError):
        return None


def available_memory():
    # Prefer cgroup limits to the host's physical RAM estimate.
    raw = read_first("/sys/fs/cgroup/memory.max")
    if raw and raw != "max" and raw.isdecimal():
        return int(raw)
    # Older cgroups.
    raw = read_first("/sys/fs/cgroup/memory/memory.limit_in_bytes")
    if raw and raw.isdecimal() and int(raw) < (1 << 60):
        return int(raw)
    return None


def cpu_count():
    try:
        affinity = len(os.sched_getaffinity(0))
    except (AttributeError, OSError):
        affinity = None
    return {"affinity": affinity, "os_count": os.cpu_count()}


def quorum(total):
    # Strictly greater than two thirds; no floating point.
    return total * 2 // 3 + 1


def safety_exhaustive():
    """Enumerate 4/7 equal validators and 1:2:4 weighted vote subsets."""
    cases = []
    for powers in ([1] * 4, [1] * 7, [1, 2, 4]):
        threshold = quorum(sum(powers))
        passing = 0
        failing = 0
        for mask in range(1 << len(powers)):
            votes = sum(p for i, p in enumerate(powers) if mask & (1 << i))
            got = votes >= threshold
            assert got == (3 * votes > 2 * sum(powers))
            passing += int(got)
            failing += int(not got)
        cases.append({"validator_count": len(powers),
                      "powers": powers,
                      "quorum_power": threshold,
                      "passing_subsets": passing,
                      "rejected_subsets": failing})
    assert cases[1]["quorum_power"] == 5
    assert quorum(4) == 3
    return cases


def allocation_checks():
    """Independent integer Hamilton quota reference, not a live chain check."""
    samples = ([100] * 4, [100] * 7, [1, 2, 4], [1, 99],
               [(2**64-1)//2, (2**64-1)//2])
    results = []
    for powers in samples:
        total = sum(powers)
        numerator = [256 * p for p in powers]
        quotas = [n // total for n in numerator]
        remaining = 256 - sum(quotas)
        ranked = sorted(range(len(powers)), key=lambda i: (-(numerator[i] % total), i))
        for i in ranked[:remaining]:
            quotas[i] += 1
        assert sum(quotas) == 256
        assert all(abs(quotas[i] * total - numerator[i]) < total for i in range(len(powers)))
        results.append({"weights": powers, "quotas": quotas,
                        "max_abs_quota_error_num": max(abs(quotas[i]*total-numerator[i]) for i in range(len(powers)))})
    assert len(set(results[0]["quotas"])) == 1
    assert max(results[1]["quotas"]) - min(results[1]["quotas"]) <= 1
    return results


def gpu_guard():
    # GPU/TPU runs are forbidden. Detect visible device nodes rather than
    # importing GPU libraries or probing accelerators.
    dev = Path("/dev")
    forbidden = [str(p) for pattern in ("nvidia[0-9]*", "nvidiactl", "nvidia-uvm", "accel[0-9]*")
                 for p in dev.glob(pattern)]
    if forbidden:
        raise RuntimeError("CPU-only invariant failed: accelerator device exposed")
    return True


def adversarial_quorum_checks():
    """Reproducible adversarial weighted quorum intersection, offline CPU only.

    Independent of Go implementation. This is mathematical checking, NOT
    a real network-partition, signature or validator-key test.
    """
    import random
    rng = random.Random(20261114)
    tested_sets = 0
    quorum_pairs = 0
    subset_cases = 0
    for count in (4, 7):
        for case in range(160):
            # Extremely skewed and normal sets; each total stays in uint64.
            if case % 4 == 0:
                powers = [1] * (count - 1) + [2**60]
            elif case % 4 == 1:
                powers = [rng.randint(1, 10_000) for _ in range(count)]
            elif case % 4 == 2:
                powers = [rng.randint(1, (2**64 - 1) // (count * 2)) for _ in range(count)]
            else:
                powers = [1] * count
            total = sum(powers)
            assert total <= 2**64 - 1
            required = quorum(total)
            subsets = []
            for mask in range(1 << count):
                weight = sum(p for i, p in enumerate(powers) if mask & (1 << i))
                assert (weight >= required) == (3 * weight > 2 * total)
                subset_cases += 1
                if weight >= required:
                    subsets.append(mask)
            for a in subsets:
                for b in subsets:
                    overlap = sum(p for i, p in enumerate(powers)
                                  if (a & b) & (1 << i))
                    assert 3 * overlap > total, (powers, a, b, overlap)
                    quorum_pairs += 1
            tested_sets += 1
    return {"seed": 20261114, "validator_sets": tested_sets,
            "subset_cases": subset_cases, "quorum_pair_checks": quorum_pairs}


def candidate_streak_diagnostics():
    """Independent smooth-quota reference, not a consensus implementation."""
    cases = ([100] * 4, [100] * 7, [1, 2, 4], [1, 99])
    evidence = []
    for powers in cases:
        total = sum(powers)
        raw = [256 * p for p in powers]
        quotas = [n // total for n in raw]
        ranked = sorted(range(len(powers)), key=lambda i: (-(raw[i] % total), i))
        for i in ranked[:256 - sum(quotas)]:
            quotas[i] += 1
        deficit = [0] * len(powers)
        selected = []
        for _ in range(256):
            for i, quota in enumerate(quotas):
                if quota:
                    deficit[i] += quota
            leader = max((i for i, q in enumerate(quotas) if q),
                         key=lambda i: (deficit[i], -i))
            deficit[leader] -= 256
            selected.append(leader)
        assert [selected.count(i) for i in range(len(powers))] == quotas
        longest = 0
        streak = 0
        previous = None
        for leader in selected:
            streak = streak + 1 if leader == previous else 1
            previous = leader
            longest = max(longest, streak)
        evidence.append({"powers": powers, "quotas": quotas,
                         "max_consecutive_leader_slots": longest})
    return evidence


def main():
    gpu_guard()
    cases = safety_exhaustive()
    quotas = allocation_checks()
    adversarial = adversarial_quorum_checks()
    streaks = candidate_streak_diagnostics()
    obs = {
        "schema": "zephyr-kaggle-cpu-probe/v1",
        "scope": "offline Python reference arithmetic + resource report; NOT Zephyr-Go or live BFT",
        "run_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "python": platform.python_version(),
        "platform": platform.system(),
        "machine": platform.machine(),
        "cpu": cpu_count(),
        "memory_cgroup_bytes": available_memory(),
        "memory_host_kb": (read_first("/proc/meminfo") or "").splitlines()[0][:100],
        "cuda_visible_devices": "configured" if os.getenv("CUDA_VISIBLE_DEVICES") not in (None, "", "-1") else "unset_or_disabled",
        "accelerator_device_present": False,
        "threshold_subset_tests": cases,
        "quota_reference_tests": quotas,
        "adversarial_quorum_checks": adversarial,
        "candidate_streak_diagnostics": streaks,
        "passed": True,
    }
    # Use the Kaggle output directory only, never home/token files.
    out = Path("/kaggle/working")
    if not out.is_dir():
        out = Path.cwd()
    data = json.dumps(obs, sort_keys=True, indent=2)
    (out / "zephyr_kaggle_cpu_evidence.json").write_text(data + "\n", encoding="utf-8")
    print("ZEPHYR_CPU_PROBE_OK " + hashlib.sha256(data.encode()).hexdigest())
    print("CPU affinity count:", obs["cpu"]["affinity"])
    print("Cgroup RAM bytes:", obs["memory_cgroup_bytes"])
    print("Reference subset checks:", sum(1 << x["validator_count"] for x in cases))
    print("Adversarial quorum pair checks:", adversarial["quorum_pair_checks"])


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Prove observer-only V2 certified-history restart and fail-closed corruption."""
import argparse
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import time


def run_observer(binary, fixture, observer_dir, log, timeout=35):
    cmd = [
        binary,
        "-genesis", fixture["genesisFile"],
        "-expected-network-id", fixture["networkId"],
        "-expected-validator-root", fixture["validatorRoot"],
        "-data-dir", str(observer_dir),
        "-listen", "/ip4/127.0.0.1/udp/0/quic-v1",
    ]
    with log.open("w") as stream:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=stream,
                                text=True, bufsize=1)
        try:
            with selectors.DefaultSelector() as selector:
                selector.register(proc.stdout, selectors.EVENT_READ)
                if not selector.select(timeout):
                    raise AssertionError("restarted observer did not become ready")
            line = proc.stdout.readline()
            if not line:
                raise AssertionError(f"observer startup rejected ({proc.poll()})")
            return json.loads(line)
        finally:
            if proc.poll() is None:
                proc.terminate()
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=10)
            proc.stdout.close()


def reject_corruption(binary, fixture, observer_dir, log):
    cmd = [
        binary, "-genesis", fixture["genesisFile"],
        "-expected-network-id", fixture["networkId"],
        "-expected-validator-root", fixture["validatorRoot"],
        "-data-dir", str(observer_dir),
        "-listen", "/ip4/127.0.0.1/udp/0/quic-v1",
    ]
    with log.open("w") as stream:
        proc = subprocess.Popen(cmd, stdout=subprocess.PIPE,
                                stderr=stream, text=True)
        try:
            stdout, _ = proc.communicate(timeout=20)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.communicate()
            raise AssertionError("observer accepted corrupt certified history and stayed online")
    if proc.returncode == 0 or stdout.strip():
        raise AssertionError(f"corrupted history admitted: exit={proc.returncode}, ready={stdout[:300]!r}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--observer-binary", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--summary", required=True)
    parser.add_argument("--evidence", required=True)
    args = parser.parse_args()
    evidence = Path(args.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    with open(args.manifest) as stream:
        fixture = json.load(stream)
    with open(args.summary) as stream:
        summary = json.load(stream)

    observer_dir = Path(fixture["genesisFile"]).parent / "nonvoting-observer"
    assert observer_dir.is_dir()
    assert summary["validatorRoot"] == fixture["validatorRoot"]
    assert summary["networkId"] == fixture["networkId"]
    assert summary["height"] == 2 and summary["certifiedHeights"] == [1, 2]
    assert summary["independentNonvotingObserver"]["qcVerified"] is True
    assert summary["independentNonvotingObserver"]["headerHash"] == summary["headerHash"]

    recovered = run_observer(args.observer_binary, fixture, observer_dir,
                             evidence / "observer-recovery.stderr.log")
    assert recovered["mode"] == "observer-only", recovered
    assert recovered["networkId"] == fixture["networkId"]
    assert recovered["validatorRoot"] == fixture["validatorRoot"]
    assert recovered["finalizedHeight"] == 2, recovered
    assert recovered["finalizedHeaderHash"] == summary["headerHash"], recovered
    assert recovered["recoveryMode"] == "bounded QC-certified genesis-to-tip history replay"

    history = observer_dir / "certified-history.log"
    assert history.is_file() and history.stat().st_size > 0
    # Intentionally corrupt the private test fixture only, AFTER proving a
    # valid full restart. This must never be a production mutation.
    with history.open("ab") as stream:
        stream.write(b"\xff")
        stream.flush()
        os.fsync(stream.fileno())
    reject_corruption(args.observer_binary, fixture, observer_dir,
                      evidence / "observer-corrupt-history.stderr.log")

    with (evidence / "observer-recovery-proof.json").open("w") as stream:
        json.dump({
            "scope": "same-host observer-only certified history replay and corruption rejection",
            "networkId": fixture["networkId"],
            "validatorRoot": fixture["validatorRoot"],
            "recoveredHeight": recovered["finalizedHeight"],
            "recoveredHeaderHash": recovered["finalizedHeaderHash"],
            "corruptedHistoryRefused": True,
            "independentProvider": False,
        }, stream, sort_keys=True, indent=2)
    print("PASS: observer independently replays heights 1 and 2 QC history after restart")
    print("PASS: corrupted certified history refuses observer startup")


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"OBSERVER REPLAY TEST FAILED: {exc}", file=sys.stderr)
        raise SystemExit(1)

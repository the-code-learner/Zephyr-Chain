#!/usr/bin/env python3
"""Four *real OS processes* of lab-only V2 validators (no QC claims)."""
import argparse
import json
import os
from pathlib import Path
import selectors
import subprocess
import sys
import time


def launch(binary, fixture, node, evidence):
    args = [
        binary, "-acknowledge-lab-only",
        "-genesis", fixture["genesisFile"],
        "-expected-network-id", fixture["networkId"],
        "-expected-validator-root", fixture["validatorRoot"],
        "-data-dir", node["dataDir"],
        "-signing-journal-dir", node["journalDir"],
        "-private-key-der", node["keyFile"],
        "-listen", node["listen"],
    ]
    log = (evidence / f'node-{node["index"]:02d}.stderr.log').open("w")
    proc = subprocess.Popen(args, stdout=subprocess.PIPE, stderr=log,
                            text=True, bufsize=1)
    return proc, log


def first_line(proc, seconds=35):
    with selectors.DefaultSelector() as selector:
        selector.register(proc.stdout, selectors.EVENT_READ)
        events = selector.select(seconds)
    if not events:
        raise AssertionError(f"validator {proc.pid} startup timed out")
    line = proc.stdout.readline()
    if not line:
        raise AssertionError(f"validator {proc.pid} exited without ready metadata: {proc.poll()}")
    return json.loads(line)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--evidence", required=True)
    opts = parser.parse_args()
    evidence = Path(opts.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    with open(opts.manifest) as source:
        fixture = json.load(source)
    assert fixture["mode"] == "ephemeral-ci-keys-not-mainnet"
    assert len(fixture["validators"]) == 4
    processes = []
    logs = []
    try:
        infos = []
        for node in fixture["validators"]:
            proc, handle = launch(opts.binary, fixture, node, evidence)
            processes.append(proc)
            logs.append(handle)
            info = first_line(proc)
            assert info["mode"] == "lab-validator-not-mainnet", info
            assert info["networkId"] == fixture["networkId"], info
            assert info["validatorRoot"] == fixture["validatorRoot"], info
            assert info["finalizedHeight"] == 0, info
            assert len(info["addresses"]) > 0, info
            infos.append({"index": node["index"], "pid": proc.pid,
                          "peerId": info["peerId"], "addresses": info["addresses"]})
        assert len({x["pid"] for x in infos}) == 4
        assert len({x["peerId"] for x in infos}) == 4

        # A second process with the same key and signing journal but a
        # different state directory must fail before creating any state.
        first = fixture["validators"][0]
        duplicate = dict(first)
        duplicate["index"] = 99
        duplicate["dataDir"] = str(Path(first["dataDir"]).with_name("node-duplicate"))
        duplicate_proc, duplicate_log = launch(opts.binary, fixture, duplicate, evidence)
        logs.append(duplicate_log)
        try:
            result = duplicate_proc.wait(timeout=15)
            assert result != 0, "double-holder incorrectly started"
            assert not os.path.exists(duplicate["dataDir"]), "rejected signer created state"
        finally:
            if duplicate_proc.poll() is None:
                duplicate_proc.kill()
                duplicate_proc.wait(timeout=5)

        # Prove killing one process does not terminate the other three.
        processes[0].terminate()
        assert processes[0].wait(timeout=15) == 0
        time.sleep(1)
        assert all(p.poll() is None for p in processes[1:]), "peer process exited after isolated kill"
        with (evidence / "v2-process-evidence.json").open("w") as target:
            json.dump({
                "scope": "four independent V2 OS processes on one GitHub runner, startup and signer isolation only",
                "networkId": fixture["networkId"],
                "validatorRoot": fixture["validatorRoot"],
                "nodes": infos,
                "sameHost": True,
                "verifiedSignedQC": False,
                "productionTPS": None,
                "secondSignerRejected": True,
                "survivorsAfterOneProcessTermination": 3,
            }, target, sort_keys=True, indent=2)
        print("PASS: four V2 OS processes, four distinct P2P identities, exclusive signer lock")
    finally:
        for proc in processes:
            if proc.poll() is None:
                proc.terminate()
        for proc in processes:
            try:
                proc.wait(timeout=10)
            except subprocess.TimeoutExpired:
                proc.kill()
                proc.wait(timeout=5)
            if proc.stdout:
                proc.stdout.close()
        for handle in logs:
            handle.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"V2 LAB PROCESS SMOKE FAILED: {exc}", file=sys.stderr)
        sys.exit(1)

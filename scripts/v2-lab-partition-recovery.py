#!/usr/bin/env python3
"""Lab-only 7-process V2: fail closed without 5/7, then rejoin and finalize."""
import argparse
import http.client
import json
from pathlib import Path
import selectors
import socket
import subprocess
import sys
import time


def control(path, method, route, payload=None):
    conn = http.client.HTTPConnection("localhost", timeout=25)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.settimeout(25)
    conn.sock.connect(path)
    try:
        body = json.dumps(payload) if payload is not None else None
        conn.request(method, route, body=body,
                     headers={"Content-Type": "application/json"} if body else {})
        response = conn.getresponse()
        data = response.read()
        try:
            value = json.loads(data)
        except ValueError:
            value = {"error": data.decode("utf-8", errors="replace")[:200]}
        return response.status, value
    finally:
        conn.close()


def ready(process, timeout=40):
    with selectors.DefaultSelector() as selector:
        selector.register(process.stdout, selectors.EVENT_READ)
        if not selector.select(timeout):
            raise AssertionError(f"no startup record for PID {process.pid}")
    line = process.stdout.readline()
    if not line:
        raise AssertionError(f"PID {process.pid} failed before ready: {process.poll()}")
    return json.loads(line)


def loopback_address(info):
    for address in info["addresses"]:
        if address.startswith("/ip4/127.0.0.1/udp/"):
            return address
    raise AssertionError(f"missing loopback QUIC address: {info}")


def await_certified(sockets, observer_socket, height, target_hash, timeout=25):
    deadline = time.monotonic() + timeout
    states = []
    observer = {}
    while time.monotonic() < deadline:
        states = [control(path, "GET", "/status") for path in sockets]
        obs_code, observer = control(observer_socket, "GET", "/status")
        if all(code == 200 and value.get("height") == height and
               value.get("qcVerified") is True and
               value.get("headerHash") == target_hash and
               value.get("voteCount", 0) >= 5 for code, value in states) and (
                   obs_code == 200 and observer.get("height") == height and
                   observer.get("headerHash") == target_hash and
                   observer.get("qcVerified") is True and
                   observer.get("isValidator") is False):
            return
        time.sleep(.25)
    raise AssertionError(f"QC height {height} replicas did not converge: {states}, {observer}")


def start(binary, fixture, node, evidence, suffix):
    command = [
        binary, "-acknowledge-lab-only",
        "-genesis", fixture["genesisFile"],
        "-expected-network-id", fixture["networkId"],
        "-expected-validator-root", fixture["validatorRoot"],
        "-data-dir", node["dataDir"],
        "-signing-journal-dir", node["journalDir"],
        "-private-key-der", node["keyFile"],
        "-listen", node["listen"],
        "-lab-control-socket", str(Path(node["dataDir"]) / "lab-control.sock"),
    ]
    log = (evidence / f'partition-node-{node["index"]:02d}-{suffix}.stderr.log').open("w")
    process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=log,
                               text=True, bufsize=1)
    return process, log


def chosen_proposer(sockets, height):
    choices = []
    for i, path in enumerate(sockets):
        code, value = control(path, "GET", "/scheduled-proposer")
        assert code == 200 and value.get("height") == height, (i, code, value)
        if value.get("scheduled") is True:
            choices.append(i)
    assert len(choices) == 1, f"scheduled proposer not unique at {height}: {choices}"
    return choices[0]


def connect_all(sockets, infos, observer_address):
    addresses = [loopback_address(info) for info in infos]
    for i, path in enumerate(sockets):
        peers = [addr for j, addr in enumerate(addresses) if j != i]
        peers.append(observer_address)
        code, result = control(path, "POST", "/connect", {"peers": peers})
        assert code == 200 and result.get("connectedPeers") == 7, (i, code, result)


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--observer-binary", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--evidence", required=True)
    args = parser.parse_args()
    evidence = Path(args.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    with open(args.manifest) as stream:
        fixture = json.load(stream)
    nodes = fixture["validators"]
    assert fixture["mode"] == "ephemeral-ci-keys-not-mainnet" and len(nodes) == 7
    sockets = [str(Path(n["dataDir"]) / "lab-control.sock") for n in nodes]
    processes = []
    handles = []
    infos = []
    observer = None
    observer_log = None
    try:
        for node in nodes:
            process, handle = start(args.binary, fixture, node, evidence, "initial")
            processes.append(process)
            handles.append(handle)
        infos = [ready(process) for process in processes]
        assert all(x["mode"] == "lab-validator-not-mainnet" and
                   x["finalizedHeight"] == 0 for x in infos)
        assert len({x["peerId"] for x in infos}) == 7

        obs_dir = str(Path(fixture["genesisFile"]).parent / "partition-observer")
        obs_socket = str(Path(obs_dir) / "lab-observer-status.sock")
        observer_log = (evidence / "partition-observer.stderr.log").open("w")
        observer = subprocess.Popen([
            args.observer_binary, "-genesis", fixture["genesisFile"],
            "-expected-network-id", fixture["networkId"],
            "-expected-validator-root", fixture["validatorRoot"],
            "-data-dir", obs_dir, "-listen", "/ip4/127.0.0.1/udp/0/quic-v1",
            "-lab-control-socket", obs_socket,
        ], stdout=subprocess.PIPE, stderr=observer_log, text=True, bufsize=1)
        obs_info = ready(observer)
        assert obs_info["mode"] == "observer-only"
        assert obs_info["peerId"] not in {x["peerId"] for x in infos}
        obs_address = loopback_address(obs_info)
        connect_all(sockets, infos, obs_address)

        first = chosen_proposer(sockets, 1)
        code, commit = control(sockets[first], "POST", "/propose")
        assert code == 200 and commit.get("height") == 1 and commit.get("qcVerified"), (code, commit)
        assert commit["voteCount"] >= 5
        await_certified(sockets, obs_socket, 1, commit["headerHash"])
        first_hash = commit["headerHash"]

        scheduled = chosen_proposer(sockets, 2)
        offline = [i for i in range(7) if i != scheduled][:3]
        survivors = [i for i in range(7) if i not in offline]
        assert len(survivors) == 4 and scheduled in survivors
        killed_pids = []
        for i in offline:
            process = processes[i]
            killed_pids.append(process.pid)
            process.kill()
            process.wait(timeout=10)
            process.stdout.close()

        # Four of seven is less than the cryptographic threshold FIVE.
        # Preserve an alive scheduled proposer; a missing proposer alone
        # would not prove quorum enforcement.
        code, result = control(sockets[scheduled], "POST", "/propose")
        assert code == 503 and "insufficient signed QC quorum" in result.get("error", ""), (
            "missing 5/7 must not finalize", code, result
        )
        for i in survivors:
            code, state = control(sockets[i], "GET", "/status")
            assert code == 200 and state.get("height") == 1 and state.get("headerHash") == first_hash, (
                i, code, state
            )
        code, obs_state = control(obs_socket, "GET", "/status")
        assert code == 200 and obs_state.get("height") == 1 and obs_state.get("headerHash") == first_hash

        # These *ephemeral lab-only* nodes were SIGKILLed; their owner-only
        # Unix control socket has no listener but remains on disk. Only
        # unlink after the original process is confirmed dead and only in
        # its private test fixture tree. Never alter signing journals.
        restarted = []
        for i in offline:
            stale = Path(sockets[i])
            assert stale.is_socket(), f"unexpected missing stale lab socket: {stale}"
            stale.unlink()
            process, handle = start(args.binary, fixture, nodes[i], evidence, "restarted")
            handles.append(handle)
            processes[i] = process
            info = ready(process)
            assert info["finalizedHeight"] == 1 and info["networkId"] == fixture["networkId"]
            assert info["validatorRoot"] == fixture["validatorRoot"]
            restarted.append({"node": i, "oldPID": killed_pids[len(restarted)],
                              "newPID": process.pid, "newPeerID": info["peerId"]})
            infos[i] = info
        assert len({x["peerId"] for x in infos}) == 7
        connect_all(sockets, infos, obs_address)

        # Retry the original height-two proposal under the same signing
        # target. Journals MUST allow identical intent but not equivocation.
        assert chosen_proposer(sockets, 2) == scheduled
        code, commit = control(sockets[scheduled], "POST", "/propose")
        assert code == 200 and commit.get("height") == 2 and commit.get("qcVerified"), (code, commit)
        assert commit["voteCount"] >= 5
        await_certified(sockets, obs_socket, 2, commit["headerHash"])
        assert commit["headerHash"] != first_hash

        with (evidence / "partition-no-quorum-recovery.json").open("w") as stream:
            json.dump({
                "scope": "ONE GitHub runner, real seven libp2p QUIC validator OS processes and nonvoting observer",
                "networkId": fixture["networkId"],
                "validatorRoot": fixture["validatorRoot"],
                "quorum": 5,
                "offlineValidators": offline,
                "remainingValidators": survivors,
                "scheduledProposerStayedOnline": True,
                "heightOneHeaderHash": first_hash,
                "noQuorumAttemptRejected": True,
                "noHeightTwoCertifiedDuringMinorityOffline": True,
                "restartedKeyCustody": restarted,
                "heightTwoHeaderHash": commit["headerHash"],
                "allSevenAndObserverCertifiedHeightTwoAfterRejoin": True,
                "independentHosts": False,
            }, stream, indent=2, sort_keys=True)
        print("PASS: 4/7 online including proposer refused signed QC at height two")
        print("PASS: SIGKILL 3 validators, restart certified state/journals, rejoin 7/7 and QC-finalize height two")
    finally:
        if observer is not None and observer.poll() is None:
            observer.terminate()
        if observer is not None:
            try:
                observer.wait(timeout=10)
            except subprocess.TimeoutExpired:
                observer.kill()
                observer.wait(timeout=5)
            if observer.stdout is not None:
                observer.stdout.close()
        if observer_log is not None:
            observer_log.close()
        for process in processes:
            if process.poll() is None:
                process.terminate()
        for process in processes:
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            if process.stdout is not None and not process.stdout.closed:
                process.stdout.close()
        for handle in handles:
            handle.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"V2 PARTITION LAB FAILED: {exc}", file=sys.stderr)
        sys.exit(1)

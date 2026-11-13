#!/usr/bin/env python3
"""Real, same-host V2 QUIC 4/7-process finalized QC lab; not production."""
import argparse
import http.client
import json
from pathlib import Path
import selectors
import socket
import subprocess
import sys
import time


def control(socket_path, method, endpoint, payload=None):
    conn = http.client.HTTPConnection("localhost", timeout=25)
    conn.sock = socket.socket(socket.AF_UNIX)
    conn.sock.settimeout(25)
    conn.sock.connect(socket_path)
    try:
        body = json.dumps(payload) if payload is not None else None
        conn.request(method, endpoint, body=body,
                     headers={"Content-Type": "application/json"} if body else {})
        response = conn.getresponse()
        raw = response.read()
        status = response.status
        try:
            obj = json.loads(raw)
        except ValueError:
            obj = {"error": raw.decode("utf-8", errors="replace")[:400]}
        return status, obj
    finally:
        conn.close()


def ready(proc, timeout=45):
    with selectors.DefaultSelector() as selector:
        selector.register(proc.stdout, selectors.EVENT_READ)
        if not selector.select(timeout):
            raise AssertionError(f"V2 process {proc.pid} had no ready event")
    line = proc.stdout.readline()
    if not line:
        raise AssertionError(f"V2 process {proc.pid} terminated at startup: {proc.poll()}")
    return json.loads(line)


def endpoint(info):
    for address in info["addresses"]:
        if address.startswith("/ip4/127.0.0.1/udp/"):
            return address
    raise AssertionError(f"missing loopback-pinned QUIC multiaddr: {info}")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--binary", required=True)
    parser.add_argument("--manifest", required=True)
    parser.add_argument("--evidence", required=True)
    args = parser.parse_args()
    evidence = Path(args.evidence)
    evidence.mkdir(parents=True, exist_ok=True)
    with open(args.manifest) as f:
        fixture = json.load(f)
    nodes = fixture["validators"]
    assert fixture["mode"] == "ephemeral-ci-keys-not-mainnet" and len(nodes) in (4, 7)
    count = len(nodes)
    quorum = (2 * count) // 3 + 1
    processes, handles, infos = [], [], []
    try:
        # Spawn all validator processes BEFORE waiting for any ready event.
        for node in nodes:
            socket_path = str(Path(node["dataDir"]) / "lab-control.sock")
            command = [
                args.binary, "-acknowledge-lab-only",
                "-genesis", fixture["genesisFile"],
                "-expected-network-id", fixture["networkId"],
                "-expected-validator-root", fixture["validatorRoot"],
                "-data-dir", node["dataDir"],
                "-signing-journal-dir", node["journalDir"],
                "-private-key-der", node["keyFile"],
                "-listen", node["listen"],
                "-lab-control-socket", socket_path,
            ]
            log = (evidence / f'qc-node-{node["index"]:02d}.stderr.log').open("w")
            handles.append(log)
            processes.append(subprocess.Popen(command, stdout=subprocess.PIPE,
                                             stderr=log, text=True, bufsize=1))
        for process in processes:
            info = ready(process)
            assert info["mode"] == "lab-validator-not-mainnet"
            assert info["networkId"] == fixture["networkId"]
            assert info["validatorRoot"] == fixture["validatorRoot"]
            assert info["finalizedHeight"] == 0
            infos.append(info)
        assert len({process.pid for process in processes}) == count
        assert len({info["peerId"] for info in infos}) == count

        sockets = [str(Path(node["dataDir"]) / "lab-control.sock") for node in nodes]
        addresses = [endpoint(info) for info in infos]
        for i, socket_path in enumerate(sockets):
            peers = [address for j, address in enumerate(addresses) if i != j]
            status, result = control(socket_path, "POST", "/connect", {"peers": peers})
            assert status == 200 and result["connectedPeers"] == count - 1, (i, status, result)

        # Negative case: no anonymous peers without /p2p identity proof.
        status, _ = control(sockets[0], "POST", "/connect",
                            {"peers": ["/ip4/127.0.0.1/udp/19001/quic-v1"]})
        assert status == 400, "unbound peer address admitted"

        certified = None
        for i, socket_path in enumerate(sockets):
            status, reply = control(socket_path, "POST", "/propose")
            if status == 409:
                continue
            assert status == 200 and reply.get("qcVerified"), (i, status, reply)
            certified = reply
            break
        assert certified is not None, "scheduled proposer never finalized"
        assert certified["height"] == 1
        assert certified["voteCount"] >= quorum, certified
        assert certified["snapshot"]["Certificate"]["Height"] == 1
        header_hash = certified["headerHash"]

        # Every follower must have independently verified and committed the QC.
        deadline = time.monotonic() + 25
        final_states = []
        while time.monotonic() < deadline:
            final_states = [control(sock, "GET", "/status") for sock in sockets]
            if all(code == 200 and obj.get("height") == 1 and
                   obj.get("qcVerified") and obj.get("headerHash") == header_hash
                   for code, obj in final_states):
                break
            time.sleep(.2)
        else:
            raise AssertionError(f"replicas failed to converge: {final_states!r}")
        assert all(obj["voteCount"] >= quorum for code, obj in final_states)

        # Publish the COMPLETE signed certificate for a separate Go verifier.
        with (evidence / "v2-finalized-snapshot.json").open("w") as f:
            json.dump(certified["snapshot"], f, sort_keys=True)
        with (evidence / "v2-four-process-qc-summary.json").open("w") as f:
            json.dump({
                "scope": f"{count} libp2p QUIC validator OS processes on ONE GitHub runner; real signed QC; no independent provider",
                "validatorCount": count,
                "minimumQuorumVotes": quorum,
                "commit": "use workflow artifact exact SHA",
                "networkId": fixture["networkId"],
                "validatorRoot": fixture["validatorRoot"],
                "height": 1,
                "headerHash": header_hash,
                "voteCount": certified["voteCount"],
                "independentProcessIDs": [proc.pid for proc in processes],
                "independentPeerIDs": [info["peerId"] for info in infos],
                "allReplicasCommittedSameHeader": True,
                "signedQCExternalVerifierRequired": True,
            }, f, indent=2, sort_keys=True)
        print(f"PASS: {count} V2 validator OS processes finalized SAME signed-QC height-1 header")
    finally:
        for process in processes:
            if process.poll() is None:
                process.terminate()
        for process in processes:
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
                process.wait(timeout=5)
            if process.stdout:
                process.stdout.close()
        for log in handles:
            log.close()


if __name__ == "__main__":
    try:
        main()
    except Exception as exc:
        print(f"FOUR-PROCESS V2 QC FAILED: {exc}", file=sys.stderr)
        sys.exit(1)

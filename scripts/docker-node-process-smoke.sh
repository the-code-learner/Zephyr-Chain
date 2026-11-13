#!/usr/bin/env bash
# GitHub-only, *legacy cmd/node* multi-process Docker restart/isolation smoke.
# This is NOT a Protocol V2 multi-host validator/finality or TPS test.
set -euo pipefail

mkdir -p evidence
namespace="zephyr-smoke-${GITHUB_RUN_ID:-local}-${GITHUB_RUN_ATTEMPT:-1}"
network="${namespace}-net"
binary="${PWD}/evidence/zephyr-node"
containers=()
volumes=()
cleanup() {
  local result=$?
  for name in "${containers[@]}"; do
    docker logs --timestamps "$name" > "evidence/${name}.log" 2>&1 || true
    docker inspect "$name" > "evidence/${name}.inspect.json" 2>/dev/null || true
    docker rm -f "$name" >/dev/null 2>&1 || true
  done
  docker network rm "$network" >/dev/null 2>&1 || true
  for name in "${volumes[@]}"; do
    docker volume rm "$name" >/dev/null 2>&1 || true
  done
  echo "test_exit=${result}" >> evidence/environment.txt
}
trap cleanup EXIT

if [[ ! -f "$binary" ]]; then
  echo "build the zephyr-node binary before running this harness" >&2
  exit 2
fi
{
  echo "commit=${GITHUB_SHA:-unknown}"
  echo "run_id=${GITHUB_RUN_ID:-local}"
  echo "runner_os=${RUNNER_OS:-unknown}"
  echo "topology=three-independent-linux-processes-in-separate-containers-on-one-docker-host"
  echo "protocol=legacy-cmd-node-not-protocol-v2"
  echo "proof_scope=startup-status-process-isolation-restart-persistent-volume"
  echo "started_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)"
  echo "binary_sha256=$(sha256sum "$binary" | cut -d ' ' -f1)"
} > evidence/environment.txt

docker network create "$network" >/dev/null
for i in 1 2 3; do
  name="${namespace}-node${i}"
  volume="${namespace}-state${i}"
  port="$((18080 + i))"
  docker volume create "$volume" >/dev/null
  volumes+=("$volume")
  docker run -d \
    --name "$name" \
    --network "$network" \
    --read-only \
    --tmpfs /tmp:rw,nosuid,noexec,size=16m \
    --cap-drop ALL \
    --security-opt no-new-privileges \
    --mount "type=bind,src=${binary},dst=/zephyr-node,readonly" \
    --mount "type=volume,src=${volume},dst=/data" \
    -p "127.0.0.1:${port}:8080" \
    -e "ZEPHYR_HTTP_ADDR=0.0.0.0:8080" \
    -e "ZEPHYR_DATA_DIR=/data" \
    -e "ZEPHYR_NODE_ID=ci-node-${i}" \
    -e "ZEPHYR_ENABLE_BLOCK_PRODUCTION=false" \
    -e "ZEPHYR_ENABLE_CONSENSUS_AUTOMATION=false" \
    -e "ZEPHYR_ENABLE_PEER_SYNC=false" \
    alpine:3.21 /zephyr-node >/dev/null
  containers+=("$name")
done

python3 - <<'PY' | tee evidence/status-check.json
import json
import time
import urllib.request

def read_node(i):
    url = f"http://127.0.0.1:{18080+i}/v1/status"
    last = None
    for _ in range(80):
        try:
            with urllib.request.urlopen(url, timeout=2) as response:
                if response.status != 200:
                    raise AssertionError(f"node {i} HTTP {response.status}")
                doc = json.load(response)
            if doc.get("nodeId") != f"ci-node-{i}":
                raise AssertionError(f"node {i} wrong id: {doc.get('nodeId')!r}")
            if not isinstance(doc.get("status"), dict):
                raise AssertionError(f"node {i} missing status")
            return {"node": i, "nodeId": doc["nodeId"], "chainId": doc.get("chainId")}
        except (OSError, ValueError) as exc:
            last = exc
            time.sleep(.25)
    raise AssertionError(f"node {i} never became reachable: {last}")

before = [read_node(i) for i in (1, 2, 3)]
assert len({n["nodeId"] for n in before}) == 3, before
assert len({n["chainId"] for n in before}) == 1, before
print(json.dumps({"phase": "initial", "nodes": before}, sort_keys=True))
PY

for name in "${containers[@]}"; do
  docker exec "$name" test -f /data/state.json
  docker inspect --format '{{.State.Pid}}' "$name" >> evidence/container-host-pids.txt
done
sort -n evidence/container-host-pids.txt | uniq -c > evidence/container-host-pid-counts.txt
if [[ "$(sort -u evidence/container-host-pids.txt | wc -l)" != 3 ]]; then
  echo "nodes did not have distinct container host PIDs" >&2
  exit 1
fi

restart_name="${namespace}-node2"
before_hash="$(docker exec "$restart_name" sha256sum /data/state.json | cut -d ' ' -f1)"
before_pid="$(docker inspect --format '{{.State.Pid}}' "$restart_name")"
docker restart "$restart_name" > evidence/restart.txt
after_pid="$(docker inspect --format '{{.State.Pid}}' "$restart_name")"
after_hash="$(docker exec "$restart_name" sha256sum /data/state.json | cut -d ' ' -f1)"
if [[ "$before_pid" == "$after_pid" ]]; then
  echo "restart did not change container process PID" >&2
  exit 1
fi
if [[ "$before_hash" != "$after_hash" ]]; then
  echo "state file changed across idle-node restart" >&2
  exit 1
fi

python3 - <<'PY' | tee evidence/post-restart-status.json
import json
import time
import urllib.request
for i in (1, 2, 3):
    last = None
    for _ in range(80):
        try:
            with urllib.request.urlopen(f"http://127.0.0.1:{18080+i}/v1/status", timeout=2) as response:
                doc = json.load(response)
            assert doc["nodeId"] == f"ci-node-{i}", doc
            print(json.dumps({"node": i, "nodeId": doc["nodeId"], "ready_after_restart": True}))
            break
        except (OSError, ValueError, AssertionError) as exc:
            last = exc
            time.sleep(.25)
    else:
        raise AssertionError(f"node {i} unhealthy after restart: {last}")
PY
echo "node2_state_sha256_before=${before_hash}" >> evidence/environment.txt
echo "node2_state_sha256_after=${after_hash}" >> evidence/environment.txt
echo "completed_utc=$(date -u +%Y-%m-%dT%H:%M:%SZ)" >> evidence/environment.txt

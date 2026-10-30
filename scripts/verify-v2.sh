#!/usr/bin/env bash
# Reproduce the local correctness gate before proposing a Protocol v2 merge.
# Usage: bash scripts/verify-v2.sh [quick|full]
set -euo pipefail

mode="${1:-full}"
if [[ "$mode" != "quick" && "$mode" != "full" || "$#" -gt 1 ]]; then
  printf 'Usage: bash scripts/verify-v2.sh [quick|full]\n' >&2
  exit 2
fi

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$root"

for tool in go node npm; do
  if ! command -v "$tool" >/dev/null 2>&1; then
    printf 'Required executable not found: %s\n' "$tool" >&2
    exit 2
  fi
done

printf '[1/6] Static Go checks\n'
go vet ./...
printf '[2/6] Go test suite\n'
go test ./...

printf '[3/6] Wallet lock/audit/typecheck/build\n'
(
  cd apps/wallet
  npm ci
  node ../../scripts/check-wallet-audit.mjs
  npm run build
)

if [[ "$mode" == "quick" ]]; then
  printf 'Quick correctness gate passed. This is not a public-network readiness certificate.\n'
  exit 0
fi

printf '[4/6] Race-detect security-sensitive v2 packages'\ngo test -race ./internal/v2/genesis ./internal/v2/network/p2p ./internal/v2/provider ./internal/v2/committee ./internal/v2/worldstate ./internal/v2/consensus ./cmd/compute-provider ./cmd/zephyr-genesis-audit -count=1 -timeout=8m\n\ngo test -race ./internal/v2/node -run '^TestProtectedValidatorServicePersistsSigningIntent\n'
go test ./internal/v2/lab \
  -run '^TestV2Lab(FourThreePartitionStallsThenHeals|FiveTwoPartitionFinalizesQuorumSideAndMinorityCatchesUp)$' \
  -count=10 -timeout=120s

printf '[6/6] Real QUIC/libp2p ephemeral devnet on one local host\n'
ZEPHYR_DEVNET_VALIDATORS=7 go test ./internal/v2/node \
  -run '^TestProtocolV2Devnet' -count=1 -timeout=300s

ZEPHYR_DEVNET_VALIDATORS=20 go test ./internal/v2/node \
  -run '^TestProtocolV2DevnetTransactionVotingAndFinality$' -count=1 -timeout=300s

printf 'Full local correctness gate passed. No multi-host/production capacity guarantee.\n'
 -count=1 -timeout=3m

printf '[5/6] Repeated v2 consensus partition and recovery tests\n'
go test ./internal/v2/lab \
  -run '^TestV2Lab(FourThreePartitionStallsThenHeals|FiveTwoPartitionFinalizesQuorumSideAndMinorityCatchesUp)$' \
  -count=10 -timeout=120s

printf '[6/6] Real QUIC/libp2p ephemeral devnet on one local host\n'
ZEPHYR_DEVNET_VALIDATORS=7 go test ./internal/v2/node \
  -run '^TestProtocolV2Devnet' -count=1 -timeout=300s

ZEPHYR_DEVNET_VALIDATORS=20 go test ./internal/v2/node \
  -run '^TestProtocolV2DevnetTransactionVotingAndFinality$' -count=1 -timeout=300s

printf 'Full local correctness gate passed. No multi-host/production capacity guarantee.\n'

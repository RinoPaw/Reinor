#!/usr/bin/env bash
set -euo pipefail
repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
root="$(mktemp -d)"
trap 'rm -rf "$root"' EXIT
mkdir -p "$root/bin" "$root/updates"
printf 'old gateway' > "$root/bin/rinomesh-gateway"
printf 'new gateway' > "$root/updates/rinomesh-gateway"
original="$(sha256sum "$root/bin/rinomesh-gateway")"
expected="$(sha256sum "$root/updates/rinomesh-gateway" | awk '{print $1}')"
output="$(REINOR_UPDATER_TEST_ROOT="$root" bash "$repo/deploy/update-gateway.sh" --sha256 "$expected" --dry-run)"
[[ "$output" == *VALIDATION_OK* ]] || { echo "Valid dry-run was rejected"; exit 1; }
if REINOR_UPDATER_TEST_ROOT="$root" bash "$repo/deploy/update-gateway.sh" --sha256 "$(printf '0%.0s' {1..64})" --dry-run 2>/dev/null; then
  echo "Invalid SHA256 was accepted" >&2
  exit 1
fi
[[ "$(sha256sum "$root/bin/rinomesh-gateway")" == "$original" ]] || { echo "Dry-run changed target"; exit 1; }
[[ ! -e "$root/updates/update.lock" ]] || { echo "Dry-run created lock"; exit 1; }
echo "UPDATE_GATEWAY_STAGE_TESTS_OK"

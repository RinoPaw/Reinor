#!/usr/bin/env bash
# Explicitly staged SHA256 update. Never downloads code or rotates credentials.
set -Eeuo pipefail
usage() { echo "usage: sudo $0 --sha256 HEX64 [--dry-run]" >&2; exit 2; }
sha=''; dry=0
while (($#)); do
  case "$1" in
    --sha256) sha="${2:-}"; shift 2 ;;
    --dry-run) dry=1; shift ;;
    *) usage ;;
  esac
done
[[ "$sha" =~ ^[A-Fa-f0-9]{64}$ ]] || usage
root=/opt/rinomesh-control
# A disposable test root is permitted only for --dry-run validation.
if ((dry)) && [[ -n "${REINOR_UPDATER_TEST_ROOT:-}" ]]; then root="$REINOR_UPDATER_TEST_ROOT"; fi
target="$root/bin/rinomesh-gateway"
stage="$root/updates/rinomesh-gateway"
tmp="$root/bin/.rinomesh-gateway.pending"
backup="$root/bin/rinomesh-gateway.backup-$(date -u +%Y%m%dT%H%M%SZ)-$$"
failed="$root/updates/failed-gateway-$$"
lock="$root/updates/update.lock"
service=rinomesh-gateway.service
[[ -f "$target" && -f "$stage" && -d "$root/updates" ]] || { echo 'Missing stage/target' >&2; exit 1; }
[[ ! -L "$target" && ! -L "$stage" && ! -L "$root/updates" && ! -L "$root/bin" ]] || { echo 'Symlink path refused' >&2; exit 1; }
verify() { local x; x="$(sha256sum "$1" | cut -d' ' -f1)"; [[ "${x,,}" == "${sha,,}" ]]; }
verify "$stage" || { echo 'SHA mismatch' >&2; exit 1; }
if ((dry)); then echo 'VALIDATION_OK (no changes)'; exit 0; fi
((EUID == 0)) || { echo 'root privileges required' >&2; exit 1; }
[[ ! -e "$tmp" ]] || { echo 'Stale pending file' >&2; exit 1; }
exec 9>"$lock"
flock -n 9 || { echo 'Update already in progress' >&2; exit 1; }
audit() { printf '%s status=%s sha256=%s\n' "$(date -u +%FT%TZ)" "$1" "${sha,,}" >> "$root/updates/update-history.log"; }
healthy() { [[ "$(systemctl is-active "$service")" == active ]] && curl --noproxy '*' --fail --silent --max-time 3 http://127.0.0.1:18889/healthz | grep -qx ok; }
check() { for ((n=0; n<30; n++)); do if healthy; then return 0; fi; sleep 2; done; return 1; }
stopped=0; backup_ready=0; committed=0
recover() {
  local status="$?"
  trap - ERR
  if ((stopped)); then
    audit rollback_begin
    systemctl stop "$service" || true
    if ((backup_ready)); then
      if [[ -e "$target" ]]; then mv -f "$target" "$failed" || true; fi
      mv -f "$backup" "$target" || true
    fi
    systemctl start "$service" || true
    if check; then audit rollback_healthy; else audit rollback_unverified; fi
  fi
  rm -f "$tmp"
  if ((committed)); then echo 'UPDATE_FAILED_ROLLED_BACK_OR_RECOVERY_ATTEMPTED' >&2; fi
  exit "$status"
}
trap recover ERR
trap 'rm -f "$tmp"' EXIT
audit begin
install -m 0755 "$stage" "$tmp"
verify "$tmp" || { echo 'SHA changed while staging' >&2; false; }
systemctl stop "$service"
stopped=1
cp -p "$target" "$backup"
backup_ready=1
mv "$tmp" "$target"
committed=1
systemctl start "$service"
if check; then
  stopped=0
  audit healthy
  echo "UPDATED_OK backup=$backup"
else
  audit health_failed
  false
fi

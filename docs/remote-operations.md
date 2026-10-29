# Remote operations (Reinor / RinoMesh)

This document records the **currently deployed** remote-access environment as of 2026-10-29. Reinor is the device-control project; the existing deployed mesh and gateway still use RinoMesh names for compatibility.

## Access architecture

- Android Termux `rino-phone` → public SSH `ubuntu@xuhuaai.top` → Yggdrasil IPv6 → Windows OpenSSH `rino@rino-pc`.
- The Windows Yggdrasil address is `200:b3c8:28ad:5342:a4a4:5638:170d:625c`. Cloud node: `200:e9c5:447:8534:15f8:d467:f47d:6161`. Router node: `200:d6f6:ce0d:a6b:18c8:ca0f:a433:e44`.
- Phone SSH jump: `ssh -J ubuntu@xuhuaai.top rino@200:b3c8:28ad:5342:a4a4:5638:170d:625c`.
- Windows OpenSSH ED25519 host fingerprint: `SHA256:fQqiDdxR1jZ19Gl+laNnKJ6zjqJL+ctEzYkwtA7P+q4`. Verify this fingerprint before trusting a newly provisioned client.
- The phone SSH key and cloud maintenance SSH key are **separate**. Both were verified by actual passwordless logins. Never store private keys, SSH login passwords, or Yggdrasil `GroupPassword` in this repository.
- The dedicated cloud maintenance private key is at `/home/ubuntu/.ssh/reinor_maintenance_ed25519` (mode 0600). Its public-key authorization in Windows includes `restrict` and `from="200:e9c5:447:8534:15f8:d467:f47d:6161"`. These restrictions disable SSH forwarding/PTY and limit network origin, but **non-interactive remote commands still have Windows administrator privileges**. Protect the cloud account accordingly.
- Cloud operator command: `/home/ubuntu/.local/bin/reinor-pc-admin whoami`. PowerShell scripts can be piped into `/home/ubuntu/.local/bin/reinor-pc-ps`; this encodes stdin as UTF-16LE for Windows PowerShell. The cloud wrapper validates the pinned Windows SSH host key.
- Basic cloud maintenance audit: `/home/ubuntu/reinor-ops/maintenance-audit.log` (0600). Records timestamp, command digest prefix, exit code. It is **not** a complete Windows security audit.

## Auto-start and runtime

- Windows services `sshd` and `Yggdrasil` are both `Automatic`. Their configured SCM failure actions restart them after 10 s, 30 s and 60 s, with a 1-day counter reset. These actions apply to service failures and do not prove full cold-boot readiness.
- Windows Yggdrasil executable/config: `C:\ProgramData\RinoMesh\yggdrasil.exe`, `C:\ProgramData\RinoMesh\config.json`. Local Yggdrasil Admin API: `tcp://127.0.0.1:19091`.
- Windows `InterfacePeers["Wi-Fi"]` pins the router's link-local TLS peer at `[fe80::3a02:e3ff:fe02:32fc]:38459` **with its Yggdrasil public key**. The router's `br-lan` multicast listener now has fixed port `38459`, and the router configuration authorizes the Windows node's public key.
- Router Yggdrasil config: `/etc/rinomesh/yggdrasil.json`. A backup with name `before-port-fixed-20261029` and another `before-pc-allow-20261029` were created before adjustments.
- Windows Yggdrasil pre-change backup: `C:\ProgramData\RinoMesh\config.json.before-lan-persist-20261029-233015`.
- A router Yggdrasil service restart was tested successfully. A **Windows Yggdrasil service restart** was tested using a SYSTEM scheduled task with local config rollback, and both cloud and LAN peers came back automatically. Log: `C:\ProgramData\RinoMesh\reinor-ygg-service-verify.log`. The one-off scheduled task was removed after a passing test. **A full Windows reboot and a simultaneous power-loss recovery have not been tested.**
- The regular (unprivileged) Reinor Agent is `%LOCALAPPDATA%\RinoMesh\rinomesh-agent-join.exe join --device rino-pc-joined`. Its startup script is `%LOCALAPPDATA%\RinoMesh\start-agent.ps1`, launched by scheduled task `Reinor-Agent` in the interactive `rino` account. It starts on logon and has periodic recovery checks; it avoids inheriting dead localhost proxy settings. Its process may be unavailable when nobody is logged in, but the **independent administrative SSH channel** uses the auto-start services above and does not depend on the Agent.
- The Agent connects to `https://xuhuaai.top/agent/ws`; the public MCP endpoint is `https://xuhuaai.top/mcp`. Avoid modifying existing runtime endpoints before validating compatibility.

## Safe checks

Windows via administrator SSH:

```powershell
whoami
fltmc
Get-Service sshd,Yggdrasil
Get-ScheduledTask -TaskName Reinor-Agent
& 'C:\Program Files\Yggdrasil\yggdrasilctl.exe' -endpoint=tcp://127.0.0.1:19091 getPeers
```

Linux cloud:

```sh
/home/ubuntu/.local/bin/reinor-pc-admin whoami
tail /home/ubuntu/reinor-ops/maintenance-audit.log
```

On the OpenWrt router, use `/usr/bin/rinomesh-yggdrasilctl -endpoint=unix:///tmp/rinomesh.sock getPeers` and `getMulticastInterfaces`.

## Revocation and recovery

- To revoke the **cloud admin SSH channel**: in an independent elevated Windows session, remove **only** the `reinor-tx-maintenance-2026-10-29` line from `C:\ProgramData\ssh\administrators_authorized_keys`, preserving other user keys and its SYSTEM/Administrators-only ACL; then securely remove the dedicated cloud keypair and wrapper. Confirm that phone SSH remains valid first.
- To revoke the **phone**: remove only the `rino-termux` public-key line from that same Windows file, and separately remove the phone key from the cloud Ubuntu user's `~/.ssh/authorized_keys`. Never overwrite the entire authorized-keys file.
- To undo Reinor Agent autostart: `Unregister-ScheduledTask -TaskName Reinor-Agent -Confirm:$false` in elevated Windows PowerShell, then stop only the appropriate Agent process. A backup of `start-agent.ps1` was created before changing it.
- To revert the LAN-only Yggdrasil change, restore the Windows `config.json.before-lan-persist-20261029-233015` backup and the router's corresponding dated configuration backup, checking key/identity and parser validity first. Service restarts can temporarily disrupt SSH, so always have an independent recovery path before doing this.
- `Set-PSReadLineOption -PredictionSource History` in the Windows user's PowerShell profile is now guarded for interactive terminals; a dated local backup is retained.

## Android Yggdrasil client limitation

The official Android release `v0.1-021` bundles Yggdrasil `0.5.13`. `GroupPassword` was introduced in core `0.5.14`, and core `0.5.15` specifically fixed mobile bindings to apply the `GroupPassword` configuration. The deployed private overlay enables `AllowedPublicKeys` and `GroupPassword`. The former controls direct peering; the latter filters **session traffic**, not peering. The old Android client cannot yet access this closed overlay. Keep using the working SSH bastion until a vetted Android build with recent mobile bindings, a GroupPassword setting, and a properly provisioned device public key is available.

References: https://yggdrasilnetwork.org/configurationref ; https://github.com/yggdrasil-network/yggdrasil-go/releases ; https://github.com/yggdrasil-network/yggdrasil-android/releases .

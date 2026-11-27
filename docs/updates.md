# Manual, verified updates (phase 1)

This phase deliberately **does not auto-download or auto-install releases**. The operator chooses an artifact and its independently verified SHA-256. Both deployed roles are updated separately; no enrollment, OAuth or mesh configuration files are modified.

## Windows Agent

The active Agent is currently `%LOCALAPPDATA%\RinoMesh\rinomesh-agent-join.exe`, connected as `rino-pc-joined`, with its approved enrollment in `joined-rino-pc-joined.json` and its existing `Reinor-Agent` scheduled task.

1. Build and obtain a reviewed `cmd/agent` Windows executable using the repository CI workflow. Independently record the release asset's **SHA-256** from the trusted build.
2. Place the executable at `%LOCALAPPDATA%\RinoMesh\updates\rinomesh-agent-join.exe`. Place `deploy/update-agent.ps1` in a stable location (for example `%LOCALAPPDATA%\RinoMesh\updates\update-agent.ps1`). Existing credentials and the active binary stay untouched at this stage.
3. Validate from an authorized PowerShell session:

   ```powershell
   $expected = '<the verified 64-character SHA256>'
   & "$env:LOCALAPPDATA\RinoMesh\updates\update-agent.ps1" -Sha256 $expected -DryRun
   ```

4. Use the existing **authenticated MCP `execute_command` tool** with `device_id=rino-pc-joined` to launch the updater *detached*. In the command below, replace the digest literal with the expected hash:

   ```powershell
   $hash = '<the verified 64-character SHA256>'
   Start-Process -FilePath 'powershell.exe' -ArgumentList @(
       '-NoProfile','-NonInteractive','-ExecutionPolicy','Bypass',
       '-File', ('"' + "$env:LOCALAPPDATA\RinoMesh\updates\update-agent.ps1" + '"'),
       '-Sha256', $hash, '-Device','rino-pc-joined'
   ) -WindowStyle Hidden
   'UPDATE_PROCESS_LAUNCHED'
   ```

   The launcher returns first; the updater delays several seconds so the MCP acknowledgement can be delivered. It copies and validates the staged binary again, stops only processes whose executable path matches the installed Agent, keeps a dated backup, and activates the new binary through the existing `Reinor-Agent` scheduled task. It looks for a **fresh** `connected to gateway device=rino-pc-joined` log message and a running Agent process within 60 seconds.
5. Inspect `%LOCALAPPDATA%\RinoMesh\updates\update-history.log`. A `healthy` entry and confirmed MCP device online status indicate success. On a failed health check, the updater attempts to stop the replacement, restore the backed-up binary and restart the previous Agent. Investigate any `rollback_unverified` / `rollback_error` entry over the independent SSH route.

The updater only validates **integrity** against the supplied SHA-256. The operator must ensure the digest comes from a trusted source. A hash copied from the same untrusted download gives no authenticity guarantee. Keep the updates directory private to the device owner. The update script currently trusts the existing Agent scheduled task and logfile. It does not provide a signed release channel or long-term end-to-end functional attestation.

The manual administrator SSH maintenance channel through `rino-tx` stays independent of the Agent, so a bad Agent upgrade does not remove the operator's recovery path.

## Linux Gateway

The Gateway runs as `rinomesh-gateway.service` with binary `/opt/rinomesh-control/bin/rinomesh-gateway`. For a reviewed Linux build, stage `/opt/rinomesh-control/updates/rinomesh-gateway` and record its verified SHA-256. Run as an authorized administrator:

```bash
sudo bash deploy/update-gateway.sh --sha256 '64_CHARACTER_SHA256' --dry-run
sudo bash deploy/update-gateway.sh --sha256 '64_CHARACTER_SHA256'
```

The script holds an exclusive update lock, verifies the staged file twice, retains a dated binary backup, restarts the existing systemd service and checks the local `/healthz` endpoint. If service startup or health validation fails, it attempts to restore the previous binary and restart it. It leaves all service environment files, credentials and state unchanged.

**Gateway health checks are local service checks**, so reconfirm `rino-pc-joined` online through MCP before declaring the entire control plane healthy. Both update scripts keep logs and dated binaries for forensic review. No live deployment is performed merely by merging this branch.

## Known limitations / follow-up

- Establish signed, immutable release manifests and provenance verification before enabling unattended downloads.
- Add an authenticated, dedicated `update_agent` MCP tool with an explicit approval policy once staging and rollback are proven against a disposable device.
- Add a stronger Agent health acknowledgement to the protocol (matching the new version/build) instead of relying on the reconnection log.
- Exercise success and rollback in disposable VMs before enabling unattended updates on the one live remote-management machine.
- Support a bounded, protected cleanup policy for older binary backups and detect stale locks after interruption.

Keep the existing SSH maintenance channel available throughout all deployments. See `docs/remote-operations.md` for recovery and key revocation.

## Android ARM64 executable updates

Use the Android CLI update command with an independently verified SHA-256
from the matching GitHub Release and a local artifact or HTTPS asset URL.

```sh
reinor update -file /path/to/reinor-android-arm64 -sha256 VERIFIED_SHA256 -dry-run
reinor update -file /path/to/reinor-android-arm64 -sha256 VERIFIED_SHA256
reinor update -rollback -sha256 PREVIOUS_VERIFIED_SHA256
```

The command accepts exactly one of -file or -url for installation. The previous
executable is retained as .previous. Enrollment credentials are unchanged.
Restart the process separately to activate an installed update. If the new
executable cannot start, restore the verified previous binary from another
Termux shell. A stale update lock may be removed after confirming no update
is running. Unattended updates and automatic health-check rollback are not
implemented.
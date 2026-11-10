# APatch boot module for Reinor Android ARM64

This optional APM uses APatch's nonblocking `service.sh` phase to start and
restart a previously enrolled Android ARM64 Agent after reboot. The APatch
Manager **Action** button runs `action.sh` to start the same monitor immediately,
without another USB/ADB session or reboot.

- **Privilege:** the APatch startup supervisor is root; the Agent is launched
  with `/system/bin/su -g 2000 -G 3003 shell -c` as UID 2000, with
  Android's AID_INET supplementary group for socket/DNS access. The APatch `magisk` SELinux
  context may differ from an ADB shell. On the test OPPO, even UID 2000 can
  obtain root with `su -c`, so all connected MCP clients have potentially
  root-capable remote command execution. Protect the owner token and use
  device-specific permissions when those are implemented.
- **Network:** the Agent makes outbound WSS connections to the existing
  `https://xuhuaai.top` Gateway. No ADB forwarding, local listening port,
  or wireless debugging port is opened. Mobile data requires an active SIM/data
  plan; the test OPPO has no SIM.
- **Enrollment:** the owner-approved `joined-*.json` stays in the shell-owned
  `DATA_DIR/.config/RinoMesh`, not inside the module or repository.
- **Availability:** the boot script waits for Android to complete startup,
  runs only if the Agent binary is executable, and restarts it after unexpected
  termination. The Agent handles network disconnects/reconnects. A `/dev`
  one-monitor lock prevents duplicate supervisors when Action is clicked more
  than once.
- **Stop:** create `DATA_DIR/.stop` to inhibit new launches, then stop the
  running Agent if desired. Remove `.stop` to re-enable future starts and
  restart the module supervisor (or reboot).
- **Uninstall:** `su -c '/data/adb/apd module uninstall reinor_boot'` and
  reboot, or disable in APatch Manager. This doesn't delete Agent credentials.
- **Compatibility:** APatch 11282 was tested on OPPO CPH2127, Android 12.

The shipped `agent.conf` is specific to `rino-oppo-backup`. Change
`DEVICE_ID` and `DATA_DIR` for another phone before installing. Test the
binary and owner-approved join in a normal user shell before adding autostart.

## Verified v0.2 module package

Download: `https://xuhuaai.top/downloads/reinor/reinor-boot-oppo-apatch-v0.2.zip`

SHA-256: `24ef843bc93408503fc7bfd82a313a568cc0bc89e1d3770adc1977aed335e04a`
(1,849 bytes, no agent binary or credential inside).

The boot supervisor uses the APatch shell identity plus supplementary group
`AID_INET` (3003). Without that group, the long-lived boot-launched Agent
repeatedly failed DNS despite normal Wi-Fi and a valid enrollment. The Action
entrypoint uses BusyBox `setsid` and closed standard streams, avoiding hung
remote calls. APatch may retain effective Linux capabilities even with
`uid=2000(shell)`; explicit `su -c` is also available on the test phone.
Treat this as a **root-capable remote endpoint**, including its API permissions.

## Cable-free installation on an already enrolled OPPO

1. Download the verified `reinor-boot-oppo-apatch-v0.2.zip` to OPPO via its browser or, if the Agent is already connected, retrieve it through its remote shell.
2. In APatch Manager choose **Modules → Install from storage** and select the
   ZIP. It includes *no* `join_secret_...` ticket or Gateway owner credential.
3. Press the module **Action** button to launch the supervisor immediately.
   It reuses the existing credential in
   `/data/local/tmp/reinor-oppo-backup/.config/RinoMesh/`.
4. Confirm `list_devices` shows `rino-oppo-backup` online; run `id` and
   `su -c id` as deliberate diagnostics. Do not reboot until online testing
   succeeds.
5. Later, reboot once to verify APatch auto-start (the proof has not been
   completed just by installing the module).

A phone that has no SIM cannot fall back from Wi-Fi to cellular data.
An ADB-launched orphan Agent may also die when USB debugging disconnects;
the APatch root-backed supervisor is designed to recover this situation.

**Warning:** the existing APatch root grant to Android `shell` means commands
executed by the Agent can elevate via `su -c`. Since the Gateway exposes
remote execution to connected MCP clients, protect device enrollment,
the owner's token, and access to connected models accordingly.

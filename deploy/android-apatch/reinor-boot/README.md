# APatch boot module for Reinor Android ARM64

This optional APM uses APatch's nonblocking `service.sh` phase to start and
restart a previously enrolled Android ARM64 Agent after reboot.

- **Privilege:** the APatch startup supervisor is root; the Agent is launched
  with `/system/bin/su shell -c` as UID 2000. The APatch `magisk` SELinux
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
  termination. The Agent handles network disconnects/reconnects.
- **Stop:** create `DATA_DIR/.stop` to inhibit new launches, then stop the
  running Agent if desired. Remove `.stop` to re-enable future starts and
  restart the module supervisor (or reboot).
- **Uninstall:** `su -c '/data/adb/apd module uninstall reinor_boot'` and
  reboot, or disable in APatch Manager. This doesn't delete Agent credentials.
- **Compatibility:** APatch 11282 was tested on OPPO CPH2127, Android 12.

The shipped `agent.conf` is specific to `rino-oppo-backup`. Change
`DEVICE_ID` and `DATA_DIR` for another phone before installing. Test the
binary and owner-approved join in a normal user shell before adding autostart.

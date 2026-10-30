# Enroll a Linux host in RinoCommander

The existing Gateway accepts device enrollment from Linux and Windows. The Agent uses PowerShell on Windows and `/bin/sh -c` on Linux, while retaining the same authenticated WebSocket framing, execution deadline, output limits and device approval flow.

## Tencent Cloud deployment (rino-tx)

On the target Linux host, build with the Go toolchain matching `go.mod`:

```sh
go test ./cmd/agent ./internal/wire
go build -trimpath -o rinomesh-agent-linux ./cmd/agent
sudo install -o root -g root -m 0755 rinomesh-agent-linux /opt/rinomesh-control/bin/rinomesh-agent-linux
sudo install -d -o ubuntu -g ubuntu -m 0700 /home/ubuntu/.config/RinoMesh
sudo install -o root -g root -m 0644 deploy/rinomesh-agent-linux.service /etc/systemd/system/rinomesh-agent-linux.service
sudo systemctl daemon-reload
sudo systemctl enable --now rinomesh-agent-linux.service
```

Then, from the authenticated RinoCommander owner control surface, use `list_join_requests` to check the reported device ID is **rino-tx** with OS **linux**, and approve the pending request. The Agent saves `/home/ubuntu/.config/RinoMesh/joined-rino-tx.json` with mode 0600. **Do not commit or print this file**; it contains the approved enrollment secret.

Validate using RinoCommander `list_devices` for `rino-tx online=true`, followed by a harmless `execute_command` such as `hostname; id -un; uname -s`. Check `systemctl status rinomesh-agent-linux.service` and `journalctl -u rinomesh-agent-linux.service` for reconnection diagnostics.

The example service uses the existing deployment directories and Gateway `https://xuhuaai.top`. Adapt the unit's paths, account and endpoint for other hosts. **Reinor currently has no application-level privilege management.** The Linux Agent runs as the configured systemd account (`ubuntu` on `rino-tx`) and inherits that account's operating-system permissions. The unit does not add systemd privilege restrictions. On `rino-tx`, existing passwordless sudo policy permits `sudo -n` from RinoCommander, verified after restarting the Agent. This host-level policy should be managed independently of Reinor.

The installer does not alter the Gateway, mesh, PC authorization, or existing join credentials. During upgrades, preserve the enrollment file and use the approved update/rollback workflow; never silently regenerate enrollment tickets.

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

The example service is tied to the existing deployment directories and the existing Gateway `https://xuhuaai.top`. Adapt unit paths, identity and endpoint when deploying elsewhere. The Agent intentionally runs as `ubuntu` with `NoNewPrivileges=yes`, empty capabilities, and a read-only system mount except its own enrollment directory. **RinoCommander commands on this Linux Agent do not have sudo escalation**, even if other desktop tools for the same host have a separate administrative authorization.

The installer does not alter the Gateway, mesh, PC authorization, or existing join credentials. During upgrades, preserve the enrollment file and use the approved update/rollback workflow; never silently regenerate enrollment tickets.

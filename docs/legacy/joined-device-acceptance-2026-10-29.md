# RinoMesh single-MCP and joined-device acceptance — 2026-10-29 (CST)

This is a point-in-time, sanitized verification record. It supersedes the public-MCP and ChatGPT-integration status in `live-activation-2026-10-29.md`; it does not supersede the earlier router/Tailnet architecture.

## Verified deployment

- On `rino-tx`, `rinomesh-gateway.service` and Nginx are active, and the Gateway listens on loopback `127.0.0.1:18889`. The Gateway systemd unit is enabled.
- The only publicly exposed **MCP server URL** is `https://xuhuaai.top/mcp`; its Nginx location forwards to the authenticated Gateway `/mcp` endpoint. Unauthenticated GET returned HTTP 401. Legacy MCP paths were verified removed from the running Gateway.
- OAuth resource metadata at `/.well-known/oauth-protected-resource/mcp` returned HTTP 200. OAuth registration/authorization/token routes and `/agent/ws` remain separate protocol/agent endpoints, not extra public MCP servers.
- The connected `RinoCommander` ChatGPT plugin successfully called `list_devices`, `list_join_requests`, and `execute_command` through its authorized connection.

## Windows joined device

- `rino-pc-joined` is online as a Windows device. The old `rino-pc` registry entry was offline at verification time.
- The join registry contained one approved enrollment for `rino-pc-joined`, with two superseded earlier requests. These counts stayed unchanged after a client restart.
- The Windows client uses `%LOCALAPPDATA%\RinoMesh\rinomesh-agent-join.exe join --device rino-pc-joined`, with its persisted enrollment under `%LOCALAPPDATA%\RinoMesh\joined-rino-pc-joined.json`. The credential file's reported inherited ACL granted access to the signed-in user, Administrators and SYSTEM.
- Windows Startup shortcut `RinoMesh Agent.lnk` points to `start-agent.ps1`, which launches the joined Agent. Calling this launcher while the Agent was running preserved one process and the same PID. **Sign-in auto-start is configured, but an actual Windows sign-out/sign-in or reboot has not yet been tested.**

## End-to-end validation

- From ChatGPT, `RinoCommander.execute_command` executed a read-only PowerShell system-identification command on `rino-pc-joined`: exit code 0 and matching hostname `RINO-PC`, without stderr.
- The Agent was stopped and restarted once using the independent Desktop Commander connection. It resumed directly from its saved enrollment without another approval request. `RinoCommander.list_devices` then reported it online, and a second remote PowerShell command succeeded with exit code 0.
- `/opt/go/bin/go test ./...` passed across the Go module's packages (Agent, Gateway, signer, OAuth, registry, trust and wire).
- No router/firewall/Tailnet settings or website application routes were changed during this verification. No secret values were copied to Git.

## Boundaries and recovery

- `execute_command` can invoke PowerShell as the logged-in Windows user. Keep the Gateway owner credential, OAuth grants and device enrollment private; do not expose an anonymous command endpoint.
- This check validates enrollment recovery after an **Agent process** restart. It does not validate server reboot, Windows reboot, network-loss resilience across locations, or full router proxy behavior.
- To disable sign-in startup without deleting enrollment, remove/disable the Windows Startup shortcut. To temporarily disconnect, stop the joined Agent process. Re-enrollment or credential revocation must be handled deliberately, without publishing credential contents.
- Existing Nginx backups and the Gateway service recovery paths are documented in `live-activation-2026-10-29.md`.

## 2026-10-29 single-endpoint cleanup

- Removed every additional MCP handler from the Gateway source and the running service. The sole MCP server URL is `https://xuhuaai.top/mcp`.
- Removed redundant read-only server registration and the superseded standalone OAuth implementation. The active OAuth login system continues to serve `/mcp`.
- Gateway process restarted successfully; anonymous access to `/mcp` returned HTTP 401. Each former alternative MCP path returned HTTP 404 on the local Gateway; the obsolete secondary OAuth metadata path also returned 404.
- The `RinoCommander` connection survived the rollout: `list_devices`, `list_join_requests`, and remote `hostname` returned successfully after the Gateway and Windows Agent reconnected.
- All current Go module packages passed tests and the new Gateway binary was rebuilt. A prior binary remains available as a recovery artifact outside the source tree; it is not served by any endpoint.

### Deployment-source consistency check

- Synchronized the cleaned `cmd`, `internal`, and `proto` sources, plus Go module files, to the Gateway's actual working directory at `/opt/rinomesh-control/mvp`. Removed the obsolete OAuth package there as well.
- Ran `go test ./...` from that production working directory; all packages passed.
- The active Nginx snippet directory now contains only `rinomesh-mcp.conf` for this service; older snippet copies were deleted after preserving the current single-entry configuration in a restricted backup directory.
- Final checks: the Gateway and Nginx services remained active, the sole MCP URL denied anonymous access (401), OAuth metadata returned 200, and the unauthenticated device WSS route returned 401. No extra MCP routes were registered.

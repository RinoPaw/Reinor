# RinoMesh MVP live activation — 2026-10-29 (CST)

## Verified live state

- Tencent server `rino-tx`: **rinomesh-gateway.service active and enabled**; bind `127.0.0.1:18889`. Runtime built from `experiments/rinomesh-mvp`, with Go 1.25.4.
- Existing public HTTPS `xuhuaai.top`: Nginx exact route `/agent/ws` proxies WSS to `127.0.0.1:18889/agent/ws`. Anonymous GET returns **401**.
- Public MCP `https://xuhuaai.top/mcp`: exact route now proxies `127.0.0.1:18889/mcp-public`, exposing **only `list_devices`**. The earlier `ping` and `utc_time` tools were removed from the repository probe sources; their old systemd service `rinomesh-mcp-probe` was disabled and stopped. Confirmed public `tools/list` contains only `list_devices`, and direct calls of `ping` and `utc_time` through the existing ChatGPT `test` connection now return `unknown tool`.
- Private MCP `http://127.0.0.1:18889/mcp-control`: requires a separate bearer token at `/home/ubuntu/.config/rinomesh/control.token`, only bound to the server loopback, with `list_devices` and `execute_command`. No public Nginx location for this endpoint.
- Windows `rino-pc`: Go Agent `%LOCALAPPDATA%\RinoMesh\rinomesh-agent.exe` is running with user privileges; authenticates with `%LOCALAPPDATA%\RinoMesh\agent.token`; connects outbound to `wss://xuhuaai.top/agent/ws` and automatically reconnects.
- Auto-start at Windows sign-in is set using `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\RinoMesh Agent.lnk`, launching `%LOCALAPPDATA%\RinoMesh\start-agent.ps1`. Script checks that no Agent is already running. Verified launcher exits successfully and only one Agent process is present.
- Superseded local Python `agent.py` HTTP runner on port 19891 was stopped; that port is no longer listening.

## Actual verified end-to-end MCP call

Called the authenticated private Gateway MCP using server-local `curl`, with `tools/call`:

```json
{"name":"execute_command","arguments":{"device_id":"rino-pc","command":"hostname"}}
```

Result: `exit_code: 0`, `stdout: "rino-pc\r\n"`, empty stderr and error. The Gateway journal recorded Agent online, including successful re-registration after Gateway restart. `list_devices` reported `device_id: rino-pc, online: true, os: windows`.

## Regression checks

- Existing `xuhuaai.top/` home: HTTP 200.
- Existing `/_rinomesh` with normal HTTP GET: HTTP 426 (requires upgrade).
- New `/agent/ws` unauthenticated: HTTP 401.
- Nginx config `nginx -t` passes. Gateway and Nginx both active.
- Old MCP probe service disabled. Public tools list includes no test tools.

## Limitations and next integration

ChatGPT's installed `test` plugin may cache its old tool list until refreshed in plugin settings. No programmatic refresh capability was available in the current connected tools.

The command tool is **not yet reachable from ChatGPT's public MCP connection**, because the private `/mcp-control` requires credentials and is intentionally not exposed to anonymous traffic. Use OAuth or an authorized secure tunnel/plugin integration for direct ChatGPT invocation; never proxy arbitrary Windows execution to the unauthenticated `/mcp` URL.

Gateway uses in-memory state; after restart, device online state is re-established by the Agent reconnect. Current Windows auto-start occurs at **user logon**, not at boot before login.

## Rollback

- Original Nginx snippet backed up at `/etc/nginx/snippets/rinomesh-mcp.conf.before-public`; restore and reload Nginx if reverting the public MCP route.
- Original Windows/website configuration backed up before Agent route activation as `/etc/nginx/sites-available/xuhua.before-mvp-*`.
- Stop Gateway with `sudo systemctl disable --now rinomesh-gateway` only after restoring a working route or intentionally retiring MCP.
- Windows autostart shortcut can be removed without modifying installed binaries or token files.

No credentials are stored in this document or repository.

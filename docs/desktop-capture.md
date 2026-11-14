# Desktop screenshot capabilities and availability

This is an initial, **opt-in, owner-authorized** desktop screenshot tool. Windows currently implements desktop capture; a headless Linux Agent can report why the feature is unavailable. Device capability discovery is separate from network connectivity and from the operating-system name.

## MCP interface

`capture_screen` accepts:

| Argument | Meaning |
|---|---|
| `device_id` | Enrolled Agent (from `list_devices`) |
| `max_width` | Optional maximum JPEG width; default 1600, range 320–3840 |
| `quality` | Optional JPEG quality; default 75, range 35–90 |

The result contains a native `image/jpeg` MCP image plus text metadata:

- `width` and `height`: actual encoded bitmap dimensions, possibly downscaled.
- `physical_width` and `physical_height`: full Windows virtual desktop dimensions in **physical pixels**.
- `origin_x` and `origin_y`: Windows virtual desktop top-left in physical pixels, possibly negative for multi-monitor layouts.
- `device_id` and `mime_type`.

When mapping a bitmap pixel back to desktop coordinates, use:

```text
physicalX = origin_x + imageX * physical_width / width
physicalY = origin_y + imageY * physical_height / height
```

All captures use a temporary, **thread-scoped** Windows Per-Monitor DPI Awareness V2 context, restored after the call. The Agent never changes the user's global scaling. Captures may fail when no interactive desktop is available, a session is locked, Windows prevents capture, or the image exceeds the transport limit. The tool returns explicit errors for capture failures; it cannot guarantee every protected/occluded surface is readable.

## Capability discovery

An authenticated Agent announces a capability snapshot in its initial `hello` message, with no explicit protocol version:

```json
[
  {"id":"desktop.capture","supported":true,"available":true}
]
```

Two independent fields matter: `supported` indicates whether the Agent implements the operation, while `available` describes its desktop environment **at the time of observation**. An unavailable capability includes a `reason`, for example `NO_DESKTOP_SESSION`, `DESKTOP_LOCKED`, or `CAPABILITY_UNSUPPORTED`.

- `list_devices` includes `capabilities` and `capabilities_observed_at` for online Agents. This snapshot may become stale if the device locks, unlocks, or changes sessions.
- `get_device_capabilities(device_id)` queries the Agent for **current** status and updates that snapshot.
- `capture_screen(device_id)` routes by declared `desktop.capture` support, never by hardcoded Windows/Linux OS checks. Supported Agents re-check actual availability immediately before capture.
- On a headless Linux server with no GUI session, `desktop.capture` is currently `supported=false`, `available=false`, `reason=NO_DESKTOP_SESSION`. A graphical Linux server remains unsupported until a Linux capture backend is implemented; its reason is `CAPABILITY_UNSUPPORTED`. A future Linux backend can advertise support without changing Gateway dispatch.
- When capture cannot run, MCP returns a structured error with `device_id`, `code`, and `error`. For example:

```json
{"device_id":"rino-tx","code":"NO_DESKTOP_SESSION","error":"desktop capture unavailable on device"}
```

A future headless browser screenshot capability should use a separate ID (such as `browser.capture`), since a browser can render without a desktop.

## Protocol and deployment

- No explicit wire protocol version or version negotiation is used. The Agent and Gateway exchange the screenshot request and result types directly; no legacy fallback is maintained.
- The Agent advertises supported/available capabilities in the authenticated hello. The Gateway validates that announcement and rejects missing or malformed capability data, without a version field or compatibility fallback.
- The Agent uses native Win32/GDI to capture the virtual desktop, scales it before transfer and encodes JPEG; image bytes are bounded to 3 MiB. The binary WebSocket frame limit is now 4 MiB.
- The Gateway returns the captured image via `mcp.ImageContent`; pixels are not inserted into PowerShell stdout, written to disk or logged by default.
- Existing `execute_command` behavior and auth stay unchanged.

**Deployment:** capability announcements require matching Gateway and Agent builds across **all** devices, including headless Linux servers. No backward-compatibility guarantee is provided. Stage verified binaries before a coordinated, reversible deployment with an independent recovery route (see #2). This PR does not alter the running deployment.

## Testing

```sh
go test ./internal/capability ./internal/desktop ./internal/wire ./cmd/agent ./cmd/gateway
go vet ./internal/capability ./internal/desktop ./internal/wire ./cmd/agent ./cmd/gateway
GOOS=windows GOARCH=amd64 go build ./cmd/agent
```

For an authorized unlocked Windows desktop, an opt-in test checks actual GDI capture and JPEG decoding without writing pixels to disk or displaying them:

```powershell
$env:REINOR_CAPTURE_TEST = '1'
go test ./internal/desktop -run TestCaptureScreenInteractive -v
```

## Security

Desktop screenshots can disclose private messages and credentials. Only an authenticated, owner-authorized client should invoke `capture_screen`; maintain explicit user intent for each capture workflow. Do not store or publish image payloads in logs, analytics, public URLs, or CI artifacts. Browser/WebUI control, scoped window capture, consent prompts, and desktop input belong to subsequent issues (#1, #4, #5, #7); this first version captures the full virtual desktop.

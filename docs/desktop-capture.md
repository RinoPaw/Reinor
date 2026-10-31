# First-class Windows desktop screenshots

This is an initial, **opt-in, owner-authorized** screenshot tool for an enrolled interactive Windows Agent. It is independent of Knoven and other networking stacks.

## MCP interface

`capture_screen` accepts:

| Argument | Meaning |
|---|---|
| `device_id` | Enrolled Windows Agent (from `list_devices`) |
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

## Protocol and deployment

- The Agent and Gateway require the same **wire protocol version 2**. Enrollment, authenticated Agent hello and Gateway acknowledgment must match exactly. Earlier and newer protocol revisions are rejected; there is no capability fallback.
- Desktop capture is only available on Windows; non-Windows Agents receive an explicit platform error.
- The Agent uses native Win32/GDI to capture the virtual desktop, scales it before transfer and encodes JPEG; image bytes are bounded to 3 MiB. The binary WebSocket frame limit is now 4 MiB.
- The Gateway returns the captured image via `mcp.ImageContent`; pixels are not inserted into PowerShell stdout, written to disk or logged by default.
- Existing `execute_command` behavior and auth stay unchanged.

**Breaking change:** all deployed Agents (Windows and Linux) must be upgraded together with the Gateway. Older Agents cannot connect to a v2 Gateway, and v2 Agents reject older Gateways. Do not deploy by replacing just one side. Prepare verified binaries for every device, maintain the independent SSH recovery route, and perform a coordinated, reversible cutover (see #2). The live system is intentionally unchanged by this PR.

## Testing

```sh
go test ./internal/desktop ./internal/wire ./cmd/agent ./cmd/gateway
go vet ./internal/desktop ./internal/wire ./cmd/agent ./cmd/gateway
GOOS=windows GOARCH=amd64 go build ./cmd/agent
```

For an authorized unlocked Windows desktop, an opt-in test checks actual GDI capture and JPEG decoding without writing pixels to disk or displaying them:

```powershell
$env:REINOR_CAPTURE_TEST = '1'
go test ./internal/desktop -run TestCaptureScreenInteractive -v
```

## Security

Desktop screenshots can disclose private messages and credentials. Only an authenticated, owner-authorized client should invoke `capture_screen`; maintain explicit user intent for each capture workflow. Do not store or publish image payloads in logs, analytics, public URLs, or CI artifacts. Browser/WebUI control, scoped window capture, consent prompts, and desktop input belong to subsequent issues (#1, #4, #5, #7); this first version captures the full virtual desktop.

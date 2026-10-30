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

- Windows Agents advertise `capture_screen/v1` during the authenticated WebSocket hello.
- The Gateway refuses capture requests for older Agents or non-Windows devices instead of waiting for a timeout.
- The Agent uses native Win32/GDI to capture the virtual desktop, scales it before transfer and encodes JPEG; image bytes are bounded to 3 MiB. The binary WebSocket frame limit is now 4 MiB.
- The Gateway returns the captured image via `mcp.ImageContent`; pixels are not inserted into PowerShell stdout, written to disk or logged by default.
- Existing `execute_command` behavior and auth stay unchanged.

**Both the Gateway and Windows Agent need updating before this MCP tool can be used.** Do not hot-swap the live Agent or Gateway merely to verify this feature. Follow the existing staged-update and rollback procedures (#2).

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

# Hexveil

**One control surface. Any network underneath.**

Hexveil is a standalone **Model Context Protocol (MCP)** application for managing and executing tasks on enrolled personal devices. It is deliberately separate from the mesh, router, DNS, and proxy infrastructure.

**Boundary:** Yggdrasil, LAN, Tailscale, or a conventional HTTPS network may provide connectivity. Hexveil does not implement the network itself, and the network must continue working if Hexveil is stopped or removed.

## What belongs here

- MCP tools and their authorization boundary.
- Device enrollment / owner approval and online-status tracking.
- Windows Agent command execution and its authenticated Gateway connection.
- MCP OAuth integration and app-specific tests.

## What does not belong here

- Yggdrasil routing, peers or node configuration.
- ImmortalWrt/OpenWrt, DNS, Mihomo, OpenClash, Tailscale or Headscale configuration.
- Router backup files, proxy subscriptions, or credentials.

## Repository layout

- `cmd/gateway`: authenticated MCP server and device command dispatcher.
- `cmd/agent`: Windows client and enrollment flow.
- `cmd/signer`: experimental device-owner signature CLI.
- `internal/`: protocol framing, OAuth, registry and trust building blocks.
- `proto/`: wire protocol.

Some internal identifiers and default deployment URLs still use historical RinoMesh names so existing deployed clients keep working. These are **application compatibility details**, not a dependency on the retired Rino-Network repository. A later change can make the OAuth public origin configurable without changing the current deployment implicitly.

## Development

Go 1.25 or later.

```sh
go test ./...
go vet ./...
go build ./cmd/gateway
GOOS=windows GOARCH=amd64 go build -o hexveil-agent.exe ./cmd/agent
```

`go build ./cmd/gateway` builds a local binary; do not commit it. The project requires no Yggdrasil daemon or kernel networking features to compile or test.

## Operational compatibility

The live deployment (as of 2026-10-29) uses `https://xuhuaai.top/mcp` as its authenticated MCP endpoint and `wss://xuhuaai.top/agent/ws` for outbound device Agent connections. These are configurable application endpoints at the hosting/proxy level; the MVP OAuth issuer currently defaults to this public domain. Moving Hexveil source to a separate repository **does not** change the running Gateway or its enrolled Windows Agent automatically.

The Agent executes PowerShell with the signed-in Windows user's permissions. The MCP control surface must require authorization, restrict trusted clients, and never expose anonymous remote execution.

Keep live enrollment tickets, OAuth tokens, owner credentials and generated configuration **outside Git**, even for private repositories.

## Migration notes

This is a source-only extraction of `experiments/rinomesh-mvp` from `RinoPaw/Rino-Network`, retaining wire compatibility while moving the Go module path to `github.com/RinoPaw/Hexveil`. Runtime service names, local state and enrollment files are intentionally untouched during migration. Do not delete the old repository until the new GitHub repository exists, its source is verified, and the required historical records have been preserved separately.

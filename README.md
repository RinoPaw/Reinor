# Reinor

**One control surface. Any network underneath.**

Reinor is a standalone **Model Context Protocol (MCP)** application for managing and executing tasks on enrolled personal devices. It is deliberately separate from the mesh, router, DNS, and proxy infrastructure.

**Naming:** **Knoven** is the private mesh-network project. **Reinor** is the network-independent device management and MCP application. The currently installed ChatGPT connector may still be displayed as **RinoCommander** until its connection title is updated.

**Boundary:** Yggdrasil, LAN, Tailscale, or a conventional HTTPS network may provide connectivity. Reinor does not implement the network itself, and the network must continue working if Reinor is stopped or removed.

## What belongs here

- MCP tools and their authorization boundary.
- Device enrollment / owner approval and online-status tracking.
- Windows and Linux Agent command execution and their authenticated Gateway connections.
- Owner-authorized Windows desktop screenshot capture (staged; see [desktop capture documentation](docs/desktop-capture.md)).
- MCP OAuth integration and app-specific tests.

## What does not belong here

- Yggdrasil routing, peers or node configuration.
- ImmortalWrt/OpenWrt, DNS, Mihomo, OpenClash, Tailscale or Headscale configuration.
- Router backup files, proxy subscriptions, or credentials.

## Repository layout

- `cmd/gateway`: authenticated MCP server and device command dispatcher.
- `cmd/agent`: Windows/Linux client and enrollment flow.
- `cmd/signer`: experimental device-owner signature CLI.
- `internal/`: protocol framing, OAuth, registry and trust building blocks.
- `proto/`: wire protocol.

Some internal identifiers, persisted paths, service names, and default deployment URLs still use historical RinoMesh names so existing deployed clients keep working. These are **application compatibility details**, not a dependency on the retired Rino-Network repository. A later change can make the OAuth public origin configurable without changing the current deployment implicitly.

## Development

Go 1.25 or later.

```sh
go test ./...
go vet ./...
go build ./cmd/gateway
GOOS=windows GOARCH=amd64 go build -o reinor-agent.exe ./cmd/agent
GOOS=linux GOARCH=amd64 go build -o reinor-agent-linux ./cmd/agent
```

`go build ./cmd/gateway` builds a local binary; do not commit it. The project requires no Yggdrasil daemon or kernel networking features to compile or test.

## Operational compatibility

The live deployment (as of 2026-10-29) uses `https://xuhuaai.top/mcp` as its authenticated MCP endpoint and `wss://xuhuaai.top/agent/ws` for outbound device Agent connections. These are configurable application endpoints at the hosting/proxy level; the MVP OAuth issuer currently defaults to this public domain. Moving Reinor source to a separate repository **does not** change the running Gateway or its enrolled Windows Agent automatically.

The Agent executes PowerShell on Windows and `/bin/sh` on Linux, using the operating-system permissions of the Agent account. Reinor does not implement per-device privilege management; host sudo permissions remain controlled by the operating system. The MCP control surface must require authorization, restrict trusted clients, and never expose anonymous remote execution.

Keep live enrollment tickets, OAuth tokens, owner credentials and generated configuration **outside Git**, even for private repositories.

## Migration notes

The project was extracted from experiments/rinomesh-mvp in RinoPaw/Rino-Network and maintained as RinoPaw/Hexveil before being named Reinor. The active Go module path is github.com/RinoPaw/Reinor.

This source-only rename retains the existing deployed service names, config, credentials and enrolled-device state. Read docs/rename-compatibility.md before any deployment changes.
The Linux Agent uses `/bin/sh -c` and has a systemd deployment documented in [docs/linux-agent.md](docs/linux-agent.md). The Windows Agent continues using PowerShell.

# Windows 7 Agent (experimental)

This is a compatibility build of the existing Reinor device Agent, using **Go 1.20.14** (the last Go release that supports Windows 7) with a separate dependency graph in `go.win7.mod` / `go.win7.sum`. The main module, authenticated Gateway, and owner-approval enrollment protocol are unchanged. No per-device privilege-management layer is added.

## Reproducible builds

From the repository root on a supported Windows build machine, install the official Go 1.20.14 toolchain. The `golang.org/dl/go1.20.14` version selector is one option; run `go1.20.14 download` after installing that selector.

```powershell
powershell -ExecutionPolicy Bypass -File .\scripts\build-win7-agent.ps1
```

The script tests and vets `./cmd/agent` and `./internal/wire`, then creates:

- `dist/reinor-agent-win7-amd64.exe` (64-bit Windows)
- `dist/reinor-agent-win7-386.exe` (32-bit Windows)

It prints a SHA-256 checksum for each binary. Build only one architecture with `-Architecture amd64` or `-Architecture 386`. Override the Go path with `-GoExecutable` if needed. `CGO_ENABLED=0`; no C compiler or Win7 SDK is required.

The regular `go.mod` remains on Go 1.25 or later, and ordinary builds continue to use it.

## Enroll the Win7 device

1. Confirm Windows 7 SP1, CPU architecture and basic Internet reachability. The Win7 certificate store must trust the Gateway certificate; old systems may lack suitable TLS/root-certificate updates.
2. Copy the matching executable to the Win7 machine, e.g. `C:\Reinor\reinor-agent-win7-amd64.exe`.
3. In a Win7 command prompt in that directory, run:

```cmd
reinor-agent-win7-amd64.exe join -gateway https://xuhuaai.top -device rino-win7
```

4. The Agent prints a join request ID and waits. From the authenticated Reinor owner interface, inspect `list_join_requests` and approve this specific request ID via `approve_join_request`.
5. After approval, the Agent saves `%LOCALAPPDATA%\RinoMesh\joined-rino-win7.json` and connects to `wss://xuhuaai.top/agent/ws`. Keep the process running; it reconnects automatically after disconnection.
6. Confirm `rino-win7` is online with `list_devices`, then run a harmless command such as `hostname` using `execute_command`.

Use a stable, unique device ID. Do not print or commit the JSON enrollment file: it contains an authorization secret. The Agent executes PowerShell using the Windows account that launched it. The Windows 7 compatibility build does not add permission management or modify machine privileges.

For launch on user login, create a shortcut in the Win7 user's `shell:startup` directory targeting the Agent executable with the `join -gateway ... -device ...` arguments. The process runs during that user's session.

## Scope and verification

Compatibility changes replace `math/rand/v2` retry jitter with the Go 1.20 `math/rand` equivalent and pin `github.com/coder/websocket v1.8.13` and `google.golang.org/protobuf v1.33.0` in the alternate module file. The normal Agent protocol and message framing are shared with current builds.

Builds, tests and vet were checked on Windows 11 using the actual Go 1.20.14 compiler, for both `windows/amd64` and `windows/386`. **An actual Windows 7 boot, TLS handshake, enrollment and remote-command run still need verification on a Win7 machine.** Do not mark this as Windows 7 production support solely from cross-compilation.

References: [Go 1.20 release notes](https://go.dev/doc/go1.20) and [Go Windows support matrix](https://go.dev/wiki/Windows).

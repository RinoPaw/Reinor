# Reinor rename and runtime compatibility

**Knoven** names the private mesh network; **Reinor** names the network-independent personal-device management and MCP application, previously known as Hexveil. The historical **RinoCommander** ChatGPT connection is part of Reinor, not a third project.

## Renamed in source

- Product name in the active README.
- Go module and internal imports: github.com/RinoPaw/Reinor.
- Go CI artifact names: reinor-gateway and reinor-agent.exe.
- Windows agent example build artifact: reinor-agent.exe.

## Kept intentionally for deployed compatibility

- rinomesh-gateway.service, its systemd account, work directory and /opt/rinomesh-control installation layout.
- Windows startup launcher, %LOCALAPPDATA%\RinoMesh, rinomesh-agent.exe and enrolled device state/credentials.
- RINOMESH_* environment variables, device IDs, persisted keys, and existing enrollment/protocol data. The *source code* now reports the MCP server implementation name as `reinor`; deployed clients are unchanged until an independently verified rollout.
- wss://xuhuaai.top/agent/ws, HTTPS MCP paths, OAuth callback/issuer, protocol signatures and persisted tokens.
- docs/legacy historical deployment records.

This is a SOURCE-ONLY rebrand. Do not redeploy the Gateway, restart services, rotate credentials or rename persisted state as part of branding.

The GitHub repository already lives at `RinoPaw/Reinor`, and the Go module path is `github.com/RinoPaw/Reinor`. The ChatGPT custom-connector display name `RinoCommander` must be changed in the connection configuration; changing a Go MCP implementation name alone does not rename the ChatGPT connection.

Do not alter systemd unit IDs, network interfaces, saved device credentials, OAuth issuer, access tokens, signing domains, or wire-protocol identifiers solely for branding. The live Gateway and enrolled Agents must be updated separately with a health check and rollback plan.
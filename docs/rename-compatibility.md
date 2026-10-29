# Reinor rename and runtime compatibility

Reinor is the new name of the independent personal-device MCP control application, previously known as Hexveil.

## Renamed in source

- Product name in the active README.
- Go module and internal imports: github.com/RinoPaw/Reinor.
- Go CI artifact names: reinor-gateway and reinor-agent.exe.
- Windows agent example build artifact: reinor-agent.exe.

## Kept intentionally for deployed compatibility

- rinomesh-gateway.service, its systemd account, work directory and /opt/rinomesh-control installation layout.
- Windows startup launcher, %LOCALAPPDATA%\RinoMesh, rinomesh-agent.exe and enrolled device state/credentials.
- RINOMESH_* environment variables and rinomesh-control MCP implementation name.
- wss://xuhuaai.top/agent/ws, HTTPS MCP paths, OAuth callback/issuer, protocol signatures and persisted tokens.
- docs/legacy historical deployment records.

This is a SOURCE-ONLY rebrand. Do not redeploy the Gateway, restart services, rotate credentials or rename persisted state as part of branding.

GitHub repository rename to RinoPaw/Reinor is a separate administrative step. Existing consumers of the Go module may need their imports updated.
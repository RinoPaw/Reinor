# Temporary support sessions

One-time support is for a friend's computer during a specific, consented troubleshooting session. It is independent of long-lived device enrollment and does not alter existing enrollment credentials.

## Prerequisites

- Reinor Gateway with temporary session support deployed.
- Windows 10/11 x64 or supported Linux Agent built from this branch.
- The friend must explicitly run the Agent on their own computer.
- The operator must approve the exact temporary device request.

## On the friend's Windows computer

Download the verified `reinor-agent-temp.exe` from the operator's trusted link, then run from a normal user terminal:

```powershell
.\reinor-agent-temp.exe join -temporary -device temp-friend-mysql
```

Do not run as Administrator unless a diagnostic actually needs administrator rights and the friend separately approves that specific action. No service or startup task is created.

The program prints a `join_...` request ID, but **does not print or save the private join ticket**. The friend sends the request ID to the operator. This is a 10-minute pending approval window.

The operator calls `list_join_requests` to confirm the exact device name and `mode=temporary`, then `approve_join_request(request_id)`. Approval starts a **60-minute server-side lifetime**.

During that time, the operator can use the normal `execute_command` and `get_device_capabilities` tools. These commands run with the same operating-system user privileges as the friend-launched Agent. Avoid reading files unrelated to the incident or printing credentials in terminal output. Before changing MySQL configuration or data, ask the owner and capture a suitable backup.

## Ending a session

- **Friend:** close the Agent console or terminate its process.
- **Operator:** call `revoke_temporary_session(request_id)` to invalidate the credential and disconnect the session immediately.
- **Gateway:** closes the session automatically when its 60-minute lifetime ends.
- **Agent:** makes only one authorized WebSocket connection, never automatically retries after disconnect and never writes a `joined-*.json` file.

After the first authorized connection, the Gateway persistently records the ticket as consumed. It cannot be used for another connection, even after Gateway restart. A revoked or expired ticket also cannot be used. An unexpected network disconnect requires a **fresh join request and fresh owner approval**.

**The owner should request revocation even after the Agent exits.** The code rejects re-use after the first connection, but explicit revocation provides an unambiguous end state in Gateway records.

## Scope and security boundaries

- `join -temporary` requires a device ID beginning `temp-`. A currently pending or approved device with the same ID blocks a second temporary join.
- Long-lived `join` credentials remain valid, unchanged, and capable of reconnecting as before.
- The `revoke_temporary_session` tool rejects long-lived requests; it is not a general device-deletion operation.
- The Gateway persists only a SHA-256 hash of each temporary ticket, the approval metadata, the first-use timestamp, and its expiry, not the cleartext ticket.
- The Gateway validates authorization before registering the Agent and shuts down its transport on revocation or expiry. Commands already started by the Agent receive connection-context cancellation. Child processes created outside the command's process tree may survive and must be handled separately.
- No per-command user approval, file-scope access control, screenshot consent prompt, or sandbox isolation is implemented. **This session grants full command execution within the friend's OS account.** Use an unprivileged account and ensure that the friend understands the scope.

For a new temporary diagnostic session, do not copy another device's local credential file. Launch the program again with a different temporary ID or revoke the old request before reusing its ID.

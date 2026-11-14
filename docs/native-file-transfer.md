# Native binary file transfer (experimental)

Status: integrated with the existing desktop-capture and temporary-session code. Deployment requires matching Gateway and Agent binaries.

Reinor transfers binary data through its existing authenticated MCP control
surface and outbound Agent WebSocket connection. It does not require SSH, a
separate port, a mesh VPN, or a public upload URL.

## MCP operations

| Tool | Purpose |
| --- | --- |
| `begin_upload(device_id, path, size, sha256)` | Open a staged upload and return a 32-character transfer ID |
| `upload_chunk(device_id, transfer_id, offset, data_base64)` | Write up to 128 KiB, returning `next_offset` |
| `finish_upload(device_id, transfer_id)` | Verify full SHA-256, then publish a complete file |
| `abort_upload(device_id, transfer_id)` | Remove the staged temporary file |
| `get_file_info(device_id, path)` | Return byte size and full SHA-256 |
| `download_chunk(device_id, path, offset)` | Return up to 128 KiB in base64, with byte size and per-chunk SHA-256 |

All paths are absolute **on the Agent device**. No path or data is inferred
from the MCP client host. The caller must compute the complete file size and
SHA-256 before starting an upload. Chunks must be sent in order using the
`next_offset` returned by each acknowledgment. An identical chunk may be
retried at the same offset without duplicating bytes. A conflicting retry fails.

Download callers should fetch `get_file_info`, request chunks from offset
zero, check each chunk checksum, assemble the bytes, then compare the final
size and SHA-256 against the initial metadata. This detects changes to the
remote file while downloading.

## Safety properties

- Standard owner OAuth / control-token authorization remains required.
- A connected Agent must advertise `file-v1`; older Agents fail fast.
- Agent filesystem permissions are unchanged. No privilege escalation.
- No overwrite support: existing destinations are rejected; publication uses
  an atomic, same-directory hard link after complete verification.
- Incomplete uploads stay in random temporary files in the destination
  directory, never under the final filename.
- Checksum failures, explicit aborts, Agent connection loss, and 15 minutes
  of upload inactivity clean up staged files. Calling `abort_upload` is
  recommended for immediate cleanup after a cancelled client operation.
- Maximum chunk: 128 KiB; maximum file: 4 GiB; at most two staged uploads
  per Agent connection. No binary payload or file content is logged by the
  file transfer implementation.

## Limits and upgrade procedure

This first implementation has no cross-reconnect resume, no overwrite
option, and no direct bulk-stream endpoint. It is suitable for small/medium artifacts; large files require
many MCP calls. `get_file_info` computes a full SHA-256 and may time out on
slow storage. The client must handle partial failure or retry explicitly.

The shared frame remains limited to 4 MiB to preserve desktop capture; the 128 KiB decoded chunk limit leaves
space for framing overhead. The old command protocol is unchanged.

To deploy safely, upgrade the Gateway and Agents as separate supervised
operations with a rollback path. An older Gateway ignores the new hello
field; a newer Gateway rejects file operations from older Agents. Do not
replace a running Agent or Gateway from its own remote command session.

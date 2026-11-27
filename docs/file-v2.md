# File v2 (binary data plane)

File v2 separates MCP control from HTTP binary transfer. No type detection,
conversion, base64 file payload, or image-specific processing is performed.
All binary bodies use application/octet-stream.

## Control tools

- download_file(device_id, path): start a transfer from an online Agent.
- upload_file(device_id, path, size, sha256): authorize staging original bytes.
  The returned URL accepts a single HTTP PUT with application/octet-stream
  and exact declared Content-Length and SHA-256.
- get_transfer(transfer_id): inspect status and staged bytes; a completed
  download returns a short-lived URL supporting GET/HEAD and HTTP Range.
- cancel_transfer(transfer_id): revoke access, remove staging and request
  Agent cancellation. This cannot undo publication already committed.
- list_transfers(): list records without bearer URLs.

Do not log or share capability URLs. Their random tokens grant access to
one transfer. Agent tokens use an Authorization header and are scoped to
the transfer and direction. Data URLs derive from the authenticated WSS
origin. Agent HTTP redirects are rejected.

Downloads are published only after the Gateway verifies length and SHA-256.
Uploads complete only after the Agent verifies, syncs the staging file,
atomically publishes without overwriting and reports success. A lost
confirmation after possible publication produces unknown. Filesystems
without hard-link support reject upload publication.

## Deployment

Set REINOR_PUBLIC_ORIGIN=https://your-existing-gateway-host on the Gateway.
The origin must also be the Agent WSS host. Upgrade Gateway and Agent.
The reverse proxy must route /transfer/v2/ to the same Gateway. Nginx example:

    location ^~ /transfer/v2/ {
        access_log off;
        proxy_pass http://127.0.0.1:18889;
        proxy_http_version 1.1;
        proxy_request_buffering off;
        proxy_buffering off;
        client_max_body_size 256m;
        proxy_read_timeout 900s;
        proxy_send_timeout 900s;
    }

Keep the data directory (join DB path + .transfers) private and exclusively
owned by the single Gateway. Do not expose it through a static file server.
Do not log transfer URLs or Authorization headers in proxies. HTTPS only.
Old File v1 methods remain during migration.

## Initial limits

256 MiB/file, four retained records, one active stream per Agent, 15 minute
transfer deadline, 10 minute completed URL lifetime. Completed URLs may be
reused within their lifetime. State is in memory. Gateway restart invalidates
credentials and cleans old owned staging files. Permanent devices only.
Cross-reconnect upload resume, persistent journal, confirmed per-byte
delivery progress and temporary-session transfers remain to be implemented.
HTTP Range enables resumable client downloads of completed files.

The MCP result contains metadata and a URL, not file bytes. The calling
application must support HTTP GET/PUT outside MCP. Verify the real client
environment before migration.

# Dynamic Connector

Gateway owns operation definitions and execution. The MCP transport exposes
three stable tools in dynamic mode. Operations are registered at startup
from typed Go handlers, with input schemas generated from those types.

- discover(namespace?, operation?, device_id?): return the current catalog.
  With no namespace or operation filter, return a compact index without input
  schemas. Device availability is an advisory snapshot, not authorization.
- query(operation, revision, arguments): call a registered read operation.
- invoke(operation, revision, arguments): call a registered action. Actions
  may modify devices or disclose data. Client confirmation requirements remain
  applicable.

Revision is an opaque SHA-256 of the definition and a handler semantics
version. Copy it verbatim from discover. Change Spec.Version when semantics
change without a schema/description change. Unknown operations and stale
revisions fail without invoking the handler. Rediscover and reconsider the
operation before retrying; never automatically retry side effects.

Arguments are JSON objects validated against the operation schema, with
additional top-level properties rejected. Results retain the native MCP
content format, including image content for desktop capture and the existing
binary file transfer URLs for File v2. No file bytes are moved through this
dispatch layer.

The existing owner authentication model is unchanged: authenticated owners
can call all registered operations. No operation ACL, whitelist, RBAC or new
authorization configuration is introduced. Existing device enrollment,
temporary session restrictions and Agent operating system privileges remain
in force. query and invoke describe effects; query rejects action operations.
File download, desktop capture and retrieval of bearer download credentials
are actions because they can disclose data. Dynamic call logs contain the
operation name, effect and success, not arguments, outputs or credentials.

## Deployment

REINOR_MCP_MODE=dual (default): native tools and three dynamic tools, sharing
the same definitions and typed handlers, for migration.
REINOR_MCP_MODE=dynamic: only discover, query and invoke are exposed.

After switching to dynamic mode, refresh the MCP client definition once.
Future operation additions and removals need no changes to the three fixed
tool definitions. The server contract test verifies this across two
registries; real ChatGPT tool-schema acceptance and client confirmation
behavior must still be validated. Legacy File v1 operations remain until
the file subsystem migration is completed.

Registration is performed before serving requests; the catalog changes on
Gateway upgrades, not by hot-registering arbitrary remote code. No Agent
protocol or credentials are changed by this migration.

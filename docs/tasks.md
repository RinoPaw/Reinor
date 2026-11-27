# Asynchronous command tasks

This first implementation separates command submission from result retrieval
using task.submit, task.get and task.list, inspired by AWS Systems Manager
Run Command. The MCP request can finish before the device command does.

- A modern Agent advertises task.async.v1; old Agents retain synchronous
  execution. No Windows 7 compatibility build changes.
- Only online devices are accepted. One active asynchronous task per device;
  a concurrent synchronous operation can cause delivery to fail.
- Execution timeout: 1-3600 seconds, default 300. Existing synchronous
  commands remain bounded at 20 seconds.
- States: pending, dispatched, succeeded, failed, timed_out, unknown.
  Dispatched means attempting delivery, not proof of process start.
  Unknown means the result cannot be established.
- Records and final output (existing 64 KiB per stream limit) persist in
  private JSON files under the join database path with a .tasks suffix.
  Command text is not stored. Outputs can contain sensitive data.
  No automatic retention or pruning yet.
- Restart or disconnect marks unfinished work unknown; never automatically
  replay commands with possible side effects. Connection cancellation stops
  the direct child; independently spawned descendants may survive.
- Cancellation API, live output, offline delivery and cross-reconnect
  recovery are not implemented. They require process supervision and
  explicit acknowledgment semantics.

Deploy an updated Gateway and modern Agents before use. Dynamic MCP
clients discover the new operations without changing the three fixed tools. No database migration required.

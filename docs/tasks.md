# Asynchronous command tasks

The Gateway operation registry exposes task.submit (invoke), task.get
(query), task.list (query), and task.cancel (invoke). The three fixed MCP
tools are unchanged. Discover the task namespace to obtain current schemas
and revisions. Native equivalents remain in dual mode.

- A modern Agent advertises task.async.v1; older Agents retain synchronous
  execution. No Windows 7 compatibility build changes.
- Submission requires an online task-capable device. One active async task
  per device; commands and legacy file operations share the execution slot.
  File v2 uses its independent stream slot.
- Execution timeout: 1-3600 seconds, default 300. Synchronous commands
  remain bounded at 20 seconds. Base64 is the optional command argument.
- States: pending, dispatched, running, succeeded, failed, timed_out,
  cancelled, unknown. Dispatched means attempting delivery. Running is
  reported only after Agent process start. A very short task may complete
  without a running snapshot.
- Agent sends cumulative stdout/stderr snapshots on start and at most once
  per second when changed. Each stream retains its first 64 KiB. task.get
  returns the latest stored snapshot or final output; this is bounded
  polling, not a complete unbounded log stream. task.list omits output.
- task.cancel persists a cancellation request and signals the exact active
  task on its original Agent connection. The request is idempotent.
  Cancelled is recorded only if cancelled before dispatch or the Agent
  reports cancellation. A normal completion may win the race. Cancellation
  stops the direct process; independently spawned descendants may survive.
- Metadata and bounded output persist in private JSON files under the
  join database path plus .tasks. Command text is not stored. No retention
  policy yet; logs can contain sensitive information.
- Restart or disconnect marks unfinished work unknown, preserves already
  persisted output, and never automatically replays commands.
- Offline queueing, process-tree supervision and cross-reconnect recovery
  are not implemented.

Deploy an updated Gateway and modern Agents before using async tasks.
Dynamic clients do not need a tool-list refresh.
# sandbox — run the CLI somewhere else

A `claude.Transport` that drives the Claude Code CLI inside a sandbox over a
socket, so the process holding the SDK session is not the process that can
execute the agent's tool calls.

```
┌────────────────────┐                    ┌──────────────────────┐
│ your program       │                    │ sandbox              │
│  claude.Client     │ ── JSON frames ──▶ │  sandbox-host        │
│  sandbox.Transport │ ◀── over socket ── │  claude CLI          │
└────────────────────┘                    └──────────────────────┘
```

Two halves:

- **`sandbox.Transport`** — implements `claude.Transport`, dials a host.
- **`sandbox.Host`** / **`cmd/sandbox-host`** — listens, spawns the CLI, pipes
  frames back.

## Quick start

In the sandbox:

```bash
go run ./cmd/sandbox-host -network unix -listen /run/claude-sandbox.sock -cwd /work
```

In your program:

```go
tr := sandbox.New(sandbox.Config{
    Network: "unix",
    Address: "/run/claude-sandbox.sock",
    Start:   sandbox.DefaultStartRequest(),
})

client, err := claude.NewClientWithTransport(ctx, opts, tr)
```

Everything the SDK carries over the control protocol keeps working unchanged:
hooks, `CanUseTool`, in-process MCP servers, and the runtime control methods on
`claude.Client` (`Interrupt`, `SetModel`, `GetContextUsage`, …).

## The one thing to understand

**A custom transport bypasses the SDK's subprocess builder.** Only the
`initialize` request crosses the wire; every option the SDK would have turned
into a CLI flag is silently dropped.

That is why the *host* builds the argv, not the client. A `Policy` holds the
flags the sandbox operator controls; `StartRequest` carries the small set a
client may vary. The sandbox decides its own containment rather than trusting
whatever dials in.

### Options that do NOT survive a custom transport

Setting these in `AgentOptions` has no effect. Move them to the host `Policy`:

| `AgentOptions` field | Host equivalent |
|---|---|
| `AllowedTools`, `DisallowedTools` | `-allowed-tools`, `-disallowed-tools` |
| `PermissionMode` | `-permission-mode` |
| `MaxTurns` | `-max-turns` |
| `Cwd`, `AddDirs` | `-cwd`, `-add-dir` |
| `Model`, `FallbackModel` | `-model` |
| `SettingSources` | `-setting-sources` |
| `Sandbox` (bash isolation) | `-bash-sandbox` / `-settings` |
| `Resume`, `ForkSession`, `SessionID` | `StartRequest`, gated by `-allow-resume` |
| `MCPServers` (in-process) | `StartRequest.SDKMCPServers` — see below |

Two of these are worth calling out because they fail *silently*:

- **`--permission-prompt-tool`** is what routes tool approvals to
  `AgentOptions.CanUseTool`. If the host does not pass it, your permission
  callback is never consulted and the CLI decides every tool call by itself.
  `DefaultStartRequest()` sets it; `TestPolicyBuildArgv` guards it.

- **`--mcp-config`** is how in-process (`sdk`) MCP servers get registered. Set
  `StartRequest.SDKMCPServers` to the same names you put in
  `AgentOptions.MCPServers`, or the CLI never learns your tools exist and the
  agent runs without them.

### Options that DO survive

Hooks, `CanUseTool` (given the flag above), `Agents`, `SystemPrompt` /
`AppendSystemPrompt`, `ToolAliases`, `PlanModeInstructions`, and every
`claude.Client` runtime control method. These travel on the initialize request
or the control protocol.

## Security

The host is the trusted side. Some notes:

- **Set a token.** `SANDBOX_TOKEN` is compared in constant time. Without one a
  TCP host accepts anybody who can reach the port.
- **Prefer a unix socket** when both halves share a machine; the host chmods it
  to `0600`.
- **Use TLS across a network** (`-tls-cert` / `-tls-key`), or tunnel it over
  SSH / WireGuard / Tailscale. The token and the agent's entire tool traffic
  are otherwise in the clear.
- **`-allow-resume` is off by default.** Session IDs are addressable, so
  honoring a client-supplied one lets a client reattach to another's
  conversation.
- **Filesystem settings are loaded by default.** Pass `-setting-sources=` to
  load none, so the agent's containment does not depend on whatever dotfiles
  happen to exist in the sandbox.

## Gotcha: something else auto-approving your tool calls

If `CanUseTool` never fires, the SDK emits a warning naming the likely causes.
In order of likelihood:

1. A `permissions` block in a settings file the CLI loaded — pass
   `-setting-sources=` on the host.
2. `AllowedTools` naming a whole tool (`"Bash"` rather than `"Bash(ls:*)"`),
   which auto-approves before the callback runs.
3. The ambient environment. Some managed environments approve tool calls
   through policy the SDK cannot see.

A `PreToolUse` hook is consulted unconditionally and is the reliable way to
gate every call.

## Tests

```bash
go test ./...                    # unit + loopback, no credentials needed
SANDBOX_E2E=1 go test ./...      # adds a real claude session (spends tokens)
```

The default suite drives a fake CLI over a real socket. The gated test runs an
actual session and asserts a `PreToolUse` hook fires, which proves the control
protocol completes a callback round trip through the transport.

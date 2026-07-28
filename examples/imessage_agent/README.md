# imessage_agent — a self-modifying agent over iMessage

An agent that can rewrite its own skills, system prompt, memory, and source
code, reachable over iMessage, with its Claude running inside a sandbox.

> Not to be confused with [`../imessage_channel`](../imessage_channel), which
> is a thin auto-responder. This one has persistent state and edits itself.

```
┌────────────────────────┐        ┌──────────────────────────┐
│ this program (macOS)   │        │ sandbox                  │
│  • polls iMessages     │──────▶ │  • sandbox-host          │
│  • proxies brain tools │ socket │  • claude CLI            │
│  • drives claude.Client│        │  • brain HTTP server     │
└────────────────────────┘        └──────────────────────────┘
```

The program no longer builds or supervises a container — an earlier version
managed Podman directly. The sandbox and the brain are started by whoever owns
them; this process only connects. That split is what lets the thing holding
your Messages database be a different thing from the one running shell
commands.

## The brain

`brain/` is an HTTP server giving the agent tools over its own state:

- `read_file` / `write_file` / `list_dir` — its workspace
- `list_skills` / `read_skill` / `write_skill` / `delete_skill` — its skills
- `get_prompt` / `set_prompt` — its own system prompt
- `read_memory` / `update_memory` — long-term memory
- `save_conversation` / `update_summary` — conversation history
- `run_bash` — shell inside its container
- `read_source` / `write_source` / `rebuild_self` — its own source code
- `read_dockerfile` / `write_dockerfile` — its container image

These are exposed to Claude as an in-process MCP server. The tools run in
*this* process and call the brain over HTTP, so they are serviced through the
SDK's control protocol rather than inside the sandbox.

Because in-process MCP servers are registered with `--mcp-config` — a CLI flag
a custom transport never emits — the client declares them explicitly:

```go
start := sandbox.DefaultStartRequest()
start.SDKMCPServers = []string{brainServerName}
```

Without that the CLI never learns the brain tools exist and the agent runs
without them, silently.

## Running it

Requires macOS with Messages configured and Full Disk Access for your terminal
(System Settings → Privacy & Security → Full Disk Access).

Start the brain and a sandbox host wherever the agent should live, then:

```bash
go run . \
    -sandbox-address 127.0.0.1:8378 \
    -brain-url http://localhost:8377 \
    -trigger claude
```

Send yourself an iMessage containing the trigger word.

For a terminal chat instead of iMessage:

```bash
go run . -chat
```

## Flags

| Flag | Default | Meaning |
|---|---|---|
| `-sandbox-network` | `tcp` | `tcp` or `unix` |
| `-sandbox-address` | `127.0.0.1:8378` | Sandbox host address or socket path |
| `-brain-url` | `http://localhost:8377` | Brain server base URL |
| `-trigger` | `claude` | Only act on messages containing this word |
| `-chat` | `false` | Terminal chat TUI instead of iMessage |

`SANDBOX_TOKEN` authenticates to the sandbox host.

## Bounding it

The agent runs with whatever the sandbox host allows. Since it can edit its own
prompt and source, the host's policy is the only real limit — set
`-allowed-tools`, `-max-turns`, and `-cwd` there rather than expecting the
agent to restrain itself. See [`../sandbox`](../sandbox).

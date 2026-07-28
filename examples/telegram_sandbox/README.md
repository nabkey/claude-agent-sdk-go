# telegram_sandbox — drive a sandboxed Claude from your phone

A Telegram bot that runs a Claude Code session per chat, with the agent
executing inside a sandbox reached over a socket. Tool calls that need approval
arrive as inline buttons.

```
  phone                bot process                   sandbox
┌────────┐         ┌──────────────────┐        ┌──────────────────┐
│Telegram│ ──────▶ │ claude.Client    │ ─────▶ │ sandbox-host     │
│        │ ◀────── │ sandbox.Transport│ ◀───── │ claude CLI       │
└────────┘         └──────────────────┘        └──────────────────┘
```

The bot process never runs the CLI. It can live on a laptop, a VPS, or
anywhere else with outbound access to both Telegram and the sandbox.

## Running it

1. Create a bot with [@BotFather](https://t.me/botfather) and copy the token.
2. Get your numeric user ID from [@userinfobot](https://t.me/userinfobot).
3. Start a sandbox host where the agent should work:

```bash
cd ../sandbox
go run ./cmd/sandbox-host \
    -listen 127.0.0.1:8377 \
    -cwd /path/to/project \
    -setting-sources= \
    -bash-sandbox
```

4. Start the bot:

```bash
export TELEGRAM_BOT_TOKEN=123456:ABC...
go run . -users 12345678 -sandbox-address 127.0.0.1:8377
```

Message the bot. `/help` lists the commands.

## Commands

| Command | What it does |
|---|---|
| *(any text)* | Sent to Claude as a turn |
| `/new` | Start a fresh conversation |
| `/stop` | Interrupt the running turn |
| `/status` | What this chat is connected to |
| `/mode` | Permission mode: `default`, `acceptEdits`, `plan`, `bypassPermissions` |
| `/model` | List models, or `/model <name>` to switch |
| `/ctx` | Context window usage |
| `/usage` | Cost and rate limits |
| `/mcp` | MCP server status |

## Security

**This is a remote shell with a chat interface.** `-users` is mandatory and
the bot refuses to start without it: a bot token is reachable by anyone who
finds the bot, and without an allowlist they get your sandbox.

Containment is the sandbox host's job, not the bot's — see
[`../sandbox`](../sandbox). Useful host flags:

- `-permission-mode plan` — the agent proposes, you approve
- `-bash-sandbox` — isolate bash commands
- `-setting-sources=` — ignore filesystem settings, so approvals actually
  reach your phone rather than being auto-approved by a dotfile
- `-allowed-tools` — restrict the tool set outright

`/mode bypassPermissions` disables approval prompts for the session. The bot
warns when you set it.

## Implementation notes

Worth knowing if you adapt this:

- **One turn at a time per chat.** `claude.Client` carries a single
  conversation and `ReceiveResponse` runs until the turn's `ResultMessage`, so
  a second message mid-turn is refused rather than queued behind something the
  user can no longer see.
- **Edits are debounced.** Telegram rate limits edits to roughly one per
  second per chat, so the live view coalesces to one edit per 1.5s. Editing a
  message to its current text is an API error, so no-op edits are skipped.
- **Text is sent unformatted.** Claude's output is full of Markdown that
  Telegram's parsers reject — one unmatched underscore fails the whole send.
  A dropped message is worse than an unstyled one.
- **Approvals fail closed.** A prompt that cannot be delivered, times out, or
  is cancelled denies the call. A held approval holds the whole turn, so every
  path returns a decision.
- **Options live on the host.** `AllowedTools`, `PermissionMode`, `Cwd` and
  friends are CLI flags, which a custom transport never emits. Setting them in
  `AgentOptions` here would do nothing.

## Tests

```bash
go test ./...
```

Runs against a mock Bot API server — no token, no network. Covers the
allowlist, the approval round trip in both directions, the timeout, stale
button taps, command dispatch, message chunking, and the edit debounce.

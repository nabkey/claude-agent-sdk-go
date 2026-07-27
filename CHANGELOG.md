# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Fixed — wire-protocol correctness

Options in this section previously produced CLI arguments or control-protocol
payloads the CLI does not understand. They compiled and ran, but had no effect.
Verified against the Python (0.2.128) and TypeScript (0.3.220) Agent SDKs; see
[GAP_ANALYSIS.md](GAP_ANALYSIS.md).

- `Thinking` was serialized as JSON to `--thinking`. It now maps onto the flags
  the CLI actually accepts: `--thinking adaptive`, `--max-thinking-tokens N`,
  or `--thinking disabled`, plus `--thinking-display`.
- `Tools` presets were serialized as JSON. A preset now emits `--tools default`.
- `EnableFileCheckpointing` emitted a non-existent `--enable-file-checkpointing`
  flag. It now sets `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` in the
  subprocess environment. `Client.RewindFiles()` could not previously succeed.
- `AgentProgressSummaries` emitted a non-existent `--agent-progress-summaries`
  flag. It is now sent as a field on the `initialize` control request.
- `ToolConfig` emitted a non-existent `--tool-config` flag carrying a fabricated
  payload. See the breaking-changes section below.
- `Client.GetMCPStatus()` sent control subtype `get_mcp_status` (correct:
  `mcp_status`) and read the response key `servers` (correct: `mcpServers`). It
  now also populates `ServerInfo` and `Scope`.
- `Client.ReconnectMCPServer()` and `Client.ToggleMCPServer()` sent
  `server_name`; the wire protocol uses `serverName` for these two requests.
- `TaskUsage` had the wrong fields, so `TaskProgressMessage.Usage` was always
  zero. See the breaking-changes section below.
- Permission suggestions delivered to `CanUseTool` were decoded down to their
  `type` field, discarding `rules`, `behavior`, `mode`, `directories`, and
  `destination`. Echoing `ctx.Suggestions` back as `UpdatedPermissions` — the
  documented "always allow" pattern — therefore sent empty rule sets.
- The `sessions` package resolved project directories by replacing only path
  separators with hyphens. The CLI replaces **every** non-alphanumeric
  character, so any project path containing a `.`, `_`, or space resolved to a
  directory that does not exist and every `sessions` function silently returned
  nothing. Long-path hashing now matches the CLI's base36 algorithm,
  `CLAUDE_CONFIG_DIR` is honored, and a prefix-scan fallback handles long-path
  hash mismatches.

### Security

- `--resume` is now passed as `--resume=<value>`. The CLI declares `--resume`
  with an optional value, so in the two-token form a dash-leading value was not
  bound to the flag and was parsed as a separate flag instead — letting an
  untrusted session title inject arbitrary CLI flags. `ExtraArgs` values
  beginning with `-` use the same form.
- On Windows, the SDK now refuses to spawn a `.bat`/`.cmd` CLI path
  (CVE-2024-27980 "BatBadBut" class), and rejects cmd.exe metacharacters in
  `Resume` values. Both are no-ops on other platforms.

### Changed — behavior

- **`SettingSources` default.** The SDK previously always emitted
  `--setting-sources ""`, which disabled *all* filesystem settings — so
  `CLAUDE.md`, project settings, and skill discovery never loaded unless you
  set the option explicitly. A nil `SettingSources` now omits the flag, matching
  the CLI default of loading all sources, as both reference SDKs do. To keep
  the old isolation behavior, pass an empty non-nil slice:
  `SettingSources: []types.SettingSource{}`.
- `FallbackModel` equal to `Model` is now rejected at construction.
- `ExtraArgs` are emitted in sorted order so argv is deterministic.

### Breaking

- `types.ToolConfiguration` (fields `Enabled`, `MaxConcurrency`, `Timeout`) is
  replaced by `types.ToolConfig`. Those fields did not exist in the protocol.
  `AgentOptions.ToolConfig` changes from `map[string]types.ToolConfiguration`
  to `*types.ToolConfig`, and `WithToolConfig(name, config)` becomes
  `WithToolConfig(config)`. New helper:
  `WithAskUserQuestionPreviewFormat(types.PreviewFormatHTML)`.
- `types.TaskUsage` fields `InputTokens`, `OutputTokens`,
  `CacheCreationInputTokens`, `CacheReadInputTokens` are replaced by
  `TotalTokens`, `ToolUses`, `DurationMS` to match the wire format.
- `types.ThinkingConfigEnabled.BudgetTokens` changes from `int` to `*int`, so
  "no budget" (which the CLI treats as adaptive) is distinguishable from zero.
  Prefer the new constructors: `types.NewThinkingAdaptive()`,
  `types.NewThinkingEnabled(n)`, `types.NewThinkingDisabled()`.
- `types.ThinkingConfig` gains `ThinkingType()` and `DisplayMode()` methods.
  External implementations of the interface must add them.

### Added

- `types.ThinkingDisplay` (`summarized` / `omitted`) with a `Display` field on
  the adaptive and enabled thinking configs.
- `types.PermissionUpdateFromMap` and `types.PermissionUpdatesFromAny`, the
  inverse of `PermissionUpdate.ToMap`, for decoding permission suggestions.
- SDK MCP server names are now declared on the `initialize` request
  (`sdkMcpServers`).
- Golden-argv and control-protocol payload tests, plus a scriptable fake
  transport, covering every option above.
- `PersistSession` option to control session persistence
- `AgentProgressSummaries` option for AI-generated subagent progress summaries
- `ToolConfig` option for per-tool configuration of built-in tools
- `GetSessionInfo()` function for retrieving single session metadata
- `ForkSession()` standalone function for branching conversations
- `SupportedAgents()` function for discovering available agents at runtime
- GitHub Actions CI/CD pipeline with test, vet, race detection, and linting
- golangci-lint configuration
- Rate limit overage fields on `RateLimitInfo`: `OverageStatus`, `OverageResetsAt`, `OverageDisabledReason`, `Raw`
- `RateLimitType` enum values: `seven_day_opus`, `seven_day_sonnet`, `overage`
- `McpServerConnectionStatus` enum values: `needs-auth`, `disabled`
- `McpServerInfo` type and `ServerInfo`/`Scope` fields on `McpServerStatus`
- `SessionID`, `ToolUseID`, `TaskType` fields on `TaskStartedMessage`
- `Description`, `UUID`, `SessionID`, `ToolUseID`, `LastToolName` fields on `TaskProgressMessage`
- `OutputFile`, `Summary`, `UUID`, `SessionID`, `Usage` fields on `TaskNotificationMessage`
- Rate limit event parser support for both flat and nested (`rate_limit_info`) wire formats with camelCase/snake_case compatibility

## [0.1.0] - 2025-05-01

### Added
- `Query()`, `QuerySync()`, `QueryText()` for one-shot queries
- `Client` for bidirectional, interactive streaming conversations
- MCP server support (SDK in-process, stdio, HTTP, SSE)
- Hook system for intercepting tool use, permissions, and lifecycle events
- `CanUseTool` callback for custom permission logic
- Session management: list, read, rename, tag sessions
- Extended thinking configuration (adaptive, enabled, disabled)
- Effort level configuration
- Structured output via JSON schema
- File checkpointing and rewind support
- Custom agent definitions
- Plugin support
- Sandbox settings for bash command isolation
- Rate limit event handling

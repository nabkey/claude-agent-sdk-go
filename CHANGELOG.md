# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added — streaming transport, control surface, and session store

This completes the six-phase plan in [GAP_ANALYSIS.md](GAP_ANALYSIS.md),
bringing the SDK to parity with the Python (0.2.128) and TypeScript (0.3.220)
Agent SDKs.

**Transport and lifecycle**

- `Query()` now runs the CLI in streaming mode, as both reference SDKs do. It
  previously used `--print`, which cannot carry the control protocol, so
  `Hooks`, `CanUseTool`, and in-process SDK MCP servers were accepted and then
  silently ignored. They now work in `Query()` as well as `Client`.
- `Transport` is now a public interface, and `QueryWithTransport` /
  `NewClientWithTransport` accept an implementation. This allows running the
  CLI in a container, VM, or remote worker, and makes the SDK testable without
  a `claude` binary.
- Stdout framing was rewritten. Lines are framed on `\n` and parsed
  independently; a stray non-JSON line (some CLI builds write `[SandboxDebug]`
  to stdout) previously poisoned every subsequent message.
- Subprocess teardown is now staged — stdin close, then SIGTERM, then SIGKILL,
  with a grace period at each step. Killing outright interrupted the CLI's
  session-file flush and lost the last assistant message.
- Live subprocesses are terminated on SIGINT/SIGTERM/SIGHUP, so a parent that
  exits without `Close()` no longer leaks orphaned `claude` processes.
- Transport errors now reach the caller. `Query()` emits them on its channel
  and `Client.Err()` reports them; previously the stream just closed silently.
  A `ProcessError` following an error result is replaced with the structured
  error the CLI reported, instead of a bare "exit code 1".
- `control_cancel_request` is implemented: a cancelled handler is abandoned and
  no stale response is written.
- Stdin stays open past a `result` frame while delegated agent work is still in
  flight, since background subagents still need the control channel for hook
  and SDK-MCP responses.
- `Client.ReceiveMessages` and `ReceiveResponse` no longer race; the stream has
  a single consumer and misuse is now visible.
- Environment: `CLAUDE_AGENT_SDK_VERSION` is set, and `CLAUDECODE` is filtered
  from the inherited environment so the subprocess does not believe it is
  nested inside Claude Code.

**Initialize handshake**

Agents, system prompts, and much of the session configuration now travel on the
`initialize` control request rather than as CLI flags, matching both reference
SDKs. New fields: `agents`, `systemPrompt`, `appendSystemPrompt`,
`excludeDynamicSections`, `title`, `skills`, `toolAliases`,
`planModeInstructions`, `jsonSchema`, `promptSuggestions`,
`forwardSubagentText`, `supportedDialogKinds`, `sdkMcpServers`.

**Options**

`Agent`, `Skills`, `TaskBudget`, `SessionID`, `ResumeSessionAt`,
`StrictMCPConfig`, `IncludeHookEvents`, `AllowDangerouslySkipPermissions`,
`ManagedSettings`, `Title`, `ToolAliases`, `PlanModeInstructions`,
`PromptSuggestions`, `ForwardSubagentText`, `SupportedDialogKinds`,
`OnElicitation`, `OnUserDialog`, `Debug`, `DebugFile`, `SessionStore`,
`SessionStoreFlush`, `LoadTimeoutMS`, `Warn`. `SystemPrompt` additionally
accepts `[]string` (with `types.SystemPromptDynamicBoundary`) and
`*types.SystemPromptFile`.

**Control methods on `Client`**

`GetContextUsage`, `GetSessionUsage`, `SetMaxThinkingTokens`,
`ApplyFlagSettings`, `SetMCPServers`, `SetMCPPermissionModeOverride`,
`ReloadPlugins`, `ReloadSkills`, `ReadFile`, `SeedReadState`,
`BackgroundTasks`, `InitializationResult`, `Reinitialize`,
`SupportedCommands`, `SupportedModels`, `SupportedAgents`, `AccountInfo`,
`SendMessage`, `StreamInput`, `PreviewRewindFiles`, `Err`.

`RewindFiles` now returns `*types.RewindFilesResult` rather than only an error,
and `InterruptWithOptions` returns the interrupt receipt.

**Types**

- `ServerToolUseBlock` and `ServerToolResultBlock` — server-side tool calls
  (web search, advisor) were previously dropped from message content.
- `ResultMessage` gains `ModelUsage`, `PermissionDenials`, `DeferredToolUse`,
  `Errors`, `APIErrorStatus`, `UUID`, and `TerminalReason`.
- `AssistantMessage` gains `MessageID`, `StopReason`, `SessionID`, `UUID`, and
  now actually parses `Error`.
- New messages: `TaskUpdatedMessage`, `HookEventMessage`, `MirrorErrorMessage`,
  `CompactBoundaryMessage`, `SessionStateChangedMessage`,
  `PermissionDeniedMessage`, `APIRetryMessage`, `StatusMessage`,
  `ToolProgressMessage`, `BackgroundTasksChangedMessage`,
  `PromptSuggestionMessage`.
- All 31 hook events (was 10), plus `agent_id`, `agent_type`, `prompt_id` and
  `effort` on `BaseHookInput`.
- `ToolPermissionContext` gains `ToolUseID`, `AgentID`, `BlockedPath`,
  `DecisionReason`, `Title`, `DisplayName`, `Description`.
- `AgentDefinition` gains `DisallowedTools`, `InitialPrompt`, `MaxTurns`,
  `Background`, `Effort`, `PermissionMode`, `Observer`, `ObserverMessage`.
- Enum values: `PermissionMode` `dontAsk`/`auto`, `EffortLevel` `xhigh`,
  `PermissionUpdateDestination` `cliArg`, four more `AssistantMessageError`
  values, and `RateLimitType` `seven_day_overage_included`.
- Typed control responses: `InitializeResult`, `ContextUsage`,
  `RewindFilesResult`, `InterruptResult`, `SlashCommand`, `ModelInfo`,
  `AgentInfo`, `AccountInfo`, `MCPSetServersResult`, `SessionUsage`.

**Session store**

New `SessionStore` interface mirroring transcripts to external storage, with
`SessionLister`, `SessionSummarizer`, `SessionDeleter` and
`SessionSubkeyLister` as optional capabilities. Includes
`InMemorySessionStore`, `FoldSessionSummary`, `ProjectKeyForDirectory`,
transcript-mirror batching with retry, and the `*FromStore` /
`DeleteSessionViaStore` / `ImportSessionToStore` functions.

**Sessions**

- `GetSessionMessages` now reconstructs the conversation chain by walking
  `parentUuid` links, filtering sidechain and internal entries, instead of
  returning every raw JSONL line.
- New: `ListSubagents`, `GetSubagentMessages`, `DeleteSession`, and a
  `ForkSession` that rewrites the transcript offline.
- `SDKSessionInfo` gains `Summary`, `LastModified`, `FileSize`, `CustomTitle`.

**MCP**

- `mcp.NewToolFor` generates a tool's JSON Schema from a Go struct and decodes
  the handler's argument, replacing hand-written schemas and `map[string]any`
  unpacking.
- Typed `mcp.ToolAnnotations`, `MaxResultSizeChars` via `_meta`, and
  `ResourceLinkResult` / `EmbeddedResourceResult` / `AudioResult`.

**Diagnostics**

- A warning is emitted when `CanUseTool` is set alongside options that shadow
  it (`bypassPermissions`, or an `AllowedTools` entry allowing a whole tool) —
  the most common reason a permission callback appears never to fire. Route it
  with `AgentOptions.Warn`.

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

Three control-response decoders were written against the reference SDKs'
documented shapes rather than the wire, and returned zero values against a real
CLI. Corrected against captures from CLI 2.1.220, with those captures kept as
fixtures in `types/responses_test.go`:

- `Client.SupportedModels()` read the selector from `model`, which the CLI does
  not send; it sends `value`, plus `resolvedModel` for the concrete model.
  `ModelInfo.Model` was therefore always empty, leaving no identifier to pass
  back to `SetModel`. The legacy `model` and `name` keys remain accepted.
- `Client.ReadFile()` read `content`; the CLI sends `contents`, alongside the
  resolved `absPath`. `ReadFileResult.Content` was therefore always empty.
- `Client.GetSessionUsage()` read `totalCostUSD` at the top level, decoded
  `rate_limits` as an array, and expected epoch-number timestamps. The CLI
  nests cost under `session.total_cost_usd`, keys `rate_limits` as an object of
  named windows, and renders resets as RFC 3339 strings. Cost was therefore
  always `0` and `RateLimits` always empty. Both the object and the legacy
  array shape are now accepted.

- A `CanUseTool` callback that is never consulted now produces a warning at the
  end of the turn, reporting how many tool calls were approved without it. The
  existing check could only inspect `AgentOptions`, but permission modes and
  allow rules in settings files shadow the callback just as effectively — and
  since a nil `SettingSources` now loads every filesystem settings file, that
  became the common case rather than an exotic one. A user-level
  `"defaultMode": "auto"` silently disabled the callback with nothing to show
  for it, which matters because `CanUseTool` is frequently used as a security
  gate. Watching what actually happens catches every shadowing source —
  including sandboxes and the surrounding environment — without reimplementing
  the CLI's settings precedence. The static warning now also states that it
  covers `AgentOptions` only.

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

- `ForkSession` moved from the root package to `sessions.ForkSession` and
  returns `*sessions.ForkSessionResult`. It now rewrites the transcript offline
  instead of spawning a billed CLI query.
- `SupportedAgents` moved from a package-level function that screen-scraped
  `claude agents` output to `Client.SupportedAgents()`, which reads the
  structured initialize response.
- `Client.RewindFiles` returns `(*types.RewindFilesResult, error)`.
- `types.SessionMessage` gains `UUID`, `SessionID`, `Message` and
  `ParentToolUseID`; `Data` now holds the raw transcript line.
- `types.SDKSessionInfo` gains `Summary`, `LastModified`, `FileSize` and
  `CustomTitle`.
- `types.RateLimitInfo.ResetsAt` changes from `*string` to `*int64`, matching
  the wire format's Unix timestamp.
- `types.AgentDefinition.Memory` changes from `*string` to
  `*types.AgentMemoryScope`.
- `mcp.Tool.Annotations` changes from `map[string]any` to
  `*mcp.ToolAnnotations`.
- `types.ToolConfiguration` (fields `Enabled`, `MaxConcurrency`, `Timeout`) is
  replaced by `types.ToolConfig`. Those fields did not exist in the protocol.
  `AgentOptions.ToolConfig` changes from `map[string]types.ToolConfiguration`
  to `*types.ToolConfig`, and `WithToolConfig(name, config)` becomes
  `WithToolConfig(config)`. New helper:
  `WithAskUserQuestionPreviewFormat(types.PreviewFormatHTML)`.
- `types.TaskUsage` fields `InputTokens`, `OutputTokens`,
  `CacheCreationInputTokens`, `CacheReadInputTokens` are replaced by
  `TotalTokens`, `ToolUses`, `DurationMS` to match the wire format.
- `types.ReadFileResult` drops `Encoding`, `Truncated` and `Size`, and gains
  `AbsPath`. The CLI sends only the contents and the resolved path; as of
  2.1.220 it ignores the `maxBytes` hint and returns the whole file, so nothing
  reported truncation.
- `types.ModelInfo` gains `ResolvedModel`. `Model` now carries the CLI's
  selector (`default`, `opus[1m]`), which may be an alias rather than a
  concrete model ID.
- `types.SessionUsage` gains `SubscriptionType`. `RateLimits` is now ordered by
  window name, and lists only the populated windows — the CLI's `rate_limits`
  object is mostly null placeholders and carries non-window siblings
  (`limits`, `spend`, `extra_usage`), which stay reachable via `Raw`.
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

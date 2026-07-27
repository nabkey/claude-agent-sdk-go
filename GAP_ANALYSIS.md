# Gap Analysis: Go SDK vs. Python & TypeScript Agent SDKs

**Date:** 2026-07-27
**Go SDK:** `github.com/nabkey/claude-agent-sdk-go` @ `6e3cb20` (~6.6k LOC)
**Reference — Python:** `claude-agent-sdk` **0.2.128** (~11.3k LOC)
**Reference — TypeScript:** `@anthropic-ai/claude-agent-sdk` **0.3.220** (bundles Claude Code CLI 2.1.220; ~11.4k LOC of `.d.ts` alone)

> **Status: Phase 1 is complete** (see §12). Every wire-protocol divergence in
> §1 has been fixed and is covered by golden-argv / control-payload tests, and
> the Windows hardening from the cross-cutting section has landed. §2–§11
> describe the remaining gaps and are unchanged. Items below that are done are
> marked **[FIXED]**.

Method: both reference SDKs were downloaded and read in full — Python source (`types.py`, `client.py`,
`_internal/{query,message_parser,transport/subprocess_cli,sessions,session_*}.py`), TypeScript type
declarations (`sdk.d.ts`), and the TypeScript **runtime bundle** (`sdk.mjs`) to confirm the actual CLI
argv and control-protocol wire format rather than inferring it from docs.

---

## §0. Executive summary

The Go SDK is a faithful port of a **much older** Python SDK (roughly the 0.0.2x line). Against today's
SDKs it is behind in three distinct ways, in descending order of severity:

1. **Wire-protocol divergences (§1).** Eleven places where Go sends something the CLI does not
   understand, or reads a field the CLI does not send. These are not "missing features" — they are
   features that *appear* to be implemented and silently do nothing. `Thinking`, `Tools` preset,
   `EnableFileCheckpointing`, `ToolConfig`, `AgentProgressSummaries`, `GetMCPStatus`,
   `ReconnectMCPServer`, `ToggleMCPServer`, `TaskUsage`, `sessions.*`, and permission-suggestion
   round-tripping are all affected.
2. **An architectural fork (§2).** `Query()` runs the CLI in `--print` mode. Both reference SDKs
   abandoned that years ago and now run *everything* through `--input-format stream-json`. Because of
   this, `Query()` in Go cannot use hooks, `CanUseTool`, or SDK MCP servers, and `Agents`/system-prompt
   config can't move to the `initialize` handshake.
3. **Surface area (§3–§8).** ~45 options, ~17 client methods, 21 hook events, ~30 message types, the
   entire `SessionStore` subsystem, and pluggable transports are absent.

Nothing here requires a rewrite. §12 lays out a 6-phase plan; **Phase 1 alone (≈2 days) fixes every
silent-failure bug** and is by far the highest value-per-hour work in this document.

---

## §1. Wire-protocol divergences — silent failures  **[ALL FIXED]**

These are ordered by blast radius. Each was verified against the TypeScript runtime bundle
(`sdk.mjs`) and/or the Python transport, not just type declarations.

### 1.1 `Thinking` is serialized as JSON — the CLI expects a keyword **[FIXED]**

`internal/transport/subprocess.go:400`

```go
thinkingJSON, _ := json.Marshal(opts.Thinking)
cmd = append(cmd, "--thinking", string(thinkingJSON))   // --thinking {"type":"adaptive"}
```

Both reference SDKs map the config onto **two different flags**:

| `ThinkingConfig`                        | Correct argv                       |
|-----------------------------------------|------------------------------------|
| `{type: adaptive}`                       | `--thinking adaptive`              |
| `{type: enabled, budget_tokens: N}`      | `--max-thinking-tokens N`          |
| `{type: enabled}` (no budget)            | `--thinking adaptive`              |
| `{type: disabled}`                       | `--thinking disabled`              |
| any + `display`                          | `+ --thinking-display summarized\|omitted` |

Go also has no `display` field (`ThinkingDisplay = 'summarized' | 'omitted'`), which on Opus 4.7+
is what controls whether thinking text is returned at all.

**Impact:** `WithThinking(...)` is inert or errors out. `examples/thinking_config` demonstrates a
broken feature.

### 1.2 `--tools` preset is serialized as JSON — the CLI expects `default` **[FIXED]**

`internal/transport/subprocess.go:237`. TS: `H.push("--tools","default")`. Go emits
`--tools {"type":"preset","preset":"claude_code"}`.

### 1.3 `EnableFileCheckpointing` uses a flag that does not exist **[FIXED]**

Go emits `--enable-file-checkpointing` (`subprocess.go:409`). The string does not appear anywhere in
the TS bundle. Both reference SDKs set an **environment variable**:

```
CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true
```

**Impact:** checkpointing never turns on, so `Client.RewindFiles()` can never succeed. Depending on
CLI version, an unknown flag may also abort startup.

### 1.4 `ToolConfig` is a fabricated type on a fabricated flag **[FIXED]**

Go emits `--tool-config <json>` with a hand-rolled
`ToolConfiguration{Enabled, MaxConcurrency, Timeout}`. Neither the flag nor those fields exist. The
real `ToolConfig` is:

```ts
type ToolConfig = { askUserQuestion?: { previewFormat?: 'markdown' | 'html' } }
```

…and it is delivered as `CLAUDE_CODE_QUESTION_PREVIEW_FORMAT` in the subprocess env.

### 1.5 `AgentProgressSummaries` uses a flag that does not exist **[FIXED]**

Go emits `--agent-progress-summaries`. In both reference SDKs this is a field on the **`initialize`
control request** (`agentProgressSummaries: boolean`), alongside `promptSuggestions` and
`forwardSubagentText`.

### 1.6 `GetMCPStatus` sends the wrong subtype and reads the wrong key **[FIXED]**

`internal/protocol/query.go:569,577`

| | Go | Correct |
|---|---|---|
| request subtype | `get_mcp_status` | `mcp_status` |
| response key | `servers` | `mcpServers` |

Go also never populates `ServerInfo` or `Scope` even though the fields exist on `McpServerStatus`.

**Impact:** `GetMCPStatus()` errors, or returns an empty server list. `examples/mcp_status` is broken.

### 1.7 `ReconnectMCPServer` / `ToggleMCPServer` use snake_case; the wire is camelCase **[FIXED]**

`internal/protocol/query.go:626,635` send `"server_name"`. Python, TS `.d.ts`, and the TS bundle all
send **`"serverName"`** for `mcp_reconnect` and `mcp_toggle`. (Note the inconsistency is real and
deliberate on the CLI side — `stop_task` really is `task_id`, `rewind_files` really is
`user_message_id`.)

### 1.8 `TaskUsage` has the wrong shape **[FIXED]**

`types/types.go:318`

```go
// Go — these fields are never present on the wire
type TaskUsage struct { InputTokens, OutputTokens, CacheCreationInputTokens, CacheReadInputTokens int }
```

```ts
// actual wire shape on task_progress / task_notification
usage: { total_tokens: number; tool_uses: number; duration_ms: number }
```

**Impact:** `TaskProgressMessage.Usage` is always the zero value.

### 1.9 Session directory sanitization does not match the CLI **[FIXED]**

`sessions/sessions.go:155`. Go replaces only `filepath.Separator` with `-` and, past 200 chars,
appends a SHA-256 prefix. The CLI (and both SDKs) replace **every non-alphanumeric character** with
`-`, and past 200 chars append a **djb2-style 32-bit hash rendered in base36**.

```
/home/user/my.project
  Go:      home-user-my.project      ← directory does not exist
  Correct: home-user-my-project
```

Go also ignores `CLAUDE_CONFIG_DIR`, and does not fall back to prefix-scanning when the long-path
hash disagrees (the reference SDKs do, because the CLI hashes with Bun's hash and the SDK does not).

**Impact:** the entire `sessions` package silently returns empty for any project path containing a
`.`, `_`, space, or other punctuation — i.e. most real projects. `RenameSession`/`TagSession` return
"session not found".

### 1.10 Permission suggestions are decoded down to their `type` field **[FIXED]**

`internal/protocol/query.go:365-374` and `parser.go` build
`PermissionUpdate{Type: ...}` and **drop `rules`, `behavior`, `mode`, `directories`, `destination`**.
The documented pattern — surface an "always allow" affordance by echoing
`ctx.Suggestions` back as `PermissionResultAllow.UpdatedPermissions` — therefore sends empty rule
sets. Python has a `PermissionUpdate.from_dict()` inverse of `to_dict()`; Go has no inverse.

### 1.11 `--resume` and dash-leading `extra_args` are injection-prone **[FIXED]**

Go emits `--resume <value>` as two argv tokens (`subprocess.go:287`). The CLI declares `--resume`
with an *optional* value, so a dash-leading value is parsed as a separate flag rather than bound to
`--resume`. Both reference SDKs switched to `--resume=<value>` specifically to close this, and apply
the same `=` form to any `extra_args` value starting with `-`. Session titles are a documented
`--resume` input and are frequently attacker-influenced.

Related, also absent in Go: the Windows `.bat`/`.cmd` spawn refusal (CVE-2024-27980 class) and the
cmd.exe metacharacter rejection for `resume`/`session_id`.

### 1.12 Two smaller ones **[FIXED]**

- **`--setting-sources` is always emitted, even when unset** (`subprocess.go:378`), so the Go default
  is `--setting-sources ""` = *no filesystem settings at all*, whereas both reference SDKs omit the
  flag and let the CLI load all sources. Silently different default behavior (no `CLAUDE.md`, no
  settings, no skills discovery). Both SDKs also use the `=` form.
- **`--fallback-model` equal to `--model` is not rejected.** TS throws; the CLI misbehaves.

---

## §2. The architectural fork: `--print` vs. always-streaming

`query.go` builds `--print -- <prompt>` for one-shot queries and only uses
`--input-format stream-json` for `Client`. Both reference SDKs deleted the non-streaming path:

```js
// sdk.mjs — unconditional
let H = ["--output-format","stream-json","--verbose","--input-format","stream-json"]
```
```python
# subprocess_cli.py
self._is_streaming = True   # Always use streaming mode internally (matching TypeScript SDK)
```

The reason is stated in the Python source: *"This allows agents and other large configs to be sent
via initialize request."* Consequences for Go today:

| Feature | `claude.Query()` | `claude.Client` |
|---|---|---|
| Hooks | ✗ silently ignored | ✓ |
| `CanUseTool` | ✗ silently ignored | ✓ |
| SDK MCP servers | ✗ tools never resolve | ✓ |
| `Interrupt` / any control request | ✗ | ✓ |
| Agents via `initialize` | ✗ | ✗ (Go uses the `--agents` flag) |

`Query()` accepts `AgentOptions.Hooks`, `.CanUseTool`, and SDK MCP servers without complaint and
then ignores them — the same silent-failure category as §1.

**Downstream:** Go cannot move `agents`, `systemPrompt`, `appendSystemPrompt`, `title`, `skills`,
`toolAliases`, `planModeInstructions`, `excludeDynamicSections`, `sdkMcpServers`, `jsonSchema`,
`promptSuggestions`, `agentProgressSummaries`, `forwardSubagentText`, or `supportedDialogKinds` into
the `initialize` request — which is where the reference SDKs put all of them, and the only place
several are accepted at all. Go's `initialize` sends `hooks` and nothing else.

---

## §3. Missing options

Go's `AgentOptions` has 42 fields. Below is everything in Python's `ClaudeAgentOptions` or TS's
`Options` that has no Go equivalent.

### 3.1 In **both** Python and TypeScript (highest priority)

| Option | Wire | Notes |
|---|---|---|
| `strict_mcp_config` | `--strict-mcp-config` | Ignore `.mcp.json`, settings, plugin MCP |
| `session_id` | `--session-id=` | Pin a UUID for a new session |
| `skills` (`[]string \| "all"`) | `initialize.skills` + `--allowedTools Skill(...)` | Also auto-defaults `setting_sources` to `user,project` |
| `include_hook_events` | `--include-hook-events` | Surfaces `hook_started`/`hook_response` in the stream |
| `session_store` | `--session-mirror` | See §7 |
| `session_store_flush` | — | `batched` \| `eager` |
| `load_timeout_ms` | — | Bounds `SessionStore.load()` during resume |
| `task_budget` | `--task-budget N` | Model-visible token budget |
| `system_prompt` as `{type:"file", path}` | `--system-prompt-file` | Python only, but a real CLI flag |
| `system_prompt` preset `exclude_dynamic_sections` | `initialize.excludeDynamicSections` | Cross-user prompt-cache hits |
| custom `Transport` injection | — | See §9.1 |

### 3.2 TypeScript-only

| Option | Wire | Notes |
|---|---|---|
| `agent` | `--agent <name>` | Run the **main thread** as a named agent |
| `toolAliases` | `initialize.toolAliases` | Redirect `Bash` → `mcp__workspace__bash` etc. |
| `allowDangerouslySkipPermissions` | `--allow-dangerously-skip-permissions` | Now **required** with `bypassPermissions` |
| `resumeSessionAt` | `--resume-session-at=` | Resume up to a specific message UUID |
| `managedSettings` | `--managed-settings` | Policy-tier settings from the embedder |
| `settings` as an object | `--settings <json>` | Go accepts only `*string` |
| `debug` / `debugFile` | `--debug` / `--debug-file` | |
| `title` | `initialize.title` | Custom title for a new session |
| `planModeInstructions` | `initialize.planModeInstructions` | Custom plan-mode workflow body |
| `promptSuggestions` | `initialize.promptSuggestions` | |
| `forwardSubagentText` | `initialize.forwardSubagentText` | Full nested subagent transcript |
| `onElicitation` | control req | MCP elicitation callback |
| `onUserDialog` + `supportedDialogKinds` | control req | Host-rendered blocking dialogs |
| `spawnClaudeCodeProcess` | — | Run the CLI in a VM/container/remote |
| `executable` / `executableArgs` | — | Node/Bun/Deno selection (N/A for Go) |
| `plugins[].skipMcpDiscovery` | `--plugin-dir-no-mcp` | |
| `systemPrompt` as `string[]` + `SYSTEM_PROMPT_DYNAMIC_BOUNDARY` | `initialize.systemPrompt` | Cache-boundary marker |

### 3.3 Enum values missing from existing Go options

- `PermissionMode`: missing **`dontAsk`**, **`auto`**
- `EffortLevel`: missing **`xhigh`**
- `PermissionUpdateDestination`: missing **`cliArg`**
- `RateLimitType`: missing **`seven_day_overage_included`**
- `AssistantMessageError`: missing `oauth_org_not_allowed`, `overloaded`, `model_not_found`,
  `max_output_tokens`
- `HookPermissionDecision`: missing **`defer`** (Go's doc comment says `allow, deny, ask`)
- `SandboxNetworkConfig`: missing `allowedDomains`, `deniedDomains`, `allowManagedDomainsOnly`,
  `allowMachLookup`; TS additionally has `sandbox.filesystem`, `sandbox.credentials`,
  `sandbox.failIfUnavailable`
- `AgentDefinition`: missing `disallowedTools`, `initialPrompt`, `maxTurns`, `background`, `effort`,
  `permissionMode`, `observer`, `observerMessage`, `criticalSystemReminder_EXPERIMENTAL`

---

## §4. Missing client / control-protocol methods

Go's `Client` exposes 9 control methods. TS's `Query` exposes 26.

### 4.1 Present in Go
`Interrupt`, `SetPermissionMode`, `SetModel`, `StopTask`, `RewindFiles`, `GetMCPStatus` (broken, §1.6),
`ReconnectMCPServer` (broken, §1.7), `ToggleMCPServer` (broken, §1.7), `GetServerInfo`.

### 4.2 Missing

| Method | Subtype | Py | TS |
|---|---|:--:|:--:|
| `getContextUsage()` | `get_context_usage` | ✓ | ✓ |
| `supportedCommands()` | from `initialize` | | ✓ |
| `supportedModels()` | `list_models` | | ✓ |
| `supportedAgents()` | from `initialize` | | ✓ |
| `accountInfo()` | from `initialize` | | ✓ |
| `initializationResult()` / `reinitialize()` | `initialize` | | ✓ |
| `setMaxThinkingTokens(n, display)` | `set_max_thinking_tokens` | | ✓ |
| `applyFlagSettings(settings)` | `apply_flag_settings` | | ✓ |
| `setMcpServers(servers)` | `mcp_set_servers` | | ✓ |
| `setMcpPermissionModeOverride(name, mode)` | | | ✓ |
| `reloadPlugins()` | `reload_plugins` | | ✓ |
| `reloadSkills()` | `reload_skills` | | ✓ |
| `readFile(path, opts)` | `read_file` | | ✓ |
| `seedReadState(path, mtime)` | `seed_read_state` | | ✓ |
| `backgroundTasks(toolUseId?)` | `background_tasks` | | ✓ |
| `usage_EXPERIMENTAL…()` | `get_usage` | | ✓ |
| `streamInput(stream)` | — | ✓ | ✓ |

### 4.3 Signature regressions in methods Go *does* have

- **`RewindFiles`** returns only `error`. TS returns `RewindFilesResult{canRewind, error, filesChanged,
  insertions, deletions, skippedLinks}` and accepts `{dryRun: bool}` — so a Go caller cannot preview a
  rewind or learn what changed.
- **`Interrupt`** returns only `error`. TS returns `SDKControlInterruptResponse{still_queued[],
  cancelled[]}` and accepts `cancel_queued`. Without it a Go caller cannot know which queued messages
  survived an interrupt.
- **`SendQuery(ctx, string)`** only accepts a plain string. Both reference SDKs accept full message
  objects, which is the only way to send **image content blocks**, set `parent_tool_use_id`, or
  address a specific `session_id`.
- **`SupportedAgents()`** (`agents.go`) shells out to `claude agents` and **screen-scrapes the
  human-readable output**, splitting on a U+00B7 middle dot. TS reads `agents` off the structured
  `initialize` response. This will break on any output-format change and does not work at all while a
  session is live.
- **`GetServerInfo()`** returns `map[string]any` rather than a typed `SDKControlInitializeResponse`
  (`commands`, `agents`, `models`, `account`, `output_style`, `available_output_styles`,
  `fast_mode_state`, …).

---

## §5. Missing hook events and hook I/O

### 5.1 Events

Go implements the same 10 events as Python. TypeScript defines **31**. Missing (21):

`PostToolBatch`, `UserPromptExpansion`, `SessionStart`, `SessionEnd`, `StopFailure`, `PostCompact`,
`PermissionDenied`, `Setup`, `TeammateIdle`, `TaskCreated`, `TaskCompleted`, `Elicitation`,
`ElicitationResult`, `ConfigChange`, `WorktreeCreate`, `WorktreeRemove`, `InstructionsLoaded`,
`CwdChanged`, `FileChanged`, `DirectoryAdded`, `MessageDisplay`.

`SessionStart` and `SessionEnd` in particular are common in real hook configs.

### 5.2 `BaseHookInput` fields

Missing on Go's `BaseHookInput`: `prompt_id` (joins hook output to OTel events at prompt grain),
`agent_id`, `agent_type` (Go bolts these onto four subtypes individually instead of the base), and
`effort.level`.

### 5.3 Hook-specific output

Go implements 3 of TS's ~20 `HookSpecificOutput` variants (`PreToolUse`, `PostToolUse`,
`UserPromptSubmit`). Notably:

- `PreToolUseHookSpecificOutput.permissionDecision` is missing **`defer`** — the mechanism behind
  `ResultMessage.deferred_tool_use`.
- `PostToolUseHookSpecificOutput` has only `updatedMcpToolOutput`. The general
  **`updatedToolOutput`** (works for all tools, not just MCP) is missing and is the preferred field.
- Missing entirely: `PostToolUseFailure`, `SessionStart`, `Notification`, `SubagentStart`,
  `SubagentStop`, `Stop`, `PermissionRequest`, `CwdChanged`, `FileChanged`, `WorktreeCreate`,
  `MessageDisplay`, `UserPromptExpansion`, `Elicitation`, `ElicitationResult`, `Setup`,
  `PermissionDenied`, `PostToolBatch` outputs.

### 5.4 Typed-input bugs

`types/hooks.go:100` — `PostToolUseFailureHookInput.ToolInput` is declared `string`. It is
`dict[str, Any]` / `Record<string, unknown>` on the wire. The same struct is also missing
`tool_use_id`.

### 5.5 `ToolPermissionContext`

Go has `Signal` (unused) and `Suggestions` (lossy, §1.10). Missing every other field the reference
SDKs deliver, all of which exist specifically so a host can render a good permission prompt:

`tool_use_id`, `agent_id`, `blocked_path`, `decision_reason`, `title`, `display_name`, `description`.

`title` is documented as *"the full permission prompt sentence — use this as the primary prompt text
instead of reconstructing from tool name + input."* Go callers cannot reconstruct it.

### 5.6 `CanUseTool` shadowing warning

Both reference SDKs emit a warning when `CanUseTool` is set alongside options that silently shadow it
(`permission_mode: bypassPermissions`, or an `allowed_tools` entry that allows a whole tool such as
`"Read"`, `"Read()"`, `"Read(*)"`). Python exports `CanUseToolShadowedWarning` for this; TS raises a
process warning with code `CLAUDE_SDK_CAN_USE_TOOL_SHADOWED`. Go has nothing, and this is the single
most common "my callback never fires" support question.

---

## §6. Missing message and block types

### 6.1 Content blocks

Go parses `text`, `thinking`, `tool_use`, `tool_result` and **silently drops everything else**
(`parser.go:330`). Missing:

- **`server_tool_use`** → `ServerToolUseBlock`, with `ServerToolName` ∈ {`advisor`, `web_search`,
  `web_fetch`, `code_execution`, `bash_code_execution`, `text_editor_code_execution`,
  `tool_search_tool_regex`, `tool_search_tool_bm25`}
- **`advisor_tool_result`** → `ServerToolResultBlock`

Anyone using web search or the advisor sees those turns as empty content.

### 6.2 Message-level types

Go parses 7 top-level types. Missing typed handling for, at minimum:

| Type / subtype | Py | TS |
|---|:--:|:--:|
| `system`/`task_updated` → `TaskUpdatedMessage` | ✓ | ✓ |
| `system`/`mirror_error` → `MirrorErrorMessage` | ✓ | ✓ |
| `system`/`hook_started`,`hook_response` → `HookEventMessage` | ✓ | ✓ |
| `system`/`compact_boundary` | | ✓ |
| `system`/`session_state_changed` | | ✓ |
| `system`/`status`, `api_retry`, `control_request_progress` | | ✓ |
| `system`/`background_tasks_changed`, `thinking_tokens` | | ✓ |
| `system`/`permission_denied`, `prompt_suggestion` | | ✓ |
| `system`/`tool_progress`, `tool_use_summary`, `memory_recall` | | ✓ |
| `system`/`plugin_install`, `commands_changed`, `auth_status` | | ✓ |
| `system`/`model_refusal_fallback`, `model_refusal_no_fallback` | | ✓ |
| `system`/`local_command_output`, `files_persisted`, `informational` | | ✓ |
| `system`/`conversation_reset`, `worker_shutting_down`, `notification` | | ✓ |
| `user_replay` → `SDKUserMessageReplay` | | ✓ |
| `transcript_mirror` (peeled off, not yielded) | ✓ | ✓ |
| `keep_alive` | | ✓ |

`TaskUpdatedMessage` matters most: the Python source documents that a background task's **terminal
state can arrive only as `task_updated`** with no accompanying `task_notification`. Go consumers
tracking active tasks will leak them forever.

### 6.3 `ResultMessage` fields

Go has 11 fields. Missing: `modelUsage` (per-model cost/token breakdown), `permission_denials`,
`deferred_tool_use`, `errors[]`, `api_error_status`, `uuid`, **`terminal_reason`** (`completed`,
`max_turns`, `aborted_streaming`, `budget_exhausted`, … — the only way to distinguish "finished" from
"was interrupted"), plus the success-only timing fields (`ttft_ms`, `time_to_request_ms`,
`user_message_uuid`, `warm_spare_claimed`, …).

Supporting types also missing: `ModelUsage`, `SDKPermissionDenial`, `DeferredToolUse`,
`TerminalReason`, `NonNullableUsage`.

### 6.4 `AssistantMessage` fields

Missing `message_id`, `stop_reason`, `session_id`, `uuid`. `Error` is declared on the struct but
**never populated** by `parseAssistantMessage`.

### 6.5 `RateLimitEvent`

`RateLimitInfo.ResetsAt` is `*string`; the wire type is a **number** (unix timestamp). Missing
`isUsingOverage`, `overageInUse`, `surpassedThreshold`, `errorCode`,
`canUserPurchaseCredits`, `hasChargeableSavedPaymentMethod`.

---

## §7. Entirely missing subsystem: `SessionStore` + session management

This is the largest single feature gap — roughly 4,000 LOC in the Python SDK
(`session_store.py`, `session_mutations.py`, `session_resume.py`, `session_summary.py`,
`session_import.py`, `transcript_mirror_batcher.py`, `sessions.py`, plus
`testing/session_store_conformance.py`).

**What it is:** a pluggable adapter that mirrors session transcripts to external storage (S3,
Postgres, Redis) so multi-tenant/serverless deployments can resume sessions that were never on this
machine's disk.

**Interface** (`append` + `load` required; `list_sessions`, `list_session_summaries`, `delete`,
`list_subkeys` optional):

```
append(key, entries)              load(key) -> entries | null
listSessions(projectKey)          listSessionSummaries(projectKey)
delete(key)                       listSubkeys({projectKey, sessionId})
```

Supporting machinery, all absent in Go:
- `transcript_mirror` frame peeling in the read loop (never yielded to consumers)
- batching (`batched`: flush per turn / 500 entries / 1 MiB; `eager`: flush per frame)
- 3-attempt retry with backoff → `MirrorErrorMessage` on final failure
- resume materialization: `load()` → temp JSONL → temp `CLAUDE_CONFIG_DIR` → `--resume`, including
  credential copying and subagent-subkey materialization with path-traversal checks
- `foldSessionSummary()` — pure incremental summary fold that adapters persist verbatim
- `importSessionToStore()`, `InMemorySessionStore`, `projectKeyForDirectory()`
- a published **conformance test suite** third-party adapters can run

### 7.1 Session-management functions

| Function | Py | TS | Go |
|---|:--:|:--:|:--:|
| `list_sessions(dir, limit, offset, include_worktrees)` | ✓ | ✓ | partial — no limit/offset/worktrees, no all-projects mode |
| `get_session_info` | ✓ | ✓ | ✓ (broken paths, §1.9) |
| `get_session_messages` | ✓ | ✓ | partial — returns raw lines, no conversation-chain reconstruction |
| `list_subagents` | ✓ | ✓ | ✗ |
| `get_subagent_messages` | ✓ | ✓ | ✗ |
| `rename_session` | ✓ | ✓ | ✓ |
| `tag_session` | ✓ | ✓ | ✓ |
| `delete_session` | ✓ | ✓ | ✗ |
| `fork_session` → `ForkSessionResult` | ✓ | ✓ | ✗ — Go's `ForkSession()` spawns a real CLI query with an empty prompt and scrapes the new session ID out of the result. Reference SDKs rewrite the JSONL offline: no subprocess, no tokens, no cost. |
| `*_from_store` / `*_via_store` async variants (9 fns) | ✓ | ✓ | ✗ |

Also missing in Go's session reading: `SDKSessionInfo` should carry `summary`, `last_modified`,
`file_size`, `custom_title`; Go has `FirstPrompt`/`LastPrompt`/`CreatedAt` instead — a different and
smaller shape. Go parses the entire JSONL to build it; the reference SDKs do a stat + head/tail read
with a hand-rolled JSON string-field extractor precisely to avoid that cost.

`get_session_messages` in the reference SDKs reconstructs the **conversation chain** by walking
`parentUuid` links and filtering sidechains and invisible entries. Go returns every raw JSONL line as
a `map[string]any`.

---

## §8. MCP / tool gaps

- **No typed tool schemas.** Python's `@tool` decorator accepts a `TypedDict` or `{"name": str}` dict
  and generates JSON Schema; TS's `tool()` takes a Zod shape and infers the handler's argument type.
  Go's `NewToolSimple` takes `map[string]any` of *zero values* and reflects on them — no descriptions,
  no optional fields, no nesting, everything forced `required`. `github.com/google/jsonschema-go` is
  already a module dependency but is used **only by an example**, not by `mcp/`.
- **No `_meta` / `maxResultSizeChars`.** Python emits `{"anthropic/maxResultSizeChars": N}` in `_meta`
  to control the CLI's large-tool-result spill threshold.
- **`ToolAnnotations` is `map[string]any`,** not a typed struct.
- **No `resource_link` / `resource` / `audio` content handling** in tool results (both reference SDKs
  convert these to text with a warning). Go's `MultiResult` also silently drops any item whose
  `content` is not exactly `[]map[string]any`.
- **No elicitation.** `onElicitation`, `ElicitationRequest`, `ElicitationResult`,
  `Elicitation`/`ElicitationResult` hooks, `elicitation_complete` message — all absent.
- **`McpServerToolPolicy`, `McpSetServersResult`, `McpClaudeAIProxyServerConfig`** types absent.

---

## §9. Robustness, lifecycle, and process-management gaps

### 9.1 No pluggable `Transport`
`internal/transport.Transport` is an internal interface. Both reference SDKs **export** `Transport`
and accept an injected implementation (`ClaudeSDKClient(transport=...)`, `query({transport})`) so
consumers can run the CLI over WebSocket/SSE/in-process. TS additionally exposes `SpawnedProcess` /
`SpawnOptions` / `spawnClaudeCodeProcess` for VM and container execution. This also makes the Go SDK
effectively untestable without a real `claude` binary.

### 9.2 Process teardown loses the last message
`subprocess.go:737` — `Close()` calls `Process.Kill()` immediately. Both reference SDKs do a staged
teardown: close stdin → wait 5s → `SIGTERM` → wait 5s → `SIGKILL`. The Python source cites issue #625:
without the grace period, SIGTERM interrupts the CLI's session-file flush and **the last assistant
message is lost**.

### 9.3 No orphan reaping
Both reference SDKs track live children and `SIGTERM` them from an `atexit` / `process.on('exit')`
handler. Go leaves orphaned `claude` processes when the parent exits without `Close()`.

### 9.4 Broken stdout line framing
`subprocess.go:634-666` accumulates lines into a `strings.Builder` and retries `json.Unmarshal` after
each. A single non-JSON line — the CLI writes `[SandboxDebug] ...` to stdout on some builds — poisons
the buffer and corrupts **every subsequent message** until the buffer limit trips. The reference SDKs
frame on `\n` and skip any line not starting with `{`.

### 9.5 Errors are swallowed in streaming mode
`protocol.Query.errorChan` is written to but **never read by `Client`** — `ErrorChan()` isn't wired
into `ReceiveMessages()` or `ReceiveResponse()`. A CLI crash mid-conversation surfaces as a silently
closed channel with no error. The reference SDKs inject a synthetic `{"type":"error"}` frame into the
message stream and raise on it.

### 9.6 `ReceiveMessages`/`ReceiveResponse` race
Both methods spawn a goroutine reading `c.rawMsgChan`. Calling both, or calling either concurrently,
splits messages nondeterministically between consumers. There is no guard.

### 9.7 `control_cancel_request` is a TODO
`protocol/query.go:251`. The reference SDKs cancel the in-flight handler task and suppress the
response. Go ignores the frame, so a cancelled hook/permission callback still writes a stale response.

### 9.8 stdin closed too early (issue #1088)
The reference SDKs track in-flight `local_agent`/`local_workflow` tasks and **keep stdin open past a
`result` frame** while any are running, because background subagents still need the control channel
for hooks and SDK-MCP calls. Go has no equivalent; `WaitForFirstResult` closes on the first `result`.

### 9.9 `ProcessError` masks the real error
When the CLI emits `result` with `is_error: true` it then exits non-zero *on purpose*. Both reference
SDKs remember `errors[]` from that result and substitute it for the useless
`"Command failed with exit code 1"`. Go surfaces only the exit code.

### 9.10 Missing environment handling
- `CLAUDE_AGENT_SDK_VERSION` not set
- `CLAUDECODE` not filtered from the inherited env (Python issue #573 — SDK-spawned subprocesses
  wrongly believe they are nested inside Claude Code)
- No OpenTelemetry `traceparent`/`tracestate` propagation (both reference SDKs inject it, with
  scrubbing of stale inherited values)
- `NODE_OPTIONS` not deleted
- stderr is always piped; the reference SDKs pipe only when a callback is registered
- No bundled-CLI discovery, despite `README.md` claiming *"the Claude Code CLI is automatically
  bundled with the package"* — `findCLI()` only searches `PATH` and six fixed locations

### 9.11 Version check is advisory-only and blocking
`checkCLIVersion` runs `claude -v` **synchronously on every connect** (2s timeout) and only prints to
`os.Stderr`. Minor, but it adds latency to every query.

### 9.12 Missing `AbortError`
TS exports `AbortError`; Go has no distinct error for context cancellation.

---

## §10. Go-only constructs not present in either reference SDK

Flagged for review — these may be intentional local extensions, but they are not portable and at
least two look like guesses at an unpublished API:

| Construct | Assessment |
|---|---|
| `Channels` / `ChannelServerConfig` / `ChannelMessage` / `--channels <json>` | `channel_message` appears **nowhere** in the TS bundle or `.d.ts`. The TS bundle does emit `--channels`, but as a **repeated flag taking a plain server name** (`--channels foo --channels bar`), not one JSON blob of configs. Go's shape is almost certainly wrong. |
| `types.ToolConfiguration{Enabled, MaxConcurrency, Timeout}` | Fabricated — see §1.4. |
| `QueryText()` / `QuerySync()` | Harmless Go-idiomatic conveniences; keep. |
| `mcp.GetString/GetFloat/GetInt/GetBool/...Optional` | Fine, Go-idiomatic; keep. |
| `AgentOptions.With*` builder methods | Fine; keep. |
| `errors.TimeoutError`, `errors.ControlRequestError` | Reasonable additions; keep. |

---

## §11. Scorecard

| Area | Go | Python 0.2.128 | TS 0.3.220 |
|---|---|---|---|
| Wire-format correctness | ✓ (was: 11 divergences) | ✓ | ✓ |
| Always-streaming transport | ✗ (`Query()` uses `--print`) | ✓ | ✓ |
| `initialize` payload fields | 1 (`hooks`) | 5 | 17 |
| Options | 42 (4 non-functional) | 48 | ~70 |
| Control methods | 9 (3 broken) | 11 | 26 |
| Hook events | 10 | 10 | 31 |
| Hook-specific outputs | 3 | 8 | ~20 |
| Content-block types | 4 | 6 | 6 |
| Typed message types | 7 | 13 | 40+ |
| `ResultMessage` fields | 11 | 18 | 25+ |
| `SessionStore` | ✗ | ✓ | ✓ |
| Session functions | 5 (paths broken) | 19 | 12 |
| Pluggable transport | ✗ | ✓ | ✓ |
| Typed MCP tool schemas | ✗ | ✓ | ✓ |
| Graceful subprocess teardown | ✗ | ✓ | ✓ |
| Orphan reaping | ✗ | ✓ | ✓ |
| Windows hardening | ✓ | ✓ | ✓ |

---

## §12. Plan

Six phases. Phase 1 is disproportionately valuable and should not be deferred.

### Phase 1 — Correctness — **DONE**

Fix everything in §1. Nothing else on this list matters while advertised features silently no-op.

1. `--thinking` / `--max-thinking-tokens` / `--thinking-display` mapping; add `ThinkingDisplay`.
2. `--tools default` for the preset.
3. `EnableFileCheckpointing` → `CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING=true` env var; drop the flag.
4. Replace `ToolConfiguration` with `ToolConfig{AskUserQuestion{PreviewFormat}}` →
   `CLAUDE_CODE_QUESTION_PREVIEW_FORMAT` env var; drop `--tool-config`.
5. `AgentProgressSummaries` → `initialize.agentProgressSummaries`; drop the flag.
6. `get_mcp_status` → `mcp_status`; read `mcpServers`; populate `ServerInfo`/`Scope`.
7. `server_name` → `serverName` for `mcp_reconnect` / `mcp_toggle`.
8. `TaskUsage{TotalTokens, ToolUses, DurationMS}`.
9. Rewrite `sessions.sanitizePath` to match the CLI (all non-alphanumerics → `-`; djb2→base36 for
   >200 chars) + honor `CLAUDE_CONFIG_DIR` + prefix-scan fallback.
10. Full `PermissionUpdate` decode (add `PermissionUpdateFromMap`, the inverse of `ToMap`).
11. `--resume=`, `--setting-sources=` only when set, `=` form for dash-leading `ExtraArgs`.
12. Reject `FallbackModel == Model`.

*Tests:* golden-argv tests over `buildCommand()` for every option, and golden-JSON tests for every
control request. This is the regression net the SDK currently lacks entirely.

### Phase 2 — Transport & lifecycle (≈3 days)

13. **Make `Query()` streaming.** Drop `--print`; always `--input-format stream-json`; write the
    prompt as a user frame; run the same `protocol.Query` as `Client`. This unlocks hooks,
    `CanUseTool`, and SDK MCP servers for `Query()` and unblocks Phase 3.
14. Fix line framing (split on `\n`, skip non-`{` lines, never concatenate).
15. Staged teardown: stdin close → 5s wait → SIGTERM → 5s → SIGKILL.
16. `atexit`-equivalent orphan reaper (`os/signal` + a package-level child registry).
17. Wire `errorChan` into the message stream; substitute result `errors[]` for bare exit codes.
18. Guard `ReceiveMessages`/`ReceiveResponse` against concurrent use (single-consumer fan-out).
19. Implement `control_cancel_request`.
20. Track in-flight `local_agent`/`local_workflow` tasks before closing stdin (#1088).
21. Env: `CLAUDE_AGENT_SDK_VERSION`, filter `CLAUDECODE`, OTel propagation, conditional stderr pipe.
22. Export `Transport` as a public interface and accept injection on `Client`/`Query`.

### Phase 3 — `initialize` handshake & option parity (≈3 days)

23. Move `agents`, `systemPrompt`, `appendSystemPrompt` into `initialize` (keeping the flags as a
    fallback for older CLIs); add `title`, `skills`, `toolAliases`, `planModeInstructions`,
    `excludeDynamicSections`, `promptSuggestions`, `forwardSubagentText`, `sdkMcpServers`,
    `jsonSchema`, `supportedDialogKinds`.
24. Add §3.1 options (`StrictMCPConfig`, `SessionID`, `Skills`, `IncludeHookEvents`, `TaskBudget`,
    `SystemPromptFile`).
25. Add §3.2 TS options (`Agent`, `AllowDangerouslySkipPermissions`, `ResumeSessionAt`,
    `ManagedSettings`, `Debug`/`DebugFile`, object-valued `Settings`, `Plugins[].SkipMCPDiscovery`).
26. Fill in the §3.3 enum values and `AgentDefinition` / `SandboxSettings` fields.
27. Typed `SDKControlInitializeResponse`; reimplement `SupportedAgents()` from it and **delete the
    output-scraping path in `agents.go`**.

### Phase 4 — Types & messages (≈3 days)

28. `ServerToolUseBlock` / `ServerToolResultBlock` + `ServerToolName`.
29. `ResultMessage`: `ModelUsage`, `PermissionDenials`, `DeferredToolUse`, `Errors`,
    `APIErrorStatus`, `UUID`, `TerminalReason`.
30. `AssistantMessage`: `MessageID`, `StopReason`, `SessionID`, `UUID`; actually parse `Error`.
31. `TaskUpdatedMessage` + `TERMINAL_TASK_STATUSES`; `HookEventMessage`; `MirrorErrorMessage`.
32. `RateLimitInfo.ResetsAt` → `*int64`; add the overage fields.
33. Typed structs for the high-value system subtypes (`compact_boundary`, `session_state_changed`,
    `status`, `api_retry`, `permission_denied`, `background_tasks_changed`, `tool_progress`).
34. Enrich `ToolPermissionContext` (`ToolUseID`, `AgentID`, `BlockedPath`, `DecisionReason`, `Title`,
    `DisplayName`, `Description`).
35. Fix `PostToolUseFailureHookInput.ToolInput` → `map[string]any`; add `ToolUseID`.
36. Add `prompt_id` / `agent_id` / `agent_type` / `effort` to `BaseHookInput`.

### Phase 5 — Control methods, hooks, MCP (≈4 days)

37. Add the §4.2 control methods. Prioritize: `GetContextUsage`, `SupportedCommands`,
    `SupportedModels`, `SupportedAgents`, `AccountInfo`, `SetMaxThinkingTokens`, `ApplyFlagSettings`,
    `SetMCPServers`, `BackgroundTasks`.
38. `RewindFiles` → `(*RewindFilesResult, error)` + `dryRun`; `Interrupt` →
    `(*InterruptResponse, error)` + `cancelQueued`.
39. `SendMessage(ctx, types.UserInputMessage)` / `StreamInput(ctx, <-chan ...)` alongside the
    string-only `SendQuery`.
40. Add the 21 missing hook events and the missing `HookSpecificOutput` variants (incl. `defer` and
    `updatedToolOutput`).
41. `CanUseToolShadowedWarning` equivalent (a `Warn func(string)` option, defaulting to a one-shot
    `log.Print`).
42. Typed MCP tool schemas via `google/jsonschema-go` — `mcp.NewToolFor[T]()` generating the schema
    from a Go struct and unmarshalling the handler argument. Add `_meta`/`MaxResultSizeChars`, typed
    `ToolAnnotations`, and `resource_link`/`resource` content handling.
43. Elicitation: `OnElicitation` option, request/result types, the two hook events.

### Phase 6 — Sessions & `SessionStore` (≈5 days)

44. Public `SessionStore` interface (`Append`/`Load` required; `ListSessions`,
    `ListSessionSummaries`, `Delete`, `ListSubkeys` optional via type assertion) + `SessionKey`,
    `SessionStoreEntry`, `SessionSummaryEntry`.
45. `transcript_mirror` frame peeling + batcher (`batched`/`eager`, 500-entry / 1 MiB thresholds,
    3-attempt retry → `MirrorErrorMessage`).
46. Resume materialization: `Load()` → temp JSONL → temp `CLAUDE_CONFIG_DIR` → `--resume`, with
    subkey materialization and path-traversal checks.
47. `FoldSessionSummary()`, `InMemorySessionStore`, `ImportSessionToStore()`,
    `ProjectKeyForDirectory()`.
48. Rewrite session reading: stat + head/tail extraction instead of full-file parse; `SDKSessionInfo`
    shape parity (`Summary`, `LastModified`, `FileSize`, `CustomTitle`); conversation-chain
    reconstruction in `GetSessionMessages`.
49. `ListSubagents`, `GetSubagentMessages`, `DeleteSession`; rewrite `ForkSession` as an **offline
    JSONL rewrite** returning `ForkSessionResult` (today it spawns a real billed query).
50. `list_sessions` options: `limit`, `offset`, `includeWorktrees`, all-projects mode.
51. Port the `SessionStore` conformance suite so third-party Go adapters can self-verify.

### Cross-cutting

- **Windows hardening** (§1.11): `.bat`/`.cmd` spawn refusal + cmd.exe metacharacter rejection.
  **Done** — landed with Phase 1.
- **Testing.** The three existing test files cover option cloning, hook serialization, and message
  parsing. Add: golden-argv tests (Phase 1), a fake `Transport` + scripted-NDJSON harness (Phase 2 —
  this is what makes Phases 3–6 testable at all), and control-protocol round-trip tests.
- **Version pinning.** Record which CLI version the SDK targets (TS pins `claudeCodeVersion:
  2.1.220`) and raise `minimumClaudeCodeVersion` from `2.0.0` accordingly.
- **README.** Remove the bundled-CLI claim until §9.10 is implemented. **Done.**

### Suggested ordering

```
Phase 1  ──►  Phase 2  ──►  Phase 3  ──►  Phase 4  ──┬─►  Phase 5
(correct)     (transport)   (initialize)  (types)    └─►  Phase 6
```

Phases 5 and 6 are independent of each other and can run in parallel. Phase 2 is the hard dependency
for everything after it, both because the streaming transport unblocks the `initialize` work and
because the fake-transport harness it introduces is what makes the rest testable.

**Rough total: ~20 working days** to reach Python parity, plus Phase 5's TS-only items for full
TypeScript parity. Phase 1 is complete; ~18 days remain.

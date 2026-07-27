# Claude Agent SDK for Go

Go SDK for Claude Agent. Attempts to implement [Claude Agent SDK](https://docs.anthropic.com/en/docs/claude-code/sdk) in Go.

## Installation

```bash
go get github.com/nabkey/claude-agent-sdk-go
```

**Prerequisites:**

- Go 1.24+

**Note:** The Claude Code CLI must be installed separately. The SDK looks for a
`claude` binary on `PATH` and then in the usual install locations
(`~/.npm-global/bin`, `/usr/local/bin`, `~/.local/bin`, `~/node_modules/.bin`,
`~/.yarn/bin`, `~/.claude/local`).

- Install Claude Code: `curl -fsSL https://claude.ai/install.sh | bash`
- Or specify a custom path: `claude.AgentOptions{CLIPath: claude.String("/path/to/claude")}`

Minimum supported CLI version: **2.0.0**.

## Quick Start

```go
package main

import (
	"context"
	"fmt"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// Returns a channel of messages
	for msg := range claude.Query(ctx, "What is 2 + 2?", nil) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Println(text.Text)
				}
			}
		case *types.ResultMessage:
			if m.TotalCostUSD != nil {
				fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
			}
		case error:
			fmt.Printf("Error: %v\n", m)
		}
	}
}
```

## Basic Usage: Query()

`Query()` is a helper function for querying Claude Code. It returns a read-only channel of response messages. See [query.go](query.go).

`Query()` runs the CLI in streaming mode, so `Hooks`, `CanUseTool`, and
in-process SDK MCP servers all work here. What it cannot do is send follow-up
messages or interrupt a run mid-flight — use [`Client`](#client) for those.

```go
// Simple query — returns <-chan any (messages or errors)
for msg := range claude.Query(ctx, "Hello Claude", nil) {
	switch m := msg.(type) {
	case *types.AssistantMessage:
		for _, block := range m.Content {
			if textBlock, ok := block.(*types.TextBlock); ok {
				fmt.Println(textBlock.Text)
			}
		}
	}
}

// With options
options := &claude.AgentOptions{
	SystemPrompt: claude.String("You are a helpful assistant"),
	MaxTurns:     claude.Int(1),
}
for msg := range claude.Query(ctx, "Tell me a joke", options) {
	fmt.Printf("%+v\n", msg)
}

// Convenience: get just the text response
text, err := claude.QueryText(ctx, "What is the capital of France?", nil)
fmt.Println(text) // "Paris"

// Convenience: collect all messages into a slice
messages, err := claude.QuerySync(ctx, "Hello", nil)
```

### Using Tools

```go
mode := types.PermissionModeAcceptEdits
options := &claude.AgentOptions{
	AllowedTools:   []string{"Read", "Write", "Bash"},
	PermissionMode: &mode, // auto-accept file edits
}

for msg := range claude.Query(ctx, "Create a hello.go file", options) {
	// Process messages...
}
```

### Working Directory

```go
options := &claude.AgentOptions{
	Cwd: claude.String("/path/to/project"),
}
```

## Client

`Client` supports bidirectional, interactive conversations with Claude Code. See [client.go](client.go).

Both entry points support custom tools, hooks, and permission callbacks. What
`Client` adds is **runtime control**: interrupts, follow-up messages, model and
permission changes mid-session, MCP management, context usage, file rewind, and
the rest of the control surface listed below.

```go
ctx := context.Background()
options := &claude.AgentOptions{
	MaxTurns: claude.Int(5),
}

client, err := claude.NewClient(ctx, options)
if err != nil {
	log.Fatal(err)
}
defer client.Close()

// Connect with an initial prompt
if err := client.Connect(ctx, "Hello Claude"); err != nil {
	log.Fatal(err)
}

// Read messages from the first response
for msg := range client.ReceiveResponse() {
	switch m := msg.(type) {
	case *types.AssistantMessage:
		for _, block := range m.Content {
			if text, ok := block.(*types.TextBlock); ok {
				fmt.Println(text.Text)
			}
		}
	case *types.ResultMessage:
		fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
	}
}

// Send a follow-up (same conversation)
client.SendQuery(ctx, "Can you elaborate on that?")
for msg := range client.ReceiveResponse() {
	// Process second response...
}
```

### Custom Tools (as In-Process SDK MCP Servers)

A **custom tool** is a Go function that you can offer to Claude, for Claude to invoke as needed.

Custom tools are implemented as in-process MCP servers that run directly within your Go application, eliminating the need for separate processes that regular MCP servers require.

For an end-to-end example, see [examples/mcp_calculator/main.go](examples/mcp_calculator/main.go).

#### Creating a Simple Tool

```go
import (
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Define the tool
greetTool := mcp.NewTool(
	"greet",
	"Greet a user",
	map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []string{"name"},
	},
	func(ctx context.Context, args map[string]any) (map[string]any, error) {
		name, _ := mcp.GetString(args, "name")
		return mcp.TextResult(fmt.Sprintf("Hello, %s!", name)), nil
	},
)

// Create an SDK MCP server
server := mcp.NewSDKServer("my-tools", "1.0.0", greetTool)

options := &claude.AgentOptions{
	MCPServers: map[string]types.MCPServerConfig{
		"tools": server,
	},
	AllowedTools: []string{"mcp__tools__greet"},
}

client, _ := claude.NewClient(ctx, options)
defer client.Close()
client.Connect(ctx, "Greet Alice")
for msg := range client.ReceiveResponse() {
	fmt.Printf("%+v\n", msg)
}
```

#### Tool Annotations

Tools can include annotations describing their behavior:

```go
tool := mcp.NewToolWithAnnotations(
	"read_data",
	"Read data from the database",
	schema,
	handler,
	map[string]any{
		"readOnly":    true,
		"destructive": false,
		"openWorld":   false,
	},
)
```

#### Mixed Server Support

You can use both SDK and external MCP servers together:

```go
sdkServer := mcp.NewSDKServer("internal", "1.0.0", myTools...)

options := &claude.AgentOptions{
	MCPServers: map[string]types.MCPServerConfig{
		"internal": sdkServer, // In-process SDK server
		"external": &types.StdioMCPServer{ // External subprocess server
			Command: "external-server",
		},
	},
}
```

You can also provide a path to an MCP config file:

```go
options := &claude.AgentOptions{
	MCPConfigPath: claude.String("/path/to/mcp-config.json"),
}
```

### Hooks

A **hook** is a Go function that the Claude Code *application* (*not* Claude) invokes at specific points of the Claude agent loop. Hooks can provide deterministic processing and automated feedback for Claude. Read more in [Claude Code Hooks Reference](https://docs.anthropic.com/en/docs/claude-code/hooks).

#### Available Hook Events

| Event | Description |
|-------|-------------|
| `PreToolUse` | Before a tool is executed |
| `PostToolUse` | After a tool is executed |
| `PostToolUseFailure` | After a tool use fails |
| `UserPromptSubmit` | When a user prompt is submitted |
| `Stop` | When the session stops |
| `SubagentStop` | When a subagent stops |
| `SubagentStart` | When a subagent starts |
| `PreCompact` | Before context compaction |
| `Notification` | On notifications |
| `PermissionRequest` | On permission requests |

#### Example

```go
options := &claude.AgentOptions{
	AllowedTools: []string{"Bash"},
	Hooks: map[types.HookEvent][]types.HookMatcher{
		types.HookEventPreToolUse: {
			{
				Matcher: claude.String("Bash"),
				Hooks: []types.HookCallback{
					func(ctx context.Context, input types.HookInput, toolUseID *string, hookCtx *types.HookContext) (*types.HookOutput, error) {
						ptu := input.(*types.PreToolUseHookInput)
						if strings.Contains(fmt.Sprint(ptu.ToolInput["command"]), "rm -rf") {
							decision := "deny"
							reason := "Dangerous command blocked"
							return &types.HookOutput{
								HookSpecificOutput: &types.PreToolUseHookSpecificOutput{
									HookEventName:            "PreToolUse",
									PermissionDecision:       &decision,
									PermissionDecisionReason: &reason,
								},
							}, nil
						}
						return nil, nil
					},
				},
			},
		},
	},
}
```

### Runtime Control

The `Client` provides methods for runtime control of the conversation:

```go
// Interrupt the current operation
client.Interrupt(ctx)

// Change permission mode
client.SetPermissionMode(ctx, types.PermissionModeAcceptEdits)

// Change model
client.SetModel(ctx, claude.String("claude-sonnet-4-5"))

// Stop a running task
client.StopTask(ctx, "task-id")

// Rewind files to a checkpoint (requires EnableFileCheckpointing)
client.RewindFiles(ctx, "user-message-id")
```

### MCP Runtime Control

Manage MCP servers during a conversation:

```go
// Get status of all MCP servers
status, err := client.GetMCPStatus(ctx)
for _, server := range status.Servers {
	fmt.Printf("Server: %s (status: %s)\n", server.Name, server.Status)
	for _, tool := range server.Tools {
		fmt.Printf("  Tool: %s\n", tool.Name)
	}
}

// Reconnect a server
client.ReconnectMCPServer(ctx, "my-server")

// Toggle a server on/off
client.ToggleMCPServer(ctx, "my-server", false)
```

## Thinking and Effort Configuration

Control Claude's reasoning behavior:

```go
// Adaptive thinking — model decides when to think (Opus 4.6+)
options := &claude.AgentOptions{
	Thinking: types.NewThinkingAdaptive(),
}

// Fixed thinking token budget (older models)
options = &claude.AgentOptions{
	Thinking: types.NewThinkingEnabled(10000),
}

// Disabled thinking
options = &claude.AgentOptions{
	Thinking: types.NewThinkingDisabled(),
}

// Effort level (low, medium, high, max)
options = claude.DefaultAgentOptions().WithEffort(types.EffortLevelHigh)
```

Set `Display` to control whether thinking text is returned. Opus 4.7+ defaults
to `omitted` (signature only); request `summarized` to receive text:

```go
display := types.ThinkingDisplaySummarized
options := &claude.AgentOptions{
	Thinking: &types.ThinkingConfigAdaptive{Type: "adaptive", Display: &display},
}
```

`Thinking` takes precedence over the deprecated `MaxThinkingTokens`.

## Beta Features

Enable beta features using typed constants:

```go
options := &claude.AgentOptions{
	Betas: []types.SdkBeta{types.SdkBetaContext1M},
}
```

## File Checkpointing

Enable file checkpointing for the ability to rewind file changes:

```go
options := claude.DefaultAgentOptions().WithFileCheckpointing()

client, _ := claude.NewClient(ctx, options)
defer client.Close()
client.Connect(ctx, "Make some changes to my code")

// Preview first — nothing is written
preview, _ := client.PreviewRewindFiles(ctx, "user-message-id")
fmt.Printf("would change %d files\n", len(preview.FilesChanged))

// Then apply
result, _ := client.RewindFiles(ctx, "user-message-id")
fmt.Printf("restored %d files (+%d/-%d)\n",
	len(result.FilesChanged), result.Insertions, result.Deletions)
```

To learn the message UUIDs to rewind to, enable user-message replay:
`ExtraArgs: map[string]*string{"replay-user-messages": nil}`.

## System Prompt and Tools Presets

Use presets for system prompts and tools:

```go
// System prompt preset
options := &claude.AgentOptions{
	SystemPrompt: &types.SystemPromptPreset{
		Type:   "preset",
		Preset: "claude_code",
		Append: claude.String("Always write tests."),
	},
}

// Tools preset
options = &claude.AgentOptions{
	Tools: &types.ToolsPreset{
		Type:   "preset",
		Preset: "claude_code",
	},
}
```

The `claude_code` tools preset selects the CLI's full default tool set. Pass an
explicit `[]string` to select individual tools, or an empty `[]string{}` to
disable all built-in tools.

## Setting Sources

`SettingSources` controls which filesystem settings the CLI loads:

```go
// Load user and project settings (project is required for CLAUDE.md)
options := &claude.AgentOptions{
	SettingSources: []types.SettingSource{
		types.SettingSourceUser,
		types.SettingSourceProject,
	},
}

// SDK isolation mode: load no filesystem settings at all
options = &claude.AgentOptions{
	SettingSources: []types.SettingSource{},
}
```

Leaving `SettingSources` nil loads **all** sources, matching the CLI default.
Note the distinction: a nil slice omits the flag entirely, whereas an empty
non-nil slice explicitly disables filesystem settings.

## Per-Tool Configuration

`ToolConfig` configures built-in tools. Today this covers the preview format
for `AskUserQuestion` options — use HTML for web-based consumers:

```go
options := claude.DefaultAgentOptions().
	WithAskUserQuestionPreviewFormat(types.PreviewFormatHTML)
```

## Runtime Control

`Client` exposes the CLI's control protocol:

```go
// Conversation
client.Interrupt(ctx)
client.InterruptWithOptions(ctx, true) // also cancel queued messages
client.SetPermissionMode(ctx, types.PermissionModeAcceptEdits)
client.SetModel(ctx, claude.String("claude-sonnet-5"))
client.ApplyFlagSettings(ctx, map[string]any{"effortLevel": "high"})

// Introspection
usage, _ := client.GetContextUsage(ctx)
info := client.InitializationResult()   // commands, agents, models, account
models, _ := client.SupportedModels(ctx)
commands := client.SupportedCommands()
agents := client.SupportedAgents()

// MCP
status, _ := client.GetMCPStatus(ctx)
client.ReconnectMCPServer(ctx, "my-server")
client.ToggleMCPServer(ctx, "my-server", false)
client.SetMCPServers(ctx, servers)

// Tasks and files
client.StopTask(ctx, taskID)
client.BackgroundTasks(ctx, "")         // background everything in flight
client.ReadFile(ctx, "src/main.go", 0, "utf-8")
client.ReloadPlugins(ctx)
client.ReloadSkills(ctx)
```

## Typed MCP Tools

`NewToolFor` derives a tool's JSON Schema from a Go struct and hands the
handler a decoded value, so there is no hand-written schema to keep in step:

```go
type AddArgs struct {
	A float64 `json:"a" jsonschema:"the first addend"`
	B float64 `json:"b" jsonschema:"the second addend"`
}

addTool, err := mcp.NewToolFor("add", "Add two numbers",
	func(ctx context.Context, args AddArgs) (map[string]any, error) {
		return mcp.TextResult(fmt.Sprintf("%g", args.A+args.B)), nil
	})
```

`mcp.NewTool` remains for hand-written schemas. See
[examples/typed_tools](examples/typed_tools/main.go).

## Custom Transports

`Transport` abstracts how the SDK talks to Claude Code. The default spawns the
`claude` CLI locally; supply your own to run it in a container, a VM, or a
remote worker — or to drive the SDK from a scripted fake in tests, with no
binary required:

```go
type Transport interface {
	Connect(ctx context.Context) error
	Write(ctx context.Context, data string) error
	ReadMessages(ctx context.Context) (<-chan map[string]any, <-chan error)
	EndInput() error
	Close() error
	IsReady() bool
}

// Either entry point accepts one:
claude.QueryWithTransport(ctx, "hello", options, myTransport)
claude.NewClientWithTransport(ctx, options, myTransport)
```

## Session Store

A `SessionStore` mirrors transcripts to external storage, so a multi-tenant or
serverless deployment can resume a session that was never on this machine's
disk. Only `Append` and `Load` are required; listing, deletion, summaries, and
subagent enumeration are separate optional interfaces a store may also
implement.

```go
store := claude.NewInMemorySessionStore() // or your own adapter

options := claude.DefaultAgentOptions().WithSessionStore(store)

for msg := range claude.Query(ctx, "Hello", options) { /* ... */ }

projectKey := claude.ProjectKeyForDirectory(".")
sessions, _ := claude.ListSessionsFromStore(ctx, store, projectKey)
```

See [examples/session_store](examples/session_store/main.go).

## Session Management

The `sessions` package provides functions to list, read, and modify Claude Code sessions:

```go
import "github.com/nabkey/claude-agent-sdk-go/sessions"

// List all sessions for the current directory
sessionList, err := sessions.ListSessions(nil)
for _, s := range sessionList {
	fmt.Printf("Session: %s — %s\n", s.SessionID, s.FirstPrompt)
}

// Read messages from a session
messages, err := sessions.GetSessionMessages("session-id", nil)

// Tag a session
tag := "important"
sessions.TagSession("session-id", &tag, nil)

// Rename a session
sessions.RenameSession("session-id", "My Session Title", nil)
```

## Custom Agent Definitions

Define custom agents with specialized behavior:

```go
options := &claude.AgentOptions{
	Agents: map[string]types.AgentDefinition{
		"reviewer": {
			Description: "Code review specialist",
			Prompt:      "Review code for bugs and style issues",
			Tools:       []string{"Read", "Grep", "Glob"},
			Model:       claude.String("sonnet"),
			Skills:      []string{"code-review"},
			Memory:      claude.String("project_context"),
			MCPServers:  []any{"linter-server"},
		},
	},
}
```

## Types

See [types/](types/) for complete type definitions:

- **Messages**: `AssistantMessage`, `UserMessage`, `SystemMessage`, `ResultMessage`, `StreamEvent`, `RateLimitEvent`
- **Task messages**: `TaskStartedMessage`, `TaskProgressMessage`, `TaskNotificationMessage`, `TaskUpdatedMessage`
- **System messages**: `HookEventMessage`, `CompactBoundaryMessage`, `SessionStateChangedMessage`, `PermissionDeniedMessage`, `APIRetryMessage`, `StatusMessage`, `ToolProgressMessage`, `PromptSuggestionMessage`, `MirrorErrorMessage`
- **Content blocks**: `TextBlock`, `ThinkingBlock`, `ToolUseBlock`, `ToolResultBlock`, `ServerToolUseBlock`, `ServerToolResultBlock`
- **Results**: `ModelUsage`, `PermissionDenial`, `DeferredToolUse`, `TerminalReason`
- **Configuration**: `AgentOptions`, `AgentDefinition`, `ThinkingConfig`, `ThinkingDisplay`, `EffortLevel`, `SdkBeta`, `ToolConfig`
- **MCP**: `MCPServerConfig`, `McpStatusResponse`, `McpServerStatus`, `McpToolInfo`
- **Hooks**: `HookInput`, `HookOutput`, `HookCallback`, `HookMatcher`
- **Permissions**: `PermissionResult`, `PermissionUpdate`, `PermissionUpdateFromMap`, `ToolPermissionContext`
- **Sessions**: `SDKSessionInfo`, `SessionMessage`, `SessionKey`, `SessionStoreEntry`, `SessionSummaryEntry`
- **Control responses**: `InitializeResult`, `ContextUsage`, `RewindFilesResult`, `InterruptResult`, `SlashCommand`, `ModelInfo`, `AgentInfo`, `AccountInfo`

## Error Handling

The SDK uses standard Go error handling. Specific error types are available for type assertions.

```go
import "github.com/nabkey/claude-agent-sdk-go/errors"

text, err := claude.QueryText(ctx, "Hello", nil)
if err != nil {
	var notFound *errors.CLINotFoundError
	var processErr *errors.ProcessError

	switch {
	case errors.As(err, &notFound):
		fmt.Println("Please install Claude Code")
	case errors.As(err, &processErr):
		fmt.Printf("Process failed with exit code: %d\n", processErr.ExitCode)
	default:
		fmt.Printf("Error: %v\n", err)
	}
}
```

## Available Tools

See the [Claude Code documentation](https://docs.anthropic.com/en/docs/claude-code/settings#tools-available-to-claude) for a complete list of available tools.

## Examples

| Example | Description |
|---------|-------------|
| [quick_start](examples/quick_start/main.go) | Basic query usage |
| [streaming_mode](examples/streaming_mode/main.go) | Interactive `Client` with multi-turn conversations |
| [mcp_calculator](examples/mcp_calculator/main.go) | Custom tools via SDK MCP servers |
| [hooks](examples/hooks/main.go) | Hook callbacks for tool control |
| [thinking_config](examples/thinking_config/main.go) | Thinking and effort configuration |
| [session_management](examples/session_management/main.go) | Session listing and management |
| [mcp_status](examples/mcp_status/main.go) | MCP runtime control |
| [structured_output](examples/structured_output/main.go) | JSON schema output format |
| [system_prompt](examples/system_prompt/main.go) | Custom system prompts |
| [session_resume](examples/session_resume/main.go) | Resuming previous sessions |
| [tool_permission_callback](examples/tool_permission_callback/main.go) | Custom permission handling |
| [typed_tools](examples/typed_tools/main.go) | MCP tools with schemas generated from Go structs |
| [session_store](examples/session_store/main.go) | Mirroring transcripts to external storage |

## Development

```bash
# Run tests
go test ./...

# Build
go build ./...

# Vet
go vet ./...
```

### Release Workflow

The package is versioned via git tags.

```bash
git tag v0.1.0
git push origin v0.1.0
```

## License

MIT

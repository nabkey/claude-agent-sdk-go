# Claude Agent SDK for Go

Go SDK for Claude Agent. Attempts to implement [Claude Agent SDK](https://docs.anthropic.com/en/docs/claude-code/sdk) in Go.

## Installation

```bash
go get github.com/nabkey/claude-agent-sdk-go
```

**Prerequisites:**

- Go 1.26

**Note:** The Claude Code CLI is automatically bundled with the package or downloaded on first use—no separate installation required! The SDK will use the bundled CLI by default. If you prefer to use a system-wide installation or a specific version, you can:

- Install Claude Code separately: `curl -fsSL https://claude.ai/install.sh | bash`
- Specify a custom path: `claude.AgentOptions{CLIPath: claude.String("/path/to/claude")}`

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

Unlike `Query()`, the `Client` additionally enables **custom tools**, **hooks**, and **runtime control** (interrupts, model changes, MCP management, etc.).

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
// Adaptive thinking — model decides when to think
options := &claude.AgentOptions{
	Thinking: &types.ThinkingConfigAdaptive{Type: "adaptive"},
}

// Enabled thinking with a token budget
options = &claude.AgentOptions{
	Thinking: &types.ThinkingConfigEnabled{
		Type:         "enabled",
		BudgetTokens: 10000,
	},
}

// Disabled thinking
options = &claude.AgentOptions{
	Thinking: &types.ThinkingConfigDisabled{Type: "disabled"},
}

// Effort level (low, medium, high, max)
options = claude.DefaultAgentOptions().WithEffort(types.EffortLevelHigh)
```

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

// Later, rewind to a specific point
client.RewindFiles(ctx, "user-message-id")
```

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
- **Task messages**: `TaskStartedMessage`, `TaskProgressMessage`, `TaskNotificationMessage`
- **Content blocks**: `TextBlock`, `ThinkingBlock`, `ToolUseBlock`, `ToolResultBlock`
- **Configuration**: `AgentOptions`, `AgentDefinition`, `ThinkingConfig`, `EffortLevel`, `SdkBeta`
- **MCP**: `MCPServerConfig`, `McpStatusResponse`, `McpServerStatus`, `McpToolInfo`
- **Hooks**: `HookInput`, `HookOutput`, `HookCallback`, `HookMatcher`
- **Permissions**: `PermissionResult`, `PermissionUpdate`, `ToolPermissionContext`
- **Sessions**: `SDKSessionInfo`, `SessionMessage`

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

# Claude Agent SDK for Go

Go SDK for Claude Agent. See the [Claude Agent SDK documentation](https://docs.anthropic.com/en/docs/claude-code/sdk) for more information.

## Installation

```bash
go get github.com/nabkey/claude-agent-sdk-go
```

**Prerequisites:**

  - Go 1.22+

**Note:** The Claude Code CLI is automatically bundled with the package or downloaded on first use—no separate installation required\! The SDK will use the bundled CLI by default. If you prefer to use a system-wide installation or a specific version, you can:

  - Install Claude Code separately: `curl -fsSL https://claude.ai/install.sh | bash`
  - Specify a custom path: `claude.AgentOptions{CLIPath: "/path/to/claude"}`

## Quick Start

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nabkey/claude-agent-sdk-go"
)

func main() {
	ctx := context.Background()
	
	// Returns a channel of messages
	msgChan, err := claude.Query(ctx, "What is 2 + 2?", nil)
	if err != nil {
		log.Fatal(err)
	}

	for msg := range msgChan {
		fmt.Printf("%+v\n", msg)
	}
}
```

## Basic Usage: Query()

`Query()` is a helper function for querying Claude Code. It returns a read-only channel of response messages. See [query.go](query.go).

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// Simple query
	msgChan, err := claude.Query(ctx, "Hello Claude", nil)
	if err != nil {
		log.Fatal(err)
	}

	for msg := range msgChan {
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
		SystemPrompt: "You are a helpful assistant",
		MaxTurns:     1,
	}

	msgChan, err = claude.Query(ctx, "Tell me a joke", options)
	if err != nil {
		log.Fatal(err)
	}

	for msg := range msgChan {
		fmt.Printf("%+v\n", msg)
	}
}
```

### Using Tools

```go
options := &claude.AgentOptions{
    AllowedTools:   []string{"Read", "Write", "Bash"},
    PermissionMode: types.PermissionModeAcceptEdits, // auto-accept file edits
}

msgChan, err := claude.Query(ctx, "Create a hello.go file", options)
// Process messages...
```

### Working Directory

```go
options := &claude.AgentOptions{
    Cwd: "/path/to/project",
}
```

## Client

`Client` supports bidirectional, interactive conversations with Claude Code. See [client.go](client.go).

Unlike `Query()`, the `Client` additionally enables **custom tools** and **hooks**, both of which can be defined as Go functions.

### Custom Tools (as In-Process SDK MCP Servers)

A **custom tool** is a Go function that you can offer to Claude, for Claude to invoke as needed.

Custom tools are implemented as in-process MCP servers that run directly within your Go application, eliminating the need for separate processes that regular MCP servers require.

For an end-to-end example, see [examples/mcp\_calculator/main.go](examples/mcp_calculator/main.go).

#### Creating a Simple Tool

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Define the tool function
func GreetUser(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	name, ok := args["name"].(string)
	if !ok {
		return nil, fmt.Errorf("name is required")
	}
	
	return map[string]interface{}{
		"content": []map[string]string{
			{"type": "text", "text": fmt.Sprintf("Hello, %s!", name)},
		},
	}, nil
}

func main() {
	// Define the tool definition schema
	greetTool := mcp.NewTool(
		"greet", 
		"Greet a user", 
		map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]string{"type": "string"},
			},
			"required": []string{"name"},
		},
		GreetUser,
	)

	// Create an SDK MCP server
	server := mcp.NewSDKServer("my-tools", "1.0.0", greetTool)

	options := &claude.AgentOptions{
		MCPServers: map[string]types.MCPServerConfig{
			"tools": server,
		},
		AllowedTools: []string{"mcp__tools__greet"},
	}

	ctx := context.Background()
	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.Query("Greet Alice"); err != nil {
		log.Fatal(err)
	}

	// Extract and print response
	for msg := range client.ReceiveResponse() {
		fmt.Printf("%+v\n", msg)
	}
}
```

#### Benefits Over External MCP Servers

  - **No subprocess management** - Runs in the same process as your application
  - **Better performance** - No IPC overhead for tool calls
  - **Simpler deployment** - Single Go binary instead of multiple processes
  - **Easier debugging** - All code runs in the same process
  - **Type safety** - Direct Go function calls

#### Mixed Server Support

You can use both SDK and external MCP servers together:

```go
sdkServer := mcp.NewSDKServer("internal", "1.0.0", myTools...)

options := &claude.AgentOptions{
    MCPServers: map[string]types.MCPServerConfig{
        "internal": sdkServer, // In-process SDK server
        "external": types.StdioMCPServer{ // External subprocess server
            Type:    "stdio",
            Command: "external-server",
        },
    },
}
```

### Hooks

A **hook** is a Go function that the Claude Code *application* (*not* Claude) invokes at specific points of the Claude agent loop. Hooks can provide deterministic processing and automated feedback for Claude. Read more in [Claude Code Hooks Reference](https://docs.anthropic.com/en/docs/claude-code/hooks).

#### Example

```go
import (
	"context"
	"strings"
	"github.com/nabkey/claude-agent-sdk-go/hooks"
)

func CheckBashCommand(ctx context.Context, input map[string]interface{}, toolUseID string, context interface{}) (map[string]interface{}, error) {
	toolName, _ := input["tool_name"].(string)
	if toolName != "Bash" {
		return nil, nil
	}
	
	toolInput, _ := input["tool_input"].(map[string]interface{})
	command, _ := toolInput["command"].(string)
	
	if strings.Contains(command, "foo.sh") {
		return map[string]interface{}{
			"hookSpecificOutput": map[string]interface{}{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "deny",
				"permissionDecisionReason": "Command contains invalid pattern: foo.sh",
			},
		}, nil
	}
	return nil, nil
}

// Configuration
options := &claude.AgentOptions{
    AllowedTools: []string{"Bash"},
    Hooks: map[types.HookEvent][]types.HookMatcher{
        types.PreToolUse: {
            {
                Matcher: "Bash",
                Hook:    CheckBashCommand,
            },
        },
    },
}
```

## Types

See [types/types.go](types/types.go) for complete type definitions:

  - `AgentOptions` - Configuration options
  - `AssistantMessage`, `UserMessage`, `SystemMessage`, `ResultMessage` - Message structs
  - `TextBlock`, `ToolUseBlock`, `ToolResultBlock` - Content block interfaces and structs

## Error Handling

The SDK uses standard Go error handling. Specific error types are available for type assertions.

```go
import (
    "github.com/nabkey/claude-agent-sdk-go/errors"
)

msgChan, err := claude.Query(ctx, "Hello", nil)
if err != nil {
    var notFound *errors.CLINotFoundError
    var processErr *errors.ProcessError
    
    if errors.As(err, &notFound) {
        fmt.Println("Please install Claude Code")
    } else if errors.As(err, &processErr) {
        fmt.Printf("Process failed with exit code: %d\n", processErr.ExitCode)
    } else {
        fmt.Printf("Error: %v\n", err)
    }
}
```

## Available Tools

See the [Claude Code documentation](https://docs.anthropic.com/en/docs/claude-code/settings#tools-available-to-claude) for a complete list of available tools.

## Examples

See [examples/quick\_start/main.go](examples/quick_start/main.go) for a complete working example.

See [examples/streaming\_mode/main.go](examples/streaming_mode/main.go) for comprehensive examples involving `Client`.

## Development

If you're contributing to this project, run the tests and linter to ensure everything is working:

```bash
make test
make lint
```

### Building

The Go SDK embeds the Claude Code CLI binary or manages its download. The build scripts help verify this integration.

```bash
make build
```

### Release Workflow

The package is versioned via git tags:

```bash
git tag v0.1.0
git push origin v0.1.0
```

## License

MIT
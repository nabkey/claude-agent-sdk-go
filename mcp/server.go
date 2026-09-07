package mcp

import (
	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// SDKServer represents an in-process MCP server.
type SDKServer struct {
	name         string
	version      string
	tools        []Tool
	instructions string
	timeoutMS    int
	alwaysLoad   bool
}

// ServerOption configures an in-process MCP server.
type ServerOption func(*SDKServer)

// WithInstructions sets server instructions, returned from the MCP initialize
// response and surfaced to the model as an instructions block.
//
// When proxying a real MCP server through the SDK transport, pass that
// server's instructions here so they are not dropped.
func WithInstructions(instructions string) ServerOption {
	return func(s *SDKServer) { s.instructions = instructions }
}

// WithToolTimeout sets a per-server tool-call timeout in milliseconds,
// overriding the MCP_TOOL_TIMEOUT environment variable for this server.
//
// It is a hard wall-clock limit per call: progress notifications do not extend
// it. Values below 1000ms are ignored by the CLI, which falls through to
// MCP_TOOL_TIMEOUT or its default. The timeout applies when the server is
// first registered, so changing it for an already registered server takes
// effect only after a remove and re-add.
func WithToolTimeout(milliseconds int) ServerOption {
	return func(s *SDKServer) { s.timeoutMS = milliseconds }
}

// WithAlwaysLoad keeps every tool from this server in the prompt rather than
// deferring it behind tool search.
//
// Per-tool Tool.AlwaysLoad still applies and is OR'd with this.
func WithAlwaysLoad() ServerOption {
	return func(s *SDKServer) { s.alwaysLoad = true }
}

// NewSDKServer creates an in-process MCP server that runs within your Go application.
//
// Unlike external MCP servers that run as separate processes, SDK MCP servers
// run directly in your application's process. This provides:
//   - Better performance (no IPC overhead)
//   - Simpler deployment (single binary)
//   - Easier debugging (same process)
//   - Direct access to your application's state
//
// Parameters:
//   - name: Unique identifier for the server
//   - version: Server version string (e.g., "1.0.0")
//   - tools: List of Tool instances created with NewTool
//
// Returns an MCPServerConfig that can be passed to AgentOptions.MCPServers.
//
// Example:
//
//	greetTool := mcp.NewTool("greet", "Greet a user", schema, greetHandler)
//	calcTool := mcp.NewTool("calculate", "Do math", schema, calcHandler)
//
//	server := mcp.NewSDKServer("my-tools", "1.0.0", greetTool, calcTool)
//
//	options := &claude.AgentOptions{
//	    MCPServers: map[string]types.MCPServerConfig{
//	        "tools": server,
//	    },
//	    AllowedTools: []string{"mcp__tools__greet", "mcp__tools__calculate"},
//	}
func NewSDKServer(name, version string, tools ...Tool) *types.SDKMCPServer {
	return NewSDKServerWithOptions(name, version, tools, nil)
}

// NewSDKServerWithOptions is NewSDKServer with per-server configuration:
// instructions, a tool-call timeout, or always-loaded tools.
//
//	server := mcp.NewSDKServerWithOptions("my-tools", "1.0.0",
//	    []mcp.Tool{greetTool},
//	    []mcp.ServerOption{mcp.WithToolTimeout(30_000)})
func NewSDKServerWithOptions(name, version string, tools []Tool, options []ServerOption) *types.SDKMCPServer {
	server := &SDKServer{
		name:    name,
		version: version,
		tools:   tools,
	}
	for _, option := range options {
		option(server)
	}

	// Create the handler for the protocol layer
	handler := server.toHandler()

	return &types.SDKMCPServer{
		Type:      "sdk",
		Name:      name,
		Version:   version,
		TimeoutMS: server.timeoutMS,
		Instance:  handler,
	}
}

// toHandler converts the SDKServer to a protocol.MCPServerHandler.
func (s *SDKServer) toHandler() *protocol.MCPServerHandler {
	mcpTools := make([]protocol.MCPTool, len(s.tools))
	for i, tool := range s.tools {
		// Capture tool in closure
		t := tool
		mcpTools[i] = protocol.MCPTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: t.InputSchema,
			Handler:     t.Handler,
			Meta:        t.meta(s.alwaysLoad),
		}
		if t.Annotations != nil {
			mcpTools[i].Annotations = t.Annotations
		}
	}

	return &protocol.MCPServerHandler{
		Name:         s.name,
		Version:      s.version,
		Tools:        mcpTools,
		Instructions: s.instructions,
		TimeoutMS:    s.timeoutMS,
	}
}

// Instructions returns the server instructions, if any.
func (s *SDKServer) Instructions() string { return s.instructions }

// ToolTimeoutMS returns the per-server tool-call timeout, or zero for none.
func (s *SDKServer) ToolTimeoutMS() int { return s.timeoutMS }

// Name returns the server name.
func (s *SDKServer) Name() string {
	return s.name
}

// Version returns the server version.
func (s *SDKServer) Version() string {
	return s.version
}

// Tools returns the registered tools.
func (s *SDKServer) Tools() []Tool {
	return s.tools
}

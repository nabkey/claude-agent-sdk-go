package mcp

import (
	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// SDKServer represents an in-process MCP server.
type SDKServer struct {
	name    string
	version string
	tools   []Tool
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
	server := &SDKServer{
		name:    name,
		version: version,
		tools:   tools,
	}

	// Create the handler for the protocol layer
	handler := server.toHandler()

	return &types.SDKMCPServer{
		Type:     "sdk",
		Name:     name,
		Version:  version,
		Instance: handler,
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
		}
	}

	return &protocol.MCPServerHandler{
		Name:    s.name,
		Version: s.version,
		Tools:   mcpTools,
	}
}

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

// Example: MCP runtime control - checking status and managing servers
package main

import (
	"context"
	"fmt"
	"log"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// Create a client with an MCP server
	options := &claude.AgentOptions{
		MCPServers: map[string]types.MCPServerConfig{
			"example": &types.StdioMCPServer{
				Command: "npx",
				Args:    []string{"-y", "@modelcontextprotocol/server-everything"},
			},
		},
		PermissionMode: &[]types.PermissionMode{types.PermissionModeBypassPermissions}[0],
	}

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	// Get MCP server status
	status, err := client.GetMCPStatus(ctx)
	if err != nil {
		log.Fatal(err)
	}

	for _, server := range status.Servers {
		fmt.Printf("Server: %s (status: %s)\n", server.Name, server.Status)
		if server.Error != nil {
			fmt.Printf("  Error: %s\n", *server.Error)
		}
		for _, tool := range server.Tools {
			fmt.Printf("  Tool: %s\n", tool.Name)
			if tool.Description != nil {
				fmt.Printf("    Description: %s\n", *tool.Description)
			}
		}
	}

	// Reconnect a server
	if err := client.ReconnectMCPServer(ctx, "example"); err != nil {
		log.Printf("Reconnect failed: %v\n", err)
	}

	// Toggle a server off
	if err := client.ToggleMCPServer(ctx, "example", false); err != nil {
		log.Printf("Toggle failed: %v\n", err)
	}
}

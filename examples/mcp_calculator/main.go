// Example: MCP Calculator
//
// This example demonstrates creating custom tools using in-process MCP servers.
// These tools run directly in your Go application without subprocess overhead.
package main

import (
	"context"
	"fmt"
	"log"
	"math"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// Define calculator tools
	addTool := mcp.NewTool(
		"add",
		"Add two numbers together",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number", "description": "First number"},
				"b": map[string]any{"type": "number", "description": "Second number"},
			},
			"required": []string{"a", "b"},
		},
		func(ctx context.Context, args map[string]any) (map[string]any, error) {
			a, err := mcp.GetFloat(args, "a")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			b, err := mcp.GetFloat(args, "b")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return mcp.TextResult(fmt.Sprintf("%.2f + %.2f = %.2f", a, b, a+b)), nil
		},
	)

	multiplyTool := mcp.NewTool(
		"multiply",
		"Multiply two numbers together",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"a": map[string]any{"type": "number", "description": "First number"},
				"b": map[string]any{"type": "number", "description": "Second number"},
			},
			"required": []string{"a", "b"},
		},
		func(ctx context.Context, args map[string]any) (map[string]any, error) {
			a, err := mcp.GetFloat(args, "a")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			b, err := mcp.GetFloat(args, "b")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			return mcp.TextResult(fmt.Sprintf("%.2f × %.2f = %.2f", a, b, a*b)), nil
		},
	)

	sqrtTool := mcp.NewTool(
		"sqrt",
		"Calculate the square root of a number",
		map[string]any{
			"type": "object",
			"properties": map[string]any{
				"n": map[string]any{"type": "number", "description": "Number to take square root of"},
			},
			"required": []string{"n"},
		},
		func(ctx context.Context, args map[string]any) (map[string]any, error) {
			n, err := mcp.GetFloat(args, "n")
			if err != nil {
				return mcp.ErrorResult(err.Error()), nil
			}
			if n < 0 {
				return mcp.ErrorResult("Cannot take square root of negative number"), nil
			}
			return mcp.TextResult(fmt.Sprintf("√%.2f = %.4f", n, math.Sqrt(n))), nil
		},
	)

	// Create SDK MCP server with the calculator tools
	calculatorServer := mcp.NewSDKServer("calculator", "1.0.0", addTool, multiplyTool, sqrtTool)

	// Configure Claude to use the calculator
	options := &claude.AgentOptions{
		MCPServers: map[string]types.MCPServerConfig{
			"calc": calculatorServer,
		},
		AllowedTools: []string{
			"mcp__calc__add",
			"mcp__calc__multiply",
			"mcp__calc__sqrt",
		},
		MaxTurns: claude.Int(5),
	}

	// Create and connect client
	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Calculator MCP Server ready!")
	fmt.Println("Asking Claude to solve: (5 + 3) × 2, then find √64")
	fmt.Println()

	// Ask Claude to use the calculator
	prompt := "Please calculate (5 + 3) × 2 using the calculator tools, then find the square root of 64."
	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	// Process the response
	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case *types.TextBlock:
					fmt.Printf("Claude: %s\n", b.Text)
				case *types.ToolUseBlock:
					fmt.Printf("  [Using tool: %s with args: %v]\n", b.Name, b.Input)
				}
			}
		case *types.UserMessage:
			// Tool results come back as user messages
			if content, ok := m.Content.([]types.ContentBlock); ok {
				for _, block := range content {
					if tr, ok := block.(*types.ToolResultBlock); ok {
						fmt.Printf("  [Tool result: %v]\n", tr.Content)
					}
				}
			}
		case *types.ResultMessage:
			fmt.Printf("\n--- Done (Cost: $%.4f, Turns: %d) ---\n",
				getValue(m.TotalCostUSD, 0), m.NumTurns)
		}
	}
}

func getValue(ptr *float64, defaultVal float64) float64 {
	if ptr != nil {
		return *ptr
	}
	return defaultVal
}

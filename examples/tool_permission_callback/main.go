// Example: Tool Permission Callback
//
// This example demonstrates how to use tool permission callbacks to control
// which tools Claude can use and optionally modify their inputs.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Track tool usage for demonstration
var toolUsageLog []map[string]any

func main() {
	ctx := context.Background()

	fmt.Println("============================================================")
	fmt.Println("Tool Permission Callback Example")
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("This example demonstrates how to:")
	fmt.Println("1. Allow/deny tools based on type")
	fmt.Println("2. Modify tool inputs for safety")
	fmt.Println("3. Log tool usage")
	fmt.Println("============================================================")
	fmt.Println()

	// Configure options with our permission callback
	options := &claude.AgentOptions{
		CanUseTool: myPermissionCallback,
		MaxTurns:   claude.Int(5),
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

	// Send a query that will trigger multiple tool uses
	prompt := `Please do the following:
1. List the files in the current directory using ls
2. Show the first 5 lines of go.mod
3. Run: echo "Hello from the SDK!"`

	fmt.Println("Sending query to Claude...")
	fmt.Printf("User: %s\n\n", prompt)

	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	// Process the response
	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			fmt.Println()
			fmt.Println("============================================================")
			fmt.Println("Task completed!")
			if m.TotalCostUSD != nil {
				fmt.Printf("Cost: $%.4f, Turns: %d\n", *m.TotalCostUSD, m.NumTurns)
			}
		}
	}

	// Print tool usage summary
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("Tool Usage Summary")
	fmt.Println("============================================================")
	for i, usage := range toolUsageLog {
		fmt.Printf("\n%d. Tool: %s\n", i+1, usage["tool"])
		inputJSON, _ := json.MarshalIndent(usage["input"], "   ", "  ")
		fmt.Printf("   Input: %s\n", string(inputJSON))
		fmt.Printf("   Decision: %s\n", usage["decision"])
	}
}

// myPermissionCallback controls tool permissions based on tool type and input.
func myPermissionCallback(
	ctx context.Context,
	toolName string,
	input map[string]any,
	permCtx types.ToolPermissionContext,
) (types.PermissionResult, error) {

	fmt.Printf("\n>>> Tool Permission Request: %s\n", toolName)
	inputJSON, _ := json.Marshal(input)
	fmt.Printf("    Input: %s\n", string(inputJSON))

	// Always allow read operations
	if toolName == "Read" || toolName == "Glob" || toolName == "Grep" {
		fmt.Printf("    ALLOWED (read-only operation)\n")
		logUsage(toolName, input, "allowed")
		return &types.PermissionResultAllow{}, nil
	}

	// Check Bash commands for dangerous patterns
	if toolName == "Bash" {
		command, _ := input["command"].(string)
		dangerousPatterns := []string{"rm -rf", "sudo", "chmod 777", "dd if=", "mkfs"}

		for _, dangerous := range dangerousPatterns {
			if strings.Contains(command, dangerous) {
				fmt.Printf("    DENIED (dangerous command pattern: %s)\n", dangerous)
				logUsage(toolName, input, "denied: dangerous command")
				return &types.PermissionResultDeny{
					Message: fmt.Sprintf("Dangerous command pattern detected: %s", dangerous),
				}, nil
			}
		}

		// Allow safe bash commands
		fmt.Printf("    ALLOWED (safe bash command)\n")
		logUsage(toolName, input, "allowed")
		return &types.PermissionResultAllow{}, nil
	}

	// Deny write operations to system directories
	if toolName == "Write" || toolName == "Edit" {
		filePath, _ := input["file_path"].(string)
		if strings.HasPrefix(filePath, "/etc/") || strings.HasPrefix(filePath, "/usr/") {
			fmt.Printf("    DENIED (system directory write)\n")
			logUsage(toolName, input, "denied: system directory")
			return &types.PermissionResultDeny{
				Message: fmt.Sprintf("Cannot write to system directory: %s", filePath),
			}, nil
		}

		// Example of modifying input: redirect writes to a safe location
		if !strings.HasPrefix(filePath, "/tmp/") && !strings.HasPrefix(filePath, "./") {
			safePath := fmt.Sprintf("/tmp/safe_output_%s", strings.ReplaceAll(filePath, "/", "_"))
			fmt.Printf("    ALLOWED with modification (redirecting to %s)\n", safePath)

			modifiedInput := make(map[string]any)
			for k, v := range input {
				modifiedInput[k] = v
			}
			modifiedInput["file_path"] = safePath

			logUsage(toolName, input, fmt.Sprintf("allowed with redirect to %s", safePath))
			return &types.PermissionResultAllow{
				UpdatedInput: modifiedInput,
			}, nil
		}
	}

	// Default: allow other tools
	fmt.Printf("    ALLOWED (default)\n")
	logUsage(toolName, input, "allowed")
	return &types.PermissionResultAllow{}, nil
}

func logUsage(toolName string, input map[string]any, decision string) {
	toolUsageLog = append(toolUsageLog, map[string]any{
		"tool":     toolName,
		"input":    input,
		"decision": decision,
	})
}

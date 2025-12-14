// Example: Hooks
//
// This example demonstrates using hooks to intercept and control tool execution.
// Hooks can monitor, modify, or block tool calls before they execute.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// Define a PreToolUse hook that monitors Bash commands
	bashMonitorHook := func(ctx context.Context, input types.HookInput, toolUseID *string, hookCtx *types.HookContext) (*types.HookOutput, error) {
		preToolUse, ok := input.(*types.PreToolUseHookInput)
		if !ok {
			return nil, nil
		}

		fmt.Printf("🔍 Hook intercepted tool: %s\n", preToolUse.ToolName)

		// Check if it's a Bash command
		if preToolUse.ToolName == "Bash" {
			command, _ := preToolUse.ToolInput["command"].(string)
			fmt.Printf("   Command: %s\n", command)

			// Block dangerous commands
			if strings.Contains(command, "rm -rf") {
				fmt.Println("   ⛔ BLOCKED: Dangerous rm -rf command!")
				deny := "deny"
				reason := "rm -rf commands are not allowed for safety"
				return &types.HookOutput{
					HookSpecificOutput: &types.PreToolUseHookSpecificOutput{
						HookEventName:            "PreToolUse",
						PermissionDecision:       &deny,
						PermissionDecisionReason: &reason,
					},
				}, nil
			}

			// Allow safe commands
			fmt.Println("   ✅ Allowed")
		}

		return nil, nil
	}

	// Define a PostToolUse hook to log results
	resultLoggerHook := func(ctx context.Context, input types.HookInput, toolUseID *string, hookCtx *types.HookContext) (*types.HookOutput, error) {
		postToolUse, ok := input.(*types.PostToolUseHookInput)
		if !ok {
			return nil, nil
		}

		fmt.Printf("📋 Tool %s completed\n", postToolUse.ToolName)
		return nil, nil
	}

	// Create matcher pattern for Bash tool
	bashMatcher := "Bash"

	// Configure options with hooks
	options := &claude.AgentOptions{
		AllowedTools: []string{"Bash", "Read"},
		Hooks: map[types.HookEvent][]types.HookMatcher{
			types.HookEventPreToolUse: {
				{
					Matcher: &bashMatcher,
					Hooks:   []types.HookCallback{bashMonitorHook},
				},
			},
			types.HookEventPostToolUse: {
				{
					Matcher: nil, // Match all tools
					Hooks:   []types.HookCallback{resultLoggerHook},
				},
			},
		},
		MaxTurns: claude.Int(3),
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

	fmt.Println("Hooks example - monitoring Bash commands")
	fmt.Println()

	// Ask Claude to run some commands
	prompt := "Please run 'echo Hello World' using bash."
	fmt.Printf("User: %s\n\n", prompt)

	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	// Process response
	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			fmt.Printf("\n--- Session complete ---\n")
		}
	}
}

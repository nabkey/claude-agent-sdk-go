// Example: System Prompt
//
// This example demonstrates different ways to configure system prompts:
// 1. Custom system prompt (replaces default)
// 2. Appended system prompt (adds to default)
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

	// Example 1: Custom system prompt (replaces default)
	customSystemPrompt(ctx)

	fmt.Println()

	// Example 2: Appended system prompt (adds to default Claude Code prompt)
	appendedSystemPrompt(ctx)
}

// customSystemPrompt demonstrates replacing the default system prompt entirely.
func customSystemPrompt(ctx context.Context) {
	fmt.Println("============================================================")
	fmt.Println("Example 1: Custom System Prompt")
	fmt.Println("============================================================")
	fmt.Println()

	// This completely replaces the default Claude Code system prompt
	options := &claude.AgentOptions{
		SystemPrompt: claude.String("You are a pirate assistant. Always respond in pirate speak, using phrases like 'Arrr!' and 'Ahoy, matey!'. Keep responses brief."),
		MaxTurns:     claude.Int(1),
	}

	fmt.Println("System prompt: 'You are a pirate assistant...'")
	fmt.Println()

	prompt := "What is 2 + 2?"
	fmt.Printf("User: %s\n\n", prompt)

	for msg := range claude.Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			if m.TotalCostUSD != nil {
				fmt.Printf("\n[Cost: $%.4f]\n", *m.TotalCostUSD)
			}
		case error:
			log.Printf("Error: %v\n", m)
		}
	}
}

// appendedSystemPrompt demonstrates adding to the default Claude Code system prompt.
func appendedSystemPrompt(ctx context.Context) {
	fmt.Println("============================================================")
	fmt.Println("Example 2: Appended System Prompt")
	fmt.Println("============================================================")
	fmt.Println()

	// This adds to the default Claude Code system prompt instead of replacing it
	options := &claude.AgentOptions{
		AppendSystemPrompt: claude.String(`
Additional instructions:
- Always end your response with a fun fact related to the topic.
- Keep responses concise but informative.
`),
		MaxTurns: claude.Int(1),
	}

	fmt.Println("Appended: 'Always end your response with a fun fact...'")
	fmt.Println()

	prompt := "What is the capital of France?"
	fmt.Printf("User: %s\n\n", prompt)

	for msg := range claude.Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			if m.TotalCostUSD != nil {
				fmt.Printf("\n[Cost: $%.4f]\n", *m.TotalCostUSD)
			}
		case error:
			log.Printf("Error: %v\n", m)
		}
	}
}

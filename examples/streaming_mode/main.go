// Example: Streaming Mode (Interactive Client)
//
// This example demonstrates using the Client for interactive, multi-turn conversations.
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

	// Create client with options
	options := &claude.AgentOptions{
		MaxTurns: claude.Int(3),
	}

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	// Connect to Claude
	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	fmt.Println("Connected to Claude. Starting conversation...")
	fmt.Println()

	// First query
	fmt.Println("User: What is the capital of France?")
	if err := client.SendQuery(ctx, "What is the capital of France?"); err != nil {
		log.Fatal(err)
	}

	// Receive and print response
	for msg := range client.ReceiveResponse() {
		printMessage(msg)
	}

	// Follow-up query
	fmt.Println("\nUser: What's a famous landmark there?")
	if err := client.SendQuery(ctx, "What's a famous landmark there?"); err != nil {
		log.Fatal(err)
	}

	// Receive and print second response
	for msg := range client.ReceiveResponse() {
		printMessage(msg)
	}

	fmt.Println("\nConversation complete.")
}

func printMessage(msg types.Message) {
	switch m := msg.(type) {
	case *types.AssistantMessage:
		for _, block := range m.Content {
			if text, ok := block.(*types.TextBlock); ok {
				fmt.Printf("Claude: %s\n", text.Text)
			}
		}
	case *types.ResultMessage:
		if m.TotalCostUSD != nil {
			fmt.Printf("  [Cost: $%.4f, Turns: %d]\n", *m.TotalCostUSD, m.NumTurns)
		}
	}
}

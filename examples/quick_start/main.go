// Example: Quick Start
//
// This example demonstrates the simplest way to query Claude Code using the SDK.
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

	// Simple query with default options
	fmt.Println("Querying Claude...")

	for msg := range claude.Query(ctx, "What is 2 + 2?", nil) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			if m.TotalCostUSD != nil {
				fmt.Printf("\nCost: $%.4f\n", *m.TotalCostUSD)
			}
			fmt.Printf("Turns: %d\n", m.NumTurns)
		case error:
			log.Printf("Error: %v\n", m)
		}
	}
}

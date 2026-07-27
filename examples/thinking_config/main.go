// Example: Using ThinkingConfig and Effort options
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

	// Example 1: Adaptive thinking (model decides when to think)
	adaptiveOpts := &claude.AgentOptions{
		Thinking: types.NewThinkingAdaptive(),
		MaxTurns: claude.Int(1),
	}

	text, err := claude.QueryText(ctx, "What is 2+2?", adaptiveOpts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Adaptive:", text)

	// Example 2: Enabled thinking with budget
	enabledOpts := &claude.AgentOptions{
		Thinking: types.NewThinkingEnabled(10000),
		MaxTurns: claude.Int(1),
	}

	text, err = claude.QueryText(ctx, "Explain the halting problem", enabledOpts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Enabled:", text)

	// Example 3: Using effort level
	effortOpts := claude.DefaultAgentOptions().
		WithEffort(types.EffortLevelMax).
		WithMaxTurns(1)

	text, err = claude.QueryText(ctx, "Write a Go function to sort a linked list", effortOpts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("High effort:", text)

	// Example 4: Disabled thinking
	disabledOpts := &claude.AgentOptions{
		Thinking: types.NewThinkingDisabled(),
		MaxTurns: claude.Int(1),
	}

	text, err = claude.QueryText(ctx, "Hello", disabledOpts)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Disabled:", text)
}

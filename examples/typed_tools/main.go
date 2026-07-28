// Example: in-process MCP tools with schemas generated from Go structs.
//
// NewToolFor derives the JSON Schema from the argument type and hands the
// handler a decoded struct, so there is no hand-written schema to keep in step
// and no map[string]any unpacking.
package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// AddArgs is the input to the add tool. The jsonschema tags become field
// descriptions in the generated schema.
type AddArgs struct {
	A float64 `json:"a" jsonschema:"the first addend"`
	B float64 `json:"b" jsonschema:"the second addend"`
}

// RepeatArgs shows optional fields and slices.
type RepeatArgs struct {
	Text  string `json:"text" jsonschema:"the text to repeat"`
	Times int    `json:"times" jsonschema:"how many times to repeat it"`
}

func main() {
	ctx := context.Background()

	addTool, err := mcp.NewToolFor("add", "Add two numbers",
		func(_ context.Context, args AddArgs) (map[string]any, error) {
			return mcp.TextResult(fmt.Sprintf("%g", args.A+args.B)), nil
		})
	if err != nil {
		log.Fatal(err)
	}

	repeatTool, err := mcp.NewToolFor("repeat", "Repeat text several times",
		func(_ context.Context, args RepeatArgs) (map[string]any, error) {
			if args.Times < 1 {
				return mcp.ErrorResult("times must be at least 1"), nil
			}
			return mcp.TextResult(strings.Repeat(args.Text, args.Times)), nil
		})
	if err != nil {
		log.Fatal(err)
	}

	server := mcp.NewSDKServer("calc", "1.0.0", addTool, repeatTool)

	options := claude.DefaultAgentOptions().
		WithMCPServer("calc", server).
		WithAllowedTools("mcp__calc__add", "mcp__calc__repeat").
		WithMaxTurns(3)

	for msg := range claude.Query(ctx, "What is 17 + 25? Use the add tool.", options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Println(text.Text)
				}
			}
		case error:
			log.Printf("error: %v", m)
		}
	}
}

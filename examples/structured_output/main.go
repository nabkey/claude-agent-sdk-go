// Example: Structured Output
//
// This example demonstrates using JSON schema to constrain Claude's output.
// The --json-schema flag tells Claude to produce output matching your schema.
//
// This example uses github.com/google/jsonschema-go to generate a proper
// JSON Schema from Go struct definitions, ensuring schema correctness.
//
// Note: The structured_output field in ResultMessage is populated by the CLI
// when the agent produces valid JSON matching the schema. This feature requires
// Claude Code CLI version 2.1.0 or later.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// CalculationResult defines the expected structure of Claude's response.
type CalculationResult struct {
	Expression string   `json:"expression" jsonschema:"The mathematical expression"`
	Result     int      `json:"result" jsonschema:"The calculated result"`
	Steps      []string `json:"steps" jsonschema:"Step-by-step calculation"`
}

func main() {
	ctx := context.Background()

	fmt.Println("============================================================")
	fmt.Println("Structured Output Example")
	fmt.Println("============================================================")
	fmt.Println()
	fmt.Println("This example uses JSON schema to constrain Claude's output")
	fmt.Println("to a specific structure that can be parsed into Go structs.")
	fmt.Println("============================================================")
	fmt.Println()

	// Generate JSON schema from Go struct using google/jsonschema-go
	schema, err := jsonschema.For[CalculationResult](nil)
	if err != nil {
		log.Fatalf("Failed to generate schema: %v", err)
	}

	// Convert schema to map for SDK
	schemaJSON, err := json.Marshal(schema)
	if err != nil {
		log.Fatalf("Failed to marshal schema: %v", err)
	}
	fmt.Printf("Generated JSON Schema:\n%s\n\n", string(schemaJSON))

	var schemaMap map[string]any
	if err := json.Unmarshal(schemaJSON, &schemaMap); err != nil {
		log.Fatalf("Failed to unmarshal schema: %v", err)
	}

	options := &claude.AgentOptions{
		OutputFormat: map[string]any{
			"type":   "json_schema",
			"schema": schemaMap,
		},
		// Note: structured output uses an internal tool call, so we need at least 2 turns
		MaxTurns: claude.Int(3),
	}

	prompt := "Calculate (5 + 3) * 2. Show your work step by step."
	fmt.Printf("User: %s\n\n", prompt)

	var structuredOutput any
	var textResponse string

	for msg := range claude.Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					textResponse = text.Text
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			structuredOutput = m.StructuredOutput
			fmt.Println()
			if m.TotalCostUSD != nil {
				fmt.Printf("[Cost: $%.4f, Turns: %d]\n", *m.TotalCostUSD, m.NumTurns)
			}
		case error:
			log.Printf("Error: %v\n", m)
		}
	}

	// Parse the structured output
	fmt.Println()
	fmt.Println("============================================================")
	fmt.Println("Structured Output Result")
	fmt.Println("============================================================")

	if structuredOutput != nil {
		// Convert to JSON and then parse into our struct
		jsonBytes, err := json.Marshal(structuredOutput)
		if err != nil {
			log.Printf("Failed to marshal structured output: %v\n", err)
			return
		}

		fmt.Printf("Raw JSON: %s\n\n", string(jsonBytes))

		var calc CalculationResult
		if err := json.Unmarshal(jsonBytes, &calc); err != nil {
			log.Printf("Failed to parse response: %v\n", err)
			return
		}

		fmt.Printf("Expression: %s\n", calc.Expression)
		fmt.Printf("Result: %d\n", calc.Result)
		fmt.Println("Steps:")
		for i, step := range calc.Steps {
			fmt.Printf("  %d. %s\n", i+1, step)
		}
	} else {
		fmt.Println("No structured_output field in result.")
		fmt.Println()
		fmt.Println("The --json-schema flag was passed to constrain output.")
		fmt.Println("Claude's text response (above) should follow the schema.")
		fmt.Println()

		// Try to parse the text response as JSON
		if textResponse != "" {
			var calc CalculationResult
			if err := json.Unmarshal([]byte(textResponse), &calc); err == nil {
				fmt.Println("Successfully parsed text response as JSON:")
				fmt.Printf("  Expression: %s\n", calc.Expression)
				fmt.Printf("  Result: %d\n", calc.Result)
				fmt.Println("  Steps:")
				for i, step := range calc.Steps {
					fmt.Printf("    %d. %s\n", i+1, step)
				}
			}
		}
	}
}

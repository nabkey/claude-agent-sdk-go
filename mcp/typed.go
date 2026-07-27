package mcp

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/jsonschema-go/jsonschema"
)

// TypedToolFunc is a tool handler whose argument is a Go struct.
type TypedToolFunc[T any] func(ctx context.Context, args T) (map[string]any, error)

// NewToolFor builds a tool whose input schema is generated from a Go struct,
// and whose handler receives that struct already decoded.
//
// This is the counterpart to the reference SDKs' typed tool decorators: it
// removes the hand-written JSON Schema and the map[string]any unpacking that
// NewTool requires, and keeps the schema in step with the struct
// automatically.
//
// Field documentation comes from the `jsonschema` struct tag, and field names
// from the `json` tag:
//
//	type AddArgs struct {
//	    A float64 `json:"a" jsonschema:"the first addend"`
//	    B float64 `json:"b" jsonschema:"the second addend"`
//	}
//
//	tool, err := mcp.NewToolFor("add", "Add two numbers",
//	    func(ctx context.Context, args AddArgs) (map[string]any, error) {
//	        return mcp.TextResult(fmt.Sprintf("%v", args.A+args.B)), nil
//	    })
//
// An error is returned only when a schema cannot be derived from T, which is a
// programming error rather than a runtime condition.
func NewToolFor[T any](name, description string, handler TypedToolFunc[T]) (Tool, error) {
	schema, err := jsonschema.For[T](nil)
	if err != nil {
		return Tool{}, fmt.Errorf("mcp: cannot derive schema for tool %q: %w", name, err)
	}

	encoded, err := json.Marshal(schema)
	if err != nil {
		return Tool{}, fmt.Errorf("mcp: cannot encode schema for tool %q: %w", name, err)
	}

	var inputSchema map[string]any
	if err := json.Unmarshal(encoded, &inputSchema); err != nil {
		return Tool{}, fmt.Errorf("mcp: cannot decode schema for tool %q: %w", name, err)
	}

	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler: func(ctx context.Context, raw map[string]any) (map[string]any, error) {
			var args T
			// Round-trip through JSON so the decoded value honors the same
			// json tags the schema was generated from.
			encoded, err := json.Marshal(raw)
			if err != nil {
				return ErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
			}
			if err := json.Unmarshal(encoded, &args); err != nil {
				return ErrorResult(fmt.Sprintf("invalid arguments: %v", err)), nil
			}
			return handler(ctx, args)
		},
	}, nil
}

// MustToolFor is NewToolFor for package-level tool definitions, panicking if
// the schema cannot be derived.
//
// Use it where a failure means the program is misconfigured and cannot
// usefully continue:
//
//	var addTool = mcp.MustToolFor("add", "Add two numbers", addHandler)
func MustToolFor[T any](name, description string, handler TypedToolFunc[T]) Tool {
	tool, err := NewToolFor(name, description, handler)
	if err != nil {
		panic(err)
	}
	return tool
}

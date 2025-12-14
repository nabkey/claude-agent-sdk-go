// Package mcp provides Model Context Protocol (MCP) server implementations.
package mcp

import (
	"context"
	"fmt"
	"reflect"
)

// ToolFunc is the function signature for MCP tool handlers.
type ToolFunc func(ctx context.Context, args map[string]any) (map[string]any, error)

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     ToolFunc
}

// NewTool creates a new MCP tool definition.
//
// Parameters:
//   - name: Unique identifier for the tool. This is what Claude will use to reference the tool.
//   - description: Human-readable description of what the tool does.
//   - inputSchema: JSON Schema defining the tool's input parameters.
//   - handler: Function that implements the tool's behavior.
//
// Example:
//
//	greetTool := mcp.NewTool(
//	    "greet",
//	    "Greet a user by name",
//	    map[string]any{
//	        "type": "object",
//	        "properties": map[string]any{
//	            "name": map[string]any{"type": "string"},
//	        },
//	        "required": []string{"name"},
//	    },
//	    func(ctx context.Context, args map[string]any) (map[string]any, error) {
//	        name := args["name"].(string)
//	        return map[string]any{
//	            "content": []map[string]any{
//	                {"type": "text", "text": fmt.Sprintf("Hello, %s!", name)},
//	            },
//	        }, nil
//	    },
//	)
func NewTool(name, description string, inputSchema map[string]any, handler ToolFunc) Tool {
	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
	}
}

// NewToolSimple creates a tool with a simplified schema definition.
// The schema parameter maps parameter names to their types.
//
// Supported types: string, int, float64, bool
//
// Example:
//
//	addTool := mcp.NewToolSimple(
//	    "add",
//	    "Add two numbers",
//	    map[string]any{
//	        "a": float64(0),
//	        "b": float64(0),
//	    },
//	    func(ctx context.Context, args map[string]any) (map[string]any, error) {
//	        a := args["a"].(float64)
//	        b := args["b"].(float64)
//	        return TextResult(fmt.Sprintf("Result: %f", a+b)), nil
//	    },
//	)
func NewToolSimple(name, description string, schema map[string]any, handler ToolFunc) Tool {
	// Convert simple schema to JSON Schema
	properties := make(map[string]any)
	required := make([]string, 0, len(schema))

	for paramName, paramType := range schema {
		var jsonType string
		switch reflect.TypeOf(paramType).Kind() {
		case reflect.String:
			jsonType = "string"
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			jsonType = "integer"
		case reflect.Float32, reflect.Float64:
			jsonType = "number"
		case reflect.Bool:
			jsonType = "boolean"
		default:
			jsonType = "string"
		}
		properties[paramName] = map[string]any{"type": jsonType}
		required = append(required, paramName)
	}

	inputSchema := map[string]any{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}

	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
	}
}

// TextResult creates a standard text result for tool responses.
//
// Example:
//
//	return mcp.TextResult("Hello, World!"), nil
func TextResult(text string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": text},
		},
	}
}

// ErrorResult creates an error result for tool responses.
//
// Example:
//
//	return mcp.ErrorResult("Division by zero"), nil
func ErrorResult(message string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "text", "text": message},
		},
		"isError": true,
	}
}

// ImageResult creates an image result for tool responses.
//
// Example:
//
//	return mcp.ImageResult(imageBase64, "image/png"), nil
func ImageResult(base64Data, mimeType string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{
				"type":     "image",
				"data":     base64Data,
				"mimeType": mimeType,
			},
		},
	}
}

// MultiResult combines multiple content items into a single result.
func MultiResult(items ...map[string]any) map[string]any {
	content := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if c, ok := item["content"].([]map[string]any); ok {
			content = append(content, c...)
		}
	}
	return map[string]any{"content": content}
}

// GetString safely extracts a string from args.
func GetString(args map[string]any, key string) (string, error) {
	v, ok := args[key]
	if !ok {
		return "", fmt.Errorf("missing required parameter: %s", key)
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("parameter %s must be a string", key)
	}
	return s, nil
}

// GetFloat safely extracts a float64 from args.
func GetFloat(args map[string]any, key string) (float64, error) {
	v, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("missing required parameter: %s", key)
	}
	switch n := v.(type) {
	case float64:
		return n, nil
	case float32:
		return float64(n), nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	default:
		return 0, fmt.Errorf("parameter %s must be a number", key)
	}
}

// GetInt safely extracts an int from args.
func GetInt(args map[string]any, key string) (int, error) {
	f, err := GetFloat(args, key)
	if err != nil {
		return 0, err
	}
	return int(f), nil
}

// GetBool safely extracts a bool from args.
func GetBool(args map[string]any, key string) (bool, error) {
	v, ok := args[key]
	if !ok {
		return false, fmt.Errorf("missing required parameter: %s", key)
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("parameter %s must be a boolean", key)
	}
	return b, nil
}

// GetStringOptional extracts a string with a default value.
func GetStringOptional(args map[string]any, key string, defaultValue string) string {
	v, ok := args[key]
	if !ok {
		return defaultValue
	}
	s, ok := v.(string)
	if !ok {
		return defaultValue
	}
	return s
}

// GetFloatOptional extracts a float64 with a default value.
func GetFloatOptional(args map[string]any, key string, defaultValue float64) float64 {
	v, ok := args[key]
	if !ok {
		return defaultValue
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return defaultValue
	}
}

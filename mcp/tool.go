// Package mcp provides Model Context Protocol (MCP) server implementations.
package mcp

import (
	"context"
	"fmt"
	"reflect"
)

// ToolFunc is the function signature for MCP tool handlers.
type ToolFunc func(ctx context.Context, args map[string]any) (map[string]any, error)

// ToolAnnotations describes a tool's behavior to the client.
type ToolAnnotations struct {
	// Title is a human-readable name for the tool.
	Title string `json:"title,omitempty"`
	// ReadOnlyHint marks a tool that does not modify its environment.
	ReadOnlyHint *bool `json:"readOnlyHint,omitempty"`
	// DestructiveHint marks a tool that may perform destructive updates.
	DestructiveHint *bool `json:"destructiveHint,omitempty"`
	// IdempotentHint marks a tool where repeated calls with the same
	// arguments have no additional effect.
	IdempotentHint *bool `json:"idempotentHint,omitempty"`
	// OpenWorldHint marks a tool that interacts with external entities.
	OpenWorldHint *bool `json:"openWorldHint,omitempty"`
}

// Tool represents an MCP tool definition.
type Tool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     ToolFunc
	// Annotations describe the tool's behavior to the client.
	Annotations *ToolAnnotations
	// MaxResultSizeChars caps this tool's result before the CLI spills it to
	// a file. Zero uses the CLI default.
	//
	// The MCP schema strips unknown annotation fields, so this is carried in
	// the tool's _meta under an Anthropic-namespaced key.
	MaxResultSizeChars int
	// AlwaysLoad keeps this tool in the prompt rather than deferring it
	// behind tool search. Set it per server with mcp.WithAlwaysLoad; the two
	// are OR'd.
	AlwaysLoad bool
}

// meta renders the Anthropic-namespaced _meta payload, or nil if nothing is
// set. serverAlwaysLoad is the server-wide default, OR'd with the tool's own.
func (t Tool) meta(serverAlwaysLoad bool) map[string]any {
	meta := map[string]any{}
	if t.MaxResultSizeChars > 0 {
		meta["anthropic/maxResultSizeChars"] = t.MaxResultSizeChars
	}
	if t.AlwaysLoad || serverAlwaysLoad {
		meta["anthropic/alwaysLoad"] = true
	}
	if len(meta) == 0 {
		return nil
	}
	return meta
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

// NewToolWithAnnotations creates a new MCP tool definition with annotations.
//
// Annotations provide metadata about the tool's behavior, such as whether it is
// read-only, destructive, or accesses external systems.
func NewToolWithAnnotations(name, description string, inputSchema map[string]any, handler ToolFunc, annotations *ToolAnnotations) Tool {
	return Tool{
		Name:        name,
		Description: description,
		InputSchema: inputSchema,
		Handler:     handler,
		Annotations: annotations,
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

// MultiResult combines several tool results into one.
//
// Content is accepted in either the []map[string]any form the helpers in this
// package produce or the []any form that survives a JSON round trip, so
// results assembled from decoded payloads combine correctly too.
func MultiResult(items ...map[string]any) map[string]any {
	content := make([]map[string]any, 0, len(items))
	isError := false

	for _, item := range items {
		if flag, ok := item["isError"].(bool); ok && flag {
			isError = true
		}
		switch c := item["content"].(type) {
		case []map[string]any:
			content = append(content, c...)
		case []any:
			for _, block := range c {
				if m, ok := block.(map[string]any); ok {
					content = append(content, m)
				}
			}
		}
	}

	result := map[string]any{"content": content}
	if isError {
		result["isError"] = true
	}
	return result
}

// ResourceLinkResult creates a result referencing an external resource.
func ResourceLinkResult(name, uri, description string) map[string]any {
	link := map[string]any{"type": "resource_link", "uri": uri}
	if name != "" {
		link["name"] = name
	}
	if description != "" {
		link["description"] = description
	}
	return map[string]any{"content": []map[string]any{link}}
}

// EmbeddedResourceResult creates a result embedding a text resource.
func EmbeddedResourceResult(uri, mimeType, text string) map[string]any {
	resource := map[string]any{"uri": uri, "text": text}
	if mimeType != "" {
		resource["mimeType"] = mimeType
	}
	return map[string]any{
		"content": []map[string]any{{"type": "resource", "resource": resource}},
	}
}

// AudioResult creates an audio result.
func AudioResult(base64Data, mimeType string) map[string]any {
	return map[string]any{
		"content": []map[string]any{
			{"type": "audio", "data": base64Data, "mimeType": mimeType},
		},
	}
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

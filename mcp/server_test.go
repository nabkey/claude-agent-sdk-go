package mcp

import (
	"context"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestNewSDKServer(t *testing.T) {
	readOnly := true
	greet := NewToolWithAnnotations("greet", "Greet a user",
		map[string]any{"type": "object"},
		func(context.Context, map[string]any) (map[string]any, error) {
			return TextResult("hi"), nil
		},
		&ToolAnnotations{ReadOnlyHint: &readOnly})
	greet.MaxResultSizeChars = 2048

	config := NewSDKServer("my-tools", "1.0.0", greet)

	if config.Type != "sdk" {
		t.Errorf("Type = %q, want sdk", config.Type)
	}
	if config.Name != "my-tools" || config.Version != "1.0.0" {
		t.Errorf("unexpected identity: %+v", config)
	}
	if config.Instance == nil {
		t.Fatal("expected a protocol handler instance")
	}

	// The config is what AgentOptions.MCPServers accepts.
	var _ types.MCPServerConfig = config
}

// The handler is what the protocol layer dispatches against, so every field a
// tool declares has to survive the conversion.
func TestSDKServerHandlerCarriesToolFields(t *testing.T) {
	readOnly := true
	tool := NewToolWithAnnotations("greet", "Greet a user",
		map[string]any{"type": "object", "properties": map[string]any{}},
		func(context.Context, map[string]any) (map[string]any, error) {
			return TextResult("hi"), nil
		},
		&ToolAnnotations{ReadOnlyHint: &readOnly})
	tool.MaxResultSizeChars = 2048

	server := &SDKServer{name: "my-tools", version: "1.0.0", tools: []Tool{tool}}
	handler := server.toHandler()

	if handler.Name != "my-tools" || handler.Version != "1.0.0" {
		t.Errorf("unexpected handler identity: %+v", handler)
	}
	if len(handler.Tools) != 1 {
		t.Fatalf("expected one tool, got %d", len(handler.Tools))
	}

	got := handler.Tools[0]
	if got.Name != "greet" || got.Description != "Greet a user" {
		t.Errorf("unexpected tool: %+v", got)
	}
	if got.InputSchema == nil || got.Handler == nil {
		t.Error("expected the schema and handler to be carried")
	}
	if got.Annotations == nil {
		t.Error("expected the annotations to be carried")
	}
	if got.Meta["anthropic/maxResultSizeChars"] != 2048 {
		t.Errorf("Meta = %v", got.Meta)
	}
}

// Each tool's handler must be bound to its own tool: a loop variable captured
// by reference would give every tool the last one's behavior.
func TestSDKServerHandlersAreBoundPerTool(t *testing.T) {
	server := &SDKServer{
		name: "my-tools",
		tools: []Tool{
			NewTool("first", "", nil, func(context.Context, map[string]any) (map[string]any, error) {
				return TextResult("first"), nil
			}),
			NewTool("second", "", nil, func(context.Context, map[string]any) (map[string]any, error) {
				return TextResult("second"), nil
			}),
		},
	}

	handler := server.toHandler()
	for i, want := range []string{"first", "second"} {
		result, err := handler.Tools[i].Handler(context.Background(), nil)
		if err != nil {
			t.Fatalf("tool %d: %v", i, err)
		}
		content, _ := result["content"].([]map[string]any)
		if len(content) != 1 {
			t.Fatalf("tool %d content = %v", i, result["content"])
		}
		if got := content[0]["text"]; got != want {
			t.Errorf("tool %d returned %v, want %q", i, got, want)
		}
	}
}

func TestSDKServerAccessors(t *testing.T) {
	tool := NewTool("greet", "", nil, nil)
	server := &SDKServer{name: "my-tools", version: "2.0.0", tools: []Tool{tool}}

	if server.Name() != "my-tools" || server.Version() != "2.0.0" {
		t.Errorf("unexpected accessors: %q, %q", server.Name(), server.Version())
	}
	if len(server.Tools()) != 1 || server.Tools()[0].Name != "greet" {
		t.Errorf("Tools() = %+v", server.Tools())
	}
}

func TestNewSDKServerWithNoTools(t *testing.T) {
	config := NewSDKServer("empty", "1.0.0")
	if config.Instance == nil {
		t.Fatal("a server with no tools is still a valid server")
	}
}

// Per-server configuration has to reach both the protocol handler and the
// config the CLI is told about.
func TestNewSDKServerWithOptions(t *testing.T) {
	config := NewSDKServerWithOptions("my-tools", "1.0.0",
		[]Tool{NewTool("greet", "", nil, nil)},
		[]ServerOption{
			WithInstructions("prefer the greet tool"),
			WithToolTimeout(30000),
			WithAlwaysLoad(),
		})

	if config.TimeoutMS != 30000 {
		t.Errorf("TimeoutMS = %d, want 30000", config.TimeoutMS)
	}

	handler, ok := config.Instance.(interface {
		HandleRequest(context.Context, map[string]any) map[string]any
	})
	if !ok {
		t.Fatalf("Instance = %T, want a protocol handler", config.Instance)
	}

	// Instructions reach the model through the MCP initialize response.
	response := handler.HandleRequest(context.Background(), map[string]any{
		"method": "initialize", "id": float64(1),
	})
	result, _ := response["result"].(map[string]any)
	if result["instructions"] != "prefer the greet tool" {
		t.Errorf("instructions = %v", result["instructions"])
	}

	// The server-wide always-load applies to each tool's _meta.
	response = handler.HandleRequest(context.Background(), map[string]any{
		"method": "tools/list", "id": float64(2),
	})
	tools, _ := response["result"].(map[string]any)["tools"].([]map[string]any)
	if len(tools) != 1 {
		t.Fatalf("tools = %v", tools)
	}
	meta, _ := tools[0]["_meta"].(map[string]any)
	if meta["anthropic/alwaysLoad"] != true {
		t.Errorf("_meta = %v, want alwaysLoad", meta)
	}
}

// Without instructions the MCP initialize response must not carry an empty
// block, which the model would otherwise see as a blank instruction.
func TestSDKServerOmitsEmptyInstructions(t *testing.T) {
	config := NewSDKServer("my-tools", "1.0.0")
	handler, ok := config.Instance.(interface {
		HandleRequest(context.Context, map[string]any) map[string]any
	})
	if !ok {
		t.Fatalf("Instance = %T, want a protocol handler", config.Instance)
	}

	response := handler.HandleRequest(context.Background(), map[string]any{
		"method": "initialize", "id": float64(1),
	})
	result, _ := response["result"].(map[string]any)
	if _, ok := result["instructions"]; ok {
		t.Errorf("instructions must be omitted, got %v", result["instructions"])
	}
	if config.TimeoutMS != 0 {
		t.Errorf("TimeoutMS = %d, want 0", config.TimeoutMS)
	}
}

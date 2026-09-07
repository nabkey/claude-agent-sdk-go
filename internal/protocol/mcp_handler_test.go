package protocol

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func testHandler() *MCPServerHandler {
	return &MCPServerHandler{
		Name:    "my-tools",
		Version: "1.0.0",
		Tools: []MCPTool{
			{
				Name:        "greet",
				Description: "Greet a user",
				InputSchema: map[string]any{"type": "object"},
				Handler: func(_ context.Context, args map[string]any) (map[string]any, error) {
					name, _ := args["name"].(string)
					return map[string]any{
						"content": []map[string]any{{"type": "text", "text": "Hello, " + name}},
					}, nil
				},
			},
			{
				Name: "boom",
				Handler: func(context.Context, map[string]any) (map[string]any, error) {
					return nil, errors.New("tool exploded")
				},
			},
		},
	}
}

func TestMCPHandlerInitialize(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "initialize", "id": float64(1),
	})

	if response["jsonrpc"] != "2.0" || response["id"] != float64(1) {
		t.Errorf("envelope = %v", response)
	}
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", response["result"])
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "my-tools" || info["version"] != "1.0.0" {
		t.Errorf("serverInfo = %v", info)
	}
	capabilities, _ := result["capabilities"].(map[string]any)
	if _, ok := capabilities["tools"]; !ok {
		t.Errorf("capabilities = %v", result["capabilities"])
	}
}

func TestMCPHandlerToolsList(t *testing.T) {
	handler := testHandler()
	handler.Tools[0].Meta = map[string]any{"anthropic/maxResultSizeChars": 4096}
	handler.Tools[0].Annotations = map[string]any{"readOnlyHint": true}

	response := handler.HandleRequest(context.Background(), map[string]any{
		"method": "tools/list", "id": float64(2),
	})

	tools, ok := response["result"].(map[string]any)["tools"].([]map[string]any)
	if !ok || len(tools) != 2 {
		t.Fatalf("tools = %v", response["result"])
	}
	if tools[0]["name"] != "greet" || tools[0]["description"] != "Greet a user" {
		t.Errorf("tools[0] = %v", tools[0])
	}
	if tools[0]["_meta"] == nil || tools[0]["annotations"] == nil {
		t.Errorf("tools[0] lost its metadata: %v", tools[0])
	}
	// A tool that declares neither must not carry empty keys.
	if _, ok := tools[1]["_meta"]; ok {
		t.Errorf("tools[1] = %v, want no _meta", tools[1])
	}
	if _, ok := tools[1]["annotations"]; ok {
		t.Errorf("tools[1] = %v, want no annotations", tools[1])
	}
}

func TestMCPHandlerToolsCall(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "tools/call", "id": float64(3),
		"params": map[string]any{
			"name":      "greet",
			"arguments": map[string]any{"name": "Ada"},
		},
	})

	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("result = %v", response)
	}
	content, _ := result["content"].([]map[string]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", result["content"])
	}
	if content[0]["text"] != "Hello, Ada" {
		t.Errorf("content = %v", content)
	}
}

// A handler that fails becomes a JSON-RPC error, so the CLI can report it
// rather than waiting for a response that never comes.
func TestMCPHandlerToolsCallError(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "tools/call", "id": float64(4),
		"params": map[string]any{"name": "boom"},
	})

	errObj, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected an error envelope, got %v", response)
	}
	if errObj["message"] != "tool exploded" {
		t.Errorf("message = %v", errObj["message"])
	}
	if _, ok := response["result"]; ok {
		t.Error("an error response must not also carry a result")
	}
}

func TestMCPHandlerUnknownTool(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "tools/call", "id": float64(5),
		"params": map[string]any{"name": "nonexistent"},
	})

	if _, ok := response["error"].(map[string]any); !ok {
		t.Errorf("expected an error for an unknown tool, got %v", response)
	}
}

func TestMCPHandlerUnknownMethod(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "resources/list", "id": float64(6),
	})

	if _, ok := response["error"].(map[string]any); !ok {
		t.Errorf("expected an error for an unsupported method, got %v", response)
	}
}

// A JSON-RPC notification carries no id, but the CLI wraps it in a control
// request that still needs an answer, so it is acknowledged rather than
// dropped -- and with an id, since a response without one is malformed.
func TestMCPHandlerNotification(t *testing.T) {
	response := testHandler().HandleRequest(context.Background(), map[string]any{
		"method": "notifications/initialized",
	})

	if response == nil {
		t.Fatal("the control request must still be answered")
	}
	if response["id"] != 0 {
		t.Errorf("id = %v, want 0", response["id"])
	}
	if _, ok := response["result"].(map[string]any); !ok {
		t.Errorf("result = %v, want an empty object", response["result"])
	}
}

func TestMarshalUserInput(t *testing.T) {
	data, err := MarshalUserInput("hello", "sess-1")
	if err != nil {
		t.Fatalf("MarshalUserInput: %v", err)
	}
	if got := string(data); got == "" {
		t.Fatal("expected a serialized frame")
	}

	// The frame has to be the user-turn shape the CLI reads off stdin.
	var frame map[string]any
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatalf("frame is not JSON: %v", err)
	}
	if frame["type"] != "user" || frame["session_id"] != "sess-1" {
		t.Errorf("frame = %v", frame)
	}
	message, _ := frame["message"].(map[string]any)
	if message["role"] != "user" || message["content"] != "hello" {
		t.Errorf("message = %v", message)
	}
}

func TestParseChannelMessage(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":        "channel_message",
		"server_name": "slack",
		"content":     "a teammate replied",
		"data":        map[string]any{"thread": "T-1"},
		"uuid":        "u-1",
		"session_id":  "s-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	channel, ok := msg.(*types.ChannelMessage)
	if !ok {
		t.Fatalf("expected *types.ChannelMessage, got %T", msg)
	}
	if channel.ServerName != "slack" || channel.Content != "a teammate replied" {
		t.Errorf("channel = %+v", channel)
	}
	if channel.Data["thread"] != "T-1" {
		t.Errorf("Data = %v", channel.Data)
	}
}

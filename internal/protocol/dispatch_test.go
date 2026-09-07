package protocol

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// newDispatchQuery builds a query with the host callbacks a CLI can drive.
func newDispatchQuery(t *testing.T, ft *fakeTransport, opts *QueryOptions) (*Query, context.Context, func()) {
	t.Helper()
	opts.Transport = ft
	opts.IsStreamingMode = true
	opts.InitializeTimeout = 2 * time.Second

	q := NewQuery(opts)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	q.Start(ctx)
	return q, ctx, func() {
		cancel()
		_ = q.Close()
	}
}

// pushRequest sends a CLI-initiated control request and returns its id.
func pushRequest(ft *fakeTransport, requestID string, request map[string]any) {
	ft.msgChan <- map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}
}

// responseFor returns the payload of the control response for a request id.
func responseFor(t *testing.T, ft *fakeTransport, requestID string) map[string]any {
	t.Helper()
	for _, frame := range ft.responses() {
		if frame["request_id"] == requestID {
			payload, _ := frame["response"].(map[string]any)
			return payload
		}
	}
	t.Fatalf("no control response for %q", requestID)
	return nil
}

// subtypeFor returns the success/error subtype of a control response.
func subtypeFor(t *testing.T, ft *fakeTransport, requestID string) string {
	t.Helper()
	for _, frame := range ft.responses() {
		if frame["request_id"] == requestID {
			subtype, _ := frame["subtype"].(string)
			return subtype
		}
	}
	t.Fatalf("no control response for %q", requestID)
	return ""
}

func TestElicitationDispatch(t *testing.T) {
	ft := newFakeTransport()

	var got types.ElicitationRequest
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{
		OnElicitation: func(_ context.Context, req types.ElicitationRequest) (types.ElicitationResult, error) {
			got = req
			return types.ElicitationResult{
				Action:  types.ElicitationAccept,
				Content: map[string]any{"team": "core"},
			}, nil
		},
	})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype":          "elicitation",
		"mcp_server_name":  "linear",
		"mode":             "form",
		"message":          "pick a team",
		"requested_schema": map[string]any{"type": "object"},
		"elicitation_id":   "e-1",
		"title":            "Choose a team",
		"display_name":     "Linear",
		"description":      "so issues land in the right place",
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	if got.ServerName != "linear" || got.Mode != types.ElicitationModeForm {
		t.Errorf("request = %+v", got)
	}
	if got.RequestedSchema["type"] != "object" {
		t.Errorf("RequestedSchema = %v", got.RequestedSchema)
	}
	if got.ElicitationID != "e-1" || got.Title != "Choose a team" {
		t.Errorf("request = %+v", got)
	}
	if got.DisplayName != "Linear" || got.Description == "" {
		t.Errorf("permission display fields = %+v", got)
	}
	if got.Raw == nil {
		t.Error("expected the raw request to be retained")
	}

	payload := responseFor(t, ft, "req-1")
	if payload["action"] != "accept" {
		t.Errorf("action = %v, want accept", payload["action"])
	}
	content, _ := payload["content"].(map[string]any)
	if content["team"] != "core" {
		t.Errorf("content = %v", payload["content"])
	}
}

// An unhandled elicitation is declined rather than left hanging: the MCP
// server is blocked waiting for an answer.
func TestElicitationDeclinedWithoutCallback(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype": "elicitation", "mcp_server_name": "linear",
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	if payload := responseFor(t, ft, "req-1"); payload["action"] != "decline" {
		t.Errorf("action = %v, want decline", payload["action"])
	}
}

func TestUserDialogDispatch(t *testing.T) {
	ft := newFakeTransport()

	var got types.UserDialogRequest
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{
		OnUserDialog: func(_ context.Context, req types.UserDialogRequest) (types.UserDialogResult, error) {
			got = req
			return types.UserDialogResult{
				Behavior: types.UserDialogCompleted,
				Result:   map[string]any{"choice": "yes"},
			}, nil
		},
	})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype":     "request_user_dialog",
		"dialog_kind": "confirm",
		"payload":     map[string]any{"question": "proceed?"},
		"tool_use_id": "tool-1",
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	if got.DialogKind != "confirm" || got.Payload["question"] != "proceed?" {
		t.Errorf("request = %+v", got)
	}
	if got.ToolUseID == nil || *got.ToolUseID != "tool-1" {
		t.Errorf("ToolUseID = %v", got.ToolUseID)
	}

	payload := responseFor(t, ft, "req-1")
	if payload["behavior"] != "completed" {
		t.Errorf("behavior = %v", payload["behavior"])
	}
	result, _ := payload["result"].(map[string]any)
	if result["choice"] != "yes" {
		t.Errorf("result = %v", payload["result"])
	}
}

// On a multi-client session another attached client may be the declared
// renderer, so a host with no callback must not settle the dialog.
func TestUserDialogErrorsWithoutCallback(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype": "request_user_dialog", "dialog_kind": "confirm",
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	if got := subtypeFor(t, ft, "req-1"); got != "error" {
		t.Errorf("subtype = %q, want error", got)
	}
}

// A callback that fails must produce an error response rather than leaving
// the CLI waiting.
func TestElicitationCallbackErrorBecomesAnErrorResponse(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{
		OnElicitation: func(context.Context, types.ElicitationRequest) (types.ElicitationResult, error) {
			return types.ElicitationResult{}, errors.New("host unavailable")
		},
	})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{"subtype": "elicitation"})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	if got := subtypeFor(t, ft, "req-1"); got != "error" {
		t.Errorf("subtype = %q, want error", got)
	}
}

// An mcp_message names the server it targets, so an unknown one has to come
// back as a JSON-RPC error rather than a dropped request.
func TestMCPMessageForUnknownServer(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{
		SDKMCPServers: map[string]*MCPServerHandler{"known": testHandler()},
	})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype":     "mcp_message",
		"server_name": "unknown",
		"message":     map[string]any{"method": "tools/list", "id": float64(1)},
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	payload := responseFor(t, ft, "req-1")
	response, _ := payload["mcp_response"].(map[string]any)
	errObj, ok := response["error"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_response = %v", payload["mcp_response"])
	}
	if errObj["code"] != float64(-32601) {
		t.Errorf("code = %v", errObj["code"])
	}
}

// A tool call routed through the control channel has to reach the registered
// handler and come back as an mcp_response.
func TestMCPMessageDispatch(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newDispatchQuery(t, ft, &QueryOptions{
		SDKMCPServers: map[string]*MCPServerHandler{"my-tools": testHandler()},
	})
	defer cleanup()
	_ = q

	pushRequest(ft, "req-1", map[string]any{
		"subtype":     "mcp_message",
		"server_name": "my-tools",
		"message": map[string]any{
			"method": "tools/call", "id": float64(1),
			"params": map[string]any{
				"name": "greet", "arguments": map[string]any{"name": "Ada"},
			},
		},
	})
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	payload := responseFor(t, ft, "req-1")
	response, _ := payload["mcp_response"].(map[string]any)
	result, ok := response["result"].(map[string]any)
	if !ok {
		t.Fatalf("mcp_response = %v", payload["mcp_response"])
	}
	// The response has been through JSON, so blocks arrive as []any.
	content, _ := result["content"].([]any)
	if len(content) != 1 {
		t.Fatalf("content = %v", result["content"])
	}
	block, _ := content[0].(map[string]any)
	if block["text"] != "Hello, Ada" {
		t.Errorf("block = %v", block)
	}
}

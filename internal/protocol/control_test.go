package protocol

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// fakeTransport captures everything the Query writes and lets a test script
// the control responses that come back.
type fakeTransport struct {
	mu      sync.Mutex
	written []string

	msgChan chan map[string]any
	errChan chan error

	// respond, when set, is called with each outgoing control request and may
	// return a response payload to feed back to the Query.
	respond func(requestID string, request map[string]any) map[string]any
}

func newFakeTransport() *fakeTransport {
	return &fakeTransport{
		msgChan: make(chan map[string]any, 16),
		errChan: make(chan error, 1),
	}
}

func (f *fakeTransport) Connect(context.Context) error { return nil }
func (f *fakeTransport) EndInput() error               { return nil }
func (f *fakeTransport) IsReady() bool                 { return true }

func (f *fakeTransport) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.msgChan:
	default:
	}
	return nil
}

func (f *fakeTransport) ReadMessages(context.Context) (<-chan map[string]any, <-chan error) {
	return f.msgChan, f.errChan
}

func (f *fakeTransport) Write(_ context.Context, data string) error {
	f.mu.Lock()
	f.written = append(f.written, data)
	respond := f.respond
	f.mu.Unlock()

	var frame map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &frame); err != nil {
		return nil
	}
	if frame["type"] != "control_request" || respond == nil {
		return nil
	}

	requestID, _ := frame["request_id"].(string)
	request, _ := frame["request"].(map[string]any)
	payload := respond(requestID, request)

	f.msgChan <- map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   payload,
		},
	}
	return nil
}

// responses returns every control response written, decoded.
func (f *fakeTransport) responses() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []map[string]any
	for _, raw := range f.written {
		var frame map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &frame); err != nil {
			continue
		}
		if frame["type"] != "control_response" {
			continue
		}
		if response, ok := frame["response"].(map[string]any); ok {
			out = append(out, response)
		}
	}
	return out
}

// requests returns every control request written, decoded.
func (f *fakeTransport) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()

	var out []map[string]any
	for _, raw := range f.written {
		var frame map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &frame); err != nil {
			continue
		}
		if frame["type"] != "control_request" {
			continue
		}
		if request, ok := frame["request"].(map[string]any); ok {
			out = append(out, request)
		}
	}
	return out
}

func newTestQuery(t *testing.T, ft *fakeTransport) (*Query, context.Context, func()) {
	t.Helper()
	q := NewQuery(&QueryOptions{
		Transport:         ft,
		IsStreamingMode:   true,
		InitializeTimeout: 2 * time.Second,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	q.Start(ctx)
	return q, ctx, func() {
		cancel()
		_ = q.Close()
	}
}

// The CLI's subtype is `mcp_status`, and the response wraps servers under
// `mcpServers` -- not `servers`.
func TestGetMCPStatusWireFormat(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(_ string, _ map[string]any) map[string]any {
		return map[string]any{
			"mcpServers": []any{
				map[string]any{
					"name":   "my-server",
					"status": "connected",
					"scope":  "project",
					"serverInfo": map[string]any{
						"name":    "my-server-impl",
						"version": "1.2.3",
					},
					"tools": []any{
						map[string]any{
							"name":        "do_thing",
							"description": "Does the thing",
							"annotations": map[string]any{"readOnly": true},
						},
					},
				},
			},
		}
	}

	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	status, err := q.GetMCPStatus(ctx)
	if err != nil {
		t.Fatalf("GetMCPStatus: %v", err)
	}

	reqs := ft.requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 control request, got %d", len(reqs))
	}
	if got := reqs[0]["subtype"]; got != "mcp_status" {
		t.Errorf("subtype = %v, want mcp_status", got)
	}

	if len(status.Servers) != 1 {
		t.Fatalf("expected 1 server, got %d", len(status.Servers))
	}
	s := status.Servers[0]
	if s.Name != "my-server" {
		t.Errorf("Name = %q", s.Name)
	}
	if s.Status != types.McpServerConnectionStatusConnected {
		t.Errorf("Status = %q", s.Status)
	}
	if s.Scope == nil || *s.Scope != "project" {
		t.Errorf("Scope = %v", s.Scope)
	}
	if s.ServerInfo == nil || s.ServerInfo.Name != "my-server-impl" || s.ServerInfo.Version != "1.2.3" {
		t.Errorf("ServerInfo = %+v", s.ServerInfo)
	}
	if len(s.Tools) != 1 || s.Tools[0].Name != "do_thing" {
		t.Fatalf("Tools = %+v", s.Tools)
	}
	if s.Tools[0].Annotations == nil || s.Tools[0].Annotations.ReadOnly == nil ||
		!*s.Tools[0].Annotations.ReadOnly {
		t.Errorf("Annotations = %+v", s.Tools[0].Annotations)
	}
}

// mcp_reconnect and mcp_toggle use camelCase `serverName`, unlike stop_task
// (task_id) and rewind_files (user_message_id).
func TestMCPControlRequestsUseCamelCaseServerName(t *testing.T) {
	tests := []struct {
		name        string
		call        func(*Query, context.Context) error
		wantSubtype string
		wantExtra   map[string]any
	}{
		{
			name:        "mcp_reconnect",
			call:        func(q *Query, ctx context.Context) error { return q.ReconnectMCPServer(ctx, "srv") },
			wantSubtype: "mcp_reconnect",
		},
		{
			name:        "mcp_toggle",
			call:        func(q *Query, ctx context.Context) error { return q.ToggleMCPServer(ctx, "srv", false) },
			wantSubtype: "mcp_toggle",
			wantExtra:   map[string]any{"enabled": false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := newFakeTransport()
			ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

			q, ctx, cleanup := newTestQuery(t, ft)
			defer cleanup()

			if err := tc.call(q, ctx); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			reqs := ft.requests()
			if len(reqs) != 1 {
				t.Fatalf("expected 1 request, got %d", len(reqs))
			}
			req := reqs[0]

			if req["subtype"] != tc.wantSubtype {
				t.Errorf("subtype = %v, want %v", req["subtype"], tc.wantSubtype)
			}
			if got, ok := req["serverName"]; !ok || got != "srv" {
				t.Errorf("serverName = %v (present=%v), want \"srv\"", got, ok)
			}
			if _, ok := req["server_name"]; ok {
				t.Error("snake_case server_name must not be sent")
			}
			for k, want := range tc.wantExtra {
				if got := req[k]; got != want {
					t.Errorf("%s = %v, want %v", k, got, want)
				}
			}
		})
	}
}

// Snake_case field names are correct for these two; assert so a future
// "consistency" cleanup does not break them.
func TestTaskAndRewindRequestsUseSnakeCase(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	if err := q.StopTask(ctx, "task-1"); err != nil {
		t.Fatalf("StopTask: %v", err)
	}
	if err := q.RewindFiles(ctx, "msg-1"); err != nil {
		t.Fatalf("RewindFiles: %v", err)
	}

	reqs := ft.requests()
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}
	if reqs[0]["subtype"] != "stop_task" || reqs[0]["task_id"] != "task-1" {
		t.Errorf("stop_task request = %+v", reqs[0])
	}
	if reqs[1]["subtype"] != "rewind_files" || reqs[1]["user_message_id"] != "msg-1" {
		t.Errorf("rewind_files request = %+v", reqs[1])
	}
}

// agentProgressSummaries has no CLI flag; it rides on the initialize request.
func TestInitializeCarriesAgentProgressSummaries(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q := NewQuery(&QueryOptions{
		Transport:              ft,
		IsStreamingMode:        true,
		InitializeTimeout:      2 * time.Second,
		AgentProgressSummaries: true,
		SDKMCPServers: map[string]*MCPServerHandler{
			"zeta":  {Name: "zeta"},
			"alpha": {Name: "alpha"},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	reqs := ft.requests()
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]

	if req["subtype"] != "initialize" {
		t.Fatalf("subtype = %v", req["subtype"])
	}
	if req["agentProgressSummaries"] != true {
		t.Errorf("agentProgressSummaries = %v, want true", req["agentProgressSummaries"])
	}

	// SDK MCP server names are declared on initialize, sorted for determinism.
	names, ok := req["sdkMcpServers"].([]any)
	if !ok {
		t.Fatalf("sdkMcpServers = %v", req["sdkMcpServers"])
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "zeta" {
		t.Errorf("sdkMcpServers = %v, want [alpha zeta]", names)
	}
}

// The CLI attaches permission suggestions to can_use_tool so a host can offer
// "always allow". Decoding only the discriminator would make echoing them back
// as UpdatedPermissions a silent no-op.
func TestCanUseToolReceivesFullSuggestions(t *testing.T) {
	ft := newFakeTransport()

	var gotCtx types.ToolPermissionContext
	done := make(chan struct{})

	q := NewQuery(&QueryOptions{
		Transport:       ft,
		IsStreamingMode: true,
		CanUseTool: func(_ context.Context, _ string, _ map[string]any,
			permCtx types.ToolPermissionContext) (types.PermissionResult, error) {
			gotCtx = permCtx
			close(done)
			// Echo the suggestions back, which is the documented pattern.
			return &types.PermissionResultAllow{UpdatedPermissions: permCtx.Suggestions}, nil
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	ft.msgChan <- map[string]any{
		"type":       "control_request",
		"request_id": "req-1",
		"request": map[string]any{
			"subtype":   "can_use_tool",
			"tool_name": "Bash",
			"input":     map[string]any{"command": "ls"},
			"permission_suggestions": []any{
				map[string]any{
					"type":     "addRules",
					"behavior": "allow",
					"rules": []any{
						map[string]any{"toolName": "Bash", "ruleContent": "ls:*"},
					},
					"destination": "session",
				},
			},
		},
	}

	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("canUseTool was never invoked")
	}

	if len(gotCtx.Suggestions) != 1 {
		t.Fatalf("expected 1 suggestion, got %d", len(gotCtx.Suggestions))
	}
	s := gotCtx.Suggestions[0]
	if s.Type != types.PermissionUpdateTypeAddRules {
		t.Errorf("Type = %q", s.Type)
	}
	if s.Behavior == nil || *s.Behavior != types.PermissionBehaviorAllow {
		t.Errorf("Behavior = %v, want allow", s.Behavior)
	}
	if s.Destination == nil || *s.Destination != types.PermissionUpdateDestinationSession {
		t.Errorf("Destination = %v, want session", s.Destination)
	}
	if len(s.Rules) != 1 || s.Rules[0].ToolName != "Bash" ||
		s.Rules[0].RuleContent == nil || *s.Rules[0].RuleContent != "ls:*" {
		t.Errorf("Rules = %+v", s.Rules)
	}

	// And the response must carry those rules back out on the wire.
	waitFor(t, ctx, func() bool { return len(ft.responses()) > 0 })

	resp := ft.responses()[0]
	payload, _ := resp["response"].(map[string]any)
	perms, ok := payload["updatedPermissions"].([]any)
	if !ok || len(perms) != 1 {
		t.Fatalf("updatedPermissions = %v", payload["updatedPermissions"])
	}
	perm, _ := perms[0].(map[string]any)
	rules, ok := perm["rules"].([]any)
	if !ok || len(rules) != 1 {
		t.Fatalf("echoed rules = %v", perm["rules"])
	}
	rule, _ := rules[0].(map[string]any)
	if rule["toolName"] != "Bash" || rule["ruleContent"] != "ls:*" {
		t.Errorf("echoed rule = %v", rule)
	}
}

func waitFor(t *testing.T, ctx context.Context, cond func() bool) {
	t.Helper()
	for {
		if cond() {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("timed out waiting for condition")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestInitializeOmitsUnsetFields(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	req := ft.requests()[0]
	for _, key := range []string{"agentProgressSummaries", "sdkMcpServers", "hooks"} {
		if _, ok := req[key]; ok {
			t.Errorf("%s should be omitted when unset, got %v", key, req[key])
		}
	}
}

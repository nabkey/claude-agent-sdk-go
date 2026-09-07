package claude

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// newTestClient connects a Client against a scripted transport, so the whole
// control surface can be exercised without a claude binary.
func newTestClient(t *testing.T, ft *scriptedTransport, opts *AgentOptions) (*Client, context.Context) {
	t.Helper()
	if opts == nil {
		opts = DefaultAgentOptions()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	client, err := NewClientWithTransport(ctx, opts, ft)
	if err != nil {
		t.Fatalf("NewClientWithTransport: %v", err)
	}
	if err := client.Connect(ctx, ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, ctx
}

// Every control method has to reach the CLI with the subtype and field names
// the CLI actually reads; a wrong key is silently ignored on the wire.
func TestClientControlRequestWireFormat(t *testing.T) {
	tests := []struct {
		name        string
		call        func(*Client, context.Context) error
		wantSubtype string
		wantFields  map[string]any
		response    map[string]any
	}{
		{
			name: "SetPermissionMode",
			call: func(c *Client, ctx context.Context) error {
				return c.SetPermissionMode(ctx, types.PermissionModePlan)
			},
			wantSubtype: "set_permission_mode",
			wantFields:  map[string]any{"mode": "plan"},
		},
		{
			name: "SetModel",
			call: func(c *Client, ctx context.Context) error {
				return c.SetModel(ctx, String("claude-opus-5"))
			},
			wantSubtype: "set_model",
			wantFields:  map[string]any{"model": "claude-opus-5"},
		},
		{
			name: "ApplyFlagSettings",
			call: func(c *Client, ctx context.Context) error {
				return c.ApplyFlagSettings(ctx, map[string]any{"autoCompact": true})
			},
			wantSubtype: "apply_flag_settings",
		},
		{
			name: "StopTask",
			call: func(c *Client, ctx context.Context) error {
				return c.StopTask(ctx, "task-1")
			},
			wantSubtype: "stop_task",
			wantFields:  map[string]any{"task_id": "task-1"},
		},
		{
			name: "SeedReadState",
			call: func(c *Client, ctx context.Context) error {
				return c.SeedReadState(ctx, "/repo/main.go", 1700000000)
			},
			wantSubtype: "seed_read_state",
			wantFields:  map[string]any{"path": "/repo/main.go"},
		},
		{
			name: "ReconnectMCPServer",
			call: func(c *Client, ctx context.Context) error {
				return c.ReconnectMCPServer(ctx, "linear")
			},
			wantSubtype: "mcp_reconnect",
			wantFields:  map[string]any{"serverName": "linear"},
		},
		{
			name: "ToggleMCPServer",
			call: func(c *Client, ctx context.Context) error {
				return c.ToggleMCPServer(ctx, "linear", false)
			},
			wantSubtype: "mcp_toggle",
			wantFields:  map[string]any{"serverName": "linear", "enabled": false},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := newScriptedTransport()
			var seen map[string]any
			ft.onRequest = func(subtype string, request map[string]any) map[string]any {
				if subtype != "initialize" {
					seen = request
				}
				return tc.response
			}

			client, ctx := newTestClient(t, ft, nil)
			if err := tc.call(client, ctx); err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}

			if seen == nil {
				t.Fatal("no control request was sent")
			}
			if seen["subtype"] != tc.wantSubtype {
				t.Errorf("subtype = %v, want %q", seen["subtype"], tc.wantSubtype)
			}
			for key, want := range tc.wantFields {
				if seen[key] != want {
					t.Errorf("request[%q] = %v, want %v", key, seen[key], want)
				}
			}
		})
	}
}

// The summary detail exists to skip the per-category token-count API calls,
// so the two methods must not send the same request.
func TestClientContextUsageDetail(t *testing.T) {
	tests := []struct {
		name string
		call func(*Client, context.Context) (*types.ContextUsage, error)
		want any
	}{
		{"GetContextUsage", (*Client).GetContextUsage, "full"},
		{"GetContextUsageSummary", (*Client).GetContextUsageSummary, "summary"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := newScriptedTransport()
			var seen map[string]any
			ft.onRequest = func(subtype string, request map[string]any) map[string]any {
				if subtype == "get_context_usage" {
					seen = request
				}
				return map[string]any{"totalTokens": float64(1234), "maxTokens": float64(200000)}
			}

			client, ctx := newTestClient(t, ft, nil)
			usage, err := tc.call(client, ctx)
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if usage.TotalTokens != 1234 {
				t.Errorf("TotalTokens = %d", usage.TotalTokens)
			}
			if seen["detail"] != tc.want {
				t.Errorf("detail = %v, want %v", seen["detail"], tc.want)
			}
		})
	}
}

// A rewind reports what it did, so a caller can tell a real restore from a
// dry run that found nothing to restore.
func TestClientRewindFiles(t *testing.T) {
	ft := newScriptedTransport()
	var seen []map[string]any
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype == "rewind_files" {
			seen = append(seen, request)
		}
		return map[string]any{
			"canRewind":    true,
			"filesChanged": []any{"main.go"},
			"insertions":   float64(3),
		}
	}

	client, ctx := newTestClient(t, ft, nil)

	preview, err := client.PreviewRewindFiles(ctx, "u-1")
	if err != nil {
		t.Fatalf("PreviewRewindFiles: %v", err)
	}
	if !preview.CanRewind || len(preview.FilesChanged) != 1 {
		t.Errorf("preview = %+v", preview)
	}

	if _, err := client.RewindFiles(ctx, "u-1"); err != nil {
		t.Fatalf("RewindFiles: %v", err)
	}

	if len(seen) != 2 {
		t.Fatalf("expected two rewind requests, got %d", len(seen))
	}
	if seen[0]["dry_run"] != true {
		t.Errorf("preview must be a dry run, got %v", seen[0])
	}
	if dryRun, ok := seen[1]["dry_run"]; ok && dryRun == true {
		t.Error("the real rewind must not be a dry run")
	}
}

func TestClientInterruptWithOptions(t *testing.T) {
	ft := newScriptedTransport()
	var seen map[string]any
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype == "interrupt" {
			seen = request
		}
		return map[string]any{"still_queued": []any{"u-2"}, "cancelled": []any{"u-3"}}
	}

	client, ctx := newTestClient(t, ft, nil)
	receipt, err := client.InterruptWithOptions(ctx, true)
	if err != nil {
		t.Fatalf("InterruptWithOptions: %v", err)
	}

	if len(receipt.StillQueued) != 1 || receipt.StillQueued[0] != "u-2" {
		t.Errorf("StillQueued = %v", receipt.StillQueued)
	}
	if len(receipt.Cancelled) != 1 || receipt.Cancelled[0] != "u-3" {
		t.Errorf("Cancelled = %v", receipt.Cancelled)
	}
	if seen["cancel_queued"] != true {
		t.Errorf("cancel_queued = %v, want true", seen["cancel_queued"])
	}
}

func TestClientReadFile(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype != "read_file" {
			return nil
		}
		return map[string]any{"contents": "package main", "absPath": "/repo/main.go"}
	}

	client, ctx := newTestClient(t, ft, nil)
	result, err := client.ReadFile(ctx, "main.go", 0, "")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if result.Content != "package main" || result.AbsPath != "/repo/main.go" {
		t.Errorf("result = %+v", result)
	}
}

func TestClientBackgroundTasks(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		return map[string]any{"success": true}
	}

	client, ctx := newTestClient(t, ft, nil)
	ok, err := client.BackgroundTasks(ctx, "tool-1")
	if err != nil {
		t.Fatalf("BackgroundTasks: %v", err)
	}
	if !ok {
		t.Error("expected the backgrounding to be reported as successful")
	}
}

// The initialize response is cached, so these accessors answer without a
// round trip.
func TestClientInitializationAccessors(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype != "initialize" {
			return nil
		}
		return map[string]any{
			"commands": []any{map[string]any{"name": "review"}},
			"agents":   []any{map[string]any{"name": "Explore"}},
			"account":  map[string]any{"email": "a@example.test"},
		}
	}

	client, _ := newTestClient(t, ft, nil)

	if commands := client.SupportedCommands(); len(commands) != 1 || commands[0].Name != "review" {
		t.Errorf("SupportedCommands = %v", commands)
	}
	if agents := client.SupportedAgents(); len(agents) != 1 || agents[0].Name != "Explore" {
		t.Errorf("SupportedAgents = %v", agents)
	}
	if account := client.AccountInfo(); account == nil || account.Email != "a@example.test" {
		t.Errorf("AccountInfo = %+v", account)
	}
	if result := client.InitializationResult(); result == nil {
		t.Error("InitializationResult must be available after Connect")
	}
	if info := client.GetServerInfo(); info == nil {
		t.Error("GetServerInfo must return the raw initialize response")
	}
}

// On a remote session the worker's provider and policy decide what is
// selectable, so the live CLI answer wins over the initialize snapshot.
func TestClientSupportedModelsAsksTheCLI(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		switch subtype {
		case "initialize":
			return map[string]any{"models": []any{map[string]any{"value": "opus"}}}
		case "list_models":
			return map[string]any{"models": []any{map[string]any{"value": "sonnet"}}}
		}
		return nil
	}

	client, ctx := newTestClient(t, ft, nil)
	models, err := client.SupportedModels(ctx)
	if err != nil {
		t.Fatalf("SupportedModels: %v", err)
	}
	if len(models) != 1 || models[0].Model != "sonnet" {
		t.Errorf("models = %v, want the CLI's live list", models)
	}
}

// An older CLI has no list_models request, and the initialize snapshot is
// better than nothing.
func TestClientSupportedModelsFallsBackToInitialize(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype == "initialize" {
			return map[string]any{"models": []any{map[string]any{"value": "opus"}}}
		}
		// An unrecognized subtype is answered with an empty payload, which
		// decodes to no models rather than an error, so this exercises the
		// path where the CLI simply reports nothing.
		return map[string]any{}
	}

	client, ctx := newTestClient(t, ft, nil)
	models, err := client.SupportedModels(ctx)
	if err != nil {
		t.Fatalf("SupportedModels: %v", err)
	}
	if len(models) != 0 {
		t.Errorf("models = %v", models)
	}
	if cached := client.InitializationResult(); cached == nil || len(cached.Models) != 1 {
		t.Errorf("the initialize snapshot must still be available: %+v", cached)
	}
}

func TestClientSetMCPServers(t *testing.T) {
	ft := newScriptedTransport()
	var seen map[string]any
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype == "mcp_set_servers" {
			seen = request
		}
		return map[string]any{"added": []any{"linear"}, "removed": []any{"old"}}
	}

	client, ctx := newTestClient(t, ft, nil)
	result, err := client.SetMCPServers(ctx, map[string]types.MCPServerConfig{
		"linear": &types.HTTPMCPServer{Type: "http", URL: "https://example.test/mcp"},
	})
	if err != nil {
		t.Fatalf("SetMCPServers: %v", err)
	}
	if len(result.Added) != 1 || result.Added[0] != "linear" {
		t.Errorf("Added = %v", result.Added)
	}
	if _, ok := seen["servers"]; !ok {
		t.Errorf("request = %v, want a servers payload", seen)
	}
}

// Sending after Close is a caller mistake that must fail loudly rather than
// hang or write to a dead transport.
func TestClientMethodsFailAfterClose(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(string, map[string]any) map[string]any { return map[string]any{} }

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClientWithTransport(ctx, nil, ft)
	if err != nil {
		t.Fatalf("NewClientWithTransport: %v", err)
	}
	if err := client.Connect(ctx, ""); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	if err := client.SendQuery(ctx, "hi"); err == nil {
		t.Error("expected SendQuery to fail on a closed client")
	}
	if err := client.Interrupt(ctx); err == nil {
		t.Error("expected Interrupt to fail on a closed client")
	}
	if client.InitializationResult() != nil {
		t.Error("expected no initialization result on a closed client")
	}

	// Close is idempotent, since it runs on every teardown path.
	if err := client.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestClientMethodsFailBeforeConnect(t *testing.T) {
	ctx := context.Background()
	client, err := NewClient(ctx, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if err := client.SendQuery(ctx, "hi"); err == nil {
		t.Error("expected SendQuery to fail before Connect")
	}
	if _, err := client.GetContextUsage(ctx); err == nil {
		t.Error("expected GetContextUsage to fail before Connect")
	}
}

// A user turn carries the session id the CLI assigned, so a multi-session
// host routes it to the right conversation.
func TestClientSendMessage(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(string, map[string]any) map[string]any { return map[string]any{} }

	client, ctx := newTestClient(t, ft, nil)
	if err := client.SendMessage(ctx, types.UserInputMessage{
		Type:      "user",
		Message:   types.UserInputInner{Role: "user", Content: "hello"},
		SessionID: "sess-9",
	}); err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	sent := ft.sentUserMessages()
	if len(sent) != 1 {
		t.Fatalf("expected one user turn, got %d", len(sent))
	}
	if sent[0]["session_id"] != "sess-9" {
		t.Errorf("session_id = %v", sent[0]["session_id"])
	}
}

// The stream has a single consumer, so a second reader is a misuse the SDK
// has to make visible rather than silently splitting messages.
func TestClientReceiveMessagesHasOneConsumer(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(string, map[string]any) map[string]any { return map[string]any{} }

	client, ctx := newTestClient(t, ft, nil)
	if err := client.SendQuery(ctx, "hi"); err != nil {
		t.Fatalf("SendQuery: %v", err)
	}
	ft.push(assistantFrame("hello"))
	ft.push(resultFrame())

	var texts []string
	for msg := range client.ReceiveResponse() {
		if assistant, ok := msg.(*types.AssistantMessage); ok {
			for _, block := range assistant.Content {
				if text, ok := block.(*types.TextBlock); ok {
					texts = append(texts, text.Text)
				}
			}
		}
	}

	if len(texts) != 1 || texts[0] != "hello" {
		t.Errorf("texts = %v", texts)
	}
	if err := client.Err(); err != nil {
		t.Errorf("Err() = %v, want nil", err)
	}
}

// A transport failure must reach the caller rather than closing the stream in
// silence.
func TestClientErrSurfacesTransportFailures(t *testing.T) {
	ft := newScriptedTransport()
	ft.onRequest = func(string, map[string]any) map[string]any { return map[string]any{} }

	client, _ := newTestClient(t, ft, nil)
	ft.errChan <- &storeError{"transport exploded"}

	deadline := time.After(2 * time.Second)
	for client.Err() == nil {
		select {
		case <-deadline:
			t.Fatal("the transport error never reached Err()")
		case <-time.After(2 * time.Millisecond):
		}
	}
	if !strings.Contains(client.Err().Error(), "transport exploded") {
		t.Errorf("Err() = %v", client.Err())
	}
}

// An in-process server is named, not described, since the SDK already holds
// the instance; only its timeout has to travel.
func TestClientSetMCPServersCarriesSDKTimeout(t *testing.T) {
	ft := newScriptedTransport()
	var seen map[string]any
	ft.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype == "mcp_set_servers" {
			seen = request
		}
		return map[string]any{}
	}

	client, ctx := newTestClient(t, ft, nil)
	if _, err := client.SetMCPServers(ctx, map[string]types.MCPServerConfig{
		"slow":  &types.SDKMCPServer{Type: "sdk", Name: "slow", TimeoutMS: 30000},
		"plain": &types.SDKMCPServer{Type: "sdk", Name: "plain"},
	}); err != nil {
		t.Fatalf("SetMCPServers: %v", err)
	}

	servers, ok := seen["servers"].(map[string]any)
	if !ok {
		t.Fatalf("request = %v", seen)
	}
	slow, _ := servers["slow"].(map[string]any)
	if slow["type"] != "sdk" || slow["timeout"] != float64(30000) {
		t.Errorf("slow = %v", slow)
	}
	plain, _ := servers["plain"].(map[string]any)
	if _, ok := plain["timeout"]; ok {
		t.Errorf("an unset timeout must be omitted, got %v", plain)
	}
}

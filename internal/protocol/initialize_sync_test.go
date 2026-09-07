package protocol

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// newTestQueryWithInit builds a query whose initialize payload carries cfg.
func newTestQueryWithInit(t *testing.T, ft *fakeTransport, cfg *InitConfig) (*Query, context.Context, func()) {
	t.Helper()
	q := NewQuery(&QueryOptions{
		Transport:         ft,
		IsStreamingMode:   true,
		InitializeTimeout: 2 * time.Second,
		InitConfig:        cfg,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	q.Start(ctx)
	return q, ctx, func() {
		cancel()
		_ = q.Close()
	}
}

// perTaskStopAffordance has no CLI flag: it rides on the initialize request.
func TestInitializeCarriesPerTaskStopAffordance(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{PerTaskStopAffordance: true})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if got := ft.requests()[0]["perTaskStopAffordance"]; got != true {
		t.Errorf("perTaskStopAffordance = %v, want true", got)
	}
}

func TestInitializeOmitsPerTaskStopAffordanceWhenUnset(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, ok := ft.requests()[0]["perTaskStopAffordance"]; ok {
		t.Error("perTaskStopAffordance must be omitted when unset")
	}
}

func TestInitializeCarriesPlugins(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any {
		return map[string]any{"plugins_applied": true}
	}

	plugins := []map[string]any{{"type": "local", "path": "/plugins/a"}}
	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{Plugins: plugins})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	sent, ok := ft.requests()[0]["plugins"].([]any)
	if !ok || len(sent) != 1 {
		t.Fatalf("plugins = %v", ft.requests()[0]["plugins"])
	}
	entry, _ := sent[0].(map[string]any)
	if entry["path"] != "/plugins/a" || entry["type"] != "local" {
		t.Errorf("plugins[0] = %v", sent[0])
	}
}

// A CLI too old to read the initialize payload answers without
// plugins_applied and runs with none of them. Losing every plugin silently is
// worse than a warning.
func TestInitializeWarnsWhenPluginsNotApplied(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	var warnings []string
	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{
		Plugins: []map[string]any{{"type": "local", "path": "/plugins/a"}},
		Warn:    func(msg string) { warnings = append(warnings, msg) },
	})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("expected one warning, got %v", warnings)
	}
	if !strings.Contains(warnings[0], "plugins_applied") {
		t.Errorf("warning does not name the missing signal: %q", warnings[0])
	}
}

// A CLI that did apply them is working as intended, so it must stay quiet.
func TestInitializeSilentWhenPluginsApplied(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any {
		return map[string]any{"plugins_applied": true}
	}

	var warnings []string
	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{
		Plugins: []map[string]any{{"type": "local", "path": "/plugins/a"}},
		Warn:    func(msg string) { warnings = append(warnings, msg) },
	})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning, got %v", warnings)
	}
}

// Under argv delivery there are no plugins on the request, so a CLI that never
// reports plugins_applied is not doing anything wrong.
func TestInitializeSilentWithoutInitializePlugins(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	var warnings []string
	q, ctx, cleanup := newTestQueryWithInit(t, ft, &InitConfig{
		Warn: func(msg string) { warnings = append(warnings, msg) },
	})
	defer cleanup()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning, got %v", warnings)
	}
}

// The summary detail answers from the last response's usage instead of running
// per-category token-count API calls, so the CLI has to be told which one.
func TestGetContextUsageCarriesDetail(t *testing.T) {
	tests := []struct {
		name   string
		detail types.ContextUsageDetail
		want   any
	}{
		{"full", types.ContextUsageDetailFull, "full"},
		{"summary", types.ContextUsageDetailSummary, "summary"},
		{"unset leaves the CLI default", "", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ft := newFakeTransport()
			ft.respond = func(string, map[string]any) map[string]any {
				return map[string]any{"totalTokens": float64(10)}
			}

			q, ctx, cleanup := newTestQuery(t, ft)
			defer cleanup()

			if _, err := q.GetContextUsage(ctx, tc.detail); err != nil {
				t.Fatalf("GetContextUsage: %v", err)
			}

			req := ft.requests()[0]
			if got := req["detail"]; got != tc.want {
				t.Errorf("detail = %v, want %v", got, tc.want)
			}
		})
	}
}

// A per-server tool-call timeout has to travel alongside the server names so
// the CLI can apply it when it first registers the server.
func TestInitializeCarriesSDKMCPServerConfigs(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q := NewQuery(&QueryOptions{
		Transport:         ft,
		IsStreamingMode:   true,
		InitializeTimeout: 2 * time.Second,
		SDKMCPServers: map[string]*MCPServerHandler{
			"slow": {Name: "slow", TimeoutMS: 30000},
			"fast": {Name: "fast"},
		},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}

	req := ft.requests()[0]
	names, ok := req["sdkMcpServers"].([]any)
	if !ok || len(names) != 2 {
		t.Fatalf("sdkMcpServers = %v", req["sdkMcpServers"])
	}

	configs, ok := req["sdkMcpServerConfigs"].(map[string]any)
	if !ok {
		t.Fatalf("sdkMcpServerConfigs = %v", req["sdkMcpServerConfigs"])
	}
	// Only the server that set one appears; a default timeout is the CLI's
	// to choose.
	if len(configs) != 1 {
		t.Fatalf("configs = %v, want only the server with a timeout", configs)
	}
	slow, _ := configs["slow"].(map[string]any)
	if slow["timeout"] != float64(30000) {
		t.Errorf("slow timeout = %v, want 30000", slow["timeout"])
	}
}

func TestInitializeOmitsSDKMCPServerConfigsWhenNoneSet(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	q := NewQuery(&QueryOptions{
		Transport:         ft,
		IsStreamingMode:   true,
		InitializeTimeout: 2 * time.Second,
		SDKMCPServers:     map[string]*MCPServerHandler{"plain": {Name: "plain"}},
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	if _, err := q.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	if _, ok := ft.requests()[0]["sdkMcpServerConfigs"]; ok {
		t.Error("sdkMcpServerConfigs must be omitted when no server sets a timeout")
	}
}

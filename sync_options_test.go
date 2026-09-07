package claude

import (
	"context"
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// The initialize payload is where the options that have no CLI flag travel.
func TestBuildInitConfigSyncFields(t *testing.T) {
	cfg := buildInitConfig(&AgentOptions{
		PerTaskStopAffordance: true,
		PluginDelivery:        types.PluginDeliveryInitialize,
		Plugins: []types.PluginConfig{
			{Type: "local", Path: "/plugins/a"},
			{Type: "local", Path: "/plugins/b", SkipMCPDiscovery: true},
		},
	})

	if !cfg.PerTaskStopAffordance {
		t.Error("expected PerTaskStopAffordance on the initialize config")
	}
	if len(cfg.Plugins) != 2 {
		t.Fatalf("expected both plugins, got %v", cfg.Plugins)
	}
	if cfg.Plugins[0]["path"] != "/plugins/a" || cfg.Plugins[0]["type"] != "local" {
		t.Errorf("plugins[0] = %v", cfg.Plugins[0])
	}
	if _, ok := cfg.Plugins[0]["skipMcpDiscovery"]; ok {
		t.Error("skipMcpDiscovery must be omitted when unset")
	}
	if cfg.Plugins[1]["skipMcpDiscovery"] != true {
		t.Errorf("plugins[1] = %v", cfg.Plugins[1])
	}
}

// Under argv delivery the flags carry the plugins, so the initialize request
// must not also list them.
func TestBuildInitConfigOmitsPluginsUnderArgvDelivery(t *testing.T) {
	for _, delivery := range []types.PluginDelivery{"", types.PluginDeliveryArgv} {
		cfg := buildInitConfig(&AgentOptions{
			PluginDelivery: delivery,
			Plugins:        []types.PluginConfig{{Type: "local", Path: "/plugins/a"}},
		})
		if cfg.Plugins != nil {
			t.Errorf("delivery %q: plugins must not ride on initialize, got %v", delivery, cfg.Plugins)
		}
	}
}

func TestBuildTransportOptionsCarriesSyncOptions(t *testing.T) {
	turn := "user-msg-uuid"
	prompts := types.PermissionPromptsNone
	opts := buildTransportOptions(&AgentOptions{
		ResumeDropsTurn:   &turn,
		PermissionPrompts: &prompts,
		PluginDelivery:    types.PluginDeliveryInitialize,
	})

	if opts.ResumeDropsTurn == nil || *opts.ResumeDropsTurn != turn {
		t.Errorf("ResumeDropsTurn = %v", opts.ResumeDropsTurn)
	}
	if opts.PermissionPrompts == nil || *opts.PermissionPrompts != types.PermissionPromptsNone {
		t.Errorf("PermissionPrompts = %v", opts.PermissionPrompts)
	}
	if opts.PluginDelivery != types.PluginDeliveryInitialize {
		t.Errorf("PluginDelivery = %q", opts.PluginDelivery)
	}
}

// A typo in PluginDelivery would silently drop every plugin under argv
// delivery, so it fails at connect instead.
func TestNewSessionRejectsInvalidPluginDelivery(t *testing.T) {
	_, err := newSession(context.Background(), "hi",
		&AgentOptions{PluginDelivery: "flags"}, newScriptedTransport())
	if err == nil {
		t.Fatal("expected an invalid PluginDelivery to be rejected")
	}
	if !strings.Contains(err.Error(), "PluginDelivery") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Mirroring reads the transcript the CLI writes to disk, so there would be
// nothing to mirror.
func TestNewSessionRejectsStoreWithoutPersistence(t *testing.T) {
	persist := false
	_, err := newSession(context.Background(), "hi", &AgentOptions{
		SessionStore:   NewInMemorySessionStore(),
		PersistSession: &persist,
	}, newScriptedTransport())
	if err == nil {
		t.Fatal("expected SessionStore with PersistSession false to be rejected")
	}
	if !strings.Contains(err.Error(), "PersistSession") {
		t.Errorf("unexpected error: %v", err)
	}
}

// Both routes to a permission decision cannot be installed at once.
func TestNewSessionRejectsCanUseToolWithPromptTool(t *testing.T) {
	_, err := newSession(context.Background(), "hi", &AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		PermissionPromptToolName: String("mcp__x__prompt"),
	}, newScriptedTransport())
	if err == nil {
		t.Fatal("expected the conflicting permission options to be rejected")
	}
}

// Clone is what keeps newSession's mutations off the caller's options, so a
// new field that is not copied would leak between sessions.
func TestCloneCarriesSyncOptions(t *testing.T) {
	turn := "user-msg-uuid"
	prompts := types.PermissionPromptsNone
	original := &AgentOptions{
		ResumeDropsTurn:       &turn,
		PermissionPrompts:     &prompts,
		PerTaskStopAffordance: true,
		PluginDelivery:        types.PluginDeliveryInitialize,
	}

	clone := original.Clone()

	if clone.ResumeDropsTurn == nil || *clone.ResumeDropsTurn != turn {
		t.Errorf("ResumeDropsTurn = %v", clone.ResumeDropsTurn)
	}
	if clone.PermissionPrompts == nil || *clone.PermissionPrompts != prompts {
		t.Errorf("PermissionPrompts = %v", clone.PermissionPrompts)
	}
	if !clone.PerTaskStopAffordance {
		t.Error("PerTaskStopAffordance was not cloned")
	}
	if clone.PluginDelivery != types.PluginDeliveryInitialize {
		t.Errorf("PluginDelivery = %q", clone.PluginDelivery)
	}
}

// AgentOptions.Warn documents a nil callback as logging, so the fallback
// belongs to the helper rather than to each call site.
func TestWarnFunc(t *testing.T) {
	var got []string
	warn := warnFunc(&AgentOptions{Warn: func(msg string) { got = append(got, msg) }})
	warn("something")

	if len(got) != 1 || got[0] != "something" {
		t.Errorf("warnings = %v", got)
	}

	// A nil options value and a nil callback both fall back to the logger
	// rather than panicking.
	warnFunc(nil)("logged")
	warnFunc(&AgentOptions{})("logged")
}

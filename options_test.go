package claude

import (
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestClone_NewFields(t *testing.T) {
	effort := types.EffortLevelHigh
	mcpPath := "/path/to/config"
	original := &AgentOptions{
		Thinking:                &types.ThinkingConfigAdaptive{Type: "adaptive"},
		Effort:                  &effort,
		EnableFileCheckpointing: true,
		MCPConfigPath:           &mcpPath,
		Betas:                   []types.SdkBeta{types.SdkBetaContext1M},
		Tools:                   []string{"Bash", "Write"},
		SystemPrompt:            String("test prompt"),
	}

	clone := original.Clone()

	// Verify new fields are cloned
	if clone.Thinking == nil {
		t.Error("expected Thinking to be cloned")
	}
	if clone.Effort == nil || *clone.Effort != types.EffortLevelHigh {
		t.Error("expected Effort to be cloned")
	}
	if !clone.EnableFileCheckpointing {
		t.Error("expected EnableFileCheckpointing to be cloned")
	}
	if clone.MCPConfigPath == nil || *clone.MCPConfigPath != mcpPath {
		t.Error("expected MCPConfigPath to be cloned")
	}
	if len(clone.Betas) != 1 || clone.Betas[0] != types.SdkBetaContext1M {
		t.Error("expected Betas to be cloned")
	}

	// Verify tools are cloned ([]string case)
	tools, ok := clone.Tools.([]string)
	if !ok {
		t.Fatalf("expected Tools to be []string, got %T", clone.Tools)
	}
	if len(tools) != 2 {
		t.Errorf("expected 2 tools, got %d", len(tools))
	}
}

func TestClone_ToolsPreset(t *testing.T) {
	original := &AgentOptions{
		Tools: &types.ToolsPreset{Type: "preset", Preset: "claude_code"},
	}

	clone := original.Clone()

	preset, ok := clone.Tools.(*types.ToolsPreset)
	if !ok {
		t.Fatalf("expected Tools to be *types.ToolsPreset, got %T", clone.Tools)
	}
	if preset.Preset != "claude_code" {
		t.Errorf("expected preset 'claude_code', got '%s'", preset.Preset)
	}
}

func TestClone_SystemPromptPreset(t *testing.T) {
	original := &AgentOptions{
		SystemPrompt: &types.SystemPromptPreset{
			Type:   "preset",
			Preset: "claude_code",
		},
	}

	clone := original.Clone()

	preset, ok := clone.SystemPrompt.(*types.SystemPromptPreset)
	if !ok {
		t.Fatalf("expected SystemPrompt to be *types.SystemPromptPreset, got %T", clone.SystemPrompt)
	}
	if preset.Preset != "claude_code" {
		t.Errorf("expected preset 'claude_code', got '%s'", preset.Preset)
	}
}

func TestBuilderMethods(t *testing.T) {
	opts := DefaultAgentOptions().
		WithThinking(types.NewThinkingEnabled(5000)).
		WithEffort(types.EffortLevelMax).
		WithFileCheckpointing().
		WithMCPConfigPath("/path/to/config")

	if opts.Thinking == nil {
		t.Error("expected Thinking to be set")
	}
	if opts.Effort == nil || *opts.Effort != types.EffortLevelMax {
		t.Error("expected Effort to be max")
	}
	if !opts.EnableFileCheckpointing {
		t.Error("expected EnableFileCheckpointing to be true")
	}
	if opts.MCPConfigPath == nil || *opts.MCPConfigPath != "/path/to/config" {
		t.Error("expected MCPConfigPath to be set")
	}
}

func TestEffortToString(t *testing.T) {
	// nil case
	if result := effortToString(nil); result != nil {
		t.Error("expected nil for nil input")
	}

	// non-nil case
	effort := types.EffortLevelHigh
	result := effortToString(&effort)
	if result == nil || *result != "high" {
		t.Errorf("expected 'high', got %v", result)
	}
}

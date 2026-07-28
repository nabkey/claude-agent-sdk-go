package transport

import (
	"reflect"
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// newTestTransport builds a transport without touching the filesystem so
// buildCommand/buildEnv can be exercised without a real CLI on PATH.
func newTestTransport(opts *SubprocessOptions) *SubprocessTransport {
	if opts == nil {
		opts = &SubprocessOptions{}
	}
	return &SubprocessTransport{
		cliPath: "/usr/bin/claude",
		options: opts,
	}
}

// argsAfter returns the argv tokens following the CLI path, so tests assert on
// flags rather than on the binary location.
func argsAfter(cmd []string) []string { return cmd[1:] }

// flagValue returns the value bound to flag, handling both the two-token
// (`--flag value`) and equals (`--flag=value`) forms. The second return
// reports whether the flag was present at all.
func flagValue(cmd []string, flag string) (string, bool) {
	for i, arg := range cmd {
		if arg == flag {
			if i+1 < len(cmd) {
				return cmd[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(arg, flag+"=") {
			return strings.TrimPrefix(arg, flag+"="), true
		}
	}
	return "", false
}

// hasFlag reports whether the exact token appears in argv.
func hasFlag(cmd []string, flag string) bool {
	for _, arg := range cmd {
		if arg == flag || strings.HasPrefix(arg, flag+"=") {
			return true
		}
	}
	return false
}

func envValue(env []string, key string) (string, bool) {
	for _, kv := range env {
		if k, v, ok := strings.Cut(kv, "="); ok && k == key {
			return v, true
		}
	}
	return "", false
}

// --- Thinking configuration -------------------------------------------------

// The CLI models thinking as two distinct flags, not a JSON payload. These
// cases mirror the mapping in the reference SDKs exactly.
func TestBuildCommand_Thinking(t *testing.T) {
	summarized := types.ThinkingDisplaySummarized

	tests := []struct {
		name      string
		thinking  types.ThinkingConfig
		maxTokens *int
		want      []string // expected (flag, value) pairs, flattened
		absent    []string
	}{
		{
			name:     "adaptive",
			thinking: types.NewThinkingAdaptive(),
			want:     []string{"--thinking", "adaptive"},
			absent:   []string{"--max-thinking-tokens", "--thinking-display"},
		},
		{
			name:     "enabled with budget uses max-thinking-tokens",
			thinking: types.NewThinkingEnabled(12000),
			want:     []string{"--max-thinking-tokens", "12000"},
			absent:   []string{"--thinking"},
		},
		{
			name:     "enabled without budget falls back to adaptive",
			thinking: &types.ThinkingConfigEnabled{Type: "enabled"},
			want:     []string{"--thinking", "adaptive"},
			absent:   []string{"--max-thinking-tokens"},
		},
		{
			name:     "disabled",
			thinking: types.NewThinkingDisabled(),
			want:     []string{"--thinking", "disabled"},
			absent:   []string{"--max-thinking-tokens", "--thinking-display"},
		},
		{
			name:     "adaptive with display",
			thinking: &types.ThinkingConfigAdaptive{Type: "adaptive", Display: &summarized},
			want:     []string{"--thinking", "adaptive", "--thinking-display", "summarized"},
		},
		{
			name:      "thinking takes precedence over deprecated max thinking tokens",
			thinking:  types.NewThinkingDisabled(),
			maxTokens: intPtr(9999),
			want:      []string{"--thinking", "disabled"},
			absent:    []string{"--max-thinking-tokens"},
		},
		{
			name:      "deprecated max thinking tokens used when thinking unset",
			maxTokens: intPtr(4096),
			want:      []string{"--max-thinking-tokens", "4096"},
			absent:    []string{"--thinking"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestTransport(&SubprocessOptions{
				Thinking:          tc.thinking,
				MaxThinkingTokens: tc.maxTokens,
			}).buildCommand()

			for i := 0; i < len(tc.want); i += 2 {
				flag, want := tc.want[i], tc.want[i+1]
				got, ok := flagValue(cmd, flag)
				if !ok {
					t.Fatalf("expected %s in argv, got: %v", flag, argsAfter(cmd))
				}
				if got != want {
					t.Errorf("%s = %q, want %q", flag, got, want)
				}
			}
			for _, flag := range tc.absent {
				if hasFlag(cmd, flag) {
					t.Errorf("unexpected %s in argv: %v", flag, argsAfter(cmd))
				}
			}
		})
	}
}

// --- Tools ------------------------------------------------------------------

func TestBuildCommand_Tools(t *testing.T) {
	tests := []struct {
		name  string
		tools any
		want  string
		unset bool
	}{
		{name: "preset maps to default", tools: &types.ToolsPreset{Type: "preset", Preset: "claude_code"}, want: "default"},
		{name: "explicit list", tools: []string{"Bash", "Read"}, want: "Bash,Read"},
		{name: "empty list disables built-ins", tools: []string{}, want: ""},
		{name: "nil omits the flag", tools: nil, unset: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestTransport(&SubprocessOptions{Tools: tc.tools}).buildCommand()
			got, ok := flagValue(cmd, "--tools")
			if tc.unset {
				if ok {
					t.Errorf("expected no --tools flag, got %q", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected --tools in argv: %v", argsAfter(cmd))
			}
			if got != tc.want {
				t.Errorf("--tools = %q, want %q", got, tc.want)
			}
		})
	}
}

// --- Flags that must NOT exist ----------------------------------------------

// These three options are configured through the environment or the initialize
// request. Emitting them as CLI flags silently no-ops at best.
func TestBuildCommand_NoPhantomFlags(t *testing.T) {
	previewFormat := types.PreviewFormatHTML
	cmd := newTestTransport(&SubprocessOptions{
		EnableFileCheckpointing: true,
		ToolConfig: &types.ToolConfig{
			AskUserQuestion: &types.AskUserQuestionConfig{PreviewFormat: &previewFormat},
		},
	}).buildCommand()

	for _, flag := range []string{
		"--enable-file-checkpointing",
		"--tool-config",
		"--agent-progress-summaries",
	} {
		if hasFlag(cmd, flag) {
			t.Errorf("%s is not a real CLI flag but was emitted: %v", flag, argsAfter(cmd))
		}
	}
}

func TestBuildEnv_CheckpointingAndToolConfig(t *testing.T) {
	previewFormat := types.PreviewFormatHTML
	env := newTestTransport(&SubprocessOptions{
		EnableFileCheckpointing: true,
		ToolConfig: &types.ToolConfig{
			AskUserQuestion: &types.AskUserQuestionConfig{PreviewFormat: &previewFormat},
		},
	}).buildEnv()

	if v, ok := envValue(env, "CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"); !ok || v != "true" {
		t.Errorf("CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING = %q (present=%v), want \"true\"", v, ok)
	}
	if v, ok := envValue(env, "CLAUDE_CODE_QUESTION_PREVIEW_FORMAT"); !ok || v != "html" {
		t.Errorf("CLAUDE_CODE_QUESTION_PREVIEW_FORMAT = %q (present=%v), want \"html\"", v, ok)
	}
	if v, ok := envValue(env, "CLAUDE_CODE_ENTRYPOINT"); !ok || v != "sdk-go" {
		t.Errorf("CLAUDE_CODE_ENTRYPOINT = %q (present=%v), want \"sdk-go\"", v, ok)
	}
}

func TestBuildEnv_Defaults(t *testing.T) {
	env := newTestTransport(nil).buildEnv()

	for _, key := range []string{
		"CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING",
		"CLAUDE_CODE_QUESTION_PREVIEW_FORMAT",
	} {
		if v, ok := envValue(env, key); ok {
			t.Errorf("%s should be unset by default, got %q", key, v)
		}
	}
}

func TestBuildEnv_CallerEnvOverridesEntrypoint(t *testing.T) {
	env := newTestTransport(&SubprocessOptions{
		Env: map[string]string{"CLAUDE_CODE_ENTRYPOINT": "my-app"},
	}).buildEnv()

	if v, _ := envValue(env, "CLAUDE_CODE_ENTRYPOINT"); v != "my-app" {
		t.Errorf("CLAUDE_CODE_ENTRYPOINT = %q, want %q", v, "my-app")
	}
}

// --- Flag-injection hardening -----------------------------------------------

// --resume takes an optional value, so the two-token form lets a dash-leading
// value escape as a separate CLI flag.
func TestBuildCommand_ResumeUsesEqualsForm(t *testing.T) {
	resume := "--dangerously-skip-permissions"
	cmd := newTestTransport(&SubprocessOptions{Resume: &resume}).buildCommand()

	for _, arg := range cmd {
		if arg == "--resume" {
			t.Fatalf("--resume must use the equals form, got two-token: %v", argsAfter(cmd))
		}
	}
	if !hasFlag(cmd, "--resume") {
		t.Fatalf("expected --resume=... in argv: %v", argsAfter(cmd))
	}
	got, _ := flagValue(cmd, "--resume")
	if got != resume {
		t.Errorf("--resume = %q, want %q", got, resume)
	}
}

func TestBuildCommand_ExtraArgsDashLeadingValueUsesEqualsForm(t *testing.T) {
	value := "-injected"
	plain := "safe"
	cmd := newTestTransport(&SubprocessOptions{
		ExtraArgs: map[string]*string{
			"custom":  &value,
			"other":   &plain,
			"boolean": nil,
		},
	}).buildCommand()

	joined := strings.Join(cmd, " ")
	if !strings.Contains(joined, "--custom=-injected") {
		t.Errorf("dash-leading extra arg must use equals form, got: %v", argsAfter(cmd))
	}
	if !strings.Contains(joined, "--other safe") {
		t.Errorf("plain extra arg should use two-token form, got: %v", argsAfter(cmd))
	}
	if !hasFlag(cmd, "--boolean") {
		t.Errorf("nil-valued extra arg should emit a bare flag, got: %v", argsAfter(cmd))
	}
}

// ExtraArgs iteration order must not leak map randomization into argv.
func TestBuildCommand_ExtraArgsDeterministicOrder(t *testing.T) {
	a, b, c := "1", "2", "3"
	opts := &SubprocessOptions{ExtraArgs: map[string]*string{"zeta": &a, "alpha": &b, "mid": &c}}

	first := newTestTransport(opts).buildCommand()
	for i := 0; i < 20; i++ {
		if got := newTestTransport(opts).buildCommand(); !reflect.DeepEqual(first, got) {
			t.Fatalf("argv is not deterministic:\n first = %v\n got   = %v", first, got)
		}
	}
}

// --- Setting sources --------------------------------------------------------

// Omitting the flag lets the CLI load all sources; passing an empty value
// disables filesystem settings entirely. Those are different behaviors, so a
// nil slice must not produce a flag.
func TestBuildCommand_SettingSources(t *testing.T) {
	tests := []struct {
		name    string
		sources []types.SettingSource
		want    string
		unset   bool
	}{
		{name: "nil omits the flag entirely", sources: nil, unset: true},
		{name: "empty non-nil disables all sources", sources: []types.SettingSource{}, want: ""},
		{
			name:    "explicit sources",
			sources: []types.SettingSource{types.SettingSourceUser, types.SettingSourceProject},
			want:    "user,project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cmd := newTestTransport(&SubprocessOptions{SettingSources: tc.sources}).buildCommand()
			got, ok := flagValue(cmd, "--setting-sources")
			if tc.unset {
				if ok {
					t.Errorf("expected no --setting-sources flag, got %q", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected --setting-sources in argv: %v", argsAfter(cmd))
			}
			if got != tc.want {
				t.Errorf("--setting-sources = %q, want %q", got, tc.want)
			}
			// Must use the equals form so an empty value stays bound.
			if !strings.Contains(strings.Join(cmd, " "), "--setting-sources=") {
				t.Errorf("--setting-sources must use the equals form: %v", argsAfter(cmd))
			}
		})
	}
}

// --- Option validation ------------------------------------------------------

func TestValidateOptions_FallbackModelMustDiffer(t *testing.T) {
	model := "claude-sonnet-5"
	err := validateOptions(&SubprocessOptions{Model: &model, FallbackModel: &model})
	if err == nil {
		t.Fatal("expected an error when FallbackModel equals Model")
	}
	if !strings.Contains(err.Error(), "fallback model") {
		t.Errorf("unexpected error text: %v", err)
	}

	other := "claude-haiku-4-5"
	if err := validateOptions(&SubprocessOptions{Model: &model, FallbackModel: &other}); err != nil {
		t.Errorf("distinct models should validate, got: %v", err)
	}
	if err := validateOptions(&SubprocessOptions{FallbackModel: &model}); err != nil {
		t.Errorf("fallback without a main model should validate, got: %v", err)
	}
}

// --- Unchanged behavior, kept as regression cover ---------------------------

func TestBuildCommand_Effort(t *testing.T) {
	effort := "high"
	cmd := newTestTransport(&SubprocessOptions{Effort: &effort}).buildCommand()
	if got, ok := flagValue(cmd, "--effort"); !ok || got != "high" {
		t.Errorf("--effort = %q (present=%v), want \"high\"", got, ok)
	}
}

func TestBuildCommand_MCPConfigPath(t *testing.T) {
	path := "/path/to/mcp-config.json"
	cmd := newTestTransport(&SubprocessOptions{MCPConfigPath: &path}).buildCommand()
	if got, ok := flagValue(cmd, "--mcp-config"); !ok || got != path {
		t.Errorf("--mcp-config = %q (present=%v), want %q", got, ok, path)
	}
}

func TestBuildCommand_MCPConfigPath_NotUsedWhenMCPServersSet(t *testing.T) {
	path := "/path/to/mcp-config.json"
	cmd := newTestTransport(&SubprocessOptions{
		MCPConfigPath: &path,
		MCPServers: map[string]types.MCPServerConfig{
			"test": &types.StdioMCPServer{Command: "echo"},
		},
	}).buildCommand()

	for i, arg := range cmd {
		if arg == "--mcp-config" && i+1 < len(cmd) && cmd[i+1] == path {
			t.Error("MCPConfigPath should not be used when MCPServers is set")
		}
	}
}

func TestBuildCommand_SdkBetas(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{
		Betas: []types.SdkBeta{types.SdkBetaContext1M},
	}).buildCommand()
	if got, ok := flagValue(cmd, "--betas"); !ok || got != "context-1m-2025-08-07" {
		t.Errorf("--betas = %q (present=%v)", got, ok)
	}
}

// A preset means "use the CLI's own system prompt", which is what the CLI does
// when no flag is present. The preset's append and excludeDynamicSections
// fields travel on the initialize request instead.
func TestBuildCommand_SystemPromptPreset(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{
		SystemPrompt: &types.SystemPromptPreset{Type: "preset", Preset: "claude_code"},
	}).buildCommand()

	if hasFlag(cmd, "--system-prompt") {
		t.Errorf("a preset must not emit --system-prompt: %v", argsAfter(cmd))
	}
}

// With no system prompt configured the CLI's default must be blanked, so the
// SDK behaves as a library rather than inheriting Claude Code's own prompt.
func TestBuildCommand_SystemPromptDefaults(t *testing.T) {
	cmd := newTestTransport(nil).buildCommand()
	got, ok := flagValue(cmd, "--system-prompt")
	if !ok || got != "" {
		t.Errorf("--system-prompt = %q (present=%v), want empty", got, ok)
	}
}

func TestBuildCommand_SystemPromptFile(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{
		SystemPrompt: &types.SystemPromptFile{Type: "file", Path: "/tmp/prompt.md"},
	}).buildCommand()

	if got, ok := flagValue(cmd, "--system-prompt-file"); !ok || got != "/tmp/prompt.md" {
		t.Errorf("--system-prompt-file = %q (present=%v)", got, ok)
	}
}

// The CLI is always driven in streaming mode; --print cannot carry the
// control protocol.
func TestBuildCommand_AlwaysStreaming(t *testing.T) {
	cmd := newTestTransport(nil).buildCommand()

	if got, ok := flagValue(cmd, "--input-format"); !ok || got != "stream-json" {
		t.Errorf("--input-format = %q (present=%v), want stream-json", got, ok)
	}
	if hasFlag(cmd, "--print") {
		t.Errorf("--print must never be emitted: %v", argsAfter(cmd))
	}
}

// Agents are no longer a CLI flag: they travel on the initialize control
// request, where the payload can be larger and several fields have no flag
// equivalent. See TestInitializeCarriesAgents in the protocol package.
func TestBuildCommand_AgentsAreNotAFlag(t *testing.T) {
	memory := types.AgentMemoryProject
	cmd := newTestTransport(&SubprocessOptions{
		Agents: map[string]types.AgentDefinition{
			"test-agent": {
				Description: "Test agent",
				Prompt:      "Do stuff",
				Skills:      []string{"skill1"},
				Memory:      &memory,
			},
		},
	}).buildCommand()

	if hasFlag(cmd, "--agents") {
		t.Errorf("--agents must not be emitted: %v", argsAfter(cmd))
	}
}

func intPtr(v int) *int { return &v }

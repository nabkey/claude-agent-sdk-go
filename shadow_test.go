package claude

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// An AllowedTools entry that allows a whole tool auto-approves it before
// CanUseTool is consulted, which is the single most common reason a callback
// appears never to fire.
func TestWholeToolAllowed(t *testing.T) {
	tests := []struct {
		entry string
		want  string
	}{
		{"Read", "Read"},
		{"Read()", "Read"},
		{"Read(*)", "Read"},
		{"Bash(ls:*)", ""},
		{"Bash(git status)", ""},
		{"", ""},
		{"   ", ""},
		{"(broken", ""},
		{"(*)", ""},
		{"Write(", ""},
		{"mcp__server__tool", "mcp__server__tool"},
	}

	for _, tc := range tests {
		if got := wholeToolAllowed(tc.entry); got != tc.want {
			t.Errorf("wholeToolAllowed(%q) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}

func TestCanUseToolShadowWarning(t *testing.T) {
	bypass := types.PermissionModeBypassPermissions
	def := types.PermissionModeDefault

	tests := []struct {
		name    string
		mode    *types.PermissionMode
		allowed []string
		want    string // substring, or "" for no warning
	}{
		{name: "no shadowing", allowed: []string{"Bash(ls:*)"}},
		{name: "no allowed tools"},
		{name: "default mode with specifier", mode: &def, allowed: []string{"Read(src/*)"}},
		{
			name: "bypass permissions shadows everything",
			mode: &bypass,
			want: "bypassPermissions",
		},
		{
			name:    "whole tool allowed",
			allowed: []string{"Read"},
			want:    "Read",
		},
		{
			name:    "wildcard specifier",
			allowed: []string{"Write(*)"},
			want:    "Write",
		},
		{
			name:    "mixed entries name only the shadowing one",
			allowed: []string{"Bash(ls:*)", "Read"},
			want:    "Read",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := canUseToolShadowWarning(tc.mode, tc.allowed)
			if tc.want == "" {
				if got != "" {
					t.Errorf("expected no warning, got: %s", got)
				}
				return
			}
			if !strings.Contains(got, tc.want) {
				t.Errorf("warning should mention %q, got: %s", tc.want, got)
			}
		})
	}
}

// A redundant config such as ["Read", "Read()"] resolves to one tool and must
// not be reported twice.
func TestCanUseToolShadowWarningDeduplicates(t *testing.T) {
	got := canUseToolShadowWarning(nil, []string{"Read", "Read()", "Read(*)"})
	if strings.Count(got, "Read") != 1 {
		t.Errorf("expected Read named once, got: %s", got)
	}
}

// skills="all" makes the transport append a bare Skill entry, so it shadows
// the callback exactly like a hand-written one. A list does not.
func TestWarnIfCanUseToolShadowedSkills(t *testing.T) {
	var warnings []string
	base := func() *AgentOptions {
		return &AgentOptions{
			CanUseTool: func(_ context.Context, _ string, _ map[string]any,
				_ types.ToolPermissionContext) (types.PermissionResult, error) {
				return &types.PermissionResultAllow{}, nil
			},
			Warn: func(msg string) { warnings = append(warnings, msg) },
		}
	}

	// Reset the process-wide dedup so this test is independent of others.
	shadowWarnOnce.Range(func(k, _ any) bool { shadowWarnOnce.Delete(k); return true })

	opts := base()
	opts.Skills = types.SkillsAll
	warnIfCanUseToolShadowed(opts)
	if len(warnings) != 1 || !strings.Contains(warnings[0], "Skill") {
		t.Errorf("skills=all should warn about Skill, got: %v", warnings)
	}

	warnings = nil
	opts = base()
	opts.Skills = []string{"pdf"}
	warnIfCanUseToolShadowed(opts)
	if len(warnings) != 0 {
		t.Errorf("a skills list should not shadow the callback, got: %v", warnings)
	}
}

// The warning is advisory and config-level, so it fires once per process
// rather than once per query.
func TestWarnIfCanUseToolShadowedOncePerMessage(t *testing.T) {
	shadowWarnOnce.Range(func(k, _ any) bool { shadowWarnOnce.Delete(k); return true })

	var count int
	opts := &AgentOptions{
		AllowedTools: []string{"Glob"},
		CanUseTool: func(_ context.Context, _ string, _ map[string]any,
			_ types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(string) { count++ },
	}

	warnIfCanUseToolShadowed(opts)
	warnIfCanUseToolShadowed(opts)
	warnIfCanUseToolShadowed(opts)

	if count != 1 {
		t.Errorf("expected 1 warning, got %d", count)
	}
}

func TestWarnIfCanUseToolShadowedNoCallback(t *testing.T) {
	var called bool
	warnIfCanUseToolShadowed(&AgentOptions{
		AllowedTools: []string{"Read"},
		Warn:         func(string) { called = true },
	})
	if called {
		t.Error("no warning is due without a CanUseTool callback")
	}
}

// --- runtime detection ------------------------------------------------------

// resetShadowWarnOnce clears the process-wide dedup so each test observes its
// own warning rather than a neighbour's suppression.
func resetShadowWarnOnce(t *testing.T) {
	t.Helper()
	shadowWarnOnce.Range(func(key, _ any) bool {
		shadowWarnOnce.Delete(key)
		return true
	})
}

// toolUseAssistantFrame is an assistant turn that calls a tool.
func toolUseAssistantFrame(name string) map[string]any {
	return map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"model": "claude-sonnet-5",
			"content": []any{map[string]any{
				"type": "tool_use", "id": "tu-1", "name": name, "input": map[string]any{},
			}},
		},
	}
}

func TestShadowDetectorWarnsWhenNeverConsulted(t *testing.T) {
	resetShadowWarnOnce(t)

	var warnings []string
	d := newShadowDetector(&AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	})

	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.ToolUseBlock{ID: "tu-1", Name: "Bash"},
		&types.ToolUseBlock{ID: "tu-2", Name: "Read"},
	}})
	d.observe(&types.ResultMessage{})

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	if !strings.Contains(warnings[0], "never consulted") {
		t.Errorf("warning missing the headline: %q", warnings[0])
	}
	// The count makes the warning checkable against what the caller saw.
	if !strings.Contains(warnings[0], "all 2 tool call(s)") {
		t.Errorf("warning should report 2 tool calls: %q", warnings[0])
	}
}

func TestShadowDetectorSilentWhenConsulted(t *testing.T) {
	resetShadowWarnOnce(t)

	var warnings []string
	d := newShadowDetector(&AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	})

	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.ToolUseBlock{ID: "tu-1", Name: "Bash"},
	}})
	d.noteConsult()
	d.observe(&types.ResultMessage{})

	if len(warnings) != 0 {
		t.Errorf("expected no warning, got %v", warnings)
	}
}

// A turn with no tool calls proves nothing either way.
func TestShadowDetectorSilentWithoutToolCalls(t *testing.T) {
	resetShadowWarnOnce(t)

	var warnings []string
	d := newShadowDetector(&AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	})

	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.TextBlock{Text: "no tools here"},
	}})
	d.observe(&types.ResultMessage{})

	if len(warnings) != 0 {
		t.Errorf("expected no warning, got %v", warnings)
	}
}

// Counts fold across turns, so a consult in a later turn clears a warning the
// first turn would have raised.
func TestShadowDetectorAccumulatesAcrossTurns(t *testing.T) {
	resetShadowWarnOnce(t)

	var warnings []string
	d := newShadowDetector(&AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	})

	// Turn 1: a tool call with no consult would warn on its own...
	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.ToolUseBlock{ID: "tu-1", Name: "Bash"},
	}})
	d.observe(&types.ResultMessage{})
	if len(warnings) != 1 {
		t.Fatalf("turn 1 should warn, got %v", warnings)
	}

	// ...and once the callback does fire, later turns stay quiet.
	resetShadowWarnOnce(t)
	d.noteConsult()
	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.ToolUseBlock{ID: "tu-2", Name: "Read"},
	}})
	d.observe(&types.ResultMessage{})
	if len(warnings) != 1 {
		t.Errorf("turn 2 should stay quiet, got %v", warnings)
	}
}

// No callback, nothing to watch: the detector is nil and inert.
func TestShadowDetectorNilWithoutCallback(t *testing.T) {
	d := newShadowDetector(&AgentOptions{})
	if d != nil {
		t.Fatal("expected a nil detector without CanUseTool")
	}
	// The nil receiver must tolerate every call site.
	d.noteConsult()
	d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
		&types.ToolUseBlock{ID: "tu-1", Name: "Bash"},
	}})
	d.observe(&types.ResultMessage{})
}

// The warning fires at most once per process, even though it interpolates a
// count that differs between sessions.
func TestShadowDetectorWarnsOncePerProcess(t *testing.T) {
	resetShadowWarnOnce(t)

	var warnings []string
	opts := &AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	}

	for i := 0; i < 3; i++ {
		d := newShadowDetector(opts)
		d.observe(&types.AssistantMessage{Content: []types.ContentBlock{
			&types.ToolUseBlock{ID: "tu-1", Name: "Bash"},
		}})
		d.observe(&types.ResultMessage{})
	}

	if len(warnings) != 1 {
		t.Errorf("expected 1 warning across 3 sessions, got %d: %v", len(warnings), warnings)
	}
}

// End-to-end: the detector must be wired into Query's message loop, not just
// correct in isolation. A tool call that the CLI approves without ever issuing
// a can_use_tool request is exactly what settings-file allow rules produce.
func TestQueryWarnsWhenCanUseToolNeverConsulted(t *testing.T) {
	resetShadowWarnOnce(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	trans := newScriptedTransport()
	trans.onRequest = func(subtype string, _ map[string]any) map[string]any {
		if subtype == "initialize" {
			go func() {
				// The CLI runs the tool without asking permission.
				trans.push(toolUseAssistantFrame("Bash"))
				trans.push(resultFrame())
				_ = trans.Close()
			}()
		}
		return nil
	}

	var warnings []string
	opts := &AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			t.Error("callback should not have been consulted in this scenario")
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	}

	for msg := range QueryWithTransport(ctx, "run something", opts, trans) {
		if err, ok := msg.(error); ok {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d: %v", len(warnings), warnings)
	}
	for _, want := range []string{"never consulted", "all 1 tool call(s)", "SettingSources"} {
		if !strings.Contains(warnings[0], want) {
			t.Errorf("warning missing %q: %q", want, warnings[0])
		}
	}
}

// The mirror image: a session whose tool calls do reach the callback stays
// quiet, so the warning cannot become background noise.
func TestQuerySilentWhenCanUseToolConsulted(t *testing.T) {
	resetShadowWarnOnce(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	trans := newScriptedTransport()
	trans.onRequest = func(subtype string, _ map[string]any) map[string]any {
		if subtype == "initialize" {
			go func() {
				trans.push(toolUseAssistantFrame("Bash"))
				// The CLI asks before running the tool.
				trans.push(map[string]any{
					"type":       "control_request",
					"request_id": "perm-1",
					"request": map[string]any{
						"subtype":   "can_use_tool",
						"tool_name": "Bash",
						"input":     map[string]any{"command": "ls"},
					},
				})
				// The CLI holds the turn open until the permission verdict
				// arrives. Ending it early races the SDK's dispatch.
				if err := trans.awaitResponse("perm-1", 5*time.Second); err != nil {
					t.Errorf("permission dispatch: %v", err)
				}
				trans.push(resultFrame())
				_ = trans.Close()
			}()
		}
		return nil
	}

	consulted := make(chan struct{}, 1)
	var warnings []string
	opts := &AgentOptions{
		CanUseTool: func(context.Context, string, map[string]any,
			types.ToolPermissionContext) (types.PermissionResult, error) {
			select {
			case consulted <- struct{}{}:
			default:
			}
			return &types.PermissionResultAllow{}, nil
		},
		Warn: func(w string) { warnings = append(warnings, w) },
	}

	for msg := range QueryWithTransport(ctx, "run something", opts, trans) {
		if err, ok := msg.(error); ok {
			t.Fatalf("unexpected error: %v", err)
		}
	}

	if len(consulted) == 0 {
		t.Fatal("callback was never consulted; the scenario did not exercise the quiet path")
	}
	if len(warnings) != 0 {
		t.Errorf("expected no warning, got %v", warnings)
	}
}

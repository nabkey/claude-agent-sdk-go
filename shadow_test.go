package claude

import (
	"context"
	"strings"
	"testing"

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

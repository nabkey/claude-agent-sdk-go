package transport

import (
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Skill names are formatted into --allowedTools, which the CLI splits into
// rules on commas and spaces outside parentheses. That tokenizer honors no
// escapes, so a delimiter-carrying name could inject extra permission rules.
func TestValidateSkillNameRejectsInjection(t *testing.T) {
	tests := []struct {
		name  string
		skill string
		want  string
	}{
		{"closing paren escapes the rule", "docs) Bash(rm -rf /", "parentheses"},
		{"comma starts a new rule", "docs,Bash", "commas"},
		{"space and comma", "a, b", "commas"},
		{"control character", "docs\x01", "control characters"},
		{"byte order mark", "docs\ufeff", "byte-order marks"},
		{"bare wildcard", "*", "types.SkillsAll"},
		{"plugin wildcard", "plugin:*", "wildcard-suffix"},
		{"space wildcard", "plugin *", "wildcard-suffix"},
		{"slash command form", "/review", "may not start with"},
		{"leading whitespace", " docs", "whitespace"},
		{"trailing whitespace", "docs ", "whitespace"},
		{"empty", "", "non-empty"},
		{"whitespace only", "   ", "non-empty"},
		{"consecutive backslashes", `a\\b`, "consecutive backslashes"},
		{"trailing backslash", `a\`, "unpaired backslash"},
		{"malformed utf-8", "docs\xed\xa0\x80", "malformed UTF-8"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkillName(tc.skill)
			if err == nil {
				t.Fatalf("expected %q to be rejected", tc.skill)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not explain %q", err, tc.want)
			}
		})
	}
}

func TestValidateSkillNameAcceptsRealNames(t *testing.T) {
	for _, name := range []string{
		"pdf", "artifact-design", "my_skill", "plugin:skill", "skill.v2", "スキル",
	} {
		if err := validateSkillName(name); err != nil {
			t.Errorf("validateSkillName(%q) = %v, want nil", name, err)
		}
	}
}

// A skill list reaches the CLI through argv, so an invalid entry has to fail
// at construction rather than silently building a rule that grants nothing.
func TestValidateSkills(t *testing.T) {
	tests := []struct {
		name    string
		skills  any
		wantErr bool
	}{
		{"nil is a no-op", nil, false},
		{"all", types.SkillsAll, false},
		{"list", []string{"pdf", "docx"}, false},
		{"list with an injected rule", []string{"pdf", "x) Bash(rm"}, true},
		{"a bare string is not a list", "pdf", true},
		{"any other type", 42, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateSkills(tc.skills)
			if (err != nil) != tc.wantErr {
				t.Errorf("validateSkills(%v) = %v, wantErr %v", tc.skills, err, tc.wantErr)
			}
		})
	}
}

// A bare string is the likely mistake, so the error should name the fix.
func TestValidateSkillsSuggestsAList(t *testing.T) {
	err := validateSkills("pdf")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), `[]string{"pdf"}`) {
		t.Errorf("error %q does not suggest the list form", err)
	}
}

// Construction is where an invalid skill has to fail: past this point the name
// is already in argv.
func TestNewSubprocessTransportRejectsInvalidSkill(t *testing.T) {
	_, err := NewSubprocessTransport(&SubprocessOptions{
		CLIPath: strPtr("/usr/bin/claude"),
		Skills:  []string{"docs) Bash(rm -rf /"},
	})
	if err == nil {
		t.Fatal("expected construction to fail on an injecting skill name")
	}
	if !strings.Contains(err.Error(), "parentheses") {
		t.Errorf("unexpected error: %v", err)
	}
}

// A valid list still produces one Skill(name) allow rule per entry.
func TestBuildCommandSkillRules(t *testing.T) {
	transport := newTestTransport(&SubprocessOptions{Skills: []string{"pdf", "docx"}})
	allowed, _ := flagValue(transport.buildCommand(), "--allowedTools")

	for _, want := range []string{"Skill(pdf)", "Skill(docx)"} {
		if !strings.Contains(allowed, want) {
			t.Errorf("--allowedTools %q missing %q", allowed, want)
		}
	}
}

func strPtr(s string) *string { return &s }

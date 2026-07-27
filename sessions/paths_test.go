package sessions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Expected values were produced with the reference implementation's algorithm
// (32-bit `h*31 + c` with JS wraparound, absolute value, base36).
func TestSimpleHash(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"", "0"},
		{"a", "2p"},
		{"/home/user/my.project", "sb9mg6"},
		{"/Users/dev/repo_name", "cq7fzs"},
		{strings.Repeat("x", 250), "mmfaww"},
		{"/home/user/" + strings.Repeat("deep/", 60) + "leaf", "aps5lr"},
		{"café/项目", "929nb7"},
	}

	for _, tc := range tests {
		if got := simpleHash(tc.in); got != tc.want {
			t.Errorf("simpleHash(%q) = %q, want %q", truncate(tc.in), got, tc.want)
		}
	}
}

// Every non-alphanumeric character becomes a hyphen -- not just separators.
// Sanitizing only separators resolves to a directory that does not exist.
func TestSanitizePath(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"dots become hyphens", "/home/user/my.project", "-home-user-my-project"},
		{"underscores become hyphens", "/Users/dev/repo_name", "-Users-dev-repo-name"},
		{"leading separator is preserved as a hyphen", "/a", "-a"},
		{"spaces become hyphens", "/home/my project", "-home-my-project"},
		{"alphanumerics survive", "abc123XYZ", "abc123XYZ"},
		{"empty", "", ""},
		{"non-ascii becomes hyphens", "café/项目", "caf----"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizePath(tc.in); got != tc.want {
				t.Errorf("sanitizePath(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizePathTruncatesLongPaths(t *testing.T) {
	long := "/home/user/" + strings.Repeat("deep/", 60) + "leaf"
	got := sanitizePath(long)

	if len(got) != maxSanitizedLength+1+len("aps5lr") {
		t.Errorf("unexpected length %d for %q", len(got), got)
	}
	if !strings.HasSuffix(got, "-aps5lr") {
		t.Errorf("expected hash suffix -aps5lr, got %q", got)
	}
	// The hash is computed over the ORIGINAL path, not the truncated form.
	if want := sanitizePath(long)[:maxSanitizedLength]; !strings.HasPrefix(got, want) {
		t.Errorf("expected truncated prefix %q", want)
	}
}

func TestSanitizePathBoundary(t *testing.T) {
	exactly := strings.Repeat("a", maxSanitizedLength)
	if got := sanitizePath(exactly); got != exactly {
		t.Errorf("a path sanitizing to exactly %d chars must not be hashed, got %q",
			maxSanitizedLength, got)
	}

	oneOver := strings.Repeat("a", maxSanitizedLength+1)
	if got := sanitizePath(oneOver); !strings.Contains(got, "-") {
		t.Errorf("a path one char over the limit must be hashed, got %q", got)
	}
}

func TestClaudeConfigHomeDirHonorsEnv(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "/custom/config")
	if got := claudeConfigHomeDir(); got != "/custom/config" {
		t.Errorf("claudeConfigHomeDir() = %q, want /custom/config", got)
	}
	if got := projectsDir(); got != filepath.Join("/custom/config", "projects") {
		t.Errorf("projectsDir() = %q", got)
	}
}

func TestClaudeConfigHomeDirDefault(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}
	if got, want := claudeConfigHomeDir(), filepath.Join(home, ".claude"); got != want {
		t.Errorf("claudeConfigHomeDir() = %q, want %q", got, want)
	}
}

// A project path containing punctuation must resolve to the directory the CLI
// actually writes -- this is the regression that made session listing return
// nothing for most real projects.
func TestFindProjectDirExactMatch(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	projectPath := "/home/user/my.project"
	want := filepath.Join(configDir, "projects", "-home-user-my-project")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findProjectDir(projectPath); got != want {
		t.Errorf("findProjectDir(%q) = %q, want %q", projectPath, got, want)
	}
}

func TestFindProjectDirMissing(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	if got := findProjectDir("/nonexistent/project"); got != "" {
		t.Errorf("expected empty string for a missing project, got %q", got)
	}
}

// Long paths may disagree on the hash suffix between the CLI and this SDK, so
// an exact miss falls back to prefix scanning.
func TestFindProjectDirPrefixFallback(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	longPath := "/home/user/" + strings.Repeat("deep/", 60) + "leaf"
	sanitized := sanitizePath(longPath)
	prefix := sanitized[:maxSanitizedLength]

	// A directory with the same prefix but a *different* hash suffix.
	want := filepath.Join(configDir, "projects", prefix+"-different")
	if err := os.MkdirAll(want, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findProjectDir(longPath); got != want {
		t.Errorf("findProjectDir fallback = %q, want %q", got, want)
	}
}

// Short paths must NOT prefix-scan: /root/project should not match
// /root/project-foo.
func TestFindProjectDirNoPrefixFallbackForShortPaths(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", configDir)

	decoy := filepath.Join(configDir, "projects", "-root-project-foo")
	if err := os.MkdirAll(decoy, 0o755); err != nil {
		t.Fatal(err)
	}

	if got := findProjectDir("/root/project"); got != "" {
		t.Errorf("short path must not prefix-match, got %q", got)
	}
}

func truncate(s string) string {
	if len(s) > 40 {
		return s[:40] + "..."
	}
	return s
}

package sessions

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/text/unicode/norm"
)

// maxSanitizedLength is the length past which the CLI truncates a sanitized
// project path and appends a hash suffix.
const maxSanitizedLength = 200

// simpleHash reproduces the CLI's 32-bit string hash, rendered in base36.
//
// This is the `h = h*31 + c` hash with JavaScript's `hash |= 0` semantics:
// arithmetic wraps to a 32-bit signed integer at every step. Go's int32
// overflow behavior matches that exactly, so no explicit masking is needed.
//
// The result is the absolute value formatted with strconv.FormatInt(_, 36),
// which matches JavaScript's Number.prototype.toString(36) for integers.
func simpleHash(s string) string {
	var h int32
	for _, r := range s {
		h = (h << 5) - h + int32(r)
	}
	// Widen before negating: -math.MinInt32 overflows int32 but is
	// representable in int64, and JavaScript's Math.abs produces 2147483648
	// for that input.
	v := int64(h)
	if v < 0 {
		v = -v
	}
	return strconv.FormatInt(v, 36)
}

// sanitizePath makes a directory path safe for use as a single directory name,
// matching the CLI's project-directory naming.
//
// Every non-alphanumeric character becomes a hyphen — not just path separators.
// A path such as "/home/user/my.project" therefore maps to
// "home-user-my-project", and sanitizing only separators would look in a
// directory that does not exist.
//
// Paths whose sanitized form exceeds maxSanitizedLength are truncated and
// suffixed with a hash of the *original* path.
func sanitizePath(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	sanitized := b.String()

	if len(sanitized) <= maxSanitizedLength {
		return sanitized
	}
	return sanitized[:maxSanitizedLength] + "-" + simpleHash(name)
}

// claudeConfigHomeDir returns the Claude configuration directory, honoring
// CLAUDE_CONFIG_DIR.
func claudeConfigHomeDir() string {
	if dir := os.Getenv("CLAUDE_CONFIG_DIR"); dir != "" {
		return norm.NFC.String(dir)
	}
	homeDir, _ := os.UserHomeDir()
	return norm.NFC.String(filepath.Join(homeDir, ".claude"))
}

// projectsDir returns the directory holding per-project session transcripts.
func projectsDir() string {
	return filepath.Join(claudeConfigHomeDir(), "projects")
}

// canonicalizePath resolves a directory to the canonical form the CLI records,
// following symlinks and normalizing to NFC.
//
// NFC matters on macOS, where the filesystem hands back decomposed (NFD)
// names: without normalization a path containing non-ASCII characters would
// sanitize differently than the CLI's own record of it.
func canonicalizePath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	return norm.NFC.String(abs)
}

// getProjectDir returns the exact project directory for a canonical path.
func getProjectDir(projectPath string) string {
	return filepath.Join(projectsDir(), sanitizePath(projectPath))
}

// findProjectDir locates the project directory for a path, tolerating hash
// mismatches on long paths.
//
// The CLI and this SDK can disagree on the hash suffix for paths past
// maxSanitizedLength, so an exact-match miss on a long path falls back to
// scanning for a directory sharing the truncated prefix. For short paths an
// exact-match miss simply means no sessions exist.
//
// Returns an empty string when no directory is found.
func findProjectDir(projectPath string) string {
	exact := getProjectDir(projectPath)
	if info, err := os.Stat(exact); err == nil && info.IsDir() {
		return exact
	}

	sanitized := sanitizePath(projectPath)
	if len(sanitized) <= maxSanitizedLength {
		return ""
	}

	prefix := sanitized[:maxSanitizedLength] + "-"
	entries, err := os.ReadDir(projectsDir())
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() && strings.HasPrefix(entry.Name(), prefix) {
			return filepath.Join(projectsDir(), entry.Name())
		}
	}
	return ""
}

// ProjectKeyForDirectory returns the sanitized project key for a directory,
// matching the CLI's project-directory naming.
//
// This is the default SessionStore project key, so a mirrored session lines up
// with the on-disk layout.
func ProjectKeyForDirectory(directory string) string {
	return sanitizePath(canonicalizePath(directory))
}

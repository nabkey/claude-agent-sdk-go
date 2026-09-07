package transport

import (
	"reflect"
	"runtime"
	"testing"
)

// isWindowsBatchPath is pure string logic so it can be tested off Windows,
// where the code actually runs. Every path component is classified, not just
// the final one, because Win32 normalization can make any of them effective.
func TestIsWindowsBatchPath(t *testing.T) {
	tests := []struct {
		name string
		path string
		want bool
	}{
		{"plain exe", `C:\Program Files\claude\claude.exe`, false},
		{"posix binary", "/usr/local/bin/claude", false},
		{"extensionless", `C:\tools\claude`, false},

		{"npm cmd shim", `C:\Users\me\AppData\npm\claude.cmd`, true},
		{"bat script", `C:\tools\claude.bat`, true},
		{"uppercase extension", `C:\tools\CLAUDE.CMD`, true},
		{"forward slashes", "C:/tools/claude.cmd", true},

		// Windows trims trailing dots and spaces at path resolution.
		{"trailing dot", `C:\tools\claude.cmd.`, true},
		{"trailing space", `C:\tools\claude.cmd `, true},
		{"trailing dots and spaces", `C:\tools\claude.cmd. . `, true},

		// A non-final component can become effective after normalization.
		{"batch-named directory", `C:\claude.cmd\..\claude.exe`, true},
		{"batch dir deep", `C:\a\evil.bat\b\claude.exe`, true},

		// NTFS stream specs: last-dot scan spans the stream, and a stream
		// spec also opens its base file.
		{"stream spec extension", `C:\tools\claude:evil.cmd`, true},
		{"batch with stream", `C:\tools\claude.cmd:stream`, true},
		{"drive-relative", `C:claude.cmd`, true},

		// A bare extension counts, as Win32 PathFindExtension treats it.
		{"bare extension", `C:\tools\.cmd`, true},

		{"similar but different suffix", `C:\tools\claude.command`, false},
		{"cmd in the middle of a name", `C:\tools\cmdclaude.exe`, false},
		{"empty", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isWindowsBatchPath(tc.path); got != tc.want {
				t.Errorf("isWindowsBatchPath(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestFindCmdMetacharacters(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"clean uuid", "6ba7b810-9dad-11d1-80b4-00c04fd430c8", nil},
		{"clean title", "My session title", nil},
		{"ampersand", "a&b", []string{"&"}},
		{"pipe and redirect", "a|b>c", []string{">", "|"}},
		{"percent expansion", "%PATH%", []string{"%"}},
		{"delayed expansion", "!VAR!", []string{"!"}},
		{"quote", `a"b`, []string{`"`}},
		{"caret", "a^b", []string{"^"}},
		{"newline", "a\nb", []string{"\n"}},
		{"carriage return", "a\rb", []string{"\r"}},
		{"deduplicated and sorted", "b&a&c|d", []string{"&", "|"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := findCmdMetacharacters(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("findCmdMetacharacters(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// Both gates are Windows-only. Assert the platform-conditional behavior
// directly so POSIX consumers keep accepting arbitrary session titles.
func TestWindowsGatesArePlatformConditional(t *testing.T) {
	onWindows := runtime.GOOS == "windows"

	batchErr := rejectWindowsBatchCLI(`C:\tools\claude.cmd`)
	if onWindows && batchErr == nil {
		t.Error("expected a batch-script refusal on Windows")
	}
	if !onWindows && batchErr != nil {
		t.Errorf("batch refusal must be a no-op off Windows, got: %v", batchErr)
	}

	// A resume value containing cmd.exe metacharacters: a legitimate session
	// title on POSIX, rejected on Windows.
	resume := "My session & more"
	optsErr := validateOptions(&SubprocessOptions{Resume: &resume})
	if onWindows && optsErr == nil {
		t.Error("expected Resume metacharacters to be refused on Windows")
	}
	if !onWindows && optsErr != nil {
		t.Errorf("Resume metacharacters must be accepted off Windows, got: %v", optsErr)
	}

	// A clean CLI path and resume value validate on every platform.
	if err := rejectWindowsBatchCLI("/usr/local/bin/claude"); err != nil {
		t.Errorf("native executable must always validate, got: %v", err)
	}
	clean := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	if err := validateOptions(&SubprocessOptions{Resume: &clean}); err != nil {
		t.Errorf("clean resume value must always validate, got: %v", err)
	}
}

// The SDK warns when the CLI is older than it supports, so the comparison has
// to handle the shapes a real `claude -v` produces.
func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"2.1.263", "2.1.263", 0},
		{"2.1.262", "2.1.263", -1},
		{"2.1.264", "2.1.263", 1},
		{"2.2.0", "2.1.263", 1},
		{"1.9.9", "2.0.0", -1},
		// A shorter version is padded with zeros rather than compared as
		// text, so 2.1 is not "greater" than 2.1.263.
		{"2.1", "2.1.263", -1},
		{"2.1.263", "2.1", 1},
		{"2", "2.0.0", 0},
		// Double-digit components must not compare lexically.
		{"2.1.9", "2.1.10", -1},
		{"2.10.0", "2.9.0", 1},
	}

	for _, tc := range tests {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

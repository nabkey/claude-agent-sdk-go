package transport

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
)

// cmdExeMetacharacters are cmd.exe metacharacters, plus the quote character
// cmd.exe uses to toggle its quoting state, and "!", which expands like "%"
// when delayed expansion is enabled.
const cmdExeMetacharacters = `&|<>^%!"`

// validateOptions rejects option combinations the CLI cannot honor, so callers
// fail fast at construction rather than mid-conversation.
func validateOptions(opts *SubprocessOptions) error {
	if opts.Model != nil && opts.FallbackModel != nil && *opts.Model == *opts.FallbackModel {
		return fmt.Errorf(
			"fallback model cannot be the same as the main model (%q); "+
				"specify a different model for FallbackModel", *opts.Model)
	}

	if opts.Resume != nil {
		if err := rejectWindowsCmdMetacharacters("Resume", *opts.Resume); err != nil {
			return err
		}
	}

	return nil
}

// isWindowsBatchPath reports whether any component of the path names a
// .bat/.cmd batch script.
//
// Deliberately plain string logic rather than path/filepath: the two disagree
// on several of these spellings depending on the host OS, and this check must
// behave identically wherever it runs so it can be tested off Windows.
//
// Every path component is classified, not only the final one. Win32 opens a
// path after lexical normalization ("." / ".." collapsing, repeated
// separators, position-dependent trailing dot/space trimming), and any attempt
// to re-derive the effective final component here would be a race against that
// ruleset. Refusing whenever ANY component carries a batch extension closes the
// whole class, and costs nothing legitimate: no real claude executable lives
// beneath a directory named like a batch file.
//
// Within a component, Win32 finds the extension with a last-dot scan over the
// whole component including any NTFS stream spec ("claude:evil.cmd" has
// extension ".cmd"), while a stream spec also opens its base file
// ("claude.cmd:stream" opens claude.cmd), and a drive prefix ("C:claude.cmd")
// rides in the same component. Splitting each component on ":" covers all of
// these; colons cannot appear in real file names, so nothing legitimate is
// over-refused.
func isWindowsBatchPath(cliPath string) bool {
	normalized := strings.ReplaceAll(cliPath, `\`, "/")
	for _, component := range strings.Split(normalized, "/") {
		for _, segment := range strings.Split(component, ":") {
			trimmed := strings.ToLower(strings.TrimRight(segment, ". "))
			if strings.HasSuffix(trimmed, ".bat") || strings.HasSuffix(trimmed, ".cmd") {
				return true
			}
		}
	}
	return false
}

// rejectWindowsBatchCLI refuses to execute a .bat/.cmd script as the CLI on
// Windows.
//
// Windows has no shebang mechanism: CreateProcess runs batch scripts by
// silently rewriting the spawn into a "cmd.exe /c" invocation, and cmd.exe
// re-parses the whole command line at execution time. Go's os/exec quotes
// arguments for the MSVCRT argv rules only, not for cmd.exe, so cmd.exe
// metacharacters inside an argument value -- for example a session title
// passed to --resume -- reach cmd.exe unescaped and can execute injected
// commands. Reliable escaping for cmd.exe does not exist (%VAR% expands even
// inside double quotes), so spawning a batch script with runtime-provided
// arguments cannot be made safe. Refusing is the same remediation Node.js
// shipped for this vulnerability class (CVE-2024-27980, "BatBadBut").
//
// In practice this refuses npm's claude.cmd shim. The alternatives in the
// error message avoid cmd.exe entirely.
func rejectWindowsBatchCLI(cliPath string) error {
	if runtime.GOOS != "windows" || !isWindowsBatchPath(cliPath) {
		return nil
	}
	return fmt.Errorf(
		"refusing to execute batch script %q: Windows runs .bat/.cmd files via "+
			"cmd.exe, which can execute commands injected through CLI arguments, "+
			"and no reliable escaping for cmd.exe exists. Use a native claude "+
			"executable instead: install Claude Code natively "+
			"(irm https://claude.ai/install.ps1 | iex), or point "+
			"AgentOptions.CLIPath at a claude.exe",
		cliPath)
}

// findCmdMetacharacters returns the sorted, deduplicated set of characters in
// value that are unsafe on a Windows command line.
func findCmdMetacharacters(value string) []string {
	seen := map[rune]bool{}
	for _, c := range value {
		if strings.ContainsRune(cmdExeMetacharacters, c) || c == '\r' || c == '\n' {
			seen[c] = true
		}
	}
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for c := range seen {
		out = append(out, string(c))
	}
	sort.Strings(out)
	return out
}

// rejectWindowsCmdMetacharacters is defense in depth for Windows.
//
// With batch-script spawning refused (rejectWindowsBatchCLI) these characters
// are harmless, since Go quotes correctly for native executables. They are
// rejected anyway so that Resume / SessionID values, which applications
// commonly take from external input, stay inert even if a cmd.exe hop is ever
// reintroduced between the SDK and the CLI. No format is imposed beyond this
// (resume values may be arbitrary session titles, not only UUIDs), and POSIX
// behavior is unchanged.
func rejectWindowsCmdMetacharacters(optionName, value string) error {
	if runtime.GOOS != "windows" {
		return nil
	}
	bad := findCmdMetacharacters(value)
	if len(bad) == 0 {
		return nil
	}
	return fmt.Errorf(
		"%s value %q contains characters that are unsafe to pass on a Windows "+
			"command line: %v", optionName, value, bad)
}

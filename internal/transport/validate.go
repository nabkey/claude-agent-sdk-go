package transport

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/nabkey/claude-agent-sdk-go/types"
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

	if opts.SessionID != nil {
		if err := rejectWindowsCmdMetacharacters("SessionID", *opts.SessionID); err != nil {
			return err
		}
	}

	if err := validateSkills(opts.Skills); err != nil {
		return err
	}

	return nil
}

// validateSkills rejects a Skills value that cannot be turned into rules.
//
// Only a []string of names or types.SkillsAll is meaningful. Any other value
// -- a bare string, a typed slice -- would silently install no skill filter
// at all, so it fails here instead.
func validateSkills(skills any) error {
	switch v := skills.(type) {
	case nil:
		return nil
	case []string:
		for _, name := range v {
			if err := validateSkillName(name); err != nil {
				return err
			}
		}
		return nil
	case string:
		if v == types.SkillsAll {
			return nil
		}
		return fmt.Errorf(
			"AgentOptions.Skills must be a []string of skill names or types.SkillsAll, "+
				"got the string %q; did you mean []string{%q}?", v, v)
	default:
		return fmt.Errorf(
			"AgentOptions.Skills must be a []string of skill names or types.SkillsAll, got %T",
			skills)
	}
}

// skillNameInvalidChars are characters a skill name may not contain.
//
// Parentheses and commas are delimiters to the --allowedTools tokenizer;
// control characters (C0, DEL, C1) never appear in a skill directory name.
// U+FEFF is listed here rather than left to the whitespace check because the
// CLI trims it as whitespace and Go's strings.TrimSpace does not.
func skillNameInvalidChars(r rune) bool {
	switch {
	case r == '(' || r == ')' || r == ',':
		return true
	case r <= 0x1f || (r >= 0x7f && r <= 0x9f):
		return true
	case r == '\ufeff':
		return true
	}
	return false
}

// validateSkillName rejects a skill name that cannot ride safely in a
// Skill(name) rule.
//
// Names from AgentOptions.Skills are formatted into the --allowedTools value,
// which the CLI splits into rules on commas and spaces outside parentheses.
// That tokenizer does not honor escape sequences -- escaping exists only in
// the per-rule grammar, applied after splitting -- so a name carrying a
// delimiter cannot be passed through reliably: what it tokenizes into depends
// on what surrounds it. A crafted name could inject extra permission rules.
//
// Names that tokenize cleanly but can never match the named skill are
// rejected too, so a dead rule fails loudly here rather than silently
// granting nothing.
func validateSkillName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("invalid skill name %q: skill names must be non-empty", name)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf(
			"invalid skill name %q: contains a surrogate or otherwise malformed "+
				"UTF-8 sequence, which can never match a skill the CLI discovered", name)
	}
	if name != strings.TrimSpace(name) {
		return fmt.Errorf(
			"invalid skill name %q: leading or trailing whitespace can never match "+
				"-- the Skill tool trims the invoked name", name)
	}
	if strings.ContainsFunc(name, skillNameInvalidChars) {
		return fmt.Errorf(
			"invalid skill name %q: parentheses, commas, control characters, and "+
				"byte-order marks are not allowed. Names match the skill's directory "+
				"name, or \"plugin:skill\" for plugin-qualified skills", name)
	}
	if name == "*" {
		return fmt.Errorf(
			"invalid skill name \"*\": use types.SkillsAll to enable every skill")
	}
	if strings.HasSuffix(name, ":*") || strings.HasSuffix(name, " *") {
		return fmt.Errorf(
			"invalid skill name %q: wildcard-suffix names are not allowed; list each "+
				"skill by its exact name", name)
	}
	if strings.HasPrefix(name, "/") {
		return fmt.Errorf(
			"invalid skill name %q: skill names may not start with '/'. The Skills "+
				"option takes the canonical name, not the slash-command form", name)
	}
	if strings.Contains(name, `\\`) {
		return fmt.Errorf(
			"invalid skill name %q: consecutive backslashes are not allowed -- the "+
				"per-rule parser collapses them, so the rule would name a different "+
				"skill", name)
	}
	if strings.HasSuffix(name, `\`) {
		return fmt.Errorf(
			"invalid skill name %q: names may not end with an unpaired backslash", name)
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

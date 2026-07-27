package claude

import (
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// shadowWarnOnce keeps a given warning to one emission per process, matching
// the reference SDKs. Shadowing is usually a config-level mistake, so
// repeating it once per query would be noise.
var shadowWarnOnce sync.Map

// warnIfCanUseToolShadowed warns when CanUseTool is set alongside options that
// will prevent it from ever being consulted.
//
// This is advisory, not an error: shadowing can be intentional, for instance a
// callback used only for tools outside AllowedTools.
func warnIfCanUseToolShadowed(o *AgentOptions) {
	if o.CanUseTool == nil {
		return
	}

	allowed := o.AllowedTools
	// A skills allowlist of "all" makes the transport append a bare "Skill"
	// to the effective allowed tools, so it shadows the callback just like a
	// hand-written entry. A list appends Skill(name) specifiers, which do not.
	if skills, ok := o.Skills.(string); ok && skills == types.SkillsAll {
		allowed = append(append([]string{}, allowed...), "Skill")
	}

	message := canUseToolShadowWarning(o.PermissionMode, allowed)
	if message == "" {
		return
	}

	if _, seen := shadowWarnOnce.LoadOrStore(message, struct{}{}); seen {
		return
	}

	if o.Warn != nil {
		o.Warn(message)
		return
	}
	log.Print("claude-agent-sdk: " + message)
}

// canUseToolShadowWarning returns the warning for these options, or "".
func canUseToolShadowWarning(mode *types.PermissionMode, allowedTools []string) string {
	if mode != nil && *mode == types.PermissionModeBypassPermissions {
		return "CanUseTool will not be invoked: permission mode 'bypassPermissions' " +
			"auto-approves every tool call (except explicit deny rules) before the " +
			"callback is consulted. To gate every tool call, use a PreToolUse hook instead."
	}

	// Deduplicate while preserving order: a redundant config such as
	// ["Read", "Read()"] resolves to the same tool and must not be listed twice.
	var shadowed []string
	seen := map[string]bool{}
	for _, entry := range allowedTools {
		tool := wholeToolAllowed(entry)
		if tool == "" || seen[tool] {
			continue
		}
		seen[tool] = true
		shadowed = append(shadowed, tool)
	}
	if len(shadowed) == 0 {
		return ""
	}

	return fmt.Sprintf(
		"CanUseTool will not be invoked for: %s. An AllowedTools entry that allows a "+
			"whole tool auto-approves it before the callback is consulted. To gate every "+
			"tool call, use a PreToolUse hook; or narrow the entry so calls fall through "+
			"to CanUseTool. Allow rules from settings files can also shadow the callback "+
			"but are not visible here.",
		strings.Join(shadowed, ", "))
}

// wholeToolAllowed returns the tool an AllowedTools entry allows outright, or
// "" if it does not allow one.
//
// This mirrors the CLI's rule parser: an entry allows a whole tool when it has
// no specifier ("Read"), or when the specifier is empty or a lone wildcard
// ("Read()", "Read(*)"). A real specifier such as "Bash(ls:*)" only allows
// matching invocations. Malformed entries fall back to the whole string as a
// tool name in the CLI, so they match nothing and are ignored here.
func wholeToolAllowed(entry string) string {
	if strings.TrimSpace(entry) == "" {
		return ""
	}

	open := strings.Index(entry, "(")
	if open == -1 {
		return entry
	}
	if open == 0 || !strings.HasSuffix(entry, ")") {
		return ""
	}

	switch entry[open+1 : len(entry)-1] {
	case "", "*":
		return entry[:open]
	default:
		return ""
	}
}

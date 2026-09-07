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

	emitShadowWarning(message, message, o.Warn)
}

// emitShadowWarning delivers a warning at most once per process.
//
// The dedup key is separate from the message because the runtime warning
// interpolates a call count, which would otherwise defeat deduplication.
func emitShadowWarning(key, message string, warn func(string)) {
	if _, seen := shadowWarnOnce.LoadOrStore(key, struct{}{}); seen {
		return
	}

	if warn != nil {
		warn(message)
		return
	}
	log.Print("claude-agent-sdk: " + message)
}

// warnFunc returns a warning sink for these options.
//
// AgentOptions.Warn documents that a nil callback logs to the standard
// logger, so that fallback lives here rather than at each call site.
func warnFunc(o *AgentOptions) func(string) {
	return func(message string) {
		if o != nil && o.Warn != nil {
			o.Warn(message)
			return
		}
		log.Print("claude-agent-sdk: " + message)
	}
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
		"CanUseTool will not be invoked for at least: %s. This list covers AgentOptions "+
			"only -- allow rules and permission modes from settings files shadow the "+
			"callback just as effectively and cannot be seen from here. An AllowedTools "+
			"entry that allows a whole tool auto-approves it before the callback is "+
			"consulted. To gate every tool call, use a PreToolUse hook; or narrow the "+
			"entry so calls fall through to CanUseTool.",
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

// --- runtime detection --------------------------------------------------

// shadowDetectorKey deduplicates the runtime warning across a process. The
// message interpolates a call count, so it cannot serve as its own key.
const shadowDetectorKey = "can-use-tool-never-consulted"

// shadowDetector notices a CanUseTool callback that is never consulted.
//
// canUseToolShadowWarning can only inspect the options, but allow rules in
// settings files shadow the callback just as effectively and are invisible
// from there. Since a nil SettingSources loads every filesystem settings file,
// that is now the ordinary case rather than an exotic one -- a user-level
// "defaultMode": "auto" disables the callback entirely with nothing to show
// for it. Watching what actually happens catches every shadowing source at
// once, including ones that do not exist yet, without reimplementing the CLI's
// settings precedence.
type shadowDetector struct {
	warn func(string)

	mu       sync.Mutex
	toolUses int
	consults int
}

// newShadowDetector returns a detector, or nil when there is no callback to
// watch. A nil detector is inert, so callers need not check.
func newShadowDetector(o *AgentOptions) *shadowDetector {
	if o.CanUseTool == nil {
		return nil
	}
	return &shadowDetector{warn: o.Warn}
}

// noteConsult records that the callback ran.
func (d *shadowDetector) noteConsult() {
	if d == nil {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.consults++
}

// observe counts tool calls and reports at the end of a turn.
//
// Counts accumulate across turns: a Client session whose first turn uses no
// tools is not evidence of anything, and folding later turns in keeps the
// judgement to "this session never consulted the callback".
func (d *shadowDetector) observe(msg types.Message) {
	if d == nil {
		return
	}

	switch m := msg.(type) {
	case *types.AssistantMessage:
		uses := 0
		for _, block := range m.Content {
			if _, ok := block.(*types.ToolUseBlock); ok {
				uses++
			}
		}
		if uses == 0 {
			return
		}
		d.mu.Lock()
		d.toolUses += uses
		d.mu.Unlock()

	case *types.ResultMessage:
		d.report()
	}
}

// report warns when tool calls ran without the callback ever being consulted.
//
// Only the total-shadow case warrants a warning. Partial shadowing is often
// deliberate -- a callback meant to gate everything except an explicitly
// allowed tool -- and canUseToolShadowWarning already covers the options that
// cause it.
func (d *shadowDetector) report() {
	d.mu.Lock()
	toolUses, consults := d.toolUses, d.consults
	d.mu.Unlock()

	if toolUses == 0 || consults > 0 {
		return
	}

	emitShadowWarning(shadowDetectorKey, fmt.Sprintf(
		"CanUseTool was set but never consulted: all %d tool call(s) in this session "+
			"were approved before the callback ran. Something auto-approved them "+
			"first -- most often \"permissions\" in the settings files that a nil "+
			"SettingSources loads, but AgentOptions, a sandbox, or the surrounding "+
			"environment can do it too. Pass SettingSources: []types.SettingSource{} "+
			"to rule out settings files, or use a PreToolUse hook to gate every call "+
			"unconditionally.",
		toolUses), d.warn)
}

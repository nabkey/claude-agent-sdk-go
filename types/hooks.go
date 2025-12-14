package types

import "context"

// HookInput is the interface for all hook input types.
type HookInput interface {
	isHookInput()
	GetSessionID() string
	GetHookEventName() HookEvent
}

// BaseHookInput contains fields common to all hook inputs.
type BaseHookInput struct {
	SessionID      string  `json:"session_id"`
	TranscriptPath string  `json:"transcript_path"`
	Cwd            string  `json:"cwd"`
	PermissionMode *string `json:"permission_mode,omitempty"`
}

func (b *BaseHookInput) GetSessionID() string { return b.SessionID }

// PreToolUseHookInput is the input for PreToolUse hook events.
type PreToolUseHookInput struct {
	BaseHookInput
	HookEventName HookEvent      `json:"hook_event_name"` // "PreToolUse"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
}

func (p *PreToolUseHookInput) isHookInput()              {}
func (p *PreToolUseHookInput) GetHookEventName() HookEvent { return HookEventPreToolUse }

// PostToolUseHookInput is the input for PostToolUse hook events.
type PostToolUseHookInput struct {
	BaseHookInput
	HookEventName HookEvent      `json:"hook_event_name"` // "PostToolUse"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	ToolResponse  any            `json:"tool_response"`
}

func (p *PostToolUseHookInput) isHookInput()              {}
func (p *PostToolUseHookInput) GetHookEventName() HookEvent { return HookEventPostToolUse }

// UserPromptSubmitHookInput is the input for UserPromptSubmit hook events.
type UserPromptSubmitHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "UserPromptSubmit"
	Prompt        string    `json:"prompt"`
}

func (u *UserPromptSubmitHookInput) isHookInput()              {}
func (u *UserPromptSubmitHookInput) GetHookEventName() HookEvent { return HookEventUserPromptSubmit }

// StopHookInput is the input for Stop hook events.
type StopHookInput struct {
	BaseHookInput
	HookEventName  HookEvent `json:"hook_event_name"` // "Stop"
	StopHookActive bool      `json:"stop_hook_active"`
}

func (s *StopHookInput) isHookInput()              {}
func (s *StopHookInput) GetHookEventName() HookEvent { return HookEventStop }

// SubagentStopHookInput is the input for SubagentStop hook events.
type SubagentStopHookInput struct {
	BaseHookInput
	HookEventName  HookEvent `json:"hook_event_name"` // "SubagentStop"
	StopHookActive bool      `json:"stop_hook_active"`
}

func (s *SubagentStopHookInput) isHookInput()              {}
func (s *SubagentStopHookInput) GetHookEventName() HookEvent { return HookEventSubagentStop }

// PreCompactHookInput is the input for PreCompact hook events.
type PreCompactHookInput struct {
	BaseHookInput
	HookEventName      HookEvent `json:"hook_event_name"` // "PreCompact"
	Trigger            string    `json:"trigger"`         // "manual" or "auto"
	CustomInstructions *string   `json:"custom_instructions,omitempty"`
}

func (p *PreCompactHookInput) isHookInput()              {}
func (p *PreCompactHookInput) GetHookEventName() HookEvent { return HookEventPreCompact }

// HookContext provides context for hook callbacks.
type HookContext struct {
	Signal any // Reserved for future abort signal support
}

// HookSpecificOutput is the interface for hook-specific output types.
type HookSpecificOutput interface {
	isHookSpecificOutput()
}

// PreToolUseHookSpecificOutput is the hook-specific output for PreToolUse events.
type PreToolUseHookSpecificOutput struct {
	HookEventName            string         `json:"hookEventName"` // "PreToolUse"
	PermissionDecision       *string        `json:"permissionDecision,omitempty"` // "allow", "deny", "ask"
	PermissionDecisionReason *string        `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
}

func (p *PreToolUseHookSpecificOutput) isHookSpecificOutput() {}

// PostToolUseHookSpecificOutput is the hook-specific output for PostToolUse events.
type PostToolUseHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "PostToolUse"
	AdditionalContext *string `json:"additionalContext,omitempty"`
}

func (p *PostToolUseHookSpecificOutput) isHookSpecificOutput() {}

// UserPromptSubmitHookSpecificOutput is the hook-specific output for UserPromptSubmit events.
type UserPromptSubmitHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "UserPromptSubmit"
	AdditionalContext *string `json:"additionalContext,omitempty"`
}

func (u *UserPromptSubmitHookSpecificOutput) isHookSpecificOutput() {}

// HookOutput is the output from a hook callback.
type HookOutput struct {
	// Control fields
	Continue       *bool   `json:"continue,omitempty"`
	SuppressOutput *bool   `json:"suppressOutput,omitempty"`
	StopReason     *string `json:"stopReason,omitempty"`

	// Decision fields
	Decision      *string `json:"decision,omitempty"` // "block"
	SystemMessage *string `json:"systemMessage,omitempty"`
	Reason        *string `json:"reason,omitempty"`

	// Async support
	Async        *bool `json:"async,omitempty"`
	AsyncTimeout *int  `json:"asyncTimeout,omitempty"`

	// Hook-specific output
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput,omitempty"`
}

// HookCallback is the function signature for hook handlers.
type HookCallback func(ctx context.Context, input HookInput, toolUseID *string, hookCtx *HookContext) (*HookOutput, error)

// HookMatcher defines a matcher and associated hooks for a specific event.
type HookMatcher struct {
	// Matcher is a pattern to match against (e.g., tool name like "Bash" or "Write|Edit").
	Matcher *string
	// Hooks is a list of callback functions to execute when matched.
	Hooks []HookCallback
	// Timeout is the timeout in seconds for all hooks in this matcher.
	Timeout *float64
}

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
	// PromptID identifies the prompt the event belongs to, when the CLI
	// reports one.
	PromptID string `json:"prompt_id,omitempty"`
	// Effort is the session's applied effort level, when one is set.
	Effort string `json:"effort,omitempty"`
}

func (b *BaseHookInput) GetSessionID() string { return b.SessionID }

// PreToolUseHookInput is the input for PreToolUse hook events.
type PreToolUseHookInput struct {
	BaseHookInput
	HookEventName HookEvent      `json:"hook_event_name"` // "PreToolUse"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	AgentID       *string        `json:"agent_id,omitempty"`
	AgentType     *string        `json:"agent_type,omitempty"`
	ToolUseID     string         `json:"tool_use_id,omitempty"`
}

func (p *PreToolUseHookInput) isHookInput()                {}
func (p *PreToolUseHookInput) GetHookEventName() HookEvent { return HookEventPreToolUse }

// PostToolUseHookInput is the input for PostToolUse hook events.
type PostToolUseHookInput struct {
	BaseHookInput
	HookEventName HookEvent      `json:"hook_event_name"` // "PostToolUse"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input"`
	ToolResponse  any            `json:"tool_response"`
	AgentID       *string        `json:"agent_id,omitempty"`
	AgentType     *string        `json:"agent_type,omitempty"`
	ToolUseID     string         `json:"tool_use_id,omitempty"`
}

func (p *PostToolUseHookInput) isHookInput()                {}
func (p *PostToolUseHookInput) GetHookEventName() HookEvent { return HookEventPostToolUse }

// UserPromptSubmitHookInput is the input for UserPromptSubmit hook events.
type UserPromptSubmitHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "UserPromptSubmit"
	Prompt        string    `json:"prompt"`
}

func (u *UserPromptSubmitHookInput) isHookInput()                {}
func (u *UserPromptSubmitHookInput) GetHookEventName() HookEvent { return HookEventUserPromptSubmit }

// StopHookInput is the input for Stop hook events.
type StopHookInput struct {
	BaseHookInput
	HookEventName  HookEvent `json:"hook_event_name"` // "Stop"
	StopHookActive bool      `json:"stop_hook_active"`
}

func (s *StopHookInput) isHookInput()                {}
func (s *StopHookInput) GetHookEventName() HookEvent { return HookEventStop }

// SubagentStopHookInput is the input for SubagentStop hook events.
type SubagentStopHookInput struct {
	BaseHookInput
	HookEventName       HookEvent `json:"hook_event_name"` // "SubagentStop"
	StopHookActive      bool      `json:"stop_hook_active"`
	AgentID             string    `json:"agent_id,omitempty"`
	AgentTranscriptPath string    `json:"agent_transcript_path,omitempty"`
	AgentType           string    `json:"agent_type,omitempty"`
}

func (s *SubagentStopHookInput) isHookInput()                {}
func (s *SubagentStopHookInput) GetHookEventName() HookEvent { return HookEventSubagentStop }

// PreCompactHookInput is the input for PreCompact hook events.
type PreCompactHookInput struct {
	BaseHookInput
	HookEventName      HookEvent `json:"hook_event_name"` // "PreCompact"
	Trigger            string    `json:"trigger"`         // "manual" or "auto"
	CustomInstructions *string   `json:"custom_instructions,omitempty"`
}

func (p *PreCompactHookInput) isHookInput()                {}
func (p *PreCompactHookInput) GetHookEventName() HookEvent { return HookEventPreCompact }

// PostToolUseFailureHookInput is the input for PostToolUseFailure hook events.
type PostToolUseFailureHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "PostToolUseFailure"
	ToolName      string    `json:"tool_name"`
	ToolInput     string    `json:"tool_input"`
	Error         string    `json:"error"`
	IsInterrupt   bool      `json:"is_interrupt"`
}

func (p *PostToolUseFailureHookInput) isHookInput() {}
func (p *PostToolUseFailureHookInput) GetHookEventName() HookEvent {
	return HookEventPostToolUseFailure
}

// SubagentStartHookInput is the input for SubagentStart hook events.
type SubagentStartHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "SubagentStart"
	AgentID       string    `json:"agent_id"`
	AgentType     string    `json:"agent_type"`
}

func (s *SubagentStartHookInput) isHookInput()                {}
func (s *SubagentStartHookInput) GetHookEventName() HookEvent { return HookEventSubagentStart }

// NotificationHookInput is the input for Notification hook events.
type NotificationHookInput struct {
	BaseHookInput
	HookEventName    HookEvent `json:"hook_event_name"` // "Notification"
	Message          string    `json:"message"`
	Title            string    `json:"title"`
	NotificationType string    `json:"notification_type"`
}

func (n *NotificationHookInput) isHookInput()                {}
func (n *NotificationHookInput) GetHookEventName() HookEvent { return HookEventNotification }

// PermissionRequestHookInput is the input for PermissionRequest hook events.
type PermissionRequestHookInput struct {
	BaseHookInput
	HookEventName         HookEvent          `json:"hook_event_name"` // "PermissionRequest"
	ToolName              string             `json:"tool_name"`
	ToolInput             map[string]any     `json:"tool_input"`
	PermissionSuggestions []PermissionUpdate `json:"permission_suggestions,omitempty"`
}

func (p *PermissionRequestHookInput) isHookInput()                {}
func (p *PermissionRequestHookInput) GetHookEventName() HookEvent { return HookEventPermissionRequest }

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
	HookEventName            string         `json:"hookEventName"`                // "PreToolUse"
	PermissionDecision       *string        `json:"permissionDecision,omitempty"` // "allow", "deny", "ask"
	PermissionDecisionReason *string        `json:"permissionDecisionReason,omitempty"`
	UpdatedInput             map[string]any `json:"updatedInput,omitempty"`
	AdditionalContext        *string        `json:"additionalContext,omitempty"`
}

func (p *PreToolUseHookSpecificOutput) isHookSpecificOutput() {}

// PostToolUseHookSpecificOutput is the hook-specific output for PostToolUse events.
type PostToolUseHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "PostToolUse"
	AdditionalContext *string `json:"additionalContext,omitempty"`
	// ClassifierContext is a short host-asserted note about the call's result
	// that auto mode's permission classifier reads alongside that result. It
	// is not shown to the model.
	ClassifierContext    *string `json:"classifierContext,omitempty"`
	UpdatedToolOutput    any     `json:"updatedToolOutput,omitempty"`
	UpdatedMCPToolOutput any     `json:"updatedMcpToolOutput,omitempty"`
}

func (p *PostToolUseHookSpecificOutput) isHookSpecificOutput() {}

// UserPromptSubmitHookSpecificOutput is the hook-specific output for UserPromptSubmit events.
type UserPromptSubmitHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "UserPromptSubmit"
	AdditionalContext *string `json:"additionalContext,omitempty"`
	// SessionTitle renames the session.
	SessionTitle *string `json:"sessionTitle,omitempty"`
	// SuppressOriginalPrompt drops the user's prompt text, leaving only
	// AdditionalContext. Use it when the hook fully replaces the prompt.
	SuppressOriginalPrompt *bool `json:"suppressOriginalPrompt,omitempty"`
}

func (u *UserPromptSubmitHookSpecificOutput) isHookSpecificOutput() {}

// UserPromptExpansionHookSpecificOutput is the hook-specific output for
// UserPromptExpansion events.
type UserPromptExpansionHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "UserPromptExpansion"
	AdditionalContext *string `json:"additionalContext,omitempty"`
	// SuppressOriginalPrompt drops the expanded prompt text, leaving only
	// AdditionalContext.
	SuppressOriginalPrompt *bool `json:"suppressOriginalPrompt,omitempty"`
}

func (u *UserPromptExpansionHookSpecificOutput) isHookSpecificOutput() {}

// SessionStartHookSpecificOutput is the hook-specific output for SessionStart
// events.
type SessionStartHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"` // "SessionStart"
	AdditionalContext *string `json:"additionalContext,omitempty"`
	// InitialUserMessage is auto-submitted as the session's first turn.
	InitialUserMessage *string `json:"initialUserMessage,omitempty"`
	// SessionTitle names the session.
	SessionTitle *string `json:"sessionTitle,omitempty"`
	// WatchPaths registers paths whose changes raise FileChanged hooks.
	WatchPaths []string `json:"watchPaths,omitempty"`
	// ReloadSkills rescans skills from disk before the session runs.
	ReloadSkills *bool `json:"reloadSkills,omitempty"`
}

func (s *SessionStartHookSpecificOutput) isHookSpecificOutput() {}

// PermissionDeniedHookSpecificOutput is the hook-specific output for
// PermissionDenied events.
type PermissionDeniedHookSpecificOutput struct {
	HookEventName string `json:"hookEventName"` // "PermissionDenied"
	// Retry re-runs the denied tool call, for a hook that fixed whatever
	// caused the denial.
	Retry *bool `json:"retry,omitempty"`
}

func (p *PermissionDeniedHookSpecificOutput) isHookSpecificOutput() {}

// PermissionDecision is a PermissionRequest hook's ruling on a tool call.
type PermissionDecision struct {
	// Behavior is PermissionBehaviorAllow or PermissionBehaviorDeny.
	Behavior PermissionBehavior `json:"behavior"`
	// UpdatedInput replaces the tool input, on an allow.
	UpdatedInput map[string]any `json:"updatedInput,omitempty"`
	// UpdatedPermissions are rule changes to apply, on an allow.
	UpdatedPermissions []PermissionUpdate `json:"updatedPermissions,omitempty"`
	// Message explains a denial to the model.
	Message *string `json:"message,omitempty"`
	// Interrupt ends the turn instead of letting the model continue after a
	// denial.
	Interrupt *bool `json:"interrupt,omitempty"`
}

// PermissionRequestHookSpecificOutput is the hook-specific output for
// PermissionRequest events. It answers the prompt outright.
type PermissionRequestHookSpecificOutput struct {
	HookEventName string              `json:"hookEventName"` // "PermissionRequest"
	Decision      *PermissionDecision `json:"decision,omitempty"`
}

func (p *PermissionRequestHookSpecificOutput) isHookSpecificOutput() {}

// ContextHookSpecificOutput is the hook-specific output for the events whose
// only output is additional context: Notification, Stop, SubagentStart,
// SubagentStop, PostToolUseFailure, and PostToolBatch.
//
// Set HookEventName to the event's own name; the CLI matches on it.
type ContextHookSpecificOutput struct {
	HookEventName     string  `json:"hookEventName"`
	AdditionalContext *string `json:"additionalContext,omitempty"`
}

func (c *ContextHookSpecificOutput) isHookSpecificOutput() {}

// HookOutput is the output from a hook callback.
type HookOutput struct {
	// Control fields
	Continue       *bool   `json:"continue,omitempty"`
	SuppressOutput *bool   `json:"suppressOutput,omitempty"`
	StopReason     *string `json:"stopReason,omitempty"`

	// Decision fields
	Decision      *string `json:"decision,omitempty"` // "approve" or "block"
	SystemMessage *string `json:"systemMessage,omitempty"`
	Reason        *string `json:"reason,omitempty"`
	// TerminalSequence is written to the host's terminal, for hooks that
	// drive a TUI.
	TerminalSequence *string `json:"terminalSequence,omitempty"`

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

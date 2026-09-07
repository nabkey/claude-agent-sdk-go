package types

// This file holds the hook input types for events beyond the tool and
// lifecycle core in hooks.go, plus the generic fallback used for an event this
// SDK version does not model. Registering a callback for any event in
// AllHookEvents works: a hook never fails to dispatch just because its input
// type is not spelled out here.

// GenericHookInput carries a hook event this SDK version does not model as a
// dedicated type.
//
// The CLI adds hook events faster than an SDK can name them, and an unknown
// event must reach the callback rather than fail the dispatch. Read the raw
// payload from Data; HookEventName says which event it is.
type GenericHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"`
	// Data is the full raw hook payload from the CLI.
	Data map[string]any `json:"-"`
}

func (g *GenericHookInput) isHookInput()                {}
func (g *GenericHookInput) GetHookEventName() HookEvent { return g.HookEventName }

// SessionStartSource is why a session began.
type SessionStartSource string

const (
	SessionStartStartup SessionStartSource = "startup"
	SessionStartResume  SessionStartSource = "resume"
	SessionStartClear   SessionStartSource = "clear"
	SessionStartCompact SessionStartSource = "compact"
	SessionStartFork    SessionStartSource = "fork"
)

// SessionStartHookInput is the input for SessionStart hook events.
type SessionStartHookInput struct {
	BaseHookInput
	HookEventName HookEvent          `json:"hook_event_name"` // "SessionStart"
	Source        SessionStartSource `json:"source"`
	AgentType     string             `json:"agent_type,omitempty"`
	Model         string             `json:"model,omitempty"`
	SessionTitle  string             `json:"session_title,omitempty"`
	// SecondsSinceLastResponse is the gap since the previous response, on a
	// resumed session.
	SecondsSinceLastResponse float64 `json:"seconds_since_last_response,omitempty"`
	ContextTokens            int     `json:"context_tokens,omitempty"`
	// PromptCacheLikelyExpired reports that the first turn will probably pay
	// a cache write.
	PromptCacheLikelyExpired bool    `json:"prompt_cache_likely_expired,omitempty"`
	EstimatedCacheWriteUSD   float64 `json:"estimated_cache_write_usd,omitempty"`
}

func (s *SessionStartHookInput) isHookInput()                {}
func (s *SessionStartHookInput) GetHookEventName() HookEvent { return HookEventSessionStart }

// ExitReason is why a session ended.
type ExitReason string

const (
	ExitReasonClear           ExitReason = "clear"
	ExitReasonResume          ExitReason = "resume"
	ExitReasonLogout          ExitReason = "logout"
	ExitReasonPromptInputExit ExitReason = "prompt_input_exit"
	ExitReasonOther           ExitReason = "other"
)

// SessionEndHookInput is the input for SessionEnd hook events.
type SessionEndHookInput struct {
	BaseHookInput
	HookEventName HookEvent  `json:"hook_event_name"` // "SessionEnd"
	Reason        ExitReason `json:"reason"`
}

func (s *SessionEndHookInput) isHookInput()                {}
func (s *SessionEndHookInput) GetHookEventName() HookEvent { return HookEventSessionEnd }

// PostCompactHookInput is the input for PostCompact hook events.
type PostCompactHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "PostCompact"
	// Trigger is "manual" or "auto".
	Trigger string `json:"trigger"`
	// CompactSummary is the summary that replaced the compacted transcript.
	CompactSummary string `json:"compact_summary,omitempty"`
}

func (p *PostCompactHookInput) isHookInput()                {}
func (p *PostCompactHookInput) GetHookEventName() HookEvent { return HookEventPostCompact }

// PermissionDeniedHookInput is the input for PermissionDenied hook events.
type PermissionDeniedHookInput struct {
	BaseHookInput
	HookEventName HookEvent      `json:"hook_event_name"` // "PermissionDenied"
	ToolName      string         `json:"tool_name"`
	ToolInput     map[string]any `json:"tool_input,omitempty"`
	ToolUseID     string         `json:"tool_use_id,omitempty"`
	// Reason is why the call was denied.
	Reason string `json:"reason,omitempty"`
}

func (p *PermissionDeniedHookInput) isHookInput()                {}
func (p *PermissionDeniedHookInput) GetHookEventName() HookEvent { return HookEventPermissionDenied }

// UserPromptExpansionType is what kind of expansion produced the prompt.
type UserPromptExpansionType string

const (
	UserPromptExpansionSlashCommand UserPromptExpansionType = "slash_command"
	UserPromptExpansionMCPPrompt    UserPromptExpansionType = "mcp_prompt"
)

// UserPromptExpansionHookInput is the input for UserPromptExpansion hook
// events, which fire while a slash command or MCP prompt is expanded.
type UserPromptExpansionHookInput struct {
	BaseHookInput
	HookEventName HookEvent               `json:"hook_event_name"` // "UserPromptExpansion"
	ExpansionType UserPromptExpansionType `json:"expansion_type"`
	CommandName   string                  `json:"command_name,omitempty"`
	CommandArgs   string                  `json:"command_args,omitempty"`
	CommandSource string                  `json:"command_source,omitempty"`
	// Prompt is the expanded prompt text.
	Prompt string `json:"prompt,omitempty"`
}

func (u *UserPromptExpansionHookInput) isHookInput() {}
func (u *UserPromptExpansionHookInput) GetHookEventName() HookEvent {
	return HookEventUserPromptExpansion
}

// StopFailureHookInput is the input for StopFailure hook events, which fire
// when a turn ends in an error rather than a normal stop.
type StopFailureHookInput struct {
	BaseHookInput
	HookEventName HookEvent             `json:"hook_event_name"` // "StopFailure"
	Error         AssistantMessageError `json:"error"`
	ErrorDetails  string                `json:"error_details,omitempty"`
	// LastAssistantMessage is the text of the last assistant message, if any.
	LastAssistantMessage string `json:"last_assistant_message,omitempty"`
}

func (s *StopFailureHookInput) isHookInput()                {}
func (s *StopFailureHookInput) GetHookEventName() HookEvent { return HookEventStopFailure }

// PostToolBatchToolCall is one call in a PostToolBatch event.
type PostToolBatchToolCall struct {
	ToolName     string         `json:"tool_name"`
	ToolInput    map[string]any `json:"tool_input,omitempty"`
	ToolUseID    string         `json:"tool_use_id,omitempty"`
	ToolResponse any            `json:"tool_response,omitempty"`
}

// PostToolBatchHookInput is the input for PostToolBatch hook events, which
// fire once after a parallel batch of tool calls completes.
type PostToolBatchHookInput struct {
	BaseHookInput
	HookEventName HookEvent               `json:"hook_event_name"` // "PostToolBatch"
	ToolCalls     []PostToolBatchToolCall `json:"tool_calls,omitempty"`
}

func (p *PostToolBatchHookInput) isHookInput()                {}
func (p *PostToolBatchHookInput) GetHookEventName() HookEvent { return HookEventPostToolBatch }

// SetupHookInput is the input for Setup hook events.
type SetupHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "Setup"
	// Trigger is "init" or "maintenance".
	Trigger string `json:"trigger,omitempty"`
}

func (s *SetupHookInput) isHookInput()                {}
func (s *SetupHookInput) GetHookEventName() HookEvent { return HookEventSetup }

// TaskCreatedHookInput is the input for TaskCreated hook events.
type TaskCreatedHookInput struct {
	BaseHookInput
	HookEventName   HookEvent `json:"hook_event_name"` // "TaskCreated"
	TaskID          string    `json:"task_id"`
	TaskSubject     string    `json:"task_subject,omitempty"`
	TaskDescription string    `json:"task_description,omitempty"`
	TeammateName    string    `json:"teammate_name,omitempty"`
	TeamName        string    `json:"team_name,omitempty"`
}

func (t *TaskCreatedHookInput) isHookInput()                {}
func (t *TaskCreatedHookInput) GetHookEventName() HookEvent { return HookEventTaskCreated }

// TaskCompletedHookInput is the input for TaskCompleted hook events.
type TaskCompletedHookInput struct {
	BaseHookInput
	HookEventName   HookEvent `json:"hook_event_name"` // "TaskCompleted"
	TaskID          string    `json:"task_id"`
	TaskSubject     string    `json:"task_subject,omitempty"`
	TaskDescription string    `json:"task_description,omitempty"`
	TeammateName    string    `json:"teammate_name,omitempty"`
	TeamName        string    `json:"team_name,omitempty"`
}

func (t *TaskCompletedHookInput) isHookInput()                {}
func (t *TaskCompletedHookInput) GetHookEventName() HookEvent { return HookEventTaskCompleted }

// TeammateIdleHookInput is the input for TeammateIdle hook events.
type TeammateIdleHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "TeammateIdle"
	TeammateName  string    `json:"teammate_name"`
	TeamName      string    `json:"team_name,omitempty"`
}

func (t *TeammateIdleHookInput) isHookInput()                {}
func (t *TeammateIdleHookInput) GetHookEventName() HookEvent { return HookEventTeammateIdle }

// ConfigChangeHookInput is the input for ConfigChange hook events.
type ConfigChangeHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "ConfigChange"
	// Source is which settings layer changed, e.g. "user_settings",
	// "project_settings", "local_settings", "policy_settings", "skills".
	Source   string `json:"source"`
	FilePath string `json:"file_path,omitempty"`
}

func (c *ConfigChangeHookInput) isHookInput()                {}
func (c *ConfigChangeHookInput) GetHookEventName() HookEvent { return HookEventConfigChange }

// CwdChangedHookInput is the input for CwdChanged hook events.
type CwdChangedHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "CwdChanged"
	OldCwd        string    `json:"old_cwd,omitempty"`
	NewCwd        string    `json:"new_cwd,omitempty"`
}

func (c *CwdChangedHookInput) isHookInput()                {}
func (c *CwdChangedHookInput) GetHookEventName() HookEvent { return HookEventCwdChanged }

// FileChangedHookInput is the input for FileChanged hook events.
type FileChangedHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "FileChanged"
	FilePath      string    `json:"file_path"`
	// Event is "change", "add" or "unlink".
	Event string `json:"event,omitempty"`
}

func (f *FileChangedHookInput) isHookInput()                {}
func (f *FileChangedHookInput) GetHookEventName() HookEvent { return HookEventFileChanged }

// DirectoryAddedHookInput is the input for DirectoryAdded hook events.
type DirectoryAddedHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "DirectoryAdded"
	Directory     string    `json:"directory"`
	// Source is "slash_command" or "register_repo_root".
	Source string `json:"source,omitempty"`
}

func (d *DirectoryAddedHookInput) isHookInput()                {}
func (d *DirectoryAddedHookInput) GetHookEventName() HookEvent { return HookEventDirectoryAdded }

// MessageDisplayHookInput is the input for MessageDisplay hook events, which
// fire as assistant text is rendered.
type MessageDisplayHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "MessageDisplay"
	TurnID        string    `json:"turn_id,omitempty"`
	MessageID     string    `json:"message_id,omitempty"`
	Index         int       `json:"index,omitempty"`
	// Final marks the last delta of a message.
	Final bool   `json:"final,omitempty"`
	Delta string `json:"delta,omitempty"`
}

func (m *MessageDisplayHookInput) isHookInput()                {}
func (m *MessageDisplayHookInput) GetHookEventName() HookEvent { return HookEventMessageDisplay }

// InstructionsLoadedHookInput is the input for InstructionsLoaded hook events,
// which fire when a memory file is loaded into context.
type InstructionsLoadedHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "InstructionsLoaded"
	FilePath      string    `json:"file_path"`
	// MemoryType is "User", "Project", "Local" or "Managed".
	MemoryType string `json:"memory_type,omitempty"`
	// LoadReason is why the file was loaded, e.g. "session_start",
	// "nested_traversal", "path_glob_match", "include", "compact".
	LoadReason      string   `json:"load_reason,omitempty"`
	Globs           []string `json:"globs,omitempty"`
	TriggerFilePath string   `json:"trigger_file_path,omitempty"`
	ParentFilePath  string   `json:"parent_file_path,omitempty"`
}

func (i *InstructionsLoadedHookInput) isHookInput() {}
func (i *InstructionsLoadedHookInput) GetHookEventName() HookEvent {
	return HookEventInstructionsLoaded
}

// WorktreeCreateHookInput is the input for WorktreeCreate hook events.
type WorktreeCreateHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "WorktreeCreate"
	Name          string    `json:"name,omitempty"`
}

func (w *WorktreeCreateHookInput) isHookInput()                {}
func (w *WorktreeCreateHookInput) GetHookEventName() HookEvent { return HookEventWorktreeCreate }

// WorktreeRemoveHookInput is the input for WorktreeRemove hook events.
type WorktreeRemoveHookInput struct {
	BaseHookInput
	HookEventName HookEvent `json:"hook_event_name"` // "WorktreeRemove"
	WorktreePath  string    `json:"worktree_path,omitempty"`
}

func (w *WorktreeRemoveHookInput) isHookInput()                {}
func (w *WorktreeRemoveHookInput) GetHookEventName() HookEvent { return HookEventWorktreeRemove }

// ElicitationHookInput is the input for Elicitation hook events, which fire
// when an MCP server asks for user input.
type ElicitationHookInput struct {
	BaseHookInput
	HookEventName   HookEvent       `json:"hook_event_name"` // "Elicitation"
	MCPServerName   string          `json:"mcp_server_name"`
	Message         string          `json:"message,omitempty"`
	Mode            ElicitationMode `json:"mode,omitempty"`
	URL             string          `json:"url,omitempty"`
	ElicitationID   string          `json:"elicitation_id,omitempty"`
	RequestedSchema map[string]any  `json:"requested_schema,omitempty"`
}

func (e *ElicitationHookInput) isHookInput()                {}
func (e *ElicitationHookInput) GetHookEventName() HookEvent { return HookEventElicitation }

// ElicitationResultHookInput is the input for ElicitationResult hook events.
type ElicitationResultHookInput struct {
	BaseHookInput
	HookEventName HookEvent         `json:"hook_event_name"` // "ElicitationResult"
	MCPServerName string            `json:"mcp_server_name"`
	ElicitationID string            `json:"elicitation_id,omitempty"`
	Mode          ElicitationMode   `json:"mode,omitempty"`
	Action        ElicitationAction `json:"action"`
	Content       map[string]any    `json:"content,omitempty"`
}

func (e *ElicitationResultHookInput) isHookInput() {}
func (e *ElicitationResultHookInput) GetHookEventName() HookEvent {
	return HookEventElicitationResult
}

// ModelSwitch carries the fields common to the model-switch hook events.
type ModelSwitch struct {
	FromModel string `json:"from_model,omitempty"`
	ToModel   string `json:"to_model,omitempty"`
	// RequestedModel is the selector the caller asked for, which may be an
	// alias. Empty when the switch was not explicitly requested.
	RequestedModel string `json:"requested_model,omitempty"`
	// Source is where the switch came from, e.g. "command", "picker", "sdk",
	// and for PostModelSwitch also "auto" or "resume".
	Source        string `json:"source,omitempty"`
	ContextTokens int    `json:"context_tokens,omitempty"`
	// PromptCacheWarm reports whether the prompt cache is expected to hit.
	PromptCacheWarm bool `json:"prompt_cache_warm,omitempty"`
	// CacheTTL is "5m" or "1h".
	CacheTTL               string  `json:"cache_ttl,omitempty"`
	EstimatedCacheWriteUSD float64 `json:"estimated_cache_write_usd,omitempty"`
	// Pricing is "configured", "catalog" or "default".
	Pricing string `json:"pricing,omitempty"`
}

// PreModelSwitchHookInput is the input for PreModelSwitch hook events.
type PreModelSwitchHookInput struct {
	BaseHookInput
	ModelSwitch
	HookEventName HookEvent `json:"hook_event_name"` // "PreModelSwitch"
}

func (p *PreModelSwitchHookInput) isHookInput()                {}
func (p *PreModelSwitchHookInput) GetHookEventName() HookEvent { return HookEventPreModelSwitch }

// PostModelSwitchHookInput is the input for PostModelSwitch hook events.
type PostModelSwitchHookInput struct {
	BaseHookInput
	ModelSwitch
	HookEventName HookEvent `json:"hook_event_name"` // "PostModelSwitch"
}

func (p *PostModelSwitchHookInput) isHookInput()                {}
func (p *PostModelSwitchHookInput) GetHookEventName() HookEvent { return HookEventPostModelSwitch }

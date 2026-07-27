package types

// ServerToolName identifies a tool the API executes server-side on the
// model's behalf.
type ServerToolName string

const (
	ServerToolAdvisor                 ServerToolName = "advisor"
	ServerToolWebSearch               ServerToolName = "web_search"
	ServerToolWebFetch                ServerToolName = "web_fetch"
	ServerToolCodeExecution           ServerToolName = "code_execution"
	ServerToolBashCodeExecution       ServerToolName = "bash_code_execution"
	ServerToolTextEditorCodeExecution ServerToolName = "text_editor_code_execution"
	ServerToolSearchRegex             ServerToolName = "tool_search_tool_regex"
	ServerToolSearchBM25              ServerToolName = "tool_search_tool_bm25"
)

// ServerToolUseBlock is a server-side tool invocation, e.g. web search.
//
// These run on the API's side, so they appear in the message stream alongside
// regular tool_use blocks but the caller never returns a result. Branch on
// Name to know which server tool ran.
type ServerToolUseBlock struct {
	ID    string         `json:"id"`
	Name  ServerToolName `json:"name"`
	Input map[string]any `json:"input"`
}

func (b *ServerToolUseBlock) isContentBlock() {}

// ServerToolResultBlock is the result of a server-side tool call.
//
// Content is the raw payload from the API and is opaque here; callers that
// care about a specific server tool's schema can inspect Content["type"].
type ServerToolResultBlock struct {
	ToolUseID string         `json:"tool_use_id"`
	Content   map[string]any `json:"content"`
}

func (b *ServerToolResultBlock) isContentBlock() {}

// ModelUsage is a per-model token and cost breakdown.
type ModelUsage struct {
	InputTokens              int     `json:"inputTokens"`
	OutputTokens             int     `json:"outputTokens"`
	CacheReadInputTokens     int     `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int     `json:"cacheCreationInputTokens"`
	WebSearchRequests        int     `json:"webSearchRequests"`
	CostUSD                  float64 `json:"costUSD"`
	ContextWindow            int     `json:"contextWindow"`
	MaxOutputTokens          int     `json:"maxOutputTokens"`
	// CanonicalModel is the model ID used for the pricing lookup, which may
	// differ from the key this entry is filed under.
	CanonicalModel string `json:"canonicalModel,omitempty"`
	// Provider is the API provider that served this model, e.g. firstParty,
	// bedrock, vertex.
	Provider string `json:"provider,omitempty"`
}

// ModelUsageFromAny decodes the per-model usage map.
func ModelUsageFromAny(raw any) map[string]ModelUsage {
	entries, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	out := make(map[string]ModelUsage, len(entries))
	for model, item := range entries {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out[model] = ModelUsage{
			InputTokens:              mapInt(m, "inputTokens"),
			OutputTokens:             mapInt(m, "outputTokens"),
			CacheReadInputTokens:     mapInt(m, "cacheReadInputTokens"),
			CacheCreationInputTokens: mapInt(m, "cacheCreationInputTokens"),
			WebSearchRequests:        mapInt(m, "webSearchRequests"),
			CostUSD:                  mapFloat(m, "costUSD"),
			ContextWindow:            mapInt(m, "contextWindow"),
			MaxOutputTokens:          mapInt(m, "maxOutputTokens"),
			CanonicalModel:           mapString(m, "canonicalModel"),
			Provider:                 mapString(m, "provider"),
		}
	}
	return out
}

// DeferredToolUse is a tool call deferred by a PreToolUse hook.
//
// When a PreToolUse hook returns permissionDecision "defer", the run stops and
// the result message carries the deferred call here so the caller can inspect
// it and decide whether to resume.
type DeferredToolUse struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// PermissionDenial records a tool call that was denied.
type PermissionDenial struct {
	ToolName  string         `json:"tool_name"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
}

// PermissionDenialsFromAny decodes the permission denial list.
func PermissionDenialsFromAny(raw any) []PermissionDenial {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]PermissionDenial, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		denial := PermissionDenial{
			ToolName:  mapString(m, "tool_name"),
			ToolUseID: mapString(m, "tool_use_id"),
		}
		if input, ok := m["tool_input"].(map[string]any); ok {
			denial.ToolInput = input
		}
		out = append(out, denial)
	}
	return out
}

// TaskUpdatedMessage reports a change to a background task's state.
//
// A background task's terminal state can arrive *only* here, with no
// accompanying TaskNotificationMessage -- a task stopped via a stop request
// reports status "killed" and the matching notification is sometimes
// suppressed. Consumers tracking active task IDs should therefore clear them
// on a terminal status (see IsTerminalTaskStatus) from either message.
type TaskUpdatedMessage struct {
	SystemMessage
	TaskID string `json:"task_id"`
	// Patch carries the changed fields.
	Patch map[string]any `json:"patch,omitempty"`
	// Status is the patched status, if the patch carried one. A patch with
	// only end_time or error leaves this empty.
	Status    TaskUpdatedStatus `json:"status,omitempty"`
	SessionID string            `json:"session_id,omitempty"`
	UUID      string            `json:"uuid,omitempty"`
}

// HookEventMessage is a hook lifecycle event, emitted when
// AgentOptions.IncludeHookEvents is set.
//
// These arrive as system messages with subtype hook_started or hook_response;
// the latter carries output, exit_code and outcome in Data.
type HookEventMessage struct {
	SystemMessage
	// HookEventName is the hook that fired, e.g. PreToolUse.
	HookEventName string `json:"hook_event_name,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	UUID          string `json:"uuid,omitempty"`
}

// MirrorErrorMessage reports a failed SessionStore append.
//
// Non-fatal: the local transcript is already durable, so the session continues.
// The mirrored copy will be missing the failed batch.
type MirrorErrorMessage struct {
	SystemMessage
	Key   *SessionKey `json:"key,omitempty"`
	Error string      `json:"error,omitempty"`
}

// CompactBoundaryMessage marks where context compaction occurred.
type CompactBoundaryMessage struct {
	SystemMessage
	// Trigger is "manual" or "auto".
	Trigger string `json:"trigger,omitempty"`
	// PreTokens is the context size before compaction.
	PreTokens int    `json:"pre_tokens,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	UUID      string `json:"uuid,omitempty"`
}

// SessionStateChangedMessage reports the session's run state.
//
// The "idle" state is the authoritative turn-over signal: it fires after any
// held-back result flushes and the background-agent loop exits.
type SessionStateChangedMessage struct {
	SystemMessage
	// State is "idle", "running" or "requires_action".
	State     string `json:"state,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	UUID      string `json:"uuid,omitempty"`
}

// PermissionDeniedMessage reports a denied tool call.
type PermissionDeniedMessage struct {
	SystemMessage
	ToolName  string         `json:"tool_name,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	ToolInput map[string]any `json:"tool_input,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	UUID      string         `json:"uuid,omitempty"`
}

// APIRetryMessage reports that an API call is being retried.
type APIRetryMessage struct {
	SystemMessage
	Attempt      int    `json:"attempt,omitempty"`
	MaxRetries   int    `json:"max_retries,omitempty"`
	RetryDelayMS int    `json:"retry_delay_ms,omitempty"`
	ErrorStatus  *int   `json:"error_status,omitempty"`
	SessionID    string `json:"session_id,omitempty"`
	UUID         string `json:"uuid,omitempty"`
}

// StatusMessage reports a transient session status such as compacting.
type StatusMessage struct {
	SystemMessage
	// Status is "compacting", "requesting", or empty when cleared.
	Status    string `json:"status,omitempty"`
	SessionID string `json:"session_id,omitempty"`
	UUID      string `json:"uuid,omitempty"`
}

// ToolProgressMessage reports incremental progress from a running tool.
type ToolProgressMessage struct {
	SystemMessage
	ToolUseID string         `json:"tool_use_id,omitempty"`
	ToolName  string         `json:"tool_name,omitempty"`
	Progress  map[string]any `json:"progress,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
	UUID      string         `json:"uuid,omitempty"`
}

// BackgroundTasksChangedMessage reports the live set of background tasks.
type BackgroundTasksChangedMessage struct {
	SystemMessage
	Tasks     []map[string]any `json:"tasks,omitempty"`
	SessionID string           `json:"session_id,omitempty"`
	UUID      string           `json:"uuid,omitempty"`
}

// PromptSuggestionMessage carries a predicted next user prompt.
//
// Emitted after the result message when AgentOptions.PromptSuggestions is set,
// so consumers must keep iterating past the result to receive it.
type PromptSuggestionMessage struct {
	SystemMessage
	Suggestion string `json:"suggestion,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	UUID       string `json:"uuid,omitempty"`
}

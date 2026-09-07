package types

import (
	"encoding/json"
	"time"
)

// ContentBlock is a marker interface for message content blocks.
// Implementations include TextBlock, ThinkingBlock, ToolUseBlock, and ToolResultBlock.
type ContentBlock interface {
	isContentBlock()
}

// TextBlock represents a text content block.
type TextBlock struct {
	Text string `json:"text"`
}

func (t *TextBlock) isContentBlock() {}

// ThinkingBlock represents a thinking/reasoning content block.
type ThinkingBlock struct {
	Thinking  string `json:"thinking"`
	Signature string `json:"signature"`
}

func (t *ThinkingBlock) isContentBlock() {}

// ToolUseBlock represents a tool invocation request.
type ToolUseBlock struct {
	ID    string         `json:"id"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

func (t *ToolUseBlock) isContentBlock() {}

// ToolResultBlock represents the result of a tool execution.
type ToolResultBlock struct {
	ToolUseID string `json:"tool_use_id"`
	Content   any    `json:"content,omitempty"` // Can be string or []map[string]any
	IsError   *bool  `json:"is_error,omitempty"`
}

func (t *ToolResultBlock) isContentBlock() {}

// Message is a marker interface for all message types.
// Implementations include UserMessage, AssistantMessage, SystemMessage, and ResultMessage.
type Message interface {
	isMessage()
}

// UserMessage represents a user input message.
type UserMessage struct {
	Content         any            `json:"content"` // Can be string or []ContentBlock
	ParentToolUseID *string        `json:"parent_tool_use_id,omitempty"`
	UUID            *string        `json:"uuid,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
	ToolUseResult   map[string]any `json:"tool_use_result,omitempty"`
	// Origin is the provenance of this message. Nil when the CLI did not
	// attribute it. Populated on injected turns -- task notifications,
	// channel and peer messages -- and on user messages the CLI replays;
	// tool-result messages never carry it.
	Origin *MessageOrigin `json:"origin,omitempty"`
	// IsSynthetic marks a message the CLI generated rather than one the
	// caller submitted.
	IsSynthetic bool `json:"isSynthetic,omitempty"`
}

func (m *UserMessage) isMessage() {}

// AssistantMessage represents a response from Claude.
type AssistantMessage struct {
	Content         []ContentBlock         `json:"-"` // Custom unmarshal
	Model           string                 `json:"model"`
	ParentToolUseID *string                `json:"parent_tool_use_id,omitempty"`
	Error           *AssistantMessageError `json:"error,omitempty"`
	Usage           map[string]any         `json:"usage,omitempty"`
	// MessageID is the API's message identifier.
	MessageID string `json:"message_id,omitempty"`
	// StopReason is why the model stopped generating.
	StopReason string `json:"stop_reason,omitempty"`
	SessionID  string `json:"session_id,omitempty"`
	UUID       string `json:"uuid,omitempty"`
	// UserMessageUUID is the client uuid of the user message this reply
	// answers, echoed back so a streaming-input consumer can bind a reply to
	// its own send. Empty on older CLI builds and on synthetic turns.
	UserMessageUUID string `json:"user_message_uuid,omitempty"`
	// UserMessageUUIDs lists every user message whose prompt this turn
	// consumed, in consumption order, for turns that merged several sends.
	// Always contains UserMessageUUID when present.
	UserMessageUUIDs []string `json:"user_message_uuids,omitempty"`
	// ContextUsage is the structured payload behind a /context result, so a
	// host can render the context card without parsing the markdown table.
	ContextUsage *SDKContextUsage `json:"context_usage,omitempty"`
}

func (m *AssistantMessage) isMessage() {}

// SystemMessage represents a system message with metadata.
type SystemMessage struct {
	Subtype string         `json:"subtype"`
	Data    map[string]any `json:"data,omitempty"`
}

func (m *SystemMessage) isMessage() {}

// ResultMessage represents the final result of a query with cost and usage info.
type ResultMessage struct {
	Subtype          string         `json:"subtype"`
	DurationMS       int            `json:"duration_ms"`
	DurationAPIMS    int            `json:"duration_api_ms"`
	IsError          bool           `json:"is_error"`
	NumTurns         int            `json:"num_turns"`
	SessionID        string         `json:"session_id"`
	TotalCostUSD     *float64       `json:"total_cost_usd,omitempty"`
	Usage            map[string]any `json:"usage,omitempty"`
	Result           *string        `json:"result,omitempty"`
	StructuredOutput any            `json:"structured_output,omitempty"`
	StopReason       *string        `json:"stop_reason,omitempty"`
	UUID             string         `json:"uuid,omitempty"`

	// ModelUsage breaks token use and cost down per model.
	ModelUsage map[string]ModelUsage `json:"modelUsage,omitempty"`
	// PermissionDenials lists tool calls that were denied during the run.
	PermissionDenials []PermissionDenial `json:"permission_denials,omitempty"`
	// DeferredToolUse is set when a PreToolUse hook deferred a tool call,
	// which ends the run and hands the call back for the caller to decide on.
	DeferredToolUse *DeferredToolUse `json:"deferred_tool_use,omitempty"`
	// Errors carries the structured error text when IsError is set.
	Errors []string `json:"errors,omitempty"`
	// APIErrorStatus is the HTTP status of the failing API call when IsError
	// is set on an otherwise successful subtype.
	APIErrorStatus *int `json:"api_error_status,omitempty"`
	// TerminalReason explains why the query loop stopped. An aborted reason
	// means the turn was interrupted. Empty on CLIs that do not report it.
	TerminalReason TerminalReason `json:"terminal_reason,omitempty"`
	// Origin is the provenance of the user message that triggered this turn.
	// It lets a streaming-input consumer tell the result of its own prompt
	// (nil, or a human origin it stamped) from results of injected turns.
	Origin *MessageOrigin `json:"origin,omitempty"`
	// QueuedTurnCount is the number of user-initiated sends still waiting in
	// the command queue when this result was produced. Greater than zero
	// means another turn and result follow without further input. Nil when
	// the CLI does not report it.
	QueuedTurnCount *int `json:"queued_turn_count,omitempty"`
	// UserMessageUUID is the client uuid of the user message that triggered
	// this turn, echoed back as a join key.
	UserMessageUUID string `json:"user_message_uuid,omitempty"`
	// UserMessageUUIDs lists every user message whose prompt this turn
	// consumed, in consumption order. Always contains UserMessageUUID when
	// present; fall back to UserMessageUUID on older CLI builds.
	UserMessageUUIDs []string `json:"user_message_uuids,omitempty"`
}

func (m *ResultMessage) isMessage() {}

// StreamEvent represents a partial message update during streaming.
type StreamEvent struct {
	UUID            string         `json:"uuid"`
	SessionID       string         `json:"session_id"`
	Event           map[string]any `json:"event"`
	ParentToolUseID *string        `json:"parent_tool_use_id,omitempty"`
	// UserMessageUUID is the client uuid of the user message this turn
	// answers, present on the first stream event of each turn.
	UserMessageUUID string `json:"user_message_uuid,omitempty"`
	// UserMessageUUIDs lists every user message whose prompt this turn
	// consumed, when several sends were merged into one turn.
	UserMessageUUIDs []string `json:"user_message_uuids,omitempty"`
}

func (m *StreamEvent) isMessage() {}

// RawMessage is used for parsing messages before determining their type.
type RawMessage struct {
	Type            string          `json:"type"`
	Subtype         string          `json:"subtype,omitempty"`
	Message         json.RawMessage `json:"message,omitempty"`
	ParentToolUseID *string         `json:"parent_tool_use_id,omitempty"`

	// Result fields
	DurationMS       int            `json:"duration_ms,omitempty"`
	DurationAPIMS    int            `json:"duration_api_ms,omitempty"`
	IsError          bool           `json:"is_error,omitempty"`
	NumTurns         int            `json:"num_turns,omitempty"`
	SessionID        string         `json:"session_id,omitempty"`
	TotalCostUSD     *float64       `json:"total_cost_usd,omitempty"`
	Usage            map[string]any `json:"usage,omitempty"`
	Result           *string        `json:"result,omitempty"`
	StructuredOutput any            `json:"structured_output,omitempty"`

	// StreamEvent fields
	UUID  string         `json:"uuid,omitempty"`
	Event map[string]any `json:"event,omitempty"`
}

// RawInnerMessage is used for parsing the inner message content.
type RawInnerMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Model   string          `json:"model,omitempty"`
}

// RawContentBlock is used for parsing content blocks before determining their type.
type RawContentBlock struct {
	Type      string         `json:"type"`
	Text      string         `json:"text,omitempty"`
	Thinking  string         `json:"thinking,omitempty"`
	Signature string         `json:"signature,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Input     map[string]any `json:"input,omitempty"`
	ToolUseID string         `json:"tool_use_id,omitempty"`
	Content   any            `json:"content,omitempty"`
	IsError   *bool          `json:"is_error,omitempty"`
}

// PermissionRuleValue represents a permission rule.
type PermissionRuleValue struct {
	ToolName    string  `json:"toolName"`
	RuleContent *string `json:"ruleContent,omitempty"`
}

// PermissionUpdate represents a permission update configuration.
type PermissionUpdate struct {
	Type        PermissionUpdateType         `json:"type"`
	Rules       []PermissionRuleValue        `json:"rules,omitempty"`
	Behavior    *PermissionBehavior          `json:"behavior,omitempty"`
	Mode        *PermissionMode              `json:"mode,omitempty"`
	Directories []string                     `json:"directories,omitempty"`
	Destination *PermissionUpdateDestination `json:"destination,omitempty"`
}

// ToMap converts PermissionUpdate to a map for JSON serialization.
func (p *PermissionUpdate) ToMap() map[string]any {
	result := map[string]any{
		"type": p.Type,
	}

	if p.Destination != nil {
		result["destination"] = *p.Destination
	}

	switch p.Type {
	case PermissionUpdateTypeAddRules, PermissionUpdateTypeReplaceRules, PermissionUpdateTypeRemoveRules:
		if p.Rules != nil {
			rules := make([]map[string]any, len(p.Rules))
			for i, rule := range p.Rules {
				r := map[string]any{"toolName": rule.ToolName}
				if rule.RuleContent != nil {
					r["ruleContent"] = *rule.RuleContent
				}
				rules[i] = r
			}
			result["rules"] = rules
		}
		if p.Behavior != nil {
			result["behavior"] = *p.Behavior
		}
	case PermissionUpdateTypeSetMode:
		if p.Mode != nil {
			result["mode"] = *p.Mode
		}
	case PermissionUpdateTypeAddDirectories, PermissionUpdateTypeRemoveDirectories:
		if p.Directories != nil {
			result["directories"] = p.Directories
		}
	}

	return result
}

// PermissionUpdateFromMap reconstructs a PermissionUpdate from the control
// protocol's wire format. It is the inverse of ToMap and is used to decode the
// permission suggestions the CLI attaches to a can_use_tool request, so callers
// can echo them back verbatim as PermissionResultAllow.UpdatedPermissions.
func PermissionUpdateFromMap(data map[string]any) PermissionUpdate {
	update := PermissionUpdate{}

	if typeStr, ok := data["type"].(string); ok {
		update.Type = PermissionUpdateType(typeStr)
	}

	if dest, ok := data["destination"].(string); ok {
		d := PermissionUpdateDestination(dest)
		update.Destination = &d
	}

	if behavior, ok := data["behavior"].(string); ok {
		b := PermissionBehavior(behavior)
		update.Behavior = &b
	}

	if mode, ok := data["mode"].(string); ok {
		m := PermissionMode(mode)
		update.Mode = &m
	}

	if rules, ok := data["rules"].([]any); ok {
		update.Rules = make([]PermissionRuleValue, 0, len(rules))
		for _, r := range rules {
			rMap, ok := r.(map[string]any)
			if !ok {
				continue
			}
			rule := PermissionRuleValue{}
			if name, ok := rMap["toolName"].(string); ok {
				rule.ToolName = name
			}
			if content, ok := rMap["ruleContent"].(string); ok {
				rule.RuleContent = &content
			}
			update.Rules = append(update.Rules, rule)
		}
	}

	if dirs, ok := data["directories"].([]any); ok {
		update.Directories = make([]string, 0, len(dirs))
		for _, d := range dirs {
			if s, ok := d.(string); ok {
				update.Directories = append(update.Directories, s)
			}
		}
	}

	return update
}

// PermissionUpdatesFromAny decodes a slice of raw permission updates, skipping
// entries that are not objects.
func PermissionUpdatesFromAny(raw []any) []PermissionUpdate {
	if len(raw) == 0 {
		return nil
	}
	updates := make([]PermissionUpdate, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			updates = append(updates, PermissionUpdateFromMap(m))
		}
	}
	return updates
}

// ToolPermissionContext provides context for tool permission callbacks.
type ToolPermissionContext struct {
	Signal      any                // Reserved for future abort signal support
	Suggestions []PermissionUpdate // Permission suggestions from CLI
}

// PermissionResult is the interface for permission callback results.
type PermissionResult interface {
	isPermissionResult()
}

// PermissionResultAllow allows tool execution.
type PermissionResultAllow struct {
	UpdatedInput       map[string]any     `json:"updated_input,omitempty"`
	UpdatedPermissions []PermissionUpdate `json:"updated_permissions,omitempty"`
}

func (p *PermissionResultAllow) isPermissionResult() {}

// PermissionResultDeny denies tool execution.
type PermissionResultDeny struct {
	Message   string `json:"message"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

func (p *PermissionResultDeny) isPermissionResult() {}

// PreviewFormat is the content format for the `preview` field on AskUserQuestion
// options. It controls what the model is instructed to emit and how the field is
// described in the tool schema.
type PreviewFormat string

const (
	// PreviewFormatMarkdown renders previews as Markdown/ASCII (CLI default).
	PreviewFormatMarkdown PreviewFormat = "markdown"
	// PreviewFormatHTML renders previews as self-contained HTML fragments,
	// for web-based SDK consumers.
	PreviewFormatHTML PreviewFormat = "html"
)

// AskUserQuestionConfig configures the built-in AskUserQuestion tool.
type AskUserQuestionConfig struct {
	// PreviewFormat selects the content format for question option previews.
	// Defaults to markdown when unset.
	PreviewFormat *PreviewFormat `json:"previewFormat,omitempty"`
}

// ToolConfig provides per-tool configuration for built-in tools.
//
// This is delivered to the CLI through the subprocess environment rather than
// as a command-line flag.
type ToolConfig struct {
	AskUserQuestion *AskUserQuestionConfig `json:"askUserQuestion,omitempty"`
}

// AgentDefinition defines a custom agent configuration.
type AgentDefinition struct {
	// Description tells the model when to use this agent.
	Description string `json:"description"`
	// Prompt is the agent's system prompt.
	Prompt string `json:"prompt"`
	// Tools restricts the agent's tools. Omit to inherit the parent's.
	Tools []string `json:"tools,omitempty"`
	// DisallowedTools removes tools for this agent. An MCP server-level spec
	// ("mcp__server", "mcp__server__*", "mcp__*") removes every tool from
	// that server, or all MCP tools.
	DisallowedTools []string `json:"disallowedTools,omitempty"`
	// Model is an alias ("opus", "sonnet", "haiku", "inherit") or a full
	// model ID. Omit or use "inherit" for the main model.
	Model *string `json:"model,omitempty"`
	// Skills are preloaded into the agent's context.
	Skills []string `json:"skills,omitempty"`
	// Memory scopes auto-loaded agent memory files.
	Memory *AgentMemoryScope `json:"memory,omitempty"`
	// MCPServers is a list of server names, or inline {name: config} maps.
	MCPServers []any `json:"mcpServers,omitempty"`
	// InitialPrompt is auto-submitted as the first user turn when this agent
	// is the main-thread agent. Slash commands are processed.
	InitialPrompt *string `json:"initialPrompt,omitempty"`
	// MaxTurns bounds the agent's agentic iterations.
	MaxTurns *int `json:"maxTurns,omitempty"`
	// Background runs the agent as a non-blocking task when invoked.
	Background *bool `json:"background,omitempty"`
	// Effort is a named level or an integer.
	Effort any `json:"effort,omitempty"`
	// PermissionMode controls how this agent's tool calls are handled.
	PermissionMode *PermissionMode `json:"permissionMode,omitempty"`
	// Observer names an agent type auto-spawned as a background observer
	// whenever this agent runs. It receives read-only activity digests and
	// never participates in the task.
	Observer *string `json:"observer,omitempty"`
	// ObserverMessage is appended to each activity digest sent to the
	// observer. Blank values are ignored.
	ObserverMessage *string `json:"observerMessage,omitempty"`
}

// AgentMemoryScope selects where an agent's memory files are auto-loaded from.
type AgentMemoryScope string

const (
	// AgentMemoryUser reads ~/.claude/agent-memory/<agentType>/.
	AgentMemoryUser AgentMemoryScope = "user"
	// AgentMemoryProject reads .claude/agent-memory/<agentType>/.
	AgentMemoryProject AgentMemoryScope = "project"
	// AgentMemoryLocal reads .claude/agent-memory-local/<agentType>/.
	AgentMemoryLocal AgentMemoryScope = "local"
)

// ToMap renders the definition for the initialize request, omitting unset
// optional fields so the CLI sees only what the caller configured.
func (a AgentDefinition) ToMap() map[string]any {
	m := map[string]any{
		"description": a.Description,
		"prompt":      a.Prompt,
	}
	if a.Tools != nil {
		m["tools"] = a.Tools
	}
	if a.DisallowedTools != nil {
		m["disallowedTools"] = a.DisallowedTools
	}
	if a.Model != nil {
		m["model"] = *a.Model
	}
	if a.Skills != nil {
		m["skills"] = a.Skills
	}
	if a.Memory != nil {
		m["memory"] = string(*a.Memory)
	}
	if a.MCPServers != nil {
		m["mcpServers"] = a.MCPServers
	}
	if a.InitialPrompt != nil {
		m["initialPrompt"] = *a.InitialPrompt
	}
	if a.MaxTurns != nil {
		m["maxTurns"] = *a.MaxTurns
	}
	if a.Background != nil {
		m["background"] = *a.Background
	}
	if a.Effort != nil {
		m["effort"] = a.Effort
	}
	if a.PermissionMode != nil {
		m["permissionMode"] = string(*a.PermissionMode)
	}
	if a.Observer != nil {
		m["observer"] = *a.Observer
	}
	if a.ObserverMessage != nil {
		m["observerMessage"] = *a.ObserverMessage
	}
	return m
}

// SystemPromptPreset defines a system prompt preset configuration.
type SystemPromptPreset struct {
	Type   string  `json:"type"`   // "preset"
	Preset string  `json:"preset"` // "claude_code"
	Append *string `json:"append,omitempty"`
	// ExcludeDynamicSections strips per-user dynamic sections (working
	// directory, auto-memory, git status) from the system prompt so it stays
	// static and cacheable across users. The stripped content is re-injected
	// into the first user message, so the model still has access to it.
	//
	// Use this when many users share the same preset prompt and you want the
	// prompt-caching prefix to hit cross-user. The tradeoff is that this
	// context appears in a user message instead of the system prompt, so it
	// steers the model marginally less authoritatively.
	ExcludeDynamicSections *bool `json:"exclude_dynamic_sections,omitempty"`
}

// ToolsPreset defines a tools preset configuration.
type ToolsPreset struct {
	Type   string `json:"type"`   // "preset"
	Preset string `json:"preset"` // "claude_code"
}

// ThinkingDisplay controls whether thinking text is returned summarized or omitted.
// Opus 4.7+ defaults to "omitted" (signature-only); pass "summarized" to receive text.
type ThinkingDisplay string

const (
	// ThinkingDisplaySummarized returns summarized thinking text.
	ThinkingDisplaySummarized ThinkingDisplay = "summarized"
	// ThinkingDisplayOmitted returns only thinking signatures, no text.
	ThinkingDisplayOmitted ThinkingDisplay = "omitted"
)

// ThinkingConfig is the interface for thinking configuration types.
type ThinkingConfig interface {
	isThinkingConfig()
	// ThinkingType returns the type discriminator for this config.
	ThinkingType() string
	// DisplayMode returns the thinking display mode, or nil if unset.
	DisplayMode() *ThinkingDisplay
}

// ThinkingConfigAdaptive enables adaptive thinking, letting Claude decide when
// and how much to think (Opus 4.6+).
type ThinkingConfigAdaptive struct {
	Type string `json:"type"` // "adaptive"
	// Display controls whether thinking text is summarized or omitted.
	Display *ThinkingDisplay `json:"display,omitempty"`
}

func (t *ThinkingConfigAdaptive) isThinkingConfig()             {}
func (t *ThinkingConfigAdaptive) ThinkingType() string          { return "adaptive" }
func (t *ThinkingConfigAdaptive) DisplayMode() *ThinkingDisplay { return t.Display }

// ThinkingConfigEnabled enables thinking with a fixed token budget (older models).
// When BudgetTokens is nil the CLI falls back to adaptive thinking.
type ThinkingConfigEnabled struct {
	Type string `json:"type"` // "enabled"
	// BudgetTokens is the fixed thinking token budget. Nil means adaptive.
	BudgetTokens *int `json:"budget_tokens,omitempty"`
	// Display controls whether thinking text is summarized or omitted.
	Display *ThinkingDisplay `json:"display,omitempty"`
}

func (t *ThinkingConfigEnabled) isThinkingConfig()             {}
func (t *ThinkingConfigEnabled) ThinkingType() string          { return "enabled" }
func (t *ThinkingConfigEnabled) DisplayMode() *ThinkingDisplay { return t.Display }

// ThinkingConfigDisabled disables extended thinking.
type ThinkingConfigDisabled struct {
	Type string `json:"type"` // "disabled"
}

func (t *ThinkingConfigDisabled) isThinkingConfig()             {}
func (t *ThinkingConfigDisabled) ThinkingType() string          { return "disabled" }
func (t *ThinkingConfigDisabled) DisplayMode() *ThinkingDisplay { return nil }

// NewThinkingAdaptive returns an adaptive thinking config.
func NewThinkingAdaptive() *ThinkingConfigAdaptive {
	return &ThinkingConfigAdaptive{Type: "adaptive"}
}

// NewThinkingEnabled returns a thinking config with a fixed token budget.
func NewThinkingEnabled(budgetTokens int) *ThinkingConfigEnabled {
	return &ThinkingConfigEnabled{Type: "enabled", BudgetTokens: &budgetTokens}
}

// NewThinkingDisabled returns a disabled thinking config.
func NewThinkingDisabled() *ThinkingConfigDisabled {
	return &ThinkingConfigDisabled{Type: "disabled"}
}

// RateLimitInfo contains rate limit information.
type RateLimitInfo struct {
	Status RateLimitStatus `json:"status"`
	// ResetsAt is the Unix timestamp when the window resets.
	ResetsAt              *int64           `json:"resets_at,omitempty"`
	RateLimitType         *RateLimitType   `json:"rate_limit_type,omitempty"`
	Utilization           *float64         `json:"utilization,omitempty"`
	OverageStatus         *RateLimitStatus `json:"overage_status,omitempty"`
	OverageResetsAt       *int64           `json:"overage_resets_at,omitempty"`
	OverageDisabledReason *string          `json:"overage_disabled_reason,omitempty"`
	// IsUsingOverage reports whether overage credits are currently in use.
	IsUsingOverage *bool `json:"is_using_overage,omitempty"`
	// SurpassedThreshold is the utilization threshold just crossed.
	SurpassedThreshold *float64 `json:"surpassed_threshold,omitempty"`
	// ErrorCode is set when the limit produced an actionable error, e.g.
	// credits_required.
	ErrorCode *string `json:"error_code,omitempty"`
	// CanUserPurchaseCredits reports whether the user can buy more credits.
	CanUserPurchaseCredits *bool `json:"can_user_purchase_credits,omitempty"`
	// Raw is the full payload, including fields not modeled here.
	Raw map[string]any `json:"raw,omitempty"`
}

// RateLimitEvent represents a rate limit event message.
type RateLimitEvent struct {
	RateLimitInfo
	UUID      string `json:"uuid,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

func (m *RateLimitEvent) isMessage() {}

// TaskUsage contains usage statistics reported in task_progress and
// task_notification messages.
type TaskUsage struct {
	// TotalTokens is the cumulative token count for the task.
	TotalTokens int `json:"total_tokens"`
	// ToolUses is the number of tool invocations made by the task.
	ToolUses int `json:"tool_uses"`
	// DurationMS is the task's elapsed wall time in milliseconds.
	DurationMS int `json:"duration_ms"`
}

// TaskStartedMessage represents a task started system message.
type TaskStartedMessage struct {
	SystemMessage
	TaskID      string  `json:"task_id"`
	Description string  `json:"description"`
	UUID        string  `json:"uuid,omitempty"`
	SessionID   string  `json:"session_id,omitempty"`
	ToolUseID   string  `json:"tool_use_id,omitempty"`
	TaskType    *string `json:"task_type,omitempty"`
	// SubagentType names the agent this task runs, when it is a subagent.
	SubagentType string `json:"subagent_type,omitempty"`
	// IsBackgrounded reports whether the task runs without blocking the turn.
	// Set for background subagents and background Bash tasks.
	IsBackgrounded bool `json:"is_backgrounded,omitempty"`
	// SpawnDepth is how deep the task sits in the subagent spawn tree, with 0
	// meaning it was spawned by the main thread.
	SpawnDepth int `json:"spawn_depth,omitempty"`
	// WorkflowName names the workflow this task belongs to, if any.
	WorkflowName string `json:"workflow_name,omitempty"`
	// Prompt is the task's prompt, when the CLI reports it.
	Prompt string `json:"prompt,omitempty"`
	// SkipTranscript reports that the task's messages are not mirrored into
	// the parent transcript.
	SkipTranscript bool `json:"skip_transcript,omitempty"`
	// Ambient marks housekeeping work a host should leave out of its activity
	// indicators.
	Ambient bool `json:"ambient,omitempty"`
}

// TaskProgressMessage represents a task progress system message.
type TaskProgressMessage struct {
	SystemMessage
	TaskID       string    `json:"task_id"`
	Description  string    `json:"description,omitempty"`
	Usage        TaskUsage `json:"usage"`
	UUID         string    `json:"uuid,omitempty"`
	SessionID    string    `json:"session_id,omitempty"`
	ToolUseID    string    `json:"tool_use_id,omitempty"`
	LastToolName *string   `json:"last_tool_name,omitempty"`
}

// TaskNotificationMessage represents a task notification system message.
type TaskNotificationMessage struct {
	SystemMessage
	TaskID     string                 `json:"task_id"`
	Status     TaskNotificationStatus `json:"status"`
	OutputFile string                 `json:"output_file,omitempty"`
	Summary    string                 `json:"summary,omitempty"`
	UUID       string                 `json:"uuid,omitempty"`
	SessionID  string                 `json:"session_id,omitempty"`
	ToolUseID  string                 `json:"tool_use_id,omitempty"`
	Usage      *TaskUsage             `json:"usage,omitempty"`
	// ResourceLinks lists files an auto-backgrounded MCP tool call returned
	// by reference. Join to the originating call through ToolUseID.
	ResourceLinks []MCPResourceLink `json:"resource_links,omitempty"`
	// SkipTranscript reports that the task's messages are not mirrored into
	// the parent transcript.
	SkipTranscript bool `json:"skip_transcript,omitempty"`
	// Ambient marks housekeeping work a host should leave out of its activity
	// indicators.
	Ambient bool `json:"ambient,omitempty"`
}

// ChannelMessage represents a message pushed by a channel server into the session.
type ChannelMessage struct {
	ServerName string         `json:"server_name"`
	Content    string         `json:"content"`
	Data       map[string]any `json:"data,omitempty"`
	UUID       string         `json:"uuid,omitempty"`
	SessionID  string         `json:"session_id,omitempty"`
}

func (m *ChannelMessage) isMessage() {}

// SDKSessionInfo contains information about a session.
type SDKSessionInfo struct {
	SessionID string `json:"session_id"`
	// Summary is the display title: a custom title if set, otherwise the
	// first prompt.
	Summary string `json:"summary"`
	// LastModified is the last-modified time in Unix epoch milliseconds.
	LastModified int64 `json:"last_modified"`
	// FileSize is the transcript size in bytes. Only populated for local
	// JSONL storage; zero for remote backends.
	FileSize int64 `json:"file_size,omitempty"`
	// CustomTitle is a user-set or AI-generated title.
	CustomTitle *string    `json:"custom_title,omitempty"`
	CWD         string     `json:"cwd,omitempty"`
	FirstPrompt string     `json:"first_prompt,omitempty"`
	LastPrompt  string     `json:"last_prompt,omitempty"`
	GitBranch   *string    `json:"git_branch,omitempty"`
	Tag         *string    `json:"tag,omitempty"`
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

// SessionMessage is a user or assistant message from a session transcript.
type SessionMessage struct {
	// Type is "user" or "assistant".
	Type string `json:"type"`
	// UUID uniquely identifies the message.
	UUID string `json:"uuid,omitempty"`
	// SessionID is the session the message belongs to.
	SessionID string `json:"session_id,omitempty"`
	// Message is the raw API message (role, content, and so on).
	Message any `json:"message,omitempty"`
	// ParentToolUseID is set for tool-use sidechain messages. On a subagent
	// transcript it is recovered from the subagent's metadata, linking each
	// message to the Agent tool_use block in the parent session.
	ParentToolUseID *string `json:"parent_tool_use_id,omitempty"`
	// ParentAgentID is the id of the agent that spawned the subagent this
	// message belongs to. Empty on main-session transcripts.
	ParentAgentID string `json:"parent_agent_id,omitempty"`
	// Data is the full raw transcript line, including fields not modeled here.
	Data map[string]any `json:"-"`
}

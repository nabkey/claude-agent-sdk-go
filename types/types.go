package types

import "encoding/json"

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
	Content         any     `json:"content"` // Can be string or []ContentBlock
	ParentToolUseID *string `json:"parent_tool_use_id,omitempty"`
}

func (m *UserMessage) isMessage() {}

// AssistantMessage represents a response from Claude.
type AssistantMessage struct {
	Content         []ContentBlock         `json:"-"` // Custom unmarshal
	Model           string                 `json:"model"`
	ParentToolUseID *string                `json:"parent_tool_use_id,omitempty"`
	Error           *AssistantMessageError `json:"error,omitempty"`
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
}

func (m *ResultMessage) isMessage() {}

// StreamEvent represents a partial message update during streaming.
type StreamEvent struct {
	UUID            string         `json:"uuid"`
	SessionID       string         `json:"session_id"`
	Event           map[string]any `json:"event"`
	ParentToolUseID *string        `json:"parent_tool_use_id,omitempty"`
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

// AgentDefinition defines a custom agent configuration.
type AgentDefinition struct {
	Description string   `json:"description"`
	Prompt      string   `json:"prompt"`
	Tools       []string `json:"tools,omitempty"`
	Model       *string  `json:"model,omitempty"` // "sonnet", "opus", "haiku", "inherit"
}

// SystemPromptPreset defines a system prompt preset configuration.
type SystemPromptPreset struct {
	Type   string  `json:"type"`   // "preset"
	Preset string  `json:"preset"` // "claude_code"
	Append *string `json:"append,omitempty"`
}

// ToolsPreset defines a tools preset configuration.
type ToolsPreset struct {
	Type   string `json:"type"`   // "preset"
	Preset string `json:"preset"` // "claude_code"
}

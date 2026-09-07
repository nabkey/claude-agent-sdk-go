package types

// SkillsAll enables every discovered skill. Assign it to
// AgentOptions.Skills; any other value must be a []string of skill names.
const SkillsAll = "all"

// SystemPromptDynamicBoundary marks the split between the static
// (globally-cacheable) prefix and the dynamic (session-specific) suffix of a
// block-form system prompt. Include it as a standalone element of the
// []string passed to AgentOptions.SystemPrompt; blocks before it are eligible
// for cross-session prompt caching, blocks after it are not.
const SystemPromptDynamicBoundary = "__SYSTEM_PROMPT_DYNAMIC_BOUNDARY__"

// SystemPromptFile loads the system prompt from a file on disk.
type SystemPromptFile struct {
	Type string `json:"type"` // "file"
	Path string `json:"path"`
}

// TaskBudget is an API-side task budget in tokens.
//
// When set, the model is told its remaining budget so it can pace tool use and
// wrap up before the limit.
type TaskBudget struct {
	Total int `json:"total"`
}

// PermissionPromptsMode controls what the CLI does with a permission prompt
// that no host is available to answer.
type PermissionPromptsMode string

const (
	// PermissionPromptsHost routes prompts to the host, the CLI default.
	PermissionPromptsHost PermissionPromptsMode = "host"
	// PermissionPromptsNone auto-denies prompts, for sessions with nobody to
	// answer them. Auto mode's classifier still runs, so calls it approves
	// are unaffected; only what would have been asked is denied.
	PermissionPromptsNone PermissionPromptsMode = "none"
)

// PluginDelivery selects how plugin configuration reaches the CLI.
type PluginDelivery string

const (
	// PluginDeliveryArgv passes each plugin as a --plugin-dir flag. This is
	// the default, and the only form older CLI builds understand.
	PluginDeliveryArgv PluginDelivery = "argv"
	// PluginDeliveryInitialize sends plugins on the initialize request
	// instead, so the launch command line does not grow with the plugin
	// count. Use it when many plugins would overflow the platform's command
	// line limit, which is what fails first on Windows.
	PluginDeliveryInitialize PluginDelivery = "initialize"
)

// SessionStoreFlushMode controls when transcript-mirror entries are flushed to
// a SessionStore.
type SessionStoreFlushMode string

const (
	// SessionStoreFlushBatched buffers entries and flushes once per turn, or
	// when the pending buffer exceeds its size thresholds. This keeps adapter
	// latency off the streaming hot path, and is the default.
	SessionStoreFlushBatched SessionStoreFlushMode = "batched"

	// SessionStoreFlushEager flushes after every transcript frame, so the
	// store sees entries in near real time. Appends stay serialized, so a slow
	// adapter will not stall the read loop but will see frames coalesced while
	// it is busy.
	SessionStoreFlushEager SessionStoreFlushMode = "eager"
)

// ElicitationMode is how an MCP server is asking for input.
type ElicitationMode string

const (
	// ElicitationModeForm requests structured field values.
	ElicitationModeForm ElicitationMode = "form"
	// ElicitationModeURL requests the user visit a URL, typically to
	// complete an OAuth flow.
	ElicitationModeURL ElicitationMode = "url"
)

// ElicitationRequest is an MCP server's request for user input.
type ElicitationRequest struct {
	// ServerName is the MCP server making the request.
	ServerName string `json:"server_name,omitempty"`
	// Mode distinguishes form input from URL-based flows.
	Mode ElicitationMode `json:"mode,omitempty"`
	// Message is the human-readable prompt.
	Message string `json:"message,omitempty"`
	// RequestedSchema is the JSON Schema of the requested fields, for form mode.
	RequestedSchema map[string]any `json:"requestedSchema,omitempty"`
	// URL is the address to visit, for URL mode.
	URL string `json:"url,omitempty"`
	// Raw is the full request payload, including fields not modeled here.
	Raw map[string]any `json:"-"`
}

// ElicitationAction is the caller's response to an elicitation request.
type ElicitationAction string

const (
	// ElicitationAccept supplies the requested content.
	ElicitationAccept ElicitationAction = "accept"
	// ElicitationDecline refuses the request.
	ElicitationDecline ElicitationAction = "decline"
	// ElicitationCancel aborts the flow entirely.
	ElicitationCancel ElicitationAction = "cancel"
)

// ElicitationResult answers an ElicitationRequest.
type ElicitationResult struct {
	Action ElicitationAction `json:"action"`
	// Content carries the field values when Action is accept.
	Content map[string]any `json:"content,omitempty"`
}

// UserDialogRequest asks the host to render a blocking dialog.
type UserDialogRequest struct {
	// DialogKind identifies the dialog to render. This is an open string
	// union: new kinds may appear without a protocol bump, so a host must
	// answer an unrecognized kind with a cancelled result.
	DialogKind string `json:"dialog_kind"`
	// Payload is dialog-specific data, transported opaquely.
	Payload map[string]any `json:"payload,omitempty"`
	// ToolUseID identifies the tool call that triggered the dialog, if any.
	ToolUseID *string `json:"tool_use_id,omitempty"`
}

// UserDialogBehavior is how the host resolved a dialog.
type UserDialogBehavior string

const (
	// UserDialogCompleted means the host rendered the dialog and the user chose.
	UserDialogCompleted UserDialogBehavior = "completed"
	// UserDialogCancelled means the host declined to render it. The CLI then
	// applies the dialog's default behavior. This is the required answer for
	// an unrecognized dialog kind.
	UserDialogCancelled UserDialogBehavior = "cancelled"
)

// UserDialogResult answers a UserDialogRequest.
type UserDialogResult struct {
	Behavior UserDialogBehavior `json:"behavior"`
	// Result is the dialog-specific outcome, when Behavior is completed.
	Result map[string]any `json:"result,omitempty"`
}

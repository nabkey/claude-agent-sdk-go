// Package types provides type definitions for the Claude Agent SDK.
package types

// PermissionMode defines how the SDK handles tool permissions.
type PermissionMode string

const (
	// PermissionModeDefault uses the CLI's default permission prompts.
	PermissionModeDefault PermissionMode = "default"
	// PermissionModeAcceptEdits auto-accepts file edits.
	PermissionModeAcceptEdits PermissionMode = "acceptEdits"
	// PermissionModePlan enables planning mode.
	PermissionModePlan PermissionMode = "plan"
	// PermissionModeBypassPermissions allows all tools without prompting.
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
)

// HookEvent defines the types of hook events that can be intercepted.
type HookEvent string

const (
	// HookEventPreToolUse fires before a tool is executed.
	HookEventPreToolUse HookEvent = "PreToolUse"
	// HookEventPostToolUse fires after a tool is executed.
	HookEventPostToolUse HookEvent = "PostToolUse"
	// HookEventUserPromptSubmit fires when a user prompt is submitted.
	HookEventUserPromptSubmit HookEvent = "UserPromptSubmit"
	// HookEventStop fires when the session stops.
	HookEventStop HookEvent = "Stop"
	// HookEventSubagentStop fires when a subagent stops.
	HookEventSubagentStop HookEvent = "SubagentStop"
	// HookEventPreCompact fires before context compaction.
	HookEventPreCompact HookEvent = "PreCompact"
	// HookEventPostToolUseFailure fires after a tool use fails.
	HookEventPostToolUseFailure HookEvent = "PostToolUseFailure"
	// HookEventSubagentStart fires when a subagent starts.
	HookEventSubagentStart HookEvent = "SubagentStart"
	// HookEventNotification fires on notifications.
	HookEventNotification HookEvent = "Notification"
	// HookEventPermissionRequest fires on permission requests.
	HookEventPermissionRequest HookEvent = "PermissionRequest"
)

// PermissionBehavior defines how a permission decision is handled.
type PermissionBehavior string

const (
	// PermissionBehaviorAllow allows the tool to execute.
	PermissionBehaviorAllow PermissionBehavior = "allow"
	// PermissionBehaviorDeny denies tool execution.
	PermissionBehaviorDeny PermissionBehavior = "deny"
	// PermissionBehaviorAsk prompts the user for permission.
	PermissionBehaviorAsk PermissionBehavior = "ask"
)

// PermissionUpdateDestination defines where permission updates are stored.
type PermissionUpdateDestination string

const (
	PermissionUpdateDestinationUserSettings    PermissionUpdateDestination = "userSettings"
	PermissionUpdateDestinationProjectSettings PermissionUpdateDestination = "projectSettings"
	PermissionUpdateDestinationLocalSettings   PermissionUpdateDestination = "localSettings"
	PermissionUpdateDestinationSession         PermissionUpdateDestination = "session"
)

// PermissionUpdateType defines the type of permission update.
type PermissionUpdateType string

const (
	PermissionUpdateTypeAddRules          PermissionUpdateType = "addRules"
	PermissionUpdateTypeReplaceRules      PermissionUpdateType = "replaceRules"
	PermissionUpdateTypeRemoveRules       PermissionUpdateType = "removeRules"
	PermissionUpdateTypeSetMode           PermissionUpdateType = "setMode"
	PermissionUpdateTypeAddDirectories    PermissionUpdateType = "addDirectories"
	PermissionUpdateTypeRemoveDirectories PermissionUpdateType = "removeDirectories"
)

// SettingSource defines the source of settings to load.
type SettingSource string

const (
	SettingSourceUser    SettingSource = "user"
	SettingSourceProject SettingSource = "project"
	SettingSourceLocal   SettingSource = "local"
)

// AssistantMessageError defines error types that can occur in assistant messages.
type AssistantMessageError string

const (
	AssistantMessageErrorAuthenticationFailed AssistantMessageError = "authentication_failed"
	AssistantMessageErrorBillingError         AssistantMessageError = "billing_error"
	AssistantMessageErrorRateLimit            AssistantMessageError = "rate_limit"
	AssistantMessageErrorInvalidRequest       AssistantMessageError = "invalid_request"
	AssistantMessageErrorServerError          AssistantMessageError = "server_error"
	AssistantMessageErrorUnknown              AssistantMessageError = "unknown"
)

// EffortLevel defines the effort level for the model.
type EffortLevel string

const (
	EffortLevelLow    EffortLevel = "low"
	EffortLevelMedium EffortLevel = "medium"
	EffortLevelHigh   EffortLevel = "high"
	EffortLevelMax    EffortLevel = "max"
)

// SdkBeta defines beta feature identifiers.
type SdkBeta string

const (
	SdkBetaContext1M SdkBeta = "context-1m-2025-08-07"
)

// RateLimitStatus defines the status of a rate limit check.
type RateLimitStatus string

const (
	RateLimitStatusAllowed        RateLimitStatus = "allowed"
	RateLimitStatusAllowedWarning RateLimitStatus = "allowed_warning"
	RateLimitStatusRejected       RateLimitStatus = "rejected"
)

// RateLimitType defines the type of rate limit.
type RateLimitType string

const (
	RateLimitTypeFiveHour       RateLimitType = "five_hour"
	RateLimitTypeSevenDay       RateLimitType = "seven_day"
	RateLimitTypeSevenDayOpus   RateLimitType = "seven_day_opus"
	RateLimitTypeSevenDaySonnet RateLimitType = "seven_day_sonnet"
	RateLimitTypeOverage        RateLimitType = "overage"
)

// TaskNotificationStatus defines the status of a task notification.
type TaskNotificationStatus string

const (
	TaskNotificationStatusCompleted TaskNotificationStatus = "completed"
	TaskNotificationStatusFailed    TaskNotificationStatus = "failed"
	TaskNotificationStatusStopped   TaskNotificationStatus = "stopped"
)

// McpServerConnectionStatus defines the connection status of an MCP server.
type McpServerConnectionStatus string

const (
	McpServerConnectionStatusConnected McpServerConnectionStatus = "connected"
	McpServerConnectionStatusFailed    McpServerConnectionStatus = "failed"
	McpServerConnectionStatusNeedsAuth McpServerConnectionStatus = "needs-auth"
	McpServerConnectionStatusPending   McpServerConnectionStatus = "pending"
	McpServerConnectionStatusDisabled  McpServerConnectionStatus = "disabled"
)

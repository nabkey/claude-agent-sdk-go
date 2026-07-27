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
	// Requires AgentOptions.AllowDangerouslySkipPermissions.
	PermissionModeBypassPermissions PermissionMode = "bypassPermissions"
	// PermissionModeDontAsk never prompts, denying anything not already
	// approved by an allow rule.
	PermissionModeDontAsk PermissionMode = "dontAsk"
	// PermissionModeAuto routes each tool call through a model classifier
	// that approves or denies it.
	PermissionModeAuto PermissionMode = "auto"
)

// HookEvent defines the types of hook events that can be intercepted.
type HookEvent string

const (
	// HookEventPreToolUse fires before a tool is executed.
	HookEventPreToolUse HookEvent = "PreToolUse"
	// HookEventPostToolUse fires after a tool is executed.
	HookEventPostToolUse HookEvent = "PostToolUse"
	// HookEventPostToolUseFailure fires after a tool use fails.
	HookEventPostToolUseFailure HookEvent = "PostToolUseFailure"
	// HookEventPostToolBatch fires after a batch of tool calls completes.
	HookEventPostToolBatch HookEvent = "PostToolBatch"
	// HookEventNotification fires on notifications.
	HookEventNotification HookEvent = "Notification"
	// HookEventUserPromptSubmit fires when a user prompt is submitted.
	HookEventUserPromptSubmit HookEvent = "UserPromptSubmit"
	// HookEventUserPromptExpansion fires while a user prompt is expanded.
	HookEventUserPromptExpansion HookEvent = "UserPromptExpansion"
	// HookEventSessionStart fires when a session starts.
	HookEventSessionStart HookEvent = "SessionStart"
	// HookEventSessionEnd fires when a session ends.
	HookEventSessionEnd HookEvent = "SessionEnd"
	// HookEventStop fires when the session stops.
	HookEventStop HookEvent = "Stop"
	// HookEventStopFailure fires when stopping fails.
	HookEventStopFailure HookEvent = "StopFailure"
	// HookEventSubagentStart fires when a subagent starts.
	HookEventSubagentStart HookEvent = "SubagentStart"
	// HookEventSubagentStop fires when a subagent stops.
	HookEventSubagentStop HookEvent = "SubagentStop"
	// HookEventPreCompact fires before context compaction.
	HookEventPreCompact HookEvent = "PreCompact"
	// HookEventPostCompact fires after context compaction.
	HookEventPostCompact HookEvent = "PostCompact"
	// HookEventPermissionRequest fires on permission requests.
	HookEventPermissionRequest HookEvent = "PermissionRequest"
	// HookEventPermissionDenied fires when a permission request is denied.
	HookEventPermissionDenied HookEvent = "PermissionDenied"
	// HookEventSetup fires during session setup.
	HookEventSetup HookEvent = "Setup"
	// HookEventTeammateIdle fires when a teammate agent goes idle.
	HookEventTeammateIdle HookEvent = "TeammateIdle"
	// HookEventTaskCreated fires when a task is created.
	HookEventTaskCreated HookEvent = "TaskCreated"
	// HookEventTaskCompleted fires when a task completes.
	HookEventTaskCompleted HookEvent = "TaskCompleted"
	// HookEventElicitation fires when an MCP server requests user input.
	HookEventElicitation HookEvent = "Elicitation"
	// HookEventElicitationResult fires when an elicitation is answered.
	HookEventElicitationResult HookEvent = "ElicitationResult"
	// HookEventConfigChange fires when configuration changes.
	HookEventConfigChange HookEvent = "ConfigChange"
	// HookEventWorktreeCreate fires when a git worktree is created.
	HookEventWorktreeCreate HookEvent = "WorktreeCreate"
	// HookEventWorktreeRemove fires when a git worktree is removed.
	HookEventWorktreeRemove HookEvent = "WorktreeRemove"
	// HookEventInstructionsLoaded fires when instruction files are loaded.
	HookEventInstructionsLoaded HookEvent = "InstructionsLoaded"
	// HookEventCwdChanged fires when the working directory changes.
	HookEventCwdChanged HookEvent = "CwdChanged"
	// HookEventFileChanged fires when a tracked file changes.
	HookEventFileChanged HookEvent = "FileChanged"
	// HookEventDirectoryAdded fires when a directory is added to the session.
	HookEventDirectoryAdded HookEvent = "DirectoryAdded"
	// HookEventMessageDisplay fires when a message is displayed.
	HookEventMessageDisplay HookEvent = "MessageDisplay"
)

// AllHookEvents lists every hook event the CLI can dispatch.
var AllHookEvents = []HookEvent{
	HookEventPreToolUse, HookEventPostToolUse, HookEventPostToolUseFailure,
	HookEventPostToolBatch, HookEventNotification, HookEventUserPromptSubmit,
	HookEventUserPromptExpansion, HookEventSessionStart, HookEventSessionEnd,
	HookEventStop, HookEventStopFailure, HookEventSubagentStart,
	HookEventSubagentStop, HookEventPreCompact, HookEventPostCompact,
	HookEventPermissionRequest, HookEventPermissionDenied, HookEventSetup,
	HookEventTeammateIdle, HookEventTaskCreated, HookEventTaskCompleted,
	HookEventElicitation, HookEventElicitationResult, HookEventConfigChange,
	HookEventWorktreeCreate, HookEventWorktreeRemove, HookEventInstructionsLoaded,
	HookEventCwdChanged, HookEventFileChanged, HookEventDirectoryAdded,
	HookEventMessageDisplay,
}

// TaskUpdatedStatus is a status reported inside a task_updated patch.
//
// pending, running and paused are non-terminal; completed, failed and killed
// are terminal. Note task_updated reports the raw "killed"; the CLI maps that
// to "stopped" only when it emits a task_notification.
type TaskUpdatedStatus string

const (
	TaskUpdatedStatusPending   TaskUpdatedStatus = "pending"
	TaskUpdatedStatusRunning   TaskUpdatedStatus = "running"
	TaskUpdatedStatusPaused    TaskUpdatedStatus = "paused"
	TaskUpdatedStatusCompleted TaskUpdatedStatus = "completed"
	TaskUpdatedStatusFailed    TaskUpdatedStatus = "failed"
	TaskUpdatedStatusKilled    TaskUpdatedStatus = "killed"
)

// TerminalTaskStatuses are the statuses meaning a task has finished and should
// be cleared from any active-task tracking.
//
// This set spans both lifecycle vocabularies: task_notification reports
// "stopped" while task_updated reports the raw "killed". Treat the status of a
// TaskNotificationMessage and a TaskUpdatedMessage the same way.
var TerminalTaskStatuses = map[string]bool{
	"completed": true,
	"failed":    true,
	"stopped":   true,
	"killed":    true,
}

// IsTerminalTaskStatus reports whether a task lifecycle status is terminal.
func IsTerminalTaskStatus(status string) bool {
	return TerminalTaskStatuses[status]
}

// TerminalReason explains why the query loop terminated.
type TerminalReason string

const (
	TerminalReasonCompleted         TerminalReason = "completed"
	TerminalReasonMaxTurns          TerminalReason = "max_turns"
	TerminalReasonAbortedStreaming  TerminalReason = "aborted_streaming"
	TerminalReasonAbortedTools      TerminalReason = "aborted_tools"
	TerminalReasonBudgetExhausted   TerminalReason = "budget_exhausted"
	TerminalReasonToolDeferred      TerminalReason = "tool_deferred"
	TerminalReasonHookStopped       TerminalReason = "hook_stopped"
	TerminalReasonStopHookPrevented TerminalReason = "stop_hook_prevented"
	TerminalReasonPromptTooLong     TerminalReason = "prompt_too_long"
	TerminalReasonAPIError          TerminalReason = "api_error"
	TerminalReasonModelError        TerminalReason = "model_error"
	TerminalReasonBlockingLimit     TerminalReason = "blocking_limit"
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
	PermissionUpdateDestinationCLIArg          PermissionUpdateDestination = "cliArg"
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
	AssistantMessageErrorOAuthOrgNotAllowed   AssistantMessageError = "oauth_org_not_allowed"
	AssistantMessageErrorOverloaded           AssistantMessageError = "overloaded"
	AssistantMessageErrorModelNotFound        AssistantMessageError = "model_not_found"
	AssistantMessageErrorMaxOutputTokens      AssistantMessageError = "max_output_tokens"
	AssistantMessageErrorUnknown              AssistantMessageError = "unknown"
)

// EffortLevel defines the effort level for the model.
type EffortLevel string

const (
	EffortLevelLow    EffortLevel = "low"
	EffortLevelMedium EffortLevel = "medium"
	EffortLevelHigh   EffortLevel = "high"
	// EffortLevelXHigh reasons deeper than high, on models that support it.
	EffortLevelXHigh EffortLevel = "xhigh"
	EffortLevelMax   EffortLevel = "max"
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

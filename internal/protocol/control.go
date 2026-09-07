package protocol

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// GetContextUsage returns a breakdown of current context window usage.
func (q *Query) GetContextUsage(ctx context.Context, detail types.ContextUsageDetail) (*types.ContextUsage, error) {
	request := map[string]any{"subtype": "get_context_usage"}
	if detail != "" {
		request["detail"] = string(detail)
	}
	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	return types.ContextUsageFromMap(response), nil
}

// SetMaxThinkingTokens changes the thinking budget mid-session.
//
// A nil budget clears any override, returning to the session default. A nil
// display keeps the session's current display mode.
//
// Deprecated: prefer AgentOptions.Thinking at session start.
func (q *Query) SetMaxThinkingTokens(ctx context.Context, budget *int, display *types.ThinkingDisplay) error {
	request := map[string]any{"subtype": "set_max_thinking_tokens"}
	if budget != nil {
		request["max_thinking_tokens"] = *budget
	}
	if display != nil {
		request["thinking_display"] = string(*display)
	}
	_, err := q.sendControlRequest(ctx, request)
	return err
}

// ApplyFlagSettings merges settings into the flag settings layer, which sits
// above user/project/local settings and below managed policy settings.
//
// Successive calls shallow-merge top-level keys, so a second call replaces a
// whole top-level object from a prior call rather than deep-merging it. A nil
// value clears a key, falling back to lower-precedence sources.
func (q *Query) ApplyFlagSettings(ctx context.Context, settings map[string]any) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype":  "apply_flag_settings",
		"settings": settings,
	})
	return err
}

// SetMCPServers replaces the set of dynamically-added MCP servers.
//
// This affects only servers added dynamically or through the SDK. Servers from
// settings files are untouched, and plugin-owned servers keep running unless
// named explicitly, so an empty map does not guarantee zero MCP surface.
func (q *Query) SetMCPServers(ctx context.Context, servers map[string]any) (*types.MCPSetServersResult, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{
		"subtype": "mcp_set_servers",
		"servers": servers,
	})
	if err != nil {
		return nil, err
	}
	return types.MCPSetServersResultFromMap(response), nil
}

// SetMCPPermissionModeOverride pins a per-server permission mode override.
//
// Tighten-only: the override applies only where the session mode would already
// auto-allow, so it can never widen privilege. Pass nil to clear it.
func (q *Query) SetMCPPermissionModeOverride(ctx context.Context, serverName string, mode *types.PermissionMode) (string, error) {
	request := map[string]any{
		"subtype":    "set_mcp_permission_mode_override",
		"serverName": serverName,
	}
	if mode != nil {
		request["mode"] = string(*mode)
	} else {
		request["mode"] = nil
	}

	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return "", err
	}
	return getString(response, "warning"), nil
}

// ReloadPlugins reloads plugins from disk, returning the refreshed session
// components.
func (q *Query) ReloadPlugins(ctx context.Context) (*types.ReloadPluginsResult, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{"subtype": "reload_plugins"})
	if err != nil {
		return nil, err
	}
	return types.ReloadPluginsResultFromMap(response), nil
}

// ReloadSkills reloads skills from disk, returning the refreshed skill list.
func (q *Query) ReloadSkills(ctx context.Context) ([]types.SlashCommand, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{"subtype": "reload_skills"})
	if err != nil {
		return nil, err
	}
	return types.SlashCommandsFromAny(response["commands"]), nil
}

// ListModels returns the models this session may select.
func (q *Query) ListModels(ctx context.Context) ([]types.ModelInfo, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{"subtype": "list_models"})
	if err != nil {
		return nil, err
	}
	return types.ModelInfosFromAny(response["models"]), nil
}

// ReadFile reads a file from the session's filesystem.
//
// The path is resolved against the session's working directory and gated by
// the same read-permission rules as the Read tool.
func (q *Query) ReadFile(ctx context.Context, path string, maxBytes int, encoding string) (*types.ReadFileResult, error) {
	request := map[string]any{
		"subtype": "read_file",
		"path":    path,
	}
	if maxBytes > 0 {
		request["maxBytes"] = maxBytes
	}
	if encoding != "" {
		request["encoding"] = encoding
	}

	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	return types.ReadFileResultFromMap(response), nil
}

// SeedReadState primes the CLI's read-file cache with a path and mtime.
//
// Use this when the client observed a Read that has since been dropped from
// context, so a later Edit does not fail with "file not read yet". If the file
// changed since the given mtime the seed is skipped and Edit correctly
// requires a fresh Read.
func (q *Query) SeedReadState(ctx context.Context, path string, mtime int64) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype": "seed_read_state",
		"path":    path,
		"mtime":   mtime,
	})
	return err
}

// BackgroundTasks moves in-flight foreground work to the background.
//
// With a tool use ID it targets that one task; without, it backgrounds every
// foreground task. Each blocking tool call returns immediately and the turn
// continues; the task keeps running and emits a notification when it settles.
//
// Returns false only when a tool use ID was given and matched nothing.
func (q *Query) BackgroundTasks(ctx context.Context, toolUseID string) (bool, error) {
	request := map[string]any{"subtype": "background_tasks"}
	if toolUseID != "" {
		request["tool_use_id"] = toolUseID
	}

	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return false, err
	}
	if backgrounded, ok := response["backgrounded"].(bool); ok {
		return backgrounded, nil
	}
	// Older CLIs answer with an empty success payload.
	return true, nil
}

// GetSessionUsage returns session cost and token totals plus plan rate-limit
// utilization when available.
//
// Rate limits are absent for API key, Bedrock, Vertex and other sessions where
// plan limits do not apply.
func (q *Query) GetSessionUsage(ctx context.Context) (*types.SessionUsage, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{"subtype": "get_usage"})
	if err != nil {
		return nil, err
	}
	return types.SessionUsageFromMap(response), nil
}

// RewindFilesWithOptions restores tracked files to their state at a user
// message, optionally previewing the change instead of applying it.
func (q *Query) RewindFilesWithOptions(ctx context.Context, userMessageID string, dryRun bool) (*types.RewindFilesResult, error) {
	request := map[string]any{
		"subtype":         "rewind_files",
		"user_message_id": userMessageID,
	}
	if dryRun {
		request["dry_run"] = true
	}

	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	return types.RewindFilesResultFromMap(response), nil
}

// InterruptWithOptions stops the current turn, optionally also cancelling
// queued messages.
//
// On CLIs advertising the interrupt receipt capability the result lists which
// queued messages survive; older CLIs return an empty receipt.
func (q *Query) InterruptWithOptions(ctx context.Context, cancelQueued bool) (*types.InterruptResult, error) {
	request := map[string]any{"subtype": "interrupt"}
	if cancelQueued {
		request["cancel_queued"] = true
	}

	response, err := q.sendControlRequest(ctx, request)
	if err != nil {
		return nil, err
	}
	return types.InterruptResultFromMap(response), nil
}

// Reinitialize re-sends the initialize request to an already-running CLI.
//
// Use this after a transport gap: the response carries any permission or
// dialog requests the CLI is still blocked on, and the SDK redelivers them.
// Callbacks should be idempotent per request, since a request whose response
// was lost in the gap will be dispatched again.
func (q *Query) Reinitialize(ctx context.Context) (map[string]any, error) {
	return q.Initialize(ctx)
}

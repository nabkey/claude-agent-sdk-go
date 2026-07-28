package types

// This file holds the typed responses to control requests, plus decoders from
// the wire format. Each decoder is tolerant: an absent or wrongly-typed field
// yields the zero value rather than an error, so a newer CLI adding fields (or
// an older one omitting them) never breaks a caller.

import (
	"sort"
	"time"
)

// SlashCommand describes a command available in the session.
type SlashCommand struct {
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	ArgumentHint string   `json:"argumentHint,omitempty"`
	IsBuiltin    bool     `json:"isBuiltin,omitempty"`
	IsHidden     bool     `json:"isHidden,omitempty"`
	PluginName   string   `json:"pluginName,omitempty"`
	AllowedTools []string `json:"allowedTools,omitempty"`
}

// SlashCommandsFromAny decodes a list of slash commands.
func SlashCommandsFromAny(raw any) []SlashCommand {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]SlashCommand, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, SlashCommand{
			Name:         mapString(m, "name"),
			Description:  mapString(m, "description"),
			ArgumentHint: mapString(m, "argumentHint"),
			IsBuiltin:    mapBool(m, "isBuiltin"),
			IsHidden:     mapBool(m, "isHidden"),
			PluginName:   mapString(m, "pluginName"),
			AllowedTools: mapStrings(m, "allowedTools"),
		})
	}
	return out
}

// ModelInfo describes a model the session can select.
type ModelInfo struct {
	// Model is the selector to pass to Client.SetModel or AgentOptions.Model.
	// The CLI sends this as "value", and it may be an alias such as "default"
	// or "opus[1m]" rather than a concrete model ID.
	Model string `json:"model"`
	// ResolvedModel is the concrete model the selector resolves to, such as
	// "claude-opus-5[1m]". Empty when the CLI does not report one.
	ResolvedModel string `json:"resolvedModel,omitempty"`
	DisplayName   string `json:"displayName,omitempty"`
	Description   string `json:"description,omitempty"`
}

// ModelInfosFromAny decodes a list of models.
func ModelInfosFromAny(raw any) []ModelInfo {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]ModelInfo, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		info := ModelInfo{
			Model:         mapString(m, "value"),
			ResolvedModel: mapString(m, "resolvedModel"),
			DisplayName:   mapString(m, "displayName"),
			Description:   mapString(m, "description"),
		}
		// Older payloads key the selector as "model" or "name".
		if info.Model == "" {
			info.Model = mapString(m, "model")
		}
		if info.Model == "" {
			info.Model = mapString(m, "name")
		}
		out = append(out, info)
	}
	return out
}

// AgentInfo describes a subagent invokable via the Agent tool.
type AgentInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// Model is the alias the agent uses. Empty means it inherits the parent's.
	Model string `json:"model,omitempty"`
	// Source indicates where the agent is defined, when the CLI reports it.
	Source string `json:"source,omitempty"`
}

// AgentInfosFromAny decodes a list of agents.
func AgentInfosFromAny(raw any) []AgentInfo {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]AgentInfo, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, AgentInfo{
			Name:        mapString(m, "name"),
			Description: mapString(m, "description"),
			Model:       mapString(m, "model"),
			Source:      mapString(m, "source"),
		})
	}
	return out
}

// AccountInfo describes the authenticated account.
type AccountInfo struct {
	Email            string         `json:"email,omitempty"`
	Organization     string         `json:"organization,omitempty"`
	SubscriptionType string         `json:"subscriptionType,omitempty"`
	Raw              map[string]any `json:"-"`
}

// InitializeResult is the response to the initialize control request.
type InitializeResult struct {
	Commands               []SlashCommand `json:"commands,omitempty"`
	Agents                 []AgentInfo    `json:"agents,omitempty"`
	Models                 []ModelInfo    `json:"models,omitempty"`
	OutputStyle            string         `json:"output_style,omitempty"`
	AvailableOutputStyles  []string       `json:"available_output_styles,omitempty"`
	Account                *AccountInfo   `json:"account,omitempty"`
	FastModeState          string         `json:"fast_mode_state,omitempty"`
	FastModeDisabledReason string         `json:"fast_mode_disabled_reason,omitempty"`
	// Raw is the full response, including fields not modeled here.
	Raw map[string]any `json:"-"`
}

// InitializeResultFromMap decodes an initialize response.
func InitializeResultFromMap(m map[string]any) *InitializeResult {
	if m == nil {
		return nil
	}
	result := &InitializeResult{
		Commands:               SlashCommandsFromAny(m["commands"]),
		Agents:                 AgentInfosFromAny(m["agents"]),
		Models:                 ModelInfosFromAny(m["models"]),
		OutputStyle:            mapString(m, "output_style"),
		AvailableOutputStyles:  mapStrings(m, "available_output_styles"),
		FastModeState:          mapString(m, "fast_mode_state"),
		FastModeDisabledReason: mapString(m, "fast_mode_disabled_reason"),
		Raw:                    m,
	}
	if account, ok := m["account"].(map[string]any); ok {
		result.Account = &AccountInfo{
			Email:            mapString(account, "email"),
			Organization:     mapString(account, "organization"),
			SubscriptionType: mapString(account, "subscriptionType"),
			Raw:              account,
		}
	}
	return result
}

// ContextUsageCategory is one slice of the context window.
type ContextUsageCategory struct {
	Name       string `json:"name"`
	Tokens     int    `json:"tokens"`
	Color      string `json:"color,omitempty"`
	IsDeferred bool   `json:"isDeferred,omitempty"`
}

// ContextUsage is a breakdown of context window usage by category, matching
// what the CLI's /context command shows.
type ContextUsage struct {
	Categories []ContextUsageCategory `json:"categories,omitempty"`
	// TotalTokens is the tokens currently in the context window.
	TotalTokens int `json:"totalTokens"`
	// MaxTokens is the effective limit, which may be reduced by the
	// autocompact buffer.
	MaxTokens int `json:"maxTokens"`
	// RawMaxTokens is the model's raw context window size.
	RawMaxTokens int `json:"rawMaxTokens"`
	// Percentage of the context window used, 0-100.
	Percentage float64 `json:"percentage"`
	// Model the usage is calculated for.
	Model string `json:"model,omitempty"`
	// IsAutoCompactEnabled reports whether autocompact is on.
	IsAutoCompactEnabled bool `json:"isAutoCompactEnabled,omitempty"`
	// AutoCompactThreshold is the token count at which autocompact triggers.
	AutoCompactThreshold int `json:"autoCompactThreshold,omitempty"`
	// Raw is the full response, including the detailed per-item breakdowns
	// (memoryFiles, mcpTools, agents, systemPromptSections and so on).
	Raw map[string]any `json:"-"`
}

// ContextUsageFromMap decodes a context usage response.
func ContextUsageFromMap(m map[string]any) *ContextUsage {
	if m == nil {
		return nil
	}
	usage := &ContextUsage{
		TotalTokens:          mapInt(m, "totalTokens"),
		MaxTokens:            mapInt(m, "maxTokens"),
		RawMaxTokens:         mapInt(m, "rawMaxTokens"),
		Percentage:           mapFloat(m, "percentage"),
		Model:                mapString(m, "model"),
		IsAutoCompactEnabled: mapBool(m, "isAutoCompactEnabled"),
		AutoCompactThreshold: mapInt(m, "autoCompactThreshold"),
		Raw:                  m,
	}
	if items, ok := m["categories"].([]any); ok {
		for _, item := range items {
			c, ok := item.(map[string]any)
			if !ok {
				continue
			}
			usage.Categories = append(usage.Categories, ContextUsageCategory{
				Name:       mapString(c, "name"),
				Tokens:     mapInt(c, "tokens"),
				Color:      mapString(c, "color"),
				IsDeferred: mapBool(c, "isDeferred"),
			})
		}
	}
	return usage
}

// RewindFilesResult reports what a rewind did, or would do on a dry run.
type RewindFilesResult struct {
	CanRewind    bool     `json:"canRewind"`
	Error        string   `json:"error,omitempty"`
	FilesChanged []string `json:"filesChanged,omitempty"`
	Insertions   int      `json:"insertions,omitempty"`
	Deletions    int      `json:"deletions,omitempty"`
	// SkippedLinks counts tracked files not restored because a symlink, hard
	// link, or other non-regular file was found at the tracked path, or its
	// backup could not be safely read. Only populated on a real rewind.
	SkippedLinks int `json:"skippedLinks,omitempty"`
}

// RewindFilesResultFromMap decodes a rewind response.
func RewindFilesResultFromMap(m map[string]any) *RewindFilesResult {
	if m == nil {
		return nil
	}
	return &RewindFilesResult{
		CanRewind:    mapBool(m, "canRewind"),
		Error:        mapString(m, "error"),
		FilesChanged: mapStrings(m, "filesChanged"),
		Insertions:   mapInt(m, "insertions"),
		Deletions:    mapInt(m, "deletions"),
		SkippedLinks: mapInt(m, "skippedLinks"),
	}
}

// InterruptResult is the receipt for an interrupt.
type InterruptResult struct {
	// StillQueued lists uuids of queued messages that survive the interrupt
	// and will run unless cancelled. Only uuid-stamped main-thread messages
	// appear, so an empty list does not prove nothing will run.
	StillQueued []string `json:"still_queued,omitempty"`
	// Cancelled lists uuids removed by an interrupt that set cancelQueued.
	Cancelled []string `json:"cancelled,omitempty"`
}

// InterruptResultFromMap decodes an interrupt receipt.
func InterruptResultFromMap(m map[string]any) *InterruptResult {
	if m == nil {
		return &InterruptResult{}
	}
	return &InterruptResult{
		StillQueued: mapStrings(m, "still_queued"),
		Cancelled:   mapStrings(m, "cancelled"),
	}
}

// MCPSetServersResult reports the effect of replacing dynamic MCP servers.
type MCPSetServersResult struct {
	Added   []string          `json:"added,omitempty"`
	Removed []string          `json:"removed,omitempty"`
	Errors  map[string]string `json:"errors,omitempty"`
}

// MCPSetServersResultFromMap decodes a set-servers response.
func MCPSetServersResultFromMap(m map[string]any) *MCPSetServersResult {
	if m == nil {
		return &MCPSetServersResult{}
	}
	result := &MCPSetServersResult{
		Added:   mapStrings(m, "added"),
		Removed: mapStrings(m, "removed"),
	}
	if errs, ok := m["errors"].(map[string]any); ok {
		result.Errors = make(map[string]string, len(errs))
		for k, v := range errs {
			if s, ok := v.(string); ok {
				result.Errors[k] = s
			}
		}
	}
	return result
}

// ReloadPluginsResult is the refreshed session state after a plugin reload.
type ReloadPluginsResult struct {
	Commands   []SlashCommand    `json:"commands,omitempty"`
	Agents     []AgentInfo       `json:"agents,omitempty"`
	MCPServers []McpServerStatus `json:"mcpServers,omitempty"`
	Raw        map[string]any    `json:"-"`
}

// ReloadPluginsResultFromMap decodes a plugin reload response.
func ReloadPluginsResultFromMap(m map[string]any) *ReloadPluginsResult {
	if m == nil {
		return nil
	}
	return &ReloadPluginsResult{
		Commands:   SlashCommandsFromAny(m["commands"]),
		Agents:     AgentInfosFromAny(m["agents"]),
		MCPServers: McpServerStatusesFromAny(m["mcpServers"]),
		Raw:        m,
	}
}

// ReadFileResult is the content of a file read through the session.
//
// The CLI reports only the text and the path it resolved. It does not echo
// back an encoding, a size, or a truncation flag -- and as of CLI 2.1.220 it
// ignores the maxBytes hint entirely, returning the whole file.
type ReadFileResult struct {
	// Content is the file's text. The CLI sends this as "contents".
	Content string `json:"content"`
	// AbsPath is the absolute path the CLI resolved the read to.
	AbsPath string `json:"absPath,omitempty"`
}

// ReadFileResultFromMap decodes a file read response.
func ReadFileResultFromMap(m map[string]any) *ReadFileResult {
	if m == nil {
		return nil
	}
	result := &ReadFileResult{
		Content: mapString(m, "contents"),
		AbsPath: mapString(m, "absPath"),
	}
	// Tolerate the singular spelling in case another CLI build emits it.
	if result.Content == "" {
		result.Content = mapString(m, "content")
	}
	return result
}

// RateLimitWindow is utilization of one plan rate-limit window.
type RateLimitWindow struct {
	// Type names the window, such as "five_hour" or "seven_day".
	Type string `json:"type,omitempty"`
	// Utilization is the percentage of the window consumed, 0-100.
	Utilization float64 `json:"utilization,omitempty"`
	// ResetsAt is the Unix timestamp in seconds when the window resets. The
	// CLI sends an RFC 3339 string; it is parsed here so this matches the
	// epoch-seconds convention of RateLimitInfo.ResetsAt.
	ResetsAt int64 `json:"resetsAt,omitempty"`
}

// SessionUsage is the structured data behind the /usage command.
type SessionUsage struct {
	// TotalCostUSD is the session's cost so far. The CLI nests this under
	// "session", not at the top level.
	TotalCostUSD float64 `json:"totalCostUSD,omitempty"`
	// SubscriptionType is the plan backing the session, such as "max".
	SubscriptionType string `json:"subscriptionType,omitempty"`
	// RateLimitsAvailable is false for API key, Bedrock, Vertex and other
	// sessions where plan limits do not apply.
	RateLimitsAvailable bool `json:"rateLimitsAvailable,omitempty"`
	// RateLimits holds the populated plan windows, ordered by name. The CLI
	// sends these as a keyed object in which most windows are null, and it
	// carries several non-window entries ("limits", "spend", "extra_usage")
	// alongside them; only the populated windows appear here. Reach into Raw
	// for the normalized "limits" array or the spend breakdown.
	RateLimits []RateLimitWindow `json:"rateLimits,omitempty"`
	// Raw is the full response, including per-model usage breakdowns.
	Raw map[string]any `json:"-"`
}

// SessionUsageFromMap decodes a usage response.
func SessionUsageFromMap(m map[string]any) *SessionUsage {
	if m == nil {
		return nil
	}
	usage := &SessionUsage{
		SubscriptionType:    mapString(m, "subscription_type"),
		RateLimitsAvailable: mapBool(m, "rate_limits_available") || mapBool(m, "rateLimitsAvailable"),
		Raw:                 m,
	}
	if usage.SubscriptionType == "" {
		usage.SubscriptionType = mapString(m, "subscriptionType")
	}

	// Cost lives under "session"; fall back to a top-level key for other
	// CLI builds.
	if session, ok := m["session"].(map[string]any); ok {
		usage.TotalCostUSD = mapFloat(session, "total_cost_usd")
	}
	if usage.TotalCostUSD == 0 {
		usage.TotalCostUSD = mapFloat(m, "totalCostUSD")
	}
	if usage.TotalCostUSD == 0 {
		usage.TotalCostUSD = mapFloat(m, "total_cost_usd")
	}

	raw, ok := m["rate_limits"]
	if !ok {
		raw = m["rateLimits"]
	}
	usage.RateLimits = rateLimitWindows(raw)
	return usage
}

// rateLimitWindows decodes the rate-limit windows from either wire shape.
//
// Current CLI builds send an object keyed by window name; earlier drafts sent
// an array of windows carrying their own "type". Both are accepted.
func rateLimitWindows(raw any) []RateLimitWindow {
	switch v := raw.(type) {
	case map[string]any:
		// Most keys are null placeholders for windows that do not apply, and
		// a few siblings ("limits", "spend", "extra_usage") are not windows
		// at all. Rather than blacklist those names -- the CLI adds new ones
		// -- accept only entries shaped like a window: a numeric utilization.
		var windows []RateLimitWindow
		for name, entry := range v {
			w, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			utilization, ok := w["utilization"].(float64)
			if !ok {
				continue
			}
			windows = append(windows, RateLimitWindow{
				Type:        name,
				Utilization: utilization,
				ResetsAt:    mapEpochSeconds(w, "resets_at", "resetsAt"),
			})
		}
		// Map iteration is unordered; sort so callers see a stable list.
		sort.Slice(windows, func(i, j int) bool { return windows[i].Type < windows[j].Type })
		return windows

	case []any:
		var windows []RateLimitWindow
		for _, item := range v {
			w, ok := item.(map[string]any)
			if !ok {
				continue
			}
			windows = append(windows, RateLimitWindow{
				Type:        mapString(w, "type"),
				Utilization: mapFloat(w, "utilization"),
				ResetsAt:    mapEpochSeconds(w, "resets_at", "resetsAt"),
			})
		}
		return windows
	}
	return nil
}

// McpServerStatusesFromAny decodes a list of MCP server statuses.
func McpServerStatusesFromAny(raw any) []McpServerStatus {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]McpServerStatus, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, McpServerStatusFromMap(m))
	}
	return out
}

// McpServerStatusFromMap decodes one MCP server status.
func McpServerStatusFromMap(m map[string]any) McpServerStatus {
	status := McpServerStatus{
		Name:   mapString(m, "name"),
		Status: McpServerConnectionStatus(mapString(m, "status")),
	}
	if err := mapString(m, "error"); err != "" {
		status.Error = &err
	}
	if scope := mapString(m, "scope"); scope != "" {
		status.Scope = &scope
	}
	if config, ok := m["config"].(map[string]any); ok {
		status.Config = config
	}
	if info, ok := m["serverInfo"].(map[string]any); ok {
		status.ServerInfo = &McpServerInfo{
			Name:    mapString(info, "name"),
			Version: mapString(info, "version"),
		}
	}
	if tools, ok := m["tools"].([]any); ok {
		for _, item := range tools {
			t, ok := item.(map[string]any)
			if !ok {
				continue
			}
			tool := McpToolInfo{Name: mapString(t, "name")}
			if desc := mapString(t, "description"); desc != "" {
				tool.Description = &desc
			}
			if annot, ok := t["annotations"].(map[string]any); ok {
				tool.Annotations = &McpToolAnnotations{}
				if v, ok := annot["readOnly"].(bool); ok {
					tool.Annotations.ReadOnly = &v
				}
				if v, ok := annot["destructive"].(bool); ok {
					tool.Annotations.Destructive = &v
				}
				if v, ok := annot["openWorld"].(bool); ok {
					tool.Annotations.OpenWorld = &v
				}
			}
			status.Tools = append(status.Tools, tool)
		}
	}
	return status
}

// --- shared tolerant accessors ----------------------------------------------

func mapString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func mapBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

func mapFloat(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func mapInt(m map[string]any, key string) int {
	return int(mapFloat(m, key))
}

// mapEpochSeconds reads a timestamp under the first key that yields one,
// accepting either epoch seconds or an RFC 3339 string.
//
// The CLI renders rate-limit resets as RFC 3339 with a numeric offset
// ("2026-07-28T03:39:59.901559+00:00"), which time.RFC3339 parses -- Go
// tolerates the fractional seconds even though the layout omits them.
func mapEpochSeconds(m map[string]any, keys ...string) int64 {
	for _, key := range keys {
		switch v := m[key].(type) {
		case float64:
			return int64(v)
		case string:
			if v == "" {
				continue
			}
			if t, err := time.Parse(time.RFC3339, v); err == nil {
				return t.Unix()
			}
		}
	}
	return 0
}

func mapStrings(m map[string]any, key string) []string {
	items, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

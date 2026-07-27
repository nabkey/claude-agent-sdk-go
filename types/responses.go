package types

// This file holds the typed responses to control requests, plus decoders from
// the wire format. Each decoder is tolerant: an absent or wrongly-typed field
// yields the zero value rather than an error, so a newer CLI adding fields (or
// an older one omitting them) never breaks a caller.

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
	Model       string `json:"model"`
	DisplayName string `json:"displayName,omitempty"`
	Description string `json:"description,omitempty"`
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
			Model:       mapString(m, "model"),
			DisplayName: mapString(m, "displayName"),
			Description: mapString(m, "description"),
		}
		// Older payloads key the identifier as "name".
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
type ReadFileResult struct {
	Content   string `json:"content"`
	Encoding  string `json:"encoding,omitempty"`
	Truncated bool   `json:"truncated,omitempty"`
	Size      int    `json:"size,omitempty"`
}

// ReadFileResultFromMap decodes a file read response.
func ReadFileResultFromMap(m map[string]any) *ReadFileResult {
	if m == nil {
		return nil
	}
	return &ReadFileResult{
		Content:   mapString(m, "content"),
		Encoding:  mapString(m, "encoding"),
		Truncated: mapBool(m, "truncated"),
		Size:      mapInt(m, "size"),
	}
}

// RateLimitWindow is utilization of one plan rate-limit window.
type RateLimitWindow struct {
	Type        string  `json:"type,omitempty"`
	Utilization float64 `json:"utilization,omitempty"`
	ResetsAt    int64   `json:"resetsAt,omitempty"`
}

// SessionUsage is the structured data behind the /usage command.
type SessionUsage struct {
	// TotalCostUSD is the session's cost so far.
	TotalCostUSD float64 `json:"totalCostUSD,omitempty"`
	// RateLimitsAvailable is false for API key, Bedrock, Vertex and other
	// sessions where plan limits do not apply.
	RateLimitsAvailable bool              `json:"rateLimitsAvailable,omitempty"`
	RateLimits          []RateLimitWindow `json:"rateLimits,omitempty"`
	// Raw is the full response, including per-model usage breakdowns.
	Raw map[string]any `json:"-"`
}

// SessionUsageFromMap decodes a usage response.
func SessionUsageFromMap(m map[string]any) *SessionUsage {
	if m == nil {
		return nil
	}
	usage := &SessionUsage{
		TotalCostUSD:        mapFloat(m, "totalCostUSD"),
		RateLimitsAvailable: mapBool(m, "rate_limits_available") || mapBool(m, "rateLimitsAvailable"),
		Raw:                 m,
	}
	windows, ok := m["rate_limits"].([]any)
	if !ok {
		windows, _ = m["rateLimits"].([]any)
	}
	for _, item := range windows {
		w, ok := item.(map[string]any)
		if !ok {
			continue
		}
		usage.RateLimits = append(usage.RateLimits, RateLimitWindow{
			Type:        mapString(w, "type"),
			Utilization: mapFloat(w, "utilization"),
			ResetsAt:    int64(mapFloat(w, "resetsAt")),
		})
	}
	return usage
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

package types

// This file models the context_usage payload the CLI attaches to a /context
// result message. It is a different shape from ContextUsage, the response to
// the get_context_usage control request: the message form is snake_case and
// carries the per-item breakdowns inline.

// SDKContextUsageCategory is one slice of the context window.
type SDKContextUsageCategory struct {
	Name   string `json:"name"`
	Tokens int    `json:"tokens"`
	Color  string `json:"color,omitempty"`
	// IsDeferred marks content not yet loaded into the window.
	IsDeferred bool `json:"isDeferred,omitempty"`
}

// SDKContextOverLimit describes by how much the window is exceeded.
type SDKContextOverLimit struct {
	TokensOver int `json:"tokens_over"`
	// Kind says how the window was resolved, not whether the API will accept
	// the next request. "hard_limit" is the model's believed limit;
	// "compaction_window" is a compaction-policy window, which may or may not
	// coincide with it.
	Kind string `json:"kind,omitempty"`
}

// SDKContextMCPTool is one MCP tool's contribution to the context window.
type SDKContextMCPTool struct {
	// Name is the wire name, e.g. "mcp__linear__create_issue".
	Name       string `json:"name"`
	ServerName string `json:"server_name,omitempty"`
	Tokens     int    `json:"tokens"`
}

// SDKContextMemoryFile is one memory file's contribution.
type SDKContextMemoryFile struct {
	Path string `json:"path"`
	// Type is the display label of the source, e.g. "Project" or "User".
	Type   string `json:"type,omitempty"`
	Tokens int    `json:"tokens"`
}

// SDKContextAgent is one agent definition's contribution.
type SDKContextAgent struct {
	AgentType string `json:"agent_type"`
	// Source is the raw source identifier, e.g. "projectSettings", "plugin".
	Source string `json:"source,omitempty"`
	Tokens int    `json:"tokens"`
}

// SDKContextSkill is one skill's contribution.
type SDKContextSkill struct {
	Name string `json:"name"`
	// Source is the raw source identifier, e.g. "userSettings", "plugin".
	Source     string `json:"source,omitempty"`
	PluginName string `json:"plugin_name,omitempty"`
	Tokens     int    `json:"tokens"`
}

// SDKContextUsage is the structured payload behind a /context result message.
type SDKContextUsage struct {
	// Model is the main-loop model the usage was computed for.
	Model string `json:"model,omitempty"`
	// TotalTokens is the estimated tokens in use, unclamped: it may exceed
	// RawMaxTokens when over limit.
	TotalTokens int `json:"total_tokens"`
	// RawMaxTokens is the window the usage is measured against -- the
	// resolved autocompact window, which may be smaller than the model's
	// believed limit.
	RawMaxTokens int `json:"raw_max_tokens"`
	// Percentage is TotalTokens over RawMaxTokens, rounded, 0-100 and beyond.
	Percentage float64 `json:"percentage"`
	// OverLimit is set when TotalTokens exceeds RawMaxTokens.
	OverLimit  *SDKContextOverLimit      `json:"over_limit,omitempty"`
	Categories []SDKContextUsageCategory `json:"categories,omitempty"`
	MCPTools   []SDKContextMCPTool       `json:"mcp_tools,omitempty"`
	// MemoryFiles lists the memory files loaded into the window.
	MemoryFiles []SDKContextMemoryFile `json:"memory_files,omitempty"`
	Agents      []SDKContextAgent      `json:"agents,omitempty"`
	// Skills is omitted by the CLI when no skills contribute tokens.
	Skills []SDKContextSkill `json:"skills,omitempty"`
	// Raw is the full payload, including fields not modeled here.
	Raw map[string]any `json:"-"`
}

// SDKContextUsageFromAny decodes a context_usage payload, returning nil when
// absent or not an object.
func SDKContextUsageFromAny(raw any) *SDKContextUsage {
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	usage := &SDKContextUsage{
		Model:        mapString(m, "model"),
		TotalTokens:  mapInt(m, "total_tokens"),
		RawMaxTokens: mapInt(m, "raw_max_tokens"),
		Percentage:   mapFloat(m, "percentage"),
		Raw:          m,
	}
	if over, ok := m["over_limit"].(map[string]any); ok {
		usage.OverLimit = &SDKContextOverLimit{
			TokensOver: mapInt(over, "tokens_over"),
			Kind:       mapString(over, "kind"),
		}
	}
	forEachObject(m["categories"], func(c map[string]any) {
		usage.Categories = append(usage.Categories, SDKContextUsageCategory{
			Name:       mapString(c, "name"),
			Tokens:     mapInt(c, "tokens"),
			Color:      mapString(c, "color"),
			IsDeferred: mapBool(c, "isDeferred"),
		})
	})
	forEachObject(m["mcp_tools"], func(t map[string]any) {
		usage.MCPTools = append(usage.MCPTools, SDKContextMCPTool{
			Name:       mapString(t, "name"),
			ServerName: mapString(t, "server_name"),
			Tokens:     mapInt(t, "tokens"),
		})
	})
	forEachObject(m["memory_files"], func(f map[string]any) {
		usage.MemoryFiles = append(usage.MemoryFiles, SDKContextMemoryFile{
			Path:   mapString(f, "path"),
			Type:   mapString(f, "type"),
			Tokens: mapInt(f, "tokens"),
		})
	})
	forEachObject(m["agents"], func(a map[string]any) {
		usage.Agents = append(usage.Agents, SDKContextAgent{
			AgentType: mapString(a, "agent_type"),
			Source:    mapString(a, "source"),
			Tokens:    mapInt(a, "tokens"),
		})
	})
	forEachObject(m["skills"], func(s map[string]any) {
		usage.Skills = append(usage.Skills, SDKContextSkill{
			Name:       mapString(s, "name"),
			Source:     mapString(s, "source"),
			PluginName: mapString(s, "plugin_name"),
			Tokens:     mapInt(s, "tokens"),
		})
	})
	return usage
}

// forEachObject calls fn for every object in a raw JSON array, skipping
// entries that are not objects.
func forEachObject(raw any, fn func(map[string]any)) {
	items, ok := raw.([]any)
	if !ok {
		return
	}
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			fn(m)
		}
	}
}

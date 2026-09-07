package types

import (
	"reflect"
	"testing"
)

// Every decoder here is tolerant by design: a newer CLI adding fields, or an
// older one omitting them, must never break a caller.
func TestDecodersToleratePayloadsThatAreNotObjects(t *testing.T) {
	for _, raw := range []any{nil, "string", 42, map[string]any{"not": "a list"}} {
		if got := BackgroundTasksFromAny(raw); got != nil {
			t.Errorf("BackgroundTasksFromAny(%v) = %v, want nil", raw, got)
		}
		if got := MCPResourceLinksFromAny(raw); got != nil {
			t.Errorf("MCPResourceLinksFromAny(%v) = %v, want nil", raw, got)
		}
		if got := SlashCommandsFromAny(raw); got != nil {
			t.Errorf("SlashCommandsFromAny(%v) = %v, want nil", raw, got)
		}
		if got := AgentInfosFromAny(raw); got != nil {
			t.Errorf("AgentInfosFromAny(%v) = %v, want nil", raw, got)
		}
		if got := PermissionDenialsFromAny(raw); got != nil {
			t.Errorf("PermissionDenialsFromAny(%v) = %v, want nil", raw, got)
		}
		if got := McpServerStatusesFromAny(raw); got != nil {
			t.Errorf("McpServerStatusesFromAny(%v) = %v, want nil", raw, got)
		}
	}

	for _, raw := range []any{nil, "string", 42, []any{"not an object"}} {
		if got := MessageOriginFromAny(raw); got != nil {
			t.Errorf("MessageOriginFromAny(%v) = %v, want nil", raw, got)
		}
		if got := SDKContextUsageFromAny(raw); got != nil {
			t.Errorf("SDKContextUsageFromAny(%v) = %v, want nil", raw, got)
		}
		if got := ModelUsageFromAny(raw); got != nil {
			t.Errorf("ModelUsageFromAny(%v) = %v, want nil", raw, got)
		}
	}
}

// A list whose entries are all unusable yields an empty result rather than a
// decode failure, so one malformed row never costs a caller the whole list.
func TestListDecodersSkipUnusableEntries(t *testing.T) {
	unusable := []any{"not an object", 42, nil}

	if got := BackgroundTasksFromAny(unusable); len(got) != 0 {
		t.Errorf("BackgroundTasksFromAny = %v, want empty", got)
	}
	if got := SlashCommandsFromAny(unusable); len(got) != 0 {
		t.Errorf("SlashCommandsFromAny = %v, want empty", got)
	}
	if got := McpServerStatusesFromAny(unusable); len(got) != 0 {
		t.Errorf("McpServerStatusesFromAny = %v, want empty", got)
	}
}

func TestFromMapDecodersHandleNil(t *testing.T) {
	if got := InitializeResultFromMap(nil); got != nil {
		t.Errorf("InitializeResultFromMap(nil) = %v, want nil", got)
	}
	if got := ContextUsageFromMap(nil); got != nil {
		t.Errorf("ContextUsageFromMap(nil) = %v, want nil", got)
	}
	if got := RewindFilesResultFromMap(nil); got != nil {
		t.Errorf("RewindFilesResultFromMap(nil) = %v, want nil", got)
	}
	if got := ReloadPluginsResultFromMap(nil); got != nil {
		t.Errorf("ReloadPluginsResultFromMap(nil) = %v, want nil", got)
	}
	// These two answer with an empty value rather than nil, since a caller
	// reads their fields unconditionally.
	if got := InterruptResultFromMap(nil); got == nil {
		t.Error("InterruptResultFromMap(nil) must return an empty receipt")
	}
	if got := MCPSetServersResultFromMap(nil); got == nil {
		t.Error("MCPSetServersResultFromMap(nil) must return an empty result")
	}
}

func TestModelUsageFromAny(t *testing.T) {
	usage := ModelUsageFromAny(map[string]any{
		"claude-opus-5": map[string]any{
			"inputTokens":              float64(100),
			"outputTokens":             float64(200),
			"thinkingTokens":           float64(50),
			"cacheReadInputTokens":     float64(10),
			"cacheCreationInputTokens": float64(20),
			"webSearchRequests":        float64(2),
			"costUSD":                  0.75,
			"contextWindow":            float64(200000),
			"maxOutputTokens":          float64(64000),
			"canonicalModel":           "claude-opus-5",
			"provider":                 "firstParty",
			"costBasis":                "list",
		},
		"skipped": "not an object",
	})

	if len(usage) != 1 {
		t.Fatalf("expected only the well-formed entry, got %v", usage)
	}
	got := usage["claude-opus-5"]
	want := ModelUsage{
		InputTokens: 100, OutputTokens: 200, ThinkingTokens: 50,
		CacheReadInputTokens: 10, CacheCreationInputTokens: 20,
		WebSearchRequests: 2, CostUSD: 0.75, ContextWindow: 200000,
		MaxOutputTokens: 64000, CanonicalModel: "claude-opus-5",
		Provider: "firstParty", CostBasis: CostBasisList,
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("usage =\n got  %+v\n want %+v", got, want)
	}
}

// Absent on builds that predate the field, where a caller should treat it as
// list pricing rather than as a distinct state.
func TestModelUsageWithoutCostBasis(t *testing.T) {
	usage := ModelUsageFromAny(map[string]any{
		"m": map[string]any{"inputTokens": float64(1)},
	})
	if got := usage["m"].CostBasis; got != "" {
		t.Errorf("CostBasis = %q, want empty", got)
	}
}

func TestMessageOriginIsHuman(t *testing.T) {
	tests := []struct {
		name   string
		origin *MessageOrigin
		want   bool
	}{
		{"nil is not human", nil, false},
		{"human", &MessageOrigin{Kind: MessageOriginKindHuman}, true},
		{"peer", &MessageOrigin{Kind: MessageOriginKindPeer}, false},
		{"unrecognized kinds are not human", &MessageOrigin{Kind: "something-new"}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.origin.IsHuman(); got != tc.want {
				t.Errorf("IsHuman() = %v, want %v", got, tc.want)
			}
		})
	}
}

// The full payload is retained so a consumer can read keys this SDK version
// does not model.
func TestMessageOriginRetainsRawPayload(t *testing.T) {
	raw := map[string]any{"kind": "peer", "from": "a", "somethingNew": "value"}
	origin := MessageOriginFromAny(raw)

	if origin.Raw["somethingNew"] != "value" {
		t.Errorf("Raw = %v, want the unmodeled key retained", origin.Raw)
	}
}

// A pid that is not a number means unverifiable, which must read as absent
// rather than as pid zero.
func TestMessageOriginIgnoresNonNumericPID(t *testing.T) {
	origin := MessageOriginFromAny(map[string]any{"kind": "peer", "verifiedPeerPid": "4242"})
	if origin.VerifiedPeerPID != nil {
		t.Errorf("VerifiedPeerPID = %v, want nil", origin.VerifiedPeerPID)
	}
}

func TestSDKContextUsageSkipsAbsentBreakdowns(t *testing.T) {
	usage := SDKContextUsageFromAny(map[string]any{
		"total_tokens":   float64(10),
		"raw_max_tokens": float64(100),
	})

	if usage.TotalTokens != 10 || usage.RawMaxTokens != 100 {
		t.Errorf("unexpected totals: %+v", usage)
	}
	// The CLI omits skills entirely when none contribute tokens.
	if usage.Skills != nil || usage.Categories != nil || usage.OverLimit != nil {
		t.Errorf("absent breakdowns must decode to nil: %+v", usage)
	}
}

func TestBackgroundTasksFromAny(t *testing.T) {
	tasks := BackgroundTasksFromAny([]any{
		map[string]any{"task_id": "t-1", "task_type": "agent", "description": "work"},
		map[string]any{"task_id": "t-2", "ambient": true},
		"not an object",
	})

	if len(tasks) != 2 {
		t.Fatalf("expected the two objects, got %v", tasks)
	}
	if tasks[0].TaskID != "t-1" || tasks[0].Ambient {
		t.Errorf("tasks[0] = %+v", tasks[0])
	}
	if !tasks[1].Ambient {
		t.Error("expected the second task to be ambient")
	}
}

func TestMCPResourceLinksFromAny(t *testing.T) {
	links := MCPResourceLinksFromAny([]any{
		map[string]any{
			"uri": "file:///a.csv", "name": "a.csv", "title": "Results",
			"description": "the rows", "mimeType": "text/csv",
		},
	})

	if len(links) != 1 {
		t.Fatalf("expected one link, got %v", links)
	}
	got := links[0]
	if got.URI != "file:///a.csv" || got.Name != "a.csv" || got.Title != "Results" ||
		got.Description != "the rows" || got.MIMEType != "text/csv" {
		t.Errorf("link = %+v", got)
	}
	if got.Raw == nil {
		t.Error("expected the raw block to be retained")
	}
}

func TestPermissionDenialsFromAny(t *testing.T) {
	denials := PermissionDenialsFromAny([]any{
		map[string]any{
			"tool_name": "Bash", "tool_use_id": "t-1",
			"tool_input": map[string]any{"command": "rm"},
		},
		"not an object",
	})

	if len(denials) != 1 {
		t.Fatalf("expected one denial, got %v", denials)
	}
	if denials[0].ToolName != "Bash" || denials[0].ToolUseID != "t-1" {
		t.Errorf("denial = %+v", denials[0])
	}
	if denials[0].ToolInput["command"] != "rm" {
		t.Errorf("ToolInput = %v", denials[0].ToolInput)
	}
}

func TestSlashCommandsFromAny(t *testing.T) {
	commands := SlashCommandsFromAny([]any{
		map[string]any{
			"name": "review", "description": "Review the diff",
			"argumentHint": "[path]", "isBuiltin": true, "isHidden": false,
			"pluginName": "reviewer", "allowedTools": []any{"Read", 7},
		},
	})

	if len(commands) != 1 {
		t.Fatalf("expected one command, got %v", commands)
	}
	got := commands[0]
	if got.Name != "review" || !got.IsBuiltin || got.PluginName != "reviewer" {
		t.Errorf("command = %+v", got)
	}
	// The non-string entry is dropped rather than failing the decode.
	if len(got.AllowedTools) != 1 || got.AllowedTools[0] != "Read" {
		t.Errorf("AllowedTools = %v", got.AllowedTools)
	}
}

func TestAgentInfosFromAny(t *testing.T) {
	agents := AgentInfosFromAny([]any{
		map[string]any{"name": "Explore", "description": "search", "model": "haiku", "source": "plugin"},
	})
	if len(agents) != 1 {
		t.Fatalf("expected one agent, got %v", agents)
	}
	if agents[0].Name != "Explore" || agents[0].Source != "plugin" {
		t.Errorf("agent = %+v", agents[0])
	}
}

func TestInitializeResultFromMap(t *testing.T) {
	result := InitializeResultFromMap(map[string]any{
		"commands":                  []any{map[string]any{"name": "review"}},
		"agents":                    []any{map[string]any{"name": "Explore"}},
		"models":                    []any{map[string]any{"value": "opus"}},
		"output_style":              "default",
		"available_output_styles":   []any{"default", "concise"},
		"fast_mode_state":           "on",
		"fast_mode_disabled_reason": "",
		"account": map[string]any{
			"email": "a@example.test", "organization": "Acme", "subscriptionType": "max",
		},
		"somethingNew": true,
	})

	if len(result.Commands) != 1 || len(result.Agents) != 1 || len(result.Models) != 1 {
		t.Errorf("unexpected lists: %+v", result)
	}
	if result.OutputStyle != "default" || len(result.AvailableOutputStyles) != 2 {
		t.Errorf("unexpected output styles: %+v", result)
	}
	if result.Account == nil || result.Account.Email != "a@example.test" {
		t.Fatalf("account = %+v", result.Account)
	}
	if result.Account.SubscriptionType != "max" {
		t.Errorf("SubscriptionType = %q", result.Account.SubscriptionType)
	}
	// Raw keeps whatever this SDK version does not model.
	if result.Raw["somethingNew"] != true {
		t.Error("expected the raw response to be retained")
	}
}

func TestInitializeResultWithoutAccount(t *testing.T) {
	result := InitializeResultFromMap(map[string]any{"output_style": "default"})
	if result.Account != nil {
		t.Errorf("Account = %+v, want nil", result.Account)
	}
}

func TestContextUsageFromMap(t *testing.T) {
	usage := ContextUsageFromMap(map[string]any{
		"totalTokens":          float64(120000),
		"maxTokens":            float64(180000),
		"rawMaxTokens":         float64(200000),
		"percentage":           float64(60),
		"model":                "claude-opus-5",
		"isAutoCompactEnabled": true,
		"autoCompactThreshold": float64(160000),
		"categories": []any{
			map[string]any{"name": "Messages", "tokens": float64(100000), "color": "blue"},
			map[string]any{"name": "Deferred", "tokens": float64(0), "isDeferred": true},
			"not an object",
		},
	})

	if usage.TotalTokens != 120000 || usage.MaxTokens != 180000 || usage.RawMaxTokens != 200000 {
		t.Errorf("unexpected totals: %+v", usage)
	}
	if !usage.IsAutoCompactEnabled || usage.AutoCompactThreshold != 160000 {
		t.Errorf("unexpected autocompact fields: %+v", usage)
	}
	if len(usage.Categories) != 2 {
		t.Fatalf("expected the two objects, got %v", usage.Categories)
	}
	if !usage.Categories[1].IsDeferred {
		t.Error("expected the deferred flag to survive")
	}
}

func TestRewindFilesResultFromMap(t *testing.T) {
	result := RewindFilesResultFromMap(map[string]any{
		"canRewind":    true,
		"filesChanged": []any{"a.go", "b.go"},
		"insertions":   float64(4),
		"deletions":    float64(2),
		"skippedLinks": float64(1),
	})

	if !result.CanRewind || len(result.FilesChanged) != 2 {
		t.Errorf("result = %+v", result)
	}
	if result.Insertions != 4 || result.Deletions != 2 || result.SkippedLinks != 1 {
		t.Errorf("unexpected counts: %+v", result)
	}
	if result.Error != "" {
		t.Errorf("Error = %q, want empty", result.Error)
	}
}

func TestInterruptResultFromMap(t *testing.T) {
	result := InterruptResultFromMap(map[string]any{
		"still_queued": []any{"u-1"},
		"cancelled":    []any{"u-2", "u-3"},
	})
	if len(result.StillQueued) != 1 || len(result.Cancelled) != 2 {
		t.Errorf("result = %+v", result)
	}
}

func TestMCPSetServersResultFromMap(t *testing.T) {
	result := MCPSetServersResultFromMap(map[string]any{
		"added":   []any{"a"},
		"removed": []any{"b"},
		"errors":  map[string]any{"c": "connection refused", "d": 7},
	})

	if len(result.Added) != 1 || len(result.Removed) != 1 {
		t.Errorf("result = %+v", result)
	}
	// A non-string error value is dropped rather than stringified.
	if len(result.Errors) != 1 || result.Errors["c"] != "connection refused" {
		t.Errorf("Errors = %v", result.Errors)
	}
}

func TestReloadPluginsResultFromMap(t *testing.T) {
	result := ReloadPluginsResultFromMap(map[string]any{
		"commands":   []any{map[string]any{"name": "review"}},
		"agents":     []any{map[string]any{"name": "Explore"}},
		"mcpServers": []any{map[string]any{"name": "linear", "status": "connected"}},
	})

	if len(result.Commands) != 1 || len(result.Agents) != 1 || len(result.MCPServers) != 1 {
		t.Errorf("result = %+v", result)
	}
	if result.MCPServers[0].Status != McpServerConnectionStatusConnected {
		t.Errorf("status = %q", result.MCPServers[0].Status)
	}
}

func TestMcpServerStatusFromMap(t *testing.T) {
	status := McpServerStatusFromMap(map[string]any{
		"name":       "linear",
		"status":     "failed",
		"error":      "connection refused",
		"scope":      "project",
		"config":     map[string]any{"type": "http"},
		"serverInfo": map[string]any{"name": "linear-impl", "version": "2.0.0"},
		"tools": []any{
			map[string]any{
				"name":        "create_issue",
				"description": "Create an issue",
				"annotations": map[string]any{"readOnly": false, "destructive": true, "openWorld": true},
			},
			"not an object",
		},
	})

	if status.Name != "linear" || status.Status != McpServerConnectionStatusFailed {
		t.Errorf("status = %+v", status)
	}
	if status.Error == nil || *status.Error != "connection refused" {
		t.Errorf("Error = %v", status.Error)
	}
	if status.Scope == nil || *status.Scope != "project" {
		t.Errorf("Scope = %v", status.Scope)
	}
	if status.ServerInfo == nil || status.ServerInfo.Version != "2.0.0" {
		t.Errorf("ServerInfo = %+v", status.ServerInfo)
	}
	if len(status.Tools) != 1 {
		t.Fatalf("expected the one well-formed tool, got %v", status.Tools)
	}

	tool := status.Tools[0]
	if tool.Annotations == nil {
		t.Fatal("expected annotations")
	}
	if tool.Annotations.ReadOnly == nil || *tool.Annotations.ReadOnly {
		t.Errorf("ReadOnly = %v, want false", tool.Annotations.ReadOnly)
	}
	if tool.Annotations.Destructive == nil || !*tool.Annotations.Destructive {
		t.Errorf("Destructive = %v, want true", tool.Annotations.Destructive)
	}
}

// Optional pointer fields stay nil when the CLI omits them, so a caller can
// tell "not reported" from "reported empty".
func TestMcpServerStatusOmittedFieldsStayNil(t *testing.T) {
	status := McpServerStatusFromMap(map[string]any{"name": "linear", "status": "pending"})

	if status.Error != nil || status.Scope != nil || status.ServerInfo != nil {
		t.Errorf("expected nil optionals, got %+v", status)
	}
}

func TestSessionStoreEntryAccessors(t *testing.T) {
	entry := SessionStoreEntry{
		"type": "user", "uuid": "u-1", "timestamp": "2026-01-01T00:00:00Z",
	}
	if entry.Type() != "user" || entry.UUID() != "u-1" {
		t.Errorf("entry = %+v", entry)
	}
	if entry.Timestamp() != "2026-01-01T00:00:00Z" {
		t.Errorf("Timestamp = %q", entry.Timestamp())
	}

	// Entries without a uuid (titles, tags, mode markers) read as empty
	// rather than panicking.
	bare := SessionStoreEntry{"type": "title"}
	if bare.UUID() != "" || bare.Timestamp() != "" {
		t.Errorf("bare entry = %+v", bare)
	}
}

func TestIsTerminalTaskStatus(t *testing.T) {
	// The two lifecycle vocabularies have to be treated the same way:
	// task_notification reports "stopped" where task_updated reports "killed".
	for _, status := range []string{"completed", "failed", "stopped", "killed"} {
		if !IsTerminalTaskStatus(status) {
			t.Errorf("IsTerminalTaskStatus(%q) = false, want true", status)
		}
	}
	for _, status := range []string{"pending", "running", "paused", "", "unknown"} {
		if IsTerminalTaskStatus(status) {
			t.Errorf("IsTerminalTaskStatus(%q) = true, want false", status)
		}
	}
}

func TestThinkingConfigConstructors(t *testing.T) {
	adaptive := NewThinkingAdaptive()
	if adaptive.ThinkingType() != "adaptive" || adaptive.DisplayMode() != nil {
		t.Errorf("adaptive = %+v", adaptive)
	}

	enabled := NewThinkingEnabled(4096)
	if enabled.ThinkingType() != "enabled" {
		t.Errorf("enabled type = %q", enabled.ThinkingType())
	}
	if enabled.BudgetTokens == nil || *enabled.BudgetTokens != 4096 {
		t.Errorf("BudgetTokens = %v", enabled.BudgetTokens)
	}

	disabled := NewThinkingDisabled()
	if disabled.ThinkingType() != "disabled" || disabled.DisplayMode() != nil {
		t.Errorf("disabled = %+v", disabled)
	}

	// The display mode is what a caller sets to receive summarized thinking
	// text on models that omit it by default.
	summarized := ThinkingDisplaySummarized
	adaptive.Display = &summarized
	if adaptive.DisplayMode() == nil || *adaptive.DisplayMode() != summarized {
		t.Errorf("DisplayMode = %v", adaptive.DisplayMode())
	}
}

// Unset optional fields are omitted so the CLI sees only what the caller
// configured.
func TestAgentDefinitionToMap(t *testing.T) {
	minimal := AgentDefinition{Description: "d", Prompt: "p"}.ToMap()
	if len(minimal) != 2 || minimal["description"] != "d" || minimal["prompt"] != "p" {
		t.Errorf("minimal = %v", minimal)
	}

	model := "opus"
	memory := AgentMemoryProject
	initial := "start here"
	maxTurns := 5
	background := true
	mode := PermissionMode("plan")
	observer := "watcher"
	observerMessage := "watch closely"

	full := AgentDefinition{
		Description:     "d",
		Prompt:          "p",
		Tools:           []string{"Read"},
		DisallowedTools: []string{"Bash"},
		Model:           &model,
		Skills:          []string{"pdf"},
		Memory:          &memory,
		MCPServers:      []any{"linear"},
		InitialPrompt:   &initial,
		MaxTurns:        &maxTurns,
		Background:      &background,
		Effort:          "high",
		PermissionMode:  &mode,
		Observer:        &observer,
		ObserverMessage: &observerMessage,
	}.ToMap()

	want := map[string]any{
		"description": "d", "prompt": "p", "model": "opus",
		"memory": "project", "initialPrompt": "start here", "maxTurns": 5,
		"background": true, "effort": "high", "permissionMode": "plan",
		"observer": "watcher", "observerMessage": "watch closely",
	}
	for key, value := range want {
		if !reflect.DeepEqual(full[key], value) {
			t.Errorf("full[%q] = %v, want %v", key, full[key], value)
		}
	}
	if !reflect.DeepEqual(full["tools"], []string{"Read"}) {
		t.Errorf("tools = %v", full["tools"])
	}
	if !reflect.DeepEqual(full["mcpServers"], []any{"linear"}) {
		t.Errorf("mcpServers = %v", full["mcpServers"])
	}
}

package protocol

import (
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Every event in types.AllHookEvents must dispatch. Before the generic
// fallback existed, a callback registered for one of the unmodeled events
// never ran: parsing failed and the CLI got an error instead of a decision.
func TestParseHookInputCoversEveryEvent(t *testing.T) {
	for _, event := range types.AllHookEvents {
		t.Run(string(event), func(t *testing.T) {
			input, err := parseHookInput(map[string]any{
				"hook_event_name": string(event),
				"session_id":      "s-1",
				"transcript_path": "/tmp/t.jsonl",
				"cwd":             "/repo",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if input.GetHookEventName() != event {
				t.Errorf("GetHookEventName() = %q, want %q", input.GetHookEventName(), event)
			}
			if input.GetSessionID() != "s-1" {
				t.Errorf("GetSessionID() = %q, want s-1", input.GetSessionID())
			}
		})
	}
}

// The CLI adds hook events faster than the SDK can name them, so one it does
// not model still has to reach the callback.
func TestParseHookInputUnknownEventFallsBack(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "SomeFutureEvent",
		"session_id":      "s-1",
		"novel_field":     "value",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	generic, ok := input.(*types.GenericHookInput)
	if !ok {
		t.Fatalf("expected *types.GenericHookInput, got %T", input)
	}
	if generic.HookEventName != "SomeFutureEvent" {
		t.Errorf("HookEventName = %q, want SomeFutureEvent", generic.HookEventName)
	}
	if generic.Data["novel_field"] != "value" {
		t.Error("expected the raw payload to be retained")
	}
}

// A payload with no event name is malformed rather than futuristic: there is
// nothing to dispatch on.
func TestParseHookInputRejectsMissingEventName(t *testing.T) {
	if _, err := parseHookInput(map[string]any{"session_id": "s-1"}); err == nil {
		t.Error("expected an error for a hook input with no hook_event_name")
	}
}

func TestParseHookInputBaseFields(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "Stop",
		"session_id":      "s-1",
		"transcript_path": "/tmp/t.jsonl",
		"cwd":             "/repo",
		"permission_mode": "plan",
		"prompt_id":       "p-1",
		"effort":          map[string]any{"level": "high"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	stop := mustBe[*types.StopHookInput](t, input)
	if stop.PromptID != "p-1" {
		t.Errorf("PromptID = %q, want p-1", stop.PromptID)
	}
	if stop.Effort != "high" {
		t.Errorf("Effort = %q, want high", stop.Effort)
	}
	if stop.PermissionMode == nil || *stop.PermissionMode != "plan" {
		t.Errorf("PermissionMode = %v, want plan", stop.PermissionMode)
	}
}

func TestParseHookInputSessionStart(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name":             "SessionStart",
		"source":                      "resume",
		"model":                       "claude-opus-5",
		"session_title":               "Refactor",
		"seconds_since_last_response": float64(42.5),
		"context_tokens":              float64(1200),
		"prompt_cache_likely_expired": true,
		"estimated_cache_write_usd":   0.02,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	start := mustBe[*types.SessionStartHookInput](t, input)
	if start.Source != types.SessionStartResume {
		t.Errorf("Source = %q, want resume", start.Source)
	}
	if start.SecondsSinceLastResponse != 42.5 || start.ContextTokens != 1200 {
		t.Errorf("unexpected numbers: %+v", start)
	}
	if !start.PromptCacheLikelyExpired || start.EstimatedCacheWriteUSD != 0.02 {
		t.Errorf("unexpected cache fields: %+v", start)
	}
}

func TestParseHookInputSessionEnd(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "SessionEnd",
		"reason":          "logout",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mustBe[*types.SessionEndHookInput](t, input).Reason; got != types.ExitReasonLogout {
		t.Errorf("Reason = %q, want logout", got)
	}
}

func TestParseHookInputPostCompact(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "PostCompact",
		"trigger":         "auto",
		"compact_summary": "we discussed the parser",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	compact := mustBe[*types.PostCompactHookInput](t, input)
	if compact.Trigger != "auto" || compact.CompactSummary != "we discussed the parser" {
		t.Errorf("unexpected fields: %+v", compact)
	}
}

func TestParseHookInputPermissionDenied(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "PermissionDenied",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "rm -rf /"},
		"tool_use_id":     "tool-1",
		"reason":          "deny rule matched",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	denied := mustBe[*types.PermissionDeniedHookInput](t, input)
	if denied.ToolName != "Bash" || denied.ToolUseID != "tool-1" {
		t.Errorf("unexpected identity: %+v", denied)
	}
	if denied.Reason != "deny rule matched" {
		t.Errorf("Reason = %q", denied.Reason)
	}
	if denied.ToolInput["command"] != "rm -rf /" {
		t.Errorf("ToolInput = %v", denied.ToolInput)
	}
}

func TestParseHookInputUserPromptExpansion(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "UserPromptExpansion",
		"expansion_type":  "slash_command",
		"command_name":    "review",
		"command_args":    "--fix",
		"command_source":  "project",
		"prompt":          "review the diff",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expansion := mustBe[*types.UserPromptExpansionHookInput](t, input)
	if expansion.ExpansionType != types.UserPromptExpansionSlashCommand {
		t.Errorf("ExpansionType = %q", expansion.ExpansionType)
	}
	if expansion.CommandName != "review" || expansion.CommandArgs != "--fix" {
		t.Errorf("unexpected command: %+v", expansion)
	}
}

func TestParseHookInputPostToolBatch(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "PostToolBatch",
		"tool_calls": []any{
			map[string]any{
				"tool_name":     "Read",
				"tool_input":    map[string]any{"file_path": "/a"},
				"tool_use_id":   "t-1",
				"tool_response": "contents",
			},
			"not an object",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	batch := mustBe[*types.PostToolBatchHookInput](t, input)
	if len(batch.ToolCalls) != 1 {
		t.Fatalf("expected the one well-formed call, got %d", len(batch.ToolCalls))
	}
	if batch.ToolCalls[0].ToolName != "Read" || batch.ToolCalls[0].ToolResponse != "contents" {
		t.Errorf("unexpected call: %+v", batch.ToolCalls[0])
	}
}

func TestParseHookInputModelSwitch(t *testing.T) {
	for _, event := range []string{"PreModelSwitch", "PostModelSwitch"} {
		t.Run(event, func(t *testing.T) {
			input, err := parseHookInput(map[string]any{
				"hook_event_name":           event,
				"from_model":                "claude-sonnet-5",
				"to_model":                  "claude-opus-5",
				"requested_model":           "opus",
				"source":                    "sdk",
				"context_tokens":            float64(9000),
				"prompt_cache_warm":         true,
				"cache_ttl":                 "1h",
				"estimated_cache_write_usd": 0.11,
				"pricing":                   "catalog",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var got types.ModelSwitch
			switch v := input.(type) {
			case *types.PreModelSwitchHookInput:
				got = v.ModelSwitch
			case *types.PostModelSwitchHookInput:
				got = v.ModelSwitch
			default:
				t.Fatalf("unexpected type %T", input)
			}

			if got.FromModel != "claude-sonnet-5" || got.ToModel != "claude-opus-5" {
				t.Errorf("unexpected models: %+v", got)
			}
			if got.ContextTokens != 9000 || !got.PromptCacheWarm || got.CacheTTL != "1h" {
				t.Errorf("unexpected cache fields: %+v", got)
			}
			if got.EstimatedCacheWriteUSD != 0.11 || got.Pricing != "catalog" {
				t.Errorf("unexpected pricing: %+v", got)
			}
		})
	}
}

func TestParseHookInputElicitation(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name":  "Elicitation",
		"mcp_server_name":  "linear",
		"message":          "sign in",
		"mode":             "url",
		"url":              "https://example.test/oauth",
		"elicitation_id":   "e-1",
		"requested_schema": map[string]any{"type": "object"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	elicit := mustBe[*types.ElicitationHookInput](t, input)
	if elicit.MCPServerName != "linear" || elicit.Mode != types.ElicitationModeURL {
		t.Errorf("unexpected fields: %+v", elicit)
	}
	if elicit.RequestedSchema["type"] != "object" {
		t.Errorf("RequestedSchema = %v", elicit.RequestedSchema)
	}
}

func TestParseHookInputElicitationResult(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name": "ElicitationResult",
		"mcp_server_name": "linear",
		"action":          "accept",
		"content":         map[string]any{"team": "core"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := mustBe[*types.ElicitationResultHookInput](t, input)
	if result.Action != types.ElicitationAccept || result.Content["team"] != "core" {
		t.Errorf("unexpected fields: %+v", result)
	}
}

func TestParseHookInputFileAndDirectoryEvents(t *testing.T) {
	changed, err := parseHookInput(map[string]any{
		"hook_event_name": "FileChanged",
		"file_path":       "/repo/main.go",
		"event":           "change",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	file := mustBe[*types.FileChangedHookInput](t, changed)
	if file.FilePath != "/repo/main.go" || file.Event != "change" {
		t.Errorf("unexpected file event: %+v", file)
	}

	added, err := parseHookInput(map[string]any{
		"hook_event_name": "DirectoryAdded",
		"directory":       "/repo/vendor",
		"source":          "slash_command",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	dir := mustBe[*types.DirectoryAddedHookInput](t, added)
	if dir.Directory != "/repo/vendor" || dir.Source != "slash_command" {
		t.Errorf("unexpected directory event: %+v", dir)
	}
}

func TestParseHookInputInstructionsLoaded(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name":   "InstructionsLoaded",
		"file_path":         "/repo/CLAUDE.md",
		"memory_type":       "Project",
		"load_reason":       "session_start",
		"globs":             []any{"**/*.go", 7},
		"trigger_file_path": "/repo/main.go",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	loaded := mustBe[*types.InstructionsLoadedHookInput](t, input)
	if loaded.MemoryType != "Project" || loaded.LoadReason != "session_start" {
		t.Errorf("unexpected fields: %+v", loaded)
	}
	if len(loaded.Globs) != 1 || loaded.Globs[0] != "**/*.go" {
		t.Errorf("Globs = %v, want only the string entry", loaded.Globs)
	}
}

func TestParseHookInputStopFailure(t *testing.T) {
	input, err := parseHookInput(map[string]any{
		"hook_event_name":        "StopFailure",
		"error":                  "rate_limit",
		"error_details":          "retry after 60s",
		"last_assistant_message": "working on it",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	failure := mustBe[*types.StopFailureHookInput](t, input)
	if failure.Error != types.AssistantMessageErrorRateLimit {
		t.Errorf("Error = %q, want rate_limit", failure.Error)
	}
	if failure.ErrorDetails != "retry after 60s" {
		t.Errorf("ErrorDetails = %q", failure.ErrorDetails)
	}
}

func TestParseHookInputTaskEvents(t *testing.T) {
	created, err := parseHookInput(map[string]any{
		"hook_event_name": "TaskCreated",
		"task_id":         "t-1",
		"task_subject":    "Fix the parser",
		"teammate_name":   "alex",
		"team_name":       "core",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	task := mustBe[*types.TaskCreatedHookInput](t, created)
	if task.TaskID != "t-1" || task.TaskSubject != "Fix the parser" {
		t.Errorf("unexpected task: %+v", task)
	}

	completed, err := parseHookInput(map[string]any{
		"hook_event_name": "TaskCompleted",
		"task_id":         "t-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mustBe[*types.TaskCompletedHookInput](t, completed).TaskID != "t-1" {
		t.Error("unexpected completed task id")
	}
}

// The wire names live on the struct tags, so the serializer must carry every
// field of every hook-specific output through unchanged.
func TestHookOutputToMapSyncFields(t *testing.T) {
	classifier := "the command only listed files"
	suppress := true
	title := "Renamed"

	tests := []struct {
		name   string
		output *types.HookOutput
		want   map[string]any
	}{
		{
			name: "PostToolUse classifier context",
			output: &types.HookOutput{
				HookSpecificOutput: &types.PostToolUseHookSpecificOutput{
					HookEventName:     "PostToolUse",
					ClassifierContext: &classifier,
					UpdatedToolOutput: "replaced",
				},
			},
			want: map[string]any{
				"hookEventName":     "PostToolUse",
				"classifierContext": classifier,
				"updatedToolOutput": "replaced",
			},
		},
		{
			name: "UserPromptSubmit suppression and title",
			output: &types.HookOutput{
				HookSpecificOutput: &types.UserPromptSubmitHookSpecificOutput{
					HookEventName:          "UserPromptSubmit",
					SessionTitle:           &title,
					SuppressOriginalPrompt: &suppress,
				},
			},
			want: map[string]any{
				"hookEventName":          "UserPromptSubmit",
				"sessionTitle":           title,
				"suppressOriginalPrompt": true,
			},
		},
		{
			name: "UserPromptExpansion suppression",
			output: &types.HookOutput{
				HookSpecificOutput: &types.UserPromptExpansionHookSpecificOutput{
					HookEventName:          "UserPromptExpansion",
					SuppressOriginalPrompt: &suppress,
				},
			},
			want: map[string]any{
				"hookEventName":          "UserPromptExpansion",
				"suppressOriginalPrompt": true,
			},
		},
		{
			name: "PermissionDenied retry",
			output: &types.HookOutput{
				HookSpecificOutput: &types.PermissionDeniedHookSpecificOutput{
					HookEventName: "PermissionDenied",
					Retry:         &suppress,
				},
			},
			want: map[string]any{"hookEventName": "PermissionDenied", "retry": true},
		},
		{
			name: "SessionStart initial message",
			output: &types.HookOutput{
				HookSpecificOutput: &types.SessionStartHookSpecificOutput{
					HookEventName:      "SessionStart",
					InitialUserMessage: &classifier,
					WatchPaths:         []string{"/repo"},
					ReloadSkills:       &suppress,
				},
			},
			want: map[string]any{
				"hookEventName":      "SessionStart",
				"initialUserMessage": classifier,
				"reloadSkills":       true,
			},
		},
		{
			name: "additional-context output",
			output: &types.HookOutput{
				HookSpecificOutput: &types.ContextHookSpecificOutput{
					HookEventName:     "SubagentStop",
					AdditionalContext: &classifier,
				},
			},
			want: map[string]any{
				"hookEventName":     "SubagentStop",
				"additionalContext": classifier,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hookOutputToMap(tc.output)
			hso, ok := result["hookSpecificOutput"].(map[string]any)
			if !ok {
				t.Fatalf("expected hookSpecificOutput, got %v", result)
			}
			for key, want := range tc.want {
				if hso[key] != want {
					t.Errorf("hookSpecificOutput[%q] = %v, want %v", key, hso[key], want)
				}
			}
		})
	}
}

// A PermissionRequest hook answers the prompt outright, so its nested decision
// has to survive serialization.
func TestHookOutputToMapPermissionRequestDecision(t *testing.T) {
	result := hookOutputToMap(&types.HookOutput{
		HookSpecificOutput: &types.PermissionRequestHookSpecificOutput{
			HookEventName: "PermissionRequest",
			Decision: &types.PermissionDecision{
				Behavior:     types.PermissionBehaviorAllow,
				UpdatedInput: map[string]any{"command": "ls"},
			},
		},
	})

	hso, _ := result["hookSpecificOutput"].(map[string]any)
	decision, ok := hso["decision"].(map[string]any)
	if !ok {
		t.Fatalf("expected a decision, got %v", hso)
	}
	if decision["behavior"] != "allow" {
		t.Errorf("behavior = %v, want allow", decision["behavior"])
	}
	updated, ok := decision["updatedInput"].(map[string]any)
	if !ok || updated["command"] != "ls" {
		t.Errorf("updatedInput = %v", decision["updatedInput"])
	}
}

// Unset optional fields must stay off the wire: an older CLI handed a key it
// does not know, carrying an empty value, can behave differently than one that
// never saw the key.
func TestHookOutputToMapOmitsUnsetFields(t *testing.T) {
	result := hookOutputToMap(&types.HookOutput{
		HookSpecificOutput: &types.PostToolUseHookSpecificOutput{HookEventName: "PostToolUse"},
	})

	for _, key := range []string{"continue", "decision", "async", "terminalSequence"} {
		if _, ok := result[key]; ok {
			t.Errorf("unset field %q must be omitted", key)
		}
	}
	hso, _ := result["hookSpecificOutput"].(map[string]any)
	if len(hso) != 1 {
		t.Errorf("expected only hookEventName, got %v", hso)
	}
}

// mustBe asserts that v has type T, failing the test with the actual type
// rather than nil-dereferencing further down.
func mustBe[T any](t *testing.T, v any) T {
	t.Helper()
	typed, ok := v.(T)
	if !ok {
		t.Fatalf("unexpected type %T, want %T", v, typed)
	}
	return typed
}

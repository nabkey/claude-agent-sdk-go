package protocol

import (
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestParseHookInput_PostToolUseFailure(t *testing.T) {
	input := map[string]any{
		"hook_event_name": "PostToolUseFailure",
		"session_id":      "sess-1",
		"transcript_path": "/path/to/transcript",
		"cwd":             "/home/user",
		"tool_name":       "Bash",
		"tool_input":      "rm -rf /",
		"error":           "permission denied",
		"is_interrupt":    true,
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ptuf, ok := hookInput.(*types.PostToolUseFailureHookInput)
	if !ok {
		t.Fatalf("expected *types.PostToolUseFailureHookInput, got %T", hookInput)
	}

	if ptuf.ToolName != "Bash" {
		t.Errorf("expected tool_name Bash, got %s", ptuf.ToolName)
	}
	if ptuf.Error != "permission denied" {
		t.Errorf("expected error 'permission denied', got %s", ptuf.Error)
	}
	if !ptuf.IsInterrupt {
		t.Error("expected is_interrupt to be true")
	}
	if ptuf.GetHookEventName() != types.HookEventPostToolUseFailure {
		t.Errorf("expected hook event PostToolUseFailure, got %s", ptuf.GetHookEventName())
	}
}

func TestParseHookInput_SubagentStart(t *testing.T) {
	input := map[string]any{
		"hook_event_name": "SubagentStart",
		"session_id":      "sess-1",
		"transcript_path": "/path",
		"cwd":             "/home",
		"agent_id":        "agent-1",
		"agent_type":      "code-simplifier",
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ssi, ok := hookInput.(*types.SubagentStartHookInput)
	if !ok {
		t.Fatalf("expected *types.SubagentStartHookInput, got %T", hookInput)
	}

	if ssi.AgentID != "agent-1" {
		t.Errorf("expected agent_id agent-1, got %s", ssi.AgentID)
	}
	if ssi.AgentType != "code-simplifier" {
		t.Errorf("expected agent_type code-simplifier, got %s", ssi.AgentType)
	}
}

func TestParseHookInput_Notification(t *testing.T) {
	input := map[string]any{
		"hook_event_name":   "Notification",
		"session_id":        "sess-1",
		"transcript_path":   "/path",
		"cwd":               "/home",
		"message":           "Task completed",
		"title":             "Done",
		"notification_type": "info",
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ni, ok := hookInput.(*types.NotificationHookInput)
	if !ok {
		t.Fatalf("expected *types.NotificationHookInput, got %T", hookInput)
	}

	if ni.Message != "Task completed" {
		t.Errorf("expected message 'Task completed', got %s", ni.Message)
	}
	if ni.Title != "Done" {
		t.Errorf("expected title 'Done', got %s", ni.Title)
	}
}

func TestParseHookInput_PermissionRequest(t *testing.T) {
	input := map[string]any{
		"hook_event_name": "PermissionRequest",
		"session_id":      "sess-1",
		"transcript_path": "/path",
		"cwd":             "/home",
		"tool_name":       "Write",
		"tool_input":      map[string]any{"file": "test.go"},
		"permission_suggestions": []any{
			map[string]any{"type": "addRules"},
		},
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	pri, ok := hookInput.(*types.PermissionRequestHookInput)
	if !ok {
		t.Fatalf("expected *types.PermissionRequestHookInput, got %T", hookInput)
	}

	if pri.ToolName != "Write" {
		t.Errorf("expected tool_name Write, got %s", pri.ToolName)
	}
	if len(pri.PermissionSuggestions) != 1 {
		t.Errorf("expected 1 permission suggestion, got %d", len(pri.PermissionSuggestions))
	}
}

func TestParseHookInput_PreToolUseWithNewFields(t *testing.T) {
	input := map[string]any{
		"hook_event_name": "PreToolUse",
		"session_id":      "sess-1",
		"transcript_path": "/path",
		"cwd":             "/home",
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "ls"},
		"agent_id":        "agent-1",
		"agent_type":      "general",
		"tool_use_id":     "tu-123",
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ptu, ok := hookInput.(*types.PreToolUseHookInput)
	if !ok {
		t.Fatalf("expected *types.PreToolUseHookInput, got %T", hookInput)
	}

	if ptu.AgentID == nil || *ptu.AgentID != "agent-1" {
		t.Errorf("expected agent_id agent-1, got %v", ptu.AgentID)
	}
	if ptu.AgentType == nil || *ptu.AgentType != "general" {
		t.Errorf("expected agent_type general, got %v", ptu.AgentType)
	}
	if ptu.ToolUseID != "tu-123" {
		t.Errorf("expected tool_use_id tu-123, got %s", ptu.ToolUseID)
	}
}

func TestParseHookInput_SubagentStopWithNewFields(t *testing.T) {
	input := map[string]any{
		"hook_event_name":       "SubagentStop",
		"session_id":            "sess-1",
		"transcript_path":       "/path",
		"cwd":                   "/home",
		"stop_hook_active":      true,
		"agent_id":              "agent-2",
		"agent_transcript_path": "/agent/path",
		"agent_type":            "Explore",
	}

	hookInput, err := parseHookInput(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ssi, ok := hookInput.(*types.SubagentStopHookInput)
	if !ok {
		t.Fatalf("expected *types.SubagentStopHookInput, got %T", hookInput)
	}

	if ssi.AgentID != "agent-2" {
		t.Errorf("expected agent_id agent-2, got %s", ssi.AgentID)
	}
	if ssi.AgentTranscriptPath != "/agent/path" {
		t.Errorf("expected agent_transcript_path /agent/path, got %s", ssi.AgentTranscriptPath)
	}
	if ssi.AgentType != "Explore" {
		t.Errorf("expected agent_type Explore, got %s", ssi.AgentType)
	}
}

func TestHookOutputToMap_PreToolUseWithAdditionalContext(t *testing.T) {
	ctx := "Additional context for the tool"
	output := &types.HookOutput{
		HookSpecificOutput: &types.PreToolUseHookSpecificOutput{
			HookEventName:     "PreToolUse",
			AdditionalContext: &ctx,
		},
	}

	result := hookOutputToMap(output)
	hso, ok := result["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("expected hookSpecificOutput in result")
	}
	if hso["additionalContext"] != ctx {
		t.Errorf("expected additionalContext '%s', got %v", ctx, hso["additionalContext"])
	}
}

func TestHookOutputToMap_PostToolUseWithUpdatedMCPToolOutput(t *testing.T) {
	output := &types.HookOutput{
		HookSpecificOutput: &types.PostToolUseHookSpecificOutput{
			HookEventName:        "PostToolUse",
			UpdatedMCPToolOutput: map[string]any{"result": "modified"},
		},
	}

	result := hookOutputToMap(output)
	hso, ok := result["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatal("expected hookSpecificOutput in result")
	}
	if hso["updatedMcpToolOutput"] == nil {
		t.Error("expected updatedMcpToolOutput to be set")
	}
}

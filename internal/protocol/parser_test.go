package protocol

import (
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestParseRateLimitEvent(t *testing.T) {
	data := map[string]any{
		"type":            "rate_limit_event",
		"status":          "allowed_warning",
		"resets_at":       "2025-01-01T00:00:00Z",
		"rate_limit_type": "five_hour",
		"utilization":     0.85,
		"uuid":            "test-uuid",
		"session_id":      "test-session",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rle, ok := msg.(*types.RateLimitEvent)
	if !ok {
		t.Fatalf("expected *types.RateLimitEvent, got %T", msg)
	}

	if rle.Status != types.RateLimitStatusAllowedWarning {
		t.Errorf("expected status allowed_warning, got %s", rle.Status)
	}
	if rle.UUID != "test-uuid" {
		t.Errorf("expected uuid test-uuid, got %s", rle.UUID)
	}
	if rle.ResetsAt == nil || *rle.ResetsAt != "2025-01-01T00:00:00Z" {
		t.Errorf("unexpected resets_at: %v", rle.ResetsAt)
	}
	if rle.RateLimitType == nil || *rle.RateLimitType != types.RateLimitTypeFiveHour {
		t.Errorf("unexpected rate_limit_type: %v", rle.RateLimitType)
	}
	if rle.Utilization == nil || *rle.Utilization != 0.85 {
		t.Errorf("unexpected utilization: %v", rle.Utilization)
	}
}

func TestParseTaskStartedMessage(t *testing.T) {
	data := map[string]any{
		"type":        "system",
		"subtype":     "task_started",
		"task_id":     "task-123",
		"description": "Running tests",
		"uuid":        "uuid-456",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tsm, ok := msg.(*types.TaskStartedMessage)
	if !ok {
		t.Fatalf("expected *types.TaskStartedMessage, got %T", msg)
	}

	if tsm.TaskID != "task-123" {
		t.Errorf("expected task_id task-123, got %s", tsm.TaskID)
	}
	if tsm.Description != "Running tests" {
		t.Errorf("expected description 'Running tests', got %s", tsm.Description)
	}
	if tsm.UUID != "uuid-456" {
		t.Errorf("expected uuid uuid-456, got %s", tsm.UUID)
	}
	if tsm.Subtype != "task_started" {
		t.Errorf("expected subtype task_started, got %s", tsm.Subtype)
	}
}

func TestParseTaskProgressMessage(t *testing.T) {
	data := map[string]any{
		"type":    "system",
		"subtype": "task_progress",
		"task_id": "task-123",
		"usage": map[string]any{
			"input_tokens":                float64(100),
			"output_tokens":               float64(50),
			"cache_creation_input_tokens": float64(10),
			"cache_read_input_tokens":     float64(5),
		},
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tpm, ok := msg.(*types.TaskProgressMessage)
	if !ok {
		t.Fatalf("expected *types.TaskProgressMessage, got %T", msg)
	}

	if tpm.TaskID != "task-123" {
		t.Errorf("expected task_id task-123, got %s", tpm.TaskID)
	}
	if tpm.Usage.InputTokens != 100 {
		t.Errorf("expected input_tokens 100, got %d", tpm.Usage.InputTokens)
	}
	if tpm.Usage.OutputTokens != 50 {
		t.Errorf("expected output_tokens 50, got %d", tpm.Usage.OutputTokens)
	}
}

func TestParseTaskNotificationMessage(t *testing.T) {
	data := map[string]any{
		"type":        "system",
		"subtype":     "task_notification",
		"task_id":     "task-123",
		"status":      "completed",
		"tool_use_id": "tool-456",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tnm, ok := msg.(*types.TaskNotificationMessage)
	if !ok {
		t.Fatalf("expected *types.TaskNotificationMessage, got %T", msg)
	}

	if tnm.TaskID != "task-123" {
		t.Errorf("expected task_id task-123, got %s", tnm.TaskID)
	}
	if tnm.Status != types.TaskNotificationStatusCompleted {
		t.Errorf("expected status completed, got %s", tnm.Status)
	}
	if tnm.ToolUseID != "tool-456" {
		t.Errorf("expected tool_use_id tool-456, got %s", tnm.ToolUseID)
	}
}

func TestForwardCompatibleParsing(t *testing.T) {
	// Unknown message type should return nil, nil (not an error)
	data := map[string]any{
		"type": "future_message_type",
		"data": "some data",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("expected no error for unknown type, got: %v", err)
	}
	if msg != nil {
		t.Fatalf("expected nil message for unknown type, got: %T", msg)
	}
}

func TestParseUserMessageWithNewFields(t *testing.T) {
	data := map[string]any{
		"type": "user",
		"uuid": "user-uuid-123",
		"tool_use_result": map[string]any{
			"tool_use_id": "tool-1",
			"output":      "result text",
		},
		"message": map[string]any{
			"role":    "user",
			"content": "hello",
		},
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	um, ok := msg.(*types.UserMessage)
	if !ok {
		t.Fatalf("expected *types.UserMessage, got %T", msg)
	}

	if um.UUID == nil || *um.UUID != "user-uuid-123" {
		t.Errorf("expected uuid user-uuid-123, got %v", um.UUID)
	}
	if um.ToolUseResult == nil {
		t.Error("expected tool_use_result to be set")
	}
}

func TestParseAssistantMessageWithUsage(t *testing.T) {
	data := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":  "assistant",
			"model": "claude-sonnet-4-5",
			"usage": map[string]any{
				"input_tokens":  float64(100),
				"output_tokens": float64(50),
			},
			"content": []any{
				map[string]any{
					"type": "text",
					"text": "Hello!",
				},
			},
		},
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	am, ok := msg.(*types.AssistantMessage)
	if !ok {
		t.Fatalf("expected *types.AssistantMessage, got %T", msg)
	}

	if am.Usage == nil {
		t.Error("expected usage to be set")
	}
	if am.Usage["input_tokens"] != float64(100) {
		t.Errorf("expected input_tokens 100, got %v", am.Usage["input_tokens"])
	}
}

func TestParseResultMessageWithStopReason(t *testing.T) {
	data := map[string]any{
		"type":        "result",
		"subtype":     "result",
		"session_id":  "session-1",
		"stop_reason": "end_turn",
		"is_error":    false,
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rm, ok := msg.(*types.ResultMessage)
	if !ok {
		t.Fatalf("expected *types.ResultMessage, got %T", msg)
	}

	if rm.StopReason == nil || *rm.StopReason != "end_turn" {
		t.Errorf("expected stop_reason end_turn, got %v", rm.StopReason)
	}
}

func TestParseRegularSystemMessage(t *testing.T) {
	data := map[string]any{
		"type":    "system",
		"subtype": "init",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sm, ok := msg.(*types.SystemMessage)
	if !ok {
		t.Fatalf("expected *types.SystemMessage, got %T", msg)
	}

	if sm.Subtype != "init" {
		t.Errorf("expected subtype init, got %s", sm.Subtype)
	}
}

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
		"session_id":  "session-789",
		"tool_use_id": "tool-abc",
		"task_type":   "background",
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
	if tsm.SessionID != "session-789" {
		t.Errorf("expected session_id session-789, got %s", tsm.SessionID)
	}
	if tsm.ToolUseID != "tool-abc" {
		t.Errorf("expected tool_use_id tool-abc, got %s", tsm.ToolUseID)
	}
	if tsm.TaskType == nil || *tsm.TaskType != "background" {
		t.Errorf("expected task_type background, got %v", tsm.TaskType)
	}
}

func TestParseTaskProgressMessage(t *testing.T) {
	data := map[string]any{
		"type":           "system",
		"subtype":        "task_progress",
		"task_id":        "task-123",
		"description":    "Processing files",
		"uuid":           "uuid-prog",
		"session_id":     "session-prog",
		"tool_use_id":    "tool-prog",
		"last_tool_name": "Bash",
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
	if tpm.Description != "Processing files" {
		t.Errorf("expected description 'Processing files', got %s", tpm.Description)
	}
	if tpm.UUID != "uuid-prog" {
		t.Errorf("expected uuid uuid-prog, got %s", tpm.UUID)
	}
	if tpm.SessionID != "session-prog" {
		t.Errorf("expected session_id session-prog, got %s", tpm.SessionID)
	}
	if tpm.LastToolName == nil || *tpm.LastToolName != "Bash" {
		t.Errorf("expected last_tool_name Bash, got %v", tpm.LastToolName)
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
		"output_file": "/tmp/output.txt",
		"summary":     "Task finished successfully",
		"uuid":        "uuid-notif",
		"session_id":  "session-notif",
		"tool_use_id": "tool-456",
		"usage": map[string]any{
			"input_tokens":                float64(200),
			"output_tokens":               float64(100),
			"cache_creation_input_tokens": float64(20),
			"cache_read_input_tokens":     float64(10),
		},
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
	if tnm.OutputFile != "/tmp/output.txt" {
		t.Errorf("expected output_file /tmp/output.txt, got %s", tnm.OutputFile)
	}
	if tnm.Summary != "Task finished successfully" {
		t.Errorf("expected summary 'Task finished successfully', got %s", tnm.Summary)
	}
	if tnm.UUID != "uuid-notif" {
		t.Errorf("expected uuid uuid-notif, got %s", tnm.UUID)
	}
	if tnm.SessionID != "session-notif" {
		t.Errorf("expected session_id session-notif, got %s", tnm.SessionID)
	}
	if tnm.ToolUseID != "tool-456" {
		t.Errorf("expected tool_use_id tool-456, got %s", tnm.ToolUseID)
	}
	if tnm.Usage == nil {
		t.Fatal("expected usage to be set")
	}
	if tnm.Usage.InputTokens != 200 {
		t.Errorf("expected input_tokens 200, got %d", tnm.Usage.InputTokens)
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

func TestParseRateLimitEventWithOverage(t *testing.T) {
	data := map[string]any{
		"type":                    "rate_limit_event",
		"status":                  "rejected",
		"resets_at":               "2025-06-01T00:00:00Z",
		"rate_limit_type":         "seven_day_opus",
		"utilization":             1.0,
		"overage_status":          "allowed_warning",
		"overage_resets_at":       float64(1717200000),
		"overage_disabled_reason": "billing_limit_reached",
		"uuid":                    "test-uuid-2",
		"session_id":              "test-session-2",
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rle, ok := msg.(*types.RateLimitEvent)
	if !ok {
		t.Fatalf("expected *types.RateLimitEvent, got %T", msg)
	}

	if rle.Status != types.RateLimitStatusRejected {
		t.Errorf("expected status rejected, got %s", rle.Status)
	}
	if rle.RateLimitType == nil || *rle.RateLimitType != types.RateLimitTypeSevenDayOpus {
		t.Errorf("expected rate_limit_type seven_day_opus, got %v", rle.RateLimitType)
	}
	if rle.OverageStatus == nil || *rle.OverageStatus != types.RateLimitStatusAllowedWarning {
		t.Errorf("expected overage_status allowed_warning, got %v", rle.OverageStatus)
	}
	if rle.OverageResetsAt == nil || *rle.OverageResetsAt != 1717200000 {
		t.Errorf("expected overage_resets_at 1717200000, got %v", rle.OverageResetsAt)
	}
	if rle.OverageDisabledReason == nil || *rle.OverageDisabledReason != "billing_limit_reached" {
		t.Errorf("expected overage_disabled_reason billing_limit_reached, got %v", rle.OverageDisabledReason)
	}
	if rle.Raw == nil {
		t.Error("expected raw to be set")
	}
}

func TestParseRateLimitEventNested(t *testing.T) {
	// Test the nested rate_limit_info format (as used by Python SDK parser)
	data := map[string]any{
		"type":       "rate_limit_event",
		"uuid":       "nested-uuid",
		"session_id": "nested-session",
		"rate_limit_info": map[string]any{
			"status":        "allowed_warning",
			"resetsAt":      "2025-06-01T12:00:00Z",
			"rateLimitType": "overage",
			"utilization":   0.75,
			"overageStatus": "allowed",
		},
	}

	msg, err := ParseMessage(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	rle, ok := msg.(*types.RateLimitEvent)
	if !ok {
		t.Fatalf("expected *types.RateLimitEvent, got %T", msg)
	}

	if rle.UUID != "nested-uuid" {
		t.Errorf("expected uuid nested-uuid, got %s", rle.UUID)
	}
	if rle.Status != types.RateLimitStatusAllowedWarning {
		t.Errorf("expected status allowed_warning, got %s", rle.Status)
	}
	if rle.RateLimitType == nil || *rle.RateLimitType != types.RateLimitTypeOverage {
		t.Errorf("expected rate_limit_type overage, got %v", rle.RateLimitType)
	}
	if rle.ResetsAt == nil || *rle.ResetsAt != "2025-06-01T12:00:00Z" {
		t.Errorf("expected resets_at from nested camelCase, got %v", rle.ResetsAt)
	}
	if rle.OverageStatus == nil || *rle.OverageStatus != types.RateLimitStatusAllowed {
		t.Errorf("expected overage_status allowed, got %v", rle.OverageStatus)
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

package protocol

import (
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// A reset clears the transcript and zeroes the running totals on later
// results, so a consumer accumulating them has to see the frame.
func TestParseConversationReset(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":                "conversation_reset",
		"new_conversation_id": "conv-new",
		"uuid":                "uuid-1",
		"session_id":          "session-old",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	reset, ok := msg.(*types.ConversationResetMessage)
	if !ok {
		t.Fatalf("expected *types.ConversationResetMessage, got %T", msg)
	}
	if reset.NewConversationID != "conv-new" {
		t.Errorf("NewConversationID = %q, want conv-new", reset.NewConversationID)
	}
	if reset.SessionID != "session-old" {
		t.Errorf("SessionID = %q, want the outgoing session", reset.SessionID)
	}
}

// Origin is what lets a streaming-input consumer tell its own turns from ones
// the session injected.
func TestParseUserMessageOrigin(t *testing.T) {
	tests := []struct {
		name    string
		origin  any
		want    types.MessageOriginKind
		human   bool
		checkFn func(*testing.T, *types.MessageOrigin)
	}{
		{
			name:   "human",
			origin: map[string]any{"kind": "human"},
			want:   types.MessageOriginKindHuman,
			human:  true,
		},
		{
			name:   "channel carries the server",
			origin: map[string]any{"kind": "channel", "server": "slack"},
			want:   types.MessageOriginKindChannel,
			checkFn: func(t *testing.T, o *types.MessageOrigin) {
				if o.Server != "slack" {
					t.Errorf("Server = %q, want slack", o.Server)
				}
			},
		},
		{
			name: "peer carries sender fields",
			origin: map[string]any{
				"kind":            "peer",
				"from":            "agent-a",
				"fromMode":        "bypass",
				"name":            "Agent A",
				"fromSession":     "local_123",
				"senderTaskId":    "task-9",
				"body":            "hello",
				"verifiedPeerPid": float64(4242),
			},
			want: types.MessageOriginKindPeer,
			checkFn: func(t *testing.T, o *types.MessageOrigin) {
				if o.From != "agent-a" || o.Name != "Agent A" || o.Body != "hello" {
					t.Errorf("unexpected peer fields: %+v", o)
				}
				if o.FromMode != types.PeerOriginModeBypass {
					t.Errorf("FromMode = %q, want bypass", o.FromMode)
				}
				if o.VerifiedPeerPID == nil || *o.VerifiedPeerPID != 4242 {
					t.Errorf("VerifiedPeerPID = %v, want 4242", o.VerifiedPeerPID)
				}
			},
		},
		{
			name:   "task notification subkind",
			origin: map[string]any{"kind": "task-notification", "subkind": "scheduled-trigger"},
			want:   types.MessageOriginKindTaskNotification,
			checkFn: func(t *testing.T, o *types.MessageOrigin) {
				if o.Subkind != types.TaskNotificationOriginScheduledTrigger {
					t.Errorf("Subkind = %q, want scheduled-trigger", o.Subkind)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			msg, err := ParseMessage(map[string]any{
				"type":       "user",
				"session_id": "s1",
				"origin":     tc.origin,
				"message":    map[string]any{"role": "user", "content": "hi"},
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			user := mustBe[*types.UserMessage](t, msg)
			if user.Origin == nil {
				t.Fatal("expected an origin")
			}
			if user.Origin.Kind != tc.want {
				t.Errorf("Kind = %q, want %q", user.Origin.Kind, tc.want)
			}
			if user.Origin.IsHuman() != tc.human {
				t.Errorf("IsHuman() = %v, want %v", user.Origin.IsHuman(), tc.human)
			}
			if user.SessionID != "s1" {
				t.Errorf("SessionID = %q, want s1", user.SessionID)
			}
			if tc.checkFn != nil {
				tc.checkFn(t, user.Origin)
			}
		})
	}
}

// An absent origin means the CLI did not attribute the message, which is not
// the same as attributing it to a human.
func TestParseUserMessageWithoutOrigin(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": "hi"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	user := mustBe[*types.UserMessage](t, msg)
	if user.Origin != nil {
		t.Errorf("expected no origin, got %+v", user.Origin)
	}
	if user.Origin.IsHuman() {
		t.Error("a nil origin must not report as human")
	}
}

func TestParseResultMessageSyncFields(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":               "result",
		"subtype":            "success",
		"queued_turn_count":  float64(2),
		"user_message_uuid":  "u-2",
		"user_message_uuids": []any{"u-1", "u-2"},
		"origin":             map[string]any{"kind": "task-notification"},
		"modelUsage": map[string]any{
			"claude-opus-5": map[string]any{
				"inputTokens":    float64(10),
				"outputTokens":   float64(20),
				"thinkingTokens": float64(7),
				"costUSD":        1.5,
				"costBasis":      "managed",
			},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result := mustBe[*types.ResultMessage](t, msg)
	if result.QueuedTurnCount == nil || *result.QueuedTurnCount != 2 {
		t.Errorf("QueuedTurnCount = %v, want 2", result.QueuedTurnCount)
	}
	if result.UserMessageUUID != "u-2" {
		t.Errorf("UserMessageUUID = %q, want u-2", result.UserMessageUUID)
	}
	if len(result.UserMessageUUIDs) != 2 {
		t.Errorf("UserMessageUUIDs = %v, want two entries", result.UserMessageUUIDs)
	}
	if result.Origin == nil || result.Origin.Kind != types.MessageOriginKindTaskNotification {
		t.Errorf("unexpected origin: %+v", result.Origin)
	}

	usage := result.ModelUsage["claude-opus-5"]
	if usage.ThinkingTokens != 7 {
		t.Errorf("ThinkingTokens = %d, want 7", usage.ThinkingTokens)
	}
	if usage.CostBasis != types.CostBasisManaged {
		t.Errorf("CostBasis = %q, want managed", usage.CostBasis)
	}
}

// queued_turn_count is absent on fatal startup results and on surfaces with no
// command queue, which is distinct from a queue that happens to be empty.
func TestParseResultMessageWithoutQueuedTurnCount(t *testing.T) {
	msg, err := ParseMessage(map[string]any{"type": "result", "subtype": "success"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := mustBe[*types.ResultMessage](t, msg).QueuedTurnCount; got != nil {
		t.Errorf("QueuedTurnCount = %v, want nil when the CLI omits it", got)
	}
}

func TestParseAssistantMessageContextUsage(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":               "assistant",
		"user_message_uuid":  "u-1",
		"user_message_uuids": []any{"u-1"},
		"context_usage": map[string]any{
			"model":          "claude-opus-5",
			"total_tokens":   float64(120000),
			"raw_max_tokens": float64(200000),
			"percentage":     float64(60),
			"over_limit":     map[string]any{"tokens_over": float64(5), "kind": "hard_limit"},
			"categories":     []any{map[string]any{"name": "System prompt", "tokens": float64(1000)}},
			"mcp_tools": []any{
				map[string]any{"name": "mcp__linear__create", "server_name": "linear", "tokens": float64(50)},
			},
			"memory_files": []any{map[string]any{"path": "CLAUDE.md", "type": "Project", "tokens": float64(30)}},
			"agents":       []any{map[string]any{"agent_type": "Explore", "source": "plugin", "tokens": float64(20)}},
			"skills":       []any{map[string]any{"name": "pdf", "source": "userSettings", "tokens": float64(10)}},
		},
		"message": map[string]any{"model": "claude-opus-5", "content": []any{}},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	assistant := mustBe[*types.AssistantMessage](t, msg)
	if assistant.UserMessageUUID != "u-1" {
		t.Errorf("UserMessageUUID = %q, want u-1", assistant.UserMessageUUID)
	}
	usage := assistant.ContextUsage
	if usage == nil {
		t.Fatal("expected a context usage payload")
	}
	if usage.TotalTokens != 120000 || usage.RawMaxTokens != 200000 {
		t.Errorf("unexpected token counts: %+v", usage)
	}
	if usage.OverLimit == nil || usage.OverLimit.Kind != "hard_limit" {
		t.Errorf("unexpected over limit: %+v", usage.OverLimit)
	}
	if len(usage.Categories) != 1 || len(usage.MCPTools) != 1 ||
		len(usage.MemoryFiles) != 1 || len(usage.Agents) != 1 || len(usage.Skills) != 1 {
		t.Errorf("expected one entry in every breakdown, got %+v", usage)
	}
	if usage.MCPTools[0].ServerName != "linear" {
		t.Errorf("MCPTools[0].ServerName = %q, want linear", usage.MCPTools[0].ServerName)
	}
}

func TestParseTaskStartedSyncFields(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":            "system",
		"subtype":         "task_started",
		"task_id":         "t-1",
		"description":     "Reviewing",
		"subagent_type":   "code-reviewer",
		"is_backgrounded": true,
		"spawn_depth":     float64(2),
		"workflow_name":   "review",
		"prompt":          "review the diff",
		"skip_transcript": true,
		"ambient":         true,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	task := mustBe[*types.TaskStartedMessage](t, msg)
	if !task.IsBackgrounded || task.SpawnDepth != 2 || !task.Ambient || !task.SkipTranscript {
		t.Errorf("unexpected task flags: %+v", task)
	}
	if task.SubagentType != "code-reviewer" || task.WorkflowName != "review" {
		t.Errorf("unexpected task identity: %+v", task)
	}
}

func TestParseTaskNotificationResourceLinks(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":        "system",
		"subtype":     "task_notification",
		"task_id":     "t-1",
		"status":      "completed",
		"tool_use_id": "tool-1",
		"ambient":     true,
		"resource_links": []any{
			map[string]any{
				"uri": "file:///tmp/out.csv", "name": "out.csv", "mimeType": "text/csv",
			},
			"not an object",
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	notification := mustBe[*types.TaskNotificationMessage](t, msg)
	if !notification.Ambient {
		t.Error("expected Ambient to be set")
	}
	if len(notification.ResourceLinks) != 1 {
		t.Fatalf("expected the one well-formed link, got %d", len(notification.ResourceLinks))
	}
	if notification.ResourceLinks[0].URI != "file:///tmp/out.csv" {
		t.Errorf("unexpected link: %+v", notification.ResourceLinks[0])
	}
}

func TestParseBackgroundTasksChanged(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":    "system",
		"subtype": "background_tasks_changed",
		"tasks": []any{
			map[string]any{"task_id": "t-1", "task_type": "agent", "description": "work"},
			map[string]any{"task_id": "t-2", "ambient": true},
		},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tasks := mustBe[*types.BackgroundTasksChangedMessage](t, msg).Tasks
	if len(tasks) != 2 {
		t.Fatalf("expected two tasks, got %d", len(tasks))
	}
	if tasks[0].TaskType != "agent" || tasks[0].Ambient {
		t.Errorf("unexpected first task: %+v", tasks[0])
	}
	if !tasks[1].Ambient {
		t.Error("expected the second task to be ambient")
	}
	if tasks[1].Raw == nil {
		t.Error("expected the raw entry to be retained")
	}
}

func TestParseThinkingTokensMessage(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":                   "system",
		"subtype":                "thinking_tokens",
		"estimated_tokens":       float64(500),
		"estimated_tokens_delta": float64(120),
		"user_message_uuid":      "u-1",
		"session_id":             "s-1",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	thinking, ok := msg.(*types.ThinkingTokensMessage)
	if !ok {
		t.Fatalf("expected *types.ThinkingTokensMessage, got %T", msg)
	}
	if thinking.EstimatedTokens != 500 || thinking.EstimatedTokensDelta != 120 {
		t.Errorf("unexpected counts: %+v", thinking)
	}
	if thinking.UserMessageUUID != "u-1" {
		t.Errorf("UserMessageUUID = %q, want u-1", thinking.UserMessageUUID)
	}
}

func TestParseStreamEventUserMessageUUIDs(t *testing.T) {
	msg, err := ParseMessage(map[string]any{
		"type":               "stream_event",
		"uuid":               "e-1",
		"session_id":         "s-1",
		"event":              map[string]any{"type": "content_block_delta"},
		"user_message_uuid":  "u-2",
		"user_message_uuids": []any{"u-1", "u-2", 42},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	event := mustBe[*types.StreamEvent](t, msg)
	if event.UserMessageUUID != "u-2" {
		t.Errorf("UserMessageUUID = %q, want u-2", event.UserMessageUUID)
	}
	// The non-string entry is dropped rather than failing the parse.
	if len(event.UserMessageUUIDs) != 2 {
		t.Errorf("UserMessageUUIDs = %v, want the two strings", event.UserMessageUUIDs)
	}
}

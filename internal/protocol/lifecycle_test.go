package protocol

import (
	"context"
	stderrors "errors"
	"strings"
	"testing"
	"time"

	sdkerrors "github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// A result frame ends one turn, not the run. Background agent work keeps
// running past it and still needs stdin for hook and SDK-MCP responses, so
// stdin must stay open while any is in flight.
func TestStdinStaysOpenWhileAgentTasksAreInFlight(t *testing.T) {
	ft := newFakeTransport()
	ft.respond = func(string, map[string]any) map[string]any { return map[string]any{} }

	// Hooks are what make the control channel necessary after a result.
	matcher := types.HookMatcher{Hooks: []types.HookCallback{
		func(context.Context, types.HookInput, *string, *types.HookContext) (*types.HookOutput, error) {
			return &types.HookOutput{}, nil
		},
	}}
	q := NewQuery(&QueryOptions{
		Transport:       ft,
		IsStreamingMode: true,
		Hooks:           map[types.HookEvent][]types.HookMatcher{types.HookEventPreToolUse: {matcher}},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	// A delegated agent task starts, then a turn ends.
	ft.msgChan <- map[string]any{
		"type": "system", "subtype": "task_started",
		"task_id": "t1", "task_type": "local_agent",
	}
	ft.msgChan <- map[string]any{"type": "result", "subtype": "success"}

	drain(t, ctx, q, 2)

	// The run-ending signal must not have fired yet.
	select {
	case <-q.firstResultEvent:
		t.Fatal("stdin was released while an agent task was still in flight")
	default:
	}

	// The task finishes, and the next result ends the run.
	ft.msgChan <- map[string]any{
		"type": "system", "subtype": "task_notification",
		"task_id": "t1", "status": "completed",
	}
	ft.msgChan <- map[string]any{"type": "result", "subtype": "success"}

	drain(t, ctx, q, 2)

	select {
	case <-q.firstResultEvent:
	case <-time.After(time.Second):
		t.Fatal("stdin was never released after the task finished")
	}
}

// A background shell can run indefinitely, so tracking it would withhold the
// stdin close forever rather than briefly.
func TestNonDeferringTaskTypesAreNotTracked(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	ft.msgChan <- map[string]any{
		"type": "system", "subtype": "task_started",
		"task_id": "shell-1", "task_type": "background_shell",
	}
	ft.msgChan <- map[string]any{"type": "result", "subtype": "success"}

	drain(t, ctx, q, 2)

	select {
	case <-q.firstResultEvent:
	case <-time.After(time.Second):
		t.Fatal("a background shell must not defer the stdin close")
	}
}

// Terminal completion can arrive only as a task_updated patch, with no
// accompanying notification.
func TestTaskUpdatedTerminalStatusClearsTask(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	ft.msgChan <- map[string]any{
		"type": "system", "subtype": "task_started",
		"task_id": "t1", "task_type": "local_workflow",
	}
	ft.msgChan <- map[string]any{
		"type": "system", "subtype": "task_updated",
		"task_id": "t1", "patch": map[string]any{"status": "killed"},
	}

	drain(t, ctx, q, 2)

	q.taskMu.Lock()
	inflight := len(q.inflightTasks)
	q.taskMu.Unlock()

	if inflight != 0 {
		t.Errorf("a terminal task_updated must clear the task, %d still in flight", inflight)
	}
}

// When the CLI emits an error result it then exits non-zero on purpose. The
// bare "exit code 1" carries no information, so the structured error the CLI
// already reported is substituted.
func TestProcessErrorIsReplacedWithResultErrors(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	ft.msgChan <- map[string]any{
		"type":     "result",
		"subtype":  "error_during_execution",
		"is_error": true,
		"errors":   []any{"tool failed", "and again"},
	}
	drain(t, ctx, q, 1)

	ft.errChan <- sdkerrors.NewProcessError("Command failed with exit code 1", 1, "")

	waitFor(t, ctx, func() bool { return q.Err() != nil })

	err := q.Err()
	if err == nil {
		t.Fatal("expected a terminal error")
	}
	if got := err.Error(); !strings.Contains(got, "claude code returned an error result: tool failed; and again") {
		t.Errorf("unexpected error text: %s", got)
	}

	// The replacement is structured, so callers can branch on the reason
	// without matching on strings.
	var resultErr *sdkerrors.ResultError
	if !stderrors.As(err, &resultErr) {
		t.Fatalf("expected a *ResultError, got %T", err)
	}
	if resultErr.Subtype != "error_during_execution" {
		t.Errorf("Subtype = %q, want error_during_execution", resultErr.Subtype)
	}
	if len(resultErr.Errors) != 2 || resultErr.Errors[0] != "tool failed" {
		t.Errorf("Errors = %v, want the two reported strings", resultErr.Errors)
	}
	if resultErr.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", resultErr.ExitCode)
	}

	// It embeds ProcessError, so handlers written against that keep matching.
	var processErr *sdkerrors.ProcessError
	if !stderrors.As(err, &processErr) {
		t.Error("a ResultError must still match *ProcessError")
	}
}

// A crash unrelated to a prior error result must surface as itself.
func TestProcessErrorSurvivesWhenConversationMovedOn(t *testing.T) {
	ft := newFakeTransport()
	q, ctx, cleanup := newTestQuery(t, ft)
	defer cleanup()

	ft.msgChan <- map[string]any{
		"type": "result", "subtype": "error_during_execution",
		"is_error": true, "errors": []any{"old failure"},
	}
	drain(t, ctx, q, 1)

	// An assistant turn means the conversation moved on.
	ft.msgChan <- map[string]any{
		"type":    "assistant",
		"message": map[string]any{"model": "m", "content": []any{}},
	}
	drain(t, ctx, q, 1)

	ft.errChan <- sdkerrors.NewProcessError("Command failed with exit code 1", 1, "")
	waitFor(t, ctx, func() bool { return q.Err() != nil })

	if got := q.Err().Error(); strings.Contains(got, "old failure") {
		t.Error("a stale error result must not mask a fresh crash")
	}
}

// The CLI abandons a cancelled request, so writing a response would
// desynchronize the channel.
func TestControlCancelRequestSuppressesResponse(t *testing.T) {
	ft := newFakeTransport()

	started := make(chan struct{})
	finished := make(chan struct{})

	q := NewQuery(&QueryOptions{
		Transport:       ft,
		IsStreamingMode: true,
		CanUseTool: func(ctx context.Context, _ string, _ map[string]any,
			_ types.ToolPermissionContext) (types.PermissionResult, error) {
			close(started)
			// Block until cancelled, so the handler is unambiguously still
			// running when the cancel frame arrives.
			<-ctx.Done()
			close(finished)
			return &types.PermissionResultAllow{}, nil
		},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	q.Start(ctx)
	defer func() { _ = q.Close() }()

	ft.msgChan <- map[string]any{
		"type": "control_request", "request_id": "req-1",
		"request": map[string]any{
			"subtype": "can_use_tool", "tool_name": "Bash",
			"input": map[string]any{},
		},
	}

	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("the permission callback never started")
	}

	ft.msgChan <- map[string]any{"type": "control_cancel_request", "request_id": "req-1"}

	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal("the cancel never reached the handler's context")
	}

	// Give any (incorrect) write a chance to land before asserting.
	time.Sleep(50 * time.Millisecond)

	for _, resp := range ft.responses() {
		if resp["request_id"] == "req-1" {
			t.Errorf("a cancelled request must not be answered: %v", resp)
		}
	}
}

// drain consumes n messages, failing if they do not arrive.
func drain(t *testing.T, ctx context.Context, q *Query, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-q.ReceiveMessages():
		case <-ctx.Done():
			t.Fatalf("timed out draining message %d of %d", i+1, n)
		}
	}
}

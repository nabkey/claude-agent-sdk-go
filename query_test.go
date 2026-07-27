package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// scriptedTransport is a Transport that answers control requests from a script
// and can push arbitrary frames into the stream.
//
// This is what makes the streaming machinery testable without a real CLI
// binary, which the SDK previously had no way to do.
type scriptedTransport struct {
	mu      sync.Mutex
	written []string
	closed  bool

	msgChan chan map[string]any
	errChan chan error

	// onRequest answers an outgoing control request. Returning nil sends an
	// empty success payload.
	onRequest func(subtype string, request map[string]any) map[string]any
}

func newScriptedTransport() *scriptedTransport {
	return &scriptedTransport{
		msgChan: make(chan map[string]any, 64),
		errChan: make(chan error, 1),
	}
}

func (s *scriptedTransport) Connect(context.Context) error { return nil }
func (s *scriptedTransport) IsReady() bool                 { return true }
func (s *scriptedTransport) EndInput() error               { return nil }

func (s *scriptedTransport) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		close(s.msgChan)
	}
	return nil
}

func (s *scriptedTransport) ReadMessages(context.Context) (<-chan map[string]any, <-chan error) {
	return s.msgChan, s.errChan
}

func (s *scriptedTransport) Write(_ context.Context, data string) error {
	s.mu.Lock()
	s.written = append(s.written, data)
	closed := s.closed
	respond := s.onRequest
	s.mu.Unlock()

	if closed {
		return nil
	}

	var frame map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &frame); err != nil {
		return nil
	}
	if frame["type"] != "control_request" {
		return nil
	}

	requestID, _ := frame["request_id"].(string)
	request, _ := frame["request"].(map[string]any)
	subtype, _ := request["subtype"].(string)

	var payload map[string]any
	if respond != nil {
		payload = respond(subtype, request)
	}
	if payload == nil {
		payload = map[string]any{}
	}

	s.push(map[string]any{
		"type": "control_response",
		"response": map[string]any{
			"subtype":    "success",
			"request_id": requestID,
			"response":   payload,
		},
	})
	return nil
}

// push queues a frame for the read loop, ignoring a closed transport.
func (s *scriptedTransport) push(frame map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	select {
	case s.msgChan <- frame:
	default:
	}
}

// sentUserMessages returns the user turns written to the transport.
func (s *scriptedTransport) sentUserMessages() []map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []map[string]any
	for _, raw := range s.written {
		var frame map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &frame); err != nil {
			continue
		}
		if frame["type"] == "user" {
			out = append(out, frame)
		}
	}
	return out
}

func assistantFrame(text string) map[string]any {
	return map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"model":   "claude-sonnet-5",
			"content": []any{map[string]any{"type": "text", "text": text}},
		},
	}
}

func resultFrame() map[string]any {
	return map[string]any{
		"type":            "result",
		"subtype":         "success",
		"is_error":        false,
		"duration_ms":     float64(10),
		"duration_api_ms": float64(8),
		"num_turns":       float64(1),
		"session_id":      "sess-1",
		"result":          "done",
	}
}

// Query now runs the CLI in streaming mode, so the prompt is written to stdin
// after the initialize handshake rather than baked into argv.
func TestQueryStreamsPromptAfterInitialize(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	trans := newScriptedTransport()
	trans.onRequest = func(subtype string, _ map[string]any) map[string]any {
		if subtype == "initialize" {
			// Only once initialize is answered can the prompt be sent.
			go func() {
				trans.push(assistantFrame("hello"))
				trans.push(resultFrame())
				_ = trans.Close()
			}()
		}
		return nil
	}

	var texts []string
	for msg := range QueryWithTransport(ctx, "hi there", nil, trans) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if tb, ok := block.(*types.TextBlock); ok {
					texts = append(texts, tb.Text)
				}
			}
		case error:
			t.Fatalf("unexpected error: %v", m)
		}
	}

	if len(texts) != 1 || texts[0] != "hello" {
		t.Errorf("assistant text = %v", texts)
	}

	sent := trans.sentUserMessages()
	if len(sent) != 1 {
		t.Fatalf("expected 1 user message, got %d", len(sent))
	}
	inner, _ := sent[0]["message"].(map[string]any)
	if inner["content"] != "hi there" {
		t.Errorf("prompt content = %v", inner["content"])
	}
}

// Hooks were silently ignored by the old --print path. They must now reach the
// initialize request and be dispatched.
func TestQuerySupportsHooks(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	trans := newScriptedTransport()

	hookFired := make(chan string, 1)
	opts := DefaultAgentOptions().WithHook(types.HookEventPreToolUse, types.HookMatcher{
		Hooks: []types.HookCallback{
			func(_ context.Context, input types.HookInput, _ *string, _ *types.HookContext) (*types.HookOutput, error) {
				if pre, ok := input.(*types.PreToolUseHookInput); ok {
					hookFired <- pre.ToolName
				}
				return &types.HookOutput{}, nil
			},
		},
	})

	var initRequest map[string]any
	trans.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype != "initialize" {
			return nil
		}
		initRequest = request
		go func() {
			// Dispatch a hook callback the way the CLI would.
			callbackID, err := firstHookCallbackID(request, "PreToolUse")
			if err != nil {
				t.Errorf("initialize hooks payload: %v", err)
				_ = trans.Close()
				return
			}

			trans.push(map[string]any{
				"type":       "control_request",
				"request_id": "hook-1",
				"request": map[string]any{
					"subtype":     "hook_callback",
					"callback_id": callbackID,
					"input": map[string]any{
						"hook_event_name": "PreToolUse",
						"session_id":      "sess-1",
						"tool_name":       "Bash",
						"tool_input":      map[string]any{"command": "ls"},
					},
				},
			})
			trans.push(resultFrame())
			_ = trans.Close()
		}()
		return nil
	}

	for range QueryWithTransport(ctx, "run something", opts, trans) {
	}

	if initRequest == nil {
		t.Fatal("initialize was never sent")
	}
	if _, ok := initRequest["hooks"]; !ok {
		t.Error("hooks must be declared on the initialize request")
	}

	select {
	case name := <-hookFired:
		if name != "Bash" {
			t.Errorf("hook received tool %q", name)
		}
	default:
		t.Error("the hook callback was never invoked")
	}
}

// SDK MCP servers run in-process over the control channel, which the old
// --print path could not carry either.
func TestQuerySupportsSDKMCPServers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	greet := mcp.NewTool("greet", "Greet someone",
		map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		func(_ context.Context, args map[string]any) (map[string]any, error) {
			name, _ := args["name"].(string)
			return mcp.TextResult("Hello, " + name), nil
		})

	opts := DefaultAgentOptions().
		WithMCPServer("tools", mcp.NewSDKServer("tools", "1.0.0", greet))

	trans := newScriptedTransport()
	var declared []any

	trans.onRequest = func(subtype string, request map[string]any) map[string]any {
		if subtype != "initialize" {
			return nil
		}
		declared = append(declared, toAnySlice(request["sdkMcpServers"])...)
		go func() {
			trans.push(map[string]any{
				"type":       "control_request",
				"request_id": "mcp-1",
				"request": map[string]any{
					"subtype":     "mcp_message",
					"server_name": "tools",
					"message": map[string]any{
						"jsonrpc": "2.0",
						"id":      float64(1),
						"method":  "tools/call",
						"params": map[string]any{
							"name":      "greet",
							"arguments": map[string]any{"name": "World"},
						},
					},
				},
			})
			// The MCP handler answers asynchronously, so wait for its
			// response before ending the run; closing first would race it.
			trans.waitForWrite(ctx, "Hello, World")
			trans.push(resultFrame())
			_ = trans.Close()
		}()
		return nil
	}

	for range QueryWithTransport(ctx, "greet the world", opts, trans) {
	}

	if len(declared) != 1 || declared[0] != "tools" {
		t.Errorf("initialize must declare the SDK MCP server, got %v", declared)
	}

	// The tool result must come back over the control channel.
	trans.mu.Lock()
	written := append([]string{}, trans.written...)
	trans.mu.Unlock()

	var sawGreeting bool
	for _, raw := range written {
		if strings.Contains(raw, "Hello, World") {
			sawGreeting = true
		}
	}
	if !sawGreeting {
		t.Errorf("the in-process tool result never reached the transport: %v", written)
	}
}

// A transport failure must surface to the caller rather than looking like a
// conversation that simply ended.
func TestQuerySurfacesTransportErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	trans := newScriptedTransport()
	trans.onRequest = func(subtype string, _ map[string]any) map[string]any {
		if subtype == "initialize" {
			go func() {
				trans.errChan <- errTransportBroken
			}()
		}
		return nil
	}

	var gotErr error
	for msg := range QueryWithTransport(ctx, "hi", nil, trans) {
		if err, ok := msg.(error); ok {
			gotErr = err
		}
	}

	if gotErr == nil {
		t.Fatal("expected the transport error to reach the caller")
	}
	if !strings.Contains(gotErr.Error(), "broken") {
		t.Errorf("unexpected error: %v", gotErr)
	}
}

var errTransportBroken = &transportError{"transport broken"}

type transportError struct{ msg string }

func (e *transportError) Error() string { return e.msg }

// firstHookCallbackID digs the registered callback ID out of an initialize
// request. The payload has been through JSON by the time a transport sees it,
// so every nested slice is []any.
func firstHookCallbackID(request map[string]any, event string) (string, error) {
	hooks, ok := request["hooks"].(map[string]any)
	if !ok {
		return "", fmt.Errorf("no hooks declared: %v", request["hooks"])
	}

	matchers := toAnySlice(hooks[event])
	if len(matchers) == 0 {
		return "", fmt.Errorf("no %s matchers: %v", event, hooks)
	}

	matcher, ok := matchers[0].(map[string]any)
	if !ok {
		return "", fmt.Errorf("malformed matcher: %v", matchers[0])
	}

	ids := toAnySlice(matcher["hookCallbackIds"])
	if len(ids) == 0 {
		return "", fmt.Errorf("no callback IDs: %v", matcher)
	}

	id, ok := ids[0].(string)
	if !ok {
		return "", fmt.Errorf("callback ID is not a string: %v", ids[0])
	}
	return id, nil
}

// toAnySlice normalizes a slice that may be typed or []any, since values
// crossing the transport have been through JSON but values captured in-process
// have not.
func toAnySlice(v any) []any {
	switch items := v.(type) {
	case []any:
		return items
	case []string:
		out := make([]any, len(items))
		for i, s := range items {
			out[i] = s
		}
		return out
	default:
		return nil
	}
}

// waitForWrite blocks until a written frame contains needle, or the context
// expires. Control responses are produced by handler goroutines, so a test
// that needs one must wait rather than assume ordering.
func (s *scriptedTransport) waitForWrite(ctx context.Context, needle string) {
	for {
		s.mu.Lock()
		for _, raw := range s.written {
			if strings.Contains(raw, needle) {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Millisecond):
		}
	}
}

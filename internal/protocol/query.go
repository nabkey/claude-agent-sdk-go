// Package protocol handles the bidirectional control protocol with the CLI.
package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/internal/transport"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Query handles the bidirectional control protocol on top of Transport.
// It manages control request/response routing, hook callbacks, tool permission
// callbacks, message streaming, and initialization handshake.
type Query struct {
	transport         transport.Transport
	isStreamingMode   bool
	canUseTool        CanUseToolCallback
	hooks             map[types.HookEvent][]HookMatcherInternal
	sdkMCPServers     map[string]*MCPServerHandler
	initializeTimeout time.Duration

	// Control protocol state
	pendingResponses map[string]chan *ControlResult
	hookCallbacks    map[string]types.HookCallback
	nextCallbackID   int64
	requestCounter   int64
	pendingMu        sync.Mutex
	hookMu           sync.Mutex

	// Message stream
	messageChan        chan map[string]any
	errorChan          chan error
	initialized        bool
	closed             atomic.Bool
	initResult         map[string]any
	firstResultEvent   chan struct{}
	streamCloseTimeout time.Duration

	// Context for cancellation
	ctx    context.Context
	cancel context.CancelFunc
}

// CanUseToolCallback is the function signature for tool permission callbacks.
type CanUseToolCallback func(
	ctx context.Context,
	toolName string,
	input map[string]any,
	permissionCtx types.ToolPermissionContext,
) (types.PermissionResult, error)

// HookMatcherInternal is the internal representation of a hook matcher.
type HookMatcherInternal struct {
	Matcher     *string
	CallbackIDs []string
	Timeout     *float64
}

// ControlResult holds the result of a control request.
type ControlResult struct {
	Response map[string]any
	Error    error
}

// MCPServerHandler wraps an SDK MCP server for handling requests.
type MCPServerHandler struct {
	Name     string
	Version  string
	Instance any
	Tools    []MCPTool
}

// MCPTool represents a tool in an MCP server.
type MCPTool struct {
	Name        string
	Description string
	InputSchema map[string]any
	Handler     func(ctx context.Context, args map[string]any) (map[string]any, error)
}

// QueryOptions configures a new Query instance.
type QueryOptions struct {
	Transport         transport.Transport
	IsStreamingMode   bool
	CanUseTool        CanUseToolCallback
	Hooks             map[types.HookEvent][]types.HookMatcher
	SDKMCPServers     map[string]*MCPServerHandler
	InitializeTimeout time.Duration
}

// NewQuery creates a new Query instance.
func NewQuery(opts *QueryOptions) *Query {
	if opts.InitializeTimeout == 0 {
		opts.InitializeTimeout = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &Query{
		transport:          opts.Transport,
		isStreamingMode:    opts.IsStreamingMode,
		canUseTool:         opts.CanUseTool,
		sdkMCPServers:      opts.SDKMCPServers,
		initializeTimeout:  opts.InitializeTimeout,
		pendingResponses:   make(map[string]chan *ControlResult),
		hookCallbacks:      make(map[string]types.HookCallback),
		messageChan:        make(chan map[string]any, 100),
		errorChan:          make(chan error, 1),
		firstResultEvent:   make(chan struct{}),
		streamCloseTimeout: 60 * time.Second,
		ctx:                ctx,
		cancel:             cancel,
	}

	// Convert hooks to internal format and register callbacks
	if opts.Hooks != nil {
		q.hooks = make(map[types.HookEvent][]HookMatcherInternal)
		for event, matchers := range opts.Hooks {
			q.hooks[event] = make([]HookMatcherInternal, 0, len(matchers))
			for _, m := range matchers {
				callbackIDs := make([]string, 0, len(m.Hooks))
				for _, callback := range m.Hooks {
					callbackID := fmt.Sprintf("hook_%d", atomic.AddInt64(&q.nextCallbackID, 1)-1)
					q.hookCallbacks[callbackID] = callback
					callbackIDs = append(callbackIDs, callbackID)
				}
				q.hooks[event] = append(q.hooks[event], HookMatcherInternal{
					Matcher:     m.Matcher,
					CallbackIDs: callbackIDs,
					Timeout:     m.Timeout,
				})
			}
		}
	}

	return q
}

// Start begins reading messages from the transport.
func (q *Query) Start(ctx context.Context) {
	go q.readMessages(ctx)
}

// Initialize sends the initialization request and waits for response.
func (q *Query) Initialize(ctx context.Context) (map[string]any, error) {
	if !q.isStreamingMode {
		return nil, nil
	}

	// Build hooks configuration
	var hooksConfig map[string]any
	if len(q.hooks) > 0 {
		hooksConfig = make(map[string]any)
		for event, matchers := range q.hooks {
			if len(matchers) > 0 {
				eventMatchers := make([]map[string]any, 0, len(matchers))
				for _, m := range matchers {
					matcherConfig := map[string]any{
						"matcher":         m.Matcher,
						"hookCallbackIds": m.CallbackIDs,
					}
					if m.Timeout != nil {
						matcherConfig["timeout"] = *m.Timeout
					}
					eventMatchers = append(eventMatchers, matcherConfig)
				}
				hooksConfig[string(event)] = eventMatchers
			}
		}
	}

	request := map[string]any{
		"subtype": "initialize",
	}
	if hooksConfig != nil {
		request["hooks"] = hooksConfig
	}

	initCtx, cancel := context.WithTimeout(ctx, q.initializeTimeout)
	defer cancel()

	response, err := q.sendControlRequest(initCtx, request)
	if err != nil {
		return nil, err
	}

	q.initialized = true
	q.initResult = response
	return response, nil
}

// readMessages reads messages from transport and routes them.
func (q *Query) readMessages(ctx context.Context) {
	defer close(q.messageChan)

	msgChan, errChan := q.transport.ReadMessages(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.ctx.Done():
			return
		case err, ok := <-errChan:
			if ok && err != nil {
				// Signal pending control requests
				q.pendingMu.Lock()
				for _, ch := range q.pendingResponses {
					select {
					case ch <- &ControlResult{Error: err}:
					default:
					}
				}
				q.pendingMu.Unlock()

				select {
				case q.errorChan <- err:
				default:
				}
			}
			return
		case msg, ok := <-msgChan:
			if !ok {
				return
			}

			if q.closed.Load() {
				return
			}

			msgType, _ := msg["type"].(string)

			switch msgType {
			case "control_response":
				q.handleControlResponse(msg)
				continue

			case "control_request":
				go q.handleControlRequest(ctx, msg)
				continue

			case "control_cancel_request":
				// TODO: Implement cancellation support
				continue

			case "result":
				// Signal first result for stream closure
				select {
				case <-q.firstResultEvent:
				default:
					close(q.firstResultEvent)
				}
			}

			// Regular messages go to the stream
			select {
			case q.messageChan <- msg:
			case <-ctx.Done():
				return
			case <-q.ctx.Done():
				return
			}
		}
	}
}

// handleControlResponse processes incoming control responses.
func (q *Query) handleControlResponse(msg map[string]any) {
	response, _ := msg["response"].(map[string]any)
	requestID, _ := response["request_id"].(string)

	q.pendingMu.Lock()
	ch, exists := q.pendingResponses[requestID]
	if exists {
		delete(q.pendingResponses, requestID)
	}
	q.pendingMu.Unlock()

	if !exists {
		return
	}

	subtype, _ := response["subtype"].(string)
	if subtype == "error" {
		errMsg, _ := response["error"].(string)
		ch <- &ControlResult{Error: fmt.Errorf("%s", errMsg)}
	} else {
		respData, _ := response["response"].(map[string]any)
		ch <- &ControlResult{Response: respData}
	}
}

// handleControlRequest processes incoming control requests from CLI.
func (q *Query) handleControlRequest(ctx context.Context, msg map[string]any) {
	requestID, _ := msg["request_id"].(string)
	request, _ := msg["request"].(map[string]any)
	subtype, _ := request["subtype"].(string)

	var responseData map[string]any
	var err error

	switch subtype {
	case "can_use_tool":
		responseData, err = q.handleToolPermission(ctx, request)

	case "hook_callback":
		responseData, err = q.handleHookCallback(ctx, request)

	case "mcp_message":
		responseData, err = q.handleMCPMessage(ctx, request)

	default:
		err = fmt.Errorf("unsupported control request subtype: %s", subtype)
	}

	// Send response
	var response map[string]any
	if err != nil {
		response = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "error",
				"request_id": requestID,
				"error":      err.Error(),
			},
		}
	} else {
		response = map[string]any{
			"type": "control_response",
			"response": map[string]any{
				"subtype":    "success",
				"request_id": requestID,
				"response":   responseData,
			},
		}
	}

	data, _ := json.Marshal(response)
	q.transport.Write(ctx, string(data)+"\n")
}

// handleToolPermission handles tool permission requests.
func (q *Query) handleToolPermission(ctx context.Context, request map[string]any) (map[string]any, error) {
	if q.canUseTool == nil {
		return nil, fmt.Errorf("canUseTool callback is not provided")
	}

	toolName, _ := request["tool_name"].(string)
	input, _ := request["input"].(map[string]any)
	suggestions, _ := request["permission_suggestions"].([]any)

	permCtx := types.ToolPermissionContext{
		Signal: nil,
	}
	// Convert suggestions to PermissionUpdate slice
	for _, s := range suggestions {
		if sMap, ok := s.(map[string]any); ok {
			update := types.PermissionUpdate{
				Type: types.PermissionUpdateType(sMap["type"].(string)),
			}
			permCtx.Suggestions = append(permCtx.Suggestions, update)
		}
	}

	result, err := q.canUseTool(ctx, toolName, input, permCtx)
	if err != nil {
		return nil, err
	}

	switch r := result.(type) {
	case *types.PermissionResultAllow:
		responseData := map[string]any{
			"behavior": "allow",
		}
		if r.UpdatedInput != nil {
			responseData["updatedInput"] = r.UpdatedInput
		} else {
			responseData["updatedInput"] = input
		}
		if r.UpdatedPermissions != nil {
			perms := make([]map[string]any, len(r.UpdatedPermissions))
			for i, p := range r.UpdatedPermissions {
				perms[i] = p.ToMap()
			}
			responseData["updatedPermissions"] = perms
		}
		return responseData, nil

	case *types.PermissionResultDeny:
		responseData := map[string]any{
			"behavior": "deny",
			"message":  r.Message,
		}
		if r.Interrupt {
			responseData["interrupt"] = true
		}
		return responseData, nil

	default:
		return nil, fmt.Errorf("invalid permission result type")
	}
}

// handleHookCallback handles hook callback requests.
func (q *Query) handleHookCallback(ctx context.Context, request map[string]any) (map[string]any, error) {
	callbackID, _ := request["callback_id"].(string)
	input := request["input"]
	toolUseID, _ := request["tool_use_id"].(*string)

	q.hookMu.Lock()
	callback, exists := q.hookCallbacks[callbackID]
	q.hookMu.Unlock()

	if !exists {
		return nil, fmt.Errorf("no hook callback found for ID: %s", callbackID)
	}

	// Convert input to appropriate HookInput type
	hookInput, err := parseHookInput(input)
	if err != nil {
		return nil, err
	}

	hookCtx := &types.HookContext{Signal: nil}
	output, err := callback(ctx, hookInput, toolUseID, hookCtx)
	if err != nil {
		return nil, err
	}

	// Convert output to map
	return hookOutputToMap(output), nil
}

// handleMCPMessage handles MCP server requests.
func (q *Query) handleMCPMessage(ctx context.Context, request map[string]any) (map[string]any, error) {
	serverName, _ := request["server_name"].(string)
	message, _ := request["message"].(map[string]any)

	handler, exists := q.sdkMCPServers[serverName]
	if !exists {
		return map[string]any{
			"mcp_response": map[string]any{
				"jsonrpc": "2.0",
				"id":      message["id"],
				"error": map[string]any{
					"code":    -32601,
					"message": fmt.Sprintf("Server '%s' not found", serverName),
				},
			},
		}, nil
	}

	mcpResponse := handler.HandleRequest(ctx, message)
	return map[string]any{"mcp_response": mcpResponse}, nil
}

// sendControlRequest sends a control request and waits for response.
func (q *Query) sendControlRequest(ctx context.Context, request map[string]any) (map[string]any, error) {
	if !q.isStreamingMode {
		return nil, fmt.Errorf("control requests require streaming mode")
	}

	// Generate unique request ID
	counter := atomic.AddInt64(&q.requestCounter, 1)
	randBytes := make([]byte, 4)
	rand.Read(randBytes)
	requestID := fmt.Sprintf("req_%d_%s", counter, hex.EncodeToString(randBytes))

	// Create response channel
	respChan := make(chan *ControlResult, 1)
	q.pendingMu.Lock()
	q.pendingResponses[requestID] = respChan
	q.pendingMu.Unlock()

	// Build and send request
	controlRequest := map[string]any{
		"type":       "control_request",
		"request_id": requestID,
		"request":    request,
	}

	data, err := json.Marshal(controlRequest)
	if err != nil {
		q.pendingMu.Lock()
		delete(q.pendingResponses, requestID)
		q.pendingMu.Unlock()
		return nil, err
	}

	if err := q.transport.Write(ctx, string(data)+"\n"); err != nil {
		q.pendingMu.Lock()
		delete(q.pendingResponses, requestID)
		q.pendingMu.Unlock()
		return nil, err
	}

	// Wait for response
	select {
	case <-ctx.Done():
		q.pendingMu.Lock()
		delete(q.pendingResponses, requestID)
		q.pendingMu.Unlock()
		return nil, errors.NewTimeoutError(fmt.Sprintf("control request: %s", request["subtype"]))

	case result := <-respChan:
		if result.Error != nil {
			return nil, result.Error
		}
		return result.Response, nil
	}
}

// Interrupt sends an interrupt control request.
func (q *Query) Interrupt(ctx context.Context) error {
	_, err := q.sendControlRequest(ctx, map[string]any{"subtype": "interrupt"})
	return err
}

// SetPermissionMode changes the permission mode.
func (q *Query) SetPermissionMode(ctx context.Context, mode types.PermissionMode) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype": "set_permission_mode",
		"mode":    string(mode),
	})
	return err
}

// SetModel changes the AI model.
func (q *Query) SetModel(ctx context.Context, model *string) error {
	request := map[string]any{"subtype": "set_model"}
	if model != nil {
		request["model"] = *model
	}
	_, err := q.sendControlRequest(ctx, request)
	return err
}

// ReceiveMessages returns a channel for receiving SDK messages.
func (q *Query) ReceiveMessages() <-chan map[string]any {
	return q.messageChan
}

// ErrorChan returns the error channel.
func (q *Query) ErrorChan() <-chan error {
	return q.errorChan
}

// Close closes the query and transport.
func (q *Query) Close() error {
	q.closed.Store(true)
	q.cancel()
	return q.transport.Close()
}

// GetInitResult returns the initialization result.
func (q *Query) GetInitResult() map[string]any {
	return q.initResult
}

// WaitForFirstResult waits for the first result message.
func (q *Query) WaitForFirstResult(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-q.firstResultEvent:
		return nil
	case <-time.After(q.streamCloseTimeout):
		return nil
	}
}

// HandleRequest processes an MCP request.
func (h *MCPServerHandler) HandleRequest(ctx context.Context, message map[string]any) map[string]any {
	method, _ := message["method"].(string)
	params, _ := message["params"].(map[string]any)
	id := message["id"]

	switch method {
	case "initialize":
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result": map[string]any{
				"protocolVersion": "2024-11-05",
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    h.Name,
					"version": h.Version,
				},
			},
		}

	case "tools/list":
		tools := make([]map[string]any, len(h.Tools))
		for i, tool := range h.Tools {
			tools[i] = map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			}
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"result":  map[string]any{"tools": tools},
		}

	case "tools/call":
		toolName, _ := params["name"].(string)
		args, _ := params["arguments"].(map[string]any)

		for _, tool := range h.Tools {
			if tool.Name == toolName {
				result, err := tool.Handler(ctx, args)
				if err != nil {
					return map[string]any{
						"jsonrpc": "2.0",
						"id":      id,
						"error": map[string]any{
							"code":    -32603,
							"message": err.Error(),
						},
					}
				}
				return map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result":  result,
				}
			}
		}
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]any{
				"code":    -32601,
				"message": fmt.Sprintf("Tool '%s' not found", toolName),
			},
		}

	case "notifications/initialized":
		return map[string]any{"jsonrpc": "2.0", "result": map[string]any{}}

	default:
		return map[string]any{
			"jsonrpc": "2.0",
			"id":      id,
			"error": map[string]any{
				"code":    -32601,
				"message": fmt.Sprintf("Method '%s' not found", method),
			},
		}
	}
}

// parseHookInput converts raw input to a typed HookInput.
func parseHookInput(input any) (types.HookInput, error) {
	data, ok := input.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid hook input format")
	}

	eventName, _ := data["hook_event_name"].(string)

	base := types.BaseHookInput{
		SessionID:      getString(data, "session_id"),
		TranscriptPath: getString(data, "transcript_path"),
		Cwd:            getString(data, "cwd"),
	}
	if pm, ok := data["permission_mode"].(string); ok {
		base.PermissionMode = &pm
	}

	switch eventName {
	case "PreToolUse":
		return &types.PreToolUseHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPreToolUse,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
		}, nil

	case "PostToolUse":
		return &types.PostToolUseHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPostToolUse,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
			ToolResponse:  data["tool_response"],
		}, nil

	case "UserPromptSubmit":
		return &types.UserPromptSubmitHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventUserPromptSubmit,
			Prompt:        getString(data, "prompt"),
		}, nil

	case "Stop":
		return &types.StopHookInput{
			BaseHookInput:  base,
			HookEventName:  types.HookEventStop,
			StopHookActive: getBool(data, "stop_hook_active"),
		}, nil

	case "SubagentStop":
		return &types.SubagentStopHookInput{
			BaseHookInput:  base,
			HookEventName:  types.HookEventSubagentStop,
			StopHookActive: getBool(data, "stop_hook_active"),
		}, nil

	case "PreCompact":
		input := &types.PreCompactHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPreCompact,
			Trigger:       getString(data, "trigger"),
		}
		if ci, ok := data["custom_instructions"].(string); ok {
			input.CustomInstructions = &ci
		}
		return input, nil

	default:
		return nil, fmt.Errorf("unknown hook event: %s", eventName)
	}
}

// hookOutputToMap converts HookOutput to a map for JSON serialization.
func hookOutputToMap(output *types.HookOutput) map[string]any {
	if output == nil {
		return map[string]any{}
	}

	result := make(map[string]any)

	if output.Continue != nil {
		result["continue"] = *output.Continue
	}
	if output.SuppressOutput != nil {
		result["suppressOutput"] = *output.SuppressOutput
	}
	if output.StopReason != nil {
		result["stopReason"] = *output.StopReason
	}
	if output.Decision != nil {
		result["decision"] = *output.Decision
	}
	if output.SystemMessage != nil {
		result["systemMessage"] = *output.SystemMessage
	}
	if output.Reason != nil {
		result["reason"] = *output.Reason
	}
	if output.Async != nil {
		result["async"] = *output.Async
	}
	if output.AsyncTimeout != nil {
		result["asyncTimeout"] = *output.AsyncTimeout
	}
	if output.HookSpecificOutput != nil {
		// Convert hook-specific output based on type
		switch hso := output.HookSpecificOutput.(type) {
		case *types.PreToolUseHookSpecificOutput:
			hsoMap := map[string]any{"hookEventName": hso.HookEventName}
			if hso.PermissionDecision != nil {
				hsoMap["permissionDecision"] = *hso.PermissionDecision
			}
			if hso.PermissionDecisionReason != nil {
				hsoMap["permissionDecisionReason"] = *hso.PermissionDecisionReason
			}
			if hso.UpdatedInput != nil {
				hsoMap["updatedInput"] = hso.UpdatedInput
			}
			result["hookSpecificOutput"] = hsoMap

		case *types.PostToolUseHookSpecificOutput:
			hsoMap := map[string]any{"hookEventName": hso.HookEventName}
			if hso.AdditionalContext != nil {
				hsoMap["additionalContext"] = *hso.AdditionalContext
			}
			result["hookSpecificOutput"] = hsoMap

		case *types.UserPromptSubmitHookSpecificOutput:
			hsoMap := map[string]any{"hookEventName": hso.HookEventName}
			if hso.AdditionalContext != nil {
				hsoMap["additionalContext"] = *hso.AdditionalContext
			}
			result["hookSpecificOutput"] = hsoMap
		}
	}

	return result
}

// Helper functions for type-safe map access
func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getMap(m map[string]any, key string) map[string]any {
	if v, ok := m[key].(map[string]any); ok {
		return v
	}
	return nil
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

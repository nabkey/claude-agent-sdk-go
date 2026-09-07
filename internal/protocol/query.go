// Package protocol handles the bidirectional control protocol with the CLI.
package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

	// agentProgressSummaries is sent as a field on the initialize request.
	// There is no corresponding CLI flag.
	agentProgressSummaries bool

	// initConfig carries the rest of the initialize payload.
	initConfig *InitConfig

	onElicitation ElicitationCallback
	onUserDialog  UserDialogCallback

	// mirror receives transcript_mirror frames when a SessionStore is set.
	mirror MirrorSink

	// Control protocol state
	pendingResponses map[string]chan *ControlResult
	hookCallbacks    map[string]types.HookCallback
	nextCallbackID   int64
	requestCounter   int64
	pendingMu        sync.Mutex
	hookMu           sync.Mutex

	// Message stream
	messageChan      chan map[string]any
	errorChan        chan error
	initialized      bool
	closed           atomic.Bool
	initResult       map[string]any
	firstResultEvent chan struct{}
	resultEventOnce  sync.Once

	// terminalErr is the error that ended the message stream, surfaced to
	// consumers through Err() after the channel closes.
	terminalErr   error
	terminalErrMu sync.Mutex

	// inflightTasks holds task IDs of delegated agent work that has started
	// but not finished. A result frame ends one turn, not the run: background
	// subagents keep running past it and still need stdin for hook and
	// SDK-MCP control responses, so stdin must not close while any are live.
	inflightTasks map[string]struct{}
	taskMu        sync.Mutex

	// lastErrorResult carries the result frame that reported is_error=true.
	// The CLI then exits non-zero on purpose, and the resulting "exit code 1"
	// ProcessError carries no information, so it is replaced with a
	// ResultError built from this.
	lastErrorResult map[string]any

	// inflightRequests maps a control request_id to the cancel func for its
	// handler, so control_cancel_request can abandon it.
	inflightRequests map[string]context.CancelFunc
	requestMu        sync.Mutex

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
	Annotations any
	Meta        map[string]any
}

// InitConfig is the configuration carried on the initialize control request
// rather than as CLI flags.
//
// Several of these have no flag equivalent at all; others are sent here
// because the payload can exceed a comfortable argv size.
type InitConfig struct {
	// Agents are custom subagent definitions, keyed by agent name.
	Agents map[string]any
	// SystemPrompt is the block form of the system prompt. Include
	// types.SystemPromptDynamicBoundary to mark the cacheable prefix.
	SystemPrompt []string
	// AppendSystemPrompt is appended to the system prompt.
	AppendSystemPrompt *string
	// ExcludeDynamicSections strips per-user dynamic sections from a preset
	// prompt so it stays cacheable across users.
	ExcludeDynamicSections *bool
	// Title names a new session.
	Title *string
	// Skills is an explicit skill allowlist. Omitted means no filter.
	Skills []string
	// ToolAliases redirects model-emitted tool names before resolution.
	ToolAliases map[string]string
	// PlanModeInstructions replaces plan mode's default workflow body.
	PlanModeInstructions *string
	// JSONSchema requests structured output matching this schema.
	JSONSchema map[string]any
	// PromptSuggestions emits a predicted next prompt after each turn.
	PromptSuggestions bool
	// ForwardSubagentText forwards subagent text and thinking blocks.
	ForwardSubagentText bool
	// SupportedDialogKinds declares which dialogs the host can render.
	SupportedDialogKinds []string
	// PerTaskStopAffordance narrows an interrupt to the current turn, leaving
	// background agents and workflows running.
	PerTaskStopAffordance bool
	// Plugins carries the plugin list when the caller chose initialize
	// delivery. Nil under argv delivery, where the flags carry it instead.
	Plugins []map[string]any
	// Warn receives advisory warnings raised while initializing.
	Warn func(string)
}

// apply writes the configured fields onto an initialize request, omitting
// anything unset so older CLIs are not handed unknown keys with empty values.
func (c *InitConfig) apply(request map[string]any) {
	if c == nil {
		return
	}
	if len(c.Agents) > 0 {
		request["agents"] = c.Agents
	}
	if len(c.SystemPrompt) > 0 {
		request["systemPrompt"] = c.SystemPrompt
	}
	if c.AppendSystemPrompt != nil {
		request["appendSystemPrompt"] = *c.AppendSystemPrompt
	}
	if c.ExcludeDynamicSections != nil {
		request["excludeDynamicSections"] = *c.ExcludeDynamicSections
	}
	if c.Title != nil {
		request["title"] = *c.Title
	}
	if len(c.Skills) > 0 {
		request["skills"] = c.Skills
	}
	if len(c.ToolAliases) > 0 {
		request["toolAliases"] = c.ToolAliases
	}
	if c.PlanModeInstructions != nil {
		request["planModeInstructions"] = *c.PlanModeInstructions
	}
	if len(c.JSONSchema) > 0 {
		request["jsonSchema"] = c.JSONSchema
	}
	if c.PromptSuggestions {
		request["promptSuggestions"] = true
	}
	if c.ForwardSubagentText {
		request["forwardSubagentText"] = true
	}
	if len(c.SupportedDialogKinds) > 0 {
		request["supportedDialogKinds"] = c.SupportedDialogKinds
	}
	if c.PerTaskStopAffordance {
		request["perTaskStopAffordance"] = true
	}
	if len(c.Plugins) > 0 {
		request["plugins"] = c.Plugins
	}
}

// QueryOptions configures a new Query instance.
type QueryOptions struct {
	Transport         transport.Transport
	IsStreamingMode   bool
	CanUseTool        CanUseToolCallback
	Hooks             map[types.HookEvent][]types.HookMatcher
	SDKMCPServers     map[string]*MCPServerHandler
	InitializeTimeout time.Duration

	// AgentProgressSummaries enables AI-generated progress summaries for
	// subagents. Sent on the initialize request; there is no CLI flag.
	AgentProgressSummaries bool

	// InitConfig carries the rest of the initialize payload.
	InitConfig *InitConfig

	// OnElicitation handles MCP elicitation control requests.
	OnElicitation ElicitationCallback

	// OnUserDialog renders host-side blocking dialogs.
	OnUserDialog UserDialogCallback

	// Mirror receives transcript_mirror frames for SessionStore mirroring.
	Mirror MirrorSink
}

// MirrorSink consumes transcript mirror frames.
//
// Enqueue must not block the read loop; a slow store is expected to buffer
// and flush asynchronously.
type MirrorSink interface {
	Enqueue(filePath string, entries []map[string]any)
	Flush()
}

// ElicitationCallback handles an MCP server's request for user input.
type ElicitationCallback func(context.Context, types.ElicitationRequest) (types.ElicitationResult, error)

// UserDialogCallback renders a blocking dialog on the CLI's behalf.
type UserDialogCallback func(context.Context, types.UserDialogRequest) (types.UserDialogResult, error)

// NewQuery creates a new Query instance.
func NewQuery(opts *QueryOptions) *Query {
	if opts.InitializeTimeout == 0 {
		opts.InitializeTimeout = 60 * time.Second
	}

	ctx, cancel := context.WithCancel(context.Background())

	q := &Query{
		transport:              opts.Transport,
		isStreamingMode:        opts.IsStreamingMode,
		canUseTool:             opts.CanUseTool,
		sdkMCPServers:          opts.SDKMCPServers,
		initializeTimeout:      opts.InitializeTimeout,
		agentProgressSummaries: opts.AgentProgressSummaries,
		initConfig:             opts.InitConfig,
		onElicitation:          opts.OnElicitation,
		onUserDialog:           opts.OnUserDialog,
		mirror:                 opts.Mirror,
		pendingResponses:       make(map[string]chan *ControlResult),
		hookCallbacks:          make(map[string]types.HookCallback),
		messageChan:            make(chan map[string]any, 100),
		errorChan:              make(chan error, 1),
		firstResultEvent:       make(chan struct{}),
		inflightTasks:          make(map[string]struct{}),
		inflightRequests:       make(map[string]context.CancelFunc),
		ctx:                    ctx,
		cancel:                 cancel,
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
	if q.agentProgressSummaries {
		request["agentProgressSummaries"] = true
	}
	if len(q.sdkMCPServers) > 0 {
		names := make([]string, 0, len(q.sdkMCPServers))
		for name := range q.sdkMCPServers {
			names = append(names, name)
		}
		sort.Strings(names)
		request["sdkMcpServers"] = names
	}
	q.initConfig.apply(request)

	initCtx, cancel := context.WithTimeout(ctx, q.initializeTimeout)
	defer cancel()

	response, err := q.sendControlRequest(initCtx, request)
	if err != nil {
		return nil, err
	}

	q.initialized = true
	q.initResult = response
	q.warnIfPluginsNotApplied(response)
	return response, nil
}

// warnIfPluginsNotApplied reports a CLI that ignored initialize-delivered
// plugins.
//
// A CLI too old to read the initialize payload answers without
// plugins_applied and runs with only the plugins it was launched with, which
// under initialize delivery is none. Silently losing every plugin is worse
// than a warning, so say so.
func (q *Query) warnIfPluginsNotApplied(response map[string]any) {
	if q.initConfig == nil || len(q.initConfig.Plugins) == 0 {
		return
	}
	if applied, ok := response["plugins_applied"].(bool); ok && applied {
		return
	}
	warn := q.initConfig.Warn
	if warn == nil {
		return
	}
	warn(fmt.Sprintf(
		"claude code did not report plugins_applied for the %d plugins sent with "+
			"PluginDelivery %q; the process is running with the plugins it was "+
			"launched with. Use types.PluginDeliveryArgv with this CLI version.",
		len(q.initConfig.Plugins), types.PluginDeliveryInitialize))
}

// deferringTaskTypes are the task types whose completion runs a follow-up
// turn, and which therefore may still need the control channel after a turn's
// result frame.
//
// This mirrors the set the CLI itself holds a result back for. Background
// shells and monitors are deliberately excluded: they can run indefinitely by
// design, so tracking one would withhold the stdin close forever rather than
// briefly.
var deferringTaskTypes = map[string]struct{}{
	"local_agent":    {},
	"local_workflow": {},
}

// terminalTaskStatuses are the task statuses meaning the task has finished.
// The vocabulary spans both lifecycle frames: task_notification reports
// "stopped" (the CLI's mapped form of a killed task) while task_updated
// reports the raw "killed".
var terminalTaskStatuses = map[string]struct{}{
	"completed": {},
	"failed":    {},
	"stopped":   {},
	"killed":    {},
}

// readMessages reads messages from transport and routes them.
func (q *Query) readMessages(ctx context.Context) {
	defer close(q.messageChan)
	// Unblock anything waiting on a run-ending result: on an early exit the
	// result may never arrive, and a waiter would otherwise stall.
	defer q.signalResultEvent()
	// Flush buffered mirror entries so an early EOF or transport error does
	// not drop what was batched this turn.
	defer q.flushMirror()

	msgChan, errChan := q.transport.ReadMessages(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-q.ctx.Done():
			return
		case err, ok := <-errChan:
			if ok && err != nil {
				q.failPendingRequests(err)
				q.setTerminalError(err)
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
				q.spawnControlRequestHandler(ctx, msg)
				continue

			case "control_cancel_request":
				q.cancelControlRequest(msg)
				continue

			case "transcript_mirror":
				// Mirror frames feed the SessionStore write path. They are
				// bookkeeping, not conversation, so they never reach
				// consumers.
				q.enqueueMirrorFrame(msg)
				continue
			}

			q.trackLifecycle(msg, msgType)

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

// trackLifecycle maintains the in-flight task ledger and the run-ending result
// signal, and remembers structured errors from an error result.
func (q *Query) trackLifecycle(msg map[string]any, msgType string) {
	if msgType == "system" {
		q.trackTaskLifecycle(msg)
	}

	if msgType != "result" {
		// Anything other than the post-turn session_state_changed marker means
		// the conversation moved on, so a later process error is a fresh crash
		// rather than the expected exit from a prior error result.
		if !(msgType == "system" && getString(msg, "subtype") == "session_state_changed") {
			q.lastErrorResult = nil
		}
		return
	}

	if isError, _ := msg["is_error"].(bool); isError {
		q.lastErrorResult = msg
	} else {
		q.lastErrorResult = nil
	}

	q.flushMirror()

	// A result ends one turn. Only signal the run as ending -- which releases
	// the stdin close -- when no delegated agent work is still in flight.
	q.taskMu.Lock()
	inflight := len(q.inflightTasks)
	q.taskMu.Unlock()
	if inflight == 0 {
		q.signalResultEvent()
	}
}

// errorTextFromResult renders the structured errors on an error result.
func errorTextFromResult(msg map[string]any) string {
	if raw, ok := msg["errors"].([]any); ok && len(raw) > 0 {
		parts := make([]string, 0, len(raw))
		for _, e := range raw {
			if s, ok := e.(string); ok {
				parts = append(parts, s)
			}
		}
		if len(parts) > 0 {
			return strings.Join(parts, "; ")
		}
	}
	if subtype := getString(msg, "subtype"); subtype != "" {
		return subtype
	}
	return "unknown error"
}

// trackTaskLifecycle adds and removes delegated agent work from the ledger.
//
// task_started marks a task in flight; task_notification, or a task_updated
// patch carrying a terminal status, clears it. Terminal completion can arrive
// as either frame -- not every terminal task emits a notification -- so both
// are handled.
func (q *Query) trackTaskLifecycle(msg map[string]any) {
	taskID := getString(msg, "task_id")
	if taskID == "" {
		return
	}

	switch getString(msg, "subtype") {
	case "task_started":
		if _, deferring := deferringTaskTypes[getString(msg, "task_type")]; !deferring {
			return
		}
		q.taskMu.Lock()
		q.inflightTasks[taskID] = struct{}{}
		q.taskMu.Unlock()

	case "task_notification":
		q.taskMu.Lock()
		delete(q.inflightTasks, taskID)
		q.taskMu.Unlock()

	case "task_updated":
		patch, ok := msg["patch"].(map[string]any)
		if !ok {
			return
		}
		if _, terminal := terminalTaskStatuses[getString(patch, "status")]; !terminal {
			return
		}
		q.taskMu.Lock()
		delete(q.inflightTasks, taskID)
		q.taskMu.Unlock()
	}
}

// enqueueMirrorFrame hands a transcript mirror frame to the configured sink.
func (q *Query) enqueueMirrorFrame(msg map[string]any) {
	if q.mirror == nil {
		return
	}

	filePath := getString(msg, "filePath")
	raw, ok := msg["entries"].([]any)
	if !ok {
		return
	}

	entries := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if entry, ok := item.(map[string]any); ok {
			entries = append(entries, entry)
		}
	}
	if len(entries) > 0 {
		q.mirror.Enqueue(filePath, entries)
	}
}

// flushMirror drains buffered mirror entries, if a sink is configured.
func (q *Query) flushMirror() {
	if q.mirror != nil {
		q.mirror.Flush()
	}
}

// signalResultEvent releases waiters blocked on a run-ending result.
func (q *Query) signalResultEvent() {
	q.resultEventOnce.Do(func() { close(q.firstResultEvent) })
}

// setTerminalError records the error that ended the stream.
//
// A CLI that emitted a result with is_error=true then exits non-zero on
// purpose, for shell-script consumers. The trailing ProcessError carries no
// information beyond "exit code 1", so it is replaced with the structured
// error the CLI already reported.
func (q *Query) setTerminalError(err error) {
	var processErr *errors.ProcessError
	if errors.As(err, &processErr) && q.lastErrorResult != nil {
		// The transport's stderr is a generic placeholder here, and the
		// result text is the real cause, so it is deliberately not carried
		// over. The ProcessError becomes the wrapped cause instead.
		resultErr := errors.NewResultError(
			"claude code returned an error result: "+errorTextFromResult(q.lastErrorResult),
			q.lastErrorResult, processErr.ExitCode, "")
		resultErr.Cause = err
		err = resultErr
	}

	q.terminalErrMu.Lock()
	if q.terminalErr == nil {
		q.terminalErr = err
	}
	q.terminalErrMu.Unlock()

	select {
	case q.errorChan <- err:
	default:
	}
}

// failPendingRequests wakes every in-flight control request so callers fail
// fast rather than waiting out their timeout.
func (q *Query) failPendingRequests(err error) {
	q.pendingMu.Lock()
	defer q.pendingMu.Unlock()
	for _, ch := range q.pendingResponses {
		select {
		case ch <- &ControlResult{Error: err}:
		default:
		}
	}
}

// spawnControlRequestHandler runs a control request handler, tracking it so a
// control_cancel_request can abandon it.
func (q *Query) spawnControlRequestHandler(ctx context.Context, msg map[string]any) {
	requestID, _ := msg["request_id"].(string)

	handlerCtx, cancel := context.WithCancel(ctx)
	if requestID != "" {
		q.requestMu.Lock()
		q.inflightRequests[requestID] = cancel
		q.requestMu.Unlock()
	}

	go func() {
		defer cancel()
		defer func() {
			if requestID == "" {
				return
			}
			q.requestMu.Lock()
			delete(q.inflightRequests, requestID)
			q.requestMu.Unlock()
		}()
		q.handleControlRequest(handlerCtx, msg)
	}()
}

// cancelControlRequest abandons an in-flight handler at the CLI's request.
//
// The CLI has already given up on the request, so the handler is cancelled and
// no response is written.
func (q *Query) cancelControlRequest(msg map[string]any) {
	requestID, _ := msg["request_id"].(string)
	if requestID == "" {
		return
	}

	q.requestMu.Lock()
	cancel, ok := q.inflightRequests[requestID]
	delete(q.inflightRequests, requestID)
	q.requestMu.Unlock()

	if ok {
		cancel()
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

	case "elicitation":
		responseData, err = q.handleElicitation(ctx, request)

	case "request_user_dialog":
		responseData, err = q.handleUserDialog(ctx, request)

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

	// A cancelled request has already been abandoned by the CLI; writing a
	// stale response would desynchronize the channel.
	if ctx.Err() != nil {
		return
	}

	data, _ := json.Marshal(response)
	_ = q.transport.Write(context.WithoutCancel(ctx), string(data)+"\n")
}

// handleToolPermission handles tool permission requests.
func (q *Query) handleToolPermission(ctx context.Context, request map[string]any) (map[string]any, error) {
	if q.canUseTool == nil {
		return nil, fmt.Errorf("canUseTool callback is not provided")
	}

	toolName, _ := request["tool_name"].(string)
	input, _ := request["input"].(map[string]any)
	suggestions, _ := request["permission_suggestions"].([]any)

	// Decode suggestions in full. Callers are expected to echo these back as
	// PermissionResultAllow.UpdatedPermissions to implement "always allow",
	// so dropping the rules/behavior/mode/directories payload would silently
	// turn that into a no-op.
	permCtx := types.ToolPermissionContext{
		Signal:      nil,
		Suggestions: types.PermissionUpdatesFromAny(suggestions),
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

// handleElicitation forwards an MCP server's request for user input to the
// caller's callback.
//
// With no callback configured the request is declined, matching the reference
// SDKs: an unanswered elicitation would block the server indefinitely.
func (q *Query) handleElicitation(ctx context.Context, request map[string]any) (map[string]any, error) {
	if q.onElicitation == nil {
		return map[string]any{"action": string(types.ElicitationDecline)}, nil
	}

	req := types.ElicitationRequest{
		ServerName: getString(request, "server_name"),
		Mode:       types.ElicitationMode(getString(request, "mode")),
		Message:    getString(request, "message"),
		URL:        getString(request, "url"),
		Raw:        request,
	}
	if schema, ok := request["requestedSchema"].(map[string]any); ok {
		req.RequestedSchema = schema
	}

	result, err := q.onElicitation(ctx, req)
	if err != nil {
		return nil, err
	}

	response := map[string]any{"action": string(result.Action)}
	if result.Content != nil {
		response["content"] = result.Content
	}
	return response, nil
}

// handleUserDialog asks the host to render a blocking dialog.
//
// A host that did not supply a callback must not answer: on a multi-client
// session another attached client may be the declared renderer, and replying
// here would settle the dialog out from under it. Returning an error leaves
// the dialog for the CLI's own park deadline to resolve.
func (q *Query) handleUserDialog(ctx context.Context, request map[string]any) (map[string]any, error) {
	if q.onUserDialog == nil {
		return nil, fmt.Errorf("no user dialog handler configured")
	}

	req := types.UserDialogRequest{
		DialogKind: getString(request, "dialog_kind"),
	}
	if payload, ok := request["payload"].(map[string]any); ok {
		req.Payload = payload
	}
	if id := getString(request, "tool_use_id"); id != "" {
		req.ToolUseID = &id
	}

	result, err := q.onUserDialog(ctx, req)
	if err != nil {
		return nil, err
	}

	response := map[string]any{"behavior": string(result.Behavior)}
	if result.Result != nil {
		response["result"] = result.Result
	}
	return response, nil
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
	_, _ = rand.Read(randBytes)
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

// StopTask sends a request to stop a running task.
func (q *Query) StopTask(ctx context.Context, taskID string) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype": "stop_task",
		"task_id": taskID,
	})
	return err
}

// RewindFiles sends a request to rewind files to a checkpoint.
func (q *Query) RewindFiles(ctx context.Context, userMessageID string) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype":         "rewind_files",
		"user_message_id": userMessageID,
	})
	return err
}

// GetMCPStatus retrieves the status of all MCP servers.
func (q *Query) GetMCPStatus(ctx context.Context) (*types.McpStatusResponse, error) {
	response, err := q.sendControlRequest(ctx, map[string]any{
		"subtype": "mcp_status",
	})
	if err != nil {
		return nil, err
	}

	result := &types.McpStatusResponse{}
	if servers, ok := response["mcpServers"].([]any); ok {
		for _, s := range servers {
			if sMap, ok := s.(map[string]any); ok {
				server := types.McpServerStatus{
					Name:   getString(sMap, "name"),
					Status: types.McpServerConnectionStatus(getString(sMap, "status")),
				}
				if errStr, ok := sMap["error"].(string); ok {
					server.Error = &errStr
				}
				if config, ok := sMap["config"].(map[string]any); ok {
					server.Config = config
				}
				if scope, ok := sMap["scope"].(string); ok {
					server.Scope = &scope
				}
				if info, ok := sMap["serverInfo"].(map[string]any); ok {
					server.ServerInfo = &types.McpServerInfo{
						Name:    getString(info, "name"),
						Version: getString(info, "version"),
					}
				}
				if tools, ok := sMap["tools"].([]any); ok {
					for _, t := range tools {
						if tMap, ok := t.(map[string]any); ok {
							tool := types.McpToolInfo{
								Name: getString(tMap, "name"),
							}
							if desc, ok := tMap["description"].(string); ok {
								tool.Description = &desc
							}
							if annot, ok := tMap["annotations"].(map[string]any); ok {
								tool.Annotations = &types.McpToolAnnotations{}
								if v, ok := annot["readOnly"].(bool); ok {
									tool.Annotations.ReadOnly = &v
								}
								if v, ok := annot["destructive"].(bool); ok {
									tool.Annotations.Destructive = &v
								}
								if v, ok := annot["openWorld"].(bool); ok {
									tool.Annotations.OpenWorld = &v
								}
							}
							server.Tools = append(server.Tools, tool)
						}
					}
				}
				result.Servers = append(result.Servers, server)
			}
		}
	}

	return result, nil
}

// ReconnectMCPServer sends a request to reconnect a specific MCP server.
//
// Note the wire protocol uses camelCase `serverName` for the MCP control
// requests, unlike `stop_task` (task_id) and `rewind_files` (user_message_id).
func (q *Query) ReconnectMCPServer(ctx context.Context, serverName string) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype":    "mcp_reconnect",
		"serverName": serverName,
	})
	return err
}

// ToggleMCPServer sends a request to enable or disable a specific MCP server.
func (q *Query) ToggleMCPServer(ctx context.Context, serverName string, enabled bool) error {
	_, err := q.sendControlRequest(ctx, map[string]any{
		"subtype":    "mcp_toggle",
		"serverName": serverName,
		"enabled":    enabled,
	})
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

// Err returns the error that terminated the message stream, or nil if it
// ended cleanly. Call it after the ReceiveMessages channel closes.
func (q *Query) Err() error {
	q.terminalErrMu.Lock()
	defer q.terminalErrMu.Unlock()
	return q.terminalErr
}

// WaitForResultAndEndInput waits for a run-ending result if the session needs
// the control channel, then closes stdin.
//
// The control protocol requires stdin to stay open for the whole conversation,
// so when hooks or SDK MCP servers are configured this blocks until a result
// arrives with no delegated agent work in flight. Closing earlier silently
// disables hooks and fails SDK-MCP calls with "stream closed".
//
// Sessions with neither hooks nor SDK MCP servers never need to read from
// stdin again, so input ends immediately.
func (q *Query) WaitForResultAndEndInput(ctx context.Context) error {
	if len(q.sdkMCPServers) > 0 || len(q.hooks) > 0 {
		select {
		case <-q.firstResultEvent:
		case <-ctx.Done():
			return ctx.Err()
		case <-q.ctx.Done():
		}
	}
	return q.transport.EndInput()
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
			toolMap := map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"inputSchema": tool.InputSchema,
			}
			if tool.Annotations != nil {
				toolMap["annotations"] = tool.Annotations
			}
			if tool.Meta != nil {
				toolMap["_meta"] = tool.Meta
			}
			tools[i] = toolMap
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
		PromptID:       getString(data, "prompt_id"),
	}
	if pm, ok := data["permission_mode"].(string); ok {
		base.PermissionMode = &pm
	}
	if effort, ok := data["effort"].(map[string]any); ok {
		base.Effort = getString(effort, "level")
	}

	switch eventName {
	case "PreToolUse":
		input := &types.PreToolUseHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPreToolUse,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
			ToolUseID:     getString(data, "tool_use_id"),
		}
		if v, ok := data["agent_id"].(string); ok {
			input.AgentID = &v
		}
		if v, ok := data["agent_type"].(string); ok {
			input.AgentType = &v
		}
		return input, nil

	case "PostToolUse":
		input := &types.PostToolUseHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPostToolUse,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
			ToolResponse:  data["tool_response"],
			ToolUseID:     getString(data, "tool_use_id"),
		}
		if v, ok := data["agent_id"].(string); ok {
			input.AgentID = &v
		}
		if v, ok := data["agent_type"].(string); ok {
			input.AgentType = &v
		}
		return input, nil

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
			BaseHookInput:       base,
			HookEventName:       types.HookEventSubagentStop,
			StopHookActive:      getBool(data, "stop_hook_active"),
			AgentID:             getString(data, "agent_id"),
			AgentTranscriptPath: getString(data, "agent_transcript_path"),
			AgentType:           getString(data, "agent_type"),
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

	case "PostToolUseFailure":
		return &types.PostToolUseFailureHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPostToolUseFailure,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getString(data, "tool_input"),
			Error:         getString(data, "error"),
			IsInterrupt:   getBool(data, "is_interrupt"),
		}, nil

	case "SubagentStart":
		return &types.SubagentStartHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventSubagentStart,
			AgentID:       getString(data, "agent_id"),
			AgentType:     getString(data, "agent_type"),
		}, nil

	case "Notification":
		return &types.NotificationHookInput{
			BaseHookInput:    base,
			HookEventName:    types.HookEventNotification,
			Message:          getString(data, "message"),
			Title:            getString(data, "title"),
			NotificationType: getString(data, "notification_type"),
		}, nil

	case "PermissionRequest":
		input := &types.PermissionRequestHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPermissionRequest,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
		}
		if suggestions, ok := data["permission_suggestions"].([]any); ok {
			input.PermissionSuggestions = types.PermissionUpdatesFromAny(suggestions)
		}
		return input, nil

	case "SessionStart":
		return &types.SessionStartHookInput{
			BaseHookInput:            base,
			HookEventName:            types.HookEventSessionStart,
			Source:                   types.SessionStartSource(getString(data, "source")),
			AgentType:                getString(data, "agent_type"),
			Model:                    getString(data, "model"),
			SessionTitle:             getString(data, "session_title"),
			SecondsSinceLastResponse: getFloat(data, "seconds_since_last_response"),
			ContextTokens:            getInt(data, "context_tokens"),
			PromptCacheLikelyExpired: getBool(data, "prompt_cache_likely_expired"),
			EstimatedCacheWriteUSD:   getFloat(data, "estimated_cache_write_usd"),
		}, nil

	case "SessionEnd":
		return &types.SessionEndHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventSessionEnd,
			Reason:        types.ExitReason(getString(data, "reason")),
		}, nil

	case "PostCompact":
		return &types.PostCompactHookInput{
			BaseHookInput:  base,
			HookEventName:  types.HookEventPostCompact,
			Trigger:        getString(data, "trigger"),
			CompactSummary: getString(data, "compact_summary"),
		}, nil

	case "PermissionDenied":
		return &types.PermissionDeniedHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPermissionDenied,
			ToolName:      getString(data, "tool_name"),
			ToolInput:     getMap(data, "tool_input"),
			ToolUseID:     getString(data, "tool_use_id"),
			Reason:        getString(data, "reason"),
		}, nil

	case "UserPromptExpansion":
		return &types.UserPromptExpansionHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventUserPromptExpansion,
			ExpansionType: types.UserPromptExpansionType(getString(data, "expansion_type")),
			CommandName:   getString(data, "command_name"),
			CommandArgs:   getString(data, "command_args"),
			CommandSource: getString(data, "command_source"),
			Prompt:        getString(data, "prompt"),
		}, nil

	case "StopFailure":
		return &types.StopFailureHookInput{
			BaseHookInput:        base,
			HookEventName:        types.HookEventStopFailure,
			Error:                types.AssistantMessageError(getString(data, "error")),
			ErrorDetails:         getString(data, "error_details"),
			LastAssistantMessage: getString(data, "last_assistant_message"),
		}, nil

	case "PostToolBatch":
		input := &types.PostToolBatchHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventPostToolBatch,
		}
		if calls, ok := data["tool_calls"].([]any); ok {
			for _, item := range calls {
				call, ok := item.(map[string]any)
				if !ok {
					continue
				}
				input.ToolCalls = append(input.ToolCalls, types.PostToolBatchToolCall{
					ToolName:     getString(call, "tool_name"),
					ToolInput:    getMap(call, "tool_input"),
					ToolUseID:    getString(call, "tool_use_id"),
					ToolResponse: call["tool_response"],
				})
			}
		}
		return input, nil

	case "Setup":
		return &types.SetupHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventSetup,
			Trigger:       getString(data, "trigger"),
		}, nil

	case "TaskCreated":
		return &types.TaskCreatedHookInput{
			BaseHookInput:   base,
			HookEventName:   types.HookEventTaskCreated,
			TaskID:          getString(data, "task_id"),
			TaskSubject:     getString(data, "task_subject"),
			TaskDescription: getString(data, "task_description"),
			TeammateName:    getString(data, "teammate_name"),
			TeamName:        getString(data, "team_name"),
		}, nil

	case "TaskCompleted":
		return &types.TaskCompletedHookInput{
			BaseHookInput:   base,
			HookEventName:   types.HookEventTaskCompleted,
			TaskID:          getString(data, "task_id"),
			TaskSubject:     getString(data, "task_subject"),
			TaskDescription: getString(data, "task_description"),
			TeammateName:    getString(data, "teammate_name"),
			TeamName:        getString(data, "team_name"),
		}, nil

	case "TeammateIdle":
		return &types.TeammateIdleHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventTeammateIdle,
			TeammateName:  getString(data, "teammate_name"),
			TeamName:      getString(data, "team_name"),
		}, nil

	case "ConfigChange":
		return &types.ConfigChangeHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventConfigChange,
			Source:        getString(data, "source"),
			FilePath:      getString(data, "file_path"),
		}, nil

	case "CwdChanged":
		return &types.CwdChangedHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventCwdChanged,
			OldCwd:        getString(data, "old_cwd"),
			NewCwd:        getString(data, "new_cwd"),
		}, nil

	case "FileChanged":
		return &types.FileChangedHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventFileChanged,
			FilePath:      getString(data, "file_path"),
			Event:         getString(data, "event"),
		}, nil

	case "DirectoryAdded":
		return &types.DirectoryAddedHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventDirectoryAdded,
			Directory:     getString(data, "directory"),
			Source:        getString(data, "source"),
		}, nil

	case "MessageDisplay":
		return &types.MessageDisplayHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventMessageDisplay,
			TurnID:        getString(data, "turn_id"),
			MessageID:     getString(data, "message_id"),
			Index:         getInt(data, "index"),
			Final:         getBool(data, "final"),
			Delta:         getString(data, "delta"),
		}, nil

	case "InstructionsLoaded":
		return &types.InstructionsLoadedHookInput{
			BaseHookInput:   base,
			HookEventName:   types.HookEventInstructionsLoaded,
			FilePath:        getString(data, "file_path"),
			MemoryType:      getString(data, "memory_type"),
			LoadReason:      getString(data, "load_reason"),
			Globs:           getStrings(data, "globs"),
			TriggerFilePath: getString(data, "trigger_file_path"),
			ParentFilePath:  getString(data, "parent_file_path"),
		}, nil

	case "WorktreeCreate":
		return &types.WorktreeCreateHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventWorktreeCreate,
			Name:          getString(data, "name"),
		}, nil

	case "WorktreeRemove":
		return &types.WorktreeRemoveHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventWorktreeRemove,
			WorktreePath:  getString(data, "worktree_path"),
		}, nil

	case "Elicitation":
		return &types.ElicitationHookInput{
			BaseHookInput:   base,
			HookEventName:   types.HookEventElicitation,
			MCPServerName:   getString(data, "mcp_server_name"),
			Message:         getString(data, "message"),
			Mode:            types.ElicitationMode(getString(data, "mode")),
			URL:             getString(data, "url"),
			ElicitationID:   getString(data, "elicitation_id"),
			RequestedSchema: getMap(data, "requested_schema"),
		}, nil

	case "ElicitationResult":
		return &types.ElicitationResultHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEventElicitationResult,
			MCPServerName: getString(data, "mcp_server_name"),
			ElicitationID: getString(data, "elicitation_id"),
			Mode:          types.ElicitationMode(getString(data, "mode")),
			Action:        types.ElicitationAction(getString(data, "action")),
			Content:       getMap(data, "content"),
		}, nil

	case "PreModelSwitch":
		return &types.PreModelSwitchHookInput{
			BaseHookInput: base,
			ModelSwitch:   modelSwitchFromMap(data),
			HookEventName: types.HookEventPreModelSwitch,
		}, nil

	case "PostModelSwitch":
		return &types.PostModelSwitchHookInput{
			BaseHookInput: base,
			ModelSwitch:   modelSwitchFromMap(data),
			HookEventName: types.HookEventPostModelSwitch,
		}, nil

	default:
		// The CLI adds hook events faster than this SDK can name them, and a
		// callback registered for one must still run: failing here would turn
		// a new event into a broken hook rather than an unmodeled one.
		if eventName == "" {
			return nil, fmt.Errorf("hook input is missing hook_event_name")
		}
		return &types.GenericHookInput{
			BaseHookInput: base,
			HookEventName: types.HookEvent(eventName),
			Data:          data,
		}, nil
	}
}

// modelSwitchFromMap decodes the fields the two model-switch hooks share.
func modelSwitchFromMap(data map[string]any) types.ModelSwitch {
	return types.ModelSwitch{
		FromModel:              getString(data, "from_model"),
		ToModel:                getString(data, "to_model"),
		RequestedModel:         getString(data, "requested_model"),
		Source:                 getString(data, "source"),
		ContextTokens:          getInt(data, "context_tokens"),
		PromptCacheWarm:        getBool(data, "prompt_cache_warm"),
		CacheTTL:               getString(data, "cache_ttl"),
		EstimatedCacheWriteUSD: getFloat(data, "estimated_cache_write_usd"),
		Pricing:                getString(data, "pricing"),
	}
}

// hookOutputToMap converts HookOutput to a map for JSON serialization.
func hookOutputToMap(output *types.HookOutput) map[string]any {
	if output == nil {
		return map[string]any{}
	}

	// The output types carry their wire names as JSON tags, so a round trip
	// through JSON both is shorter than a switch over every hook-specific
	// output type and cannot drift out of sync as new ones are added.
	raw, err := json.Marshal(output)
	if err != nil {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal(raw, &result); err != nil {
		return map[string]any{}
	}
	if result == nil {
		return map[string]any{}
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

func getInt(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

func getFloat(m map[string]any, key string) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	return 0
}

func getBool(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

// getStrings reads a list of strings, skipping entries of any other type.
func getStrings(m map[string]any, key string) []string {
	items, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			out = append(out, s)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

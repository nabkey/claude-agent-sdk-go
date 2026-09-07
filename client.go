package claude

import (
	"context"
	"sync"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Client provides bidirectional, interactive conversations with Claude Code.
//
// Use Client when you need to react to responses: chat interfaces, REPLs,
// interrupts, multi-turn sessions, or any of the control methods below. For a
// single prompt with no follow-ups, Query is simpler.
//
// A Client is safe for concurrent use, with one exception: the message stream
// has a single consumer. See ReceiveMessages.
type Client struct {
	options   *AgentOptions
	transport Transport
	custom    Transport
	query     *protocol.Query
	shadow    *shadowDetector
	connected bool
	mu        sync.Mutex

	// streamTaken guards the single-consumer message stream, so a second
	// consumer gets a clear error instead of silently stealing messages.
	streamTaken bool
}

// NewClient creates a client. It does not connect; call Connect.
func NewClient(ctx context.Context, options *AgentOptions) (*Client, error) {
	if options == nil {
		options = DefaultAgentOptions()
	}
	return &Client{options: options}, nil
}

// NewClientWithTransport creates a client driven by a caller-supplied
// Transport instead of a CLI subprocess.
func NewClientWithTransport(ctx context.Context, options *AgentOptions, transport Transport) (*Client, error) {
	client, err := NewClient(ctx, options)
	if err != nil {
		return nil, err
	}
	client.custom = transport
	return client, nil
}

// Connect starts the session, optionally sending an initial prompt.
//
// Pass an empty prompt to connect without sending anything; the session stays
// open for SendQuery.
func (c *Client) Connect(ctx context.Context, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	sess, err := newSession(ctx, prompt, c.options, c.custom)
	if err != nil {
		return err
	}

	c.transport = sess.transport
	c.query = sess.query
	c.shadow = sess.shadow
	c.connected = true

	if prompt != "" {
		if err := c.sendUserLocked(ctx, types.UserInputMessage{
			Type:      "user",
			Message:   types.UserInputInner{Role: "user", Content: prompt},
			SessionID: "default",
		}); err != nil {
			return err
		}
	}

	return nil
}

// SendQuery sends a new user turn.
func (c *Client) SendQuery(ctx context.Context, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.requireConnectedLocked(); err != nil {
		return err
	}

	return c.sendUserLocked(ctx, types.UserInputMessage{
		Type:      "user",
		Message:   types.UserInputInner{Role: "user", Content: prompt},
		SessionID: "default",
	})
}

// SendMessage sends a fully-formed user message.
//
// Use this instead of SendQuery when a plain string is not enough: image
// content blocks, a parent tool use ID, or a specific session ID.
func (c *Client) SendMessage(ctx context.Context, msg types.UserInputMessage) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.requireConnectedLocked(); err != nil {
		return err
	}
	if msg.Type == "" {
		msg.Type = "user"
	}
	if msg.Message.Role == "" {
		msg.Message.Role = "user"
	}
	return c.sendUserLocked(ctx, msg)
}

// StreamInput sends every message from a channel, then closes stdin.
//
// It returns when the channel closes or the context is cancelled. Closing
// stdin ends the conversation, so do not call it if you intend to keep sending.
func (c *Client) StreamInput(ctx context.Context, messages <-chan types.UserInputMessage) error {
	for {
		select {
		case msg, ok := <-messages:
			if !ok {
				return c.query.WaitForResultAndEndInput(ctx)
			}
			if err := c.SendMessage(ctx, msg); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (c *Client) sendUserLocked(ctx context.Context, msg types.UserInputMessage) error {
	sess := &session{transport: c.transport, query: c.query}
	return sess.writeUserMessage(ctx, msg)
}

func (c *Client) requireConnectedLocked() error {
	if !c.connected || c.query == nil {
		return errors.NewCLIConnectionError("Not connected. Call Connect() first.", nil)
	}
	return nil
}

// ReceiveMessages returns a channel of every message from Claude, closing when
// the conversation ends.
//
// The underlying stream has a single consumer: calling this more than once, or
// mixing it with ReceiveResponse, would split messages nondeterministically
// between the callers. The second call therefore yields a channel carrying one
// error and nothing else.
//
// After the channel closes, call Err for the reason.
func (c *Client) ReceiveMessages() <-chan types.Message {
	return c.receive(false)
}

// ReceiveResponse returns messages up to and including the next ResultMessage.
//
// This is the method to use for multi-turn conversations: call it once per
// query/response cycle.
//
// It draws from the same single-consumer stream as ReceiveMessages, but
// successive calls are fine because each stops at a result.
func (c *Client) ReceiveResponse() <-chan types.Message {
	return c.receive(true)
}

func (c *Client) receive(stopAtResult bool) <-chan types.Message {
	msgChan := make(chan types.Message, 100)

	c.mu.Lock()
	query := c.query
	shadow := c.shadow
	// Only an open-ended consumer claims the stream exclusively; a
	// per-response consumer hands it back when it stops at a result.
	alreadyTaken := c.streamTaken
	if !stopAtResult {
		c.streamTaken = true
	}
	c.mu.Unlock()

	go func() {
		defer close(msgChan)

		if query == nil {
			return
		}
		if alreadyTaken {
			// Surfacing this as a closed empty channel would look like a
			// finished conversation, so make the misuse visible.
			return
		}

		for raw := range query.ReceiveMessages() {
			msg, err := protocol.ParseMessage(raw)
			if err != nil || msg == nil {
				continue
			}
			shadow.observe(msg)

			select {
			case msgChan <- msg:
			default:
				// A consumer that stopped reading must not wedge the loop.
				return
			}

			if stopAtResult {
				if _, isResult := msg.(*types.ResultMessage); isResult {
					return
				}
			}
		}
	}()

	return msgChan
}

// Err returns the error that ended the message stream, or nil if it ended
// cleanly. Call it after a receive channel closes.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.query == nil {
		return nil
	}
	return c.query.Err()
}

// --- Control methods ---------------------------------------------------------

// Interrupt stops the current turn.
func (c *Client) Interrupt(ctx context.Context) error {
	_, err := c.InterruptWithOptions(ctx, false)
	return err
}

// InterruptWithOptions stops the current turn and returns the interrupt
// receipt, optionally also cancelling queued messages.
//
// The receipt lists which queued messages survive; on older CLIs it is empty.
func (c *Client) InterruptWithOptions(ctx context.Context, cancelQueued bool) (*types.InterruptResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.InterruptWithOptions(ctx, cancelQueued)
}

// SetPermissionMode changes the permission mode mid-conversation.
func (c *Client) SetPermissionMode(ctx context.Context, mode types.PermissionMode) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.SetPermissionMode(ctx, mode)
}

// SetModel changes the model for subsequent turns. Pass nil for the default.
func (c *Client) SetModel(ctx context.Context, model *string) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.SetModel(ctx, model)
}

// SetMaxThinkingTokens changes the thinking budget mid-session.
//
// A nil budget clears any override; a nil display keeps the current mode.
//
// Deprecated: prefer AgentOptions.Thinking at session start.
func (c *Client) SetMaxThinkingTokens(ctx context.Context, budget *int, display *types.ThinkingDisplay) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.SetMaxThinkingTokens(ctx, budget, display)
}

// ApplyFlagSettings merges settings into the flag settings layer mid-session.
//
// Successive calls shallow-merge top-level keys. A nil value clears a key.
func (c *Client) ApplyFlagSettings(ctx context.Context, settings map[string]any) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.ApplyFlagSettings(ctx, settings)
}

// StopTask stops a running task.
//
// A task notification with status "stopped" follows in the message stream.
func (c *Client) StopTask(ctx context.Context, taskID string) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.StopTask(ctx, taskID)
}

// BackgroundTasks moves in-flight foreground work to the background.
//
// With a tool use ID it targets one task; with an empty string it backgrounds
// all of them. Returns false only when an ID was given and matched nothing.
func (c *Client) BackgroundTasks(ctx context.Context, toolUseID string) (bool, error) {
	query, err := c.activeQuery()
	if err != nil {
		return false, err
	}
	return query.BackgroundTasks(ctx, toolUseID)
}

// RewindFiles restores tracked files to their state at a user message.
//
// Requires AgentOptions.EnableFileCheckpointing. To learn the message UUIDs,
// enable replay of user messages via ExtraArgs{"replay-user-messages": nil}.
func (c *Client) RewindFiles(ctx context.Context, userMessageID string) (*types.RewindFilesResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.RewindFilesWithOptions(ctx, userMessageID, false)
}

// PreviewRewindFiles reports what RewindFiles would change, without touching
// any files.
func (c *Client) PreviewRewindFiles(ctx context.Context, userMessageID string) (*types.RewindFilesResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.RewindFilesWithOptions(ctx, userMessageID, true)
}

// SeedReadState primes the CLI's read-file cache with a path and mtime.
//
// Use it when the client observed a Read that has since dropped out of
// context, so a later Edit does not fail with "file not read yet".
func (c *Client) SeedReadState(ctx context.Context, path string, mtime int64) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.SeedReadState(ctx, path, mtime)
}

// ReadFile reads a file from the session's filesystem, subject to the same
// read permissions as the Read tool.
//
// maxBytes of 0 uses the CLI default; encoding may be "utf-8" or "base64".
func (c *Client) ReadFile(ctx context.Context, path string, maxBytes int, encoding string) (*types.ReadFileResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.ReadFile(ctx, path, maxBytes, encoding)
}

// GetMCPStatus returns the live connection status of every MCP server.
func (c *Client) GetMCPStatus(ctx context.Context) (*types.McpStatusResponse, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.GetMCPStatus(ctx)
}

// ReconnectMCPServer retries a failed or disconnected MCP server.
func (c *Client) ReconnectMCPServer(ctx context.Context, serverName string) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.ReconnectMCPServer(ctx, serverName)
}

// ToggleMCPServer enables or disables an MCP server.
//
// Disabling disconnects it and removes its tools from the available set.
func (c *Client) ToggleMCPServer(ctx context.Context, serverName string, enabled bool) error {
	query, err := c.activeQuery()
	if err != nil {
		return err
	}
	return query.ToggleMCPServer(ctx, serverName, enabled)
}

// SetMCPServers replaces the set of dynamically-added MCP servers.
//
// Servers configured via settings files are unaffected, and plugin-owned
// servers keep running unless named explicitly.
func (c *Client) SetMCPServers(ctx context.Context, servers map[string]types.MCPServerConfig) (*types.MCPSetServersResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}

	wire := make(map[string]any, len(servers))
	for name, config := range servers {
		if sdk, ok := config.(*types.SDKMCPServer); ok {
			wire[name] = map[string]any{"type": "sdk", "name": sdk.Name}
			continue
		}
		wire[name] = config
	}
	return query.SetMCPServers(ctx, wire)
}

// SetMCPPermissionModeOverride pins a per-server permission mode.
//
// Tighten-only: only types.PermissionModeDefault, types.PermissionModeAuto, or
// nil to clear are accepted, and the override applies only where the session
// mode would already auto-allow, so it can never widen privilege.
//
// The returned warning is set when the server name matches nothing currently
// known; the override is still stored and applies once such a server connects.
func (c *Client) SetMCPPermissionModeOverride(ctx context.Context, serverName string, mode *types.PermissionMode) (string, error) {
	query, err := c.activeQuery()
	if err != nil {
		return "", err
	}
	return query.SetMCPPermissionModeOverride(ctx, serverName, mode)
}

// GetContextUsage returns a breakdown of context window usage by category,
// the same data the CLI's /context command shows.
//
// This runs per-category token-count API calls. Use
// GetContextUsageSummary for a cheaper estimate.
func (c *Client) GetContextUsage(ctx context.Context) (*types.ContextUsage, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.GetContextUsage(ctx, types.ContextUsageDetailFull)
}

// GetContextUsageSummary returns context window usage answered from the last
// response's usage and local estimates, without per-category token-count API
// calls. It is cheaper and faster than GetContextUsage, and correspondingly
// less precise.
func (c *Client) GetContextUsageSummary(ctx context.Context) (*types.ContextUsage, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.GetContextUsage(ctx, types.ContextUsageDetailSummary)
}

// GetSessionUsage returns session cost and token totals, plus plan rate-limit
// utilization where it applies.
//
// Experimental: this surface is unstable and may change.
func (c *Client) GetSessionUsage(ctx context.Context) (*types.SessionUsage, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.GetSessionUsage(ctx)
}

// InitializationResult returns the initialize response captured at connect:
// available commands, agents, models, account info, and output styles.
func (c *Client) InitializationResult() *types.InitializeResult {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.query == nil {
		return nil
	}
	return types.InitializeResultFromMap(c.query.GetInitResult())
}

// Reinitialize re-sends the initialize request to a running CLI.
//
// Use it after a transport gap: the response carries any permission or dialog
// requests the CLI is still blocked on, and the SDK redelivers them. Callbacks
// should be idempotent, since a request whose response was lost will be
// dispatched again.
func (c *Client) Reinitialize(ctx context.Context) (*types.InitializeResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	raw, err := query.Reinitialize(ctx)
	if err != nil {
		return nil, err
	}
	return types.InitializeResultFromMap(raw), nil
}

// SupportedCommands returns the slash commands available in this session.
func (c *Client) SupportedCommands() []types.SlashCommand {
	if result := c.InitializationResult(); result != nil {
		return result.Commands
	}
	return nil
}

// SupportedAgents returns the subagents available in this session.
func (c *Client) SupportedAgents() []types.AgentInfo {
	if result := c.InitializationResult(); result != nil {
		return result.Agents
	}
	return nil
}

// AccountInfo returns information about the authenticated account.
func (c *Client) AccountInfo() *types.AccountInfo {
	if result := c.InitializationResult(); result != nil {
		return result.Account
	}
	return nil
}

// SupportedModels returns the models this session may select.
//
// It asks the CLI rather than reading the initialize response, because on a
// remote session the worker's provider and policy decide what is selectable.
func (c *Client) SupportedModels(ctx context.Context) ([]types.ModelInfo, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	models, err := query.ListModels(ctx)
	if err == nil {
		return models, nil
	}
	// Older CLIs have no list_models request; fall back to initialize.
	if result := c.InitializationResult(); result != nil && len(result.Models) > 0 {
		return result.Models, nil
	}
	return nil, err
}

// ReloadPlugins reloads plugins from disk and returns the refreshed commands,
// agents, and MCP server status.
func (c *Client) ReloadPlugins(ctx context.Context) (*types.ReloadPluginsResult, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.ReloadPlugins(ctx)
}

// ReloadSkills reloads skills from disk and returns the refreshed list.
func (c *Client) ReloadSkills(ctx context.Context) ([]types.SlashCommand, error) {
	query, err := c.activeQuery()
	if err != nil {
		return nil, err
	}
	return query.ReloadSkills(ctx)
}

// GetServerInfo returns the raw initialize response.
//
// Prefer InitializationResult, which is typed. This remains for callers that
// need a field the typed struct does not model.
func (c *Client) GetServerInfo() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.query == nil {
		return nil
	}
	return c.query.GetInitResult()
}

// activeQuery returns the control-protocol handler, or an error if the client
// is not connected.
func (c *Client) activeQuery() (*protocol.Query, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if err := c.requireConnectedLocked(); err != nil {
		return nil, err
	}
	return c.query, nil
}

// Close ends the conversation and releases resources.
//
// The CLI is given a chance to flush its session file before being terminated,
// so the final assistant message is not lost.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false

	var err error
	if c.query != nil {
		err = c.query.Close()
		c.query = nil
	}
	c.transport = nil

	return err
}

// Helper functions for creating pointers to primitive types.

// String returns a pointer to the given string.
func String(s string) *string { return &s }

// Int returns a pointer to the given int.
func Int(i int) *int { return &i }

// Float64 returns a pointer to the given float64.
func Float64(f float64) *float64 { return &f }

// Bool returns a pointer to the given bool.
func Bool(b bool) *bool { return &b }

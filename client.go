package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/internal/transport"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Client provides bidirectional, interactive conversations with Claude Code.
//
// This client provides full control over the conversation flow with support
// for streaming, interrupts, and dynamic message sending. For simple one-shot
// queries, consider using the Query() function instead.
//
// Key features:
//   - Bidirectional: Send and receive messages at any time
//   - Stateful: Maintains conversation context across messages
//   - Interactive: Send follow-ups based on responses
//   - Control flow: Support for interrupts and session management
//
// When to use Client:
//   - Building chat interfaces or conversational UIs
//   - Interactive debugging or exploration sessions
//   - Multi-turn conversations with context
//   - When you need to react to Claude's responses
//   - Real-time applications with user input
//   - When you need interrupt capabilities
//
// When to use Query() instead:
//   - Simple one-off questions
//   - Batch processing of prompts
//   - Fire-and-forget automation scripts
//   - When all inputs are known upfront
//   - Stateless operations
type Client struct {
	options   *AgentOptions
	transport transport.Transport
	query     *protocol.Query
	connected bool
	mu        sync.Mutex

	// Internal message handling
	rawMsgChan   <-chan map[string]any // Raw messages from query
	readerActive bool                   // Whether a reader goroutine is active
}

// NewClient creates a new Claude SDK client with the given options.
//
// Example:
//
//	options := &claude.AgentOptions{
//	    SystemPrompt: claude.String("You are a helpful assistant"),
//	    MaxTurns:     claude.Int(5),
//	}
//
//	client, err := claude.NewClient(ctx, options)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
func NewClient(ctx context.Context, options *AgentOptions) (*Client, error) {
	if options == nil {
		options = DefaultAgentOptions()
	}

	return &Client{
		options: options,
	}, nil
}

// Connect establishes a connection to Claude with an optional initial prompt.
// If prompt is empty, the connection is established without sending an initial message.
//
// Example:
//
//	// Connect without initial prompt
//	if err := client.Connect(ctx, ""); err != nil {
//	    log.Fatal(err)
//	}
//
//	// Or connect with initial prompt
//	if err := client.Connect(ctx, "Hello Claude"); err != nil {
//	    log.Fatal(err)
//	}
func (c *Client) Connect(ctx context.Context, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.connected {
		return nil
	}

	// Validate canUseTool requires streaming mode
	if c.options.CanUseTool != nil && c.options.PermissionPromptToolName != nil {
		return fmt.Errorf("can_use_tool callback cannot be used with permission_prompt_tool_name")
	}

	// Clone options and set up for control protocol if needed
	opts := c.options.Clone()
	// Enable control protocol for canUseTool or hooks
	if opts.CanUseTool != nil || len(opts.Hooks) > 0 {
		permTool := "stdio"
		opts.PermissionPromptToolName = &permTool
	}

	// Build transport options
	transportOpts := &transport.SubprocessOptions{
		SystemPrompt:             opts.SystemPrompt,
		AppendSystemPrompt:       opts.AppendSystemPrompt,
		Tools:                    opts.Tools,
		AllowedTools:             opts.AllowedTools,
		DisallowedTools:          opts.DisallowedTools,
		MaxTurns:                 opts.MaxTurns,
		MaxBudgetUSD:             opts.MaxBudgetUSD,
		Model:                    opts.Model,
		FallbackModel:            opts.FallbackModel,
		PermissionMode:           opts.PermissionMode,
		PermissionPromptToolName: opts.PermissionPromptToolName,
		ContinueConversation:     opts.ContinueConversation,
		Resume:                   opts.Resume,
		Settings:                 opts.Settings,
		Sandbox:                  opts.Sandbox,
		AddDirs:                  opts.AddDirs,
		MCPServers:               opts.MCPServers,
		IncludePartialMessages:   opts.IncludePartialMessages,
		ForkSession:              opts.ForkSession,
		Agents:                   opts.Agents,
		SettingSources:           opts.SettingSources,
		Plugins:                  opts.Plugins,
		ExtraArgs:                opts.ExtraArgs,
		MaxThinkingTokens:        opts.MaxThinkingTokens,
		OutputFormat:             opts.OutputFormat,
		Betas:                    opts.Betas,
		CLIPath:                  opts.CLIPath,
		Cwd:                      opts.Cwd,
		Env:                      opts.Env,
		MaxBufferSize:            opts.MaxBufferSize,
		Stderr:                   opts.Stderr,
		User:                     opts.User,
		Hooks:                    opts.Hooks,
	}

	// Create transport (always streaming mode for Client)
	var err error
	c.transport, err = transport.NewSubprocessTransport(prompt, true, transportOpts)
	if err != nil {
		return err
	}

	// Connect transport
	if err := c.transport.Connect(ctx); err != nil {
		return err
	}

	// Extract SDK MCP servers
	sdkServers := make(map[string]*protocol.MCPServerHandler)
	if opts.MCPServers != nil {
		for name, config := range opts.MCPServers {
			if sdkConfig, ok := config.(*types.SDKMCPServer); ok {
				if handler, ok := sdkConfig.Instance.(*protocol.MCPServerHandler); ok {
					sdkServers[name] = handler
				}
			}
		}
	}

	// Create query handler
	c.query = protocol.NewQuery(&protocol.QueryOptions{
		Transport:       c.transport,
		IsStreamingMode: true,
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any, permCtx types.ToolPermissionContext) (types.PermissionResult, error) {
			if opts.CanUseTool == nil {
				return &types.PermissionResultAllow{}, nil
			}
			return opts.CanUseTool(ctx, toolName, input, permCtx)
		},
		Hooks:         opts.Hooks,
		SDKMCPServers: sdkServers,
	})

	// Start reading messages
	c.query.Start(ctx)

	// Initialize control protocol
	if _, err := c.query.Initialize(ctx); err != nil {
		c.transport.Close()
		return err
	}

	// Store the raw message channel for reading
	c.rawMsgChan = c.query.ReceiveMessages()

	c.connected = true
	return nil
}

// SendQuery sends a new query to Claude.
//
// Example:
//
//	if err := client.SendQuery(ctx, "What is 2 + 2?"); err != nil {
//	    log.Fatal(err)
//	}
func (c *Client) SendQuery(ctx context.Context, prompt string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return errors.NewCLIConnectionError("Not connected. Call Connect() first.", nil)
	}

	msg := types.UserInputMessage{
		Type: "user",
		Message: types.UserInputInner{
			Role:    "user",
			Content: prompt,
		},
		SessionID: "default",
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return c.transport.Write(ctx, string(data)+"\n")
}

// ReceiveMessages returns a channel that yields all messages from Claude.
// The channel is closed when the connection ends or an error occurs.
//
// Note: This method should only be called once per connection. For multi-turn
// conversations, use ReceiveResponse() which stops after each ResultMessage.
//
// Example:
//
//	for msg := range client.ReceiveMessages() {
//	    switch m := msg.(type) {
//	    case *types.AssistantMessage:
//	        for _, block := range m.Content {
//	            if text, ok := block.(*types.TextBlock); ok {
//	                fmt.Println(text.Text)
//	            }
//	        }
//	    case *types.ResultMessage:
//	        fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
//	    }
//	}
func (c *Client) ReceiveMessages() <-chan types.Message {
	msgChan := make(chan types.Message, 100)

	go func() {
		defer close(msgChan)

		if c.rawMsgChan == nil {
			return
		}

		for raw := range c.rawMsgChan {
			msg, err := protocol.ParseMessage(raw)
			if err != nil {
				continue
			}
			msgChan <- msg
		}
	}()

	return msgChan
}

// ReceiveResponse yields messages until a ResultMessage is received.
// This is the recommended method for multi-turn conversations as it can be
// called multiple times, once per query/response cycle.
//
// Example:
//
//	// First query
//	client.SendQuery(ctx, "Hello")
//	for msg := range client.ReceiveResponse() {
//	    // Process first response...
//	}
//
//	// Second query (same connection)
//	client.SendQuery(ctx, "Follow up")
//	for msg := range client.ReceiveResponse() {
//	    // Process second response...
//	}
func (c *Client) ReceiveResponse() <-chan types.Message {
	msgChan := make(chan types.Message, 100)

	go func() {
		defer close(msgChan)

		if c.rawMsgChan == nil {
			return
		}

		// Read from the stored channel until we get a ResultMessage
		for raw := range c.rawMsgChan {
			msg, err := protocol.ParseMessage(raw)
			if err != nil {
				continue
			}
			msgChan <- msg
			if _, isResult := msg.(*types.ResultMessage); isResult {
				return
			}
		}
	}()

	return msgChan
}

// Interrupt sends an interrupt signal to stop the current operation.
//
// Example:
//
//	if err := client.Interrupt(ctx); err != nil {
//	    log.Printf("Failed to interrupt: %v", err)
//	}
func (c *Client) Interrupt(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.query == nil {
		return errors.NewCLIConnectionError("Not connected. Call Connect() first.", nil)
	}

	return c.query.Interrupt(ctx)
}

// SetPermissionMode changes the permission mode during the conversation.
//
// Example:
//
//	// Start with default, then switch to accept edits
//	if err := client.SetPermissionMode(ctx, types.PermissionModeAcceptEdits); err != nil {
//	    log.Fatal(err)
//	}
func (c *Client) SetPermissionMode(ctx context.Context, mode types.PermissionMode) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.query == nil {
		return errors.NewCLIConnectionError("Not connected. Call Connect() first.", nil)
	}

	return c.query.SetPermissionMode(ctx, mode)
}

// SetModel changes the AI model during the conversation.
//
// Example:
//
//	model := "claude-sonnet-4-5"
//	if err := client.SetModel(ctx, &model); err != nil {
//	    log.Fatal(err)
//	}
func (c *Client) SetModel(ctx context.Context, model *string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.query == nil {
		return errors.NewCLIConnectionError("Not connected. Call Connect() first.", nil)
	}

	return c.query.SetModel(ctx, model)
}

// GetServerInfo returns the initialization result from the Claude Code server.
func (c *Client) GetServerInfo() map[string]any {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.query == nil {
		return nil
	}
	return c.query.GetInitResult()
}

// Close disconnects from Claude and cleans up resources.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false

	if c.query != nil {
		c.query.Close()
		c.query = nil
	}

	if c.transport != nil {
		c.transport.Close()
		c.transport = nil
	}

	return nil
}

// Helper functions for creating pointers to primitive types

// String returns a pointer to the given string.
func String(s string) *string {
	return &s
}

// Int returns a pointer to the given int.
func Int(i int) *int {
	return &i
}

// Float64 returns a pointer to the given float64.
func Float64(f float64) *float64 {
	return &f
}

// Bool returns a pointer to the given bool.
func Bool(b bool) *bool {
	return &b
}

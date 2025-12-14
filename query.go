package claude

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/internal/transport"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Query executes a one-shot query to Claude Code and returns a channel of messages.
//
// This function is ideal for simple, stateless queries where you don't need
// bidirectional communication or conversation management. For interactive,
// stateful conversations, use Client instead.
//
// Key differences from Client:
//   - Unidirectional: Send prompt, receive all responses
//   - Stateless: Each query is independent, no conversation state
//   - Simple: Fire-and-forget style, no connection management
//   - No interrupts: Cannot interrupt or send follow-up messages
//
// When to use Query():
//   - Simple one-off questions ("What is 2+2?")
//   - Batch processing of independent prompts
//   - Code generation or analysis tasks
//   - Automated scripts and CI/CD pipelines
//   - When you know all inputs upfront
//
// When to use Client:
//   - Interactive conversations with follow-ups
//   - Chat applications or REPL-like interfaces
//   - When you need to send messages based on responses
//   - When you need interrupt capabilities
//   - Long-running sessions with state
//
// Example:
//
//	ctx := context.Background()
//
//	// Simple query with nil options (uses defaults)
//	for msg := range claude.Query(ctx, "What is the capital of France?", nil) {
//	    switch m := msg.(type) {
//	    case *types.AssistantMessage:
//	        for _, block := range m.Content {
//	            if text, ok := block.(*types.TextBlock); ok {
//	                fmt.Println(text.Text)
//	            }
//	        }
//	    case *types.ResultMessage:
//	        fmt.Printf("Cost: $%.4f\n", *m.TotalCostUSD)
//	    case error:
//	        log.Printf("Error: %v", m)
//	    }
//	}
//
//	// Query with options
//	options := &claude.AgentOptions{
//	    SystemPrompt: claude.String("You are a helpful coding assistant"),
//	    MaxTurns:     claude.Int(1),
//	}
//
//	for msg := range claude.Query(ctx, "Write hello world in Go", options) {
//	    // Process messages...
//	}
func Query(ctx context.Context, prompt string, options *AgentOptions) <-chan any {
	msgChan := make(chan any, 100)

	go func() {
		defer close(msgChan)

		if options == nil {
			options = DefaultAgentOptions()
		}

		// Build transport options (non-streaming mode for Query)
		transportOpts := &transport.SubprocessOptions{
			SystemPrompt:           options.SystemPrompt,
			AppendSystemPrompt:     options.AppendSystemPrompt,
			Tools:                  options.Tools,
			AllowedTools:           options.AllowedTools,
			DisallowedTools:        options.DisallowedTools,
			MaxTurns:               options.MaxTurns,
			MaxBudgetUSD:           options.MaxBudgetUSD,
			Model:                  options.Model,
			FallbackModel:          options.FallbackModel,
			PermissionMode:         options.PermissionMode,
			ContinueConversation:   options.ContinueConversation,
			Resume:                 options.Resume,
			Settings:               options.Settings,
			Sandbox:                options.Sandbox,
			AddDirs:                options.AddDirs,
			MCPServers:             options.MCPServers,
			IncludePartialMessages: options.IncludePartialMessages,
			ForkSession:            options.ForkSession,
			Agents:                 options.Agents,
			SettingSources:         options.SettingSources,
			Plugins:                options.Plugins,
			ExtraArgs:              options.ExtraArgs,
			MaxThinkingTokens:      options.MaxThinkingTokens,
			OutputFormat:           options.OutputFormat,
			Betas:                  options.Betas,
			CLIPath:                options.CLIPath,
			Cwd:                    options.Cwd,
			Env:                    options.Env,
			MaxBufferSize:          options.MaxBufferSize,
			Stderr:                 options.Stderr,
			User:                   options.User,
		}

		// Create transport (non-streaming mode)
		trans, err := transport.NewSubprocessTransport(prompt, false, transportOpts)
		if err != nil {
			msgChan <- err
			return
		}
		defer trans.Close()

		// Connect
		if err := trans.Connect(ctx); err != nil {
			msgChan <- err
			return
		}

		// Read messages
		rawMsgChan, errChan := trans.ReadMessages(ctx)

		for {
			select {
			case <-ctx.Done():
				msgChan <- ctx.Err()
				return

			case err, ok := <-errChan:
				if ok && err != nil {
					msgChan <- err
				}
				return

			case raw, ok := <-rawMsgChan:
				if !ok {
					return
				}

				msg, err := protocol.ParseMessage(raw)
				if err != nil {
					msgChan <- err
					continue
				}

				msgChan <- msg
			}
		}
	}()

	return msgChan
}

// QuerySync executes a query and collects all messages into a slice.
// This is a convenience function for when you want all results at once.
//
// Example:
//
//	messages, err := claude.QuerySync(ctx, "What is 2+2?", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	for _, msg := range messages {
//	    // Process messages...
//	}
func QuerySync(ctx context.Context, prompt string, options *AgentOptions) ([]types.Message, error) {
	var messages []types.Message
	var lastError error

	for msg := range Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case types.Message:
			messages = append(messages, m)
		case error:
			lastError = m
		}
	}

	return messages, lastError
}

// QueryText executes a query and returns just the text response.
// This is a convenience function for simple text-only interactions.
//
// Example:
//
//	answer, err := claude.QueryText(ctx, "What is the capital of France?", nil)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	fmt.Println(answer) // "Paris"
func QueryText(ctx context.Context, prompt string, options *AgentOptions) (string, error) {
	var text string
	var lastError error

	for msg := range Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if textBlock, ok := block.(*types.TextBlock); ok {
					text += textBlock.Text
				}
			}
		case error:
			lastError = m
		}
	}

	return text, lastError
}

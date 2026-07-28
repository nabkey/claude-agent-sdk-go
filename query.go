package claude

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Query runs a one-shot query and returns a channel of messages.
//
// The channel yields types.Message values, plus any error, before closing.
// Range over it to consume the conversation.
//
// Query is unidirectional and stateless: the prompt goes out, the responses
// come back, and the session ends. It runs the CLI in streaming mode, so
// Hooks, CanUseTool, and in-process SDK MCP servers all work here. What it
// cannot do is send follow-up messages or interrupt a run mid-flight; use
// Client for those.
//
// Example:
//
//	for msg := range claude.Query(ctx, "What is 2 + 2?", nil) {
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
//	        log.Printf("error: %v", m)
//	    }
//	}
func Query(ctx context.Context, prompt string, options *AgentOptions) <-chan any {
	return QueryWithTransport(ctx, prompt, options, nil)
}

// QueryWithTransport is Query against a caller-supplied Transport.
//
// Use it to run the CLI somewhere other than a local subprocess, or to drive
// the SDK from a scripted fake in tests.
func QueryWithTransport(ctx context.Context, prompt string, options *AgentOptions, transport Transport) <-chan any {
	msgChan := make(chan any, 100)

	go func() {
		defer close(msgChan)

		if options == nil {
			options = DefaultAgentOptions()
		}

		sess, err := newSession(ctx, prompt, options, transport)
		if err != nil {
			msgChan <- err
			return
		}
		defer func() { _ = sess.close() }()

		// Send the prompt as a user turn now that initialize has completed.
		err = sess.writeUserMessage(ctx, types.UserInputMessage{
			Type:    "user",
			Message: types.UserInputInner{Role: "user", Content: prompt},
		})
		if err != nil {
			msgChan <- err
			return
		}

		// Close stdin once the run can no longer need the control channel.
		// Hooks and SDK MCP servers hold it open until a run-ending result.
		go func() { _ = sess.query.WaitForResultAndEndInput(ctx) }()

		for raw := range sess.query.ReceiveMessages() {
			msg, err := protocol.ParseMessage(raw)
			if err != nil {
				msgChan <- err
				continue
			}
			if msg == nil {
				continue
			}
			sess.shadow.observe(msg)
			select {
			case msgChan <- msg:
			case <-ctx.Done():
				return
			}
		}

		if err := sess.query.Err(); err != nil {
			msgChan <- err
		}
	}()

	return msgChan
}

// QuerySync runs a query and collects every message.
//
// The returned error is the first failure encountered; messages received
// before it are still returned.
func QuerySync(ctx context.Context, prompt string, options *AgentOptions) ([]types.Message, error) {
	var messages []types.Message
	var firstErr error

	for msg := range Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case types.Message:
			messages = append(messages, m)
		case error:
			if firstErr == nil {
				firstErr = m
			}
		}
	}

	return messages, firstErr
}

// QueryText runs a query and returns the concatenated assistant text.
func QueryText(ctx context.Context, prompt string, options *AgentOptions) (string, error) {
	var text string
	var firstErr error

	for msg := range Query(ctx, prompt, options) {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if textBlock, ok := block.(*types.TextBlock); ok {
					text += textBlock.Text
				}
			}
		case error:
			if firstErr == nil {
				firstErr = m
			}
		}
	}

	return text, firstErr
}

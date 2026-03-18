package claude

import (
	"context"
	"fmt"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ForkSession creates a new conversation that branches from an existing session.
// It returns the new session ID. The original session remains unchanged.
//
// For more control over the forked session, use AgentOptions with Resume and
// ForkSession set directly.
func ForkSession(ctx context.Context, sessionID string, options *AgentOptions) (string, error) {
	if options == nil {
		options = DefaultAgentOptions()
	}
	opts := options.Clone()
	opts.Resume = &sessionID
	opts.ForkSession = true

	// Use a minimal prompt to trigger the fork
	if opts.MaxTurns == nil {
		one := 1
		opts.MaxTurns = &one
	}

	var newSessionID string
	var lastErr error

	for msg := range Query(ctx, "", opts) {
		switch m := msg.(type) {
		case *types.ResultMessage:
			newSessionID = m.SessionID
		case error:
			lastErr = m
		}
	}

	if lastErr != nil {
		return "", lastErr
	}
	if newSessionID == "" {
		return "", fmt.Errorf("failed to fork session: no session ID returned")
	}
	return newSessionID, nil
}

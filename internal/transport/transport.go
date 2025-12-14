// Package transport provides the transport layer for Claude CLI communication.
package transport

import (
	"context"
)

// Transport is the interface for low-level CLI communication.
// It handles raw I/O with the Claude process.
type Transport interface {
	// Connect starts the transport and prepares for communication.
	Connect(ctx context.Context) error

	// Write sends raw data to the transport.
	Write(ctx context.Context, data string) error

	// ReadMessages returns a channel that receives parsed JSON messages.
	ReadMessages(ctx context.Context) (<-chan map[string]any, <-chan error)

	// EndInput closes the input stream (stdin for process transports).
	EndInput() error

	// Close terminates the transport and cleans up resources.
	Close() error

	// IsReady returns true if the transport is ready for communication.
	IsReady() bool
}

package claude

import "context"

// Transport is the low-level communication layer with Claude Code.
//
// The SDK ships a subprocess implementation that spawns the `claude` CLI, which
// is what both Query and Client use by default. Supplying your own lets you run
// the CLI somewhere else entirely — a container, a VM, a remote worker reached
// over WebSocket or SSE — or substitute a scripted fake in tests so a real
// binary is not required.
//
// Implementations must be safe for concurrent use by one writer and one reader.
type Transport interface {
	// Connect starts the transport and prepares it for communication.
	Connect(ctx context.Context) error

	// Write sends raw data (a newline-terminated JSON frame) to the peer.
	Write(ctx context.Context, data string) error

	// ReadMessages returns channels carrying decoded JSON messages and any
	// fatal read error. Both channels are closed when the stream ends.
	ReadMessages(ctx context.Context) (<-chan map[string]any, <-chan error)

	// EndInput closes the input stream, signaling end-of-input to the peer
	// without tearing down the transport.
	EndInput() error

	// Close terminates the transport and releases its resources.
	//
	// Close is reached on cancellation paths, so implementations must bound
	// every await inside it; an implementation that blocks forever will hang
	// the caller's shutdown.
	Close() error

	// IsReady reports whether the transport can currently accept writes.
	IsReady() bool
}

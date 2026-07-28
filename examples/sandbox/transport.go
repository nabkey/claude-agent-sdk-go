package sandbox

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"

	claude "github.com/nabkey/claude-agent-sdk-go"
)

// Transport must satisfy the SDK's interface. The SDK accepts it structurally,
// so without this assertion a signature drift would only surface at the call
// site in whatever program embeds the package.
var _ claude.Transport = (*Transport)(nil)

// Default bounds for a Transport. Each is overridable through [Config].
const (
	DefaultDialTimeout      = 10 * time.Second
	DefaultHandshakeTimeout = 15 * time.Second
	DefaultCloseTimeout     = 5 * time.Second
	DefaultMaxFrameBytes    = 8 << 20 // 8MiB, above the CLI's 1MiB stdout cap
)

// Config describes how to reach a sandbox host.
type Config struct {
	// Network and Address are passed to net.Dial. Use "unix" for a host on
	// the same machine and "tcp" for one across a network.
	Network string
	Address string

	// Token authenticates the client. The host rejects a mismatch. Required
	// unless the host was started with an empty token.
	Token string

	// TLS, when set, wraps the connection. Strongly recommended for "tcp"
	// over anything but loopback: the token is otherwise sent in the clear
	// and the session carries the agent's entire tool traffic.
	TLS *tls.Config

	// Start configures the CLI session. Use [DefaultStartRequest] to keep
	// CanUseTool working; a zero value disables tool approval callbacks.
	Start StartRequest

	// Stderr receives the sandboxed CLI's stderr, one line per call.
	Stderr func(string)

	// Timeouts and limits. Zero selects the corresponding Default constant.
	DialTimeout      time.Duration
	HandshakeTimeout time.Duration
	CloseTimeout     time.Duration
	MaxFrameBytes    int
}

func (c Config) dialTimeout() time.Duration {
	if c.DialTimeout > 0 {
		return c.DialTimeout
	}
	return DefaultDialTimeout
}

func (c Config) handshakeTimeout() time.Duration {
	if c.HandshakeTimeout > 0 {
		return c.HandshakeTimeout
	}
	return DefaultHandshakeTimeout
}

func (c Config) closeTimeout() time.Duration {
	if c.CloseTimeout > 0 {
		return c.CloseTimeout
	}
	return DefaultCloseTimeout
}

func (c Config) maxFrameBytes() int {
	if c.MaxFrameBytes > 0 {
		return c.MaxFrameBytes
	}
	return DefaultMaxFrameBytes
}

// Transport implements claude.Transport against a sandbox host.
//
// Pass one to claude.NewClientWithTransport or claude.QueryWithTransport. The
// SDK's control protocol — hooks, in-process MCP servers, CanUseTool, and the
// runtime control methods on claude.Client — all work unchanged, because they
// travel inside the frames this transport relays rather than as CLI flags.
type Transport struct {
	cfg Config

	conn net.Conn
	br   *bufio.Reader

	msgCh chan map[string]any
	errCh chan error

	// writeMu serializes frame writes; the SDK may write from a caller
	// goroutine while the pump reads.
	writeMu sync.Mutex

	// mu guards ready and closed.
	mu     sync.Mutex
	ready  bool
	closed bool

	// done is closed when the read pump exits, so Close can wait for it.
	done chan struct{}
	// stop is closed by Close to unblock a pump parked on a channel send.
	stop     chan struct{}
	pumpOnce sync.Once
}

// New returns a Transport that will dial the host described by cfg. Nothing is
// opened until Connect.
func New(cfg Config) *Transport {
	return &Transport{
		cfg:   cfg,
		msgCh: make(chan map[string]any, 64),
		errCh: make(chan error, 1),
		done:  make(chan struct{}),
		stop:  make(chan struct{}),
	}
}

// Connect dials the host, authenticates, and starts the CLI.
func (t *Transport) Connect(ctx context.Context) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return errors.New("sandbox: transport is closed")
	}
	if t.ready {
		t.mu.Unlock()
		return nil
	}
	t.mu.Unlock()

	conn, err := t.dial(ctx)
	if err != nil {
		return fmt.Errorf("sandbox: dial %s/%s: %w", t.cfg.Network, t.cfg.Address, err)
	}

	t.conn = conn
	t.br = bufio.NewReaderSize(conn, 64<<10)

	if err := t.handshake(ctx); err != nil {
		conn.Close()
		return err
	}

	t.mu.Lock()
	t.ready = true
	t.mu.Unlock()

	go t.pump()
	return nil
}

func (t *Transport) dial(ctx context.Context) (net.Conn, error) {
	ctx, cancel := context.WithTimeout(ctx, t.cfg.dialTimeout())
	defer cancel()

	d := &net.Dialer{}
	if t.cfg.TLS != nil {
		return (&tls.Dialer{NetDialer: d, Config: t.cfg.TLS}).DialContext(ctx, t.cfg.Network, t.cfg.Address)
	}
	return d.DialContext(ctx, t.cfg.Network, t.cfg.Address)
}

// handshake performs the hello/start exchange. The deadline covers both round
// trips so a host that accepts the connection but never answers cannot wedge
// Connect indefinitely.
func (t *Transport) handshake(ctx context.Context) error {
	deadline := time.Now().Add(t.cfg.handshakeTimeout())
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	if err := t.conn.SetDeadline(deadline); err != nil {
		return fmt.Errorf("sandbox: set handshake deadline: %w", err)
	}
	// The pump does its own blocking reads with no deadline.
	defer t.conn.SetDeadline(time.Time{})

	if err := t.sendFrame(Frame{
		Type:    FrameHello,
		Version: ProtocolVersion,
		Token:   t.cfg.Token,
	}); err != nil {
		return fmt.Errorf("sandbox: send hello: %w", err)
	}
	if _, err := t.expect(FrameHelloOK); err != nil {
		return err
	}

	start := t.cfg.Start
	if err := t.sendFrame(Frame{Type: FrameStart, Start: &start}); err != nil {
		return fmt.Errorf("sandbox: send start: %w", err)
	}
	if _, err := t.expect(FrameStarted); err != nil {
		return err
	}
	return nil
}

// expect reads frames until one of the wanted type arrives, forwarding stderr
// along the way and surfacing a host error frame as an error.
func (t *Transport) expect(want FrameType) (Frame, error) {
	for {
		f, err := t.readFrame()
		if err != nil {
			return Frame{}, fmt.Errorf("sandbox: awaiting %s: %w", want, err)
		}
		switch f.Type {
		case want:
			return f, nil
		case FrameStderr:
			t.reportStderr(f.Line)
		case FrameError:
			return Frame{}, fmt.Errorf("sandbox: host rejected session: %s", f.Error)
		case FrameExit:
			return Frame{}, fmt.Errorf("sandbox: CLI exited during handshake: %s", f.Error)
		default:
			return Frame{}, fmt.Errorf("sandbox: unexpected %q frame while awaiting %s", f.Type, want)
		}
	}
}

// pump relays host frames until the stream ends, then closes both channels
// exactly once.
func (t *Transport) pump() {
	defer close(t.done)
	defer t.pumpOnce.Do(func() {
		close(t.msgCh)
		close(t.errCh)
	})

	for {
		f, err := t.readFrame()
		if err != nil {
			t.markNotReady()
			// A closed connection after Close is the expected ending, not a
			// failure worth surfacing to the SDK.
			if !t.isClosed() {
				t.emitErr(fmt.Errorf("sandbox: read: %w", err))
			}
			return
		}

		switch f.Type {
		case FrameMsg:
			if f.Msg == nil {
				continue
			}
			select {
			case t.msgCh <- f.Msg:
			case <-t.stop:
				return
			}
		case FrameStderr:
			t.reportStderr(f.Line)
		case FrameExit:
			t.markNotReady()
			if f.Error != "" {
				t.emitErr(fmt.Errorf("sandbox: CLI exited: %s", f.Error))
			}
			return
		case FrameError:
			t.markNotReady()
			t.emitErr(fmt.Errorf("sandbox: host error: %s", f.Error))
			return
		default:
			// Unknown frame types are ignored so a newer host can add them
			// without breaking an older client.
		}
	}
}

// Write relays one newline-terminated JSON frame to the sandboxed CLI's stdin.
func (t *Transport) Write(ctx context.Context, data string) error {
	if !t.IsReady() {
		return errors.New("sandbox: transport is not ready")
	}
	if err := t.sendFrame(Frame{Type: FrameStdin, Data: data}); err != nil {
		return fmt.Errorf("sandbox: write: %w", err)
	}
	return nil
}

// ReadMessages returns the decoded CLI stdout stream and a fatal-error channel.
// Both are closed when the session ends.
func (t *Transport) ReadMessages(ctx context.Context) (<-chan map[string]any, <-chan error) {
	return t.msgCh, t.errCh
}

// EndInput closes the CLI's stdin without tearing down the session.
func (t *Transport) EndInput() error {
	if !t.IsReady() {
		return nil
	}
	return t.sendFrame(Frame{Type: FrameEndInput})
}

// Close terminates the session and releases the connection.
//
// Every wait here is bounded: Close is reached on cancellation paths, and a
// host that has stopped reading must not be able to hang the caller.
func (t *Transport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	t.ready = false
	conn := t.conn
	t.mu.Unlock()

	if conn == nil {
		// Never connected; release any reader waiting on the channels.
		t.pumpOnce.Do(func() {
			close(t.msgCh)
			close(t.errCh)
		})
		return nil
	}

	// Best-effort goodbye so the host can reap the CLI gracefully. A failure
	// here is uninteresting — the deferred Close below is the real teardown.
	conn.SetWriteDeadline(time.Now().Add(t.cfg.closeTimeout()))
	_ = t.sendFrame(Frame{Type: FrameClose})

	// Unblock a pump parked on a channel send before waiting for it.
	close(t.stop)

	select {
	case <-t.done:
	case <-time.After(t.cfg.closeTimeout()):
		// Pump is wedged; drop the connection out from under it.
	}

	err := conn.Close()

	// The pump closes these on exit, but it may have been the wedged case.
	t.pumpOnce.Do(func() {
		close(t.msgCh)
		close(t.errCh)
	})
	return err
}

// IsReady reports whether the transport can currently accept writes.
func (t *Transport) IsReady() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.ready && !t.closed
}

func (t *Transport) sendFrame(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.conn == nil {
		return errors.New("sandbox: not connected")
	}
	_, err = t.conn.Write(b)
	return err
}

func (t *Transport) readFrame() (Frame, error) {
	line, err := readLine(t.br, t.cfg.maxFrameBytes())
	if err != nil {
		return Frame{}, err
	}
	var f Frame
	if err := json.Unmarshal(line, &f); err != nil {
		return Frame{}, fmt.Errorf("decode frame: %w", err)
	}
	return f, nil
}

func (t *Transport) reportStderr(line string) {
	if t.cfg.Stderr != nil && line != "" {
		t.cfg.Stderr(line)
	}
}

func (t *Transport) emitErr(err error) {
	select {
	case t.errCh <- err:
	default:
		// Buffered slot already holds a fatal error; the first one wins.
	}
}

func (t *Transport) markNotReady() {
	t.mu.Lock()
	t.ready = false
	t.mu.Unlock()
}

func (t *Transport) isClosed() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.closed
}

// readLine reads one newline-terminated frame, refusing anything over max so a
// peer cannot exhaust memory with a single unterminated line.
func readLine(br *bufio.Reader, max int) ([]byte, error) {
	var buf []byte
	for {
		chunk, isPrefix, err := br.ReadLine()
		if err != nil {
			return nil, err
		}
		buf = append(buf, chunk...)
		if len(buf) > max {
			return nil, fmt.Errorf("frame exceeds %d bytes", max)
		}
		if !isPrefix {
			return buf, nil
		}
	}
}

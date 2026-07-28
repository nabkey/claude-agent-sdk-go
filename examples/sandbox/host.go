package sandbox

import (
	"bufio"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// gracefulKillTimeout bounds how long a session waits for the CLI to exit after
// stdin EOF before escalating to Kill.
const gracefulKillTimeout = 5 * time.Second

// Policy is the sandbox operator's half of the configuration: the CLI flags a
// dialing client cannot influence.
//
// Everything that determines what the agent can reach lives here, because the
// host is the trusted side. A client only gets to vary the fields in
// [StartRequest].
type Policy struct {
	// CLIPath is the claude binary. Empty resolves "claude" on PATH.
	CLIPath string

	// Cwd is the CLI's working directory, and the root the agent operates in.
	Cwd string

	// AddDirs grants access to directories beyond Cwd.
	AddDirs []string

	// PermissionMode is the CLI's --permission-mode. Empty leaves the default.
	PermissionMode string

	// Model and FallbackModel pin the model. Empty leaves the account default.
	Model         string
	FallbackModel string

	// AllowedTools and DisallowedTools filter the tool set.
	AllowedTools    []string
	DisallowedTools []string

	// SettingSources maps to --setting-sources. A nil pointer omits the flag,
	// which lets the CLI load every source; a pointer to "" disables
	// filesystem settings entirely.
	SettingSources *string

	// Settings is a raw JSON settings blob passed as --settings. This is where
	// bash sandboxing goes, e.g. {"sandbox":{"enabled":true}}.
	Settings string

	// MaxTurns caps agent turns per session. Zero omits the flag.
	MaxTurns int

	// MCPConfigPath points the CLI at an MCP config file inside the sandbox.
	// Ignored when the client declares StartRequest.SDKMCPServers, since the
	// CLI takes a single --mcp-config.
	MCPConfigPath string

	// ExtraArgs are appended verbatim. Operator-supplied, never client-supplied.
	ExtraArgs []string

	// BlankSystemPrompt passes --system-prompt "" so the CLI does not apply
	// its own default prompt.
	//
	// The SDK does this whenever no system prompt is configured, so leaving it
	// false gives a sandboxed session a different personality than the same
	// options would produce locally. Default it to true unless you want the
	// CLI's stock prompt. See [DefaultPolicy].
	BlankSystemPrompt bool

	// AllowResume permits a client to set StartRequest.Resume and SessionID.
	// Off by default: session IDs are addressable, so honoring them lets one
	// client reattach to another's conversation.
	AllowResume bool

	// AllowEnv lists environment variable names a client may set. Anything
	// else in StartRequest.Env is dropped.
	AllowEnv []string

	// InheritEnv passes the host process's environment to the CLI. Required
	// for the CLI to find its credentials unless you populate AllowEnv.
	InheritEnv bool
}

// DefaultPolicy returns a Policy matching how the SDK spawns the CLI locally:
// a blank system prompt and the host's environment inherited.
func DefaultPolicy(cwd string) Policy {
	return Policy{
		Cwd:               cwd,
		BlankSystemPrompt: true,
		InheritEnv:        true,
	}
}

// buildArgv renders the command line. The base flags are fixed: the SDK speaks
// stream-json in both directions and needs --verbose for full message output.
func (p Policy) buildArgv(req StartRequest) []string {
	cli := p.CLIPath
	if cli == "" {
		cli = "claude"
	}
	argv := []string{cli,
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
	}

	if p.BlankSystemPrompt {
		argv = append(argv, "--system-prompt", "")
	}

	// Without this the SDK's CanUseTool callback is never consulted, because
	// the SDK sets the flag on a path a custom transport bypasses.
	if req.PermissionPromptTool != "" {
		argv = append(argv, "--permission-prompt-tool", req.PermissionPromptTool)
	}

	if p.PermissionMode != "" {
		argv = append(argv, "--permission-mode", p.PermissionMode)
	}
	if p.Model != "" {
		argv = append(argv, "--model", p.Model)
	}
	if p.FallbackModel != "" {
		argv = append(argv, "--fallback-model", p.FallbackModel)
	}
	for _, dir := range p.AddDirs {
		argv = append(argv, "--add-dir", dir)
	}
	if len(p.AllowedTools) > 0 {
		argv = append(argv, "--allowedTools", strings.Join(p.AllowedTools, ","))
	}
	if len(p.DisallowedTools) > 0 {
		argv = append(argv, "--disallowedTools", strings.Join(p.DisallowedTools, ","))
	}
	if p.SettingSources != nil {
		argv = append(argv, "--setting-sources="+*p.SettingSources)
	}
	if p.Settings != "" {
		argv = append(argv, "--settings", p.Settings)
	}
	if p.MaxTurns > 0 {
		argv = append(argv, "--max-turns", strconv.Itoa(p.MaxTurns))
	}

	if p.AllowResume {
		// Bound with '=' so a dash-leading value cannot be read as a flag;
		// --resume takes an optional argument, so the two-token form would
		// not bind to it.
		if req.Resume != "" {
			argv = append(argv, "--resume="+req.Resume)
		}
		if req.SessionID != "" {
			argv = append(argv, "--session-id="+req.SessionID)
		}
		if req.ForkSession {
			argv = append(argv, "--fork-session")
		}
	}

	// In-process MCP servers are registered through --mcp-config, which the
	// SDK emits from its own subprocess builder. A custom transport bypasses
	// that, so the client declares them and the host emits the flag.
	if len(req.SDKMCPServers) > 0 {
		servers := make(map[string]any, len(req.SDKMCPServers))
		for _, name := range req.SDKMCPServers {
			servers[name] = map[string]any{"type": "sdk", "name": name}
		}
		if blob, err := json.Marshal(map[string]any{"mcpServers": servers}); err == nil {
			argv = append(argv, "--mcp-config", string(blob))
		}
	} else if p.MCPConfigPath != "" {
		argv = append(argv, "--mcp-config", p.MCPConfigPath)
	}

	if req.IncludePartialMessages {
		argv = append(argv, "--include-partial-messages")
	}
	if req.IncludeHookEvents {
		argv = append(argv, "--include-hook-events")
	}

	return append(argv, p.ExtraArgs...)
}

// env renders the CLI's environment from the policy and the client's request.
func (p Policy) env(req StartRequest) []string {
	var base []string
	if p.InheritEnv {
		base = os.Environ()
	}
	if len(req.Env) == 0 || len(p.AllowEnv) == 0 {
		return base
	}
	allowed := make(map[string]bool, len(p.AllowEnv))
	for _, name := range p.AllowEnv {
		allowed[name] = true
	}
	for name, value := range req.Env {
		if allowed[name] {
			base = append(base, name+"="+value)
		}
	}
	return base
}

// Host serves sandbox sessions. Each accepted connection gets its own CLI
// process, so one host can back several chats at once.
type Host struct {
	// Policy fixes the CLI flags for every session.
	Policy Policy

	// Token authenticates clients. An empty token disables authentication,
	// which is only safe on a unix socket with restrictive permissions.
	Token string

	// Log receives operational messages. Nil discards them.
	Log func(string, ...any)

	// MaxFrameBytes bounds a single client frame. Zero selects
	// DefaultMaxFrameBytes.
	MaxFrameBytes int
}

func (h *Host) logf(format string, args ...any) {
	if h.Log != nil {
		h.Log(format, args...)
	}
}

func (h *Host) maxFrameBytes() int {
	if h.MaxFrameBytes > 0 {
		return h.MaxFrameBytes
	}
	return DefaultMaxFrameBytes
}

// Serve accepts connections until ctx is cancelled or the listener fails.
func (h *Host) Serve(ctx context.Context, ln net.Listener) error {
	// Unblock Accept on cancellation.
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	var wg sync.WaitGroup
	defer wg.Wait()

	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return fmt.Errorf("sandbox: accept: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer conn.Close()
			if err := h.serveConn(ctx, conn); err != nil {
				h.logf("session ended: %v", err)
			}
		}()
	}
}

// session holds the per-connection state: the CLI process and a serialized
// writer back to the client.
type session struct {
	conn    net.Conn
	writeMu sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
}

func (s *session) send(f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b = append(b, '\n')

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.conn.Write(b)
	return err
}

func (h *Host) serveConn(ctx context.Context, conn net.Conn) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	br := bufio.NewReaderSize(conn, 64<<10)
	s := &session{conn: conn}

	readFrame := func() (Frame, error) {
		line, err := readLine(br, h.maxFrameBytes())
		if err != nil {
			return Frame{}, err
		}
		var f Frame
		if err := json.Unmarshal(line, &f); err != nil {
			return Frame{}, fmt.Errorf("decode frame: %w", err)
		}
		return f, nil
	}

	// --- hello ---------------------------------------------------------
	hello, err := readFrame()
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	if hello.Type != FrameHello {
		s.send(Frame{Type: FrameError, Error: "expected hello frame"})
		return errors.New("client did not open with hello")
	}
	if hello.Version != ProtocolVersion {
		msg := fmt.Sprintf("protocol version %d not supported, host speaks %d",
			hello.Version, ProtocolVersion)
		s.send(Frame{Type: FrameError, Error: msg})
		return errors.New(msg)
	}
	// Constant-time so a wrong token cannot be recovered by timing.
	if subtle.ConstantTimeCompare([]byte(hello.Token), []byte(h.Token)) != 1 {
		s.send(Frame{Type: FrameError, Error: "authentication failed"})
		return errors.New("bad token")
	}
	if err := s.send(Frame{Type: FrameHelloOK}); err != nil {
		return fmt.Errorf("send hello_ok: %w", err)
	}

	// --- start ---------------------------------------------------------
	startFrame, err := readFrame()
	if err != nil {
		return fmt.Errorf("read start: %w", err)
	}
	if startFrame.Type != FrameStart || startFrame.Start == nil {
		s.send(Frame{Type: FrameError, Error: "expected start frame"})
		return errors.New("client did not send start")
	}
	req := *startFrame.Start

	if !h.Policy.AllowResume && (req.Resume != "" || req.SessionID != "") {
		h.logf("ignoring client resume request: policy forbids it")
	}

	argv := h.Policy.buildArgv(req)
	h.logf("starting: %s", strings.Join(argv, " "))

	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = h.Policy.Cwd
	cmd.Env = h.Policy.env(req)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		s.send(Frame{Type: FrameError, Error: "stdin pipe: " + err.Error()})
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		s.send(Frame{Type: FrameError, Error: "stdout pipe: " + err.Error()})
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		s.send(Frame{Type: FrameError, Error: "stderr pipe: " + err.Error()})
		return err
	}

	if err := cmd.Start(); err != nil {
		s.send(Frame{Type: FrameError, Error: "spawn " + argv[0] + ": " + err.Error()})
		return fmt.Errorf("spawn CLI: %w", err)
	}
	s.cmd = cmd
	s.stdin = stdin

	if err := s.send(Frame{Type: FrameStarted}); err != nil {
		return fmt.Errorf("send started: %w", err)
	}

	// --- pipes ---------------------------------------------------------
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); h.pumpStdout(s, stdout) }()
	go func() { defer wg.Done(); h.pumpStderr(s, stderr) }()

	// Reap the CLI once both pipes are drained. cmd.Wait closes them, so it
	// must not run while the pumps are still reading.
	waited := make(chan error, 1)
	go func() {
		wg.Wait()
		waited <- cmd.Wait()
	}()

	clientDone := make(chan error, 1)
	go func() { clientDone <- h.pumpClient(s, readFrame) }()

	// Either side can end the session. Waiting only on the client would
	// strand one whose CLI died on its own — a crash, a turn limit, or an
	// end-of-input that ran to completion.
	var clientErr, waitErr error
	select {
	case clientErr = <-clientDone:
		// Client is done writing: EOF the CLI's stdin and give it a moment to
		// flush before the context teardown escalates to a kill.
		stdin.Close()
		select {
		case waitErr = <-waited:
		case <-time.After(gracefulKillTimeout):
			h.logf("CLI did not exit within %s, killing", gracefulKillTimeout)
			cancel()
			waitErr = <-waited
		}
	case waitErr = <-waited:
		// The CLI exited first. Returning closes the connection, which
		// unblocks the client pump still parked on a read.
	}

	code := 0
	msg := ""
	var exitErr *exec.ExitError
	if errors.As(waitErr, &exitErr) {
		code = exitErr.ExitCode()
		msg = fmt.Sprintf("exit status %d", code)
	} else if waitErr != nil {
		msg = waitErr.Error()
	}
	s.send(Frame{Type: FrameExit, Code: &code, Error: msg})

	return clientErr
}

// pumpClient relays client frames to the CLI. It returns nil on an orderly
// close or EOF.
func (h *Host) pumpClient(s *session, readFrame func() (Frame, error)) error {
	for {
		f, err := readFrame()
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("read from client: %w", err)
		}

		switch f.Type {
		case FrameStdin:
			if _, err := io.WriteString(s.stdin, f.Data); err != nil {
				return fmt.Errorf("write to CLI stdin: %w", err)
			}
		case FrameEndInput:
			// The SDK signals end-of-input without wanting teardown; the CLI
			// finishes its turn and exits on its own.
			s.stdin.Close()
		case FrameClose:
			return nil
		default:
			h.logf("ignoring unexpected %q frame from client", f.Type)
		}
	}
}

// pumpStdout forwards each decoded CLI stdout object to the client.
func (h *Host) pumpStdout(s *session, r io.Reader) {
	br := bufio.NewReaderSize(r, 64<<10)
	for {
		line, err := readLine(br, h.maxFrameBytes())
		if err != nil {
			return
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var obj map[string]any
		if err := json.Unmarshal(line, &obj); err != nil {
			// The CLI occasionally prints non-JSON noise on stdout; surface
			// it as stderr rather than killing the session over it.
			s.send(Frame{Type: FrameStderr, Line: "non-JSON stdout: " + string(line)})
			continue
		}
		if err := s.send(Frame{Type: FrameMsg, Msg: obj}); err != nil {
			return
		}
	}
}

// pumpStderr forwards the CLI's stderr lines for logging on the client side.
func (h *Host) pumpStderr(s *session, r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	for sc.Scan() {
		if err := s.send(Frame{Type: FrameStderr, Line: sc.Text()}); err != nil {
			return
		}
	}
}

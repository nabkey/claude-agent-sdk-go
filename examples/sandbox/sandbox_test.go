package sandbox

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCLI writes a stand-in for the claude binary: it ignores every flag, emits
// one line on stdout at startup, echoes an ack per stdin line, and writes one
// stderr line so the forwarding path is covered.
func fakeCLI(t *testing.T) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "fake-claude")
	script := `#!/bin/sh
echo 'boot' >&2
echo '{"type":"system","subtype":"init"}'
while IFS= read -r line; do
  case "$line" in
    *bye*) exit 0 ;;
  esac
  echo '{"type":"ack"}'
done
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake CLI: %v", err)
	}
	return path
}

// startHost brings up a Host on a unix socket and returns its address.
func startHost(t *testing.T, h *Host) string {
	t.Helper()

	// A unix socket in TempDir keeps the test off any shared TCP port. Linux
	// caps sun_path near 108 bytes, so keep the name short.
	dir, err := os.MkdirTemp("", "sbx")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	addr := filepath.Join(dir, "s")

	ln, err := net.Listen("unix", addr)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.Serve(ctx, ln)
	}()
	t.Cleanup(func() {
		cancel()
		ln.Close()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("host did not shut down within 5s")
		}
	})
	return addr
}

func TestPolicyBuildArgv(t *testing.T) {
	empty := ""
	tests := []struct {
		name       string
		policy     Policy
		req        StartRequest
		wantSubseq []string
		wantAbsent []string
	}{
		{
			// The whole reason the host builds the argv: without this flag
			// the SDK's CanUseTool callback is never consulted.
			name:       "permission prompt tool is forwarded",
			policy:     DefaultPolicy("/work"),
			req:        DefaultStartRequest(),
			wantSubseq: []string{"--permission-prompt-tool", "stdio"},
		},
		{
			name:       "base flags are always present",
			policy:     Policy{},
			req:        StartRequest{},
			wantSubseq: []string{"--output-format", "stream-json", "--input-format", "stream-json"},
		},
		{
			name:       "blank system prompt matches SDK default",
			policy:     DefaultPolicy("/work"),
			req:        StartRequest{},
			wantSubseq: []string{"--system-prompt", ""},
		},
		{
			name:       "cli system prompt opt-out drops the flag",
			policy:     Policy{BlankSystemPrompt: false},
			req:        StartRequest{},
			wantAbsent: []string{"--system-prompt"},
		},
		{
			name:       "resume is ignored unless policy allows it",
			policy:     Policy{},
			req:        StartRequest{Resume: "abc", SessionID: "def", ForkSession: true},
			wantAbsent: []string{"--resume=abc", "--session-id=def", "--fork-session"},
		},
		{
			name:       "resume is honored under AllowResume",
			policy:     Policy{AllowResume: true},
			req:        StartRequest{Resume: "abc", ForkSession: true},
			wantSubseq: []string{"--resume=abc", "--fork-session"},
		},
		{
			// --resume takes an optional value, so a two-token form would not
			// bind and a dash-leading ID could be read as a flag.
			name:       "resume binds its value with equals",
			policy:     Policy{AllowResume: true},
			req:        StartRequest{Resume: "--malicious"},
			wantSubseq: []string{"--resume=--malicious"},
			wantAbsent: []string{"--resume"},
		},
		{
			name:       "empty setting sources disables filesystem settings",
			policy:     Policy{SettingSources: &empty},
			req:        StartRequest{},
			wantSubseq: []string{"--setting-sources="},
		},
		{
			name:       "nil setting sources omits the flag entirely",
			policy:     Policy{},
			req:        StartRequest{},
			wantAbsent: []string{"--setting-sources="},
		},
		{
			name:       "tool lists are comma joined",
			policy:     Policy{AllowedTools: []string{"Read", "Bash"}, DisallowedTools: []string{"Write"}},
			req:        StartRequest{},
			wantSubseq: []string{"--allowedTools", "Read,Bash", "--disallowedTools", "Write"},
		},
		{
			name:       "partial messages flag is client controlled",
			policy:     Policy{},
			req:        StartRequest{IncludePartialMessages: true},
			wantSubseq: []string{"--include-partial-messages"},
		},
		{
			// Without this the CLI never learns the in-process server exists
			// and the agent silently runs without those tools.
			name:   "sdk mcp servers are registered",
			policy: Policy{},
			req:    StartRequest{SDKMCPServers: []string{"brain"}},
			wantSubseq: []string{"--mcp-config",
				`{"mcpServers":{"brain":{"name":"brain","type":"sdk"}}}`},
		},
		{
			name:       "mcp config path is used when no sdk servers are declared",
			policy:     Policy{MCPConfigPath: "/etc/mcp.json"},
			req:        StartRequest{},
			wantSubseq: []string{"--mcp-config", "/etc/mcp.json"},
		},
		{
			// The CLI takes a single --mcp-config, so the declared servers win
			// rather than emitting the flag twice.
			name:       "sdk servers take precedence over a config path",
			policy:     Policy{MCPConfigPath: "/etc/mcp.json"},
			req:        StartRequest{SDKMCPServers: []string{"brain"}},
			wantAbsent: []string{"/etc/mcp.json"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			argv := tc.policy.buildArgv(tc.req)
			if !containsSubsequence(argv, tc.wantSubseq) {
				t.Errorf("argv %q missing subsequence %q", argv, tc.wantSubseq)
			}
			for _, absent := range tc.wantAbsent {
				for _, got := range argv {
					if got == absent {
						t.Errorf("argv %q should not contain %q", argv, absent)
					}
				}
			}
		})
	}
}

// containsSubsequence reports whether want appears in argv in order and
// adjacently, so flag/value pairing is checked rather than mere presence.
func containsSubsequence(argv, want []string) bool {
	if len(want) == 0 {
		return true
	}
	for i := 0; i+len(want) <= len(argv); i++ {
		match := true
		for j, w := range want {
			if argv[i+j] != w {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func TestPolicyEnvFiltersByAllowlist(t *testing.T) {
	p := Policy{AllowEnv: []string{"SAFE"}}
	env := p.env(StartRequest{Env: map[string]string{"SAFE": "yes", "SECRET": "no"}})

	var sawSafe bool
	for _, kv := range env {
		if kv == "SAFE=yes" {
			sawSafe = true
		}
		if strings.HasPrefix(kv, "SECRET=") {
			t.Errorf("env %q leaked a name outside the allowlist", kv)
		}
	}
	if !sawSafe {
		t.Error("allowlisted variable was not passed through")
	}
}

func TestTransportRoundTrip(t *testing.T) {
	var stderrLines []string
	addr := startHost(t, &Host{
		Policy: Policy{CLIPath: fakeCLI(t)},
		Token:  "secret",
	})

	tr := New(Config{
		Network: "unix",
		Address: addr,
		Token:   "secret",
		Start:   DefaultStartRequest(),
		Stderr:  func(line string) { stderrLines = append(stderrLines, line) },
	})

	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	if !tr.IsReady() {
		t.Error("transport should be ready after Connect")
	}

	msgs, errs := tr.ReadMessages(ctx)

	// The fake CLI's startup line proves stdout -> FrameMsg -> channel works.
	select {
	case msg := <-msgs:
		if msg["type"] != "system" {
			t.Errorf("first message = %v, want type=system", msg)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the CLI's first message")
	}

	// And a write proves the reverse path reaches the CLI's stdin.
	if err := tr.Write(ctx, "{\"hello\":true}\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	select {
	case msg := <-msgs:
		if msg["type"] != "ack" {
			t.Errorf("second message = %v, want type=ack", msg)
		}
	case err := <-errs:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for the CLI's ack")
	}

	if len(stderrLines) == 0 || stderrLines[0] != "boot" {
		t.Errorf("stderr forwarding: got %q, want first line %q", stderrLines, "boot")
	}
}

func TestTransportChannelsCloseWhenCLIExits(t *testing.T) {
	addr := startHost(t, &Host{Policy: Policy{CLIPath: fakeCLI(t)}})

	tr := New(Config{Network: "unix", Address: addr, Start: DefaultStartRequest()})
	ctx := context.Background()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer tr.Close()

	msgs, _ := tr.ReadMessages(ctx)
	<-msgs // startup line

	// "bye" makes the fake CLI exit, which must close the stream.
	if err := tr.Write(ctx, "bye\n"); err != nil {
		t.Fatalf("write: %v", err)
	}

	deadline := time.After(15 * time.Second)
	for {
		select {
		case _, ok := <-msgs:
			if !ok {
				return // channel closed, which is the contract
			}
		case <-deadline:
			t.Fatal("message channel was not closed after the CLI exited")
		}
	}
}

func TestHostRejectsBadToken(t *testing.T) {
	addr := startHost(t, &Host{Policy: Policy{CLIPath: fakeCLI(t)}, Token: "right"})

	tr := New(Config{Network: "unix", Address: addr, Token: "wrong", Start: DefaultStartRequest()})
	err := tr.Connect(context.Background())
	if err == nil {
		tr.Close()
		t.Fatal("connect should fail with a bad token")
	}
	if !strings.Contains(err.Error(), "authentication failed") {
		t.Errorf("error = %v, want it to mention authentication", err)
	}
}

func TestHostRejectsProtocolMismatch(t *testing.T) {
	addr := startHost(t, &Host{Policy: Policy{CLIPath: fakeCLI(t)}})

	// Hand-roll a hello so a wrong version can be sent.
	conn, err := net.Dial("unix", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Write([]byte(`{"t":"hello","v":999}` + "\n")); err != nil {
		t.Fatalf("write hello: %v", err)
	}

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	buf := make([]byte, 512)
	n, err := conn.Read(buf)
	if err != nil {
		t.Fatalf("read reply: %v", err)
	}
	if reply := string(buf[:n]); !strings.Contains(reply, "not supported") {
		t.Errorf("reply = %q, want a version rejection", reply)
	}
}

func TestCloseBeforeConnectIsSafe(t *testing.T) {
	tr := New(Config{Network: "unix", Address: "/nonexistent/socket"})

	if err := tr.Close(); err != nil {
		t.Errorf("Close on an unconnected transport: %v", err)
	}
	// The contract says both channels are closed when the stream ends, and a
	// transport that never opened has trivially ended.
	msgs, errs := tr.ReadMessages(context.Background())
	select {
	case _, ok := <-msgs:
		if ok {
			t.Error("message channel should be closed")
		}
	case <-time.After(2 * time.Second):
		t.Error("message channel was not closed")
	}
	select {
	case _, ok := <-errs:
		if ok {
			t.Error("error channel should be closed")
		}
	case <-time.After(2 * time.Second):
		t.Error("error channel was not closed")
	}

	// Close must stay idempotent.
	if err := tr.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestCloseIsBoundedWhenNobodyDrains(t *testing.T) {
	addr := startHost(t, &Host{Policy: Policy{CLIPath: fakeCLI(t)}})

	// CloseTimeout is set here rather than after Connect: the pump goroutine
	// reads cfg, so mutating it post-connect is a data race.
	tr := New(Config{
		Network:      "unix",
		Address:      addr,
		Start:        DefaultStartRequest(),
		CloseTimeout: time.Second,
	})
	if err := tr.Connect(context.Background()); err != nil {
		t.Fatalf("connect: %v", err)
	}

	// Deliberately never read from the channels, so the pump may be parked on
	// a send. Close must still return promptly.
	done := make(chan struct{})
	go func() {
		defer close(done)
		tr.Close()
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked with an undrained message channel")
	}
}

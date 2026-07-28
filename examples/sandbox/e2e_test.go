package sandbox

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// TestEndToEndWithRealCLI drives a real claude session through the sandbox
// host and asserts that a PreToolUse hook fires.
//
// The hook is the load-bearing assertion. Hooks travel on the initialize
// request rather than as CLI flags, so a hook that fires proves the control
// protocol survives the round trip through the socket — the SDK issued a
// callback mid-turn, the CLI blocked on it, and the verdict travelled back.
// That is the part a custom transport can break.
//
// CanUseTool is only logged, not asserted. It depends on nothing being able to
// auto-approve the call first, and settings files, managed policy, or the
// surrounding environment can all do that — as they do inside a CI container.
// The regression guard for the flag itself is TestPolicyBuildArgv, which
// asserts --permission-prompt-tool is present in the argv.
//
// Gated on SANDBOX_E2E=1: it spends real tokens and needs a logged-in CLI.
func TestEndToEndWithRealCLI(t *testing.T) {
	if os.Getenv("SANDBOX_E2E") != "1" {
		t.Skip("set SANDBOX_E2E=1 to run against the real claude CLI")
	}
	cli, err := exec.LookPath("claude")
	if err != nil {
		t.Skip("claude CLI not on PATH")
	}

	workdir := t.TempDir()
	policy := DefaultPolicy(workdir)
	policy.CLIPath = cli
	policy.MaxTurns = 4

	// Load no settings files. Without this the host's own machine decides
	// permissions: a "permissions" block in user or project settings
	// auto-approves the tool call before CanUseTool is ever reached, and the
	// assertion below fails for a reason that has nothing to do with the
	// transport. A sandbox generally wants this anyway — the agent's
	// containment should not depend on dotfiles that happen to be present.
	noSettings := ""
	policy.SettingSources = &noSettings

	addr := startHost(t, &Host{
		Policy: policy,
		Log:    func(format string, args ...any) { t.Logf("host: "+format, args...) },
	})

	tr := New(Config{
		Network: "unix",
		Address: addr,
		Start:   DefaultStartRequest(),
		Stderr:  func(line string) { t.Logf("cli stderr: %s", line) },
	})

	var (
		mu      sync.Mutex
		asked   []string
		hookSaw []string
	)

	opts := claude.DefaultAgentOptions()

	// A PreToolUse hook is consulted unconditionally, whatever the ambient
	// permission configuration decides about the call.
	opts.Hooks = map[types.HookEvent][]types.HookMatcher{
		types.HookEventPreToolUse: {{
			Hooks: []types.HookCallback{
				func(ctx context.Context, input types.HookInput, _ *string,
					_ *types.HookContext) (*types.HookOutput, error) {
					if ptu, ok := input.(*types.PreToolUseHookInput); ok {
						mu.Lock()
						hookSaw = append(hookSaw, ptu.ToolName)
						mu.Unlock()
					}
					return nil, nil
				},
			},
		}},
	}
	// Note what is deliberately NOT set here: AllowedTools, PermissionMode,
	// and MaxTurns are CLI flags, so a custom transport drops them. They live
	// on the host Policy instead. Setting AllowedTools to a whole tool would
	// also auto-approve it before CanUseTool ever ran.
	opts.CanUseTool = func(ctx context.Context, tool string, input map[string]any,
		_ types.ToolPermissionContext) (types.PermissionResult, error) {
		mu.Lock()
		asked = append(asked, tool)
		mu.Unlock()
		return &types.PermissionResultAllow{}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := claude.NewClientWithTransport(ctx, opts, tr)
	if err != nil {
		t.Fatalf("new client: %v", err)
	}
	defer client.Close()

	if err := client.Connect(ctx, "Run the bash command `echo sandbox-ok` and tell me what it printed."); err != nil {
		t.Fatalf("connect: %v", err)
	}

	var transcript strings.Builder
	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case *types.TextBlock:
					transcript.WriteString(b.Text)
				case *types.ToolUseBlock:
					t.Logf("tool use: %s %v", b.Name, b.Input)
				}
			}
		case *types.ResultMessage:
			t.Logf("result: turns=%d", m.NumTurns)
		case error:
			t.Fatalf("session error: %v", m)
		}
	}
	if err := client.Err(); err != nil {
		t.Fatalf("client error: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()

	if len(hookSaw) == 0 {
		t.Fatal("PreToolUse hook never fired: the control protocol is not " +
			"completing a callback round trip through the sandbox transport")
	}
	t.Logf("PreToolUse hook fired for: %v", hookSaw)

	// Informational only — see the doc comment for why this is not asserted.
	if len(asked) == 0 {
		t.Log("CanUseTool was not consulted; something in this environment " +
			"auto-approved the call before the callback ran")
	} else {
		t.Logf("CanUseTool consulted for: %v", asked)
	}
	t.Logf("transcript: %s", transcript.String())
}

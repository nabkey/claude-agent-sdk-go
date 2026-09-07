package transport

import (
	"runtime"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestBuildCommand_ResumeDropsTurn(t *testing.T) {
	turn := "user-msg-uuid"
	cmd := newTestTransport(&SubprocessOptions{
		Resume:          strPtr("session-1"),
		ResumeSessionAt: strPtr("entry-uuid"),
		ResumeDropsTurn: &turn,
	}).buildCommand()

	got, ok := flagValue(cmd, "--resume-drops-turn")
	if !ok {
		t.Fatal("expected --resume-drops-turn")
	}
	if got != turn {
		t.Errorf("--resume-drops-turn = %q, want %q", got, turn)
	}
}

func TestBuildCommand_ResumeDropsTurnOmittedWhenUnset(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{Resume: strPtr("session-1")}).buildCommand()
	if hasFlag(cmd, "--resume-drops-turn") {
		t.Error("--resume-drops-turn must not appear when unset")
	}
}

func TestBuildCommand_PermissionPrompts(t *testing.T) {
	mode := types.PermissionPromptsNone
	cmd := newTestTransport(&SubprocessOptions{PermissionPrompts: &mode}).buildCommand()

	got, ok := flagValue(cmd, "--permission-prompts")
	if !ok {
		t.Fatal("expected --permission-prompts")
	}
	if got != "none" {
		t.Errorf("--permission-prompts = %q, want none", got)
	}
}

func TestBuildCommand_PermissionPromptsOmittedWhenUnset(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{}).buildCommand()
	if hasFlag(cmd, "--permission-prompts") {
		t.Error("--permission-prompts must not appear when unset: the CLI has its own default")
	}
}

// Under argv delivery each plugin is a flag, which is what makes a long plugin
// list overflow the command line on Windows.
func TestBuildCommand_PluginsViaArgv(t *testing.T) {
	plugins := []types.PluginConfig{
		{Type: "local", Path: "/plugins/a"},
		{Type: "local", Path: "/plugins/b", SkipMCPDiscovery: true},
	}

	for _, delivery := range []types.PluginDelivery{"", types.PluginDeliveryArgv} {
		cmd := newTestTransport(&SubprocessOptions{
			Plugins: plugins, PluginDelivery: delivery,
		}).buildCommand()

		if got, _ := flagValue(cmd, "--plugin-dir"); got != "/plugins/a" {
			t.Errorf("delivery %q: --plugin-dir = %q, want /plugins/a", delivery, got)
		}
		if got, _ := flagValue(cmd, "--plugin-dir-no-mcp"); got != "/plugins/b" {
			t.Errorf("delivery %q: --plugin-dir-no-mcp = %q, want /plugins/b", delivery, got)
		}
	}
}

// Under initialize delivery the flags disappear, since the plugins ride on the
// initialize request instead.
func TestBuildCommand_PluginsViaInitialize(t *testing.T) {
	cmd := newTestTransport(&SubprocessOptions{
		Plugins:        []types.PluginConfig{{Type: "local", Path: "/plugins/a"}},
		PluginDelivery: types.PluginDeliveryInitialize,
		Model:          strPtr("claude-opus-5"),
	}).buildCommand()

	if hasFlag(cmd, "--plugin-dir") {
		t.Error("--plugin-dir must not appear under initialize delivery")
	}
	// Suppressing the plugin flags must not truncate the rest of argv.
	if got, _ := flagValue(cmd, "--model"); got != "claude-opus-5" {
		t.Errorf("--model = %q, want claude-opus-5", got)
	}
}

// Flags emitted after the plugin block must survive initialize delivery.
func TestBuildCommand_ExtraArgsSurviveInitializePluginDelivery(t *testing.T) {
	value := "value"
	cmd := newTestTransport(&SubprocessOptions{
		Plugins:        []types.PluginConfig{{Type: "local", Path: "/plugins/a"}},
		PluginDelivery: types.PluginDeliveryInitialize,
		ExtraArgs:      map[string]*string{"custom-flag": &value},
	}).buildCommand()

	if got, ok := flagValue(cmd, "--custom-flag"); !ok || got != "value" {
		t.Errorf("--custom-flag = %q (present %v), want value", got, ok)
	}
}

// Applications commonly take a session id from external input, so it is
// rejected on Windows for the same reason Resume is.
func TestValidateOptions_SessionIDWindowsMetacharacters(t *testing.T) {
	err := validateOptions(&SubprocessOptions{SessionID: strPtr("id & calc.exe")})

	if runtime.GOOS == "windows" {
		if err == nil {
			t.Fatal("expected the session id to be rejected on Windows")
		}
		return
	}
	if err != nil {
		t.Errorf("POSIX behavior must be unchanged, got %v", err)
	}
}

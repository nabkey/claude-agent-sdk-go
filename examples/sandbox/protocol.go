// Package sandbox runs the Claude Code CLI inside a sandbox and drives it over
// a socket, so the process holding the SDK session does not have to be the
// process that can execute the agent's tool calls.
//
// The package has two halves:
//
//   - [Transport] implements claude.Transport. It dials a sandbox host and
//     relays the SDK's JSON frames to a CLI running on the other side.
//   - [Host] listens on a socket, spawns the CLI, and pipes frames back.
//
// # Why the host builds the argv
//
// Supplying a custom transport to the SDK bypasses its subprocess builder
// entirely: only the initialize request crosses the wire, and every option that
// the SDK would otherwise turn into a CLI flag is dropped. That includes
// --permission-prompt-tool, which is what routes tool approvals back through
// AgentOptions.CanUseTool.
//
// So the host, not the client, constructs the command line. A [Policy] holds
// the flags the sandbox operator controls (working directory, permission mode,
// model, tool allowlists), and [StartRequest] carries the small set a client
// may vary per session. That split is deliberate: the sandbox decides its own
// containment rules rather than trusting whatever dials in.
//
// Callers who need CanUseTool must leave [StartRequest.PermissionPromptTool]
// set to "stdio"; [DefaultStartRequest] does this.
package sandbox

// ProtocolVersion is the frame protocol revision. The host rejects a client
// that does not match, since a mismatch means the argv split above may differ.
const ProtocolVersion = 1

// FrameType discriminates the wire envelope.
type FrameType string

const (
	// FrameHello is the client's opening frame, carrying the token and
	// protocol version. Sent before anything else.
	FrameHello FrameType = "hello"
	// FrameHelloOK is the host's acceptance of a hello.
	FrameHelloOK FrameType = "hello_ok"
	// FrameStart asks the host to spawn the CLI.
	FrameStart FrameType = "start"
	// FrameStarted reports that the CLI is running.
	FrameStarted FrameType = "started"
	// FrameStdin carries one newline-terminated JSON frame for the CLI's
	// stdin, exactly as the SDK handed it to Transport.Write.
	FrameStdin FrameType = "stdin"
	// FrameMsg carries one decoded JSON object read from the CLI's stdout.
	FrameMsg FrameType = "msg"
	// FrameStderr carries one line of the CLI's stderr, for logging.
	FrameStderr FrameType = "stderr"
	// FrameEndInput closes the CLI's stdin without tearing down the session.
	FrameEndInput FrameType = "end_input"
	// FrameClose asks the host to terminate the CLI and hang up.
	FrameClose FrameType = "close"
	// FrameExit reports the CLI's exit status. Terminal.
	FrameExit FrameType = "exit"
	// FrameError reports a host-side failure. Terminal.
	FrameError FrameType = "error"
)

// Frame is the wire envelope. Exactly one payload field is meaningful for any
// given Type; the rest are omitted.
//
// Frames are newline-delimited JSON in both directions, which keeps the host a
// straight pipe: the SDK's own framing passes through untouched inside Data.
type Frame struct {
	Type FrameType `json:"t"`

	// Version and Token are set on FrameHello only.
	Version int    `json:"v,omitempty"`
	Token   string `json:"token,omitempty"`

	// Start is set on FrameStart only.
	Start *StartRequest `json:"start,omitempty"`

	// Data is set on FrameStdin: a newline-terminated JSON frame for the CLI.
	Data string `json:"data,omitempty"`

	// Msg is set on FrameMsg: one decoded object from the CLI's stdout.
	Msg map[string]any `json:"msg,omitempty"`

	// Line is set on FrameStderr.
	Line string `json:"line,omitempty"`

	// Code is set on FrameExit. A pointer so a zero exit is distinguishable
	// from an absent field.
	Code *int `json:"code,omitempty"`

	// Error is set on FrameError, and may also accompany FrameExit to explain
	// a non-zero status.
	Error string `json:"error,omitempty"`
}

// StartRequest is the per-session configuration a client may set. Everything
// else is fixed by the host's [Policy].
//
// The fields here are limited on purpose. Anything that widens what the agent
// can reach — the working directory, extra directories, the permission mode —
// belongs to the host, so a client cannot talk its way out of the sandbox.
type StartRequest struct {
	// PermissionPromptTool must be "stdio" for AgentOptions.CanUseTool to be
	// consulted. Empty means the CLI handles permissions itself, which in a
	// non-interactive session means it cannot prompt at all.
	PermissionPromptTool string `json:"permission_prompt_tool,omitempty"`

	// Resume continues a prior session by ID. Honored only when the host's
	// Policy sets AllowResume.
	Resume string `json:"resume,omitempty"`

	// ForkSession branches a resumed session instead of continuing it.
	ForkSession bool `json:"fork_session,omitempty"`

	// SessionID pins the new session's ID. Honored only under AllowResume,
	// since it lets a client address a session slot by name.
	SessionID string `json:"session_id,omitempty"`

	// IncludePartialMessages streams token-level deltas.
	IncludePartialMessages bool `json:"include_partial_messages,omitempty"`

	// IncludeHookEvents emits hook activity as system messages.
	IncludeHookEvents bool `json:"include_hook_events,omitempty"`

	// SDKMCPServers names the in-process MCP servers the client is exposing,
	// so the host can register them with --mcp-config.
	//
	// This is required for AgentOptions.MCPServers to work at all over a
	// custom transport: the SDK registers in-process servers through that
	// flag, which a custom transport never emits, so without this the CLI
	// never learns the tools exist and silently runs without them.
	//
	// Naming a server here does not widen what the sandbox can reach — the
	// tools run in the client process and are serviced over the control
	// protocol, not in the sandbox.
	SDKMCPServers []string `json:"sdk_mcp_servers,omitempty"`

	// Env sets environment variables for the CLI process. The host drops any
	// name not in its Policy.AllowEnv allowlist.
	Env map[string]string `json:"env,omitempty"`
}

// DefaultStartRequest returns a StartRequest wired for the common case: tool
// approvals routed back to the SDK's CanUseTool callback.
func DefaultStartRequest() StartRequest {
	return StartRequest{PermissionPromptTool: "stdio"}
}

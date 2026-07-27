package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/nabkey/claude-agent-sdk-go/errors"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

const (
	defaultMaxBufferSize     = 1024 * 1024 // 1MB
	minimumClaudeCodeVersion = "2.0.0"
)

// SubprocessTransport implements Transport using the Claude CLI as a subprocess.
type SubprocessTransport struct {
	prompt        string
	isStreaming   bool
	options       *SubprocessOptions
	cliPath       string
	cwd           string
	cmd           *exec.Cmd
	stdin         io.WriteCloser
	stdout        io.ReadCloser
	stderr        io.ReadCloser
	ready         bool
	exitError     error
	maxBufferSize int
	writeMu       sync.Mutex
	closeMu       sync.Mutex
	closed        bool
}

// SubprocessOptions contains configuration for the subprocess transport.
type SubprocessOptions struct {
	// SystemPrompt sets or replaces the system prompt.
	// Can be *string or *types.SystemPromptPreset.
	SystemPrompt any
	// AppendSystemPrompt appends to the default system prompt.
	AppendSystemPrompt *string
	// Tools defines the base set of tools.
	// Can be []string or *types.ToolsPreset.
	Tools any
	// AllowedTools specifies allowed tools.
	AllowedTools []string
	// DisallowedTools specifies disallowed tools.
	DisallowedTools []string
	// MaxTurns limits agentic iterations.
	MaxTurns *int
	// MaxBudgetUSD limits cost.
	MaxBudgetUSD *float64
	// Model specifies the Claude model.
	Model *string
	// FallbackModel specifies a fallback model.
	FallbackModel *string
	// PermissionMode controls permission handling.
	PermissionMode *types.PermissionMode
	// PermissionPromptToolName sets a custom tool for permission prompts.
	PermissionPromptToolName *string
	// ContinueConversation continues the most recent conversation.
	ContinueConversation bool
	// Resume resumes a specific session.
	Resume *string
	// Settings specifies settings JSON or file path.
	Settings *string
	// Sandbox configures sandbox settings.
	Sandbox *types.SandboxSettings
	// AddDirs adds additional directories.
	AddDirs []string
	// MCPServers configures MCP servers.
	MCPServers map[string]types.MCPServerConfig
	// Channels configures channel servers.
	Channels map[string]types.ChannelServerConfig
	// IncludePartialMessages enables partial message streaming.
	IncludePartialMessages bool
	// ForkSession forks instead of continuing sessions.
	ForkSession bool
	// Agents defines custom agents.
	Agents map[string]types.AgentDefinition
	// SettingSources specifies setting sources to load.
	SettingSources []types.SettingSource
	// Plugins configures plugins.
	Plugins []types.PluginConfig
	// ExtraArgs passes arbitrary CLI flags.
	ExtraArgs map[string]*string
	// MaxThinkingTokens limits thinking tokens.
	MaxThinkingTokens *int
	// Thinking configures thinking behavior.
	Thinking types.ThinkingConfig
	// Effort sets effort level.
	Effort *string
	// OutputFormat configures structured output.
	OutputFormat map[string]any
	// Betas enables beta features.
	Betas []types.SdkBeta
	// EnableFileCheckpointing enables file checkpointing.
	EnableFileCheckpointing bool
	// MCPConfigPath specifies MCP config file path.
	MCPConfigPath *string
	// CLIPath specifies a custom CLI path.
	CLIPath *string
	// Cwd sets the working directory.
	Cwd *string
	// Env sets environment variables.
	Env map[string]string
	// MaxBufferSize sets max buffer size.
	MaxBufferSize *int
	// Stderr callback for stderr output.
	Stderr func(string)
	// User sets the Unix user.
	User *string
	// Hooks configuration (for initialize request)
	Hooks map[types.HookEvent][]types.HookMatcher
	// PersistSession controls whether sessions are saved to disk.
	PersistSession *bool
	// ToolConfig provides per-tool configuration. Delivered via the
	// subprocess environment, not as a CLI flag.
	ToolConfig *types.ToolConfig
}

// NewSubprocessTransport creates a new subprocess transport.
func NewSubprocessTransport(prompt string, isStreaming bool, opts *SubprocessOptions) (*SubprocessTransport, error) {
	if opts == nil {
		opts = &SubprocessOptions{}
	}

	if err := validateOptions(opts); err != nil {
		return nil, err
	}

	cliPath := ""
	if opts.CLIPath != nil {
		cliPath = *opts.CLIPath
	} else {
		var err error
		cliPath, err = findCLI()
		if err != nil {
			return nil, err
		}
	}

	// Validate the resolved CLI before anything is spawned with it.
	if err := rejectWindowsBatchCLI(cliPath); err != nil {
		return nil, err
	}

	cwd := ""
	if opts.Cwd != nil {
		cwd = *opts.Cwd
	}

	maxBufferSize := defaultMaxBufferSize
	if opts.MaxBufferSize != nil {
		maxBufferSize = *opts.MaxBufferSize
	}

	return &SubprocessTransport{
		prompt:        prompt,
		isStreaming:   isStreaming,
		options:       opts,
		cliPath:       cliPath,
		cwd:           cwd,
		maxBufferSize: maxBufferSize,
	}, nil
}

// findCLI locates the Claude CLI binary.
func findCLI() (string, error) {
	// Check PATH first
	if cli, err := exec.LookPath("claude"); err == nil {
		return cli, nil
	}

	// Check common locations
	homeDir, _ := os.UserHomeDir()
	locations := []string{
		filepath.Join(homeDir, ".npm-global", "bin", "claude"),
		"/usr/local/bin/claude",
		filepath.Join(homeDir, ".local", "bin", "claude"),
		filepath.Join(homeDir, "node_modules", ".bin", "claude"),
		filepath.Join(homeDir, ".yarn", "bin", "claude"),
		filepath.Join(homeDir, ".claude", "local", "claude"),
	}

	for _, path := range locations {
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}

	return "", errors.NewCLINotFoundError(
		"Claude Code not found. Install with:\n"+
			"  npm install -g @anthropic-ai/claude-code\n\n"+
			"If already installed locally, try:\n"+
			"  export PATH=\"$HOME/node_modules/.bin:$PATH\"\n\n"+
			"Or provide the path via AgentOptions:\n"+
			"  AgentOptions{CLIPath: \"/path/to/claude\"}",
		"",
	)
}

// appendFlagValue appends a --flag/value pair, using the `--flag=value` form
// when the value begins with a dash.
//
// Several CLI options are declared with an *optional* value. In the two-token
// form a dash-leading value is not bound to its flag and is instead parsed as a
// separate flag, letting an untrusted value inject arbitrary CLI flags. The
// equals form always binds the value to the flag.
func appendFlagValue(cmd []string, flag, value string) []string {
	if len(value) > 1 && strings.HasPrefix(value, "-") {
		return append(cmd, fmt.Sprintf("--%s=%s", flag, value))
	}
	return append(cmd, fmt.Sprintf("--%s", flag), value)
}

// buildCommand constructs the CLI command with arguments.
func (t *SubprocessTransport) buildCommand() []string {
	cmd := []string{t.cliPath, "--output-format", "stream-json", "--verbose"}

	opts := t.options

	// System prompt handling
	switch sp := opts.SystemPrompt.(type) {
	case *string:
		cmd = append(cmd, "--system-prompt", *sp)
	case *types.SystemPromptPreset:
		spJSON, _ := json.Marshal(sp)
		cmd = append(cmd, "--system-prompt", string(spJSON))
	case nil:
		if opts.AppendSystemPrompt != nil {
			cmd = append(cmd, "--append-system-prompt", *opts.AppendSystemPrompt)
		} else {
			cmd = append(cmd, "--system-prompt", "")
		}
	}
	if opts.SystemPrompt != nil && opts.AppendSystemPrompt != nil {
		cmd = append(cmd, "--append-system-prompt", *opts.AppendSystemPrompt)
	}

	// Tools. A preset maps to the literal "default"; the CLI does not accept
	// a JSON-encoded preset object here.
	switch tools := opts.Tools.(type) {
	case []string:
		if len(tools) == 0 {
			cmd = append(cmd, "--tools", "")
		} else {
			cmd = append(cmd, "--tools", strings.Join(tools, ","))
		}
	case *types.ToolsPreset:
		cmd = append(cmd, "--tools", "default")
	}

	if len(opts.AllowedTools) > 0 {
		cmd = append(cmd, "--allowedTools", strings.Join(opts.AllowedTools, ","))
	}

	if opts.MaxTurns != nil {
		cmd = append(cmd, "--max-turns", fmt.Sprintf("%d", *opts.MaxTurns))
	}

	if opts.MaxBudgetUSD != nil {
		cmd = append(cmd, "--max-budget-usd", fmt.Sprintf("%f", *opts.MaxBudgetUSD))
	}

	if len(opts.DisallowedTools) > 0 {
		cmd = append(cmd, "--disallowedTools", strings.Join(opts.DisallowedTools, ","))
	}

	if opts.Model != nil {
		cmd = append(cmd, "--model", *opts.Model)
	}

	if opts.FallbackModel != nil {
		cmd = append(cmd, "--fallback-model", *opts.FallbackModel)
	}

	if len(opts.Betas) > 0 {
		betaStrs := make([]string, len(opts.Betas))
		for i, b := range opts.Betas {
			betaStrs[i] = string(b)
		}
		cmd = append(cmd, "--betas", strings.Join(betaStrs, ","))
	}

	if opts.PermissionPromptToolName != nil {
		cmd = append(cmd, "--permission-prompt-tool", *opts.PermissionPromptToolName)
	}

	if opts.PermissionMode != nil {
		cmd = append(cmd, "--permission-mode", string(*opts.PermissionMode))
	}

	if opts.ContinueConversation {
		cmd = append(cmd, "--continue")
	}

	// Bind with `=` so a dash-leading session title cannot inject CLI flags:
	// --resume takes an optional value, so the two-token form would not bind.
	if opts.Resume != nil {
		cmd = append(cmd, fmt.Sprintf("--resume=%s", *opts.Resume))
	}

	// Settings and sandbox handling
	settingsValue := t.buildSettingsValue()
	if settingsValue != "" {
		cmd = append(cmd, "--settings", settingsValue)
	}

	for _, dir := range opts.AddDirs {
		cmd = append(cmd, "--add-dir", dir)
	}

	// MCP servers
	if len(opts.MCPServers) > 0 {
		serversForCLI := make(map[string]any)
		for name, config := range opts.MCPServers {
			switch c := config.(type) {
			case *types.SDKMCPServer:
				// For SDK servers, exclude the instance field
				serversForCLI[name] = map[string]any{
					"type": "sdk",
					"name": c.Name,
				}
			default:
				serversForCLI[name] = config
			}
		}
		if len(serversForCLI) > 0 {
			mcpConfig := map[string]any{"mcpServers": serversForCLI}
			mcpJSON, _ := json.Marshal(mcpConfig)
			cmd = append(cmd, "--mcp-config", string(mcpJSON))
		}
	}

	// Channel servers
	if len(opts.Channels) > 0 {
		channelsForCLI := make(map[string]any)
		for name, config := range opts.Channels {
			channelsForCLI[name] = config
		}
		channelsJSON, _ := json.Marshal(channelsForCLI)
		cmd = append(cmd, "--channels", string(channelsJSON))
	}

	if opts.IncludePartialMessages {
		cmd = append(cmd, "--include-partial-messages")
	}

	if opts.ForkSession {
		cmd = append(cmd, "--fork-session")
	}

	// Agents
	if len(opts.Agents) > 0 {
		agentsMap := make(map[string]any)
		for name, agent := range opts.Agents {
			agentMap := map[string]any{
				"description": agent.Description,
				"prompt":      agent.Prompt,
			}
			if agent.Tools != nil {
				agentMap["tools"] = agent.Tools
			}
			if agent.Model != nil {
				agentMap["model"] = *agent.Model
			}
			if agent.Skills != nil {
				agentMap["skills"] = agent.Skills
			}
			if agent.Memory != nil {
				agentMap["memory"] = *agent.Memory
			}
			if agent.MCPServers != nil {
				agentMap["mcpServers"] = agent.MCPServers
			}
			agentsMap[name] = agentMap
		}
		agentsJSON, _ := json.Marshal(agentsMap)
		cmd = append(cmd, "--agents", string(agentsJSON))
	}

	// Setting sources. Only emitted when explicitly configured: omitting the
	// flag lets the CLI load all sources (its default), whereas passing an
	// empty value disables filesystem settings entirely. A nil slice must
	// therefore not produce a flag, while an empty non-nil slice must.
	if opts.SettingSources != nil {
		sources := make([]string, len(opts.SettingSources))
		for i, s := range opts.SettingSources {
			sources[i] = string(s)
		}
		cmd = append(cmd, fmt.Sprintf("--setting-sources=%s", strings.Join(sources, ",")))
	}

	// Plugins
	for _, plugin := range opts.Plugins {
		if plugin.Type == "local" {
			cmd = append(cmd, "--plugin-dir", plugin.Path)
		}
	}

	// Extra args. Sort for deterministic argv ordering across runs.
	extraFlags := make([]string, 0, len(opts.ExtraArgs))
	for flag := range opts.ExtraArgs {
		extraFlags = append(extraFlags, flag)
	}
	sort.Strings(extraFlags)
	for _, flag := range extraFlags {
		value := opts.ExtraArgs[flag]
		if value == nil {
			cmd = append(cmd, fmt.Sprintf("--%s", flag))
		} else {
			cmd = appendFlagValue(cmd, flag, *value)
		}
	}

	// Thinking configuration. `Thinking` takes precedence over the deprecated
	// `MaxThinkingTokens`. The CLI models this as two separate flags rather
	// than a single JSON payload:
	//
	//   adaptive              -> --thinking adaptive
	//   enabled w/ budget     -> --max-thinking-tokens N
	//   enabled w/o budget    -> --thinking adaptive
	//   disabled              -> --thinking disabled
	if opts.Thinking != nil {
		switch cfg := opts.Thinking.(type) {
		case *types.ThinkingConfigAdaptive:
			cmd = append(cmd, "--thinking", "adaptive")
		case *types.ThinkingConfigEnabled:
			if cfg.BudgetTokens != nil {
				cmd = append(cmd, "--max-thinking-tokens", fmt.Sprintf("%d", *cfg.BudgetTokens))
			} else {
				cmd = append(cmd, "--thinking", "adaptive")
			}
		case *types.ThinkingConfigDisabled:
			cmd = append(cmd, "--thinking", "disabled")
		}
		if display := opts.Thinking.DisplayMode(); display != nil {
			cmd = append(cmd, "--thinking-display", string(*display))
		}
	} else if opts.MaxThinkingTokens != nil {
		cmd = append(cmd, "--max-thinking-tokens", fmt.Sprintf("%d", *opts.MaxThinkingTokens))
	}

	if opts.Effort != nil {
		cmd = append(cmd, "--effort", *opts.Effort)
	}

	// Note: file checkpointing, per-tool config, and agent progress summaries
	// are NOT CLI flags. Checkpointing and tool config are delivered through
	// the subprocess environment (see buildEnv); agent progress summaries are
	// sent as a field on the initialize control request.

	// MCPConfigPath is only used when MCPServers is not set
	if opts.MCPConfigPath != nil && len(opts.MCPServers) == 0 {
		cmd = append(cmd, "--mcp-config", *opts.MCPConfigPath)
	}

	if opts.PersistSession != nil && !*opts.PersistSession {
		cmd = append(cmd, "--no-session-persistence")
	}

	// Output format / JSON schema
	if opts.OutputFormat != nil {
		if schemaType, ok := opts.OutputFormat["type"].(string); ok && schemaType == "json_schema" {
			if schema, ok := opts.OutputFormat["schema"]; ok {
				schemaJSON, _ := json.Marshal(schema)
				cmd = append(cmd, "--json-schema", string(schemaJSON))
			}
		}
	}

	// Input handling - must come after all flags
	if t.isStreaming {
		cmd = append(cmd, "--input-format", "stream-json")
	} else {
		cmd = append(cmd, "--print", "--", t.prompt)
	}

	return cmd
}

// buildSettingsValue builds the settings value, merging sandbox if provided.
func (t *SubprocessTransport) buildSettingsValue() string {
	opts := t.options
	hasSettings := opts.Settings != nil
	hasSandbox := opts.Sandbox != nil

	if !hasSettings && !hasSandbox {
		return ""
	}

	if hasSettings && !hasSandbox {
		return *opts.Settings
	}

	// Need to merge sandbox into settings
	settingsObj := make(map[string]any)

	if hasSettings {
		settingsStr := strings.TrimSpace(*opts.Settings)
		if strings.HasPrefix(settingsStr, "{") && strings.HasSuffix(settingsStr, "}") {
			_ = json.Unmarshal([]byte(settingsStr), &settingsObj)
		} else {
			// It's a file path
			data, err := os.ReadFile(settingsStr)
			if err == nil {
				_ = json.Unmarshal(data, &settingsObj)
			}
		}
	}

	if hasSandbox {
		settingsObj["sandbox"] = opts.Sandbox
	}

	result, _ := json.Marshal(settingsObj)
	return string(result)
}

// buildEnv assembles the subprocess environment.
//
// Several options are configured through the environment rather than through
// CLI flags: file checkpointing and the AskUserQuestion preview format have no
// corresponding flag on the CLI.
//
// Precedence, lowest to highest: the inherited process environment, the SDK's
// default entrypoint marker, caller-supplied Env, then SDK-controlled values
// that must not be overridden.
func (t *SubprocessTransport) buildEnv() []string {
	opts := t.options

	env := make(map[string]string, len(os.Environ())+8)
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}

	// Default entrypoint marker; callers may override it via Env.
	env["CLAUDE_CODE_ENTRYPOINT"] = "sdk-go"

	for k, v := range opts.Env {
		env[k] = v
	}

	// SDK-controlled values. These are applied after caller Env so the
	// transport's own configuration cannot be silently disabled.
	if opts.EnableFileCheckpointing {
		env["CLAUDE_CODE_ENABLE_SDK_FILE_CHECKPOINTING"] = "true"
	}
	if opts.ToolConfig != nil &&
		opts.ToolConfig.AskUserQuestion != nil &&
		opts.ToolConfig.AskUserQuestion.PreviewFormat != nil {
		env["CLAUDE_CODE_QUESTION_PREVIEW_FORMAT"] = string(*opts.ToolConfig.AskUserQuestion.PreviewFormat)
	}
	if t.cwd != "" {
		env["PWD"] = t.cwd
	}

	// Flatten deterministically so argv/env dumps are stable across runs.
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	result := make([]string, 0, len(keys))
	for _, k := range keys {
		result = append(result, k+"="+env[k])
	}
	return result
}

// Connect starts the subprocess and prepares for communication.
func (t *SubprocessTransport) Connect(ctx context.Context) error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()

	if t.cmd != nil {
		return nil
	}

	// Check CLI version (optional)
	if os.Getenv("CLAUDE_AGENT_SDK_SKIP_VERSION_CHECK") == "" {
		t.checkCLIVersion(ctx)
	}

	cmdArgs := t.buildCommand()
	t.cmd = exec.CommandContext(ctx, cmdArgs[0], cmdArgs[1:]...)

	t.cmd.Env = t.buildEnv()

	if t.cwd != "" {
		t.cmd.Dir = t.cwd
	}

	// Set up pipes
	var err error
	t.stdin, err = t.cmd.StdinPipe()
	if err != nil {
		return errors.NewCLIConnectionError("Failed to create stdin pipe", err)
	}

	t.stdout, err = t.cmd.StdoutPipe()
	if err != nil {
		return errors.NewCLIConnectionError("Failed to create stdout pipe", err)
	}

	t.stderr, err = t.cmd.StderrPipe()
	if err != nil {
		return errors.NewCLIConnectionError("Failed to create stderr pipe", err)
	}

	// Start the process
	if err := t.cmd.Start(); err != nil {
		if t.cwd != "" {
			if _, statErr := os.Stat(t.cwd); os.IsNotExist(statErr) {
				return errors.NewCLIConnectionError(
					fmt.Sprintf("Working directory does not exist: %s", t.cwd),
					err,
				)
			}
		}
		return errors.NewCLINotFoundError(
			fmt.Sprintf("Failed to start Claude Code: %v", err),
			t.cliPath,
		)
	}

	// Handle stderr in background
	go t.handleStderr()

	// For non-streaming mode, close stdin immediately
	if !t.isStreaming {
		_ = t.stdin.Close()
		t.stdin = nil
	}

	t.ready = true
	return nil
}

// handleStderr reads stderr and invokes callbacks.
func (t *SubprocessTransport) handleStderr() {
	if t.stderr == nil {
		return
	}

	scanner := bufio.NewScanner(t.stderr)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		if t.options.Stderr != nil {
			t.options.Stderr(line)
		}
	}
}

// Write sends data to the subprocess stdin.
func (t *SubprocessTransport) Write(ctx context.Context, data string) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if !t.ready || t.stdin == nil {
		return errors.NewCLIConnectionError("Transport is not ready for writing", nil)
	}

	if t.closed {
		return errors.NewCLIConnectionError("Transport is closed", nil)
	}

	if t.cmd != nil && t.cmd.ProcessState != nil && t.cmd.ProcessState.Exited() {
		return errors.NewCLIConnectionError(
			fmt.Sprintf("Cannot write to terminated process (exit code: %d)", t.cmd.ProcessState.ExitCode()),
			nil,
		)
	}

	_, err := io.WriteString(t.stdin, data)
	if err != nil {
		t.ready = false
		t.exitError = errors.NewCLIConnectionError("Failed to write to process stdin", err)
		return t.exitError
	}

	return nil
}

// ReadMessages returns channels for messages and errors from stdout.
func (t *SubprocessTransport) ReadMessages(ctx context.Context) (<-chan map[string]any, <-chan error) {
	msgChan := make(chan map[string]any, 100)
	errChan := make(chan error, 1)

	go func() {
		defer close(msgChan)
		defer close(errChan)

		if t.stdout == nil {
			errChan <- errors.NewCLIConnectionError("Not connected", nil)
			return
		}

		scanner := bufio.NewScanner(t.stdout)
		scanner.Buffer(make([]byte, t.maxBufferSize), t.maxBufferSize)

		var jsonBuffer strings.Builder

		for scanner.Scan() {
			select {
			case <-ctx.Done():
				return
			default:
			}

			line := strings.TrimSpace(scanner.Text())
			if line == "" {
				continue
			}

			// Handle potential partial JSON
			jsonBuffer.WriteString(line)

			if jsonBuffer.Len() > t.maxBufferSize {
				errChan <- errors.NewCLIJSONDecodeError(
					fmt.Sprintf("Buffer size %d exceeds limit %d", jsonBuffer.Len(), t.maxBufferSize),
					nil,
				)
				jsonBuffer.Reset()
				continue
			}

			var data map[string]any
			if err := json.Unmarshal([]byte(jsonBuffer.String()), &data); err != nil {
				// Not yet complete JSON, continue accumulating
				continue
			}

			jsonBuffer.Reset()

			select {
			case msgChan <- data:
			case <-ctx.Done():
				return
			}
		}

		if err := scanner.Err(); err != nil {
			errChan <- errors.NewCLIConnectionError("Error reading stdout", err)
		}

		// Wait for process to complete
		if t.cmd != nil {
			if err := t.cmd.Wait(); err != nil {
				exitCode := -1
				if t.cmd.ProcessState != nil {
					exitCode = t.cmd.ProcessState.ExitCode()
				}
				if exitCode != 0 {
					errChan <- errors.NewProcessError(
						fmt.Sprintf("Command failed with exit code %d", exitCode),
						exitCode,
						"",
					)
				}
			}
		}
	}()

	return msgChan, errChan
}

// EndInput closes the stdin pipe.
func (t *SubprocessTransport) EndInput() error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if t.stdin != nil {
		err := t.stdin.Close()
		t.stdin = nil
		return err
	}
	return nil
}

// Close terminates the subprocess and cleans up.
func (t *SubprocessTransport) Close() error {
	t.closeMu.Lock()
	defer t.closeMu.Unlock()

	t.closed = true
	t.ready = false

	// Close stdin
	t.writeMu.Lock()
	if t.stdin != nil {
		_ = t.stdin.Close()
		t.stdin = nil
	}
	t.writeMu.Unlock()

	// Close stderr
	if t.stderr != nil {
		_ = t.stderr.Close()
		t.stderr = nil
	}

	// Terminate process
	if t.cmd != nil && t.cmd.Process != nil {
		_ = t.cmd.Process.Kill()
		_ = t.cmd.Wait()
	}

	t.stdout = nil
	t.exitError = nil

	return nil
}

// IsReady returns true if the transport is ready.
func (t *SubprocessTransport) IsReady() bool {
	return t.ready && !t.closed
}

// checkCLIVersion checks if the CLI version meets minimum requirements.
func (t *SubprocessTransport) checkCLIVersion(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 2*1e9) // 2 second timeout
	defer cancel()

	cmd := exec.CommandContext(ctx, t.cliPath, "-v")
	output, err := cmd.Output()
	if err != nil {
		return
	}

	versionStr := strings.TrimSpace(string(output))
	re := regexp.MustCompile(`([0-9]+\.[0-9]+\.[0-9]+)`)
	match := re.FindStringSubmatch(versionStr)
	if len(match) < 2 {
		return
	}

	version := match[1]
	if compareVersions(version, minimumClaudeCodeVersion) < 0 {
		fmt.Fprintf(os.Stderr,
			"Warning: Claude Code version %s is unsupported in the Agent SDK. "+
				"Minimum required version is %s. "+
				"Some features may not work correctly.\n",
			version, minimumClaudeCodeVersion)
	}
}

// compareVersions compares two semver strings.
// Returns -1 if a < b, 0 if a == b, 1 if a > b.
func compareVersions(a, b string) int {
	parseVersion := func(v string) []int {
		parts := strings.Split(v, ".")
		result := make([]int, len(parts))
		for i, p := range parts {
			_, _ = fmt.Sscanf(p, "%d", &result[i])
		}
		return result
	}

	aParts := parseVersion(a)
	bParts := parseVersion(b)

	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}

	for i := 0; i < maxLen; i++ {
		aVal, bVal := 0, 0
		if i < len(aParts) {
			aVal = aParts[i]
		}
		if i < len(bParts) {
			bVal = bParts[i]
		}
		if aVal < bVal {
			return -1
		}
		if aVal > bVal {
			return 1
		}
	}
	return 0
}

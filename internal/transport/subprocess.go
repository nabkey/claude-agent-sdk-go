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
	"runtime"
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
	SystemPrompt *string
	// AppendSystemPrompt appends to the default system prompt.
	AppendSystemPrompt *string
	// Tools defines the base set of tools.
	Tools []string
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
	// OutputFormat configures structured output.
	OutputFormat map[string]any
	// Betas enables beta features.
	Betas []string
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
}

// NewSubprocessTransport creates a new subprocess transport.
func NewSubprocessTransport(prompt string, isStreaming bool, opts *SubprocessOptions) (*SubprocessTransport, error) {
	if opts == nil {
		opts = &SubprocessOptions{}
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

// buildCommand constructs the CLI command with arguments.
func (t *SubprocessTransport) buildCommand() []string {
	cmd := []string{t.cliPath, "--output-format", "stream-json", "--verbose"}

	opts := t.options

	// System prompt handling
	if opts.SystemPrompt == nil && opts.AppendSystemPrompt == nil {
		cmd = append(cmd, "--system-prompt", "")
	} else if opts.SystemPrompt != nil {
		cmd = append(cmd, "--system-prompt", *opts.SystemPrompt)
	} else if opts.AppendSystemPrompt != nil {
		cmd = append(cmd, "--append-system-prompt", *opts.AppendSystemPrompt)
	}

	// Tools
	if opts.Tools != nil {
		if len(opts.Tools) == 0 {
			cmd = append(cmd, "--tools", "")
		} else {
			cmd = append(cmd, "--tools", strings.Join(opts.Tools, ","))
		}
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
		cmd = append(cmd, "--betas", strings.Join(opts.Betas, ","))
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

	if opts.Resume != nil {
		cmd = append(cmd, "--resume", *opts.Resume)
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
			agentsMap[name] = agentMap
		}
		agentsJSON, _ := json.Marshal(agentsMap)
		cmd = append(cmd, "--agents", string(agentsJSON))
	}

	// Setting sources
	sourcesValue := ""
	if opts.SettingSources != nil {
		sources := make([]string, len(opts.SettingSources))
		for i, s := range opts.SettingSources {
			sources[i] = string(s)
		}
		sourcesValue = strings.Join(sources, ",")
	}
	cmd = append(cmd, "--setting-sources", sourcesValue)

	// Plugins
	for _, plugin := range opts.Plugins {
		if plugin.Type == "local" {
			cmd = append(cmd, "--plugin-dir", plugin.Path)
		}
	}

	// Extra args
	for flag, value := range opts.ExtraArgs {
		if value == nil {
			cmd = append(cmd, fmt.Sprintf("--%s", flag))
		} else {
			cmd = append(cmd, fmt.Sprintf("--%s", flag), *value)
		}
	}

	if opts.MaxThinkingTokens != nil {
		cmd = append(cmd, "--max-thinking-tokens", fmt.Sprintf("%d", *opts.MaxThinkingTokens))
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

	// Set up environment
	env := os.Environ()
	if t.options.Env != nil {
		for k, v := range t.options.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}
	env = append(env, "CLAUDE_CODE_ENTRYPOINT=sdk-go")
	if t.cwd != "" {
		env = append(env, fmt.Sprintf("PWD=%s", t.cwd))
	}
	t.cmd.Env = env

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

// Ensure unused imports are used (for runtime)
var _ = runtime.GOOS

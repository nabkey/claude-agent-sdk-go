// Package claude provides the Claude Agent SDK for Go.
package claude

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// AgentOptions configures the behavior of Query and Client operations.
type AgentOptions struct {
	// Tools defines the base set of tools available.
	// Can be a []string of tool names or nil to use defaults.
	Tools []string

	// AllowedTools specifies which tools are allowed to be used.
	AllowedTools []string

	// DisallowedTools specifies which tools are not allowed.
	DisallowedTools []string

	// SystemPrompt sets or replaces the system prompt.
	// Can be a string or nil to use the default.
	SystemPrompt *string

	// AppendSystemPrompt appends to the default system prompt.
	AppendSystemPrompt *string

	// MCPServers configures MCP servers by name.
	MCPServers map[string]types.MCPServerConfig

	// PermissionMode controls how tool permissions are handled.
	PermissionMode *types.PermissionMode

	// ContinueConversation continues the most recent conversation.
	ContinueConversation bool

	// Resume resumes a specific session by ID.
	Resume *string

	// MaxTurns limits the number of agentic iterations.
	MaxTurns *int

	// MaxBudgetUSD limits the maximum cost in USD.
	MaxBudgetUSD *float64

	// Model specifies the Claude model to use.
	Model *string

	// FallbackModel specifies a fallback model if the primary is unavailable.
	FallbackModel *string

	// PermissionPromptToolName sets a custom tool for permission prompts.
	// Set to "stdio" for SDK control protocol.
	PermissionPromptToolName *string

	// Cwd sets the working directory for the CLI.
	Cwd *string

	// CLIPath specifies a custom path to the Claude CLI binary.
	CLIPath *string

	// Settings specifies settings as JSON string or file path.
	Settings *string

	// AddDirs adds additional directories for context.
	AddDirs []string

	// Env sets environment variables for the CLI process.
	Env map[string]string

	// ExtraArgs passes arbitrary CLI flags.
	ExtraArgs map[string]*string

	// MaxBufferSize sets the maximum buffer size for CLI output (default: 1MB).
	MaxBufferSize *int

	// Stderr is a callback for stderr output from CLI.
	Stderr func(string)

	// CanUseTool is a callback for tool permission requests.
	// Only works in streaming mode.
	CanUseTool CanUseToolCallback

	// Hooks configures hook callbacks for various events.
	Hooks map[types.HookEvent][]types.HookMatcher

	// User sets the Unix user to run the CLI process as.
	User *string

	// IncludePartialMessages enables streaming of partial messages.
	IncludePartialMessages bool

	// ForkSession creates a new session when resuming instead of continuing.
	ForkSession bool

	// Agents defines custom agent configurations.
	Agents map[string]types.AgentDefinition

	// SettingSources specifies which setting sources to load.
	SettingSources []types.SettingSource

	// Sandbox configures bash command isolation.
	Sandbox *types.SandboxSettings

	// Plugins configures plugin directories.
	Plugins []types.PluginConfig

	// MaxThinkingTokens limits tokens for thinking blocks.
	MaxThinkingTokens *int

	// OutputFormat configures structured output format.
	// Example: map[string]any{"type": "json_schema", "schema": ...}
	OutputFormat map[string]any

	// Betas enables beta features.
	Betas []string
}

// CanUseToolCallback is the function signature for tool permission callbacks.
type CanUseToolCallback func(
	ctx context.Context,
	toolName string,
	input map[string]any,
	permissionCtx types.ToolPermissionContext,
) (types.PermissionResult, error)

// DefaultAgentOptions returns AgentOptions with sensible defaults.
func DefaultAgentOptions() *AgentOptions {
	return &AgentOptions{
		Env:       make(map[string]string),
		ExtraArgs: make(map[string]*string),
	}
}

// WithSystemPrompt sets the system prompt.
func (o *AgentOptions) WithSystemPrompt(prompt string) *AgentOptions {
	o.SystemPrompt = &prompt
	return o
}

// WithAppendSystemPrompt appends to the default system prompt.
func (o *AgentOptions) WithAppendSystemPrompt(append string) *AgentOptions {
	o.AppendSystemPrompt = &append
	return o
}

// WithMaxTurns sets the maximum number of turns.
func (o *AgentOptions) WithMaxTurns(turns int) *AgentOptions {
	o.MaxTurns = &turns
	return o
}

// WithPermissionMode sets the permission mode.
func (o *AgentOptions) WithPermissionMode(mode types.PermissionMode) *AgentOptions {
	o.PermissionMode = &mode
	return o
}

// WithCwd sets the working directory.
func (o *AgentOptions) WithCwd(cwd string) *AgentOptions {
	o.Cwd = &cwd
	return o
}

// WithCLIPath sets a custom CLI path.
func (o *AgentOptions) WithCLIPath(path string) *AgentOptions {
	o.CLIPath = &path
	return o
}

// WithModel sets the model to use.
func (o *AgentOptions) WithModel(model string) *AgentOptions {
	o.Model = &model
	return o
}

// WithMCPServer adds an MCP server configuration.
func (o *AgentOptions) WithMCPServer(name string, config types.MCPServerConfig) *AgentOptions {
	if o.MCPServers == nil {
		o.MCPServers = make(map[string]types.MCPServerConfig)
	}
	o.MCPServers[name] = config
	return o
}

// WithAllowedTools sets the allowed tools.
func (o *AgentOptions) WithAllowedTools(tools ...string) *AgentOptions {
	o.AllowedTools = tools
	return o
}

// WithHook adds a hook for a specific event.
func (o *AgentOptions) WithHook(event types.HookEvent, matcher types.HookMatcher) *AgentOptions {
	if o.Hooks == nil {
		o.Hooks = make(map[types.HookEvent][]types.HookMatcher)
	}
	o.Hooks[event] = append(o.Hooks[event], matcher)
	return o
}

// WithCanUseTool sets the tool permission callback.
func (o *AgentOptions) WithCanUseTool(callback CanUseToolCallback) *AgentOptions {
	o.CanUseTool = callback
	return o
}

// WithEnv adds an environment variable.
func (o *AgentOptions) WithEnv(key, value string) *AgentOptions {
	if o.Env == nil {
		o.Env = make(map[string]string)
	}
	o.Env[key] = value
	return o
}

// Clone creates a copy of the AgentOptions.
func (o *AgentOptions) Clone() *AgentOptions {
	if o == nil {
		return nil
	}

	// Helper to clone string slices while preserving nil vs empty distinction
	cloneStringSlice := func(s []string) []string {
		if s == nil {
			return nil
		}
		return append([]string{}, s...)
	}

	clone := &AgentOptions{
		Tools:                    cloneStringSlice(o.Tools),
		AllowedTools:             cloneStringSlice(o.AllowedTools),
		DisallowedTools:          cloneStringSlice(o.DisallowedTools),
		SystemPrompt:             o.SystemPrompt,
		AppendSystemPrompt:       o.AppendSystemPrompt,
		PermissionMode:           o.PermissionMode,
		ContinueConversation:     o.ContinueConversation,
		Resume:                   o.Resume,
		MaxTurns:                 o.MaxTurns,
		MaxBudgetUSD:             o.MaxBudgetUSD,
		Model:                    o.Model,
		FallbackModel:            o.FallbackModel,
		PermissionPromptToolName: o.PermissionPromptToolName,
		Cwd:                      o.Cwd,
		CLIPath:                  o.CLIPath,
		Settings:                 o.Settings,
		AddDirs:                  cloneStringSlice(o.AddDirs),
		MaxBufferSize:            o.MaxBufferSize,
		Stderr:                   o.Stderr,
		CanUseTool:               o.CanUseTool,
		User:                     o.User,
		IncludePartialMessages:   o.IncludePartialMessages,
		ForkSession:              o.ForkSession,
		SettingSources:           append([]types.SettingSource{}, o.SettingSources...),
		Sandbox:                  o.Sandbox,
		Plugins:                  append([]types.PluginConfig{}, o.Plugins...),
		MaxThinkingTokens:        o.MaxThinkingTokens,
		Betas:                    cloneStringSlice(o.Betas),
	}

	// Deep copy maps
	if o.MCPServers != nil {
		clone.MCPServers = make(map[string]types.MCPServerConfig)
		for k, v := range o.MCPServers {
			clone.MCPServers[k] = v
		}
	}

	if o.Env != nil {
		clone.Env = make(map[string]string)
		for k, v := range o.Env {
			clone.Env[k] = v
		}
	}

	if o.ExtraArgs != nil {
		clone.ExtraArgs = make(map[string]*string)
		for k, v := range o.ExtraArgs {
			clone.ExtraArgs[k] = v
		}
	}

	if o.Hooks != nil {
		clone.Hooks = make(map[types.HookEvent][]types.HookMatcher)
		for k, v := range o.Hooks {
			clone.Hooks[k] = append([]types.HookMatcher{}, v...)
		}
	}

	if o.Agents != nil {
		clone.Agents = make(map[string]types.AgentDefinition)
		for k, v := range o.Agents {
			clone.Agents[k] = v
		}
	}

	if o.OutputFormat != nil {
		clone.OutputFormat = make(map[string]any)
		for k, v := range o.OutputFormat {
			clone.OutputFormat[k] = v
		}
	}

	return clone
}

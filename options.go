// Package claude provides the Claude Agent SDK for Go.
package claude

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// AgentOptions configures the behavior of Query and Client operations.
type AgentOptions struct {
	// --- Tools and permissions ---------------------------------------------

	// Tools defines the base set of built-in tools available.
	// Accepts []string of tool names, *types.ToolsPreset, or nil for the
	// CLI's defaults. An empty []string disables all built-in tools.
	Tools any

	// AllowedTools lists tools auto-approved without prompting. To restrict
	// which tools exist at all, use Tools instead.
	AllowedTools []string

	// DisallowedTools lists tools removed from the model's context entirely.
	DisallowedTools []string

	// ToolAliases redirects model-emitted tool names before resolution, e.g.
	// {"Bash": "mcp__workspace__bash"} to route Bash into a sandboxed MCP
	// tool. Single-hop: an alias pointing at another alias resolves literally.
	ToolAliases map[string]string

	// ToolConfig provides per-tool configuration for built-in tools.
	ToolConfig *types.ToolConfig

	// PermissionMode controls how tool permissions are handled.
	PermissionMode *types.PermissionMode

	// AllowDangerouslySkipPermissions must be set alongside
	// types.PermissionModeBypassPermissions. It is a deliberate speed bump.
	AllowDangerouslySkipPermissions bool

	// PermissionPromptToolName routes permission requests through an MCP
	// tool. Mutually exclusive with CanUseTool.
	PermissionPromptToolName *string

	// PermissionPrompts controls what happens to a prompt no host will
	// answer. Nil leaves the CLI default (host) in place; set
	// types.PermissionPromptsNone to auto-deny in an unattended session
	// without disabling auto mode's classifier.
	PermissionPrompts *types.PermissionPromptsMode

	// CanUseTool is called when a tool call would otherwise prompt the user.
	// It is not consulted for calls already permitted by AllowedTools,
	// PermissionMode, or settings allow rules -- those never reach a prompt.
	// To gate every call regardless, use a PreToolUse hook.
	CanUseTool CanUseToolCallback

	// PlanModeInstructions replaces the default workflow body in plan mode's
	// system reminder. Only meaningful with types.PermissionModePlan.
	PlanModeInstructions *string

	// --- Prompting ----------------------------------------------------------

	// SystemPrompt sets or replaces the system prompt. Accepts *string,
	// []string (blocks; include types.SystemPromptDynamicBoundary to mark the
	// cacheable prefix), *types.SystemPromptPreset, *types.SystemPromptFile,
	// or nil.
	SystemPrompt any

	// AppendSystemPrompt appends to the system prompt.
	AppendSystemPrompt *string

	// Agent runs the main thread as a named agent, applying that agent's
	// prompt, tool restrictions, and model. The agent must be defined in
	// Agents or in settings.
	Agent *string

	// Agents defines custom subagents invokable via the Agent tool.
	Agents map[string]types.AgentDefinition

	// Skills enables skills for the main session. Accepts []string of skill
	// names or types.SkillsAll. This is the single place to turn skills on:
	// the SDK also allows the Skill tool and defaults SettingSources so the
	// CLI can discover them.
	//
	// This is a context filter, not a sandbox: unlisted skills are hidden
	// from the model but their files remain readable. Do not store secrets
	// in skill files.
	Skills any

	// --- Model and reasoning -------------------------------------------------

	// Model specifies the Claude model to use.
	Model *string

	// FallbackModel is used if the primary model is overloaded. It must
	// differ from Model.
	FallbackModel *string

	// MaxTurns limits the number of agentic iterations.
	MaxTurns *int

	// MaxBudgetUSD stops the query once this cost is exceeded.
	MaxBudgetUSD *float64

	// TaskBudget makes the model aware of a remaining token budget so it can
	// pace tool use and wrap up before the limit.
	TaskBudget *types.TaskBudget

	// MaxThinkingTokens limits thinking tokens.
	//
	// Deprecated: use Thinking, which takes precedence when both are set.
	MaxThinkingTokens *int

	// Thinking configures extended thinking behavior.
	Thinking types.ThinkingConfig

	// Effort controls how much effort Claude puts into its response.
	Effort *types.EffortLevel

	// OutputFormat requests structured output, e.g.
	// {"type": "json_schema", "schema": {...}}.
	OutputFormat map[string]any

	// Betas enables beta features.
	Betas []types.SdkBeta

	// --- Session ------------------------------------------------------------

	// ContinueConversation resumes the most recent conversation in Cwd.
	ContinueConversation bool

	// Resume loads history from a specific session ID.
	Resume *string

	// ResumeSessionAt resumes only up to and including this message UUID.
	ResumeSessionAt *string

	// ResumeDropsTurn declares which turn a truncating ResumeSessionAt
	// intends to discard, naming that turn's user message UUID. The CLI
	// refuses the resume if anything outside that turn would be dropped, so
	// a rewind-to-before-the-last-prompt cannot silently discard messages the
	// caller never observed. Ignored without ResumeSessionAt.
	ResumeDropsTurn *string

	// SessionID pins a UUID for a new session instead of generating one.
	SessionID *string

	// ForkSession branches a resumed session to a new ID rather than
	// continuing it.
	ForkSession bool

	// PerTaskStopAffordance narrows Interrupt to the current turn, leaving
	// background agents and workflows running. Without it -- and always for
	// one-shot Query prompts -- an interrupt stops them too.
	PerTaskStopAffordance bool

	// Title names a new session instead of auto-generating one. When
	// resuming, the persisted title wins.
	Title *string

	// PersistSession controls whether sessions are written to disk.
	// Nil persists normally.
	PersistSession *bool

	// SessionStore mirrors session transcripts to external storage and lets
	// Resume materialize from it when the local file is absent.
	SessionStore SessionStore

	// SessionStoreFlush controls how aggressively mirrored entries are
	// flushed. Ignored when SessionStore is nil.
	SessionStoreFlush types.SessionStoreFlushMode

	// LoadTimeoutMS bounds each SessionStore load during resume
	// materialization. Zero uses the default of 60s.
	LoadTimeoutMS int

	// --- MCP and plugins -----------------------------------------------------

	// MCPServers configures MCP servers by name.
	MCPServers map[string]types.MCPServerConfig

	// MCPConfigPath points at an MCP config file. Used only when MCPServers
	// is empty.
	MCPConfigPath *string

	// StrictMCPConfig uses only the servers in MCPServers, ignoring project
	// .mcp.json, user settings, plugins, and agent frontmatter.
	StrictMCPConfig bool

	// Channels configures channel servers that push messages into the
	// session. Research preview.
	Channels map[string]types.ChannelServerConfig

	// Plugins loads local plugins providing commands, agents, skills, hooks.
	Plugins []types.PluginConfig

	// PluginDelivery selects how Plugins reaches the CLI. Empty means
	// types.PluginDeliveryArgv.
	PluginDelivery types.PluginDelivery

	// --- Callbacks -----------------------------------------------------------

	// Hooks configures callbacks for lifecycle events.
	//
	// Matchers registered on the same event are dispatched concurrently by
	// the CLI, so each hook must be independent.
	Hooks map[types.HookEvent][]types.HookMatcher

	// OnElicitation handles MCP elicitation requests -- an MCP server asking
	// for user input. Unhandled requests are declined automatically.
	OnElicitation ElicitationCallback

	// OnUserDialog renders blocking dialogs the CLI asks the host to display.
	// Requires SupportedDialogKinds to be non-empty.
	OnUserDialog UserDialogCallback

	// SupportedDialogKinds declares which dialog kinds OnUserDialog can
	// render. The CLI fails closed: an undeclared kind is never emitted.
	SupportedDialogKinds []string

	// Stderr receives stderr lines from the CLI process.
	Stderr func(string)

	// Warn receives advisory SDK warnings, such as a CanUseTool callback that
	// other options will shadow. Nil logs to the standard logger once.
	Warn func(string)

	// --- Output stream --------------------------------------------------------

	// IncludePartialMessages emits StreamEvent messages during streaming.
	IncludePartialMessages bool

	// IncludeHookEvents emits HookEventMessage entries for hook lifecycle.
	IncludeHookEvents bool

	// ForwardSubagentText forwards subagent text and thinking blocks, not
	// just their tool calls, so a nested transcript can be rendered.
	ForwardSubagentText bool

	// PromptSuggestions emits a predicted next user prompt after each turn.
	PromptSuggestions bool

	// AgentProgressSummaries emits periodic AI-generated progress summaries
	// for running subagents on task_progress events.
	AgentProgressSummaries bool

	// --- Environment ----------------------------------------------------------

	// Cwd sets the working directory for the CLI.
	Cwd *string

	// CLIPath overrides discovery of the claude binary.
	CLIPath *string

	// Settings is a settings JSON string or file path, loaded into the
	// highest-priority user-controlled layer.
	Settings *string

	// ManagedSettings supplies policy-tier settings as a JSON string. Filtered
	// restrictive-only, and dropped entirely when an admin managed-settings
	// tier exists unless that admin opted in.
	ManagedSettings *string

	// SettingSources controls which filesystem settings load. Nil loads all
	// sources (the CLI default); an empty non-nil slice disables them.
	// Must include types.SettingSourceProject to load CLAUDE.md.
	SettingSources []types.SettingSource

	// AddDirs grants access to directories beyond Cwd.
	AddDirs []string

	// Sandbox configures bash command isolation.
	Sandbox *types.SandboxSettings

	// EnableFileCheckpointing tracks file changes so Client.RewindFiles can
	// restore them.
	EnableFileCheckpointing bool

	// Env sets environment variables for the CLI process. Set
	// CLAUDE_AGENT_SDK_CLIENT_APP to identify your app in the User-Agent.
	Env map[string]string

	// ExtraArgs passes arbitrary CLI flags. Nil values become bare flags.
	ExtraArgs map[string]*string

	// MaxBufferSize caps a single message read from CLI stdout.
	// Zero uses 1 MiB.
	MaxBufferSize *int

	// User sets the Unix user to run the CLI process as.
	User *string

	// Debug enables verbose CLI debug logging.
	Debug bool

	// DebugFile writes debug logs to a path, implying Debug.
	DebugFile *string
}

// CanUseToolCallback decides whether a tool call may proceed.
type CanUseToolCallback func(
	ctx context.Context,
	toolName string,
	input map[string]any,
	permissionCtx types.ToolPermissionContext,
) (types.PermissionResult, error)

// ElicitationCallback handles an MCP server's request for user input.
type ElicitationCallback func(
	ctx context.Context,
	request types.ElicitationRequest,
) (types.ElicitationResult, error)

// UserDialogCallback renders a blocking dialog on the CLI's behalf.
//
// Return a cancelled result for a dialog kind you do not recognize; the CLI
// then applies that dialog's default behavior.
type UserDialogCallback func(
	ctx context.Context,
	request types.UserDialogRequest,
) (types.UserDialogResult, error)

// effortToString converts an EffortLevel pointer for transport.
func effortToString(e *types.EffortLevel) *string {
	if e == nil {
		return nil
	}
	s := string(*e)
	return &s
}

// DefaultAgentOptions returns AgentOptions with sensible defaults.
func DefaultAgentOptions() *AgentOptions {
	return &AgentOptions{
		Env:       make(map[string]string),
		ExtraArgs: make(map[string]*string),
	}
}

// --- Builder helpers --------------------------------------------------------

// WithSystemPrompt sets the system prompt as a string.
func (o *AgentOptions) WithSystemPrompt(prompt string) *AgentOptions {
	o.SystemPrompt = &prompt
	return o
}

// WithSystemPromptPreset sets a system prompt preset.
func (o *AgentOptions) WithSystemPromptPreset(preset types.SystemPromptPreset) *AgentOptions {
	o.SystemPrompt = &preset
	return o
}

// WithSystemPromptFile loads the system prompt from a file.
func (o *AgentOptions) WithSystemPromptFile(path string) *AgentOptions {
	o.SystemPrompt = &types.SystemPromptFile{Type: "file", Path: path}
	return o
}

// WithAppendSystemPrompt appends to the default system prompt.
func (o *AgentOptions) WithAppendSystemPrompt(text string) *AgentOptions {
	o.AppendSystemPrompt = &text
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

// WithAgent runs the main thread as a named agent.
func (o *AgentOptions) WithAgent(name string) *AgentOptions {
	o.Agent = &name
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

// WithChannel adds a channel server configuration.
func (o *AgentOptions) WithChannel(name string, config types.ChannelServerConfig) *AgentOptions {
	if o.Channels == nil {
		o.Channels = make(map[string]types.ChannelServerConfig)
	}
	o.Channels[name] = config
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

// WithThinking sets the thinking configuration.
func (o *AgentOptions) WithThinking(config types.ThinkingConfig) *AgentOptions {
	o.Thinking = config
	return o
}

// WithEffort sets the effort level.
func (o *AgentOptions) WithEffort(level types.EffortLevel) *AgentOptions {
	o.Effort = &level
	return o
}

// WithFileCheckpointing enables file checkpointing.
func (o *AgentOptions) WithFileCheckpointing() *AgentOptions {
	o.EnableFileCheckpointing = true
	return o
}

// WithMCPConfigPath sets the MCP config file path.
func (o *AgentOptions) WithMCPConfigPath(path string) *AgentOptions {
	o.MCPConfigPath = &path
	return o
}

// WithNoPersistSession disables session persistence.
func (o *AgentOptions) WithNoPersistSession() *AgentOptions {
	f := false
	o.PersistSession = &f
	return o
}

// WithAgentProgressSummaries enables progress summaries for subagents.
func (o *AgentOptions) WithAgentProgressSummaries() *AgentOptions {
	o.AgentProgressSummaries = true
	return o
}

// WithToolConfig sets per-tool configuration for built-in tools.
func (o *AgentOptions) WithToolConfig(config types.ToolConfig) *AgentOptions {
	o.ToolConfig = &config
	return o
}

// WithAskUserQuestionPreviewFormat sets the content format for AskUserQuestion
// option previews. Use types.PreviewFormatHTML for web-based consumers.
func (o *AgentOptions) WithAskUserQuestionPreviewFormat(format types.PreviewFormat) *AgentOptions {
	if o.ToolConfig == nil {
		o.ToolConfig = &types.ToolConfig{}
	}
	if o.ToolConfig.AskUserQuestion == nil {
		o.ToolConfig.AskUserQuestion = &types.AskUserQuestionConfig{}
	}
	o.ToolConfig.AskUserQuestion.PreviewFormat = &format
	return o
}

// WithSkills enables the named skills.
func (o *AgentOptions) WithSkills(names ...string) *AgentOptions {
	o.Skills = names
	return o
}

// WithAllSkills enables every discovered skill.
func (o *AgentOptions) WithAllSkills() *AgentOptions {
	o.Skills = types.SkillsAll
	return o
}

// WithSessionStore mirrors transcripts to an external store.
func (o *AgentOptions) WithSessionStore(store SessionStore) *AgentOptions {
	o.SessionStore = store
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

// cloneTools copies the Tools field, which may be []string or a preset.
func cloneTools(tools any) any {
	switch t := tools.(type) {
	case nil:
		return nil
	case []string:
		if t == nil {
			return nil
		}
		return append([]string{}, t...)
	default:
		return tools
	}
}

// cloneSkills copies the Skills field, which may be []string or SkillsAll.
func cloneSkills(skills any) any {
	if s, ok := skills.([]string); ok {
		if s == nil {
			return nil
		}
		return append([]string{}, s...)
	}
	return skills
}

// Clone creates a copy of the AgentOptions.
//
// Slices and maps are copied so a clone cannot be mutated through the
// original. Callback and interface fields are shared by reference.
func (o *AgentOptions) Clone() *AgentOptions {
	if o == nil {
		return nil
	}

	cloneStrings := func(s []string) []string {
		if s == nil {
			return nil
		}
		return append([]string{}, s...)
	}

	clone := *o

	clone.Tools = cloneTools(o.Tools)
	clone.Skills = cloneSkills(o.Skills)
	clone.AllowedTools = cloneStrings(o.AllowedTools)
	clone.DisallowedTools = cloneStrings(o.DisallowedTools)
	clone.AddDirs = cloneStrings(o.AddDirs)
	clone.SupportedDialogKinds = cloneStrings(o.SupportedDialogKinds)

	if o.SettingSources != nil {
		clone.SettingSources = append([]types.SettingSource{}, o.SettingSources...)
	}
	if o.Plugins != nil {
		clone.Plugins = append([]types.PluginConfig{}, o.Plugins...)
	}
	if o.Betas != nil {
		clone.Betas = append([]types.SdkBeta{}, o.Betas...)
	}

	if o.MCPServers != nil {
		clone.MCPServers = make(map[string]types.MCPServerConfig, len(o.MCPServers))
		for k, v := range o.MCPServers {
			clone.MCPServers[k] = v
		}
	}
	if o.Channels != nil {
		clone.Channels = make(map[string]types.ChannelServerConfig, len(o.Channels))
		for k, v := range o.Channels {
			clone.Channels[k] = v
		}
	}
	if o.Env != nil {
		clone.Env = make(map[string]string, len(o.Env))
		for k, v := range o.Env {
			clone.Env[k] = v
		}
	}
	if o.ToolAliases != nil {
		clone.ToolAliases = make(map[string]string, len(o.ToolAliases))
		for k, v := range o.ToolAliases {
			clone.ToolAliases[k] = v
		}
	}
	if o.ExtraArgs != nil {
		clone.ExtraArgs = make(map[string]*string, len(o.ExtraArgs))
		for k, v := range o.ExtraArgs {
			clone.ExtraArgs[k] = v
		}
	}
	if o.Hooks != nil {
		clone.Hooks = make(map[types.HookEvent][]types.HookMatcher, len(o.Hooks))
		for k, v := range o.Hooks {
			clone.Hooks[k] = append([]types.HookMatcher{}, v...)
		}
	}
	if o.Agents != nil {
		clone.Agents = make(map[string]types.AgentDefinition, len(o.Agents))
		for k, v := range o.Agents {
			clone.Agents[k] = v
		}
	}
	if o.OutputFormat != nil {
		clone.OutputFormat = make(map[string]any, len(o.OutputFormat))
		for k, v := range o.OutputFormat {
			clone.OutputFormat[k] = v
		}
	}
	if o.ToolConfig != nil {
		tc := *o.ToolConfig
		if o.ToolConfig.AskUserQuestion != nil {
			aq := *o.ToolConfig.AskUserQuestion
			tc.AskUserQuestion = &aq
		}
		clone.ToolConfig = &tc
	}

	return &clone
}

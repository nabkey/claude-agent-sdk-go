package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/internal/protocol"
	"github.com/nabkey/claude-agent-sdk-go/internal/transport"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// defaultInitializeTimeout bounds the initialize handshake. MCP servers can
// take a while to start, so this is generous.
const defaultInitializeTimeout = 60 * time.Second

// session couples a connected transport with the control-protocol handler.
// Query and Client differ only in how long they keep one alive.
type session struct {
	transport Transport
	query     *protocol.Query
}

// buildTransportOptions projects the public options onto the subprocess
// transport's configuration.
func buildTransportOptions(o *AgentOptions) *transport.SubprocessOptions {
	return &transport.SubprocessOptions{
		SystemPrompt:                    o.SystemPrompt,
		AppendSystemPrompt:              o.AppendSystemPrompt,
		Tools:                           o.Tools,
		AllowedTools:                    o.AllowedTools,
		DisallowedTools:                 o.DisallowedTools,
		MaxTurns:                        o.MaxTurns,
		MaxBudgetUSD:                    o.MaxBudgetUSD,
		TaskBudget:                      o.TaskBudget,
		Model:                           o.Model,
		FallbackModel:                   o.FallbackModel,
		Agent:                           o.Agent,
		PermissionMode:                  o.PermissionMode,
		PermissionPromptToolName:        o.PermissionPromptToolName,
		AllowDangerouslySkipPermissions: o.AllowDangerouslySkipPermissions,
		ContinueConversation:            o.ContinueConversation,
		Resume:                          o.Resume,
		ResumeSessionAt:                 o.ResumeSessionAt,
		SessionID:                       o.SessionID,
		Settings:                        o.Settings,
		ManagedSettings:                 o.ManagedSettings,
		Sandbox:                         o.Sandbox,
		AddDirs:                         o.AddDirs,
		MCPServers:                      o.MCPServers,
		StrictMCPConfig:                 o.StrictMCPConfig,
		Channels:                        o.Channels,
		IncludePartialMessages:          o.IncludePartialMessages,
		IncludeHookEvents:               o.IncludeHookEvents,
		ForkSession:                     o.ForkSession,
		Agents:                          o.Agents,
		SettingSources:                  o.SettingSources,
		Skills:                          o.Skills,
		Plugins:                         o.Plugins,
		ExtraArgs:                       o.ExtraArgs,
		MaxThinkingTokens:               o.MaxThinkingTokens,
		Thinking:                        o.Thinking,
		Effort:                          effortToString(o.Effort),
		OutputFormat:                    o.OutputFormat,
		Betas:                           o.Betas,
		EnableFileCheckpointing:         o.EnableFileCheckpointing,
		SessionStore:                    o.SessionStore != nil,
		MCPConfigPath:                   o.MCPConfigPath,
		CLIPath:                         o.CLIPath,
		Cwd:                             o.Cwd,
		Env:                             o.Env,
		MaxBufferSize:                   o.MaxBufferSize,
		Stderr:                          o.Stderr,
		Debug:                           o.Debug,
		DebugFile:                       o.DebugFile,
		User:                            o.User,
		Hooks:                           o.Hooks,
		PersistSession:                  o.PersistSession,
		ToolConfig:                      o.ToolConfig,
	}
}

// sdkMCPHandlers extracts the in-process MCP servers from the options.
func sdkMCPHandlers(o *AgentOptions) map[string]*protocol.MCPServerHandler {
	handlers := make(map[string]*protocol.MCPServerHandler)
	for name, config := range o.MCPServers {
		sdkConfig, ok := config.(*types.SDKMCPServer)
		if !ok {
			continue
		}
		if handler, ok := sdkConfig.Instance.(*protocol.MCPServerHandler); ok {
			handlers[name] = handler
		}
	}
	return handlers
}

// initializeTimeout honors CLAUDE_CODE_STREAM_CLOSE_TIMEOUT, never dropping
// below the default.
func initializeTimeout() time.Duration {
	raw := os.Getenv("CLAUDE_CODE_STREAM_CLOSE_TIMEOUT")
	if raw == "" {
		return defaultInitializeTimeout
	}
	ms, err := strconv.Atoi(raw)
	if err != nil {
		return defaultInitializeTimeout
	}
	if d := time.Duration(ms) * time.Millisecond; d > defaultInitializeTimeout {
		return d
	}
	return defaultInitializeTimeout
}

// newSession connects a transport and completes the initialize handshake.
//
// Both Query and Client route through here, so both run the CLI in streaming
// mode and both send configuration that only exists on the initialize request.
//
// When custom is non-nil it is used instead of spawning a subprocess; the
// caller is responsible for having configured it equivalently.
func newSession(ctx context.Context, prompt string, o *AgentOptions, custom Transport) (*session, error) {
	if o.CanUseTool != nil && o.PermissionPromptToolName != nil {
		return nil, fmt.Errorf(
			"CanUseTool cannot be used with PermissionPromptToolName; use one or the other")
	}

	opts := o.Clone()

	// Route permission prompts through the control protocol when a callback
	// is installed.
	if opts.CanUseTool != nil {
		warnIfCanUseToolShadowed(opts)
		stdio := "stdio"
		opts.PermissionPromptToolName = &stdio
	}

	var (
		trans Transport
		err   error
	)
	if custom != nil {
		trans = custom
	} else {
		trans, err = transport.NewSubprocessTransport(buildTransportOptions(opts))
		if err != nil {
			return nil, err
		}
	}

	if err := trans.Connect(ctx); err != nil {
		return nil, err
	}

	query := protocol.NewQuery(&protocol.QueryOptions{
		Transport:       trans,
		IsStreamingMode: true,
		CanUseTool: func(ctx context.Context, toolName string, input map[string]any,
			permCtx types.ToolPermissionContext) (types.PermissionResult, error) {
			if opts.CanUseTool == nil {
				return &types.PermissionResultAllow{}, nil
			}
			return opts.CanUseTool(ctx, toolName, input, permCtx)
		},
		Hooks:                  opts.Hooks,
		SDKMCPServers:          sdkMCPHandlers(opts),
		InitializeTimeout:      initializeTimeout(),
		AgentProgressSummaries: opts.AgentProgressSummaries,
		InitConfig:             buildInitConfig(opts),
		OnElicitation:          elicitationAdapter(opts.OnElicitation),
		OnUserDialog:           userDialogAdapter(opts.OnUserDialog),
		Mirror:                 mirrorFor(opts),
	})

	query.Start(ctx)

	if _, err := query.Initialize(ctx); err != nil {
		_ = query.Close()
		return nil, err
	}

	return &session{transport: trans, query: query}, nil
}

// buildInitConfig assembles the configuration carried on the initialize
// request rather than as CLI flags.
func buildInitConfig(o *AgentOptions) *protocol.InitConfig {
	cfg := &protocol.InitConfig{
		Agents:               agentsToWire(o.Agents),
		Title:                o.Title,
		ToolAliases:          o.ToolAliases,
		PlanModeInstructions: o.PlanModeInstructions,
		PromptSuggestions:    o.PromptSuggestions,
		ForwardSubagentText:  o.ForwardSubagentText,
		SupportedDialogKinds: o.SupportedDialogKinds,
	}

	// A skills allowlist is a filter; "all" and omitted are equivalent on the
	// wire, so only an explicit list is sent.
	if names, ok := o.Skills.([]string); ok {
		cfg.Skills = names
	}

	switch sp := o.SystemPrompt.(type) {
	case *string:
		cfg.SystemPrompt = []string{*sp}
	case []string:
		cfg.SystemPrompt = sp
	case *types.SystemPromptPreset:
		if sp.Append != nil {
			cfg.AppendSystemPrompt = sp.Append
		}
		if sp.ExcludeDynamicSections != nil {
			cfg.ExcludeDynamicSections = sp.ExcludeDynamicSections
		}
	}
	if o.AppendSystemPrompt != nil {
		cfg.AppendSystemPrompt = o.AppendSystemPrompt
	}

	if o.OutputFormat != nil {
		if t, _ := o.OutputFormat["type"].(string); t == "json_schema" {
			if schema, ok := o.OutputFormat["schema"].(map[string]any); ok {
				cfg.JSONSchema = schema
			}
		}
	}

	return cfg
}

// agentsToWire renders agent definitions in the wire format, omitting unset
// optional fields.
func agentsToWire(agents map[string]types.AgentDefinition) map[string]any {
	if len(agents) == 0 {
		return nil
	}
	out := make(map[string]any, len(agents))
	for name, agent := range agents {
		out[name] = agent.ToMap()
	}
	return out
}

// writeUserMessage sends a user turn over the transport.
func (s *session) writeUserMessage(ctx context.Context, msg types.UserInputMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.transport.Write(ctx, string(data)+"\n")
}

// close tears down the control protocol and the transport.
func (s *session) close() error {
	if s.query != nil {
		return s.query.Close()
	}
	return nil
}

// elicitationAdapter bridges the public callback type to the protocol layer's,
// returning nil when unset so the protocol can apply its decline default.
func elicitationAdapter(cb ElicitationCallback) protocol.ElicitationCallback {
	if cb == nil {
		return nil
	}
	return func(ctx context.Context, req types.ElicitationRequest) (types.ElicitationResult, error) {
		return cb(ctx, req)
	}
}

// userDialogAdapter bridges the public callback type to the protocol layer's.
//
// A nil result here matters: without a callback the SDK must not answer a
// dialog at all, since another attached client may be the declared renderer.
func userDialogAdapter(cb UserDialogCallback) protocol.UserDialogCallback {
	if cb == nil {
		return nil
	}
	return func(ctx context.Context, req types.UserDialogRequest) (types.UserDialogResult, error) {
		return cb(ctx, req)
	}
}

// mirrorFor builds the transcript mirror sink, or nil when no SessionStore is
// configured.
func mirrorFor(o *AgentOptions) protocol.MirrorSink {
	if o.SessionStore == nil {
		return nil
	}

	cwd := "."
	if o.Cwd != nil {
		cwd = *o.Cwd
	}
	return newMirrorSink(o.SessionStore, ProjectKeyForDirectory(cwd), o.SessionStoreFlush, nil)
}

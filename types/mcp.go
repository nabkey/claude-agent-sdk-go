package types

// MCPServerConfig is the interface for MCP server configurations.
// Implementations include StdioMCPServer, SSEMCPServer, HTTPMCPServer, and SDKMCPServer.
type MCPServerConfig interface {
	isMCPServerConfig()
	// ServerType returns the type identifier for this server config.
	ServerType() string
}

// StdioMCPServer represents an external MCP server that communicates via stdio.
type StdioMCPServer struct {
	Type    string            `json:"type,omitempty"` // "stdio" (optional for backwards compatibility)
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
}

func (s *StdioMCPServer) isMCPServerConfig() {}
func (s *StdioMCPServer) ServerType() string { return "stdio" }

// SSEMCPServer represents an MCP server that communicates via Server-Sent Events.
type SSEMCPServer struct {
	Type    string            `json:"type"` // "sse"
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (s *SSEMCPServer) isMCPServerConfig() {}
func (s *SSEMCPServer) ServerType() string { return "sse" }

// HTTPMCPServer represents an MCP server that communicates via HTTP.
type HTTPMCPServer struct {
	Type    string            `json:"type"` // "http"
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers,omitempty"`
}

func (s *HTTPMCPServer) isMCPServerConfig() {}
func (s *HTTPMCPServer) ServerType() string { return "http" }

// SDKMCPServer represents an in-process MCP server running in the SDK.
// This is populated by mcp.NewSDKServer.
type SDKMCPServer struct {
	Type     string `json:"type"` // "sdk"
	Name     string `json:"name"`
	Version  string `json:"version,omitempty"`
	Instance any    `json:"-"` // Internal server instance (not serialized)
}

func (s *SDKMCPServer) isMCPServerConfig() {}
func (s *SDKMCPServer) ServerType() string { return "sdk" }

// ChannelServerConfig is the interface for channel server configurations.
// Channel servers can push messages into a Claude Code session.
// Implementations mirror MCP server types: StdioChannelServer, SSEChannelServer, etc.
type ChannelServerConfig interface {
	isChannelServerConfig()
	// ChannelServerType returns the type identifier for this channel server config.
	ChannelServerType() string
}

// ChannelCapability defines capabilities a channel server can declare.
type ChannelCapability string

const (
	// ChannelCapabilityPermission allows the channel server to relay tool approval prompts.
	ChannelCapabilityPermission ChannelCapability = "permission"
)

// StdioChannelServer represents a channel server that communicates via stdio.
type StdioChannelServer struct {
	Type         string              `json:"type,omitempty"` // "stdio"
	Command      string              `json:"command"`
	Args         []string            `json:"args,omitempty"`
	Env          map[string]string   `json:"env,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *StdioChannelServer) isChannelServerConfig()    {}
func (s *StdioChannelServer) ChannelServerType() string { return "stdio" }

// SSEChannelServer represents a channel server that communicates via Server-Sent Events.
type SSEChannelServer struct {
	Type         string              `json:"type"` // "sse"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *SSEChannelServer) isChannelServerConfig()    {}
func (s *SSEChannelServer) ChannelServerType() string { return "sse" }

// HTTPChannelServer represents a channel server that communicates via HTTP.
type HTTPChannelServer struct {
	Type         string              `json:"type"` // "http"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *HTTPChannelServer) isChannelServerConfig()    {}
func (s *HTTPChannelServer) ChannelServerType() string { return "http" }

// WebSocketChannelServer represents a channel server that communicates via WebSocket.
type WebSocketChannelServer struct {
	Type         string              `json:"type"` // "ws"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *WebSocketChannelServer) isChannelServerConfig()    {}
func (s *WebSocketChannelServer) ChannelServerType() string { return "ws" }

// SandboxNetworkConfig defines network configuration for the sandbox.
type SandboxNetworkConfig struct {
	// AllowedDomains are domains sandboxed processes may reach.
	AllowedDomains []string `json:"allowedDomains,omitempty"`
	// DeniedDomains are always blocked, even if AllowedDomains matches them.
	DeniedDomains []string `json:"deniedDomains,omitempty"`
	// AllowManagedDomainsOnly honors only managed-settings AllowedDomains.
	AllowManagedDomainsOnly *bool `json:"allowManagedDomainsOnly,omitempty"`
	// AllowUnixSockets are socket paths reachable in the sandbox, e.g. an
	// SSH agent.
	AllowUnixSockets []string `json:"allowUnixSockets,omitempty"`
	// AllowAllUnixSockets permits every Unix socket. Less secure.
	AllowAllUnixSockets *bool `json:"allowAllUnixSockets,omitempty"`
	// AllowLocalBinding permits binding localhost ports. macOS only.
	AllowLocalBinding *bool `json:"allowLocalBinding,omitempty"`
	// AllowMachLookup lists XPC/Mach service names to allow, with an optional
	// trailing wildcard. macOS only.
	AllowMachLookup []string `json:"allowMachLookup,omitempty"`
	// HTTPProxyPort is the port of a caller-supplied HTTP proxy.
	HTTPProxyPort *int `json:"httpProxyPort,omitempty"`
	// SOCKSProxyPort is the port of a caller-supplied SOCKS5 proxy.
	SOCKSProxyPort *int `json:"socksProxyPort,omitempty"`
}

// SandboxIgnoreViolations defines violations to ignore in sandbox.
type SandboxIgnoreViolations struct {
	File    []string `json:"file,omitempty"`
	Network []string `json:"network,omitempty"`
}

// SandboxSettings configures how Claude Code sandboxes bash commands for
// filesystem and network isolation.
//
// Filesystem and network *restrictions* are configured through permission
// rules, not here: Read deny rules for read access, Edit rules for writes,
// WebFetch rules for network. These settings control sandbox behavior.
type SandboxSettings struct {
	// Enabled turns on bash sandboxing. macOS and Linux only.
	Enabled *bool `json:"enabled,omitempty"`
	// AutoAllowBashIfSandboxed auto-approves bash commands that run
	// sandboxed. Defaults to true.
	AutoAllowBashIfSandboxed *bool `json:"autoAllowBashIfSandboxed,omitempty"`
	// ExcludedCommands run outside the sandbox, e.g. ["git", "docker"].
	ExcludedCommands []string `json:"excludedCommands,omitempty"`
	// AllowUnsandboxedCommands lets commands opt out via
	// dangerouslyDisableSandbox. When false every command must run sandboxed
	// or appear in ExcludedCommands. Defaults to true.
	AllowUnsandboxedCommands *bool `json:"allowUnsandboxedCommands,omitempty"`
	// Network configures sandbox networking.
	Network *SandboxNetworkConfig `json:"network,omitempty"`
	// IgnoreViolations suppresses reporting for specific paths and hosts.
	IgnoreViolations *SandboxIgnoreViolations `json:"ignoreViolations,omitempty"`
	// EnableWeakerNestedSandbox enables a weaker sandbox for unprivileged
	// Docker environments. Linux only, and reduces security.
	EnableWeakerNestedSandbox *bool `json:"enableWeakerNestedSandbox,omitempty"`
	// FailIfUnavailable fails the query when sandbox dependencies are
	// missing rather than silently running unsandboxed. Defaults to true when
	// Enabled is set through AgentOptions.
	FailIfUnavailable *bool `json:"failIfUnavailable,omitempty"`
}

// PluginConfig defines a plugin configuration.
type PluginConfig struct {
	Type string `json:"type"` // "local"
	Path string `json:"path"`
	// SkipMCPDiscovery loads the plugin without registering the MCP servers
	// it declares.
	SkipMCPDiscovery bool `json:"-"`
}

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

func (s *StdioChannelServer) isChannelServerConfig()        {}
func (s *StdioChannelServer) ChannelServerType() string     { return "stdio" }

// SSEChannelServer represents a channel server that communicates via Server-Sent Events.
type SSEChannelServer struct {
	Type         string              `json:"type"` // "sse"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *SSEChannelServer) isChannelServerConfig()        {}
func (s *SSEChannelServer) ChannelServerType() string     { return "sse" }

// HTTPChannelServer represents a channel server that communicates via HTTP.
type HTTPChannelServer struct {
	Type         string              `json:"type"` // "http"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *HTTPChannelServer) isChannelServerConfig()        {}
func (s *HTTPChannelServer) ChannelServerType() string     { return "http" }

// WebSocketChannelServer represents a channel server that communicates via WebSocket.
type WebSocketChannelServer struct {
	Type         string              `json:"type"` // "ws"
	URL          string              `json:"url"`
	Headers      map[string]string   `json:"headers,omitempty"`
	Capabilities []ChannelCapability `json:"capabilities,omitempty"`
}

func (s *WebSocketChannelServer) isChannelServerConfig()        {}
func (s *WebSocketChannelServer) ChannelServerType() string     { return "ws" }

// SandboxNetworkConfig defines network configuration for sandbox.
type SandboxNetworkConfig struct {
	AllowUnixSockets    []string `json:"allowUnixSockets,omitempty"`
	AllowAllUnixSockets *bool    `json:"allowAllUnixSockets,omitempty"`
	AllowLocalBinding   *bool    `json:"allowLocalBinding,omitempty"`
	HTTPProxyPort       *int     `json:"httpProxyPort,omitempty"`
	SOCKSProxyPort      *int     `json:"socksProxyPort,omitempty"`
}

// SandboxIgnoreViolations defines violations to ignore in sandbox.
type SandboxIgnoreViolations struct {
	File    []string `json:"file,omitempty"`
	Network []string `json:"network,omitempty"`
}

// SandboxSettings defines sandbox configuration for bash command isolation.
type SandboxSettings struct {
	Enabled                   *bool                    `json:"enabled,omitempty"`
	AutoAllowBashIfSandboxed  *bool                    `json:"autoAllowBashIfSandboxed,omitempty"`
	ExcludedCommands          []string                 `json:"excludedCommands,omitempty"`
	AllowUnsandboxedCommands  *bool                    `json:"allowUnsandboxedCommands,omitempty"`
	Network                   *SandboxNetworkConfig    `json:"network,omitempty"`
	IgnoreViolations          *SandboxIgnoreViolations `json:"ignoreViolations,omitempty"`
	EnableWeakerNestedSandbox *bool                    `json:"enableWeakerNestedSandbox,omitempty"`
}

// PluginConfig defines a plugin configuration.
type PluginConfig struct {
	Type string `json:"type"` // "local"
	Path string `json:"path"`
}

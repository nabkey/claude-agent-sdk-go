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

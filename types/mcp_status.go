package types

// McpStatusResponse contains the status of all MCP servers.
type McpStatusResponse struct {
	Servers []McpServerStatus `json:"servers"`
}

// McpServerStatus contains the status of a single MCP server.
type McpServerStatus struct {
	Name       string                    `json:"name"`
	Status     McpServerConnectionStatus `json:"status"`
	ServerInfo *McpServerInfo            `json:"serverInfo,omitempty"`
	Error      *string                   `json:"error,omitempty"`
	Config     map[string]any            `json:"config,omitempty"`
	Scope      *string                   `json:"scope,omitempty"`
	Tools      []McpToolInfo             `json:"tools,omitempty"`
}

// McpServerInfo contains server information from the MCP initialize handshake.
type McpServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// McpToolInfo describes a tool provided by an MCP server.
type McpToolInfo struct {
	Name        string              `json:"name"`
	Description *string             `json:"description,omitempty"`
	Annotations *McpToolAnnotations `json:"annotations,omitempty"`
}

// McpToolAnnotations contains metadata annotations for an MCP tool.
type McpToolAnnotations struct {
	ReadOnly    *bool `json:"readOnly,omitempty"`
	Destructive *bool `json:"destructive,omitempty"`
	OpenWorld   *bool `json:"openWorld,omitempty"`
}

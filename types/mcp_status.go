package types

// McpStatusResponse contains the status of all MCP servers.
type McpStatusResponse struct {
	Servers []McpServerStatus `json:"servers"`
}

// McpServerStatus contains the status of a single MCP server.
type McpServerStatus struct {
	Name   string                    `json:"name"`
	Status McpServerConnectionStatus `json:"status"`
	Error  *string                   `json:"error,omitempty"`
	Config map[string]any            `json:"config,omitempty"`
	Tools  []McpToolInfo             `json:"tools,omitempty"`
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

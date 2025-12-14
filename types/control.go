package types

// SDK Control Protocol types for bidirectional communication with the CLI.

// ControlRequest represents an outgoing control request to the CLI.
type ControlRequest struct {
	Type      string         `json:"type"` // "control_request"
	RequestID string         `json:"request_id"`
	Request   map[string]any `json:"request"`
}

// ControlResponse represents an incoming control response from the CLI.
type ControlResponse struct {
	Type     string                 `json:"type"` // "control_response"
	Response ControlResponsePayload `json:"response"`
}

// ControlResponsePayload is the payload of a control response.
type ControlResponsePayload struct {
	Subtype   string         `json:"subtype"` // "success" or "error"
	RequestID string         `json:"request_id"`
	Response  map[string]any `json:"response,omitempty"`
	Error     string         `json:"error,omitempty"`
}

// SDKControlInterruptRequest is a request to interrupt the current operation.
type SDKControlInterruptRequest struct {
	Subtype string `json:"subtype"` // "interrupt"
}

// SDKControlPermissionRequest is a request for tool permission.
type SDKControlPermissionRequest struct {
	Subtype               string               `json:"subtype"` // "can_use_tool"
	ToolName              string               `json:"tool_name"`
	Input                 map[string]any       `json:"input"`
	PermissionSuggestions []PermissionUpdate   `json:"permission_suggestions,omitempty"`
	BlockedPath           *string              `json:"blocked_path,omitempty"`
}

// SDKControlInitializeRequest is a request to initialize the control protocol.
type SDKControlInitializeRequest struct {
	Subtype string         `json:"subtype"` // "initialize"
	Hooks   map[string]any `json:"hooks,omitempty"`
}

// SDKControlSetPermissionModeRequest is a request to change the permission mode.
type SDKControlSetPermissionModeRequest struct {
	Subtype string         `json:"subtype"` // "set_permission_mode"
	Mode    PermissionMode `json:"mode"`
}

// SDKControlSetModelRequest is a request to change the AI model.
type SDKControlSetModelRequest struct {
	Subtype string  `json:"subtype"` // "set_model"
	Model   *string `json:"model,omitempty"`
}

// SDKHookCallbackRequest is a request for a hook callback.
type SDKHookCallbackRequest struct {
	Subtype    string  `json:"subtype"` // "hook_callback"
	CallbackID string  `json:"callback_id"`
	Input      any     `json:"input"`
	ToolUseID  *string `json:"tool_use_id,omitempty"`
}

// SDKControlMCPMessageRequest is a request to send a message to an MCP server.
type SDKControlMCPMessageRequest struct {
	Subtype    string         `json:"subtype"` // "mcp_message"
	ServerName string         `json:"server_name"`
	Message    map[string]any `json:"message"`
}

// UserInputMessage is a user message sent to the CLI in streaming mode.
type UserInputMessage struct {
	Type            string         `json:"type"` // "user"
	Message         UserInputInner `json:"message"`
	ParentToolUseID *string        `json:"parent_tool_use_id,omitempty"`
	SessionID       string         `json:"session_id,omitempty"`
}

// UserInputInner is the inner content of a user input message.
type UserInputInner struct {
	Role    string `json:"role"` // "user"
	Content string `json:"content"`
}

// MCPJSONRPCRequest represents a JSON-RPC request to an MCP server.
type MCPJSONRPCRequest struct {
	JSONRPC string         `json:"jsonrpc"` // "2.0"
	ID      any            `json:"id,omitempty"`
	Method  string         `json:"method"`
	Params  map[string]any `json:"params,omitempty"`
}

// MCPJSONRPCResponse represents a JSON-RPC response from an MCP server.
type MCPJSONRPCResponse struct {
	JSONRPC string         `json:"jsonrpc"` // "2.0"
	ID      any            `json:"id,omitempty"`
	Result  map[string]any `json:"result,omitempty"`
	Error   *MCPJSONRPCError `json:"error,omitempty"`
}

// MCPJSONRPCError represents a JSON-RPC error.
type MCPJSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

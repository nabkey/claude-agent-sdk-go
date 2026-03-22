package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/mcp"
)

// makeBrainTool creates an MCP tool that proxies to a brain HTTP endpoint.
func makeBrainTool(brainURL, name, description string, schema map[string]any, timeout time.Duration) mcp.Tool {
	return mcp.NewTool(name, description, schema, func(ctx context.Context, args map[string]any) (map[string]any, error) {
		return callBrain(ctx, brainURL, name, args, timeout)
	})
}

// callBrain makes an HTTP POST to the brain server and returns the result.
func callBrain(ctx context.Context, brainURL, toolName string, args map[string]any, timeout time.Duration) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	body, _ := json.Marshal(args)
	req, err := http.NewRequestWithContext(ctx, "POST", brainURL+"/tools/"+toolName, bytes.NewReader(body))
	if err != nil {
		return mcp.ErrorResult("failed to create request: " + err.Error()), nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return mcp.ErrorResult("brain unreachable: " + err.Error()), nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.ErrorResult("failed to read response: " + err.Error()), nil
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return mcp.ErrorResult("invalid response from brain: " + string(respBody)), nil
	}

	if errMsg, ok := result["error"].(string); ok {
		return mcp.ErrorResult(errMsg), nil
	}

	formatted, _ := json.MarshalIndent(result, "", "  ")
	return mcp.TextResult(string(formatted)), nil
}

// makeSendMessageTool creates an MCP tool that sends an iMessage from the host.
func makeSendMessageTool() mcp.Tool {
	return mcp.NewTool("send_message", "Send an iMessage to a recipient proactively", map[string]any{
		"type": "object",
		"properties": map[string]any{
			"recipient": map[string]any{"type": "string", "description": "Phone number or Apple ID of the recipient"},
			"message":   map[string]any{"type": "string", "description": "Message text to send"},
		},
		"required": []string{"recipient", "message"},
	}, func(ctx context.Context, args map[string]any) (map[string]any, error) {
		recipient, _ := args["recipient"].(string)
		message, _ := args["message"].(string)
		if recipient == "" || message == "" {
			return mcp.ErrorResult("recipient and message are required"), nil
		}
		if err := sendIMessage(recipient, message); err != nil {
			return mcp.ErrorResult("failed to send iMessage: " + err.Error()), nil
		}
		return mcp.TextResult(fmt.Sprintf("Message sent to %s", recipient)), nil
	})
}

// buildBrainTools creates all the MCP proxy tools for the brain server.
func buildBrainTools(brainURL string) []mcp.Tool {
	std := 30 * time.Second
	long := 120 * time.Second

	return []mcp.Tool{
		// Outbound messaging (runs on host, not in container)
		makeSendMessageTool(),
		// File operations
		makeBrainTool(brainURL, "read_file", "Read a file from the agent workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "File path relative to /agent/"},
			},
			"required": []string{"path"},
		}, std),

		makeBrainTool(brainURL, "write_file", "Write a file to the agent workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path":    map[string]any{"type": "string", "description": "File path relative to /agent/"},
				"content": map[string]any{"type": "string", "description": "File content"},
			},
			"required": []string{"path", "content"},
		}, std),

		makeBrainTool(brainURL, "list_dir", "List directory contents in the agent workspace", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"path": map[string]any{"type": "string", "description": "Directory path relative to /agent/"},
			},
			"required": []string{"path"},
		}, std),

		// Skills
		makeBrainTool(brainURL, "list_skills", "List all installed skills", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, std),

		makeBrainTool(brainURL, "read_skill", "Read a skill's SKILL.md and any reference files", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill directory name"},
			},
			"required": []string{"name"},
		}, std),

		makeBrainTool(brainURL, "write_skill", "Create or update a skill (writes SKILL.md with YAML frontmatter)", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string", "description": "Skill directory name (lowercase, hyphenated)"},
				"content": map[string]any{"type": "string", "description": "Full SKILL.md content including ---frontmatter---"},
			},
			"required": []string{"name", "content"},
		}, std),

		makeBrainTool(brainURL, "delete_skill", "Delete a skill", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name": map[string]any{"type": "string", "description": "Skill directory name to delete"},
			},
			"required": []string{"name"},
		}, std),

		// Prompt
		makeBrainTool(brainURL, "get_prompt", "Read the current system prompt", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, std),

		makeBrainTool(brainURL, "set_prompt", "Update the system prompt", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string", "description": "New system prompt content"},
			},
			"required": []string{"content"},
		}, std),

		// Memory
		makeBrainTool(brainURL, "read_memory", "Read MEMORY.md (long term and short term)", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, std),

		makeBrainTool(brainURL, "update_memory", "Update a section of MEMORY.md", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"section": map[string]any{"type": "string", "enum": []string{"long_term", "short_term"}, "description": "Which section to update"},
				"content": map[string]any{"type": "string", "description": "New content for the section"},
			},
			"required": []string{"section", "content"},
		}, std),

		// Conversation
		makeBrainTool(brainURL, "save_conversation", "Save a conversation exchange to today's log", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"sender":  map[string]any{"type": "string", "description": "Sender identifier"},
				"message": map[string]any{"type": "string", "description": "The incoming message"},
				"reply":   map[string]any{"type": "string", "description": "The reply that was sent"},
			},
			"required": []string{"sender", "message", "reply"},
		}, std),

		makeBrainTool(brainURL, "get_conversation_summary", "Get the conversation summary for a date", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"date": map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format (default: today)"},
			},
		}, std),

		makeBrainTool(brainURL, "update_summary", "Get un-summarized entries or write a new summary", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"date":    map[string]any{"type": "string", "description": "Date in YYYY-MM-DD format (default: today)"},
				"summary": map[string]any{"type": "string", "description": "If provided, writes this as the new summary"},
			},
		}, std),

		// Execution
		makeBrainTool(brainURL, "run_bash", "Execute a shell command inside the container", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command":         map[string]any{"type": "string", "description": "Shell command to execute"},
				"timeout_seconds": map[string]any{"type": "integer", "description": "Timeout in seconds (default 30, max 120)"},
			},
			"required": []string{"command"},
		}, long),

		// Self-modification
		makeBrainTool(brainURL, "read_source", "Read a brain server source file", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file": map[string]any{"type": "string", "description": "Source file name (e.g. 'tools.go', 'main.go')"},
			},
			"required": []string{"file"},
		}, std),

		makeBrainTool(brainURL, "write_source", "Write a brain server source file", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"file":    map[string]any{"type": "string", "description": "Source file name (e.g. 'tools.go')"},
				"content": map[string]any{"type": "string", "description": "New file content"},
			},
			"required": []string{"file", "content"},
		}, std),

		makeBrainTool(brainURL, "rebuild_self", "Recompile and restart the brain server. Rolls back on failure.", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, long),

		makeBrainTool(brainURL, "read_dockerfile", "Read the Dockerfile for the brain container", map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		}, std),

		makeBrainTool(brainURL, "write_dockerfile", "Update the Dockerfile for the brain container", map[string]any{
			"type": "object",
			"properties": map[string]any{
				"content": map[string]any{"type": "string", "description": "New Dockerfile content"},
			},
			"required": []string{"content"},
		}, std),
	}
}

// brainToolNames returns the MCP tool names for AllowedTools.
func brainToolNames() []string {
	tools := buildBrainTools("") // URL doesn't matter, just need names
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = fmt.Sprintf("mcp__talon__%s", t.Name)
	}
	return names
}

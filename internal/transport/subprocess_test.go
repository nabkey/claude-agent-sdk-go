package transport

import (
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestBuildCommand_ThinkingConfig(t *testing.T) {
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			Thinking: &types.ThinkingConfigAdaptive{Type: "adaptive"},
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--thinking") {
		t.Error("expected --thinking flag in command")
	}
	if !strings.Contains(cmdStr, "adaptive") {
		t.Error("expected 'adaptive' in thinking config")
	}
}

func TestBuildCommand_Effort(t *testing.T) {
	effort := "high"
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			Effort: &effort,
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--effort high") {
		t.Errorf("expected '--effort high' in command, got: %s", cmdStr)
	}
}

func TestBuildCommand_FileCheckpointing(t *testing.T) {
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			EnableFileCheckpointing: true,
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--enable-file-checkpointing") {
		t.Error("expected --enable-file-checkpointing flag in command")
	}
}

func TestBuildCommand_MCPConfigPath(t *testing.T) {
	path := "/path/to/mcp-config.json"
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			MCPConfigPath: &path,
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--mcp-config /path/to/mcp-config.json") {
		t.Errorf("expected '--mcp-config /path/to/mcp-config.json' in command, got: %s", cmdStr)
	}
}

func TestBuildCommand_MCPConfigPath_NotUsedWhenMCPServersSet(t *testing.T) {
	path := "/path/to/mcp-config.json"
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			MCPConfigPath: &path,
			MCPServers: map[string]types.MCPServerConfig{
				"test": &types.StdioMCPServer{Command: "echo"},
			},
		},
	}

	cmd := transport.buildCommand()

	// Count occurrences of --mcp-config
	count := 0
	for i, arg := range cmd {
		if arg == "--mcp-config" {
			count++
			// When MCPServers is set, the --mcp-config should be JSON, not the file path
			if i+1 < len(cmd) && cmd[i+1] == path {
				t.Error("MCPConfigPath should not be used when MCPServers is set")
			}
		}
	}
}

func TestBuildCommand_SdkBetas(t *testing.T) {
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			Betas: []types.SdkBeta{types.SdkBetaContext1M},
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--betas context-1m-2025-08-07") {
		t.Errorf("expected '--betas context-1m-2025-08-07' in command, got: %s", cmdStr)
	}
}

func TestBuildCommand_SystemPromptPreset(t *testing.T) {
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			SystemPrompt: &types.SystemPromptPreset{
				Type:   "preset",
				Preset: "claude_code",
			},
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--system-prompt") {
		t.Error("expected --system-prompt flag in command")
	}
	if !strings.Contains(cmdStr, "claude_code") {
		t.Error("expected 'claude_code' in system prompt preset")
	}
}

func TestBuildCommand_ToolsPreset(t *testing.T) {
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			Tools: &types.ToolsPreset{
				Type:   "preset",
				Preset: "claude_code",
			},
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--tools") {
		t.Error("expected --tools flag in command")
	}
	if !strings.Contains(cmdStr, "claude_code") {
		t.Error("expected 'claude_code' in tools preset")
	}
}

func TestBuildCommand_AgentDefinitionNewFields(t *testing.T) {
	memory := "project_memory"
	transport := &SubprocessTransport{
		cliPath:   "/usr/bin/claude",
		isStreaming: true,
		options: &SubprocessOptions{
			Agents: map[string]types.AgentDefinition{
				"test-agent": {
					Description: "Test agent",
					Prompt:      "Do stuff",
					Skills:      []string{"skill1", "skill2"},
					Memory:      &memory,
					MCPServers:  []any{"server1"},
				},
			},
		},
	}

	cmd := transport.buildCommand()
	cmdStr := strings.Join(cmd, " ")

	if !strings.Contains(cmdStr, "--agents") {
		t.Error("expected --agents flag in command")
	}
	if !strings.Contains(cmdStr, "skills") {
		t.Error("expected 'skills' in agents JSON")
	}
	if !strings.Contains(cmdStr, "memory") {
		t.Error("expected 'memory' in agents JSON")
	}
	if !strings.Contains(cmdStr, "mcpServers") {
		t.Error("expected 'mcpServers' in agents JSON")
	}
}

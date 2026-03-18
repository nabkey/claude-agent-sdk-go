package claude

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// AgentInfo describes a discovered agent available in the current environment.
type AgentInfo struct {
	// Name is the agent identifier.
	Name string
	// Model is the model the agent uses (e.g., "opus", "haiku", "inherit").
	Model string
	// Source indicates where the agent is defined ("built-in" or "plugin").
	Source string
}

// SupportedAgents discovers available agents by invoking the Claude CLI.
// Pass options to specify a custom CLI path or setting sources; nil uses defaults.
func SupportedAgents(ctx context.Context, options *AgentOptions) ([]AgentInfo, error) {
	cliPath := "claude"
	if options != nil && options.CLIPath != nil {
		cliPath = *options.CLIPath
	}

	args := []string{"agents"}
	if options != nil && options.SettingSources != nil {
		sources := make([]string, len(options.SettingSources))
		for i, s := range options.SettingSources {
			sources[i] = string(s)
		}
		args = append(args, "--setting-sources", strings.Join(sources, ","))
	}
	if options != nil && options.Cwd != nil {
		args = append(args, "--cwd", *options.Cwd)
	}

	cmd := exec.CommandContext(ctx, cliPath, args...)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list agents: %w", err)
	}

	return parseAgentsOutput(string(output)), nil
}

// parseAgentsOutput parses the human-readable output of `claude agents`.
func parseAgentsOutput(output string) []AgentInfo {
	var agents []AgentInfo
	var currentSource string

	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(lower, "plugin agents") {
			currentSource = "plugin"
			continue
		}
		if strings.HasPrefix(lower, "built-in agents") {
			currentSource = "built-in"
			continue
		}
		// Skip header lines like "N active agents"
		if strings.HasSuffix(lower, "active agents") || strings.HasSuffix(lower, "active agent") {
			continue
		}

		// Parse "name · model" lines (unicode middle dot)
		parts := strings.SplitN(trimmed, "·", 2)
		if len(parts) != 2 {
			// Try ASCII bullet as fallback
			parts = strings.SplitN(trimmed, " - ", 2)
		}
		if len(parts) == 2 {
			agents = append(agents, AgentInfo{
				Name:   strings.TrimSpace(parts[0]),
				Model:  strings.TrimSpace(parts[1]),
				Source: currentSource,
			})
		}
	}

	return agents
}

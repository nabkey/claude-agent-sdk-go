package main

import (
	"context"
	"fmt"
	"strings"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

const helpText = `Send any message to put it to Claude in the sandbox.

/new      start a fresh conversation
/stop     interrupt the current turn
/status   what this chat is connected to
/mode     permission mode: default | acceptEdits | plan | bypassPermissions
/model    list models, or /model <name> to switch
/ctx      context window usage
/usage    cost and rate limits
/mcp      MCP server status
/help     this message

Tool calls that need approval arrive as buttons.`

// handleCommand dispatches a slash command.
func (b *Bot) handleCommand(ctx context.Context, chatID int64, text string) {
	fields := strings.Fields(text)
	name := strings.ToLower(strings.TrimPrefix(fields[0], "/"))
	// In groups Telegram appends the bot's username, as in "/stop@mybot".
	if at := strings.Index(name, "@"); at >= 0 {
		name = name[:at]
	}
	args := fields[1:]

	reply := func(format string, a ...any) {
		b.api.SendMessage(ctx, chatID, fmt.Sprintf(format, a...), nil)
	}

	switch name {
	case "start", "help":
		reply("%s", helpText)

	case "new":
		if b.resetSession(chatID) {
			reply("Started a fresh conversation.")
		} else {
			reply("No conversation was running; the next message starts one.")
		}

	case "stop":
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			if !s.isRunning() {
				reply("Nothing is running.")
				return
			}
			if err := s.client.Interrupt(ctx); err != nil {
				reply("⚠️ interrupt failed: %v", err)
				return
			}
			reply("Interrupted.")
		})

	case "status":
		b.mu.Lock()
		s, connected := b.chats[chatID]
		b.mu.Unlock()

		var sb strings.Builder
		fmt.Fprintf(&sb, "Sandbox: %s/%s\n", b.cfg.SandboxNetwork, b.cfg.SandboxAddress)
		fmt.Fprintf(&sb, "Chat ID: %d\n", chatID)
		if !connected {
			sb.WriteString("Session: not connected")
			reply("%s", sb.String())
			return
		}
		state := "idle"
		if s.isRunning() {
			state = "running a turn"
		}
		fmt.Fprintf(&sb, "Session: %s", state)
		if acct := s.client.AccountInfo(); acct != nil && acct.Email != "" {
			fmt.Fprintf(&sb, "\nAccount: %s", acct.Email)
		}
		reply("%s", sb.String())

	case "mode":
		if len(args) == 0 {
			reply("Usage: /mode default | acceptEdits | plan | bypassPermissions")
			return
		}
		mode, ok := parsePermissionMode(args[0])
		if !ok {
			reply("Unknown mode %q. Use default, acceptEdits, plan, or bypassPermissions.", args[0])
			return
		}
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			if err := s.client.SetPermissionMode(ctx, mode); err != nil {
				reply("⚠️ could not set mode: %v", err)
				return
			}
			msg := fmt.Sprintf("Permission mode is now %s.", mode)
			if mode == types.PermissionModeBypassPermissions {
				msg += "\n\n⚠️ Tool calls will no longer ask for approval."
			}
			reply("%s", msg)
		})

	case "model":
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			if len(args) == 0 {
				models, err := s.client.SupportedModels(ctx)
				if err != nil {
					reply("⚠️ could not list models: %v", err)
					return
				}
				var sb strings.Builder
				sb.WriteString("Available models:\n")
				for _, m := range models {
					fmt.Fprintf(&sb, "• %s", m.Model)
					if m.DisplayName != "" {
						fmt.Fprintf(&sb, " — %s", m.DisplayName)
					}
					sb.WriteString("\n")
				}
				sb.WriteString("\nSwitch with /model <name>")
				reply("%s", sb.String())
				return
			}
			if err := s.client.SetModel(ctx, claude.String(args[0])); err != nil {
				reply("⚠️ could not switch model: %v", err)
				return
			}
			reply("Model is now %s.", args[0])
		})

	case "ctx":
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			usage, err := s.client.GetContextUsage(ctx)
			if err != nil {
				reply("⚠️ could not read context usage: %v", err)
				return
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Context: %d / %d tokens (%.1f%%)",
				usage.TotalTokens, usage.MaxTokens, usage.Percentage)
			if usage.Model != "" {
				fmt.Fprintf(&sb, "\nModel: %s", usage.Model)
			}
			if usage.IsAutoCompactEnabled {
				fmt.Fprintf(&sb, "\nAutocompact at %d tokens", usage.AutoCompactThreshold)
			}
			reply("%s", sb.String())
		})

	case "usage":
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			usage, err := s.client.GetSessionUsage(ctx)
			if err != nil {
				reply("⚠️ could not read usage: %v", err)
				return
			}
			var sb strings.Builder
			fmt.Fprintf(&sb, "Cost so far: $%.4f", usage.TotalCostUSD)
			if usage.SubscriptionType != "" {
				fmt.Fprintf(&sb, "\nPlan: %s", usage.SubscriptionType)
			}
			for _, w := range usage.RateLimits {
				fmt.Fprintf(&sb, "\n%s: %.0f%% used", w.Type, w.Utilization)
			}
			reply("%s", sb.String())
		})

	case "mcp":
		b.withSession(ctx, chatID, reply, func(s *chatSession) {
			status, err := s.client.GetMCPStatus(ctx)
			if err != nil {
				reply("⚠️ could not read MCP status: %v", err)
				return
			}
			if len(status.Servers) == 0 {
				reply("No MCP servers configured.")
				return
			}
			var sb strings.Builder
			sb.WriteString("MCP servers:\n")
			for _, srv := range status.Servers {
				fmt.Fprintf(&sb, "• %s — %s (%d tools)\n", srv.Name, srv.Status, len(srv.Tools))
				if srv.Error != nil && *srv.Error != "" {
					fmt.Fprintf(&sb, "    %s\n", *srv.Error)
				}
			}
			reply("%s", sb.String())
		})

	default:
		reply("Unknown command %q. Send /help for the list.", name)
	}
}

// withSession runs fn against the chat's session, reporting a connect failure
// rather than silently doing nothing.
func (b *Bot) withSession(ctx context.Context, chatID int64,
	reply func(string, ...any), fn func(*chatSession)) {

	s, err := b.session(ctx, chatID)
	if err != nil {
		reply("⚠️ could not reach the sandbox: %v", err)
		return
	}
	fn(s)
}

func parsePermissionMode(s string) (types.PermissionMode, bool) {
	switch strings.ToLower(s) {
	case "default":
		return types.PermissionModeDefault, true
	case "acceptedits", "accept-edits":
		return types.PermissionModeAcceptEdits, true
	case "plan":
		return types.PermissionModePlan, true
	case "bypasspermissions", "bypass":
		return types.PermissionModeBypassPermissions, true
	}
	return "", false
}

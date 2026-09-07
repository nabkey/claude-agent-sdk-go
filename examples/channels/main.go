// Example: Channels
//
// This example demonstrates the channels feature, which allows MCP servers
// to push messages into a Claude Code session. A channel server acts as a
// notification source — it can inject context, alerts, or updates into the
// conversation while Claude is working.
//
// This example sets up a stdio channel server (a simple Go program that
// writes MCP notifications to stdout) alongside a normal Claude session,
// then shows how to receive and display channel messages.
//
// Usage:
//
//	go run ./examples/channels
//
// Note: The --channels flag is a research preview feature in Claude Code 2.1.80+.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("=== Claude Code Channels Example ===")
	fmt.Println()
	fmt.Println("This example demonstrates how to configure channel servers")
	fmt.Println("that push messages into a Claude Code session.")
	fmt.Println()

	// --- Example 1: Configure channels via AgentOptions ---
	fmt.Println("--- Example 1: Channel Configuration ---")
	showChannelConfiguration()

	// --- Example 2: Run a session with channels and handle messages ---
	fmt.Println()
	fmt.Println("--- Example 2: Channel Message Handling ---")
	runChannelSession(ctx)
}

// showChannelConfiguration demonstrates all the ways to configure channel servers.
func showChannelConfiguration() {
	// Stdio channel server — runs a local process that pushes messages via stdout
	opts := claude.DefaultAgentOptions()
	opts.WithChannel("notifications", &types.StdioChannelServer{
		Command: "node",
		Args:    []string{"channel-server.js"},
		Env: map[string]string{
			"NOTIFY_INTERVAL": "30",
		},
	})

	fmt.Printf("  Stdio channel configured: %d channel(s)\n", len(opts.Channels))

	// SSE channel server — connects to a remote push endpoint
	opts.WithChannel("alerts", &types.SSEChannelServer{
		Type: "sse",
		URL:  "https://channel.example.com/sse",
		Headers: map[string]string{
			"Authorization": "Bearer ${CHANNEL_TOKEN}",
		},
	})

	fmt.Printf("  SSE channel added: %d channel(s)\n", len(opts.Channels))

	// HTTP channel server — polls or receives webhooks
	opts.WithChannel("webhooks", &types.HTTPChannelServer{
		Type: "http",
		URL:  "https://hooks.example.com/mcp",
	})

	fmt.Printf("  HTTP channel added: %d channel(s)\n", len(opts.Channels))

	// WebSocket channel server — real-time bidirectional
	opts.WithChannel("realtime", &types.WebSocketChannelServer{
		Type: "ws",
		URL:  "wss://realtime.example.com/ws",
	})

	fmt.Printf("  WebSocket channel added: %d channel(s)\n", len(opts.Channels))

	// Channel with permission relay capability
	opts.WithChannel("phone-approval", &types.SSEChannelServer{
		Type:         "sse",
		URL:          "https://approval.example.com/sse",
		Capabilities: []types.ChannelCapability{types.ChannelCapabilityPermission},
	})

	fmt.Printf("  Permission relay channel added: %d channel(s)\n", len(opts.Channels))

	// Verify Clone preserves channels
	cloned := opts.Clone()
	fmt.Printf("  Cloned options preserves channels: %v\n", len(cloned.Channels) == len(opts.Channels))
}

// runChannelSession demonstrates running a Claude session with channels
// and handling the different message types including channel messages.
func runChannelSession(ctx context.Context) {
	// For this demo, we use a simple channel server that echoes a notification.
	// In real usage, this would be a persistent service pushing updates.
	options := &claude.AgentOptions{
		// Configure a channel server — in production this would be a real
		// notification service. Here we use a simple echo script.
		Channels: map[string]types.ChannelServerConfig{
			"demo-alerts": &types.StdioChannelServer{
				Command: "echo",
				Args:    []string{"channel server configured"},
			},
		},
		MaxTurns: claude.Int(3),
	}

	// Create and connect the client
	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer func() { _ = client.Close() }()

	fmt.Println("  Connecting to Claude with channel server...")

	if err := client.Connect(ctx, ""); err != nil {
		fmt.Printf("  Note: --channels requires Claude Code 2.1.80+\n")
		fmt.Printf("  Connection error: %v\n", err)
		fmt.Println()
		fmt.Println("  Falling back to demo mode (showing message handling pattern)...")
		showMessageHandlingPattern()
		return
	}

	fmt.Println("  Connected! Sending query...")

	if err := client.SendQuery(ctx, "Say hello briefly."); err != nil {
		log.Fatal(err)
	}

	// Set a timeout for the demo
	timeout := time.After(30 * time.Second)

	// Process messages, including any channel messages that arrive
	for msg := range client.ReceiveResponse() {
		select {
		case <-timeout:
			fmt.Println("  [Timeout reached]")
			return
		default:
		}

		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("  Claude: %s\n", text.Text)
				}
			}

		case *types.ChannelMessage:
			// This is the key part — channel messages arrive here
			fmt.Printf("  [Channel %q]: %s\n", m.ServerName, m.Content)
			if m.Data != nil {
				fmt.Printf("    Data: %v\n", m.Data)
			}

		case *types.ResultMessage:
			cost := float64(0)
			if m.TotalCostUSD != nil {
				cost = *m.TotalCostUSD
			}
			fmt.Printf("  --- Done (Cost: $%.4f, Turns: %d) ---\n", cost, m.NumTurns)
		}
	}
}

// showMessageHandlingPattern demonstrates the message handling pattern
// for channel messages without requiring a live CLI connection.
func showMessageHandlingPattern() {
	fmt.Println()
	fmt.Println("  Message handling pattern for channels:")
	fmt.Println()
	fmt.Println(`    for msg := range client.ReceiveResponse() {`)
	fmt.Println(`        switch m := msg.(type) {`)
	fmt.Println(`        case *types.AssistantMessage:`)
	fmt.Println(`            // Handle Claude's response`)
	fmt.Println(`        case *types.ChannelMessage:`)
	fmt.Println(`            // Handle push notification from channel server`)
	fmt.Println("            log.Println(\"Channel:\", m.ServerName, m.Content)")
	fmt.Println(`        case *types.ResultMessage:`)
	fmt.Println(`            // Handle completion`)
	fmt.Println(`        }`)
	fmt.Println(`    }`)
	fmt.Println()
	fmt.Println("  Channel server types available:")
	fmt.Println("    - StdioChannelServer  (local process, stdio communication)")
	fmt.Println("    - SSEChannelServer    (remote, Server-Sent Events)")
	fmt.Println("    - HTTPChannelServer   (remote, HTTP)")
	fmt.Println("    - WebSocketChannelServer (remote, WebSocket)")
	fmt.Println()
	fmt.Println("  Permission relay:")
	fmt.Println("    Channels with ChannelCapabilityPermission can forward")
	fmt.Println("    tool approval prompts to external devices (e.g., your phone).")
}

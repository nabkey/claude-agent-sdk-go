// Command imessage_agent is a self-modifying iMessage agent whose Claude runs
// inside a sandbox.
//
// The agent can rewrite its own skills, system prompt, memory, and source. It
// reaches the outside world through iMessage, and reaches Claude through the
// sandbox transport in examples/sandbox — so the process holding your Messages
// database is not the process that can execute the agent's tool calls.
//
// Architecture:
//
//	┌────────────────────────┐        ┌──────────────────────────┐
//	│ this program (macOS)   │        │ sandbox                  │
//	│  • polls iMessages     │──────▶ │  • sandbox-host          │
//	│  • proxies brain tools │ socket │  • claude CLI            │
//	│  • drives claude.Client│        │  • brain HTTP server     │
//	└────────────────────────┘        └──────────────────────────┘
//
// Unlike an earlier version of this example, the program no longer builds or
// supervises a container. The sandbox and the brain are started by whoever
// owns them; this process only connects.
//
// Prerequisites:
//   - macOS with Messages configured, and Full Disk Access for the terminal
//     (System Settings → Privacy & Security → Full Disk Access)
//   - a sandbox host reachable at -sandbox-address, with the claude CLI
//     installed alongside it (see examples/sandbox)
//   - the brain server reachable at -brain-url (see brain/)
//
// Usage:
//
//	go run . -sandbox-address 127.0.0.1:8378 -brain-url http://localhost:8377
//
// Then send an iMessage containing the trigger word to start a conversation.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/examples/sandbox"
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"

	_ "modernc.org/sqlite"
)

const (
	pollInterval = 3 * time.Second

	// brainServerName is the in-process MCP server exposing the brain tools.
	// It appears in tool names as mcp__brain__<tool>.
	brainServerName = "brain"
)

// agentConfig is what the iMessage loop needs to reach its two dependencies.
type agentConfig struct {
	brainURL       string
	sandboxNetwork string
	sandboxAddress string
	sandboxToken   string
	trigger        string
}

func main() {
	var (
		chatMode = flag.Bool("chat", false, "start the terminal chat TUI instead of iMessage mode")
		brainURL = flag.String("brain-url", "http://localhost:8377", "brain server base URL")
		network  = flag.String("sandbox-network", "tcp", `sandbox host network: "tcp" or "unix"`)
		address  = flag.String("sandbox-address", "127.0.0.1:8378", "sandbox host address or socket path")
		trigger  = flag.String("trigger", "claude", "only act on iMessages containing this word")
	)
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	if err := waitForBrain(ctx, *brainURL, 30*time.Second); err != nil {
		log.Fatal(err)
	}
	go watchBrain(ctx, *brainURL)

	cfg := agentConfig{
		brainURL:       *brainURL,
		sandboxNetwork: *network,
		sandboxAddress: *address,
		sandboxToken:   os.Getenv("SANDBOX_TOKEN"),
		trigger:        strings.ToLower(*trigger),
	}

	if *chatMode {
		runChatMode(cfg)
		return
	}
	runIMessageMode(ctx, cfg)
}

// runChatMode starts the interactive terminal chat TUI.
func runChatMode(cfg agentConfig) {
	m := newChatModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal("TUI error: ", err)
	}
}

// runIMessageMode runs the iMessage polling loop with a persistent Claude client.
func runIMessageMode(ctx context.Context, cfg agentConfig) {
	fmt.Println("=== iMessage Agent ===")
	fmt.Println()

	brainURL := cfg.brainURL

	// Open iMessage database
	db, err := openMessagesDB()
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Get latest message ID (only process new messages)
	lastROWID, err := getLatestROWID(ctx, db)
	if err != nil {
		log.Fatal("Failed to get latest message ID: ", err)
	}
	fmt.Printf("Starting from message ID %d\n", lastROWID)

	// Create persistent Claude client
	client, err := createPersistentClient(ctx, cfg)
	if err != nil {
		log.Fatal("Failed to create Claude client: ", err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal("Failed to connect Claude client: ", err)
	}

	fmt.Printf("Brain at %s\n", brainURL)
	fmt.Printf("Sandbox at %s/%s\n", cfg.sandboxNetwork, cfg.sandboxAddress)
	fmt.Printf("Claude connected (persistent session).\n")
	fmt.Printf("Watching for iMessages containing %q...\n\n", cfg.trigger)

	// Message processing loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	messageCount := 0

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nShutting down...")
			return
		case <-ticker.C:
			messages, err := getNewMessages(ctx, db, lastROWID)
			if err != nil {
				log.Printf("Error polling messages: %v", err)
				continue
			}

			for _, msg := range messages {
				if msg.ROWID > lastROWID {
					lastROWID = msg.ROWID
				}
				if msg.IsFromMe {
					continue
				}
				if !strings.Contains(strings.ToLower(msg.Text), cfg.trigger) {
					continue
				}

				fmt.Printf("[%s] From %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.SenderID, msg.Text)

				reply, err := processIMessage(ctx, client, brainURL, msg)
				if err != nil {
					log.Printf("Processing error: %v", err)
					continue
				}

				fmt.Printf("  -> Reply: %s\n", reply)

				if err := sendIMessage(msg.SenderID, reply); err != nil {
					log.Printf("Failed to send reply: %v", err)
				} else {
					fmt.Printf("  -> Sent to %s\n\n", msg.SenderID)
				}

				// Save conversation to brain
				saveConversation(ctx, brainURL, msg.SenderID, msg.Text, reply)

				messageCount++
				if messageCount%5 == 0 {
					go triggerSummaryUpdate(ctx, brainURL)
				}
			}
		}
	}
}

// createPersistentClient builds a Claude client whose CLI runs in the sandbox,
// with the brain tools served in-process from here.
func createPersistentClient(ctx context.Context, cfg agentConfig) (*claude.Client, error) {
	systemPrompt := fetchBrainState(ctx, cfg.brainURL)
	tools := buildBrainTools(cfg.brainURL)
	brainServer := mcp.NewSDKServer(brainServerName, "1.0.0", tools...)

	// The host has to be told the in-process server exists; the SDK would
	// normally do that with --mcp-config, which a custom transport never
	// emits. Without this the brain tools are silently unavailable.
	start := sandbox.DefaultStartRequest()
	start.SDKMCPServers = []string{brainServerName}

	transport := sandbox.New(sandbox.Config{
		Network: cfg.sandboxNetwork,
		Address: cfg.sandboxAddress,
		Token:   cfg.sandboxToken,
		Start:   start,
		Stderr:  func(line string) { log.Printf("cli: %s", line) },
	})

	// Tools, PermissionMode and MaxTurns are deliberately absent: they become
	// CLI flags, so they belong to the sandbox host's policy rather than here.
	// Run the host with -allowed-tools and -max-turns to bound this session.
	options := &claude.AgentOptions{
		MCPServers: map[string]types.MCPServerConfig{
			brainServerName: brainServer,
		},
		AppendSystemPrompt: &systemPrompt,
		Warn:               func(w string) { log.Printf("sdk warning: %s", w) },
	}

	return claude.NewClientWithTransport(ctx, options, transport)
}

// processIMessage sends an iMessage through the persistent client.
func processIMessage(ctx context.Context, client *claude.Client, brainURL string, msg iMessageEntry) (string, error) {
	prompt := fmt.Sprintf("iMessage from %s: %s", msg.SenderID, msg.Text)

	if err := client.SendQuery(ctx, prompt); err != nil {
		return "", fmt.Errorf("send failed: %w", err)
	}

	var reply strings.Builder
	for result := range client.ReceiveResponse() {
		switch m := result.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					reply.WriteString(text.Text)
				}
			}
		case *types.ResultMessage:
			// Turn complete
		}
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		return "Sorry, I couldn't process that right now.", nil
	}
	return text, nil
}

// fetchBrainState assembles the system prompt from the brain's current state.
func fetchBrainState(ctx context.Context, brainURL string) string {
	var parts []string

	// Fetch system prompt
	if data := brainGET(ctx, brainURL, "get_prompt"); data != nil {
		if content, ok := data["content"].(string); ok {
			parts = append(parts, content)
		}
	}

	// Fetch memory
	if data := brainGET(ctx, brainURL, "read_memory"); data != nil {
		if content, ok := data["content"].(string); ok && strings.TrimSpace(content) != "" {
			parts = append(parts, "\n## Your Memory\n"+content)
		}
	}

	// Fetch skills
	if data := brainGET(ctx, brainURL, "list_skills"); data != nil {
		if skills, ok := data["skills"].([]any); ok && len(skills) > 0 {
			parts = append(parts, "\n## Your Skills")
			for _, s := range skills {
				skill, ok := s.(map[string]any)
				if !ok {
					continue
				}
				name, _ := skill["name"].(string)
				// Fetch full skill content
				skillData := brainPOST(ctx, brainURL, "read_skill", map[string]any{"name": name})
				if skillData != nil {
					if content, ok := skillData["content"].(string); ok {
						parts = append(parts, fmt.Sprintf("\n### %s\n%s", name, content))
					}
				}
			}
		}
	}

	// Fetch today's conversation summary
	if data := brainGET(ctx, brainURL, "get_conversation_summary"); data != nil {
		if summary, ok := data["summary"].(string); ok && summary != "" {
			parts = append(parts, "\n## Today's Conversation Context\n"+summary)
		}
	}

	// Tool instructions
	parts = append(parts, `
## Available Tools
You have tools to manage your own capabilities. Use them autonomously when appropriate:
- read_file/write_file/list_dir: manage files in your workspace
- list_skills/read_skill/write_skill/delete_skill: manage your skill library
- get_prompt/set_prompt: modify your own system prompt
- read_memory/update_memory: manage long-term and short-term memory
- save_conversation/get_conversation_summary/update_summary: conversation history
- run_bash: execute shell commands inside your container
- read_source/write_source/rebuild_self: modify and recompile your own brain server
- read_dockerfile/write_dockerfile: modify your container image

You decide when to just answer vs when to use tools. Create skills when you learn something reusable. Save important facts to long-term memory. Only rebuild your source when you need a genuinely new capability.`)

	return strings.Join(parts, "\n")
}

// brainGET calls a brain tool with an empty body.
func brainGET(ctx context.Context, brainURL, tool string) map[string]any {
	return brainPOST(ctx, brainURL, tool, map[string]any{})
}

// brainPOST calls a brain tool with args.
func brainPOST(ctx context.Context, brainURL, tool string, args map[string]any) map[string]any {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	body, _ := json.Marshal(args)
	req, err := http.NewRequestWithContext(ctx, "POST", brainURL+"/tools/"+tool, strings.NewReader(string(body)))
	if err != nil {
		return nil
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var result map[string]any
	json.Unmarshal(respBody, &result)
	return result
}

// saveConversation logs an exchange to the brain.
func saveConversation(ctx context.Context, brainURL, sender, message, reply string) {
	brainPOST(ctx, brainURL, "save_conversation", map[string]any{
		"sender":  sender,
		"message": message,
		"reply":   reply,
	})
}

// triggerSummaryUpdate asks the brain for un-summarized entries.
func triggerSummaryUpdate(ctx context.Context, brainURL string) {
	data := brainPOST(ctx, brainURL, "update_summary", map[string]any{})
	if data == nil {
		return
	}
	count, _ := data["count"].(float64)
	if count == 0 {
		return
	}
	log.Printf("Summary: %d new entries to summarize", int(count))
}

// openMessagesDB opens the iMessage SQLite database.
func openMessagesDB() (*sql.DB, error) {
	dbPath := filepath.Join(os.Getenv("HOME"), "Library", "Messages", "chat.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("messages database not found at %s", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return nil, fmt.Errorf("failed to open messages database: %w", err)
	}

	if err := db.Ping(); err != nil {
		fmt.Println("Cannot access Messages database.")
		fmt.Println("Grant Full Disk Access to Terminal:")
		fmt.Println("  System Settings > Privacy & Security > Full Disk Access")
		return nil, err
	}

	return db, nil
}

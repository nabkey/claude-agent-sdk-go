// Talon Agent: A self-modifying iMessage AI agent.
//
// Talon lives inside a Podman container where it can modify its own skills,
// system prompt, memory, and even its own source code. It communicates with
// the outside world through iMessage.
//
// Architecture:
//   - Outer program (this): polls iMessages, manages Podman, proxies tools, runs Claude
//   - Brain server (inside container): HTTP server providing tools for self-modification
//
// Prerequisites:
//   - macOS with Messages app configured
//   - Full Disk Access for Terminal (System Settings > Privacy & Security)
//   - Podman Desktop running
//   - Claude Code CLI installed
//
// Usage:
//
//	cd examples/talon_agent
//	go run .
//
// Then send an iMessage containing "talon" to trigger the agent.
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
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/mcp"
	"github.com/nabkey/claude-agent-sdk-go/types"

	_ "modernc.org/sqlite"
)

const (
	triggerWord  = "talon"
	pollInterval = 3 * time.Second
)

func main() {
	chatMode := flag.Bool("chat", false, "Start in terminal chat mode instead of iMessage mode")
	resetMode := flag.Bool("reset", false, "Reset brain to repo template (deletes ~/.talon/src/, re-bootstraps on next run)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.SetFlags(log.LstdFlags | log.Lshortfile)

	// Find the brain directory (relative to this source file)
	brainDir := findBrainDir()

	// Check Podman
	if err := checkPodman(ctx); err != nil {
		log.Fatal(err)
	}

	// Create container manager
	container := NewContainerManager(brainDir)

	// Handle --reset
	if *resetMode {
		if err := container.Reset(ctx); err != nil {
			log.Fatal("Reset failed: ", err)
		}
		fmt.Println("Brain reset. Run again to re-bootstrap from repo template.")
		return
	}

	// Start the brain container
	if err := container.EnsureRunning(ctx); err != nil {
		log.Fatal("Failed to start brain: ", err)
	}

	// Start health monitoring
	go container.WatchAndRecover(ctx)

	if *chatMode {
		runChatMode(container.BrainURL())
		return
	}

	runIMessageMode(ctx, container)
}

// runChatMode starts the interactive terminal chat TUI.
func runChatMode(brainURL string) {
	m := newChatModel(brainURL)
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		log.Fatal("TUI error: ", err)
	}
}

// runIMessageMode runs the iMessage polling loop with a persistent Claude client.
func runIMessageMode(ctx context.Context, container *ContainerManager) {
	fmt.Println("=== Talon Agent (iMessage mode) ===")
	fmt.Println()

	brainURL := container.BrainURL()

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
	client, err := createPersistentClient(ctx, brainURL)
	if err != nil {
		log.Fatal("Failed to create Claude client: ", err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal("Failed to connect Claude client: ", err)
	}

	fmt.Printf("Brain running at %s\n", brainURL)
	fmt.Printf("Claude connected (persistent session).\n")
	fmt.Printf("Watching for iMessages containing %q...\n\n", triggerWord)

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
				if !strings.Contains(strings.ToLower(msg.Text), triggerWord) {
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

// createPersistentClient builds a Claude client with brain tools and system prompt.
func createPersistentClient(ctx context.Context, brainURL string) (*claude.Client, error) {
	systemPrompt := fetchBrainState(ctx, brainURL)
	tools := buildBrainTools(brainURL)
	talonServer := mcp.NewSDKServer("talon", "1.0.0", tools...)

	bypassPerms := types.PermissionModeBypassPermissions
	options := &claude.AgentOptions{
		MCPServers: map[string]types.MCPServerConfig{
			"talon": talonServer,
		},
		Tools:              brainToolNames(),
		PermissionMode:     &bypassPerms,
		MaxTurns:           claude.Int(25),
		AppendSystemPrompt: &systemPrompt,
	}

	return claude.NewClient(ctx, options)
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

// findBrainDir locates the brain/ directory relative to this source file.
func findBrainDir() string {
	// Try relative to working directory first
	if info, err := os.Stat("brain"); err == nil && info.IsDir() {
		abs, _ := filepath.Abs("brain")
		return abs
	}
	// Try relative to source file
	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	return filepath.Join(dir, "brain")
}

// checkPodman verifies Podman is available and running.
func checkPodman(ctx context.Context) error {
	out, err := podmanVersion(ctx)
	if err != nil {
		return fmt.Errorf("podman not found. Install Podman Desktop: https://podman-desktop.io\n%w", err)
	}
	fmt.Printf("Podman: %s\n", strings.TrimSpace(out))
	return nil
}

func podmanVersion(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "podman", "version", "--format", "{{.Server.Version}}")
	out, err := cmd.Output()
	return string(out), err
}

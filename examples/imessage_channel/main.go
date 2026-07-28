// Example: iMessage Channel
//
// This example monitors your iMessages for messages containing the word "claude"
// and routes them into a Claude Code session via the channels feature. Claude
// processes the message and can send a reply back via iMessage.
//
// Architecture:
//
//	┌─────────────┐     poll chat.db      ┌───────────────┐
//	│   iMessage   │ ──────────────────▶  │  This program  │
//	│  (chat.db)   │                      │                │
//	└─────────────┘                      │  ┌───────────┐ │
//	       ▲                              │  │ Claude SDK│ │
//	       │  osascript reply             │  │  Client   │ │
//	       └──────────────────────────── │  └───────────┘ │
//	                                      └───────────────┘
//
// Prerequisites:
//   - macOS with Messages app configured
//   - Full Disk Access granted to Terminal (or the compiled binary)
//     System Settings → Privacy & Security → Full Disk Access
//   - Claude Code CLI installed (2.1.80+ for channels support)
//
// Usage:
//
//	go run ./examples/imessage_channel
//
// Send yourself (or have someone send you) an iMessage containing "claude"
// and watch Claude process it!
package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"

	_ "modernc.org/sqlite"
)

const (
	triggerWord  = "claude"
	pollInterval = 3 * time.Second
)

// iMessageEntry represents a single iMessage from the database.
type iMessageEntry struct {
	ROWID     int64
	Text      string
	IsFromMe  bool
	SenderID  string // phone number or email
	ChatID    string // chat identifier
	Timestamp time.Time
}

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	fmt.Println("=== iMessage Channel for Claude Code ===")
	fmt.Println()

	// Verify we can access the Messages database
	dbPath := filepath.Join(os.Getenv("HOME"), "Library", "Messages", "chat.db")
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		log.Fatal("Messages database not found at ", dbPath)
	}

	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		log.Fatal("Failed to open Messages database: ", err)
	}
	defer db.Close()

	// Quick access check
	if err := db.Ping(); err != nil {
		fmt.Println("Cannot access Messages database.")
		fmt.Println("Grant Full Disk Access to Terminal (or this binary):")
		fmt.Println("  System Settings → Privacy & Security → Full Disk Access")
		log.Fatal(err)
	}

	fmt.Println("Messages database connected.")

	// Get the latest message ROWID so we only process new messages
	var lastROWID int64
	err = db.QueryRowContext(ctx, "SELECT MAX(ROWID) FROM message").Scan(&lastROWID)
	if err != nil {
		log.Fatal("Failed to query latest message ID: ", err)
	}
	fmt.Printf("Starting from message ID %d (only new messages will be processed)\n", lastROWID)

	// Set up Claude SDK client
	options := &claude.AgentOptions{
		MaxTurns: claude.Int(3),
	}
	options.WithAppendSystemPrompt(`You are an iMessage auto-responder. Your output will be sent DIRECTLY as an iMessage reply — the recipient will see exactly what you write, nothing more.

CRITICAL RULES:
- Output ONLY the reply text itself. Nothing else.
- Do NOT include quotes, markdown, formatting, bold, or asterisks.
- Do NOT say "Here's a reply" or "Want me to adjust" or any meta-commentary.
- Do NOT explain what you're doing or offer alternatives.
- Do NOT use prefixes like "Reply:" or "Message:".
- Just write the actual message as if YOU are texting the person back.
- Keep it casual and brief like a real text message.`)

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal("Failed to create Claude client: ", err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal("Failed to connect to Claude: ", err)
	}
	fmt.Println("Claude connected.")
	fmt.Printf("Watching for iMessages containing %q...\n\n", triggerWord)

	// Poll loop
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("\nShutting down.")
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

				// Skip our own messages
				if msg.IsFromMe {
					continue
				}

				// Check for trigger word
				if !strings.Contains(strings.ToLower(msg.Text), triggerWord) {
					continue
				}

				fmt.Printf("[%s] From %s: %s\n", msg.Timestamp.Format("15:04:05"), msg.SenderID, msg.Text)

				// Send to Claude for processing
				reply, err := processWithClaude(ctx, client, msg)
				if err != nil {
					log.Printf("Claude processing error: %v", err)
					continue
				}

				fmt.Printf("  → Claude reply: %s\n", reply)

				// Send the reply back via iMessage
				if err := sendIMessage(msg.SenderID, reply); err != nil {
					log.Printf("Failed to send reply: %v", err)
				} else {
					fmt.Printf("  → Reply sent to %s\n\n", msg.SenderID)
				}
			}
		}
	}
}

// getNewMessages queries the Messages database for messages newer than lastROWID.
func getNewMessages(ctx context.Context, db *sql.DB, lastROWID int64) ([]iMessageEntry, error) {
	query := `
		SELECT
			m.ROWID,
			COALESCE(m.text, '') as text,
			m.is_from_me,
			COALESCE(h.id, '') as sender_id,
			COALESCE(c.chat_identifier, '') as chat_id,
			m.date
		FROM message m
		LEFT JOIN chat_message_join cmj ON m.ROWID = cmj.message_id
		LEFT JOIN chat c ON cmj.chat_id = c.ROWID
		LEFT JOIN handle h ON m.handle_id = h.ROWID
		WHERE m.ROWID > ?
		ORDER BY m.ROWID ASC
		LIMIT 50
	`

	rows, err := db.QueryContext(ctx, query, lastROWID)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	defer rows.Close()

	var messages []iMessageEntry
	for rows.Next() {
		var msg iMessageEntry
		var isFromMe int
		var dateNano int64

		if err := rows.Scan(&msg.ROWID, &msg.Text, &isFromMe, &msg.SenderID, &msg.ChatID, &dateNano); err != nil {
			return nil, fmt.Errorf("scan failed: %w", err)
		}

		msg.IsFromMe = isFromMe == 1
		// macOS Messages uses Apple's epoch (2001-01-01) in nanoseconds
		msg.Timestamp = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(dateNano))

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// processWithClaude sends the iMessage to Claude and gets a reply.
func processWithClaude(ctx context.Context, client *claude.Client, msg iMessageEntry) (string, error) {
	prompt := fmt.Sprintf(
		"I received this iMessage from %s: %q\n\nPlease compose a brief reply.",
		msg.SenderID, msg.Text,
	)

	if err := client.SendQuery(ctx, prompt); err != nil {
		return "", fmt.Errorf("send query failed: %w", err)
	}

	var reply strings.Builder
	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					reply.WriteString(text.Text)
				}
			}
		case *types.ResultMessage:
			// Done
		}
	}

	result := strings.TrimSpace(reply.String())
	if result == "" {
		return "I received your message but couldn't generate a reply.", nil
	}
	return result, nil
}

// sendIMessage sends an iMessage reply using AppleScript.
func sendIMessage(recipient, message string) error {
	// Escape the message for AppleScript
	escaped := strings.ReplaceAll(message, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, `"`, `\"`)

	script := fmt.Sprintf(`
		tell application "Messages"
			set targetService to 1st account whose service type = iMessage
			set targetBuddy to participant %q of targetService
			send %q to targetBuddy
		end tell
	`, recipient, escaped)

	cmd := exec.Command("osascript", "-e", script)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("osascript failed: %w (output: %s)", err, string(output))
	}
	return nil
}

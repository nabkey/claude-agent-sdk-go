package main

import (
	"context"
	"database/sql"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// iMessageEntry represents a single iMessage from the database.
type iMessageEntry struct {
	ROWID     int64
	Text      string
	IsFromMe  bool
	SenderID  string
	ChatID    string
	Timestamp time.Time
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
		msg.Timestamp = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(dateNano))

		messages = append(messages, msg)
	}

	return messages, rows.Err()
}

// sendIMessage sends an iMessage reply using AppleScript.
func sendIMessage(recipient, message string) error {
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

// getLatestROWID returns the highest message ROWID in the database.
func getLatestROWID(ctx context.Context, db *sql.DB) (int64, error) {
	var rowID int64
	err := db.QueryRowContext(ctx, "SELECT COALESCE(MAX(ROWID), 0) FROM message").Scan(&rowID)
	return rowID, err
}

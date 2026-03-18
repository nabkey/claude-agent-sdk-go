package sessions

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

// RenameSession sets or updates the title for a session.
// If directory is nil, uses the current working directory.
func RenameSession(sessionID, title string, directory *string) error {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory: %w", err)
	}

	title = sanitizeUnicode(title)

	entry := map[string]any{
		"type":  "session_metadata",
		"title": title,
	}

	return appendToSession(absDir, sessionID, entry)
}

// TagSession sets or clears a tag on a session.
// If tag is nil, the tag is removed. If directory is nil, uses the current working directory.
func TagSession(sessionID string, tag *string, directory *string) error {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	absDir, err := filepath.Abs(dir)
	if err != nil {
		return fmt.Errorf("failed to resolve directory: %w", err)
	}

	entry := map[string]any{
		"type": "session_metadata",
	}

	if tag != nil {
		entry["tag"] = sanitizeUnicode(*tag)
	} else {
		entry["tag"] = nil
	}

	return appendToSession(absDir, sessionID, entry)
}

// appendToSession appends a JSON entry to a session JSONL file using atomic O_APPEND writes.
func appendToSession(absDir, sessionID string, entry map[string]any) error {
	projectDir := getProjectDir(absDir)
	filePath := filepath.Join(projectDir, sessionID+".jsonl")

	// Verify the session file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return fmt.Errorf("session %s not found", sessionID)
	}

	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("failed to marshal entry: %w", err)
	}

	// Open with O_APPEND for atomic writes
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	_, err = file.WriteString(string(data) + "\n")
	if err != nil {
		return fmt.Errorf("failed to write to session file: %w", err)
	}

	return nil
}

// sanitizeUnicode removes control characters from a string while preserving valid unicode.
func sanitizeUnicode(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

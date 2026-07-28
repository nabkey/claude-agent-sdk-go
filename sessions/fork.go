package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ForkSessionResult describes a newly forked session.
type ForkSessionResult struct {
	// SessionID is the new session's ID.
	SessionID string `json:"session_id"`
	// Path is the new transcript's location on disk.
	Path string `json:"path"`
	// Title is the forked session's title, derived from the source.
	Title string `json:"title,omitempty"`
}

// ForkSession branches a session into a new one, leaving the original
// untouched.
//
// The fork is a transcript rewrite: entries are copied with fresh UUIDs and
// parent links rewritten to match. No CLI process runs and no tokens are
// spent, so forking is free and instant.
//
// If directory is nil the current working directory is used.
func ForkSession(sessionID string, directory *string) (*ForkSessionResult, error) {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	projectDir := findProjectDir(canonicalizePath(dir))
	if projectDir == "" {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	sourcePath := filepath.Join(projectDir, sessionID+".jsonl")
	source, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("session %s not found: %w", sessionID, err)
	}
	defer func() { _ = source.Close() }()

	newSessionID := uuid.NewString()
	targetPath := filepath.Join(projectDir, newSessionID+".jsonl")

	lines, title, err := rewriteForkTranscript(source, newSessionID)
	if err != nil {
		return nil, err
	}

	// Write via a temporary file and rename, so a crash mid-write cannot
	// leave a half-formed transcript that later reads would trip over.
	tmp, err := os.CreateTemp(projectDir, ".fork-*.jsonl")
	if err != nil {
		return nil, fmt.Errorf("failed to create fork transcript: %w", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	writer := bufio.NewWriter(tmp)
	for _, line := range lines {
		if _, err := writer.WriteString(line + "\n"); err != nil {
			_ = tmp.Close()
			return nil, fmt.Errorf("failed to write fork transcript: %w", err)
		}
	}
	if err := writer.Flush(); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("failed to flush fork transcript: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("failed to close fork transcript: %w", err)
	}
	if err := os.Rename(tmpPath, targetPath); err != nil {
		return nil, fmt.Errorf("failed to finalize fork transcript: %w", err)
	}

	return &ForkSessionResult{SessionID: newSessionID, Path: targetPath, Title: title}, nil
}

// rewriteForkTranscript copies a transcript under a new session ID, assigning
// fresh entry UUIDs and rewriting parent links to match.
//
// Reusing the source UUIDs would make the two sessions indistinguishable to
// anything that keys on them, so every UUID is remapped. Links whose target
// was never seen are left alone rather than dropped: an entry may legitimately
// reference something outside this file.
func rewriteForkTranscript(r *os.File, newSessionID string) ([]string, string, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	remap := make(map[string]string)
	var lines []string
	var title string

	for scanner.Scan() {
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}

		var entry map[string]any
		if err := json.Unmarshal([]byte(raw), &entry); err != nil {
			continue // Skip unparseable lines rather than abandoning the fork.
		}

		if t, ok := entry["title"].(string); ok && t != "" {
			title = t
		}

		if oldUUID, ok := entry["uuid"].(string); ok && oldUUID != "" {
			newUUID := uuid.NewString()
			remap[oldUUID] = newUUID
			entry["uuid"] = newUUID
		}
		if parent, ok := entry["parentUuid"].(string); ok && parent != "" {
			if mapped, ok := remap[parent]; ok {
				entry["parentUuid"] = mapped
			}
		}

		for _, key := range []string{"sessionId", "session_id"} {
			if _, present := entry[key]; present {
				entry[key] = newSessionID
			}
		}

		encoded, err := json.Marshal(entry)
		if err != nil {
			continue
		}
		lines = append(lines, string(encoded))
	}

	if err := scanner.Err(); err != nil {
		return nil, "", fmt.Errorf("error reading source transcript: %w", err)
	}

	// Record the fork's origin so the new session is traceable.
	marker, err := json.Marshal(map[string]any{
		"type":      "session_metadata",
		"uuid":      uuid.NewString(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"forked":    true,
	})
	if err == nil {
		lines = append(lines, string(marker))
	}

	return lines, title, nil
}

// DeleteSession removes a session transcript and any subagent transcripts
// beneath it.
//
// If directory is nil the current working directory is used.
func DeleteSession(sessionID string, directory *string) error {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	projectDir := findProjectDir(canonicalizePath(dir))
	if projectDir == "" {
		return fmt.Errorf("session %s not found", sessionID)
	}

	path := filepath.Join(projectDir, sessionID+".jsonl")
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("session %s not found", sessionID)
	}
	if err := os.Remove(path); err != nil {
		return fmt.Errorf("failed to delete session: %w", err)
	}

	// Subagent transcripts are orphaned without the main one, so remove them
	// too. A missing directory is not an error.
	if err := os.RemoveAll(filepath.Join(projectDir, sessionID)); err != nil {
		return fmt.Errorf("failed to delete subagent transcripts: %w", err)
	}

	return nil
}

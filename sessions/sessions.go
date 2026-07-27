// Package sessions provides session listing and history functionality
// for Claude Code conversations.
package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ListSessions lists available sessions for a given directory.
// If directory is nil, uses the current working directory.
func ListSessions(directory *string) ([]types.SDKSessionInfo, error) {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	absDir := canonicalizePath(dir)

	projectDir := findProjectDir(absDir)
	if projectDir == "" {
		return nil, nil // No sessions directory for this project
	}

	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return nil, fmt.Errorf("failed to read sessions directory: %w", err)
	}

	var sessions []types.SDKSessionInfo

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}

		sessionID := strings.TrimSuffix(entry.Name(), ".jsonl")
		filePath := filepath.Join(projectDir, entry.Name())

		info, err := parseSessionFile(filePath, sessionID, absDir)
		if err != nil {
			continue // Skip unparseable files
		}

		sessions = append(sessions, *info)
	}

	// Sort by creation time (newest first)
	sort.Slice(sessions, func(i, j int) bool {
		if sessions[i].CreatedAt == nil || sessions[j].CreatedAt == nil {
			return false
		}
		return sessions[i].CreatedAt.After(*sessions[j].CreatedAt)
	})

	return sessions, nil
}

// GetSessionInfo returns metadata for a single session by ID.
// If directory is nil, uses the current working directory.
func GetSessionInfo(sessionID string, directory *string) (*types.SDKSessionInfo, error) {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	absDir := canonicalizePath(dir)

	projectDir := findProjectDir(absDir)
	if projectDir == "" {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}

	return parseSessionFile(filePath, sessionID, absDir)
}

// GetSessionMessages reads all messages from a session transcript.
// If directory is nil, uses the current working directory.
func GetSessionMessages(sessionID string, directory *string) ([]types.SessionMessage, error) {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	absDir := canonicalizePath(dir)

	projectDir := findProjectDir(absDir)
	if projectDir == "" {
		return nil, fmt.Errorf("session %s not found", sessionID)
	}
	filePath := filepath.Join(projectDir, sessionID+".jsonl")

	file, err := os.Open(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to open session file: %w", err)
	}
	defer file.Close()

	var messages []types.SessionMessage
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var msg types.SessionMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue // Skip unparseable lines
		}

		// Also store the full data
		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err == nil {
			msg.Data = data
		}

		messages = append(messages, msg)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading session file: %w", err)
	}

	return messages, nil
}

// parseSessionFile reads a JSONL session file and extracts session info.
func parseSessionFile(filePath, sessionID, cwd string) (*types.SDKSessionInfo, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info := &types.SDKSessionInfo{
		SessionID: sessionID,
		CWD:       cwd,
	}

	stat, err := file.Stat()
	if err == nil {
		modTime := stat.ModTime()
		info.CreatedAt = &modTime
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var firstPrompt, lastPrompt string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var data map[string]any
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			continue
		}

		// Extract prompts from user messages
		msgType, _ := data["type"].(string)
		if msgType == "user" {
			if msg, ok := data["message"].(map[string]any); ok {
				if content, ok := msg["content"].(string); ok {
					if firstPrompt == "" {
						firstPrompt = content
						// Use first message time as created at
						if ts, ok := data["timestamp"].(string); ok {
							if t, err := time.Parse(time.RFC3339, ts); err == nil {
								info.CreatedAt = &t
							}
						}
					}
					lastPrompt = content
				}
			}
		}

		// Extract git branch if present
		if msgType == "system" {
			if branch, ok := data["git_branch"].(string); ok {
				info.GitBranch = &branch
			}
		}

		// Extract tag if present
		if tag, ok := data["tag"].(string); ok {
			info.Tag = &tag
		}
	}

	info.FirstPrompt = firstPrompt
	info.LastPrompt = lastPrompt

	return info, nil
}

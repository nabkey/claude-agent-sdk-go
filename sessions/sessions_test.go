package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSessionMessages(t *testing.T) {
	// Create a temporary directory structure
	tmpDir, err := os.MkdirTemp("", "claude-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a mock project directory
	projectDir := getProjectDir(tmpDir)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a mock session file
	sessionID := "test-session-123"
	messages := []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "Hello"}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "text", "text": "Hi!"}}}},
		{"type": "result", "session_id": sessionID, "is_error": false},
	}

	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}

	for _, msg := range messages {
		data, _ := json.Marshal(msg)
		file.WriteString(string(data) + "\n")
	}
	file.Close()

	// Test GetSessionMessages
	result, err := GetSessionMessages(sessionID, &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result))
	}

	if result[0].Type != "user" {
		t.Errorf("expected first message type 'user', got '%s'", result[0].Type)
	}
	if result[1].Type != "assistant" {
		t.Errorf("expected second message type 'assistant', got '%s'", result[1].Type)
	}
	if result[2].Type != "result" {
		t.Errorf("expected third message type 'result', got '%s'", result[2].Type)
	}
}

func TestListSessions(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claude-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := getProjectDir(tmpDir)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create mock session files
	for _, id := range []string{"session-1", "session-2"} {
		filePath := filepath.Join(projectDir, id+".jsonl")
		file, _ := os.Create(filePath)
		msg := map[string]any{
			"type":    "user",
			"message": map[string]any{"role": "user", "content": "Hello from " + id},
		}
		data, _ := json.Marshal(msg)
		file.WriteString(string(data) + "\n")
		file.Close()
	}

	sessions, err := ListSessions(&tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(sessions) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(sessions))
	}
}

func TestRenameSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claude-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := getProjectDir(tmpDir)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-rename"
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	file, _ := os.Create(filePath)
	file.WriteString(`{"type": "user", "message": {"role": "user", "content": "test"}}` + "\n")
	file.Close()

	err = RenameSession(sessionID, "New Title", &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the entry was appended
	data, _ := os.ReadFile(filePath)
	lines := 0
	for _, b := range data {
		if b == '\n' {
			lines++
		}
	}
	if lines != 2 {
		t.Errorf("expected 2 lines in session file, got %d", lines)
	}
}

func TestTagSession(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "claude-sessions-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tmpDir)

	projectDir := getProjectDir(tmpDir)
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-tag"
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	file, _ := os.Create(filePath)
	file.WriteString(`{"type": "user", "message": {"role": "user", "content": "test"}}` + "\n")
	file.Close()

	tag := "important"
	err = TagSession(sessionID, &tag, &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test clearing tag
	err = TagSession(sessionID, nil, &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error clearing tag: %v", err)
	}
}

func TestSanitizeUnicode(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello world", "hello world"},
		{"hello\x00world", "helloworld"},
		{"hello\nworld", "hello\nworld"},
		{"hello\tworld", "hello\tworld"},
	}

	for _, tt := range tests {
		result := sanitizeUnicode(tt.input)
		if result != tt.expected {
			t.Errorf("sanitizeUnicode(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestSanitizePath(t *testing.T) {
	result := sanitizePath("/Users/test/code/project")
	if result == "" {
		t.Error("expected non-empty sanitized path")
	}
	// Should not start with a separator
	if result[0] == filepath.Separator {
		t.Error("sanitized path should not start with separator")
	}
}

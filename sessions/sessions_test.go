package sessions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGetSessionMessages(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	projectDir := getProjectDir(canonicalizePath(tmpDir))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-session-123"
	// A realistic transcript: linked entries, plus a result row and a
	// sidechain entry that are not part of the conversation.
	entries := []map[string]any{
		{"type": "user", "uuid": "u1", "sessionId": sessionID,
			"message": map[string]any{"role": "user", "content": "Hello"}},
		{"type": "assistant", "uuid": "a1", "parentUuid": "u1", "sessionId": sessionID,
			"message": map[string]any{"role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": "Hi!"}}}},
		{"type": "user", "uuid": "sub1", "parentUuid": "a1", "isSidechain": true,
			"message": map[string]any{"role": "user", "content": "subagent traffic"}},
		{"type": "user", "uuid": "u2", "parentUuid": "a1", "sessionId": sessionID,
			"message": map[string]any{"role": "user", "content": "Thanks"}},
		{"type": "result", "uuid": "r1", "session_id": sessionID, "is_error": false},
	}

	writeTranscript(t, filepath.Join(projectDir, sessionID+".jsonl"), entries)

	result, err := GetSessionMessages(sessionID, &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Only the conversation chain: the sidechain and the result row are not
	// part of it.
	if len(result) != 3 {
		t.Fatalf("expected 3 conversation messages, got %d: %+v", len(result), result)
	}
	wantTypes := []string{"user", "assistant", "user"}
	for i, want := range wantTypes {
		if result[i].Type != want {
			t.Errorf("message %d type = %q, want %q", i, result[i].Type, want)
		}
	}
	if result[0].UUID != "u1" || result[2].UUID != "u2" {
		t.Errorf("unexpected chain order: %q, %q", result[0].UUID, result[2].UUID)
	}
	if result[0].SessionID != sessionID {
		t.Errorf("SessionID = %q", result[0].SessionID)
	}
	if result[0].Data == nil {
		t.Error("Data should carry the raw transcript line")
	}
}

// A transcript whose entries carry no UUIDs has no links to walk. Returning
// nothing would be the wrong failure mode, so file order is used instead.
func TestGetSessionMessagesWithoutUUIDs(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	projectDir := getProjectDir(canonicalizePath(tmpDir))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "legacy-session"
	writeTranscript(t, filepath.Join(projectDir, sessionID+".jsonl"), []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "Hello"}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{}}},
		{"type": "result", "is_error": false},
	})

	result, err := GetSessionMessages(sessionID, &tmpDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 conversation messages, got %d", len(result))
	}
}

// writeTranscript writes JSONL entries to a transcript file.
func writeTranscript(t *testing.T, path string, entries []map[string]any) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }()

	for _, entry := range entries {
		data, err := json.Marshal(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.WriteString(string(data) + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListSessions(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	projectDir := getProjectDir(canonicalizePath(tmpDir))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
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
		_, _ = file.WriteString(string(data) + "\n")
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
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	projectDir := getProjectDir(canonicalizePath(tmpDir))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-rename"
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	file, _ := os.Create(filePath)
	_, _ = file.WriteString(`{"type": "user", "message": {"role": "user", "content": "test"}}` + "\n")
	file.Close()

	if err := RenameSession(sessionID, "New Title", &tmpDir); err != nil {
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
	tmpDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	projectDir := getProjectDir(canonicalizePath(tmpDir))
	if err := os.MkdirAll(projectDir, 0o755); err != nil {
		t.Fatal(err)
	}

	sessionID := "test-tag"
	filePath := filepath.Join(projectDir, sessionID+".jsonl")
	file, _ := os.Create(filePath)
	_, _ = file.WriteString(`{"type": "user", "message": {"role": "user", "content": "test"}}` + "\n")
	file.Close()

	tag := "important"
	if err := TagSession(sessionID, &tag, &tmpDir); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Test clearing tag
	if err := TagSession(sessionID, nil, &tmpDir); err != nil {
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

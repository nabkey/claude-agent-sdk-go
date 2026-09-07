package sessions

import (
	"os"
	"path/filepath"
	"testing"
)

// newSubagentFixture lays out a session with one subagent transcript and
// returns the working directory to read it back from.
func newSubagentFixture(t *testing.T, sessionID, agentID string, withSidecar bool) string {
	t.Helper()

	workDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	subagentsDir := filepath.Join(getProjectDir(canonicalizePath(workDir)), sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Every entry in a subagent transcript is sidechain by construction.
	writeTranscript(t, filepath.Join(subagentsDir, "agent-"+agentID+".jsonl"), []map[string]any{
		{"type": "user", "uuid": "s1", "sessionId": sessionID, "isSidechain": true,
			"message": map[string]any{"role": "user", "content": "do the thing"}},
		{"type": "assistant", "uuid": "s2", "parentUuid": "s1", "sessionId": sessionID,
			"isSidechain": true, "message": map[string]any{"role": "assistant",
				"content": []any{map[string]any{"type": "text", "text": "done"}}}},
	})

	if withSidecar {
		sidecar := filepath.Join(subagentsDir, "agent-"+agentID+".meta.json")
		content := `{"toolUseId":"tool-abc","parentAgentId":"agent-parent"}`
		if err := os.WriteFile(sidecar, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return workDir
}

// Subagent transcripts are entirely sidechain, so filtering sidechain entries
// the way the main-session read does would return nothing at all.
func TestGetSubagentMessagesReturnsTheChain(t *testing.T) {
	sessionID, agentID := "session-1", "7"
	workDir := newSubagentFixture(t, sessionID, agentID, false)

	messages, err := GetSubagentMessages(sessionID, agentID, &workDir)
	if err != nil {
		t.Fatalf("GetSubagentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(messages), messages)
	}
	if messages[0].UUID != "s1" || messages[1].UUID != "s2" {
		t.Errorf("unexpected chain order: %q, %q", messages[0].UUID, messages[1].UUID)
	}
	if messages[0].Type != "user" || messages[1].Type != "assistant" {
		t.Errorf("unexpected types: %q, %q", messages[0].Type, messages[1].Type)
	}
	if messages[0].Data == nil {
		t.Error("Data should carry the raw transcript line")
	}
}

// The sidecar is what links a subagent's messages back to the Agent tool_use
// block in the parent session.
func TestGetSubagentMessagesRecoversParentIDs(t *testing.T) {
	sessionID, agentID := "session-1", "7"
	workDir := newSubagentFixture(t, sessionID, agentID, true)

	messages, err := GetSubagentMessages(sessionID, agentID, &workDir)
	if err != nil {
		t.Fatalf("GetSubagentMessages: %v", err)
	}

	// Every message in a subagent transcript shares the same parent ids.
	for i, msg := range messages {
		if msg.ParentToolUseID == nil || *msg.ParentToolUseID != "tool-abc" {
			t.Errorf("message %d ParentToolUseID = %v, want tool-abc", i, msg.ParentToolUseID)
		}
		if msg.ParentAgentID != "agent-parent" {
			t.Errorf("message %d ParentAgentID = %q, want agent-parent", i, msg.ParentAgentID)
		}
	}
}

// A missing sidecar is the ordinary case for an older transcript, and must
// degrade to "no parent ids" rather than failing the read.
func TestGetSubagentMessagesWithoutSidecar(t *testing.T) {
	sessionID, agentID := "session-1", "7"
	workDir := newSubagentFixture(t, sessionID, agentID, false)

	messages, err := GetSubagentMessages(sessionID, agentID, &workDir)
	if err != nil {
		t.Fatalf("GetSubagentMessages: %v", err)
	}
	if len(messages) == 0 {
		t.Fatal("expected messages")
	}
	if messages[0].ParentAgentID != "" {
		t.Errorf("ParentAgentID = %q, want empty", messages[0].ParentAgentID)
	}
}

// An unreadable or malformed sidecar is optional enrichment, not a failure.
func TestGetSubagentMessagesToleratesCorruptSidecar(t *testing.T) {
	sessionID, agentID := "session-1", "7"
	workDir := newSubagentFixture(t, sessionID, agentID, true)

	sidecar := filepath.Join(getProjectDir(canonicalizePath(workDir)), sessionID,
		"subagents", "agent-"+agentID+".meta.json")
	if err := os.WriteFile(sidecar, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	messages, err := GetSubagentMessages(sessionID, agentID, &workDir)
	if err != nil {
		t.Fatalf("GetSubagentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected the transcript to still be read, got %d", len(messages))
	}
	if messages[0].ParentAgentID != "" {
		t.Errorf("ParentAgentID = %q, want empty", messages[0].ParentAgentID)
	}
}

func TestListSubagents(t *testing.T) {
	sessionID := "session-1"
	workDir := newSubagentFixture(t, sessionID, "7", false)

	ids, err := ListSubagents(sessionID, &workDir)
	if err != nil {
		t.Fatalf("ListSubagents: %v", err)
	}
	if len(ids) != 1 || ids[0] != "7" {
		t.Errorf("ListSubagents = %v, want [7]", ids)
	}
}

func TestGetSubagentMessagesUnknownAgent(t *testing.T) {
	sessionID := "session-1"
	workDir := newSubagentFixture(t, sessionID, "7", false)

	if _, err := GetSubagentMessages(sessionID, "missing", &workDir); err == nil {
		t.Error("expected an error for an agent that never ran")
	}
}

func TestAgentMetadataSidecarPath(t *testing.T) {
	got := AgentMetadataSidecarPath(filepath.Join("subagents", "agent-7.jsonl"))
	want := filepath.Join("subagents", "agent-7.meta.json")
	if got != want {
		t.Errorf("AgentMetadataSidecarPath = %q, want %q", got, want)
	}
}

func TestParentIDsFromAgentMetadata(t *testing.T) {
	toolUseID, parentAgentID := ParentIDsFromAgentMetadata(map[string]any{
		"toolUseId": "tool-1", "parentAgentId": "agent-1",
	})
	if toolUseID != "tool-1" || parentAgentID != "agent-1" {
		t.Errorf("got (%q, %q)", toolUseID, parentAgentID)
	}

	// Absent, nil, and wrongly-typed metadata all mean "no parent ids".
	if id, parent := ParentIDsFromAgentMetadata(nil); id != "" || parent != "" {
		t.Errorf("nil metadata gave (%q, %q)", id, parent)
	}
	if id, parent := ParentIDsFromAgentMetadata(map[string]any{"toolUseId": 42}); id != "" || parent != "" {
		t.Errorf("wrongly-typed metadata gave (%q, %q)", id, parent)
	}
}

// A transcript from an older CLI has no uuids to walk, so file order is the
// only chain available; returning nothing would be the wrong failure mode.
func TestGetSubagentMessagesWithoutUUIDs(t *testing.T) {
	workDir := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", t.TempDir())

	sessionID := "session-1"
	subagentsDir := filepath.Join(getProjectDir(canonicalizePath(workDir)), sessionID, "subagents")
	if err := os.MkdirAll(subagentsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, filepath.Join(subagentsDir, "agent-7.jsonl"), []map[string]any{
		{"type": "user", "message": map[string]any{"role": "user", "content": "a"}},
		{"type": "assistant", "message": map[string]any{"role": "assistant", "content": []any{}}},
	})

	messages, err := GetSubagentMessages(sessionID, "7", &workDir)
	if err != nil {
		t.Fatalf("GetSubagentMessages: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("expected both entries in file order, got %d", len(messages))
	}
}

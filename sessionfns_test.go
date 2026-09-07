package claude

import (
	"context"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// A subagent transcript is entirely sidechain, so filtering sidechain entries
// the way the main-session read does would return nothing at all.
func TestGetSubagentMessagesFromStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	key := types.SessionKey{
		ProjectKey: "proj", SessionID: "sess-1", Subpath: "subagents/agent-7",
	}

	sidechain := func(uuid, text string) types.SessionStoreEntry {
		entry := userEntry(uuid, text)
		entry["isSidechain"] = true
		entry["sessionId"] = "sess-1"
		return entry
	}

	if err := store.Append(ctx, key, []types.SessionStoreEntry{
		{"type": "agent_metadata", "toolUseId": "tool-abc", "parentAgentId": "agent-parent"},
		sidechain("s1", "do the thing"),
		sidechain("s2", "done"),
	}); err != nil {
		t.Fatal(err)
	}

	messages, err := GetSubagentMessagesFromStore(ctx, store, "proj", "sess-1", "subagents/agent-7")
	if err != nil {
		t.Fatalf("GetSubagentMessagesFromStore: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(messages), messages)
	}

	// The metadata entry is the store's copy of the .meta.json sidecar, not a
	// transcript line.
	for i, msg := range messages {
		if msg.Type != "user" {
			t.Errorf("message %d type = %q", i, msg.Type)
		}
		if msg.ParentToolUseID == nil || *msg.ParentToolUseID != "tool-abc" {
			t.Errorf("message %d ParentToolUseID = %v, want tool-abc", i, msg.ParentToolUseID)
		}
		if msg.ParentAgentID != "agent-parent" {
			t.Errorf("message %d ParentAgentID = %q, want agent-parent", i, msg.ParentAgentID)
		}
		if msg.SessionID != "sess-1" {
			t.Errorf("message %d SessionID = %q", i, msg.SessionID)
		}
	}
}

// A store with no metadata entry is the ordinary case for a transcript
// mirrored before sidecars were carried.
func TestGetSubagentMessagesFromStoreWithoutMetadata(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	key := types.SessionKey{ProjectKey: "proj", SessionID: "sess-1", Subpath: "subagents/agent-7"}

	entry := userEntry("s1", "hi")
	entry["isSidechain"] = true
	if err := store.Append(ctx, key, []types.SessionStoreEntry{entry}); err != nil {
		t.Fatal(err)
	}

	messages, err := GetSubagentMessagesFromStore(ctx, store, "proj", "sess-1", "subagents/agent-7")
	if err != nil {
		t.Fatalf("GetSubagentMessagesFromStore: %v", err)
	}
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if messages[0].ParentAgentID != "" || messages[0].ParentToolUseID != nil {
		t.Errorf("expected no parent ids, got %+v", messages[0])
	}
}

// The metadata entry is rewritten on resume, so the last one wins.
func TestSplitAgentMetadataTakesTheLast(t *testing.T) {
	meta, transcript := splitAgentMetadata([]types.SessionStoreEntry{
		{"type": "agent_metadata", "toolUseId": "old"},
		userEntry("s1", "hi"),
		{"type": "agent_metadata", "toolUseId": "new"},
	})

	if meta == nil || meta["toolUseId"] != "new" {
		t.Errorf("metadata = %v, want the last entry", meta)
	}
	if len(transcript) != 1 || transcript[0].UUID() != "s1" {
		t.Errorf("transcript = %v, want only the transcript line", transcript)
	}
}

// The main-session read is the one that filters sidechain traffic: it is not
// part of the user-visible conversation there.
func TestGetSessionMessagesFromStoreFiltersSidechains(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	key := types.SessionKey{ProjectKey: "proj", SessionID: "sess-1"}

	sidechain := userEntry("s1", "subagent traffic")
	sidechain["isSidechain"] = true
	meta := userEntry("m1", "bookkeeping")
	meta["isMeta"] = true

	if err := store.Append(ctx, key, []types.SessionStoreEntry{
		userEntry("u1", "hello"), sidechain, meta,
		{"type": "result", "uuid": "r1"},
	}); err != nil {
		t.Fatal(err)
	}

	messages, err := GetSessionMessagesFromStore(ctx, store, "proj", "sess-1")
	if err != nil {
		t.Fatalf("GetSessionMessagesFromStore: %v", err)
	}
	if len(messages) != 1 || messages[0].UUID != "u1" {
		t.Errorf("expected only the conversation turn, got %+v", messages)
	}
}

func TestSubagentStoreFunctionsRejectNilStore(t *testing.T) {
	ctx := context.Background()
	if _, err := GetSubagentMessagesFromStore(ctx, nil, "proj", "sess", "sub"); err == nil {
		t.Error("expected an error for a nil store")
	}
	if _, err := GetSessionMessagesFromStore(ctx, nil, "proj", "sess"); err == nil {
		t.Error("expected an error for a nil store")
	}
}

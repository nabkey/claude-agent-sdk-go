package claude

import (
	"context"
	"reflect"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func userEntry(uuid, text string) types.SessionStoreEntry {
	return types.SessionStoreEntry{
		"type":      "user",
		"uuid":      uuid,
		"timestamp": "2026-01-01T00:00:00Z",
		"message":   map[string]any{"role": "user", "content": text},
	}
}

func TestInMemorySessionStoreRoundTrip(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()
	key := types.SessionKey{ProjectKey: "proj", SessionID: "sess-1"}

	// A key that was never written is distinguishable from an empty one.
	loaded, err := store.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil for an unwritten key, got %v", loaded)
	}

	entries := []types.SessionStoreEntry{userEntry("u1", "hello"), userEntry("u2", "world")}
	if err := store.Append(ctx, key, entries); err != nil {
		t.Fatalf("Append: %v", err)
	}

	loaded, err = store.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(loaded, entries) {
		t.Errorf("round trip mismatch:\n got  = %v\n want = %v", loaded, entries)
	}
}

func TestInMemorySessionStoreListing(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()

	for _, id := range []string{"a", "b", "c"} {
		key := types.SessionKey{ProjectKey: "proj", SessionID: id}
		if err := store.Append(ctx, key, []types.SessionStoreEntry{userEntry("u-"+id, "hi "+id)}); err != nil {
			t.Fatal(err)
		}
	}
	// A different project must not leak into the listing.
	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: "other", SessionID: "z"},
		[]types.SessionStoreEntry{userEntry("u-z", "nope")}); err != nil {
		t.Fatal(err)
	}
	// Nor must a subagent transcript appear as its own session.
	if err := store.Append(ctx,
		types.SessionKey{ProjectKey: "proj", SessionID: "a", Subpath: "subagents/agent-1"},
		[]types.SessionStoreEntry{userEntry("u-sub", "sub")}); err != nil {
		t.Fatal(err)
	}

	listed, err := store.ListSessions(ctx, "proj")
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(listed) != 3 {
		t.Fatalf("expected 3 sessions, got %d: %+v", len(listed), listed)
	}
	// Newest first.
	if listed[0].SessionID != "c" {
		t.Errorf("expected newest session first, got %q", listed[0].SessionID)
	}

	subkeys, err := store.ListSubkeys(ctx, "proj", "a")
	if err != nil {
		t.Fatalf("ListSubkeys: %v", err)
	}
	if !reflect.DeepEqual(subkeys, []string{"subagents/agent-1"}) {
		t.Errorf("ListSubkeys = %v", subkeys)
	}
}

// Deleting a main transcript must cascade, so subagent transcripts are not
// orphaned.
func TestInMemorySessionStoreDeleteCascades(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()

	main := types.SessionKey{ProjectKey: "proj", SessionID: "s"}
	sub := types.SessionKey{ProjectKey: "proj", SessionID: "s", Subpath: "subagents/agent-1"}
	other := types.SessionKey{ProjectKey: "proj", SessionID: "keep"}

	for _, key := range []types.SessionKey{main, sub, other} {
		if err := store.Append(ctx, key, []types.SessionStoreEntry{userEntry("u", "x")}); err != nil {
			t.Fatal(err)
		}
	}

	if err := store.Delete(ctx, main); err != nil {
		t.Fatalf("Delete: %v", err)
	}

	for _, key := range []types.SessionKey{main, sub} {
		if loaded, _ := store.Load(ctx, key); loaded != nil {
			t.Errorf("expected %+v to be deleted", key)
		}
	}
	if loaded, _ := store.Load(ctx, other); loaded == nil {
		t.Error("unrelated session must survive a cascading delete")
	}
}

func TestFoldSessionSummary(t *testing.T) {
	key := types.SessionKey{ProjectKey: "proj", SessionID: "s"}

	first := FoldSessionSummary(nil, key, []types.SessionStoreEntry{
		userEntry("u1", "first prompt"),
	})
	if got := first.Data["first_prompt"]; got != "first prompt" {
		t.Errorf("first_prompt = %v", got)
	}
	if first.SessionID != "s" {
		t.Errorf("SessionID = %q", first.SessionID)
	}

	// Folding is incremental: the first prompt sticks, the last one moves.
	second := FoldSessionSummary(&first, key, []types.SessionStoreEntry{
		userEntry("u2", "second prompt"),
	})
	if got := second.Data["first_prompt"]; got != "first prompt" {
		t.Errorf("first_prompt should not change, got %v", got)
	}
	if got := second.Data["last_prompt"]; got != "second prompt" {
		t.Errorf("last_prompt = %v", got)
	}
	if got := second.Data["entry_count"]; got != 2 {
		t.Errorf("entry_count = %v, want 2", got)
	}

	// The fold must not mutate prev.
	if got := first.Data["last_prompt"]; got != "first prompt" {
		t.Errorf("fold mutated prev: last_prompt = %v", got)
	}
}

func TestFoldSessionSummaryTitleAndTag(t *testing.T) {
	key := types.SessionKey{ProjectKey: "proj", SessionID: "s"}

	summary := FoldSessionSummary(nil, key, []types.SessionStoreEntry{
		userEntry("u1", "the prompt"),
		{"type": "session_metadata", "title": "My Session", "tag": "wip"},
	})

	info := SessionInfoFromSummary(summary)
	if info.CustomTitle == nil || *info.CustomTitle != "My Session" {
		t.Errorf("CustomTitle = %v", info.CustomTitle)
	}
	if info.Tag == nil || *info.Tag != "wip" {
		t.Errorf("Tag = %v", info.Tag)
	}
	// A custom title wins over the first prompt for the display summary.
	if info.Summary != "My Session" {
		t.Errorf("Summary = %q, want the custom title", info.Summary)
	}

	// An explicit null clears the tag.
	cleared := FoldSessionSummary(&summary, key, []types.SessionStoreEntry{
		{"type": "session_metadata", "tag": nil},
	})
	if _, present := cleared.Data["tag"]; present {
		t.Error("a null tag must clear the stored tag")
	}
}

// Content blocks are the other shape a user entry takes; tool results in them
// are not the user's words and must not become the summary.
func TestFoldSessionSummaryBlockContent(t *testing.T) {
	key := types.SessionKey{ProjectKey: "proj", SessionID: "s"}

	summary := FoldSessionSummary(nil, key, []types.SessionStoreEntry{
		{
			"type": "user",
			"uuid": "u1",
			"message": map[string]any{
				"role": "user",
				"content": []any{
					map[string]any{"type": "tool_result", "content": "ignored"},
					map[string]any{"type": "text", "text": "actual prompt"},
				},
			},
		},
	})

	if got := summary.Data["first_prompt"]; got != "actual prompt" {
		t.Errorf("first_prompt = %v, want the text block", got)
	}
}

func TestListSessionsFromStore(t *testing.T) {
	ctx := context.Background()
	store := NewInMemorySessionStore()

	for _, id := range []string{"one", "two"} {
		key := types.SessionKey{ProjectKey: "proj", SessionID: id}
		if err := store.Append(ctx, key, []types.SessionStoreEntry{userEntry("u-"+id, "prompt "+id)}); err != nil {
			t.Fatal(err)
		}
	}

	infos, err := ListSessionsFromStore(ctx, store, "proj")
	if err != nil {
		t.Fatalf("ListSessionsFromStore: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
	if infos[0].SessionID != "two" {
		t.Errorf("expected newest first, got %q", infos[0].SessionID)
	}
	if infos[0].Summary != "prompt two" {
		t.Errorf("Summary = %q", infos[0].Summary)
	}
}

// storeWithoutOptionals implements only the required methods, which is what
// the optional-capability interfaces exist to accommodate.
type storeWithoutOptionals struct{ inner *InMemorySessionStore }

func (s storeWithoutOptionals) Append(ctx context.Context, key types.SessionKey, entries []types.SessionStoreEntry) error {
	return s.inner.Append(ctx, key, entries)
}

func (s storeWithoutOptionals) Load(ctx context.Context, key types.SessionKey) ([]types.SessionStoreEntry, error) {
	return s.inner.Load(ctx, key)
}

func TestStoreOptionalCapabilitiesAreOptional(t *testing.T) {
	ctx := context.Background()
	store := storeWithoutOptionals{inner: NewInMemorySessionStore()}

	key := types.SessionKey{ProjectKey: "proj", SessionID: "s"}
	if err := store.Append(ctx, key, []types.SessionStoreEntry{userEntry("u1", "hi")}); err != nil {
		t.Fatal(err)
	}

	// Listing needs a capability this store lacks, and must say so.
	if _, err := ListSessionsFromStore(ctx, store, "proj"); err == nil {
		t.Error("expected an error listing from a store without SessionLister")
	}

	// Deletion degrades to a no-op, which is correct for append-only backends.
	if err := DeleteSessionViaStore(ctx, store, "proj", "s"); err != nil {
		t.Errorf("delete on a store without SessionDeleter should no-op, got: %v", err)
	}
	if loaded, _ := store.Load(ctx, key); loaded == nil {
		t.Error("a no-op delete must not remove anything")
	}

	// Reading works, since it only needs Load.
	msgs, err := GetSessionMessagesFromStore(ctx, store, "proj", "s")
	if err != nil {
		t.Fatalf("GetSessionMessagesFromStore: %v", err)
	}
	if len(msgs) != 1 || msgs[0].Type != "user" {
		t.Errorf("messages = %+v", msgs)
	}
}

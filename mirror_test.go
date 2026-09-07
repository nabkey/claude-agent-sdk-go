package claude

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// recordingStore captures appends so a test can assert on what the mirror
// wrote, and can be told to fail.
type recordingStore struct {
	mu      sync.Mutex
	appends []recordedAppend
	fail    error
	calls   int
}

type recordedAppend struct {
	key     types.SessionKey
	entries []types.SessionStoreEntry
}

func (s *recordingStore) Append(_ context.Context, key types.SessionKey, entries []types.SessionStoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.fail != nil {
		return s.fail
	}
	s.appends = append(s.appends, recordedAppend{key: key, entries: entries})
	return nil
}

func (s *recordingStore) Load(context.Context, types.SessionKey) ([]types.SessionStoreEntry, error) {
	return nil, nil
}

func (s *recordingStore) recorded() []recordedAppend {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recordedAppend(nil), s.appends...)
}

func (s *recordingStore) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// The transcript file path is what tells the mirror which session an entry
// belongs to, including whether it is a subagent's.
func TestMirrorKeyForPath(t *testing.T) {
	sink := newMirrorSink(&recordingStore{}, "proj", "", nil)

	tests := []struct {
		name string
		path string
		want types.SessionKey
		ok   bool
	}{
		{
			name: "main transcript",
			path: filepath.Join("projects", "proj", "sess-1.jsonl"),
			want: types.SessionKey{ProjectKey: "proj", SessionID: "sess-1"},
			ok:   true,
		},
		{
			name: "subagent transcript",
			path: filepath.Join("projects", "proj", "sess-1", "subagents", "agent-7.jsonl"),
			want: types.SessionKey{
				ProjectKey: "proj", SessionID: "sess-1", Subpath: "subagents/agent-7",
			},
			ok: true,
		},
		{name: "empty path", path: "", ok: false},
		{name: "not a transcript", path: "/tmp/notes.txt", ok: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			key, ok := sink.keyForPath(tc.path)
			if ok != tc.ok {
				t.Fatalf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && key != tc.want {
				t.Errorf("key = %+v, want %+v", key, tc.want)
			}
		})
	}
}

// Batched is the default because it keeps adapter latency off the streaming
// hot path: nothing reaches the store until the turn ends.
func TestMirrorBatchedModeHoldsUntilFlush(t *testing.T) {
	store := &recordingStore{}
	sink := newMirrorSink(store, "proj", types.SessionStoreFlushBatched, nil)

	sink.Enqueue(filepath.Join("projects", "proj", "sess-1.jsonl"), []map[string]any{
		{"type": "user", "uuid": "u1"},
	})
	if got := store.callCount(); got != 0 {
		t.Errorf("store was called %d times before the flush, want 0", got)
	}

	sink.Flush()

	recorded := store.recorded()
	if len(recorded) != 1 || len(recorded[0].entries) != 1 {
		t.Fatalf("recorded = %+v", recorded)
	}
	if recorded[0].key.SessionID != "sess-1" {
		t.Errorf("key = %+v", recorded[0].key)
	}
	if recorded[0].entries[0].UUID() != "u1" {
		t.Errorf("entry = %v", recorded[0].entries[0])
	}
}

// Eager mode exists for stores that want entries in near real time.
func TestMirrorEagerModeFlushesOnEnqueue(t *testing.T) {
	store := &recordingStore{}
	sink := newMirrorSink(store, "proj", types.SessionStoreFlushEager, nil)

	sink.Enqueue(filepath.Join("projects", "proj", "sess-1.jsonl"), []map[string]any{
		{"type": "user", "uuid": "u1"},
	})

	waitUntil(t, func() bool { return len(store.recorded()) == 1 })
}

// A flush with nothing pending must not call the adapter at all.
func TestMirrorFlushIsANoopWhenEmpty(t *testing.T) {
	store := &recordingStore{}
	newMirrorSink(store, "proj", "", nil).Flush()

	if got := store.callCount(); got != 0 {
		t.Errorf("store was called %d times, want 0", got)
	}
}

// Entries from different transcripts are keyed separately, so a subagent's
// lines never land in the main session's.
func TestMirrorSeparatesTranscripts(t *testing.T) {
	store := &recordingStore{}
	sink := newMirrorSink(store, "proj", "", nil)

	sink.Enqueue(filepath.Join("projects", "proj", "sess-1.jsonl"),
		[]map[string]any{{"type": "user", "uuid": "u1"}})
	sink.Enqueue(filepath.Join("projects", "proj", "sess-1", "subagents", "agent-7.jsonl"),
		[]map[string]any{{"type": "user", "uuid": "s1"}})
	sink.Flush()

	recorded := store.recorded()
	if len(recorded) != 2 {
		t.Fatalf("expected two keyed batches, got %+v", recorded)
	}

	bySubpath := map[string]string{}
	for _, entry := range recorded {
		bySubpath[entry.key.Subpath] = entry.entries[0].UUID()
	}
	if bySubpath[""] != "u1" || bySubpath["subagents/agent-7"] != "s1" {
		t.Errorf("batches = %v", bySubpath)
	}
}

// The local transcript is already durable, so a failed batch is reported and
// dropped rather than stalling the session.
func TestMirrorReportsAppendFailures(t *testing.T) {
	store := &recordingStore{fail: errors.New("backend unavailable")}

	var (
		mu     sync.Mutex
		gotKey *types.SessionKey
		gotErr error
	)
	sink := newMirrorSink(store, "proj", "", func(key *types.SessionKey, err error) {
		mu.Lock()
		defer mu.Unlock()
		gotKey, gotErr = key, err
	})

	sink.Enqueue(filepath.Join("projects", "proj", "sess-1.jsonl"),
		[]map[string]any{{"type": "user", "uuid": "u1"}})
	sink.Flush()

	mu.Lock()
	defer mu.Unlock()
	if gotErr == nil {
		t.Fatal("expected the failure to be reported")
	}
	if gotKey == nil || gotKey.SessionID != "sess-1" {
		t.Errorf("reported key = %+v", gotKey)
	}
	// A non-timeout failure is retried before being given up on.
	if got := store.callCount(); got != mirrorAppendAttempts {
		t.Errorf("store was called %d times, want %d", got, mirrorAppendAttempts)
	}
}

// A frame the mirror cannot key correctly is dropped: writing it under a
// guessed key would corrupt the store.
func TestMirrorDropsUnkeyableFrames(t *testing.T) {
	store := &recordingStore{}
	sink := newMirrorSink(store, "proj", "", nil)

	sink.Enqueue("/tmp/not-a-transcript.txt", []map[string]any{{"type": "user"}})
	sink.Flush()

	if got := store.callCount(); got != 0 {
		t.Errorf("store was called %d times, want 0", got)
	}
}

// A long turn must not accumulate an unbounded backlog, so the sink flushes
// early once enough entries are pending.
func TestMirrorFlushesOnEntryOverflow(t *testing.T) {
	store := &recordingStore{}
	sink := newMirrorSink(store, "proj", types.SessionStoreFlushBatched, nil)

	entries := make([]map[string]any, mirrorFlushEntries)
	for i := range entries {
		entries[i] = map[string]any{"type": "user", "uuid": "u"}
	}
	sink.Enqueue(filepath.Join("projects", "proj", "sess-1.jsonl"), entries)

	waitUntil(t, func() bool { return len(store.recorded()) == 1 })
}

// mirrorFor is what decides whether mirroring runs at all.
func TestMirrorForRequiresAStore(t *testing.T) {
	if sink := mirrorFor(&AgentOptions{}); sink != nil {
		t.Error("no store means no mirroring")
	}
	if sink := mirrorFor(&AgentOptions{SessionStore: NewInMemorySessionStore()}); sink == nil {
		t.Error("a store must produce a sink")
	}
}

func TestApproximateSize(t *testing.T) {
	// It only has to be good enough to trigger a flush, but a longer entry
	// must estimate larger than a shorter one.
	small := approximateSize(map[string]any{"type": "user"})
	large := approximateSize(map[string]any{"type": "user", "text": "a much longer value"})

	if small <= 0 || large <= small {
		t.Errorf("sizes = %d, %d", small, large)
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		if cond() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for the condition")
		case <-time.After(2 * time.Millisecond):
		}
	}
}

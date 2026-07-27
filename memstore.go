package claude

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// InMemorySessionStore is a SessionStore backed by process memory.
//
// It exists for tests and for short-lived processes that want resume within a
// single run. Nothing is persisted, so a restart loses every session.
//
// It implements every optional SessionStore capability, which also makes it a
// worked reference for adapter authors.
type InMemorySessionStore struct {
	mu sync.RWMutex

	entries   map[string][]types.SessionStoreEntry
	summaries map[string]types.SessionSummaryEntry
	mtimes    map[string]int64

	// clock is a monotonically increasing stand-in for storage write time.
	// A real adapter uses its backend's native timestamp; using a counter
	// here keeps tests deterministic and ordering stable regardless of how
	// fast writes arrive.
	clock int64
}

// NewInMemorySessionStore returns an empty in-memory store.
func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		entries:   make(map[string][]types.SessionStoreEntry),
		summaries: make(map[string]types.SessionSummaryEntry),
		mtimes:    make(map[string]int64),
	}
}

// keyString renders a SessionKey as a map key.
func keyString(key types.SessionKey) string {
	if key.Subpath == "" {
		return key.ProjectKey + "\x00" + key.SessionID
	}
	return key.ProjectKey + "\x00" + key.SessionID + "\x00" + key.Subpath
}

// nextMTime advances and returns the store's logical clock.
func (s *InMemorySessionStore) nextMTime() int64 {
	s.clock++
	return s.clock
}

// Append records a batch of transcript entries.
func (s *InMemorySessionStore) Append(_ context.Context, key types.SessionKey, entries []types.SessionStoreEntry) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	k := keyString(key)
	s.entries[k] = append(s.entries[k], entries...)

	mtime := s.nextMTime()
	s.mtimes[k] = mtime

	// Subagent transcripts must not contribute to the main session summary.
	if key.Subpath != "" {
		return nil
	}

	summaryKey := key.ProjectKey + "\x00" + key.SessionID
	prev, hadPrev := s.summaries[summaryKey]
	var prevPtr *types.SessionSummaryEntry
	if hadPrev {
		prevPtr = &prev
	}

	folded := FoldSessionSummary(prevPtr, key, entries)
	folded.MTime = mtime
	s.summaries[summaryKey] = folded
	return nil
}

// Load returns a session's entries, or nil if it was never written.
func (s *InMemorySessionStore) Load(_ context.Context, key types.SessionKey) ([]types.SessionStoreEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries, ok := s.entries[keyString(key)]
	if !ok {
		return nil, nil
	}
	return append([]types.SessionStoreEntry{}, entries...), nil
}

// ListSessions enumerates main transcripts under a project key.
func (s *InMemorySessionStore) ListSessions(_ context.Context, projectKey string) ([]types.SessionStoreListEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := projectKey + "\x00"
	var out []types.SessionStoreListEntry
	for k := range s.entries {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		rest := k[len(prefix):]
		// Subagent transcripts carry a further separator; skip them.
		if strings.Contains(rest, "\x00") {
			continue
		}
		out = append(out, types.SessionStoreListEntry{SessionID: rest, MTime: s.mtimes[k]})
	}

	sort.Slice(out, func(i, j int) bool { return out[i].MTime > out[j].MTime })
	return out, nil
}

// ListSessionSummaries returns the maintained summaries for a project key.
func (s *InMemorySessionStore) ListSessionSummaries(_ context.Context, projectKey string) ([]types.SessionSummaryEntry, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := projectKey + "\x00"
	var out []types.SessionSummaryEntry
	for k, summary := range s.summaries {
		if strings.HasPrefix(k, prefix) {
			out = append(out, summary)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].MTime > out[j].MTime })
	return out, nil
}

// Delete removes a session. Deleting a main transcript cascades to its
// subagent transcripts.
func (s *InMemorySessionStore) Delete(_ context.Context, key types.SessionKey) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if key.Subpath != "" {
		k := keyString(key)
		delete(s.entries, k)
		delete(s.mtimes, k)
		return nil
	}

	prefix := key.ProjectKey + "\x00" + key.SessionID
	for k := range s.entries {
		if k == prefix || strings.HasPrefix(k, prefix+"\x00") {
			delete(s.entries, k)
			delete(s.mtimes, k)
		}
	}
	delete(s.summaries, prefix)
	return nil
}

// ListSubkeys enumerates the subagent transcripts under a session.
func (s *InMemorySessionStore) ListSubkeys(_ context.Context, projectKey, sessionID string) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	prefix := projectKey + "\x00" + sessionID + "\x00"
	var out []string
	for k := range s.entries {
		if strings.HasPrefix(k, prefix) {
			out = append(out, k[len(prefix):])
		}
	}
	sort.Strings(out)
	return out, nil
}

// Len reports how many transcripts the store holds. Intended for tests.
func (s *InMemorySessionStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.entries)
}

// Clear empties the store. Intended for tests.
func (s *InMemorySessionStore) Clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = make(map[string][]types.SessionStoreEntry)
	s.summaries = make(map[string]types.SessionSummaryEntry)
	s.mtimes = make(map[string]int64)
	s.clock = 0
}

// Compile-time proof that the in-memory store implements every optional
// capability, so it stays a complete reference for adapter authors.
var (
	_ SessionStore        = (*InMemorySessionStore)(nil)
	_ SessionLister       = (*InMemorySessionStore)(nil)
	_ SessionSummarizer   = (*InMemorySessionStore)(nil)
	_ SessionDeleter      = (*InMemorySessionStore)(nil)
	_ SessionSubkeyLister = (*InMemorySessionStore)(nil)
)

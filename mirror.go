package claude

import (
	"context"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

const (
	// mirrorFlushEntries and mirrorFlushBytes bound how much a batched sink
	// buffers before flushing early, so a long turn does not accumulate an
	// unbounded backlog.
	mirrorFlushEntries = 500
	mirrorFlushBytes   = 1 << 20 // 1 MiB

	// mirrorAppendAttempts is the total number of tries for a batch.
	mirrorAppendAttempts = 3

	// mirrorAppendTimeout bounds a single Append call. A timeout is not
	// retried: the in-flight call may still land, and retrying would risk
	// duplicating it.
	mirrorAppendTimeout = 60 * time.Second
)

// mirrorSink buffers transcript entries and writes them to a SessionStore.
//
// The subprocess has already written to local disk by the time a frame
// arrives, so durability is never at stake here; this is a secondary copy.
// Failures are therefore reported and dropped rather than stalling the
// session.
type mirrorSink struct {
	store      SessionStore
	projectKey string
	flushMode  types.SessionStoreFlushMode
	onError    func(key *types.SessionKey, err error)

	mu      sync.Mutex
	pending map[types.SessionKey][]types.SessionStoreEntry
	bytes   int
	count   int

	// writeMu serializes Append calls so entries reach the store in enqueue
	// order even when flushes overlap.
	writeMu sync.Mutex
}

// newMirrorSink builds a sink for a store.
func newMirrorSink(store SessionStore, projectKey string, mode types.SessionStoreFlushMode,
	onError func(*types.SessionKey, error)) *mirrorSink {
	if mode == "" {
		mode = types.SessionStoreFlushBatched
	}
	return &mirrorSink{
		store:      store,
		projectKey: projectKey,
		flushMode:  mode,
		onError:    onError,
		pending:    make(map[types.SessionKey][]types.SessionStoreEntry),
	}
}

// Enqueue buffers a batch of entries from one transcript file.
//
// This runs on the read loop, so it never blocks on the store: eager mode
// schedules the flush on its own goroutine.
func (m *mirrorSink) Enqueue(filePath string, entries []map[string]any) {
	key, ok := m.keyForPath(filePath)
	if !ok {
		return
	}

	m.mu.Lock()
	for _, entry := range entries {
		m.pending[key] = append(m.pending[key], types.SessionStoreEntry(entry))
		m.count++
		m.bytes += approximateSize(entry)
	}
	shouldFlush := m.flushMode == types.SessionStoreFlushEager ||
		m.count >= mirrorFlushEntries || m.bytes >= mirrorFlushBytes
	m.mu.Unlock()

	if shouldFlush {
		go m.Flush()
	}
}

// Flush writes buffered entries to the store.
func (m *mirrorSink) Flush() {
	m.mu.Lock()
	if len(m.pending) == 0 {
		m.mu.Unlock()
		return
	}
	batch := m.pending
	m.pending = make(map[types.SessionKey][]types.SessionStoreEntry)
	m.count = 0
	m.bytes = 0
	m.mu.Unlock()

	// Serialize writes so entries land in enqueue order.
	m.writeMu.Lock()
	defer m.writeMu.Unlock()

	for key, entries := range batch {
		m.append(key, entries)
	}
}

// append writes one batch, retrying transient failures.
func (m *mirrorSink) append(key types.SessionKey, entries []types.SessionStoreEntry) {
	var lastErr error

	for attempt := 1; attempt <= mirrorAppendAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), mirrorAppendTimeout)
		err := m.store.Append(ctx, key, entries)
		timedOut := ctx.Err() != nil
		cancel()

		if err == nil {
			return
		}
		lastErr = err

		// A timeout is not retried: the call may still be in flight, and a
		// retry could duplicate the batch in stores without idempotent writes.
		if timedOut {
			break
		}
		if attempt < mirrorAppendAttempts {
			time.Sleep(time.Duration(attempt) * 100 * time.Millisecond)
		}
	}

	if m.onError != nil {
		m.onError(&key, lastErr)
		return
	}
	log.Printf("claude-agent-sdk: dropping mirrored transcript batch for session %s: %v",
		key.SessionID, lastErr)
}

// keyForPath derives a SessionKey from a transcript file path.
//
// Returns false when the path is not under the projects directory, which
// happens if the subprocess uses a different CLAUDE_CONFIG_DIR than the
// parent -- a custom spawn or container. Mirroring a frame we cannot key
// correctly would corrupt the store, so it is dropped instead.
func (m *mirrorSink) keyForPath(filePath string) (types.SessionKey, bool) {
	if filePath == "" {
		return types.SessionKey{}, false
	}

	base := filepath.Base(filePath)
	if !strings.HasSuffix(base, ".jsonl") {
		return types.SessionKey{}, false
	}
	sessionID := strings.TrimSuffix(base, ".jsonl")

	key := types.SessionKey{ProjectKey: m.projectKey, SessionID: sessionID}

	// A subagent transcript lives under <sessionID>/subagents/...; the
	// session ID is then the directory, not the file.
	dir := filepath.Dir(filePath)
	if parent := filepath.Base(filepath.Dir(dir)); filepath.Base(dir) == "subagents" {
		key.SessionID = parent
		key.Subpath = "subagents/" + sessionID
	}

	return key, true
}

// approximateSize estimates an entry's serialized size, cheaply enough to run
// on the read loop. It only has to be good enough to trigger a flush.
func approximateSize(entry map[string]any) int {
	size := 0
	for k, v := range entry {
		size += len(k) + 8
		if s, ok := v.(string); ok {
			size += len(s)
		} else {
			size += 32
		}
	}
	return size
}

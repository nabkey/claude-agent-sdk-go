package claude

import (
	"context"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// SessionStore mirrors session transcripts to external storage.
//
// The CLI subprocess still writes to local disk (set CLAUDE_CONFIG_DIR to a
// temporary directory for an ephemeral local copy); the adapter receives a
// secondary copy. This lets a multi-tenant or serverless deployment resume a
// session that was never on this machine's disk.
//
// Only Append and Load are required. The optional capabilities are declared as
// separate interfaces that a store may also implement; call sites probe for
// them at runtime, so a store need only implement what it supports.
//
// The SDK never deletes from your store unless DeleteSessionViaStore is called
// on a store implementing SessionDeleter. Retention is the adapter's
// responsibility -- implement TTLs, object lifecycle policies, or scheduled
// cleanup according to your compliance requirements.
type SessionStore interface {
	// Append mirrors a batch of transcript entries.
	//
	// Called after the subprocess's local write succeeds, so durability is
	// already guaranteed locally. Batches arrive at roughly 100ms cadence
	// during active turns.
	//
	// Within a process, entries are persisted in call order. Most carry a
	// stable UUID that adapters should treat as an idempotency key.
	//
	// A failed batch is retried a few times with backoff before being dropped
	// and surfaced as a mirror error message; the session continues either
	// way.
	Append(ctx context.Context, key types.SessionKey, entries []types.SessionStoreEntry) error

	// Load returns a full session for resume, or nil if the key was never
	// written.
	//
	// Called once, before the subprocess spawns. The result is materialized
	// to a temporary JSONL file that the subprocess resumes from.
	//
	// Returned entries must be deep-equal to what was appended; byte-equal
	// serialization is not required, since the SDK never hashes or compares
	// entries byte-wise.
	Load(ctx context.Context, key types.SessionKey) ([]types.SessionStoreEntry, error)
}

// SessionLister is a SessionStore that can enumerate its sessions.
//
// Without it, ListSessionsFromStore returns an error.
type SessionLister interface {
	// ListSessions returns the sessions under a project key. Result order is
	// unspecified; the SDK sorts by modification time, newest first.
	ListSessions(ctx context.Context, projectKey string) ([]types.SessionStoreListEntry, error)
}

// SessionSummarizer is a SessionStore that maintains incremental summaries.
//
// Stores should update these via FoldSessionSummary inside Append, skipping
// keys with a Subpath so subagent transcripts do not contribute to the main
// session's summary. When implemented, listing reads all metadata in a single
// round trip instead of loading each session.
//
// A store maintaining summaries inside Append must serialize sidecar writes if
// Append can race for the same session -- wrap the read-fold-write in a
// transaction or hold a per-session lock. FoldSessionSummary is pure;
// concurrency control belongs to the store.
type SessionSummarizer interface {
	ListSessionSummaries(ctx context.Context, projectKey string) ([]types.SessionSummaryEntry, error)
}

// SessionDeleter is a SessionStore that supports deletion.
//
// Deleting a main-transcript key (one with no Subpath) must cascade to every
// subkey under that session, so subagent transcripts are not orphaned. A
// delete with an explicit Subpath removes only that entry.
//
// Without it, deletion is a no-op, which is the right behavior for
// append-only backends such as object storage.
type SessionDeleter interface {
	Delete(ctx context.Context, key types.SessionKey) error
}

// SessionSubkeyLister is a SessionStore that can enumerate subagent
// transcripts under a session, so resume can materialize them too.
//
// Without it, resume materializes only the main transcript.
type SessionSubkeyLister interface {
	ListSubkeys(ctx context.Context, projectKey, sessionID string) ([]string, error)
}

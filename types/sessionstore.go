package types

// SessionKey identifies a session transcript, or a subagent transcript within
// one, in an external store.
type SessionKey struct {
	// ProjectKey is a caller-defined scope, defaulting to the sanitized
	// working directory. Multi-tenant deployments should set it to a tenant
	// ID or project name.
	ProjectKey string `json:"project_key"`

	// SessionID identifies the session.
	SessionID string `json:"session_id"`

	// Subpath is empty for the main transcript, and set for subagent files
	// (e.g. "subagents/agent-{id}"). It is opaque to the adapter -- treat it
	// as a storage key suffix.
	Subpath string `json:"subpath,omitempty"`
}

// SessionStoreEntry is one JSONL transcript line as an adapter observes it.
//
// The concrete shape is the CLI's on-disk transcript format, a large
// discriminated union that is internal to the CLI. Adapters should treat
// entries as pass-through blobs; surviving a JSON round trip is the only
// required invariant.
type SessionStoreEntry map[string]any

// Type returns the entry's discriminant.
func (e SessionStoreEntry) Type() string { return mapString(e, "type") }

// UUID returns the entry's stable identifier, if it has one.
//
// Most entries carry a UUID that adapters should treat as an idempotency key,
// so retries and replays do not create duplicate rows. Entries without one
// (titles, tags, mode markers) should be appended without deduplication.
func (e SessionStoreEntry) UUID() string { return mapString(e, "uuid") }

// Timestamp returns the entry's ISO 8601 timestamp, if it has one.
func (e SessionStoreEntry) Timestamp() string { return mapString(e, "timestamp") }

// SessionStoreListEntry is one session as reported by a store listing.
type SessionStoreListEntry struct {
	SessionID string `json:"session_id"`
	// MTime is the last-modified time in Unix epoch milliseconds. Adapters
	// without native modification times must maintain their own index.
	MTime int64 `json:"mtime"`
}

// SessionSummaryEntry is an incrementally-maintained session summary.
//
// Stores obtain this from FoldSessionSummary inside Append and persist it
// verbatim, then return the full set from ListSessionSummaries. Data is
// SDK-owned state -- stores must not interpret it.
type SessionSummaryEntry struct {
	SessionID string `json:"session_id"`

	// MTime is the storage write time of the sidecar, in Unix epoch
	// milliseconds. It must share a clock source with the MTime returned by
	// ListSessions for this session -- typically file mtime, S3
	// LastModified, or a database updated_at.
	//
	// Do not derive it from entry timestamps: any adapter that writes in
	// batches would then report storage times later than the last entry's
	// timestamp, making every sidecar look stale.
	MTime int64 `json:"mtime"`

	// Data is opaque SDK-owned summary state. Persist it verbatim.
	Data map[string]any `json:"data"`
}

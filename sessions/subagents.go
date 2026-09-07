package sessions

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ListSubagents returns the agent IDs that ran inside a session.
//
// Subagent transcripts live beside the main one, under a per-session
// subagents directory. If directory is nil the current working directory is
// used.
func ListSubagents(sessionID string, directory *string) ([]string, error) {
	dir, err := resolveSubagentsDir(sessionID, directory)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, nil
	}

	files, err := collectAgentFiles(dir)
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(files))
	for id := range files {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

// GetSubagentMessages reads one subagent's transcript.
//
// The returned messages are the subagent's conversation chain rather than
// every raw line. Every message in a subagent transcript shares the same
// parent ids: ParentToolUseID names the Agent tool_use block in the parent
// session that spawned this subagent, and ParentAgentID the spawning
// subagent for nested runs. Both are recovered from the transcript's
// .meta.json sidecar and are empty when it is missing or unreadable.
//
// Unlike GetSessionMessages this does not filter sidechain entries: every
// entry in a subagent transcript is sidechain by construction, so filtering
// them would return nothing.
func GetSubagentMessages(sessionID, agentID string, directory *string) ([]types.SessionMessage, error) {
	dir, err := resolveSubagentsDir(sessionID, directory)
	if err != nil {
		return nil, err
	}
	if dir == "" {
		return nil, fmt.Errorf("session %s has no subagent transcripts", sessionID)
	}

	files, err := collectAgentFiles(dir)
	if err != nil {
		return nil, err
	}

	path, ok := files[agentID]
	if !ok {
		return nil, fmt.Errorf("subagent %s not found in session %s", agentID, sessionID)
	}

	entries, err := readTranscript(path)
	if err != nil {
		return nil, err
	}

	// A failure to read the optional sidecar degrades to "no parent ids"
	// rather than failing the transcript read.
	toolUseID, parentAgentID := readAgentMetadataSidecar(path)
	return subagentEntriesToMessages(buildSubagentChain(entries), toolUseID, parentAgentID), nil
}

// AgentMetadataSidecarPath maps agent-<id>.jsonl to agent-<id>.meta.json in
// the same directory. It is the single definition of the naming convention,
// shared by the read path, session import, and resume materialization.
func AgentMetadataSidecarPath(transcriptPath string) string {
	return strings.TrimSuffix(transcriptPath, ".jsonl") + ".meta.json"
}

// readAgentMetadataSidecar reads the parent ids from a subagent transcript's
// .meta.json sidecar, returning empty strings when it is missing, unreadable,
// or not a JSON object.
func readAgentMetadataSidecar(transcriptPath string) (toolUseID, parentAgentID string) {
	raw, err := os.ReadFile(AgentMetadataSidecarPath(transcriptPath))
	if err != nil {
		return "", ""
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", ""
	}
	return ParentIDsFromAgentMetadata(meta)
}

// ParentIDsFromAgentMetadata extracts (toolUseId, parentAgentId) from an agent
// metadata object. It works for both the on-disk .meta.json sidecar and the
// synthetic agent_metadata entry a SessionStore receives in its place.
func ParentIDsFromAgentMetadata(meta map[string]any) (toolUseID, parentAgentID string) {
	if meta == nil {
		return "", ""
	}
	return stringField(meta, "toolUseId"), stringField(meta, "parentAgentId")
}

// buildSubagentChain walks parentUuid links back from the last user or
// assistant entry.
//
// Subagent transcripts are simpler than main sessions: no compaction, no
// nested sidechains, no preserved segments. Unlike buildConversationChain
// this accepts sidechain entries, since every entry here is one.
func buildSubagentChain(entries []transcriptEntry) []transcriptEntry {
	if len(entries) == 0 {
		return nil
	}

	byUUID := make(map[string]transcriptEntry, len(entries))
	for _, entry := range entries {
		if entry.UUID != "" {
			byUUID[entry.UUID] = entry
		}
	}

	var leaf *transcriptEntry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Type == "user" || entries[i].Type == "assistant" {
			leaf = &entries[i]
			break
		}
	}
	if leaf == nil {
		return nil
	}

	// Entries without uuids have no links to walk, so the walk below would
	// stop at the leaf and drop the rest. Fall back to file order.
	if leaf.UUID == "" {
		var flat []transcriptEntry
		for _, entry := range entries {
			if entry.Type == "user" || entry.Type == "assistant" {
				flat = append(flat, entry)
			}
		}
		return flat
	}

	var chain []transcriptEntry
	seen := make(map[string]bool)
	for current := leaf; current != nil; {
		if seen[current.UUID] {
			break // Defensive: a malformed transcript must not loop forever.
		}
		seen[current.UUID] = true
		chain = append(chain, *current)

		parent, ok := byUUID[current.ParentUUID]
		if !ok {
			break
		}
		current = &parent
	}

	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// subagentEntriesToMessages converts subagent transcript entries to session
// messages, stamping the shared parent ids onto each.
func subagentEntriesToMessages(entries []transcriptEntry, toolUseID, parentAgentID string) []types.SessionMessage {
	out := make([]types.SessionMessage, 0, len(entries))
	for _, entry := range entries {
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		msg := types.SessionMessage{
			Type:          entry.Type,
			UUID:          entry.UUID,
			SessionID:     entry.SessionID,
			Message:       entry.Message,
			ParentAgentID: parentAgentID,
			Data:          entry.Raw,
		}
		if toolUseID != "" {
			id := toolUseID
			msg.ParentToolUseID = &id
		} else {
			msg.ParentToolUseID = entry.ParentToolUseID
		}
		out = append(out, msg)
	}
	return out
}

// resolveSubagentsDir locates a session's subagent directory, returning "" if
// it has none.
func resolveSubagentsDir(sessionID string, directory *string) (string, error) {
	dir := "."
	if directory != nil {
		dir = *directory
	}

	projectDir := findProjectDir(canonicalizePath(dir))
	if projectDir == "" {
		return "", fmt.Errorf("session %s not found", sessionID)
	}

	subagents := filepath.Join(projectDir, sessionID, "subagents")
	if info, err := os.Stat(subagents); err == nil && info.IsDir() {
		return subagents, nil
	}

	// Older layouts nest the directory directly beside the transcript.
	alt := filepath.Join(projectDir, "subagents", sessionID)
	if info, err := os.Stat(alt); err == nil && info.IsDir() {
		return alt, nil
	}

	return "", nil
}

// collectAgentFiles maps agent IDs to their transcript paths, walking nested
// directories so subagents spawned by subagents are found too.
func collectAgentFiles(base string) (map[string]string, error) {
	files := make(map[string]string)

	err := filepath.WalkDir(base, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			// A directory we cannot read is skipped rather than failing the
			// whole listing.
			return nil //nolint:nilerr // partial results beat none here
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}

		id := strings.TrimSuffix(d.Name(), ".jsonl")
		id = strings.TrimPrefix(id, "agent-")
		files[id] = path
		return nil
	})
	if err != nil {
		return nil, err
	}
	return files, nil
}

// transcriptEntry is one parsed JSONL line, retaining the fields needed to
// rebuild the conversation chain.
type transcriptEntry struct {
	Type            string
	UUID            string
	ParentUUID      string
	SessionID       string
	IsSidechain     bool
	IsMeta          bool
	Message         any
	ParentToolUseID *string
	Raw             map[string]any
}

// readTranscript parses a JSONL transcript file.
func readTranscript(path string) ([]transcriptEntry, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("failed to open transcript: %w", err)
	}
	defer func() { _ = file.Close() }()

	var entries []transcriptEntry
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 1024*1024), 8*1024*1024)

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var raw map[string]any
		if err := json.Unmarshal([]byte(line), &raw); err != nil {
			continue // Skip unparseable lines rather than abandoning the file.
		}

		entry := transcriptEntry{
			Type:        stringField(raw, "type"),
			UUID:        stringField(raw, "uuid"),
			ParentUUID:  stringField(raw, "parentUuid"),
			SessionID:   stringField(raw, "sessionId"),
			IsSidechain: boolField(raw, "isSidechain"),
			IsMeta:      boolField(raw, "isMeta"),
			Message:     raw["message"],
			Raw:         raw,
		}
		if entry.SessionID == "" {
			entry.SessionID = stringField(raw, "session_id")
		}
		if v := stringField(raw, "parentToolUseID"); v != "" {
			entry.ParentToolUseID = &v
		}
		entries = append(entries, entry)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading transcript: %w", err)
	}
	return entries, nil
}

// buildConversationChain walks parentUuid links back from the newest leaf to
// recover the active conversation.
//
// A transcript is a tree, not a list: retried and edited turns leave orphaned
// branches behind. Following the links from the last entry yields the branch
// the conversation actually took.
func buildConversationChain(entries []transcriptEntry) []transcriptEntry {
	if len(entries) == 0 {
		return nil
	}

	byUUID := make(map[string]transcriptEntry, len(entries))
	for _, entry := range entries {
		if entry.UUID != "" {
			byUUID[entry.UUID] = entry
		}
	}

	// The leaf is the last entry that can start a chain.
	var leaf *transcriptEntry
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].UUID != "" && isVisibleEntry(entries[i]) {
			leaf = &entries[i]
			break
		}
	}

	// Transcripts without UUIDs have no links to walk -- older CLI versions,
	// or hand-written fixtures. Returning nothing would be the wrong failure
	// mode, so fall back to file order.
	if leaf == nil {
		var flat []transcriptEntry
		for _, entry := range entries {
			if isVisibleEntry(entry) {
				flat = append(flat, entry)
			}
		}
		return flat
	}

	var chain []transcriptEntry
	seen := make(map[string]bool)
	for current := leaf; current != nil; {
		if seen[current.UUID] {
			break // Defensive: a malformed transcript must not loop forever.
		}
		seen[current.UUID] = true
		chain = append(chain, *current)

		parent, ok := byUUID[current.ParentUUID]
		if !ok {
			break
		}
		current = &parent
	}

	// The walk runs newest-first; restore chronological order.
	for i, j := 0, len(chain)-1; i < j; i, j = i+1, j-1 {
		chain[i], chain[j] = chain[j], chain[i]
	}
	return chain
}

// isVisibleEntry reports whether an entry belongs in the conversation.
//
// Sidechain entries are subagent traffic and meta entries are internal
// bookkeeping; neither is part of the user-visible conversation.
func isVisibleEntry(entry transcriptEntry) bool {
	if entry.Type != "user" && entry.Type != "assistant" {
		return false
	}
	return !entry.IsSidechain && !entry.IsMeta
}

// entriesToMessages converts transcript entries to session messages.
func entriesToMessages(entries []transcriptEntry) []types.SessionMessage {
	out := make([]types.SessionMessage, 0, len(entries))
	for _, entry := range entries {
		if !isVisibleEntry(entry) {
			continue
		}
		out = append(out, types.SessionMessage{
			Type:            entry.Type,
			UUID:            entry.UUID,
			SessionID:       entry.SessionID,
			Message:         entry.Message,
			ParentToolUseID: entry.ParentToolUseID,
			Data:            entry.Raw,
		})
	}
	return out
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolField(m map[string]any, key string) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return false
}

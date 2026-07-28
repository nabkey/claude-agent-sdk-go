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
// Like GetSessionMessages, the returned messages are the visible conversation
// chain rather than every raw line.
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
	return entriesToMessages(buildConversationChain(entries)), nil
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

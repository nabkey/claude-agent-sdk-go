package claude

import (
	"context"
	"fmt"
	"sort"

	"github.com/nabkey/claude-agent-sdk-go/sessions"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// ListSessionsFromStore lists the sessions a SessionStore holds for a project.
//
// When the store maintains summaries it reads all metadata in one round trip;
// otherwise it falls back to listing sessions and loading each to derive the
// same information, which is correct but slower.
//
// The result is sorted by last-modified time, newest first.
func ListSessionsFromStore(ctx context.Context, store SessionStore, projectKey string) ([]types.SDKSessionInfo, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is nil")
	}

	if summarizer, ok := store.(SessionSummarizer); ok {
		summaries, err := summarizer.ListSessionSummaries(ctx, projectKey)
		if err != nil {
			return nil, err
		}
		infos := make([]types.SDKSessionInfo, 0, len(summaries))
		for _, summary := range summaries {
			infos = append(infos, SessionInfoFromSummary(summary))
		}
		sortSessionInfos(infos)
		return infos, nil
	}

	lister, ok := store.(SessionLister)
	if !ok {
		return nil, fmt.Errorf("session store does not implement SessionLister")
	}

	listed, err := lister.ListSessions(ctx, projectKey)
	if err != nil {
		return nil, err
	}

	infos := make([]types.SDKSessionInfo, 0, len(listed))
	for _, item := range listed {
		key := types.SessionKey{ProjectKey: projectKey, SessionID: item.SessionID}
		entries, err := store.Load(ctx, key)
		if err != nil {
			return nil, err
		}
		summary := FoldSessionSummary(nil, key, entries)
		summary.MTime = item.MTime
		infos = append(infos, SessionInfoFromSummary(summary))
	}

	sortSessionInfos(infos)
	return infos, nil
}

// GetSessionInfoFromStore returns metadata for one stored session, or nil if
// the store has never seen it.
func GetSessionInfoFromStore(ctx context.Context, store SessionStore, projectKey, sessionID string) (*types.SDKSessionInfo, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is nil")
	}

	key := types.SessionKey{ProjectKey: projectKey, SessionID: sessionID}
	entries, err := store.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	if entries == nil {
		return nil, nil
	}

	info := SessionInfoFromSummary(FoldSessionSummary(nil, key, entries))
	return &info, nil
}

// GetSessionMessagesFromStore reads a stored session's conversation.
func GetSessionMessagesFromStore(ctx context.Context, store SessionStore, projectKey, sessionID string) ([]types.SessionMessage, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is nil")
	}

	entries, err := store.Load(ctx, types.SessionKey{ProjectKey: projectKey, SessionID: sessionID})
	if err != nil {
		return nil, err
	}
	return storeEntriesToMessages(entries), nil
}

// ListSubagentsFromStore lists the subagent transcripts stored under a session.
func ListSubagentsFromStore(ctx context.Context, store SessionStore, projectKey, sessionID string) ([]string, error) {
	lister, ok := store.(SessionSubkeyLister)
	if !ok {
		return nil, fmt.Errorf("session store does not implement SessionSubkeyLister")
	}
	return lister.ListSubkeys(ctx, projectKey, sessionID)
}

// GetSubagentMessagesFromStore reads one stored subagent transcript.
//
// Every message shares the subagent's parent ids: ParentToolUseID names the
// Agent tool_use block in the parent session that spawned it, and
// ParentAgentID the spawning subagent for nested runs. Both come from the
// synthetic agent_metadata entry the store carries in place of the on-disk
// .meta.json sidecar, and are empty when the store has none.
//
// Unlike GetSessionMessagesFromStore this does not filter sidechain entries:
// every entry in a subagent transcript is sidechain by construction.
func GetSubagentMessagesFromStore(ctx context.Context, store SessionStore, projectKey, sessionID, subpath string) ([]types.SessionMessage, error) {
	if store == nil {
		return nil, fmt.Errorf("session store is nil")
	}

	entries, err := store.Load(ctx, types.SessionKey{
		ProjectKey: projectKey,
		SessionID:  sessionID,
		Subpath:    subpath,
	})
	if err != nil {
		return nil, err
	}

	meta, transcript := splitAgentMetadata(entries)
	toolUseID, parentAgentID := sessions.ParentIDsFromAgentMetadata(meta)
	return storeSubagentEntriesToMessages(transcript, toolUseID, parentAgentID), nil
}

// splitAgentMetadata separates the synthetic agent_metadata entry from the
// transcript lines.
//
// A subagent's SessionStore stream carries its .meta.json sidecar as
// {"type": "agent_metadata", ...} entries alongside the transcript. The entry
// is rewritten on resume, so the last one wins.
func splitAgentMetadata(entries []types.SessionStoreEntry) (map[string]any, []types.SessionStoreEntry) {
	var meta map[string]any
	transcript := make([]types.SessionStoreEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Type() == "agent_metadata" {
			meta = entry
			continue
		}
		transcript = append(transcript, entry)
	}
	return meta, transcript
}

// storeSubagentEntriesToMessages projects stored subagent entries onto session
// messages, stamping the shared parent ids onto each.
func storeSubagentEntriesToMessages(entries []types.SessionStoreEntry, toolUseID, parentAgentID string) []types.SessionMessage {
	out := make([]types.SessionMessage, 0, len(entries))
	for _, entry := range entries {
		entryType := entry.Type()
		if entryType != "user" && entryType != "assistant" {
			continue
		}
		if meta, ok := entry["isMeta"].(bool); ok && meta {
			continue
		}

		msg := types.SessionMessage{
			Type:          entryType,
			UUID:          entry.UUID(),
			Message:       entry["message"],
			ParentAgentID: parentAgentID,
			Data:          entry,
		}
		if id, ok := entry["sessionId"].(string); ok {
			msg.SessionID = id
		}
		if toolUseID != "" {
			id := toolUseID
			msg.ParentToolUseID = &id
		} else if id, ok := entry["parentToolUseID"].(string); ok {
			msg.ParentToolUseID = &id
		}
		out = append(out, msg)
	}
	return out
}

// DeleteSessionViaStore removes a session from a store.
//
// Stores that do not implement SessionDeleter treat this as a no-op, which is
// the right behavior for append-only backends.
func DeleteSessionViaStore(ctx context.Context, store SessionStore, projectKey, sessionID string) error {
	deleter, ok := store.(SessionDeleter)
	if !ok {
		return nil
	}
	return deleter.Delete(ctx, types.SessionKey{ProjectKey: projectKey, SessionID: sessionID})
}

// ImportSessionToStore copies a local session transcript into a store, so an
// existing on-disk session can be resumed from external storage later.
func ImportSessionToStore(ctx context.Context, store SessionStore, projectKey, sessionID string, entries []types.SessionStoreEntry) error {
	if store == nil {
		return fmt.Errorf("session store is nil")
	}
	if len(entries) == 0 {
		return nil
	}
	return store.Append(ctx, types.SessionKey{ProjectKey: projectKey, SessionID: sessionID}, entries)
}

// storeEntriesToMessages projects raw transcript entries onto session messages.
func storeEntriesToMessages(entries []types.SessionStoreEntry) []types.SessionMessage {
	out := make([]types.SessionMessage, 0, len(entries))
	for _, entry := range entries {
		entryType := entry.Type()
		if entryType != "user" && entryType != "assistant" {
			continue
		}
		if sidechain, ok := entry["isSidechain"].(bool); ok && sidechain {
			continue
		}
		if meta, ok := entry["isMeta"].(bool); ok && meta {
			continue
		}

		msg := types.SessionMessage{
			Type:    entryType,
			UUID:    entry.UUID(),
			Message: entry["message"],
			Data:    entry,
		}
		if id, ok := entry["sessionId"].(string); ok {
			msg.SessionID = id
		}
		out = append(out, msg)
	}
	return out
}

// sortSessionInfos orders sessions newest-first.
func sortSessionInfos(infos []types.SDKSessionInfo) {
	sort.SliceStable(infos, func(i, j int) bool {
		return infos[i].LastModified > infos[j].LastModified
	})
}

// ProjectKeyForDirectory returns the default SessionStore project key for a
// directory.
//
// It matches the CLI's own project-directory naming, so keys line up with the
// on-disk layout. Multi-tenant deployments should use a tenant ID instead.
func ProjectKeyForDirectory(directory string) string {
	return sessions.ProjectKeyForDirectory(directory)
}

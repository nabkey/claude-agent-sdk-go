package claude

import (
	"strings"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Keys of the opaque summary state. They are unexported strings rather than a
// struct so the state stays JSON-round-trippable through any adapter's
// storage, which is the only contract stores are held to.
const (
	summaryKeyFirstPrompt = "first_prompt"
	summaryKeyLastPrompt  = "last_prompt"
	summaryKeyCustomTitle = "custom_title"
	summaryKeyTag         = "tag"
	summaryKeyGitBranch   = "git_branch"
	summaryKeyCwd         = "cwd"
	summaryKeyCreatedAt   = "created_at"
	summaryKeyEntryCount  = "entry_count"
)

// maxSummaryPromptLength bounds the prompt text kept in a summary. Session
// listings show a one-line preview, so storing an entire prompt would bloat
// every sidecar for no benefit.
const maxSummaryPromptLength = 500

// FoldSessionSummary folds a batch of transcript entries into a session
// summary.
//
// This is a pure function: it never reads storage and never mutates prev.
// Stores call it inside Append and persist the result verbatim, then return
// the set from ListSessionSummaries. That lets a listing read all metadata in
// one round trip instead of loading every session.
//
// Pass nil for prev on a session's first batch. The returned entry's MTime is
// carried over from prev and is not set here -- stamp it after persisting, so
// it reflects storage write time rather than entry timestamps.
//
// Skip the fold for keys with a Subpath: subagent transcripts must not
// contribute to the main session's summary.
func FoldSessionSummary(prev *types.SessionSummaryEntry, key types.SessionKey, entries []types.SessionStoreEntry) types.SessionSummaryEntry {
	out := types.SessionSummaryEntry{
		SessionID: key.SessionID,
		Data:      map[string]any{},
	}
	if prev != nil {
		out.MTime = prev.MTime
		for k, v := range prev.Data {
			out.Data[k] = v
		}
		if prev.SessionID != "" {
			out.SessionID = prev.SessionID
		}
	}

	count := 0
	if v, ok := out.Data[summaryKeyEntryCount].(float64); ok {
		count = int(v)
	} else if v, ok := out.Data[summaryKeyEntryCount].(int); ok {
		count = v
	}

	for _, entry := range entries {
		count++
		foldEntry(out.Data, entry)
	}
	out.Data[summaryKeyEntryCount] = count

	return out
}

// foldEntry accumulates one transcript entry into the summary state.
func foldEntry(data map[string]any, entry types.SessionStoreEntry) {
	// Creation time comes from the first entry that carries a timestamp,
	// which is more reliable than a filesystem birth time.
	if _, seen := data[summaryKeyCreatedAt]; !seen {
		if ts := entry.Timestamp(); ts != "" {
			if t, err := time.Parse(time.RFC3339, ts); err == nil {
				data[summaryKeyCreatedAt] = t.UnixMilli()
			}
		}
	}

	switch entry.Type() {
	case "user":
		prompt := userPromptText(entry)
		if prompt == "" {
			return
		}
		if _, seen := data[summaryKeyFirstPrompt]; !seen {
			data[summaryKeyFirstPrompt] = prompt
		}
		data[summaryKeyLastPrompt] = prompt

	case "session_metadata", "summary":
		if title, ok := entry["title"].(string); ok && title != "" {
			data[summaryKeyCustomTitle] = title
		}
		// A tag is cleared by an explicit null, so presence must be checked
		// separately from the type assertion.
		if raw, present := entry["tag"]; present {
			if tag, ok := raw.(string); ok && tag != "" {
				data[summaryKeyTag] = tag
			} else {
				delete(data, summaryKeyTag)
			}
		}

	case "system":
		if branch, ok := entry["git_branch"].(string); ok && branch != "" {
			data[summaryKeyGitBranch] = branch
		}
	}

	if cwd, ok := entry["cwd"].(string); ok && cwd != "" {
		data[summaryKeyCwd] = cwd
	}
}

// userPromptText extracts displayable text from a user transcript entry.
//
// Content is either a plain string or a list of blocks; tool results are
// skipped so a session's summary is the user's actual words rather than tool
// plumbing.
func userPromptText(entry types.SessionStoreEntry) string {
	message, ok := entry["message"].(map[string]any)
	if !ok {
		return ""
	}

	switch content := message["content"].(type) {
	case string:
		return truncatePrompt(content)
	case []any:
		var parts []string
		for _, item := range content {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, _ := block["type"].(string); t != "text" {
				continue
			}
			if text, ok := block["text"].(string); ok && text != "" {
				parts = append(parts, text)
			}
		}
		return truncatePrompt(strings.Join(parts, "\n"))
	}
	return ""
}

// truncatePrompt trims and bounds prompt text for storage in a summary.
func truncatePrompt(s string) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxSummaryPromptLength {
		return s
	}
	return s[:maxSummaryPromptLength]
}

// SessionInfoFromSummary renders a stored summary as session metadata.
func SessionInfoFromSummary(entry types.SessionSummaryEntry) types.SDKSessionInfo {
	info := types.SDKSessionInfo{
		SessionID:    entry.SessionID,
		LastModified: entry.MTime,
	}

	if v, ok := entry.Data[summaryKeyFirstPrompt].(string); ok {
		info.FirstPrompt = v
	}
	if v, ok := entry.Data[summaryKeyCustomTitle].(string); ok {
		info.CustomTitle = &v
	}
	if v, ok := entry.Data[summaryKeyTag].(string); ok {
		info.Tag = &v
	}
	if v, ok := entry.Data[summaryKeyGitBranch].(string); ok {
		info.GitBranch = &v
	}
	if v, ok := entry.Data[summaryKeyCwd].(string); ok {
		info.CWD = v
	}
	if v, ok := entry.Data[summaryKeyCreatedAt].(float64); ok {
		created := time.UnixMilli(int64(v))
		info.CreatedAt = &created
	}

	// Summary is the display title: a custom title wins, else the first
	// prompt, matching how the CLI labels a session.
	info.Summary = info.FirstPrompt
	if info.CustomTitle != nil && *info.CustomTitle != "" {
		info.Summary = *info.CustomTitle
	}

	return info
}

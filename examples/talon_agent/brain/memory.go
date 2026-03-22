package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

var (
	memoryPath  = filepath.Join(agentDir, "MEMORY.md")
	conversDir  = filepath.Join(agentDir, "conversations")
)

func handleReadMemory(w http.ResponseWriter, r *http.Request) {
	data, err := os.ReadFile(memoryPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]string{"content": "# LONG TERM\n\n# SHORT TERM\n"})
			return
		}
		writeError(w, 500, "failed to read memory: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"content": string(data)})
}

func handleUpdateMemory(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Section string `json:"section"` // "long_term" or "short_term"
		Content string `json:"content"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}

	var header string
	switch req.Section {
	case "long_term":
		header = "# LONG TERM"
	case "short_term":
		header = "# SHORT TERM"
	default:
		writeError(w, 400, "section must be 'long_term' or 'short_term'")
		return
	}

	existing, _ := os.ReadFile(memoryPath)
	content := string(existing)
	if content == "" {
		content = "# LONG TERM\n\n# SHORT TERM\n"
	}

	// Find the section and replace its content
	idx := strings.Index(content, header)
	if idx == -1 {
		// Section doesn't exist, append it
		content += "\n" + header + "\n" + req.Content + "\n"
	} else {
		// Find the end of this section (next # header or EOF)
		afterHeader := idx + len(header)
		nextSection := -1
		rest := content[afterHeader:]
		for i := 1; i < len(rest); i++ {
			if rest[i] == '#' && (i == 0 || rest[i-1] == '\n') {
				nextSection = afterHeader + i
				break
			}
		}
		if nextSection == -1 {
			content = content[:afterHeader] + "\n" + req.Content + "\n"
		} else {
			content = content[:afterHeader] + "\n" + req.Content + "\n\n" + content[nextSection:]
		}
	}

	if err := os.WriteFile(memoryPath, []byte(content), 0644); err != nil {
		writeError(w, 500, "failed to write memory: "+err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func handleSaveConversation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Sender  string `json:"sender"`
		Message string `json:"message"`
		Reply   string `json:"reply"`
	}
	if err := decodeBody(r, &req); err != nil {
		writeError(w, 400, "invalid request: "+err.Error())
		return
	}

	now := time.Now()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")

	dayDir := filepath.Join(conversDir, dateStr)
	if err := os.MkdirAll(dayDir, 0755); err != nil {
		writeError(w, 500, "failed to create conversation directory: "+err.Error())
		return
	}

	convoPath := filepath.Join(dayDir, "convo.md")
	entry := fmt.Sprintf("## %s\n**From:** %s\n**Message:** %s\n**Reply:** %s\n\n",
		timeStr, req.Sender, req.Message, req.Reply)

	f, err := os.OpenFile(convoPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		writeError(w, 500, "failed to open conversation log: "+err.Error())
		return
	}
	defer f.Close()

	if _, err := f.WriteString(entry); err != nil {
		writeError(w, 500, "failed to write conversation: "+err.Error())
		return
	}

	writeJSON(w, map[string]bool{"ok": true})
}

func handleGetConversationSummary(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Date string `json:"date"` // optional, defaults to today
	}
	// Ignore decode errors for empty body
	_ = decodeBody(r, &req)

	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}

	summaryPath := filepath.Join(conversDir, req.Date, "summary.md")
	data, err := os.ReadFile(summaryPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]string{"summary": ""})
			return
		}
		writeError(w, 500, "failed to read summary: "+err.Error())
		return
	}
	writeJSON(w, map[string]string{"summary": string(data)})
}

func handleUpdateSummary(w http.ResponseWriter, r *http.Request) {
	// Returns un-summarized conversation entries so the caller can summarize them.
	// Also accepts an optional new summary to write back.
	var req struct {
		Date    string `json:"date"`    // optional, defaults to today
		Summary string `json:"summary"` // if provided, writes the summary
	}
	_ = decodeBody(r, &req)

	if req.Date == "" {
		req.Date = time.Now().Format("2006-01-02")
	}

	dayDir := filepath.Join(conversDir, req.Date)
	convoPath := filepath.Join(dayDir, "convo.md")
	summaryPath := filepath.Join(dayDir, "summary.md")

	// If a summary is provided, write it
	if req.Summary != "" {
		if err := os.MkdirAll(dayDir, 0755); err != nil {
			writeError(w, 500, "failed to create directory: "+err.Error())
			return
		}
		// Prepend the last_summarized marker
		marker := fmt.Sprintf("<!-- last_summarized: %s -->\n", time.Now().Format(time.RFC3339))
		if err := os.WriteFile(summaryPath, []byte(marker+req.Summary), 0644); err != nil {
			writeError(w, 500, "failed to write summary: "+err.Error())
			return
		}
		writeJSON(w, map[string]bool{"ok": true})
		return
	}

	// Otherwise, return un-summarized entries
	convoData, err := os.ReadFile(convoPath)
	if err != nil {
		if os.IsNotExist(err) {
			writeJSON(w, map[string]any{"new_entries": "", "count": 0})
			return
		}
		writeError(w, 500, "failed to read conversation log: "+err.Error())
		return
	}

	// Check where the summary left off
	lastSummarized := ""
	summaryData, err := os.ReadFile(summaryPath)
	if err == nil {
		content := string(summaryData)
		if start := strings.Index(content, "<!-- last_summarized: "); start != -1 {
			end := strings.Index(content[start:], " -->")
			if end != -1 {
				lastSummarized = content[start+len("<!-- last_summarized: ") : start+end]
			}
		}
	}

	// Find entries after lastSummarized time
	convo := string(convoData)
	newEntries := convo
	entryCount := strings.Count(convo, "## ")

	if lastSummarized != "" {
		// Parse the time and find entries after it
		lastTime, err := time.Parse(time.RFC3339, lastSummarized)
		if err == nil {
			// Split by entries and filter
			parts := strings.Split(convo, "## ")
			var filtered []string
			for _, part := range parts {
				if part == "" {
					continue
				}
				// Extract time from entry header (first line is HH:MM:SS)
				lines := strings.SplitN(part, "\n", 2)
				if len(lines) == 0 {
					continue
				}
				entryTimeStr := strings.TrimSpace(lines[0])
				entryTime, err := time.Parse("15:04:05",
					entryTimeStr)
				if err != nil {
					continue
				}
				// Compare using just time of day (same date)
				entryFull := time.Date(lastTime.Year(), lastTime.Month(), lastTime.Day(),
					entryTime.Hour(), entryTime.Minute(), entryTime.Second(), 0, lastTime.Location())
				if entryFull.After(lastTime) {
					filtered = append(filtered, "## "+part)
				}
			}
			newEntries = strings.Join(filtered, "")
			entryCount = len(filtered)
		}
	}

	writeJSON(w, map[string]any{
		"new_entries": newEntries,
		"count":       entryCount,
	})
}

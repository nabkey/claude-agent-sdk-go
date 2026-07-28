package main

import (
	"context"
	"strings"
	"testing"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

func TestTruncateRunes(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"under the limit", "hello", 10, "hello"},
		{"exactly the limit", "hello", 5, "hello"},
		{"over the limit", "hello world", 8, "hello w…"},
		{"zero", "hello", 0, ""},
		// Clipping by bytes would split these into replacement characters.
		{"multi-byte is not split", "日本語テキスト", 4, "日本語…"},
		{"emoji survive", "🔧🔧🔧🔧", 3, "🔧🔧…"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateRunes(tc.in, tc.n)
			if got != tc.want {
				t.Errorf("truncateRunes(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
			}
			if runes := []rune(got); len(runes) > tc.n {
				t.Errorf("result is %d runes, over the %d limit", len(runes), tc.n)
			}
		})
	}
}

func TestSplitMessageShortStaysWhole(t *testing.T) {
	chunks := splitMessage("just a short reply")
	if len(chunks) != 1 || chunks[0] != "just a short reply" {
		t.Fatalf("short message was split: %q", chunks)
	}
}

// Every chunk has to be independently sendable, or the overflow path trades
// one rejected message for several.
func TestSplitMessageChunksAreSendable(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 500; i++ {
		sb.WriteString("This is a paragraph of output from the agent.\n\n")
	}
	long := sb.String()

	chunks := splitMessage(long)
	if len(chunks) < 2 {
		t.Fatalf("expected the long message to split, got %d chunk(s)", len(chunks))
	}
	for i, chunk := range chunks {
		if n := len([]rune(chunk)); n > maxMessageRunes {
			t.Errorf("chunk %d is %d runes, over Telegram's %d limit", i, n, maxMessageRunes)
		}
		if strings.TrimSpace(chunk) == "" {
			t.Errorf("chunk %d is blank, which Telegram rejects", i)
		}
	}
}

// A wall of text with no newlines still has to be split somewhere.
func TestSplitMessageHandlesNoBoundaries(t *testing.T) {
	long := strings.Repeat("x", chunkRunes*3)

	chunks := splitMessage(long)
	if len(chunks) < 3 {
		t.Fatalf("expected at least 3 chunks, got %d", len(chunks))
	}

	var total int
	for i, chunk := range chunks {
		n := len([]rune(chunk))
		if n > maxMessageRunes {
			t.Errorf("chunk %d is %d runes, over the limit", i, n)
		}
		total += n
	}
	// Nothing may be dropped: this input has no whitespace to trim away.
	if total != chunkRunes*3 {
		t.Errorf("chunks total %d runes, want %d — content was lost", total, chunkRunes*3)
	}
}

// Editing a Telegram message to its current text is an API error, so the
// flusher must skip a no-op edit rather than spend a call discovering that.
func TestLiveMessageSkipsRedundantEdits(t *testing.T) {
	m := newMockAPI(t)
	ctx := context.Background()

	api := NewAPI("test-token")
	api.BaseURL = m.server.URL

	lm, err := newLiveMessage(ctx, api, 7, "thinking…")
	if err != nil {
		t.Fatalf("open live message: %v", err)
	}

	// Same text as the placeholder: no edit should be issued.
	lm.update(ctx, "thinking…")
	lm.flush(ctx)
	if edits := m.editRecords(); len(edits) != 0 {
		t.Fatalf("a redundant edit was sent: %+v", edits)
	}

	lm.update(ctx, "actual answer")
	lm.flush(ctx)
	if edits := m.editRecords(); len(edits) != 1 {
		t.Fatalf("expected one edit, got %d", len(m.editRecords()))
	}

	// finish with unchanged text must not double-send either.
	lm.finish(ctx, "actual answer")
	if edits := m.editRecords(); len(edits) != 1 {
		t.Errorf("finish re-sent an unchanged edit: %+v", edits)
	}
}

func TestLiveMessageFinishWritesFinalText(t *testing.T) {
	m := newMockAPI(t)
	ctx := context.Background()

	api := NewAPI("test-token")
	api.BaseURL = m.server.URL

	lm, err := newLiveMessage(ctx, api, 7, "thinking…")
	if err != nil {
		t.Fatalf("open live message: %v", err)
	}
	lm.finish(ctx, "done")

	edits := m.editRecords()
	if len(edits) != 1 || edits[0].Text != "done" {
		t.Fatalf("final text was not written: %+v", edits)
	}
}

func TestDescribeToolUseIsOneLine(t *testing.T) {
	tests := []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "echo hi"}, "🔧 Bash: echo hi"},
		{"Read", map[string]any{"file_path": "/tmp/x.go"}, "🔧 Read: /tmp/x.go"},
		{"Grep", map[string]any{"pattern": "func main"}, "🔧 Grep: func main"},
		{"Unknown", map[string]any{"x": 1}, "🔧 Unknown"},
		// A multi-line command must collapse, or one tool call eats the view.
		{"Bash", map[string]any{"command": "a\n\nb\n  c"}, "🔧 Bash: a b c"},
	}

	for _, tc := range tests {
		got := describeToolUse(&types.ToolUseBlock{Name: tc.tool, Input: tc.input})
		if got != tc.want {
			t.Errorf("describeToolUse(%s) = %q, want %q", tc.tool, got, tc.want)
		}
		if strings.Contains(got, "\n") {
			t.Errorf("describeToolUse(%s) returned multiple lines: %q", tc.tool, got)
		}
	}
}

func TestParsePermissionMode(t *testing.T) {
	for _, in := range []string{"default", "acceptEdits", "plan", "bypassPermissions", "BYPASS"} {
		if _, ok := parsePermissionMode(in); !ok {
			t.Errorf("parsePermissionMode(%q) should be recognized", in)
		}
	}
	if _, ok := parsePermissionMode("yolo"); ok {
		t.Error("parsePermissionMode should reject an unknown mode")
	}
}

func TestParseUserIDs(t *testing.T) {
	ids, err := parseUserIDs(" 123, 456 ")
	if err != nil {
		t.Fatalf("parseUserIDs: %v", err)
	}
	if len(ids) != 2 || ids[0] != 123 || ids[1] != 456 {
		t.Errorf("ids = %v, want [123 456]", ids)
	}

	if _, err := parseUserIDs(""); err == nil {
		t.Error("an empty allowlist must be rejected")
	}
	if _, err := parseUserIDs("nope"); err == nil {
		t.Error("a non-numeric ID must be rejected")
	}
}

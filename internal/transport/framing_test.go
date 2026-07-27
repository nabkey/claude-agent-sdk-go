package transport

import (
	"bufio"
	"strings"
	"testing"
)

// The CLI writes one JSON message per line. A line that fails to parse is
// corrupt, not partial -- concatenating it onto the next line is what let a
// single stray line poison every subsequent message.
func TestParseStdoutLine(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantNil bool
		wantErr bool
		check   func(*testing.T, map[string]any)
	}{
		{
			name: "valid json",
			line: `{"type":"assistant","x":1}`,
			check: func(t *testing.T, m map[string]any) {
				if m["type"] != "assistant" {
					t.Errorf("type = %v", m["type"])
				}
			},
		},
		{name: "blank line", line: "", wantNil: true},
		{name: "whitespace only", line: "   \t  ", wantNil: true},
		{name: "trailing CR is trimmed", line: "{\"type\":\"x\"}\r", check: func(t *testing.T, m map[string]any) {
			if m["type"] != "x" {
				t.Errorf("type = %v", m["type"])
			}
		}},

		// Some CLI builds write diagnostics to stdout. These carry no message
		// and must be skipped rather than treated as corrupt.
		{name: "sandbox debug line", line: "[SandboxDebug] denied /etc/passwd", wantNil: true},
		{name: "plain log line", line: "warning: something happened", wantNil: true},

		// A line that looks like JSON but does not parse is a real problem.
		{name: "truncated json", line: `{"type":"assistant"`, wantErr: true},
		{name: "malformed json", line: `{bad}`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseStdoutLine(tc.line, 1024)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error for %q", tc.line)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantNil {
				if got != nil {
					t.Errorf("expected no message, got %v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected a message")
			}
			if tc.check != nil {
				tc.check(t, got)
			}
		})
	}
}

func TestParseStdoutLineEnforcesMaxSize(t *testing.T) {
	huge := `{"text":"` + strings.Repeat("x", 200) + `"}`
	if _, err := parseStdoutLine(huge, 50); err == nil {
		t.Error("expected an oversized line to be rejected")
	}
}

// A stray non-JSON line must not corrupt the messages around it, which is the
// regression the framing rewrite exists to prevent.
func TestReadLineIsolatesBadLines(t *testing.T) {
	input := strings.Join([]string{
		`{"type":"a"}`,
		`[SandboxDebug] noise`,
		`{"type":"b"}`,
		`{"type":"c"}`,
	}, "\n") + "\n"

	reader := bufio.NewReader(strings.NewReader(input))

	var types []string
	for {
		line, err := readLine(reader, 4096)
		if line != "" {
			msg, parseErr := parseStdoutLine(line, 4096)
			if parseErr != nil {
				t.Fatalf("unexpected parse error on %q: %v", line, parseErr)
			}
			if msg != nil {
				msgType, _ := msg["type"].(string)
				types = append(types, msgType)
			}
		}
		if err != nil {
			break
		}
	}

	want := []string{"a", "b", "c"}
	if len(types) != len(want) {
		t.Fatalf("got %v, want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("message %d = %q, want %q", i, types[i], want[i])
		}
	}
}

// A line longer than the cap must be rejected rather than growing without
// bound, so a producer that never emits a newline cannot exhaust memory.
func TestReadLineBoundsLineLength(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(strings.Repeat("x", 10000)))

	if _, err := readLine(reader, 100); err == nil {
		t.Error("expected an unbounded line to be rejected")
	}
}

func TestReadLineHandlesFinalLineWithoutNewline(t *testing.T) {
	reader := bufio.NewReader(strings.NewReader(`{"type":"a"}`))

	line, err := readLine(reader, 4096)
	if line != `{"type":"a"}` {
		t.Errorf("line = %q", line)
	}
	// EOF is expected here; the line is still complete and usable.
	if err == nil {
		t.Error("expected EOF on the final unterminated line")
	}
}

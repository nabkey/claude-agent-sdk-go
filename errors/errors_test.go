package errors

import (
	stderrors "errors"
	"strings"
	"testing"
)

// A ResultError has to carry enough of the CLI's payload that callers can
// branch on why a run failed without matching on strings.
func TestNewResultErrorCarriesPayload(t *testing.T) {
	err := NewResultError("claude code returned an error result: overloaded",
		map[string]any{
			"subtype":          "error_during_execution",
			"errors":           []any{"overloaded", 42},
			"result":           "API Error: overloaded",
			"api_error_status": float64(529),
			"terminal_reason":  "api_error",
			"session_id":       "s-1",
		}, 1, "")

	if err.Subtype != "error_during_execution" {
		t.Errorf("Subtype = %q", err.Subtype)
	}
	// The non-string entry is dropped rather than failing the decode.
	if len(err.Errors) != 1 || err.Errors[0] != "overloaded" {
		t.Errorf("Errors = %v, want only the string entry", err.Errors)
	}
	if err.Result != "API Error: overloaded" {
		t.Errorf("Result = %q", err.Result)
	}
	if err.APIErrorStatus == nil || *err.APIErrorStatus != 529 {
		t.Errorf("APIErrorStatus = %v, want 529", err.APIErrorStatus)
	}
	if err.TerminalReason != "api_error" || err.SessionID != "s-1" {
		t.Errorf("unexpected terminal fields: %+v", err)
	}
	if err.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", err.ExitCode)
	}
	if err.Data == nil {
		t.Error("expected the raw payload to be retained")
	}
	if !strings.Contains(err.Error(), "overloaded") {
		t.Errorf("Error() = %q, want the reported text", err.Error())
	}
}

// Existing handlers written against ProcessError must keep matching.
func TestResultErrorMatchesProcessError(t *testing.T) {
	var err error = NewResultError("boom", map[string]any{"subtype": "error_max_turns"}, 1, "")

	var processErr *ProcessError
	if !stderrors.As(err, &processErr) {
		t.Fatal("a ResultError must match *ProcessError")
	}
	if processErr.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", processErr.ExitCode)
	}
}

// A payload the CLI did not send anything useful in must still produce a
// usable error rather than panicking.
func TestNewResultErrorTolerantOfMissingFields(t *testing.T) {
	err := NewResultError("boom", nil, 0, "stderr text")
	if err.Subtype != "" || err.APIErrorStatus != nil || err.Errors != nil {
		t.Errorf("expected zero values, got %+v", err)
	}
	if !strings.Contains(err.Error(), "stderr text") {
		t.Errorf("Error() = %q, want the stderr text", err.Error())
	}
}

// Fields present with the wrong type are ignored, so a CLI change cannot turn
// a failed run into a panic.
func TestNewResultErrorIgnoresWrongTypes(t *testing.T) {
	err := NewResultError("boom", map[string]any{
		"subtype":          42,
		"errors":           "not a list",
		"api_error_status": "529",
	}, 1, "")

	if err.Subtype != "" || err.Errors != nil || err.APIErrorStatus != nil {
		t.Errorf("expected wrongly-typed fields to be skipped, got %+v", err)
	}
}

func TestProcessErrorMessageIncludesExitCodeAndStderr(t *testing.T) {
	err := NewProcessError("Command failed", 2, "bad flag")
	if !strings.Contains(err.Error(), "exit code: 2") {
		t.Errorf("Error() = %q, want the exit code", err.Error())
	}
	if !strings.Contains(err.Error(), "bad flag") {
		t.Errorf("Error() = %q, want the stderr text", err.Error())
	}
}

func TestCLINotFoundErrorIncludesPath(t *testing.T) {
	err := NewCLINotFoundError("Claude Code not found", "/nowhere/claude")
	if !strings.Contains(err.Error(), "/nowhere/claude") {
		t.Errorf("Error() = %q, want the searched path", err.Error())
	}
	if err.CLIPath != "/nowhere/claude" {
		t.Errorf("CLIPath = %q", err.CLIPath)
	}

	var connErr *CLIConnectionError
	if !stderrors.As(error(err), &connErr) {
		t.Error("a CLINotFoundError must match *CLIConnectionError")
	}
}

// A decode error quotes the offending line, which can be a whole transcript
// frame, so it is truncated.
func TestCLIJSONDecodeErrorTruncatesLongLines(t *testing.T) {
	line := strings.Repeat("x", 500)
	err := NewCLIJSONDecodeError(line, stderrors.New("unexpected token"))

	if len(err.Error()) > 200 {
		t.Errorf("message is %d chars, want it truncated", len(err.Error()))
	}
	if !strings.Contains(err.Error(), "...") {
		t.Errorf("Error() = %q, want a truncation marker", err.Error())
	}
	// The full line stays available for callers that want it.
	if err.Line != line {
		t.Error("expected the untruncated line to be retained")
	}
	if !stderrors.Is(err, err.Cause) {
		t.Error("expected the decode cause to unwrap")
	}
}

func TestErrorsUnwrapToTheirCause(t *testing.T) {
	cause := stderrors.New("root cause")
	err := NewCLIConnectionError("could not connect", cause)

	if !stderrors.Is(err, cause) {
		t.Error("expected the cause to unwrap")
	}
	if !strings.Contains(err.Error(), "root cause") {
		t.Errorf("Error() = %q, want the cause included", err.Error())
	}
}

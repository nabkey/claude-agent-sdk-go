// Package errors provides error types for the Claude Agent SDK.
package errors

import (
	"errors"
	"fmt"
)

// ClaudeSDKError is the base error type for all Claude SDK errors.
type ClaudeSDKError struct {
	Message string
	Cause   error
}

func (e *ClaudeSDKError) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("%s: %v", e.Message, e.Cause)
	}
	return e.Message
}

func (e *ClaudeSDKError) Unwrap() error {
	return e.Cause
}

// CLIConnectionError is raised when unable to connect to Claude Code.
type CLIConnectionError struct {
	ClaudeSDKError
}

// NewCLIConnectionError creates a new CLIConnectionError.
func NewCLIConnectionError(message string, cause error) *CLIConnectionError {
	return &CLIConnectionError{
		ClaudeSDKError: ClaudeSDKError{
			Message: message,
			Cause:   cause,
		},
	}
}

// CLINotFoundError is raised when Claude Code is not found or not installed.
type CLINotFoundError struct {
	CLIConnectionError
	CLIPath string
}

// Unwrap exposes the embedded CLIConnectionError, so errors.As against
// *CLIConnectionError matches a CLINotFoundError. Go matches concrete types
// rather than embedding, so without this the hierarchy would be decorative.
func (e *CLINotFoundError) Unwrap() error { return &e.CLIConnectionError }

// NewCLINotFoundError creates a new CLINotFoundError.
func NewCLINotFoundError(message string, cliPath string) *CLINotFoundError {
	if cliPath != "" {
		message = fmt.Sprintf("%s: %s", message, cliPath)
	}
	return &CLINotFoundError{
		CLIConnectionError: CLIConnectionError{
			ClaudeSDKError: ClaudeSDKError{
				Message: message,
			},
		},
		CLIPath: cliPath,
	}
}

// ProcessError is raised when the CLI process fails.
type ProcessError struct {
	ClaudeSDKError
	ExitCode int
	Stderr   string
}

// NewProcessError creates a new ProcessError.
func NewProcessError(message string, exitCode int, stderr string) *ProcessError {
	fullMessage := message
	if exitCode != 0 {
		fullMessage = fmt.Sprintf("%s (exit code: %d)", message, exitCode)
	}
	if stderr != "" {
		fullMessage = fmt.Sprintf("%s\nError output: %s", fullMessage, stderr)
	}
	return &ProcessError{
		ClaudeSDKError: ClaudeSDKError{
			Message: fullMessage,
		},
		ExitCode: exitCode,
		Stderr:   stderr,
	}
}

// ResultError is returned when the CLI exits after reporting a terminal error
// result.
//
// The CLI ends a failed run by emitting a result message with is_error set --
// which the consumer also receives as a types.ResultMessage -- and then
// exiting non-zero. This replaces the bare "exit code 1" ProcessError for that
// case and carries the result's payload, so callers can branch on why the run
// failed without matching on strings:
//
//	var resultErr *errors.ResultError
//	if errors.As(err, &resultErr) {
//	    switch resultErr.TerminalReason {
//	    case "api_error":
//	        retry()
//	    case "max_turns":
//	        ...
//	    }
//	}
//
// It embeds ProcessError, so errors.As against *ProcessError keeps matching.
type ResultError struct {
	ProcessError
	// Subtype is the result subtype: "error_max_turns",
	// "error_during_execution", "error_max_budget_usd",
	// "error_max_structured_output_retries", or "success" when the agent loop
	// completed but the last turn was an API error.
	Subtype string
	// Errors are the error strings the CLI reported. May be empty.
	Errors []string
	// Result is the result text, if any. For API failures this holds the
	// "API Error: ..." prose.
	Result string
	// APIErrorStatus is the HTTP status of the failing API call, if any.
	APIErrorStatus *int
	// TerminalReason is why the run ended, e.g. "api_error" or "max_turns".
	// Empty when the CLI did not report one.
	TerminalReason string
	// SessionID is the session the result belongs to, if reported.
	SessionID string
	// Data is the raw result payload as the CLI emitted it.
	Data map[string]any
}

// Unwrap exposes the embedded ProcessError, so errors.As against
// *ProcessError matches a ResultError -- the relationship Python's SDK gets
// from subclassing. From there the chain continues to whatever caused the
// process to exit.
func (e *ResultError) Unwrap() error { return &e.ProcessError }

// NewResultError builds a ResultError from a raw result frame.
//
// Fields absent from the payload, or present with an unexpected type, are left
// at their zero value rather than failing: the point is to carry whatever the
// CLI did report.
func NewResultError(message string, data map[string]any, exitCode int, stderr string) *ResultError {
	err := &ResultError{
		ProcessError: *NewProcessError(message, exitCode, stderr),
		Data:         data,
	}
	if data == nil {
		return err
	}
	err.Subtype, _ = data["subtype"].(string)
	err.Result, _ = data["result"].(string)
	err.TerminalReason, _ = data["terminal_reason"].(string)
	err.SessionID, _ = data["session_id"].(string)
	if status, ok := data["api_error_status"].(float64); ok {
		s := int(status)
		err.APIErrorStatus = &s
	}
	if items, ok := data["errors"].([]any); ok {
		for _, item := range items {
			if s, ok := item.(string); ok {
				err.Errors = append(err.Errors, s)
			}
		}
	}
	return err
}

// CLIJSONDecodeError is raised when unable to decode JSON from CLI output.
type CLIJSONDecodeError struct {
	ClaudeSDKError
	Line string
}

// NewCLIJSONDecodeError creates a new CLIJSONDecodeError.
func NewCLIJSONDecodeError(line string, originalError error) *CLIJSONDecodeError {
	truncatedLine := line
	if len(line) > 100 {
		truncatedLine = line[:100] + "..."
	}
	return &CLIJSONDecodeError{
		ClaudeSDKError: ClaudeSDKError{
			Message: fmt.Sprintf("Failed to decode JSON: %s", truncatedLine),
			Cause:   originalError,
		},
		Line: line,
	}
}

// MessageParseError is raised when unable to parse a message from CLI output.
type MessageParseError struct {
	ClaudeSDKError
	Data map[string]any
}

// NewMessageParseError creates a new MessageParseError.
func NewMessageParseError(message string, data map[string]any) *MessageParseError {
	return &MessageParseError{
		ClaudeSDKError: ClaudeSDKError{
			Message: message,
		},
		Data: data,
	}
}

// ControlRequestError is raised when a control request fails.
type ControlRequestError struct {
	ClaudeSDKError
	RequestType string
}

// NewControlRequestError creates a new ControlRequestError.
func NewControlRequestError(message string, requestType string) *ControlRequestError {
	return &ControlRequestError{
		ClaudeSDKError: ClaudeSDKError{
			Message: message,
		},
		RequestType: requestType,
	}
}

// TimeoutError is raised when an operation times out.
type TimeoutError struct {
	ClaudeSDKError
	Operation string
}

// NewTimeoutError creates a new TimeoutError.
func NewTimeoutError(operation string) *TimeoutError {
	return &TimeoutError{
		ClaudeSDKError: ClaudeSDKError{
			Message: fmt.Sprintf("Operation timed out: %s", operation),
		},
		Operation: operation,
	}
}

// Helper functions for error type checking using errors.As

// Is checks if the target error is of the specified type.
func Is(err, target error) bool {
	return errors.Is(err, target)
}

// As finds the first error in err's chain that matches target.
func As(err error, target any) bool {
	return errors.As(err, target)
}

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

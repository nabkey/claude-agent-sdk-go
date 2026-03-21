# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `PersistSession` option to control session persistence
- `AgentProgressSummaries` option for AI-generated subagent progress summaries
- `ToolConfig` option for fine-grained per-tool configuration
- `GetSessionInfo()` function for retrieving single session metadata
- `ForkSession()` standalone function for branching conversations
- `SupportedAgents()` function for discovering available agents at runtime
- GitHub Actions CI/CD pipeline with test, vet, race detection, and linting
- golangci-lint configuration
- Rate limit overage fields on `RateLimitInfo`: `OverageStatus`, `OverageResetsAt`, `OverageDisabledReason`, `Raw`
- `RateLimitType` enum values: `seven_day_opus`, `seven_day_sonnet`, `overage`
- `McpServerConnectionStatus` enum values: `needs-auth`, `disabled`
- `McpServerInfo` type and `ServerInfo`/`Scope` fields on `McpServerStatus`
- `SessionID`, `ToolUseID`, `TaskType` fields on `TaskStartedMessage`
- `Description`, `UUID`, `SessionID`, `ToolUseID`, `LastToolName` fields on `TaskProgressMessage`
- `OutputFile`, `Summary`, `UUID`, `SessionID`, `Usage` fields on `TaskNotificationMessage`
- Rate limit event parser support for both flat and nested (`rate_limit_info`) wire formats with camelCase/snake_case compatibility

## [0.1.0] - 2025-05-01

### Added
- `Query()`, `QuerySync()`, `QueryText()` for one-shot queries
- `Client` for bidirectional, interactive streaming conversations
- MCP server support (SDK in-process, stdio, HTTP, SSE)
- Hook system for intercepting tool use, permissions, and lifecycle events
- `CanUseTool` callback for custom permission logic
- Session management: list, read, rename, tag sessions
- Extended thinking configuration (adaptive, enabled, disabled)
- Effort level configuration
- Structured output via JSON schema
- File checkpointing and rewind support
- Custom agent definitions
- Plugin support
- Sandbox settings for bash command isolation
- Rate limit event handling

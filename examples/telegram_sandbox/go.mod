module github.com/nabkey/claude-agent-sdk-go/examples/telegram_sandbox

go 1.24

require (
	github.com/nabkey/claude-agent-sdk-go v0.0.0
	github.com/nabkey/claude-agent-sdk-go/examples/sandbox v0.0.0
)

require (
	github.com/google/uuid v1.6.0 // indirect
	golang.org/x/text v0.21.0 // indirect
)

replace github.com/nabkey/claude-agent-sdk-go => ../..

replace github.com/nabkey/claude-agent-sdk-go/examples/sandbox => ../sandbox

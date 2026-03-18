// Example: Session management - listing and reading session history
package main

import (
	"fmt"
	"log"

	"github.com/nabkey/claude-agent-sdk-go/sessions"
)

func main() {
	// List all sessions in the current directory
	sessionList, err := sessions.ListSessions(nil)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Found %d sessions\n", len(sessionList))
	for _, s := range sessionList {
		fmt.Printf("  Session: %s\n", s.SessionID)
		fmt.Printf("  First prompt: %s\n", s.FirstPrompt)
		fmt.Printf("  Last prompt: %s\n", s.LastPrompt)
		if s.GitBranch != nil {
			fmt.Printf("  Branch: %s\n", *s.GitBranch)
		}
		if s.Tag != nil {
			fmt.Printf("  Tag: %s\n", *s.Tag)
		}
		fmt.Println()
	}

	// Read messages from the first session (if any)
	if len(sessionList) > 0 {
		messages, err := sessions.GetSessionMessages(sessionList[0].SessionID, nil)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("Session %s has %d messages\n", sessionList[0].SessionID, len(messages))

		// Tag the session
		tag := "example"
		if err := sessions.TagSession(sessionList[0].SessionID, &tag, nil); err != nil {
			log.Printf("Failed to tag session: %v\n", err)
		}

		// Rename the session
		if err := sessions.RenameSession(sessionList[0].SessionID, "Example Session", nil); err != nil {
			log.Printf("Failed to rename session: %v\n", err)
		}
	}
}

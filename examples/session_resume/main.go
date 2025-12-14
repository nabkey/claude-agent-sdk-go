// Example: Session Resume
//
// This example demonstrates how to:
// 1. Continue the most recent conversation (ContinueConversation)
// 2. Resume a specific session by ID (Resume)
//
// Session IDs are returned in the ResultMessage and can be stored
// for later resumption.
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	// First, create a new session and capture the session ID
	sessionID := createInitialSession(ctx)

	fmt.Println()

	// Now demonstrate resuming that session
	resumeSession(ctx, sessionID)

	fmt.Println()

	// Demonstrate continue conversation (continues most recent)
	continueConversation(ctx)
}

// createInitialSession creates a new session and returns its ID.
func createInitialSession(ctx context.Context) string {
	fmt.Println("============================================================")
	fmt.Println("Step 1: Creating Initial Session")
	fmt.Println("============================================================")
	fmt.Println()

	options := &claude.AgentOptions{
		MaxTurns: claude.Int(1),
	}

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	prompt := "Remember this number: 42. It's the answer to everything."
	fmt.Printf("User: %s\n\n", prompt)

	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	var sessionID string

	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			sessionID = m.SessionID
			fmt.Printf("\n[Session ID: %s]\n", sessionID)
		}
	}

	return sessionID
}

// resumeSession resumes a specific session by ID.
func resumeSession(ctx context.Context, sessionID string) {
	fmt.Println("============================================================")
	fmt.Println("Step 2: Resuming Session by ID")
	fmt.Println("============================================================")
	fmt.Printf("Resuming session: %s\n\n", sessionID)

	options := &claude.AgentOptions{
		Resume:   claude.String(sessionID),
		MaxTurns: claude.Int(1),
	}

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	prompt := "What number did I ask you to remember?"
	fmt.Printf("User: %s\n\n", prompt)

	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			fmt.Printf("\n[Session ID: %s]\n", m.SessionID)
		}
	}
}

// continueConversation continues the most recent conversation.
func continueConversation(ctx context.Context) {
	fmt.Println("============================================================")
	fmt.Println("Step 3: Continue Most Recent Conversation")
	fmt.Println("============================================================")
	fmt.Println("Using ContinueConversation flag to continue the most recent session")
	fmt.Println()

	options := &claude.AgentOptions{
		ContinueConversation: true,
		MaxTurns:             claude.Int(1),
	}

	client, err := claude.NewClient(ctx, options)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()

	if err := client.Connect(ctx, ""); err != nil {
		log.Fatal(err)
	}

	prompt := "Can you remind me what we were talking about?"
	fmt.Printf("User: %s\n\n", prompt)

	if err := client.SendQuery(ctx, prompt); err != nil {
		log.Fatal(err)
	}

	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				if text, ok := block.(*types.TextBlock); ok {
					fmt.Printf("Claude: %s\n", text.Text)
				}
			}
		case *types.ResultMessage:
			fmt.Printf("\n[Session ID: %s]\n", m.SessionID)
		}
	}
}

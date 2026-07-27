// Example: mirroring session transcripts to an external store.
//
// A SessionStore receives a copy of every transcript line the CLI writes, so a
// multi-tenant or serverless deployment can resume a session that was never on
// this machine's disk. This example uses the built-in in-memory store; a real
// deployment would back it with S3, Postgres, or Redis.
package main

import (
	"context"
	"fmt"
	"log"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

func main() {
	ctx := context.Background()

	store := claude.NewInMemorySessionStore()

	options := claude.DefaultAgentOptions().
		WithSessionStore(store).
		WithMaxTurns(1)

	// Transcript lines are mirrored to the store as the conversation runs.
	for msg := range claude.Query(ctx, "Say hello in one word.", options) {
		if result, ok := msg.(*types.ResultMessage); ok {
			fmt.Printf("session: %s\n", result.SessionID)
		}
	}

	// The store can now be listed and read back without touching local disk.
	projectKey := claude.ProjectKeyForDirectory(".")

	sessions, err := claude.ListSessionsFromStore(ctx, store, projectKey)
	if err != nil {
		log.Fatal(err)
	}

	for _, info := range sessions {
		fmt.Printf("  %s  %s\n", info.SessionID, info.Summary)

		messages, err := claude.GetSessionMessagesFromStore(ctx, store, projectKey, info.SessionID)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  %d messages\n", len(messages))
	}
}

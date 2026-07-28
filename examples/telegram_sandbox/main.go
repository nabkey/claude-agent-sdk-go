// Command telegram_sandbox drives a Claude Code session from Telegram, with
// the agent running inside a sandbox reached over a socket.
//
// The bot process holds the conversation; it never runs the CLI itself. Start
// sandbox-host wherever the agent should be able to execute tool calls, then
// point this at it:
//
//	# in the sandbox
//	sandbox-host -network unix -listen /run/claude-sandbox.sock -cwd /work
//
//	# anywhere
//	export TELEGRAM_BOT_TOKEN=...
//	telegram_sandbox -users 12345678 -sandbox-address /run/claude-sandbox.sock -sandbox-network unix
//
// Message the bot to talk to Claude. Tool calls that need approval arrive as
// inline buttons; /help lists the rest.
//
// # Security
//
// This is a remote shell with a chat interface. -users is mandatory: a bot
// token is reachable by anyone who finds the bot, and without an allowlist
// they get your sandbox. Containment itself belongs to the host's policy
// flags, not here — see examples/sandbox.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func main() {
	var (
		network  = flag.String("sandbox-network", "tcp", `sandbox host network: "tcp" or "unix"`)
		address  = flag.String("sandbox-address", "127.0.0.1:8377", "sandbox host address or socket path")
		users    = flag.String("users", "", "comma-separated Telegram user IDs allowed to use this bot (required)")
		approval = flag.Duration("approval-timeout", 5*time.Minute, "how long a tool approval waits for a tap")
		poll     = flag.Int("poll-timeout", 25, "long-poll window in seconds")
	)
	flag.Parse()

	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		log.Fatal("TELEGRAM_BOT_TOKEN is not set")
	}

	allowed, err := parseUserIDs(*users)
	if err != nil {
		log.Fatal(err)
	}

	bot, err := NewBot(Config{
		Token:           token,
		AllowedUsers:    allowed,
		SandboxNetwork:  *network,
		SandboxAddress:  *address,
		SandboxToken:    os.Getenv("SANDBOX_TOKEN"),
		ApprovalTimeout: *approval,
		PollTimeout:     *poll,
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("sandbox at %s/%s", *network, *address)
	if err := bot.Run(ctx); err != nil {
		log.Fatal(err)
	}
	log.Println("shutting down")
}

// parseUserIDs reads the allowlist. It refuses an empty list: this bot can run
// shell commands, so an unrestricted one is a remote shell for the internet.
func parseUserIDs(s string) ([]int64, error) {
	var ids []int64
	for _, field := range strings.Split(s, ",") {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		id, err := strconv.ParseInt(field, 10, 64)
		if err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, errNoUsers
	}
	return ids, nil
}

// errNoUsers is its own value so the message stays identical wherever the
// allowlist is enforced.
var errNoUsers = errNoUsersType{}

type errNoUsersType struct{}

func (errNoUsersType) Error() string {
	return "-users is required: pass the Telegram user IDs allowed to drive the agent " +
		"(message @userinfobot to find yours)"
}

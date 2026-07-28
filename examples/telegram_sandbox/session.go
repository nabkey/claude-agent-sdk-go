package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	claude "github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/examples/sandbox"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// editInterval is how often a streaming turn rewrites its Telegram message.
//
// Telegram rate limits edits to roughly one per second per chat and answers a
// burst with 429s, so the live view is debounced rather than written on every
// delta.
const editInterval = 1500 * time.Millisecond

// chatSession is one Telegram chat's Claude conversation.
//
// A claude.Client carries a single conversation and ReceiveResponse runs until
// the turn's ResultMessage, so turns must not overlap. running enforces that:
// a second message arriving mid-turn is refused rather than queued behind a
// turn the user can no longer see.
type chatSession struct {
	chatID int64
	bot    *Bot

	client    *claude.Client
	transport *sandbox.Transport

	mu      sync.Mutex
	running bool
}

// newChatSession dials the sandbox and connects a Claude client for one chat.
func newChatSession(ctx context.Context, b *Bot, chatID int64) (*chatSession, error) {
	s := &chatSession{chatID: chatID, bot: b}

	s.transport = sandbox.New(sandbox.Config{
		Network: b.cfg.SandboxNetwork,
		Address: b.cfg.SandboxAddress,
		Token:   b.cfg.SandboxToken,
		Start:   sandbox.DefaultStartRequest(),
		Stderr:  func(line string) { log.Printf("chat %d: cli stderr: %s", chatID, line) },
	})

	opts := claude.DefaultAgentOptions()
	opts.CanUseTool = s.approveTool
	opts.Warn = func(w string) { log.Printf("chat %d: sdk warning: %s", chatID, w) }
	// AllowedTools, PermissionMode, Cwd and friends are deliberately absent:
	// they become CLI flags, which a custom transport never emits. The
	// sandbox host owns them. See the README.

	client, err := claude.NewClientWithTransport(ctx, opts, s.transport)
	if err != nil {
		return nil, fmt.Errorf("create client: %w", err)
	}
	if err := client.Connect(ctx, ""); err != nil {
		return nil, fmt.Errorf("connect to sandbox: %w", err)
	}
	s.client = client
	return s, nil
}

func (s *chatSession) close() {
	if s.client != nil {
		s.client.Close()
	}
}

// tryAcquire claims the session for a turn, reporting false if one is running.
func (s *chatSession) tryAcquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return false
	}
	s.running = true
	return true
}

func (s *chatSession) release() {
	s.mu.Lock()
	s.running = false
	s.mu.Unlock()
}

func (s *chatSession) isRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// runTurn sends a prompt and streams the response into a single Telegram
// message, then posts any overflow as follow-ups.
func (s *chatSession) runTurn(ctx context.Context, prompt string) {
	defer s.release()

	live, err := newLiveMessage(ctx, s.bot.api, s.chatID, "🤔 thinking…")
	if err != nil {
		log.Printf("chat %d: open live message: %v", s.chatID, err)
		return
	}

	if err := s.client.SendQuery(ctx, prompt); err != nil {
		live.finish(ctx, "⚠️ could not send to the sandbox: "+err.Error())
		return
	}

	var (
		answer   strings.Builder
		activity []string
	)

	render := func() string {
		var b strings.Builder
		// Keep the tail: a long turn's recent steps matter more than its first.
		const maxActivity = 8
		shown := activity
		if len(shown) > maxActivity {
			b.WriteString(fmt.Sprintf("…%d earlier steps\n", len(shown)-maxActivity))
			shown = shown[len(shown)-maxActivity:]
		}
		for _, line := range shown {
			b.WriteString(line)
			b.WriteString("\n")
		}
		if answer.Len() > 0 {
			if b.Len() > 0 {
				b.WriteString("\n")
			}
			b.WriteString(answer.String())
		}
		if b.Len() == 0 {
			return "🤔 thinking…"
		}
		return b.String()
	}

	for msg := range s.client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case *types.TextBlock:
					answer.WriteString(b.Text)
				case *types.ToolUseBlock:
					activity = append(activity, describeToolUse(b))
				}
			}
			live.update(ctx, render())

		case *types.ResultMessage:
			if m.IsError {
				activity = append(activity, "⚠️ the turn ended with an error")
			}

		case error:
			log.Printf("chat %d: turn error: %v", s.chatID, m)
			answer.WriteString("\n\n⚠️ " + m.Error())
		}
	}

	if err := s.client.Err(); err != nil {
		answer.WriteString("\n\n⚠️ session error: " + err.Error())
	}

	final := strings.TrimSpace(render())
	if final == "" {
		final = "(no response)"
	}

	// The live message holds the first chunk; the rest follow as new messages
	// so nothing is silently clipped at Telegram's 4096-rune ceiling.
	chunks := splitMessage(final)
	live.finish(ctx, chunks[0])
	for _, chunk := range chunks[1:] {
		if _, err := s.bot.api.SendMessage(ctx, s.chatID, chunk, nil); err != nil {
			log.Printf("chat %d: send overflow chunk: %v", s.chatID, err)
			return
		}
	}
}

// describeToolUse renders one tool call as a single status line.
func describeToolUse(b *types.ToolUseBlock) string {
	detail := ""
	switch b.Name {
	case "Bash":
		if cmd, ok := b.Input["command"].(string); ok {
			detail = cmd
		}
	case "Read", "Write", "Edit":
		if path, ok := b.Input["file_path"].(string); ok {
			detail = path
		}
	case "Grep":
		if pattern, ok := b.Input["pattern"].(string); ok {
			detail = pattern
		}
	}
	if detail == "" {
		return "🔧 " + b.Name
	}
	return "🔧 " + b.Name + ": " + truncateRunes(oneLine(detail), 120)
}

func oneLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// liveMessage is a Telegram message rewritten in place as a turn progresses.
//
// Edits are coalesced: update records the newest text and a background flusher
// writes at most one edit per editInterval, so a fast token stream costs a
// steady trickle of API calls instead of a 429.
type liveMessage struct {
	api       *API
	chatID    int64
	messageID int

	mu     sync.Mutex
	text   string
	sent   string
	closed bool

	stop chan struct{}
	done chan struct{}
}

func newLiveMessage(ctx context.Context, api *API, chatID int64, initial string) (*liveMessage, error) {
	msg, err := api.SendMessage(ctx, chatID, initial, nil)
	if err != nil {
		return nil, err
	}

	lm := &liveMessage{
		api:       api,
		chatID:    chatID,
		messageID: msg.MessageID,
		text:      initial,
		sent:      initial,
		stop:      make(chan struct{}),
		done:      make(chan struct{}),
	}
	go lm.flushLoop(ctx)
	return lm, nil
}

func (lm *liveMessage) update(ctx context.Context, text string) {
	lm.mu.Lock()
	lm.text = text
	lm.mu.Unlock()
}

func (lm *liveMessage) flushLoop(ctx context.Context) {
	defer close(lm.done)

	ticker := time.NewTicker(editInterval)
	defer ticker.Stop()

	for {
		select {
		case <-lm.stop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			lm.flush(ctx)
		}
	}
}

// flush writes the pending text if it differs from what Telegram already has.
// Editing a message to its current text is an API error, so the comparison is
// required, not just an optimization.
func (lm *liveMessage) flush(ctx context.Context) {
	lm.mu.Lock()
	if lm.closed || lm.text == lm.sent {
		lm.mu.Unlock()
		return
	}
	text := lm.text
	lm.sent = text
	lm.mu.Unlock()

	if err := lm.api.EditMessageText(ctx, lm.chatID, lm.messageID, text); err != nil {
		log.Printf("chat %d: edit message: %v", lm.chatID, err)
	}
}

// finish stops the flusher and writes the final text once.
func (lm *liveMessage) finish(ctx context.Context, text string) {
	close(lm.stop)
	<-lm.done

	lm.mu.Lock()
	same := text == lm.sent
	lm.text = text
	lm.sent = text
	lm.closed = true
	lm.mu.Unlock()

	if same {
		return
	}
	if err := lm.api.EditMessageText(ctx, lm.chatID, lm.messageID, text); err != nil {
		log.Printf("chat %d: final edit: %v", lm.chatID, err)
	}
}

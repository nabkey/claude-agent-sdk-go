package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// Config is the bot's runtime configuration.
type Config struct {
	// Token is the Telegram bot token.
	Token string

	// AllowedUsers lists the Telegram user IDs permitted to drive the agent.
	//
	// A bot token is reachable by anyone who learns the bot's name, and this
	// bot runs shell commands. An empty allowlist is refused at startup.
	AllowedUsers []int64

	// Sandbox connection details, passed through to sandbox.Config.
	SandboxNetwork string
	SandboxAddress string
	SandboxToken   string

	// ApprovalTimeout bounds how long a tool call waits for a tap before it
	// is denied. A held approval holds the whole turn.
	ApprovalTimeout time.Duration

	// PollTimeout is the long-poll window passed to getUpdates.
	PollTimeout int
}

// Bot routes Telegram updates to per-chat Claude sessions.
type Bot struct {
	cfg     Config
	api     *API
	allowed map[int64]bool

	mu       sync.Mutex
	chats    map[int64]*chatSession
	pending  map[string]*approval
	nonceSeq int
}

// approval is one in-flight tool permission prompt awaiting a button tap.
type approval struct {
	ch        chan bool
	chatID    int64
	messageID int
	tool      string
}

// NewBot validates the configuration and returns a ready bot.
func NewBot(cfg Config) (*Bot, error) {
	if cfg.Token == "" {
		return nil, fmt.Errorf("a bot token is required")
	}
	if len(cfg.AllowedUsers) == 0 {
		return nil, fmt.Errorf("at least one allowed user ID is required: " +
			"this bot runs shell commands, so an open bot is a remote shell for anyone who finds it")
	}
	if cfg.ApprovalTimeout <= 0 {
		cfg.ApprovalTimeout = 5 * time.Minute
	}
	if cfg.PollTimeout <= 0 {
		cfg.PollTimeout = 25
	}

	allowed := make(map[int64]bool, len(cfg.AllowedUsers))
	for _, id := range cfg.AllowedUsers {
		allowed[id] = true
	}

	return &Bot{
		cfg:     cfg,
		api:     NewAPI(cfg.Token),
		allowed: allowed,
		chats:   make(map[int64]*chatSession),
		pending: make(map[string]*approval),
	}, nil
}

// Run long-polls for updates until ctx is cancelled.
func (b *Bot) Run(ctx context.Context) error {
	me, err := b.api.GetMe(ctx)
	if err != nil {
		return fmt.Errorf("verify bot token: %w", err)
	}
	log.Printf("connected as @%s; %d user(s) allowed", me.Username, len(b.allowed))

	defer b.closeAll()

	offset := 0
	for {
		if ctx.Err() != nil {
			return nil
		}

		updates, err := b.api.GetUpdates(ctx, offset, b.cfg.PollTimeout)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			// A poll failure is usually a transient network blip; backing off
			// and retrying beats tearing down every live session.
			log.Printf("getUpdates: %v", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(3 * time.Second):
			}
			continue
		}

		for _, u := range updates {
			if u.UpdateID >= offset {
				offset = u.UpdateID + 1
			}
			b.handleUpdate(ctx, u)
		}
	}
}

func (b *Bot) closeAll() {
	b.mu.Lock()
	defer b.mu.Unlock()
	for id, s := range b.chats {
		s.close()
		delete(b.chats, id)
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u Update) {
	switch {
	case u.CallbackQuery != nil:
		b.handleCallback(ctx, u.CallbackQuery)
	case u.Message != nil && strings.TrimSpace(u.Message.Text) != "":
		b.handleMessage(ctx, u.Message)
	}
}

// authorized reports whether a user may drive the agent.
func (b *Bot) authorized(u *User) bool {
	return u != nil && b.allowed[u.ID]
}

func (b *Bot) handleMessage(ctx context.Context, msg *Message) {
	if !b.authorized(msg.From) {
		// Log the ID so the operator can add it deliberately, and tell the
		// sender nothing about what this bot does.
		log.Printf("rejected message from unauthorized user %d (@%s) in chat %d",
			msg.From.ID, msg.From.Username, msg.Chat.ID)
		b.api.SendMessage(ctx, msg.Chat.ID, "Not authorized.", nil)
		return
	}

	text := strings.TrimSpace(msg.Text)
	if strings.HasPrefix(text, "/") {
		b.handleCommand(ctx, msg.Chat.ID, text)
		return
	}

	session, err := b.session(ctx, msg.Chat.ID)
	if err != nil {
		b.api.SendMessage(ctx, msg.Chat.ID, "⚠️ could not reach the sandbox: "+err.Error(), nil)
		return
	}

	if !session.tryAcquire() {
		b.api.SendMessage(ctx, msg.Chat.ID,
			"Still working on the previous message. Send /stop to interrupt it.", nil)
		return
	}
	go session.runTurn(ctx, text)
}

// session returns the chat's session, creating and connecting one on first use.
func (b *Bot) session(ctx context.Context, chatID int64) (*chatSession, error) {
	b.mu.Lock()
	if s, ok := b.chats[chatID]; ok {
		b.mu.Unlock()
		return s, nil
	}
	b.mu.Unlock()

	// Connecting dials the sandbox and spawns a CLI, so it happens outside the
	// lock. A concurrent creation is resolved below by keeping the first.
	s, err := newChatSession(ctx, b, chatID)
	if err != nil {
		return nil, err
	}

	b.mu.Lock()
	defer b.mu.Unlock()
	if existing, ok := b.chats[chatID]; ok {
		s.close()
		return existing, nil
	}
	b.chats[chatID] = s
	return s, nil
}

// resetSession tears down a chat's session so the next message starts fresh.
func (b *Bot) resetSession(chatID int64) bool {
	b.mu.Lock()
	s, ok := b.chats[chatID]
	delete(b.chats, chatID)
	b.mu.Unlock()

	if ok {
		s.close()
	}
	return ok
}

// --- tool approvals ------------------------------------------------------

// approveTool renders a tool call as an inline keyboard and blocks until the
// user taps, the request times out, or the turn is cancelled.
//
// This runs on the SDK's goroutine mid-turn: the CLI is holding the tool call
// open waiting for the verdict, which is exactly why every path here returns
// a decision rather than hanging.
func (s *chatSession) approveTool(ctx context.Context, tool string, input map[string]any,
	_ types.ToolPermissionContext) (types.PermissionResult, error) {

	b := s.bot
	id := b.newNonce()

	prompt := fmt.Sprintf("Allow %s?\n\n%s", tool, describeToolInput(tool, input))
	kb := &InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{{
		{Text: "✅ Allow", CallbackData: "a:" + id},
		{Text: "❌ Deny", CallbackData: "d:" + id},
	}}}

	msg, err := b.api.SendMessage(ctx, s.chatID, prompt, kb)
	if err != nil {
		// Failing closed is the only safe default: nobody can see the prompt.
		log.Printf("chat %d: send approval prompt: %v", s.chatID, err)
		return &types.PermissionResultDeny{Message: "could not reach the operator for approval"}, nil
	}

	ch := make(chan bool, 1)
	b.mu.Lock()
	b.pending[id] = &approval{ch: ch, chatID: s.chatID, messageID: msg.MessageID, tool: tool}
	b.mu.Unlock()

	defer func() {
		b.mu.Lock()
		delete(b.pending, id)
		b.mu.Unlock()
	}()

	select {
	case allowed := <-ch:
		if allowed {
			return &types.PermissionResultAllow{}, nil
		}
		return &types.PermissionResultDeny{Message: "denied from Telegram"}, nil

	case <-time.After(b.cfg.ApprovalTimeout):
		b.settleApproval(ctx, s.chatID, msg.MessageID,
			fmt.Sprintf("⏱ %s timed out after %s — denied.", tool, b.cfg.ApprovalTimeout))
		return &types.PermissionResultDeny{Message: "approval timed out"}, nil

	case <-ctx.Done():
		b.settleApproval(ctx, s.chatID, msg.MessageID, "🚫 "+tool+" cancelled.")
		return &types.PermissionResultDeny{Message: "session cancelled"}, ctx.Err()
	}
}

// handleCallback resolves a tapped approval button.
func (b *Bot) handleCallback(ctx context.Context, q *CallbackQuery) {
	if !b.authorized(q.From) {
		log.Printf("rejected callback from unauthorized user %d", q.From.ID)
		b.api.AnswerCallbackQuery(ctx, q.ID, "Not authorized.")
		return
	}

	verdict, id, ok := strings.Cut(q.Data, ":")
	if !ok {
		b.api.AnswerCallbackQuery(ctx, q.ID, "")
		return
	}

	b.mu.Lock()
	ap, found := b.pending[id]
	b.mu.Unlock()

	if !found {
		// Stale button: the turn already moved on, or the bot restarted.
		b.api.AnswerCallbackQuery(ctx, q.ID, "That request is no longer waiting.")
		return
	}

	allowed := verdict == "a"
	select {
	case ap.ch <- allowed:
	default:
		// Already resolved by a timeout racing this tap.
	}

	outcome := "❌ Denied " + ap.tool
	if allowed {
		outcome = "✅ Allowed " + ap.tool
	}
	b.api.AnswerCallbackQuery(ctx, q.ID, outcome)
	b.settleApproval(ctx, ap.chatID, ap.messageID, outcome)
}

// settleApproval strips the keyboard and records the outcome, so a resolved
// prompt cannot be tapped again.
func (b *Bot) settleApproval(ctx context.Context, chatID int64, messageID int, text string) {
	if err := b.api.EditMessageText(ctx, chatID, messageID, text); err != nil {
		log.Printf("chat %d: settle approval text: %v", chatID, err)
	}
	if err := b.api.EditMessageReplyMarkup(ctx, chatID, messageID,
		&InlineKeyboardMarkup{InlineKeyboard: [][]InlineKeyboardButton{}}); err != nil {
		log.Printf("chat %d: strip approval keyboard: %v", chatID, err)
	}
}

func (b *Bot) newNonce() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.nonceSeq++
	// Telegram caps callback_data at 64 bytes, so keep this short.
	return strconv.Itoa(b.nonceSeq)
}

// describeToolInput renders a tool's arguments for the approval prompt.
func describeToolInput(tool string, input map[string]any) string {
	switch tool {
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			return truncateRunes(cmd, 800)
		}
	case "Write", "Edit":
		if path, ok := input["file_path"].(string); ok {
			return path
		}
	}
	// Unknown tool: show the arguments rather than approving blind.
	raw, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", input)
	}
	return truncateRunes(string(raw), 800)
}

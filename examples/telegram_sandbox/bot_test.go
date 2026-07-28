package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nabkey/claude-agent-sdk-go/types"
)

// mockAPI is a stand-in Bot API server. It records what the bot sent so tests
// can assert on it, and never touches the network.
type mockAPI struct {
	server *httptest.Server

	mu        sync.Mutex
	sent      []sentMessage
	edits     []editRecord
	answers   []string
	markups   []int
	nextMsgID int
}

type sentMessage struct {
	ChatID   int64
	Text     string
	HasBoard bool
	Buttons  []string
}

type editRecord struct {
	ChatID    int64
	MessageID int
	Text      string
}

func newMockAPI(t *testing.T) *mockAPI {
	t.Helper()

	m := &mockAPI{nextMsgID: 100}
	m.server = httptest.NewServer(http.HandlerFunc(m.handle))
	t.Cleanup(m.server.Close)
	return m
}

func (m *mockAPI) handle(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	method := parts[len(parts)-1]

	var body map[string]any
	json.NewDecoder(r.Body).Decode(&body)

	m.mu.Lock()
	defer m.mu.Unlock()

	respond := func(result any) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": result})
	}

	switch method {
	case "getMe":
		respond(map[string]any{"id": 1, "username": "testbot"})

	case "sendMessage":
		chatID, _ := body["chat_id"].(float64)
		text, _ := body["text"].(string)

		rec := sentMessage{ChatID: int64(chatID), Text: text}
		if markup, ok := body["reply_markup"].(map[string]any); ok {
			rec.HasBoard = true
			rows, _ := markup["inline_keyboard"].([]any)
			for _, row := range rows {
				for _, btn := range row.([]any) {
					b := btn.(map[string]any)
					rec.Buttons = append(rec.Buttons, b["callback_data"].(string))
				}
			}
		}
		m.sent = append(m.sent, rec)

		m.nextMsgID++
		respond(map[string]any{
			"message_id": m.nextMsgID,
			"chat":       map[string]any{"id": chatID},
			"text":       text,
		})

	case "editMessageText":
		chatID, _ := body["chat_id"].(float64)
		msgID, _ := body["message_id"].(float64)
		text, _ := body["text"].(string)
		m.edits = append(m.edits, editRecord{int64(chatID), int(msgID), text})
		respond(true)

	case "editMessageReplyMarkup":
		msgID, _ := body["message_id"].(float64)
		m.markups = append(m.markups, int(msgID))
		respond(true)

	case "answerCallbackQuery":
		text, _ := body["text"].(string)
		m.answers = append(m.answers, text)
		respond(true)

	case "getUpdates":
		respond([]any{})

	default:
		respond(true)
	}
}

// bot returns a Bot wired to the mock server.
func (m *mockAPI) bot(t *testing.T, allowed ...int64) *Bot {
	t.Helper()

	if len(allowed) == 0 {
		allowed = []int64{42}
	}
	b, err := NewBot(Config{
		Token:           "test-token",
		AllowedUsers:    allowed,
		ApprovalTimeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("new bot: %v", err)
	}
	b.api.BaseURL = m.server.URL
	return b
}

func (m *mockAPI) sentMessages() []sentMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]sentMessage(nil), m.sent...)
}

func (m *mockAPI) editRecords() []editRecord {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]editRecord(nil), m.edits...)
}

// --- tests ---------------------------------------------------------------

// An open bot is a remote shell for anyone who finds it, so construction must
// refuse an empty allowlist rather than defaulting to permissive.
func TestNewBotRequiresAllowlist(t *testing.T) {
	_, err := NewBot(Config{Token: "t"})
	if err == nil {
		t.Fatal("NewBot should refuse an empty allowlist")
	}
	if !strings.Contains(err.Error(), "allowed user") {
		t.Errorf("error = %v, want it to explain the allowlist requirement", err)
	}

	if _, err := NewBot(Config{AllowedUsers: []int64{1}}); err == nil {
		t.Fatal("NewBot should refuse an empty token")
	}
}

func TestUnauthorizedUserIsRejected(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleMessage(context.Background(), &Message{
		From: &User{ID: 999, Username: "intruder"},
		Chat: Chat{ID: 7},
		Text: "rm -rf /",
	})

	sent := m.sentMessages()
	if len(sent) != 1 {
		t.Fatalf("expected exactly one reply, got %d: %+v", len(sent), sent)
	}
	if sent[0].Text != "Not authorized." {
		t.Errorf("reply = %q, want a bare refusal", sent[0].Text)
	}

	// The refusal must not have created a session for the intruder.
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.chats) != 0 {
		t.Errorf("an unauthorized message created %d session(s)", len(b.chats))
	}
}

func TestUnauthorizedCallbackIsRejected(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	// Register an approval the intruder will try to answer.
	ch := make(chan bool, 1)
	b.pending["1"] = &approval{ch: ch, chatID: 7, messageID: 101, tool: "Bash"}

	b.handleCallback(context.Background(), &CallbackQuery{
		ID:   "cb1",
		From: &User{ID: 999},
		Data: "a:1",
	})

	select {
	case v := <-ch:
		t.Fatalf("an unauthorized tap resolved the approval with %v", v)
	default:
	}
}

func TestApprovalAllowRoundTrip(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)
	s := &chatSession{chatID: 7, bot: b}

	result := make(chan types.PermissionResult, 1)
	go func() {
		r, _ := s.approveTool(context.Background(), "Bash",
			map[string]any{"command": "echo hi"}, types.ToolPermissionContext{})
		result <- r
	}()

	data := waitForButton(t, m)

	b.handleCallback(context.Background(), &CallbackQuery{
		ID:   "cb1",
		From: &User{ID: 42},
		Data: data,
	})

	select {
	case r := <-result:
		if _, ok := r.(*types.PermissionResultAllow); !ok {
			t.Fatalf("result = %T, want PermissionResultAllow", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approveTool did not return after the tap")
	}

	// The prompt must stop being tappable once resolved.
	m.mu.Lock()
	markups := len(m.markups)
	m.mu.Unlock()
	if markups == 0 {
		t.Error("the approval keyboard was never stripped")
	}
}

func TestApprovalDenyRoundTrip(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)
	s := &chatSession{chatID: 7, bot: b}

	result := make(chan types.PermissionResult, 1)
	go func() {
		r, _ := s.approveTool(context.Background(), "Bash",
			map[string]any{"command": "rm -rf /"}, types.ToolPermissionContext{})
		result <- r
	}()

	data := waitForButton(t, m)
	// Flip the verdict prefix to deny the same request.
	_, id, _ := strings.Cut(data, ":")

	b.handleCallback(context.Background(), &CallbackQuery{
		ID: "cb1", From: &User{ID: 42}, Data: "d:" + id,
	})

	select {
	case r := <-result:
		if _, ok := r.(*types.PermissionResultDeny); !ok {
			t.Fatalf("result = %T, want PermissionResultDeny", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("approveTool did not return after the deny tap")
	}
}

// An approval nobody answers must not hold the turn open forever.
func TestApprovalTimesOutClosed(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)
	b.cfg.ApprovalTimeout = 300 * time.Millisecond
	s := &chatSession{chatID: 7, bot: b}

	start := time.Now()
	r, _ := s.approveTool(context.Background(), "Bash",
		map[string]any{"command": "sleep 1"}, types.ToolPermissionContext{})

	if _, ok := r.(*types.PermissionResultDeny); !ok {
		t.Fatalf("result = %T, want a deny on timeout", r)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("timeout took %s, far longer than configured", elapsed)
	}
}

// A tap arriving after the turn moved on must be answered, not dropped
// silently, and must not panic on the missing registry entry.
func TestStaleCallbackIsAnswered(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleCallback(context.Background(), &CallbackQuery{
		ID: "cb1", From: &User{ID: 42}, Data: "a:does-not-exist",
	})

	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.answers) != 1 {
		t.Fatalf("expected the stale tap to be answered once, got %d", len(m.answers))
	}
	if !strings.Contains(m.answers[0], "no longer waiting") {
		t.Errorf("answer = %q, want an explanation", m.answers[0])
	}
}

func TestHelpCommand(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleMessage(context.Background(), &Message{
		From: &User{ID: 42}, Chat: Chat{ID: 7}, Text: "/help",
	})

	sent := m.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "/stop") {
		t.Fatalf("expected the help text, got %+v", sent)
	}
}

// Telegram appends the bot username to commands in groups; the dispatcher has
// to strip it or every command breaks there.
func TestCommandStripsBotSuffix(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleCommand(context.Background(), 7, "/help@mytestbot")

	sent := m.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "/stop") {
		t.Fatalf("suffixed command was not dispatched, got %+v", sent)
	}
}

func TestUnknownCommand(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleCommand(context.Background(), 7, "/nope")

	sent := m.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "Unknown command") {
		t.Fatalf("expected an unknown-command reply, got %+v", sent)
	}
}

func TestStatusWithoutSession(t *testing.T) {
	m := newMockAPI(t)
	b := m.bot(t, 42)

	b.handleCommand(context.Background(), 7, "/status")

	sent := m.sentMessages()
	if len(sent) != 1 || !strings.Contains(sent[0].Text, "not connected") {
		t.Fatalf("expected a disconnected status, got %+v", sent)
	}
}

// waitForButton polls the mock until the approval prompt appears and returns
// its allow-button callback data.
func waitForButton(t *testing.T, m *mockAPI) string {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, s := range m.sentMessages() {
			if s.HasBoard && len(s.Buttons) > 0 {
				return s.Buttons[0]
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("no approval prompt with buttons appeared")
	return ""
}

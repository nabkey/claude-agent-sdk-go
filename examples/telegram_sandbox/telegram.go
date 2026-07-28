package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Telegram's hard limits. Messages over the text cap are rejected outright, and
// edits are rate limited per chat, which is why the streaming path debounces.
const (
	maxMessageRunes = 4096
	// chunkRunes leaves room for the code fences and ellipsis the sender adds.
	chunkRunes = 3800
)

// API is a minimal Telegram Bot API client: just the methods this bot needs.
type API struct {
	Token string
	HTTP  *http.Client

	// BaseURL overrides https://api.telegram.org for tests.
	BaseURL string
}

// NewAPI returns a client whose HTTP timeout is comfortably longer than the
// long-poll window, so a quiet chat is not mistaken for a dead connection.
func NewAPI(token string) *API {
	return &API{
		Token: token,
		HTTP:  &http.Client{Timeout: 90 * time.Second},
	}
}

func (a *API) baseURL() string {
	if a.BaseURL != "" {
		return a.BaseURL
	}
	return "https://api.telegram.org"
}

// apiResponse is the envelope every Bot API method returns.
type apiResponse struct {
	OK          bool            `json:"ok"`
	Result      json.RawMessage `json:"result"`
	Description string          `json:"description"`
	ErrorCode   int             `json:"error_code"`
}

func (a *API) call(ctx context.Context, method string, req, out any) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode %s request: %w", method, err)
	}

	url := fmt.Sprintf("%s/bot%s/%s", a.baseURL(), a.Token, method)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", method, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := a.HTTP.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return fmt.Errorf("read %s response: %w", method, err)
	}

	var env apiResponse
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("decode %s response (HTTP %d): %w", method, resp.StatusCode, err)
	}
	if !env.OK {
		return fmt.Errorf("%s failed: %s (code %d)", method, env.Description, env.ErrorCode)
	}
	if out != nil && len(env.Result) > 0 {
		if err := json.Unmarshal(env.Result, out); err != nil {
			return fmt.Errorf("decode %s result: %w", method, err)
		}
	}
	return nil
}

// --- wire types ----------------------------------------------------------

// User is a Telegram account. ID is what the allowlist checks.
type User struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	FirstName string `json:"first_name"`
}

// Chat is a conversation. Its ID keys the per-chat Claude session.
type Chat struct {
	ID   int64  `json:"id"`
	Type string `json:"type"`
}

// Message is an inbound or outbound message.
type Message struct {
	MessageID int    `json:"message_id"`
	From      *User  `json:"from"`
	Chat      Chat   `json:"chat"`
	Text      string `json:"text"`
}

// CallbackQuery is the tap of an inline keyboard button.
type CallbackQuery struct {
	ID      string   `json:"id"`
	From    *User    `json:"from"`
	Message *Message `json:"message"`
	Data    string   `json:"data"`
}

// Update is one item from getUpdates.
type Update struct {
	UpdateID      int            `json:"update_id"`
	Message       *Message       `json:"message"`
	CallbackQuery *CallbackQuery `json:"callback_query"`
}

// InlineKeyboardButton is one tappable button. CallbackData comes back
// verbatim in CallbackQuery.Data and is capped at 64 bytes by Telegram.
type InlineKeyboardButton struct {
	Text         string `json:"text"`
	CallbackData string `json:"callback_data,omitempty"`
}

// InlineKeyboardMarkup is a grid of buttons attached to a message.
type InlineKeyboardMarkup struct {
	InlineKeyboard [][]InlineKeyboardButton `json:"inline_keyboard"`
}

// --- methods -------------------------------------------------------------

// GetUpdates long-polls for new updates starting at offset.
func (a *API) GetUpdates(ctx context.Context, offset, timeoutSecs int) ([]Update, error) {
	req := map[string]any{
		"offset":  offset,
		"timeout": timeoutSecs,
		// Everything else (edits, channel posts, reactions) is noise here.
		"allowed_updates": []string{"message", "callback_query"},
	}
	var updates []Update
	if err := a.call(ctx, "getUpdates", req, &updates); err != nil {
		return nil, err
	}
	return updates, nil
}

// SendMessage posts text, optionally with an inline keyboard, and returns the
// created message so it can be edited later.
//
// Text is sent unformatted on purpose. Claude's output is full of Markdown
// that Telegram's parsers reject — an unmatched underscore or a stray
// backtick fails the whole send — and a dropped message is worse than an
// unstyled one.
func (a *API) SendMessage(ctx context.Context, chatID int64, text string, kb *InlineKeyboardMarkup) (*Message, error) {
	req := map[string]any{
		"chat_id": chatID,
		"text":    truncateRunes(text, maxMessageRunes),
	}
	if kb != nil {
		req["reply_markup"] = kb
	}
	var msg Message
	if err := a.call(ctx, "sendMessage", req, &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// EditMessageText replaces the text of a message already sent.
func (a *API) EditMessageText(ctx context.Context, chatID int64, messageID int, text string) error {
	req := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"text":       truncateRunes(text, maxMessageRunes),
	}
	return a.call(ctx, "editMessageText", req, nil)
}

// EditMessageReplyMarkup swaps a message's keyboard. Passing nil strips it,
// which is how a resolved approval prompt stops being tappable.
func (a *API) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, kb *InlineKeyboardMarkup) error {
	req := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
	}
	if kb != nil {
		req["reply_markup"] = kb
	}
	return a.call(ctx, "editMessageReplyMarkup", req, nil)
}

// AnswerCallbackQuery acknowledges a button tap. Telegram shows a spinner on
// the button until this is called.
func (a *API) AnswerCallbackQuery(ctx context.Context, queryID, text string) error {
	return a.call(ctx, "answerCallbackQuery", map[string]any{
		"callback_query_id": queryID,
		"text":              text,
	}, nil)
}

// GetMe returns the bot's own account, used as a startup credential check.
func (a *API) GetMe(ctx context.Context) (*User, error) {
	var me User
	if err := a.call(ctx, "getMe", map[string]any{}, &me); err != nil {
		return nil, err
	}
	return &me, nil
}

// --- text helpers --------------------------------------------------------

// truncateRunes clips to n runes, never splitting a multi-byte character.
func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n <= 1 {
		return string(r[:n])
	}
	return string(r[:n-1]) + "…"
}

// splitMessage breaks long output into sendable chunks, preferring paragraph
// then line boundaries so code blocks and prose survive the split.
func splitMessage(s string) []string {
	if len([]rune(s)) <= chunkRunes {
		return []string{s}
	}

	var chunks []string
	remaining := s
	for len([]rune(remaining)) > chunkRunes {
		runes := []rune(remaining)
		window := string(runes[:chunkRunes])

		cut := strings.LastIndex(window, "\n\n")
		if cut < chunkRunes/2 {
			cut = strings.LastIndex(window, "\n")
		}
		if cut < chunkRunes/2 {
			// No usable boundary: hard split rather than emit one huge chunk.
			cut = len(window)
		}

		chunks = append(chunks, strings.TrimRight(window[:cut], "\n"))
		remaining = strings.TrimLeft(remaining[cut:], "\n")
	}
	if strings.TrimSpace(remaining) != "" {
		chunks = append(chunks, remaining)
	}
	return chunks
}

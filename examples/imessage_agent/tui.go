package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nabkey/claude-agent-sdk-go"
	"github.com/nabkey/claude-agent-sdk-go/types"
)

// --- Message types for Bubble Tea ---

type chatMessage struct {
	sender  string // "you", "agent", "system"
	content string
	ts      time.Time
}

type toolActivity struct {
	name   string
	input  string
	ts     time.Time
	status string // "running", "done"
}

type replyMsg struct {
	content    string
	toolEvents []toolActivity
	err        error
}

type clientReadyMsg struct {
	client *claude.Client
	err    error
}

type tickMsg time.Time

// --- Styles ---

var (
	// Panel borders
	chatPanelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62"))

	sidebarPanelStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("240"))

	statusBarStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("236")).
			Foreground(lipgloss.Color("252")).
			Padding(0, 1)

	statusKeyStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("62")).
			Foreground(lipgloss.Color("230")).
			Padding(0, 1).
			Bold(true)

	// Message styles
	youNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("10")).
			Bold(true)

	agentNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	systemMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245")).
			Italic(true)

	thinkingStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Bold(true)

	// Sidebar styles
	sidebarTitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("62")).
				Bold(true).
				Padding(0, 1)

	toolRunningStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("214"))

	toolDoneStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("245"))

	toolNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("39")).
			Bold(true)

	headerStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("62")).
			Bold(true).
			Padding(0, 1)
)

// --- Model ---

const sidebarWidth = 32

type chatModel struct {
	chatViewport    viewport.Model
	sidebarViewport viewport.Model
	textarea        textarea.Model

	messages   []chatMessage
	activities []toolActivity
	cfg        agentConfig
	brainURL   string
	client     *claude.Client
	ctx        context.Context

	width       int
	height      int
	waiting     bool
	msgCount    int
	connected   bool
	spinFrame   int
	lastToolUse string
}

func newChatModel(cfg agentConfig) chatModel {
	ta := textarea.New()
	ta.Placeholder = "Message the agent... (enter to send, ctrl+c to quit)"
	ta.Focus()
	ta.CharLimit = 2000
	ta.SetHeight(3)
	ta.ShowLineNumbers = false

	return chatModel{
		chatViewport:    viewport.New(60, 20),
		sidebarViewport: viewport.New(sidebarWidth-2, 20),
		textarea:        ta,
		cfg:             cfg,
		brainURL:        cfg.brainURL,
		ctx:             context.Background(),
		waiting:         true,
		messages:        []chatMessage{{sender: "system", content: "Connecting to the sandbox...", ts: time.Now()}},
	}
}

var spinChars = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func (m chatModel) Init() tea.Cmd {
	cfg := m.cfg
	return tea.Batch(
		textarea.Blink,
		// Connect client
		func() tea.Msg {
			client, err := createChatClient(cfg)
			if err != nil {
				return clientReadyMsg{err: err}
			}
			if err := client.Connect(context.Background(), ""); err != nil {
				return clientReadyMsg{err: err}
			}
			return clientReadyMsg{client: client}
		},
		// Spinner tick
		tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) }),
	)
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var vpCmd, sbCmd, taCmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.recalcLayout()

	case tickMsg:
		if m.waiting {
			m.spinFrame = (m.spinFrame + 1) % len(spinChars)
			m.renderChat()
		}
		return m, tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg { return tickMsg(t) })

	case clientReadyMsg:
		if msg.err != nil {
			m.messages = []chatMessage{{sender: "system", content: fmt.Sprintf("Connection failed: %v", msg.err), ts: time.Now()}}
		} else {
			m.client = msg.client
			m.connected = true
			m.messages = []chatMessage{{sender: "system", content: "Connected. Say hi!", ts: time.Now()}}
		}
		m.waiting = false
		m.renderChat()
		m.renderSidebar()

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit
		case "enter":
			if m.waiting || m.client == nil {
				break
			}
			input := strings.TrimSpace(m.textarea.Value())
			if input == "" {
				break
			}

			m.messages = append(m.messages, chatMessage{sender: "you", content: input, ts: time.Now()})
			m.textarea.Reset()
			m.waiting = true
			m.msgCount++
			m.renderChat()

			client := m.client
			ctx := m.ctx
			brainURL := m.brainURL
			return m, func() tea.Msg {
				reply, toolEvents, err := sendChatMessageWithTools(ctx, client, brainURL, input)
				return replyMsg{content: reply, toolEvents: toolEvents, err: err}
			}
		}

	case replyMsg:
		m.waiting = false
		// Add tool activities to sidebar
		m.activities = append(m.activities, msg.toolEvents...)

		if msg.err != nil {
			m.messages = append(m.messages, chatMessage{
				sender: "system", content: fmt.Sprintf("Error: %v", msg.err), ts: time.Now(),
			})
		} else {
			m.messages = append(m.messages, chatMessage{
				sender: "agent", content: msg.content, ts: time.Now(),
			})
			m.msgCount++
		}
		m.renderChat()
		m.renderSidebar()
	}

	m.chatViewport, vpCmd = m.chatViewport.Update(msg)
	m.sidebarViewport, sbCmd = m.sidebarViewport.Update(msg)
	if !m.waiting {
		m.textarea, taCmd = m.textarea.Update(msg)
	}

	return m, tea.Batch(vpCmd, sbCmd, taCmd)
}

func (m *chatModel) recalcLayout() {
	headerH := 1
	inputH := 5 // textarea height + padding
	statusH := 1
	chrome := headerH + inputH + statusH + 4 // borders

	chatW := m.width - sidebarWidth - 3 // gap + borders
	if chatW < 30 {
		chatW = 30
	}
	contentH := m.height - chrome
	if contentH < 5 {
		contentH = 5
	}

	m.chatViewport.Width = chatW - 2 // account for border padding
	m.chatViewport.Height = contentH
	m.sidebarViewport.Width = sidebarWidth - 4
	m.sidebarViewport.Height = contentH
	m.textarea.SetWidth(chatW - 2)

	m.renderChat()
	m.renderSidebar()
}

// --- Rendering ---

func (m *chatModel) renderChat() {
	var sb strings.Builder
	w := m.chatViewport.Width
	if w < 10 {
		w = 60
	}

	for _, msg := range m.messages {
		ts := msg.ts.Format("15:04")
		switch msg.sender {
		case "you":
			header := youNameStyle.Render("You") + " " + systemMsgStyle.Render(ts)
			sb.WriteString(header + "\n")
			sb.WriteString(wordWrap(msg.content, w) + "\n\n")
		case "agent":
			header := agentNameStyle.Render("Claude") + " " + systemMsgStyle.Render(ts)
			sb.WriteString(header + "\n")
			sb.WriteString(wordWrap(msg.content, w) + "\n\n")
		case "system":
			sb.WriteString(systemMsgStyle.Render("  "+msg.content) + "\n\n")
		}
	}

	if m.waiting {
		spin := spinChars[m.spinFrame]
		sb.WriteString(thinkingStyle.Render(spin+" Claude is thinking...") + "\n")
	}

	m.chatViewport.SetContent(sb.String())
	m.chatViewport.GotoBottom()
}

func (m *chatModel) renderSidebar() {
	var sb strings.Builder

	sb.WriteString(sidebarTitleStyle.Render("Tool Activity") + "\n")
	sb.WriteString(strings.Repeat("─", sidebarWidth-4) + "\n")

	if len(m.activities) == 0 {
		sb.WriteString(systemMsgStyle.Render("  No tool use yet.") + "\n")
	} else {
		// Show last N activities that fit
		start := 0
		if len(m.activities) > 50 {
			start = len(m.activities) - 50
		}
		for _, act := range m.activities[start:] {
			ts := act.ts.Format("15:04:05")
			icon := "✓"
			style := toolDoneStyle
			if act.status == "running" {
				icon = "●"
				style = toolRunningStyle
			}
			name := toolNameStyle.Render(act.name)
			line := fmt.Sprintf("%s %s %s", style.Render(icon), name, systemMsgStyle.Render(ts))
			sb.WriteString(line + "\n")
			if act.input != "" {
				inputPreview := act.input
				maxLen := sidebarWidth - 8
				if len(inputPreview) > maxLen {
					inputPreview = inputPreview[:maxLen-1] + "…"
				}
				sb.WriteString(systemMsgStyle.Render("  "+inputPreview) + "\n")
			}
		}
	}

	m.sidebarViewport.SetContent(sb.String())
	m.sidebarViewport.GotoBottom()
}

func (m chatModel) renderStatusBar() string {
	w := m.width
	if w < 20 {
		w = 80
	}

	// Left: connection status
	connStatus := "● connected"
	connColor := "10" // green
	if !m.connected {
		connStatus = "○ disconnected"
		connColor = "9" // red
	}
	left := statusKeyStyle.Render("AGENT") + " " +
		lipgloss.NewStyle().Foreground(lipgloss.Color(connColor)).Render(connStatus)

	// Center: message count
	center := fmt.Sprintf("msgs: %d", m.msgCount)

	// Right: tool count
	right := fmt.Sprintf("tools: %d", len(m.activities))

	// Pad to fill width
	usedWidth := lipgloss.Width(left) + lipgloss.Width(center) + lipgloss.Width(right)
	gap := w - usedWidth - 4
	if gap < 0 {
		gap = 0
	}
	leftGap := gap / 2
	rightGap := gap - leftGap

	bar := left + strings.Repeat(" ", leftGap) + center + strings.Repeat(" ", rightGap) + right
	return statusBarStyle.Width(w).Render(bar)
}

func (m chatModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	header := headerStyle.Render("~ Agent Chat ~")

	// Chat panel with border
	chatW := m.width - sidebarWidth - 1
	if chatW < 30 {
		chatW = 30
	}
	chatPanel := chatPanelStyle.Width(chatW - 2).Render(m.chatViewport.View())

	// Sidebar panel with border
	sidebarPanel := sidebarPanelStyle.Width(sidebarWidth - 2).Render(m.sidebarViewport.View())

	// Join chat and sidebar horizontally
	mainArea := lipgloss.JoinHorizontal(lipgloss.Top, chatPanel, sidebarPanel)

	// Input area
	input := m.textarea.View()

	// Status bar
	status := m.renderStatusBar()

	return lipgloss.JoinVertical(lipgloss.Left, header, mainArea, input, status)
}

// --- Chat processing ---

func createChatClient(cfg agentConfig) (*claude.Client, error) {
	return createPersistentClient(context.Background(), cfg)
}

// sendChatMessageWithTools sends a message and captures tool use activity.
func sendChatMessageWithTools(ctx context.Context, client *claude.Client, brainURL, input string) (string, []toolActivity, error) {
	if err := client.SendQuery(ctx, input); err != nil {
		return "", nil, fmt.Errorf("send failed: %w", err)
	}

	var reply strings.Builder
	var toolEvents []toolActivity

	for msg := range client.ReceiveResponse() {
		switch m := msg.(type) {
		case *types.AssistantMessage:
			for _, block := range m.Content {
				switch b := block.(type) {
				case *types.TextBlock:
					reply.WriteString(b.Text)
				case *types.ToolUseBlock:
					// Capture tool use for sidebar
					inputPreview := ""
					if b.Input != nil {
						// Compact summary of input args
						var parts []string
						for k, v := range b.Input {
							parts = append(parts, fmt.Sprintf("%s=%v", k, v))
						}
						inputPreview = strings.Join(parts, " ")
					}
					toolEvents = append(toolEvents, toolActivity{
						name:   b.Name,
						input:  inputPreview,
						ts:     time.Now(),
						status: "done",
					})
				}
			}
		case *types.ResultMessage:
			// done
		}
	}

	text := strings.TrimSpace(reply.String())
	if text == "" {
		return "Sorry, I couldn't process that.", toolEvents, nil
	}

	saveConversation(ctx, brainURL, "terminal-user", input, text)
	return text, toolEvents, nil
}

// --- Helpers ---

// wordWrap wraps text to fit within width, breaking on word boundaries.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}
	var result strings.Builder
	for _, line := range strings.Split(text, "\n") {
		if lipgloss.Width(line) <= width {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(line)
			continue
		}
		words := strings.Fields(line)
		currentLine := ""
		for _, word := range words {
			if currentLine == "" {
				currentLine = word
			} else if lipgloss.Width(currentLine+" "+word) <= width {
				currentLine += " " + word
			} else {
				if result.Len() > 0 {
					result.WriteString("\n")
				}
				result.WriteString(currentLine)
				currentLine = word
			}
		}
		if currentLine != "" {
			if result.Len() > 0 {
				result.WriteString("\n")
			}
			result.WriteString(currentLine)
		}
	}
	return result.String()
}

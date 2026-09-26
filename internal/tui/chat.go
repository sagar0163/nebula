package tui

import (
	"context"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type ChatAgent interface {
	DoGoalStr(ctx context.Context, goal string, opts interface{}) (string, error)
}

type chatMsg struct {
	role    string
	content string
}

type chatResultMsg struct {
	content string
	err     error
}

type ChatModel struct {
	opts interface{}
	agent    ChatAgent
	viewport viewport.Model
	input    textinput.Model
	messages []chatMsg
	err      error
	working  bool
}

func NewChat(agent ChatAgent, opts interface{}) ChatModel {
	ti := textinput.New()
	ti.Placeholder = "Tell Nebula what to do..."
	ti.Focus()
	ti.CharLimit = 256
	ti.Width = 50

	vp := viewport.New(80, 20)

	return ChatModel{
		agent:    agent,
		opts:     opts,
		input:    ti,
		viewport: vp,
		messages: []chatMsg{{role: "system", content: "Nebula Chat initialized. Type your goal or 'exit' to quit."}},
	}
}

func (m ChatModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m ChatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.input, tiCmd = m.input.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyEnter:
			val := m.input.Value()
			if val == "exit" || val == "quit" {
				return m, tea.Quit
			}
			if val != "" && !m.working {
				m.messages = append(m.messages, chatMsg{role: "user", content: val})
				m.input.SetValue("")
				m.working = true
				m.updateViewport()
				return m, m.doGoal(val)
			}
		}

	case chatResultMsg:
		m.working = false
		if msg.err != nil {
			m.messages = append(m.messages, chatMsg{role: "error", content: msg.err.Error()})
		} else {
			m.messages = append(m.messages, chatMsg{role: "agent", content: msg.content})
		}
		m.updateViewport()
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m ChatModel) doGoal(goal string) tea.Cmd {
	return func() tea.Msg {
		res, err := m.agent.DoGoalStr(context.Background(), goal, m.opts)
		return chatResultMsg{content: res, err: err}
	}
}

func (m *ChatModel) updateViewport() {
	var sb strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("69")).Render("You: ") + msg.content + "\n\n")
		case "agent":
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Render("Nebula: ") + msg.content + "\n\n")
		case "error":
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("Error: ") + msg.content + "\n\n")
		default:
			sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(msg.content) + "\n\n")
		}
	}
	m.viewport.SetContent(sb.String())
	m.viewport.GotoBottom()
}

func (m ChatModel) View() string {
	var sb strings.Builder
	sb.WriteString(m.viewport.View())
	sb.WriteString("\n\n")
	if m.working {
		sb.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Render("Nebula is thinking/executing..."))
	} else {
		sb.WriteString(m.input.View())
	}
	return sb.String()
}

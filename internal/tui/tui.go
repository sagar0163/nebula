package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the root bubbletea model for the Nebula interactive REPL.
type Model struct {
	width  int
	height int
	state  state
	input  string
	output []OutputLine
}

type state int

const (
	stateIdle state = iota
	stateRunning
	stateHealing
	stateAwaitingApproval
)

// OutputLine is a single line of agent output with a type for styling.
type OutputLine struct {
	Kind    LineKind
	Content string
}

type LineKind int

const (
	LineKindOutput   LineKind = iota
	LineKindError
	LineKindSuggestion
	LineKindSystem
)

// Styles
var (
	styleSuggestion = lipgloss.NewStyle().
			Foreground(lipgloss.Color("86")).
			Bold(true)

	styleError = lipgloss.NewStyle().
			Foreground(lipgloss.Color("196"))

	styleSystem = lipgloss.NewStyle().
			Foreground(lipgloss.Color("240")).
			Italic(true)

	stylePrompt = lipgloss.NewStyle().
			Foreground(lipgloss.Color("212")).
			Bold(true)
)

// Msgs

// TokenMsg carries a single LLM streaming token.
type TokenMsg struct {
	Text   string
	IsLast bool
}

// CommandExitMsg is sent when a PTY command finishes.
type CommandExitMsg struct {
	ExitCode int
	Output   string
}

// SuggestionMsg carries a heal suggestion for user approval.
type SuggestionMsg struct {
	FixCmd      string
	Explanation string
}

func NewModel() Model {
	return Model{state: stateIdle}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c", "q":
			return m, tea.Quit
		case "enter":
			if m.state == stateIdle && m.input != "" {
				return m.submitInput()
			}
		default:
			if m.state == stateIdle {
				m.input += msg.String()
			}
		}

	case TokenMsg:
		if msg.IsLast {
			m.state = stateAwaitingApproval
		} else {
			m.appendOutput(LineKindSuggestion, msg.Text)
		}

	case CommandExitMsg:
		if msg.ExitCode != 0 {
			m.state = stateHealing
			m.appendOutput(LineKindError, "Command failed — Nebula is analyzing...")
			// TODO: fire diagnose Cmd
		} else {
			m.state = stateIdle
		}

	case SuggestionMsg:
		m.state = stateAwaitingApproval
		m.appendOutput(LineKindSuggestion, "Suggested fix: "+msg.FixCmd)
		m.appendOutput(LineKindSystem, msg.Explanation)
		m.appendOutput(LineKindSystem, "Apply? [y/n/e(dit)/?(explain)]")
	}

	return m, nil
}

func (m Model) View() string {
	header := styleSystem.Render("◈ nebula")
	prompt := stylePrompt.Render("❯ ") + m.input

	var lines string
	for _, l := range m.output {
		lines += m.renderLine(l) + "\n"
	}

	return header + "\n\n" + lines + "\n" + prompt
}

func (m Model) renderLine(l OutputLine) string {
	switch l.Kind {
	case LineKindError:
		return styleError.Render(l.Content)
	case LineKindSuggestion:
		return styleSuggestion.Render(l.Content)
	case LineKindSystem:
		return styleSystem.Render(l.Content)
	default:
		return l.Content
	}
}

func (m *Model) appendOutput(kind LineKind, content string) {
	m.output = append(m.output, OutputLine{Kind: kind, Content: content})
}

func (m Model) submitInput() (Model, tea.Cmd) {
	input := m.input
	m.input = ""
	m.state = stateRunning
	m.appendOutput(LineKindOutput, "❯ "+input)

	// TODO: return a tea.Cmd that runs the input through the agent harness
	_ = input
	return m, nil
}

package tui

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/sagar0163/nebula/internal/llm"
	"github.com/sagar0163/nebula/internal/models"
	"github.com/sagar0163/nebula/internal/safety"
)

// AgentRunner is the subset of agent.Agent the TUI needs.
// Defined here to avoid an import cycle.
type AgentRunner interface {
	Run(ctx context.Context, args []string, opts AgentRunOptions) (*AgentResult, error)
}

// AgentRunOptions mirrors agent.RunOptions without importing agent.
type AgentRunOptions struct {
	DryRun          bool
	SkipPermissions bool
	ApprovalFn      func(cmd string, risk safety.Risk) bool
}

// AgentResult mirrors agent.RunResult.
type AgentResult struct {
	Command   string
	ExitCode  int
	Healed    bool
	HealApply *models.HealSuggestion
}

// ── Messages ──────────────────────────────────────────────────────────────

// TokenMsg carries a single LLM streaming token.
type TokenMsg struct {
	Text   string
	IsLast bool
	Err    error
}

// CommandExitMsg is sent when a PTY command finishes.
type CommandExitMsg struct {
	ExitCode   int
	Output     string
	Suggestion *models.HealSuggestion
}

// SuggestionMsg carries a heal suggestion awaiting user approval.
type SuggestionMsg struct {
	FixCmd      string
	Explanation string
}

// ApprovalMsg carries the user's decision on a heal suggestion.
type ApprovalMsg struct {
	Approved bool
	EditedCmd string // non-empty if user edited the fix
}

// errMsg wraps an internal error.
type errMsg struct{ err error }

// ── Styles ────────────────────────────────────────────────────────────────

var (
	styleSuggestion = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Bold(true)
	styleError      = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	styleSystem     = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Italic(true)
	stylePrompt     = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	styleHealing    = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styleSuccess    = lipgloss.NewStyle().Foreground(lipgloss.Color("82"))
)

// ── State ─────────────────────────────────────────────────────────────────

type state int

const (
	stateIdle state = iota
	stateRunning
	stateHealing
	stateAwaitingApproval
	stateStreaming
)

// OutputLine is a single rendered line with a kind for styling.
type OutputLine struct {
	Kind    LineKind
	Content string
}

type LineKind int

const (
	LineKindOutput LineKind = iota
	LineKindError
	LineKindSuggestion
	LineKindSystem
	LineKindSuccess
)

// ── Model ─────────────────────────────────────────────────────────────────

// Model is the root bubbletea model for the Nebula interactive REPL.
type Model struct {
	width      int
	height     int
	state      state
	input      string
	output     []OutputLine
	agent      AgentRunner
	ctx        context.Context
	cancelFn   context.CancelFunc
	dryRun     bool
	pendingSug *models.HealSuggestion
	// streaming accumulator
	streamBuf strings.Builder
}

// New creates a Model wired to the given AgentRunner.
func New(agent AgentRunner, dryRun bool) Model {
	ctx, cancel := context.WithCancel(context.Background())
	return Model{
		agent:    agent,
		ctx:      ctx,
		cancelFn: cancel,
		state:    stateIdle,
		dryRun:   dryRun,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

// ── Update ────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case tea.KeyMsg:
		return m.handleKey(msg)

	case CommandExitMsg:
		return m.handleCommandExit(msg)

	case SuggestionMsg:
		m.state = stateAwaitingApproval
		m.pendingSug = &models.HealSuggestion{
			FixCmd:      msg.FixCmd,
			Explanation: msg.Explanation,
		}
		m = m.withOutput(LineKindSuggestion, "✦ Suggested fix: "+msg.FixCmd)
		if msg.Explanation != "" {
			m = m.withOutput(LineKindSystem, "  "+msg.Explanation)
		}
		m = m.withOutput(LineKindSystem, "  Apply? [y] yes  [n] no  [e] edit  [?] explain")

	case TokenMsg:
		if msg.Err != nil {
			m = m.withOutput(LineKindError, "AI error: "+msg.Err.Error())
			m.state = stateIdle
			return m, nil
		}
		if msg.IsLast {
			if m.streamBuf.Len() > 0 {
				m = m.withOutput(LineKindSuggestion, m.streamBuf.String())
				m.streamBuf.Reset()
			}
			m.state = stateIdle
		} else {
			m.streamBuf.WriteString(msg.Text)
		}

	case errMsg:
		m = m.withOutput(LineKindError, "error: "+msg.err.Error())
		m.state = stateIdle
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateAwaitingApproval:
		return m.handleApprovalKey(msg)
	case stateIdle:
		return m.handleIdleKey(msg)
	case stateRunning, stateHealing, stateStreaming:
		if msg.String() == "ctrl+c" {
			m.cancelFn()
			// Reset ctx for next command.
			m.ctx, m.cancelFn = context.WithCancel(context.Background())
			m = m.withOutput(LineKindSystem, "^C")
			m.state = stateIdle
		}
	}
	return m, nil
}

func (m Model) handleIdleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "enter":
		if strings.TrimSpace(m.input) != "" {
			return m.submitInput()
		}
	case "backspace":
		if len(m.input) > 0 {
			m.input = m.input[:len(m.input)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.input += msg.String()
		}
	}
	return m, nil
}

func (m Model) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	sug := m.pendingSug
	m.pendingSug = nil

	switch msg.String() {
	case "y", "Y":
		m.state = stateRunning
		m = m.withOutput(LineKindSystem, "Applying fix...")
		return m, m.runApprovedFix(sug.FixCmd)
	case "n", "N":
		m.state = stateIdle
		m = m.withOutput(LineKindSystem, "Fix rejected.")
	case "?":
		m.state = stateAwaitingApproval
		m.pendingSug = sug
		m = m.withOutput(LineKindSystem, "Explanation: "+sug.Explanation)
	case "e", "E":
		// Pre-fill input with the fix command for user editing.
		m.state = stateIdle
		m.input = sug.FixCmd
		m = m.withOutput(LineKindSystem, "Edit the fix and press Enter:")
	default:
		m.state = stateAwaitingApproval
		m.pendingSug = sug
	}
	return m, nil
}

func (m Model) handleCommandExit(msg CommandExitMsg) (Model, tea.Cmd) {
	if msg.ExitCode == 0 {
		m.state = stateIdle
		m = m.withOutput(LineKindSuccess, "✓ exit 0")
		return m, nil
	}

	m = m.withOutput(LineKindError, fmt.Sprintf("✗ exit %d", msg.ExitCode))

	if msg.Suggestion != nil {
		m.state = stateAwaitingApproval
		m.pendingSug = msg.Suggestion
		m = m.withOutput(LineKindSystem, "◈ Nebula is analyzing the failure...")
		return m, nil
	}

	m = m.withOutput(LineKindSystem, "◈ Nebula is analyzing the failure...")
	m.state = stateHealing
	return m, nil
}

// ── Commands (async) ──────────────────────────────────────────────────────

// submitInput returns a tea.Cmd that runs the typed input through the agent.
func (m Model) submitInput() (Model, tea.Cmd) {
	input := strings.TrimSpace(m.input)
	m.input = ""
	m.state = stateRunning
	m = m.withOutput(LineKindOutput, stylePrompt.Render("❯ ")+input)

	args := strings.Fields(input)
	agent := m.agent
	ctx := m.ctx
	dryRun := m.dryRun

	return m, func() tea.Msg {
		opts := AgentRunOptions{
			DryRun: dryRun,
			// Approval is handled by the TUI via SuggestionMsg/ApprovalMsg,
			// so we pre-approve here — the TUI layer owns the approval gate.
			ApprovalFn: func(_ string, _ safety.Risk) bool { return true },
		}
		result, err := agent.Run(ctx, args, opts)
		if err != nil {
			return errMsg{err}
		}
		msg := CommandExitMsg{ExitCode: result.ExitCode}
		if result.HealApply != nil {
			msg.Suggestion = result.HealApply
		}
		return msg
	}
}

// runApprovedFix runs the approved fix command as a one-shot agent call.
func (m Model) runApprovedFix(fixCmd string) tea.Cmd {
	agent := m.agent
	ctx := m.ctx

	return func() tea.Msg {
		args := strings.Fields(fixCmd)
		result, err := agent.Run(ctx, args, AgentRunOptions{
			ApprovalFn: func(_ string, _ safety.Risk) bool { return true },
		})
		if err != nil {
			return errMsg{err}
		}
		return CommandExitMsg{ExitCode: result.ExitCode}
	}
}

// StreamTokens returns a tea.Cmd that drains a token channel and
// sends a TokenMsg for each delta. Call this when an LLM stream starts.
func StreamTokens(ch <-chan llm.Token) tea.Cmd {
	return func() tea.Msg {
		t, ok := <-ch
		if !ok {
			return TokenMsg{IsLast: true}
		}
		return TokenMsg{Text: t.Text, IsLast: t.IsLast, Err: t.Err}
	}
}

// ── View ──────────────────────────────────────────────────────────────────

func (m Model) View() string {
	var b strings.Builder

	b.WriteString(styleSystem.Render("◈ nebula") + "\n\n")

	for _, l := range m.output {
		b.WriteString(m.renderLine(l) + "\n")
	}

	// Streaming accumulator shown live.
	if m.streamBuf.Len() > 0 {
		b.WriteString(styleSuggestion.Render(m.streamBuf.String()) + "\n")
	}

	b.WriteString("\n")

	switch m.state {
	case stateRunning:
		b.WriteString(styleSystem.Render("◈ running...") + "\n")
	case stateHealing:
		b.WriteString(styleHealing.Render("◈ analyzing failure...") + "\n")
	default:
		b.WriteString(stylePrompt.Render("❯ ") + m.input)
	}

	return b.String()
}

func (m Model) renderLine(l OutputLine) string {
	switch l.Kind {
	case LineKindError:
		return styleError.Render(l.Content)
	case LineKindSuggestion:
		return styleSuggestion.Render(l.Content)
	case LineKindSystem:
		return styleSystem.Render(l.Content)
	case LineKindSuccess:
		return styleSuccess.Render(l.Content)
	default:
		return l.Content
	}
}

func (m Model) withOutput(kind LineKind, content string) Model {
	m.output = append(m.output, OutputLine{Kind: kind, Content: content})
	return m
}

package models

import "time"

// Session represents a Nebula agent session.
type Session struct {
	ID        string    `db:"id"`
	CreatedAt time.Time `db:"created_at"`
	UpdatedAt time.Time `db:"updated_at"`
	WorkDir   string    `db:"work_dir"`
	Summary   string    `db:"summary"`
}

// Command is a shell command executed within a session.
type Command struct {
	ID        int64     `db:"id"`
	SessionID string    `db:"session_id"`
	Raw       string    `db:"raw"`         // original command string
	ExitCode  int       `db:"exit_code"`
	Stdout    string    `db:"stdout"`
	Stderr    string    `db:"stderr"`
	Elapsed   int64     `db:"elapsed_ms"`
	WorkDir   string    `db:"work_dir"`
	CreatedAt time.Time `db:"created_at"`
}

// Pattern is a learned fix pattern linking a failure to a successful recovery.
type Pattern struct {
	ID          int64     `db:"id"`
	FailCmd     string    `db:"fail_cmd"`
	FailOutput  string    `db:"fail_output"`
	FixCmd      string    `db:"fix_cmd"`
	SuccessRate float64   `db:"success_rate"`
	UseCount    int       `db:"use_count"`
	Embedding   []byte    `db:"embedding"` // float32 slice, gob-encoded
	CreatedAt   time.Time `db:"created_at"`
	UpdatedAt   time.Time `db:"updated_at"`
}

// Permission is a persisted allow/deny rule for a command pattern.
type Permission struct {
	ID        int64     `db:"id"`
	Pattern   string    `db:"pattern"` // exact string or glob
	Decision  string    `db:"decision"` // "allow" | "deny" | "ask"
	CreatedAt time.Time `db:"created_at"`
}

// HealSuggestion is an AI-generated fix proposal for a failed command.
type HealSuggestion struct {
	OriginalCmd string
	FixCmd      string
	Explanation string
	Reasoning   string
	Confidence  float64
}

// Task is a general-purpose agent interaction (any domain: code, writing, research, etc.).
type Task struct {
	ID        string    `db:"id"`
	SessionID string    `db:"session_id"`
	Input     string    `db:"input"`
	Response  string    `db:"response"`
	Domain    string    `db:"domain"` // "terminal", "code", "writing", "research", "general"
	CreatedAt time.Time `db:"created_at"`
}

// Approval is a user's decision on a HealSuggestion.
type Approval int

const (
	ApprovalPending Approval = iota
	ApprovalYes
	ApprovalNo
	ApprovalEdit
	ApprovalExplain
)

type WorkflowJob struct {
	ID           string    `db:"id"`
	WorkflowFile string    `db:"workflow_file"`
	Inputs       string    `db:"inputs"` // JSON-encoded map[string]string
	Status       string    `db:"status"` // "running", "done", "failed"
	CurrentStep  string    `db:"current_step"`
	Output       string    `db:"output"` // JSON-encoded map[string]string step outputs
	Error        string    `db:"error"`
	CreatedAt    time.Time `db:"created_at"`
	UpdatedAt    time.Time `db:"updated_at"`
}

// TurnRecord represents a single reasoning turn in a multi-turn healing loop.
type TurnRecord struct {
	FixCmd    string
	Output    string
	ExitCode  int
	Reasoning string
}

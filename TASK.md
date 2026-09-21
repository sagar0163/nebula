# Nebula — AI Agent Task Brief

Use this document to hand any AI CLI a well-scoped task on the Nebula project.
Copy the relevant section, paste it as the prompt, and the agent should be able to complete it independently.

---

## Project Context (include this in every task)

**Repo:** `/home/sagar-jadhav/Documents/nebula` (SSH remote: `git@github.com:sagar0163/nebula.git`)
**Language:** Go 1.22+, binary at `~/.local/bin/nebula`
**Build:** `cd /home/sagar-jadhav/Documents/nebula && /home/sagar-jadhav/go/bin/go build ./...`
**Install:** `/home/sagar-jadhav/go/bin/go build -o ~/.local/bin/nebula ./cmd/nebula`
**Test build:** always run build + vet before committing
**Git push:** use SSH (already configured), `git push origin main`

**Hard rules (never break these):**
- API keys are NEVER written to disk — always OS keyring via `go-keyring` (service: `nebula`)
- Keyring keys: `groq_api_key`, `groq_api_key_2`...`_9`, same pattern for `gemini`, `mistral`, `nvidia`
- Do NOT change how keys are loaded in `internal/cli/root.go` `loadKeys()` function
- Follow existing code style: minimal comments, short functions, no unnecessary abstractions
- Single binary — no external runtime dependencies

**Key files:**
- `internal/agent/agent.go` — core agent: `Run()` (terminal healing) + `Ask()` (general tasks)
- `internal/llm/router.go` — LLM router, tries providers in registration order
- `internal/memory/store.go` + `memory.go` — SQLite store interface + implementation
- `internal/memory/migrations/` — goose SQL migrations (add new ones here)
- `internal/models/models.go` — shared domain types
- `internal/skills/skills.go` — skill loader from `~/.config/nebula/skills/`
- `internal/workflow/workflow.go` — workflow runner (sync + background)
- `internal/cli/commands.go` — all cobra subcommands
- `internal/cli/root.go` — root command, `buildAgent()`, `loadKeys()`
- `~/.config/nebula/config.toml` — user config (gitignored)
- `~/.config/nebula/skills/` — skill markdown files
- `~/.config/nebula/workflows/` — workflow YAML files

**Current commands:**
```
nebula                          interactive TUI
nebula run <cmd>                run command with auto-healing
nebula ask <prompt>             general AI task (code/writing/research/general)
nebula skill list               list skills
nebula skill show <name>        print skill instructions
nebula workflow run <file>      run workflow (sync)
nebula workflow run --background <file> [k=v...]  run in background
nebula workflow status <id>     check background job
nebula workflow result <id>     get background job output
nebula key list                 list stored API keys (masked)
nebula key add <provider> <key> add key to next free slot
nebula key remove <provider> <n> remove key from slot n
nebula session list             list past sessions
nebula setup                    first-run config wizard
```

---

## Open Tasks

### TASK-001: Doom-loop prevention
**Description:** When the same command fails with the same error 3 times in a row and the same fix is suggested each time without success, Nebula should stop auto-retrying and print a clear escalation message instead of looping forever.

**Details:**
- Fingerprint each (command, error-hash) pair — use `fmt.Sprintf("%s:%x", cmd, sha256(stderr)[:8])`
- Track attempt count in memory (can be in-process map, doesn't need to persist)
- After 3 failed fix attempts for the same fingerprint, set a `doomLoopBlocked` flag and return an error: `"healing loop detected after 3 attempts — manual intervention required"`
- Add a `DoomLoopCount` field to `RunResult` so the caller can display it
- File to change: `internal/agent/agent.go`
- No new dependencies needed

---

### TASK-002: Proactive butler mode (background daemon)
**Description:** Add a `nebula watch` command that runs as a background daemon, monitors a directory for new workflow YAML files dropped into `~/.config/nebula/queue/`, and runs them automatically.

**Details:**
- New command: `nebula watch` — polls `~/.config/nebula/queue/` every 10s
- When a `.yaml` file appears, run it as a background workflow job (`wf.RunBackground()`), then move it to `~/.config/nebula/queue/done/`
- Print a line when a job starts and when it finishes
- Runs until Ctrl+C (handle SIGINT/SIGTERM cleanly)
- Uses `fsnotify` if already in go.mod, otherwise use a simple polling loop
- New file: `internal/daemon/watch.go`
- Add `newWatchCmd()` to `internal/cli/commands.go` and register in `root.go`

---

### TASK-003: Planner / Executor split
**Description:** Formalize the separation between the LLM that plans a fix and the executor that runs it. Currently `diagnose()` and execution are in the same flow. Split them so the Planner only reads/searches and the Executor only runs pre-approved steps.

**Details:**
- Add a `Planner` struct in `internal/agent/planner.go` with method `Plan(ctx, failCmd, output string) (*models.HealSuggestion, error)` — wraps current `diagnose()` logic
- Add an `Executor` struct in `internal/agent/executor.go` with method `Execute(ctx, suggestion *models.HealSuggestion, approvalFn func(string, safety.Risk) bool) error` — wraps the fix execution + learn pattern logic
- Refactor `agent.Run()` to use `Planner.Plan()` then `Executor.Execute()` instead of inline calls
- No behaviour change — pure refactor, all existing tests/builds must still pass
- Update `internal/agent/agent.go` to wire them together

---

### TASK-004: Skill creation via CLI
**Description:** Add `nebula skill create <name>` which opens an interactive prompt to write a new skill and saves it to `~/.config/nebula/skills/<name>.md`.

**Details:**
- New subcommand under `newSkillCmd()`: `create <name>`
- Prompt for: description (single line), instructions (multi-line, end with empty line or Ctrl+D)
- Write the file in the standard frontmatter format:
  ```
  ---
  description: <user input>
  ---
  <instructions>
  ```
- Print: `Skill saved to ~/.config/nebula/skills/<name>.md`
- File to change: `internal/cli/commands.go`
- No new dependencies — use `bufio.Scanner` for multi-line input

---

### TASK-005: Workflow list command
**Description:** Add `nebula workflow list` to show all past background workflow jobs with their status.

**Details:**
- New subcommand under `newWorkflowCmd()`: `list`
- Calls `store.ListWorkflowJobs(ctx, 20)`
- Output format:
  ```
  ID                                    STATUS   STEP         STARTED
  ----                                  ------   ----         -------
  abc-123...                            done                  2026-09-21 10:30
  def-456...                            running  write        2026-09-21 11:00
  ```
- File to change: `internal/cli/commands.go`

---

## How to use this document

1. Pick a task
2. Prepend the **Project Context** section to the task description
3. Add: *"Do all work yourself in one session. Run `go build ./...` and `go vet ./...` after changes. Commit and push when done."*
4. Paste as the prompt to your AI CLI

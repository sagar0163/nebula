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

### TASK-010: Bound PTY output capture — prevent OOM on high-volume commands
**Severity:** critical
**Description:** `pty.go:63` captures all PTY output into an unbounded `bytes.Buffer`. A command like `yes` or a build that dumps megabytes will grow the buffer until OOM. The capture buffer must be capped (e.g. keep only the last 512KB), separate from the ring buffer already in `Harness`.

**Details:**
- File: `internal/pty/pty.go`
- Replace `var capture bytes.Buffer` with a ring/capped buffer that keeps only the last N bytes (512KB default)
- `CommandResult.Stdout` should return only the capped portion
- Add a config option for the cap size (default 512KB)
- Add a test: run a command producing >1MB output, verify `Stdout` length is capped

---

### TASK-011: Add timeout to all LLM streaming calls
**Severity:** critical
**Description:** `router.go:44` calls `provider.Complete()` with no deadline. A hung or slow provider hangs the CLI forever — no spinner, no timeout, no fallback.

**Details:**
- File: `internal/llm/router.go`
- Wrap each provider call with a per-provider timeout (default 60s, configurable via `~/.config/nebula/config.toml`)
- On timeout, log the provider name and fall through to the next provider
- Add a test using a slow mock provider that never responds

---

### TASK-012: Handle SIGINT — restore terminal and kill child process group
**Severity:** high
**Description:** Ctrl-C during `nebula run` kills nebula but leaves the child running and the terminal in raw mode. User has to type `reset`. `defer term.Restore` does not run on SIGINT.

**Details:**
- File: `internal/cli/root.go` + `internal/pty/pty.go`
- Use `signal.NotifyContext` to catch SIGINT/SIGTERM
- On signal: send SIGTERM to the child's process group (`syscall.Kill(-pid, syscall.SIGTERM)`)
- Ensure `term.Restore` runs via defer before exit
- Test: verify terminal fd is restored after a simulated signal

---

### TASK-013: Fix raw mode crash on non-TTY stdin (pipes and CI)
**Severity:** high
**Description:** `pty.go:53` calls `term.MakeRaw(os.Stdin.Fd())` unconditionally. When stdin is a pipe (`echo x | nebula run build`) it fails with "inappropriate ioctl" — completely breaking scripted and CI use.

**Details:**
- File: `internal/pty/pty.go`
- Before calling `term.MakeRaw`, check `term.IsTerminal(int(os.Stdin.Fd()))`
- If not a terminal: skip raw mode and stdin forwarding entirely, run with passthrough
- Add a test that runs a command with piped stdin (non-TTY)

---

### TASK-014: Fix `find -exec` safety classifier bypass
**Severity:** high
**Description:** `safety.go:90-94` classifies commands starting with `find` as read-only/safe. `find /etc -exec rm -rf {} \;` passes as `RiskSafe` — dangerous commands auto-approved.

**Details:**
- File: `internal/safety/safety.go`
- Commands with `find` + `-exec`, `-execdir`, `-delete` must be classified `RiskHigh` or `RiskDangerous`
- Check for `-exec` and `-delete` flags in the full command string, not just the binary name
- Add tests covering `find / -exec rm {} \;`, `find . -delete`, `find /tmp -execdir sh \;`

---

### TASK-015: Fix executor tokenization — quoted args break on spaces
**Severity:** high
**Description:** `executor.go:33` uses `strings.Fields(suggestion.FixCmd)` to split the fix command. A fix like `git commit -m "fix the bug"` becomes `["git", "commit", "-m", "\"fix", "the", "bug\""]` — wrong args, broken execution.

**Details:**
- File: `internal/agent/executor.go`
- Replace `strings.Fields` with a proper shell-word tokenizer (use `github.com/google/shlex` or implement a simple quoted-string splitter)
- Keep the metacharacter rejection — quoted args are fine, shell operators are not
- Add tests: `git commit -m "fix the bug"`, `echo 'hello world'`, `python -c "print(1)"`

---

### TASK-016: Fix harness ring size 0 — AI transcript feature is inert
**Severity:** high
**Description:** `root.go:211` calls `pty.NewHarness(0)`. Every write trims the ring to 0 bytes so `Transcript()` always returns `""`. The "[rolling transcript for AI context]" the package documents is completely unused.

**Details:**
- File: `internal/cli/root.go`
- Change `pty.NewHarness(0)` to `pty.NewHarness(256 * 1024)` (256KB)
- Wire `harness.Transcript()` into the planner's diagnose prompt so the agent sees recent terminal context when suggesting a fix
- File: `internal/agent/planner.go` — add transcript as optional context in `buildDiagnosePrompt`

---

### TASK-017: Implement workload-aware model selection
**Severity:** high
**Description:** `WorkloadDiagnose`, `WorkloadLearn`, `WorkloadEmbed` are defined but every provider's `selectModel()` only checks `cfg.ModelHeal`. The router's workload concept has zero effect — all calls use the same model.

**Details:**
- Files: `internal/llm/providers/groq.go`, `gemini.go`, `mistral.go`, `nvidia.go`, `ollama.go`
- Update `selectModel()` in each provider to pick `cfg.ModelDiagnose` for `WorkloadDiagnose`, `cfg.ModelLearn` for `WorkloadLearn`, falling back to `cfg.ModelHeal`
- Thread `Workload` from `Request` into the provider's model selection
- Add defaults in config for diagnose (faster/cheaper model) vs heal (stronger model)

---

### TASK-018: Surface keyring errors — fix silent "no provider" failures
**Severity:** medium
**Description:** `root.go:106-114` does `k, _ := keyring.Get(...)` — errors silently discarded. On headless Linux (no DBus/libsecret), every key lookup fails and the user gets only "no available LLM provider" with no hint why.

**Details:**
- File: `internal/cli/root.go` `loadKeys()`
- Collect keyring errors per provider
- If a provider has zero keys loaded AND had keyring errors, print a warning: `"groq: keyring unavailable (no DBus session?) — set NEBULA_GROQ_KEY or run nebula key add"`
- Also accept keys from env vars as fallback (`NEBULA_GROQ_KEY`, `NEBULA_GEMINI_KEY`, etc.)

---

### TASK-019: Add panic recovery to background workflow goroutines
**Severity:** high
**Description:** `workflow.go:113` launches background jobs in a goroutine with no `recover()`. A panic in any workflow step crashes the entire nebula process.

**Details:**
- File: `internal/workflow/workflow.go`
- Add `defer func() { if r := recover(); r != nil { ... store error in job } }()` at the top of the background goroutine
- Store the panic as the job's error string so `nebula workflow result <id>` surfaces it
- Add a test: workflow step that panics → job status is "failed", result contains panic message

---

### TASK-006: Fix safety bypass in Executor
**Description:** `Executor.Execute()` hardcodes `safety.RiskMedium` when calling `approvalFn`, bypassing the safety classifier entirely. An LLM-suggested fix like `rm -rf /` would be approved as medium-risk. It must call `safety.Classify()` on the fix command first.

**Details:**
- File to change: `internal/agent/executor.go`
- In `Execute()`, replace the hardcoded `safety.RiskMedium` with `safety.Classify(suggestion.FixCmd)`
- Also note: the fix is run via `sh -c` which is in `dangerousVectors` in `safety.go` — change execution to split the fix command into args and run it directly via `harness.Run()` instead of wrapping in `sh -c`. Use `strings.Fields(suggestion.FixCmd)` to split. If the fix command is complex (pipes, redirects), reject it and return an error rather than silently allowing shell injection.
- Add a test in `internal/agent/agent_test.go` covering a dangerous fix command being rejected

---

### TASK-007: Stream LLM output to terminal in `Ask()`
**Description:** `agent.Ask()` collects all tokens silently then returns the full string. For long responses the user sees a blank terminal for 10-20 seconds. Stream tokens to stdout as they arrive, then also return the full string for callers that need it.

**Details:**
- File to change: `internal/agent/agent.go` — `Ask()` method
- As tokens arrive in the `for t := range tokens` loop, write each `t.Text` to `os.Stdout` immediately (no buffering)
- The method signature stays the same — still returns `(string, error)`; accumulate the full string in parallel with writing
- Also change `internal/workflow/workflow.go` `Run()` and `RunBackground()` — those call `a.Ask()` and should NOT print to stdout (workflow steps are piped into each other). Add a `stream bool` field to `RunOptions` or add a separate `AskQuiet()` method that skips stdout. Workflow always uses the quiet path.
- File to change: `internal/cli/commands.go` — the `nebula ask` handler already calls `Ask()` and prints the result; remove the final print since streaming will handle output

---

### TASK-008: Fix dead `recallPattern` / embedding path
**Description:** `Planner.recallPattern()` calls `router.Embed()` but no provider implements `Embed()` — they all return an error, so the semantic memory feature silently does nothing. Either wire up a real embedding provider or remove the dead code path and replace with simpler exact-match recall.

**Details:**
- Check `internal/llm/providers/*.go` — none implement a working `Embed()` method
- Option A (preferred, simpler): replace `recallPattern` with exact-string lookup — query `store.FindSimilarPatterns` by the raw `failCmd` string match instead of vector similarity. Remove the `Embedding` field usage from `models.Pattern` and the `encodeEmbeddingText`/`embedText` helpers in `planner.go`.
- Option B: implement a real embedding call in one provider (e.g. Groq or Ollama support embeddings) and wire it through `router.Embed()`
- Whichever option is chosen, add a test proving `recallPattern` actually returns a result on a second identical failure
- Files to change: `internal/agent/planner.go`, possibly `internal/llm/providers/*.go`, `internal/memory/store.go`

---

### TASK-009: Fix `detectDomain()` ordering and false matches
**Description:** `detectDomain()` checks `writingKeywords` before `codeKeywords`, so "write a node script" routes to the writing assistant instead of code. Also "write" is too broad — it matches every prompt that starts with "write me a…" regardless of subject.

**Details:**
- File to change: `internal/agent/agent.go`
- Reorder the switch cases: check `terminal` → `code` → `research` → `writing` → `general`. Code prompts are more common and more distinct; writing should be last.
- Remove `"write"` from `writingKeywords` — it's too generic. Keep specific terms like `"draft"`, `"essay"`, `"prose"`, `"novel"`, `"poem"`, `"copywriting"`.
- Add `"write a"`, `"write me a"` as code/general keywords only if they appear with a technical noun, but don't try to be clever — just fix the ordering and prune the false-match word.
- Update `TestDetectDomain` in `internal/agent/agent_test.go` to add a case: `"write a python script"` → `"code"` (currently fails)

---

## How to use this document

1. Pick a task
2. Prepend the **Project Context** section to the task description
3. Add: *"Do all work yourself in one session. Run `go build ./...` and `go vet ./...` after changes. Commit and push when done."*
4. Paste as the prompt to your AI CLI

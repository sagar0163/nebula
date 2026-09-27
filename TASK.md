# Nebula — Complete Harness Upgrade Plan

## What We're Building

Nebula is being upgraded from a self-healing terminal agent into a **full autonomous coding agent** — on par with Claude Code, OpenCode, and Antigravity Codex. The goal: give it any GitHub issue or natural-language task, and it finds the files, writes the fix, runs the tests, and self-corrects until green.

**Current resolve rate: ~0%** (harness scaffolded but not wired end-to-end)
**Target: 30%+ SWE-bench Verified** (competitive with top open-source agents)

---

## What's Already Built ✅

| Component | Location | Status |
|---|---|---|
| PTY harness | `internal/pty/` | Done |
| LLM router (Groq/NVIDIA/Mistral/Gemini/Ollama) | `internal/llm/` | Done |
| `nebula run` self-heal loop | `internal/agent/agent.go` | Done |
| `nebula do` ReAct loop (15 iter, 5 tools) | `internal/agent/goal_executor.go` | Done |
| Harness Orchestrator skeleton | `internal/harness/orchestrator.go` | Scaffolded |
| PlannerAgent skeleton | `internal/harness/agents/agents.go` | Scaffolded |
| ExecutorAgent skeleton | `internal/harness/agents/agents.go` | Stub only |
| VerifierAgent skeleton | `internal/harness/agents/agents.go` | Stub only |
| Shared types + ToolCall structs | `internal/harness/shared/shared.go` | Done |
| ContextManager (hierarchical context) | `internal/harness/contextpkg/manager.go` | Done |
| Tool registry + ReadFile/WriteFile/Shell | `internal/harness/tools/tools.go` | Scaffolded |
| Trajectory struct | `internal/harness/shared/shared.go` | Done |
| SWE-bench command | `internal/swebench/command.go` | Done |
| Patch extraction | `internal/swebench/patch.go` | Basic |
| Go test runner | `internal/swebench/testrunner/go_runner.go` | Done |
| Test output parser | `internal/swebench/testrunner/parser.go` | Done |

---

## Gap Analysis — What's Missing

| Area | Current State | What's Needed | Priority |
|---|---|---|---|
| Executor is a stub | Returns `TODO` as path, never reads/writes real files | Real multi-step: read → analyze → write → lint loop | 🔴 Critical |
| Verifier is a stub | Returns a fake tool call, never runs actual tests | Real: apply patch → run failing tests → parse output → return error trace | 🔴 Critical |
| Executor↔Verifier retry loop | Not implemented | Fail → feed error to Executor → re-edit → retry (max 3) | 🔴 Critical |
| Repo indexer | `buildRepoSummary()` returns `"Repository at <dir>"` (stub) | Real: file tree + language detection + key symbol extraction | 🔴 Critical |
| Harness not wired to `nebula do` / `nebula fix` | Old single-pass DoGoal still used | Wire `internal/harness/Orchestrator` into CLI commands | 🔴 Critical |
| Python/JS/TS test runners | Only Go runner exists | pytest + npm test + jest runners for SWE-bench instances | 🔴 Critical |
| Patch extraction via git diff | `patch.go` basic — no `git apply --check` validation | Replace with `git diff --no-color` + validation before applying | 🟡 High |
| search_code tool | Not in harness tool registry | `grep -rn` / ripgrep wrapper, returns file:line:context | 🟡 High |
| Critic agent | Commented out (`// critic`) | LLM review of final patch for correctness + minimality | 🟢 Medium |
| Checkpoint/resume | In-memory only — crash = restart from zero | JSONL trajectory write after each step | 🟡 High |
| Parallelism | Sequential instances | `--concurrency N` flag, goroutine-per-instance | 🟡 High |
| Observability | No logging | Structured JSONL log + token/cost tracking per phase | 🟡 High |
| Symbol index (LSP/ctags) | None | ctags integration for go_to_definition + call graph | 🟢 Medium |
| Vector DB for RAG | Interface exists, no implementation | sqlite-vec for code embeddings + semantic file search | 🟢 Medium |
| Groq model config | `model_diagnose = "openai/gpt-oss-120b"` (invalid) | Fix to a real Groq model ID e.g. `llama-3.1-70b-versatile` | 🔴 Critical |
| JSON parse retry | Crashes on truncated NVIDIA response | Retry up to 3× with exponential backoff in goal_planner | 🔴 Critical |

---

## Phase 1: Make the Harness Actually Work (Week 1)

These are blockers — the harness does nothing useful until these are done.

### TASK-070: Fix Groq config model name
- **File:** `~/.config/nebula/config.toml`
- Change `llm.groq.model_diagnose` from `"openai/gpt-oss-120b"` to `"llama-3.1-70b-versatile"`
- This is why every Groq call fails immediately

### TASK-071: Add JSON parse retry in goal_planner
- **File:** `internal/agent/goal_planner.go`
- Wrap `planner.Plan()` call with retry loop (max 3, 2s backoff)
- Retry on: `unexpected end of JSON input`, `failed to parse`, any JSON unmarshal error
- This fixes the NVIDIA truncated response crash

### TASK-072: Implement real ExecutorAgent
- **File:** `internal/harness/agents/agents.go` → `ExecutorAgent.Execute()`
- Replace the `TODO` stub with a real multi-step loop:
  1. Parse plan steps from `input.Context`
  2. For each file in plan: `read_file` → build edit prompt → `write_file` with fix → `run_command` (lint)
  3. After all edits: `git diff` to extract unified patch
  4. Return patch in `AgentOutput.Context`
- Use the tool registry in `internal/harness/tools/tools.go`

### TASK-073: Implement real VerifierAgent
- **File:** `internal/harness/agents/agents.go` → `VerifierAgent.Execute()`
- Replace the stub:
  1. Apply the patch from `input.Context` via `git apply`
  2. Run failing tests only (fast path) using `testrunner`
  3. If fail: return `{passed: false, error_trace: "..."}` so Orchestrator can loop back
  4. If pass: run full suite
- Return structured `VerificationResult`

### TASK-074: Wire Executor↔Verifier retry loop in Orchestrator
- **File:** `internal/harness/orchestrator.go`
- After Executor returns patch, pass to Verifier
- If Verifier returns `passed: false`:
  - Append error trace to Executor's context
  - Re-run Executor (max 3 attempts)
- Only mark trajectory resolved when Verifier passes or max retries hit

### TASK-075: Implement real repo summarizer
- **File:** `internal/harness/orchestrator.go` → `buildRepoSummary()`
- Walk `workDir`, collect: file tree (depth 3), detect primary language, find test dirs
- Output compact summary string fed to PlannerAgent

### TASK-076: Add Python test runner
- **File:** `internal/swebench/testrunner/python_runner.go`
- Detect: pytest vs unittest
- Run: `conda activate testbed && python -m pytest <tests> -x --tb=short -q`
- Parse: use existing `parser.go` patterns + add pytest-specific output parsing
- Return `TestResult{Passed, Failed []string, Duration}`

### TASK-077: Wire harness Orchestrator into `nebula do` and `nebula fix`
- **File:** `internal/cli/commands.go` (do/fix commands)
- When `--harness` flag passed (or by default): use `harness.NewOrchestrator` instead of old `goal_executor.go`
- Old path stays as fallback with `--legacy` flag

---

## Phase 2: Make It Smart (Week 2)

### TASK-078: Real PlannerAgent prompt + structured output
- **File:** `internal/harness/agents/agents.go` → `PlannerAgent.buildPrompt()`
- Current prompt is generic. Upgrade to:
  - Include actual file tree from ContextManager
  - Request structured JSON: `{files_to_read, files_to_edit, failing_tests, approach}`
  - Use `search_code` tool call to find 3-5 candidate files before planning
- Add `search_code` to tool registry

### TASK-079: Add `search_code` tool to harness
- **File:** `internal/harness/tools/tools.go`
- Implement `SearchCodeTool`: `grep -rn --include="*.py" <pattern> <dir>`
- Returns: `[]Match{File, Line, Content}`
- Register in `NewDefaultRegistry()`

### TASK-080: Patch extraction via `git diff`
- **File:** `internal/swebench/patch.go`
- Replace current extraction with:
  1. `git stash` before agent runs (capture clean state)
  2. After agent writes files: `git diff --no-color` to get unified diff
  3. Validate with `git apply --check` on a temp branch
  4. Filter: reject patches that touch test files (unless explicitly required)

### TASK-081: Checkpoint/resume (trajectory JSONL)
- **File:** `internal/harness/orchestrator.go` + new `internal/harness/trajectory.go`
- After each step, append to `<workDir>/.nebula/trajectory.jsonl`
- On start, check if checkpoint exists → resume from last step
- Store: step phase, tool calls + results, token usage, timestamp

### TASK-082: JS/TS test runner
- **File:** `internal/swebench/testrunner/js_runner.go`
- Detect: jest vs mocha vs vitest via `package.json`
- Run: `npm test -- --testPathPattern=<test>` or `npx jest <test>`
- Parse output for pass/fail counts

---

## Phase 3: Production Quality (Week 3)

### TASK-083: `--concurrency N` parallel instances
- **File:** `internal/swebench/runner.go`
- Run N instances simultaneously via goroutines + `errgroup`
- Per-instance container isolation already exists (Docker)
- Default: `min(4, nCPU/2)`

### TASK-084: Structured observability
- **File:** new `internal/harness/logger.go`
- JSONL log line per step: `{instance_id, phase, tool, duration_ms, tokens_in, tokens_out, result}`
- Summary at end: resolve rate, avg cost, avg time, failure breakdown by category
- Categories: `MODEL_ERROR`, `PATCH_INVALID`, `TEST_INFRA`, `TIMEOUT`, `RESOLVED`

### TASK-085: Critic agent (patch quality review)
- **File:** `internal/harness/agents/agents.go` → new `CriticAgent`
- Uncomment critic in orchestrator
- Prompt: "Review this patch. Flag: test file modifications, unrelated changes, security issues"
- Output: `CriticResult{Approved bool, Warnings []string}`
- Only block on security issues; warnings are logged

### TASK-086: Symbol index (ctags)
- **File:** new `internal/harness/index/index.go`
- Run `ctags -R --output-format=json <dir>` → parse into `map[symbol]FileLocation`
- Expose `go_to_definition` tool: input symbol name → returns file:line
- Used by ExecutorAgent to navigate to the exact function to edit

---

## Phase 4: Advanced (Month 2+)

### TASK-087: Vector DB for semantic file search
- Implement `VectorStore` interface in `internal/harness/contextpkg/manager.go`
- Use `sqlite-vec` (pure Go, no external deps)
- Embed files using LLM embed workload (already wired in router)
- PlannerAgent uses semantic search instead of grep for file selection

### TASK-088: Self-improving memory
- Store `(issue_pattern, patch_summary, resolved: bool)` in SQLite memory
- On new issue: retrieve top-3 similar past fixes → inject as few-shot examples to Planner
- Use existing `internal/memory/` store

### TASK-089: Multi-model consensus (optional)
- Run same execution with 2 providers
- If both produce same patch → high confidence, skip Critic
- If different → use Critic to pick better one

---

## Architecture (Current → Target)

```
CURRENT (broken):
  nebula do "goal"
    → goal_executor.go (15-iter ReAct loop)
    → goal_planner.go (JSON parse crashes on NVIDIA truncation)
    → tools: Shell/Read/Write/Grep/Git
    → no patch extraction, no test verification, no retry

TARGET (competitive):
  nebula do "goal"  OR  nebula fix "issue description"
    → harness.Orchestrator
        ├── PlannerAgent
        │     • reads issue + file tree
        │     • calls search_code to find relevant files
        │     • outputs Plan{files_to_edit, test_commands}
        │
        ├── ExecutorAgent (up to 3 retry attempts)
        │     • reads each file in plan
        │     • writes fix with LLM
        │     • runs lint
        │     • extracts git diff patch
        │
        ├── VerifierAgent
        │     • applies patch
        │     • runs failing tests
        │     • if fail → sends error trace back to Executor
        │     • if pass → runs full suite
        │
        └── CriticAgent (optional)
              • reviews patch quality
              • flags test modifications / security issues
```

---

## Acceptance Criteria

| Metric | Target |
|---|---|
| SWE-bench Verified resolve rate | ≥ 30% |
| Cost per fix | ≤ $2.00 |
| Time per fix | ≤ 5 min |
| Patch validity (`git apply`) | 100% |
| Test infra failures | < 5% |
| Works on: Python, Go, JS/TS repos | Yes |

---

## File Targets

| File | Status | Purpose |
|---|---|---|
| `internal/harness/orchestrator.go` | Scaffolded → needs TASK-074/075 | Main control loop |
| `internal/harness/agents/agents.go` | Scaffolded → needs TASK-072/073/078/085 | All sub-agents |
| `internal/harness/tools/tools.go` | Partial → needs TASK-079 | Tool registry |
| `internal/harness/contextpkg/manager.go` | Done | Hierarchical context |
| `internal/harness/shared/shared.go` | Done | Shared types |
| `internal/swebench/patch.go` | Basic → needs TASK-080 | Git diff + validation |
| `internal/swebench/testrunner/python_runner.go` | Missing → TASK-076 | pytest runner |
| `internal/swebench/testrunner/js_runner.go` | Missing → TASK-082 | jest/mocha runner |
| `internal/harness/trajectory.go` | Missing → TASK-081 | Checkpoint/resume |
| `internal/harness/index/index.go` | Missing → TASK-086 | Symbol index |
| `internal/agent/goal_planner.go` | Broken → needs TASK-071 | JSON retry |
| `~/.config/nebula/config.toml` | Wrong model → TASK-070 | Groq model fix |

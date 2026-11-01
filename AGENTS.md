# Nebula — Agent Orientation Guide

Nebula is a Go multi-agent harness for SWE-bench. It solves real GitHub issues in Python repositories by orchestrating a pipeline of specialized AI agents. The goal is to maximize the percentage of SWE-bench instances solved (currently at 80% on a 5-instance test set).

---

## Architecture

```
CLI (nebula do) → Orchestrator → PlannerAgent → ExecutorAgent (3-attempt loop) → VerifierAgent → CriticAgent (deterministic)
```

### Components

- **Orchestrator** (`internal/harness/orchestrator.go`): Top-level coordinator. Manages the full pipeline, per-attempt timeout budgeting, retry/resample logic, test context injection, localization hints, git state management.
- **PlannerAgent** (`internal/harness/agents/agents.go`): Breaks the issue into concrete steps for the executor. Retries on empty steps array.
- **ExecutorAgent** (`internal/harness/agents/agents.go` + `internal/agent/goal_executor.go`): Runs a ReAct loop (up to 40 iterations) using tools to read/edit/test the repo. Has reproduce-first protocol: must run tests and confirm passing before calling DoneTool.
- **VerifierAgent** (`internal/harness/agents/agents.go`): Validates patch and runs tests.
- **CriticAgent** (`internal/harness/agents/agents.go`): Deterministic patch quality checks (5 rules: empty, test files, binary, size >500 lines, debug artifacts).
- **GoalSession** (`internal/agent/goal_executor.go`): Resumable executor conversation thread.
- **RunTestsTool** (`internal/tools/core_tools.go`): Registered when `--test-cmd` is set; lets executor run failing tests in-loop and get feedback.

---

## Key Files

| File | Purpose |
|------|---------|
| `internal/harness/orchestrator.go` | Orchestration, timeout, git reset, retry policy |
| `internal/harness/agents/agents.go` | All agent implementations (Planner, Executor, Verifier, Critic) |
| `internal/agent/goal_executor.go` | DoGoal/DoGoalSession, tool loop, DoneTool gate |
| `internal/tools/core_tools.go` | ReadFileTool, WriteFileTool, StrReplaceTool, GrepTool, ShellTool, RunTestsTool, GitTool, DoneTool |
| `internal/harness/tools/tools.go` | Harness-level tool registry (NewDefaultRegistry) |
| `internal/harness/environment.go` | Environment interface + LocalEnvironment (workDir-explicit, no os.Chdir) |
| `internal/swebench/patch.go` | GeneratePatch (stage-and-diff, excludes test patch files) |
| `internal/swebench/runner.go` | Local SWE-bench test runner (NOT the official harness) |
| `internal/llm/` | LLM client, message types (typed ToolCall/FunctionCall), providers |
| `internal/cli/root.go` | Key loading via OS keyring — **NEVER touch this** |
| `internal/cli/commands.go` | CLI flags including --failing-tests, --test-patch |

---

## Security Constraints (ABSOLUTE — never violate)

- API keys are **NEVER** written to disk. Always loaded from OS keyring via go-keyring (service: `"nebula"`). Keys: `groq_api_key`, `groq_api_key_2`...`9`, same pattern for `gemini`, `mistral`, `nvidia`.
- **NEVER** modify `internal/cli/root.go` `loadKeys()` function.
- **NEVER** add any code that writes API keys to files, env files, or logs.

---

## Build & Run

```bash
cd /home/sagar-jadhav/Documents/nebula
go build ./...                              # verify compilation
go build -o ~/go/bin/nebula ./cmd/nebula/  # build binary
cp ~/go/bin/nebula ~/.local/bin/nebula     # update Docker-mounted binary — do this after every rebuild
```

> **Important**: Always run the `cp` step after rebuilding. Runs 13–15 regressed to 0% because the Docker-mounted binary was stale.

---

## SWE-bench Evaluation

```bash
# Run the 5-instance test suite (uses Docker containers)
python3 scratchpad/swe_run<N>.py > /tmp/swe_run<N>.log 2>&1 &

# Check progress
tail -f /tmp/swe_run<N>.log
```

### Key log signals

| Signal | Meaning |
|--------|---------|
| `[step N/40]` | Executor iteration |
| `[DoGoal summary] N iterations, M edits made` | Executor exit |
| `PASS ✅` / `FAIL ❌` | Instance result |
| `FINAL SCORE: N/5` | Run complete |

---

## Score History

| Run | Score | Notes |
|-----|-------|-------|
| 6 | 20% | DoneTool burned all 25 iterations |
| 7 | 40% | Multi-turn + reasoning |
| 8–10 | 20% | New feature regressions |
| 11 | 80% | --failing-tests/--test-patch flags working, RunTestsTool feedback |
| 12 | 80% | All opus fixes (localization, model routing, staged diff, obs masking) |
| 13 | 0% | **REGRESSION**: pre-run localization 2-min timeout burned 600s budget |
| 14 | 0% | Same — binary not updated |
| 15 | 0% | Same — binary still old |

---

## Known Issues (Open)

- **#68**: P3 minor findings (ns literal, extractJSON fragile, conftest.py in critic, etc.)
- **Pre-run localization (#55)**: Currently disabled — conda command hangs. Needs to use `o.testCmd` (the explicit `--test-cmd` flag) instead of auto-detection.

---

## Current Model Routing

All phases use `nvidia/nemotron-3-ultra-550b-a55b` (configured in `~/.config/nebula/config.toml`).

---

## Git / GitHub

- Remote: `git@github.com:sagar0163/nebula` (SSH)
- All commits must end with:
  ```
  Co-Authored-By: Claude Sonnet 4.6 <noreply@anthropic.com>
  ```
- PRs must end with:
  ```
  🤖 Generated with [Claude Code](https://claude.com/claude-code)
  ```

---

## Design Principles (MUST follow for all decisions)

These are the non-negotiable design rules for nebula. Every code change, architectural decision, and tradeoff must be evaluated against them.

### 1. Long-term correctness over short-term workarounds
Never add hacks, temporary patches, or "it works for now" code. If the root cause isn't clear, investigate before fixing.

### 2. Efficiency first
Every LLM call costs time and money. Before adding a new LLM call (new agent phase, new prompt, new verification step), ask: can this be done deterministically? Can it be batched with an existing call? The Critic replacement (#57) is the model to follow.

### 3. No regressions
Before merging any change, trace through the execution path and identify what existing behavior could break. Runs 13/14/15 (0% from pre-run localization hanging) are the canonical example of a well-intentioned feature causing catastrophic regression. Always test the timeout/failure path, not just the happy path.

### 4. Explicit over implicit
Pass `workDir` explicitly (Environment interface), pass `testCmd` explicitly (`--test-cmd` flag), pass model config explicitly (`RunOptions.ModelConfig`). Never rely on process CWD, ambient env vars, or auto-detection when explicit values are available.

### 5. Deterministic over probabilistic
Replace LLM-based checks with deterministic ones wherever possible (e.g., Critic, patch validation). LLMs hallucinate; Go code doesn't.

### 6. Parallel-ready architecture
Every new component should be designed to work with multiple concurrent instances. The Environment interface exists for this reason. New code must not call `os.Chdir`, use process-global state, or write to shared temp paths.

### 7. Fail fast and loud
Silent failures (empty error traces, ignored errors, fallthrough success) are worse than panics. Every error path must either return a meaningful error or log a clear diagnostic message.

### 8. Score is the metric
All decisions are ultimately evaluated by SWE-bench score. A clever architectural improvement that regresses the score is wrong. Always rebuild and re-test after significant changes.

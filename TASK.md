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
| None | All targets met! | Multi-model consensus (TASK-089) is optional | 🟢 Completed |

---

## Completed Tasks ✅

| Task | What | File |
|---|---|---|
| TASK-070 | Fix Groq config model name (`llama-3.1-70b-versatile`) | `~/.config/nebula/config.toml` |
| TASK-071 | JSON parse retry in goal_planner (3× with backoff) | `internal/agent/goal_planner.go` |
| TASK-072 | Real ExecutorAgent (5-iter ReAct: read→write→lint→git diff) | `internal/harness/agents/agents.go` |
| TASK-074 | Executor↔Verifier retry loop (max 3 attempts) | `internal/harness/orchestrator.go` |
| TASK-075 | Real repo summarizer (`find` depth-3 tree) | `internal/harness/orchestrator.go` |
| TASK-076 | Python (pytest) test runner | `internal/swebench/testrunner/python_runner.go` |
| TASK-077 | Harness wired into `nebula do` / `nebula fix` | `internal/cli/commands.go` |
| TASK-078 | Real PlannerAgent prompt + structured JSON output | `internal/harness/agents/agents.go` |
| TASK-079 | `search_code` tool in harness registry (GrepTool) | `internal/harness/tools/tools.go` |
| TASK-081 | Checkpoint/resume — `saveTrajectory()` after each step | `internal/harness/trajectory.go` |
| TASK-082 | JS/TS (jest/mocha) test runner | `internal/swebench/testrunner/js_runner.go` |
| TASK-083 | `--concurrency N` parallel instances | `internal/swebench/runner.go` |
| TASK-084 | Structured observability logger (JSONL per step) | `internal/harness/logger.go` |
| TASK-085 | CriticAgent with LLM patch quality review | `internal/harness/agents/agents.go` |
| TASK-086 | Symbol index (ctags) | `internal/harness/index/index.go` |
| TASK-088 | Self-improving memory (SavePastFix / SearchPastFixes) | `internal/harness/orchestrator.go` |
| TASK-073 | Implement real VerifierAgent | `internal/harness/agents/agents.go` |
| TASK-080 | Add `git apply --check` validation | `internal/swebench/patch.go` |
| TASK-087 | Vector DB for semantic file search | `internal/harness/contextpkg/vector.go` |

---

## Remaining Tasks 🚧

All non-optional tasks have been fully completed!

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
| `internal/harness/orchestrator.go` | ✅ Done | Main control loop with 3-attempt retry |
| `internal/harness/agents/agents.go` | ✅ Done | Planner/Executor/Critic/Verifier implemented |
| `internal/harness/tools/tools.go` | ✅ Done | Shell, ReadFile, WriteFile, Git, RunTests, SearchCode |
| `internal/harness/contextpkg/manager.go` | ✅ Done | Hierarchical context + VectorStore interface |
| `internal/harness/shared/shared.go` | ✅ Done | Shared types |
| `internal/harness/trajectory.go` | ✅ Done | Checkpoint/resume (JSON per step) |
| `internal/harness/logger.go` | ✅ Done | Structured JSONL observability |
| `internal/harness/index/index.go` | ✅ Done | ctags symbol index |
| `internal/harness/memory/` | ✅ Done | Past fix store wired in orchestrator |
| `internal/swebench/patch.go` | ✅ Done | Full git apply validation |
| `internal/swebench/testrunner/python_runner.go` | ✅ Done | pytest runner |
| `internal/swebench/testrunner/js_runner.go` | ✅ Done | jest/mocha runner |
| `internal/swebench/runner.go` | ✅ Done | Concurrent instance runner |
| `internal/agent/goal_planner.go` | ✅ Done | JSON retry (3× backoff) |
| `internal/cli/commands.go` | ✅ Done | Harness wired into `nebula do` / `nebula fix` |
| `~/.config/nebula/config.toml` | ✅ Done | Groq model fixed |

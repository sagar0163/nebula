# Nebula

**A 24/7 intelligent agent for your terminal — thinks, acts, and fixes across any domain.**

> ⚠️ This is the active Go rewrite. The original Node.js version is archived at [sagar0163/Nebula_cli](https://github.com/sagar0163/Nebula_cli).

---

## Vision

Nebula is being built toward a JARVIS-style general agent: not limited to one domain, not just reactive to failures — an always-on assistant that understands your project, takes natural-language goals, and acts on them end-to-end.

**What works today:** Self-healing terminal agent — runs commands, detects failures, diagnoses with AI, suggests and applies fixes. Learns patterns over time.

**What's being built:** A general agent that accepts any goal (`nebula do "fix the Blueprint empty-name bug"`), finds the relevant files itself, writes the code, runs the tests, iterates until green — without needing a failing command as a starting point.

---

## What Nebula Does Today

When a command fails, Nebula diagnoses the error with AI, suggests a fix, and lets you apply it with one keystroke. It learns from successful fixes and recalls them on future failures.

**Core capabilities:**
- **Self-healing** — detects failures, diagnoses with AI, suggests and applies fixes
- **Multi-turn reasoning** — up to 6 turns with chain-of-thought scratchpad, anti-repetition guard
- **Smart file injection** — reads files mentioned in errors for targeted context
- **Workflow memory** — learns successful fix patterns, recalls them proactively
- **Multi-provider AI** — routes across Groq, NVIDIA, Mistral, Gemini based on workload
- **Eval harness** — fixture-based pass-rate evaluation for regression testing
- **MCP support** — connects to Model Context Protocol servers for extended tooling
- **Safety-first** — five-layer defense-in-depth: classify → approve → sandbox → execute → audit
- **Single binary** — no runtime dependencies, ships everywhere

---

## Quick Start

```bash
# Build from source (requires Go 1.22+)
git clone git@github.com:sagar0163/nebula.git
cd nebula
go build -o ~/.local/bin/nebula ./cmd/nebula
nebula setup
```

---

## Usage

```bash
# One-shot command with auto-healing
nebula run go build ./...
nebula run npm run build
nebula run python -m pytest tests/

# Interactive REPL session
nebula

# General AI task (writing, research, code questions)
nebula ask "explain the doom loop detection in agent.go"

# Run fixture-based eval suite
nebula eval --dir testdata/evals/

# Dry-run (show what would run, don't execute)
nebula --dry-run run git push origin main

# View past sessions
nebula session list
```

---

## Architecture

```
cmd/nebula/          Entry point
internal/
  agent/             Core orchestration loop (run → detect → heal → learn)
  cli/               Cobra commands and flags
  eval/              Fixture-based pass-rate eval harness
  pty/               PTY harness: head+tail buffer, ANSI strip, SIGWINCH
  tui/               Bubbletea interactive REPL
  llm/               Provider interface + workload router + parallel fanout
  memory/            SQLite-backed persistent memory (patterns, sessions, profiles)
  safety/            Five-layer safety: classify → approve → dry-run → sandbox → audit
  mcp/               MCP client (modelcontextprotocol/go-sdk)
  workflow/          Pattern learner (freq-itemset over command history)
  models/            Shared domain types
configs/             Default config template
docs/research/       Architecture research and decisions
testdata/evals/      Eval fixtures (go-build, node-missing-dep, go-undefined-var, …)
```

---

## Roadmap — General Agent (v1.0)

The path from repair daemon to general agent:

| Milestone | What it unlocks |
|---|---|
| `nebula do <goal>` | Any natural-language goal, not just broken commands |
| Tool registry | Shell, file, grep, git, web as first-class tools |
| Codebase awareness | Finds relevant files itself — no error needed to bootstrap |
| `nebula fix <description>` | SWE-bench-style natural-language code fixes |
| Proactive triggers | Cron + file-watch — acts without being called |
| User/project profiles | Knows your stack, conventions, current project |
| `nebula chat` | Full conversational mode with tool calls shown live |

See `TASK.md` for detailed task briefs for each milestone.

---

## Safety

Nebula runs a five-layer safety pipeline before executing any AI-suggested command:

1. **Parse & classify** — read-only vs mutating; hard-deny interpreter bypass vectors
2. **Approval engine** — allow/ask/deny with persistent per-pattern rules
3. **Dry-run mode** — `--dry-run` shows exactly what would run
4. **OS sandbox** — bubblewrap (Linux) / sandbox-exec (macOS)
5. **Runtime controls** — timeouts, doom-loop detection, audit log

---

## API Keys

Keys are stored in the OS keyring (never on disk). Set via the setup wizard or directly:

```bash
nebula key add groq   gsk_...
nebula key add nvidia nvapi-...
```

Or pass via env vars for Docker/CI:

```bash
NEBULA_GROQ_KEY=... NEBULA_NVIDIA_KEY=... nebula run go build ./...
```

---

## Development

```bash
go test ./...
go build ./cmd/nebula
nebula eval --dir testdata/evals/   # regression check
```

---

## License

MIT — see [LICENSE](LICENSE)

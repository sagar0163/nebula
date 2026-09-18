# Nebula

**Self-healing terminal agent with AI-powered command recovery — written in Go.**

> ⚠️ This is the active Go rewrite. The original Node.js version is archived at [sagar0163/Nebula_cli](https://github.com/sagar0163/Nebula_cli).

---

## What Nebula Does

Nebula is a terminal agent that learns from your commands and automatically fixes failures. When a command fails, it analyzes the error, suggests a fix, and lets you apply it with one keystroke.

**Core capabilities:**
- **Self-healing** — detects failures, diagnoses with AI, suggests and applies fixes
- **Workflow memory** — learns successful patterns, suggests them proactively
- **Multi-provider AI** — routes to Gemini, Groq, or Ollama based on workload
- **MCP support** — connects to Model Context Protocol servers for extended tooling
- **Safety-first** — five-layer defense-in-depth: classify → approve → sandbox → execute → audit
- **Single binary** — no runtime dependencies, ships everywhere

---

## Quick Start

```bash
# Install (once published)
curl -fsSL https://github.com/sagar0163/nebula/releases/latest/download/install.sh | bash

# Or build from source (requires Go 1.22+)
git clone git@github.com:sagar0163/nebula.git
cd nebula
go build -o nebula ./cmd/nebula
./nebula setup
```

---

## Usage

```bash
# Interactive REPL session
nebula

# One-shot command with auto-healing
nebula run docker compose up -d

# Dry-run (show what would run, don't execute)
nebula --dry-run run git push origin main

# View past sessions
nebula session list

# Resume a session
nebula session resume <id>
```

---

## Configuration

Copy `configs/config.toml` to `~/.config/nebula/config.toml` and set your API keys:

```bash
mkdir -p ~/.config/nebula
cp configs/config.toml ~/.config/nebula/config.toml
```

Or run the setup wizard:

```bash
nebula setup
```

---

## Architecture

```
cmd/nebula/          Entry point
internal/
  agent/             Core orchestration loop (run → detect → heal → learn)
  cli/               Cobra commands and flags
  pty/               PTY harness with sentinel-based failure detection
  tui/               Bubbletea interactive REPL
  llm/               Provider interface + workload router
  memory/            SQLite-backed persistent memory + brute-force cosine search
  safety/            Five-layer safety: classify → approve → dry-run → sandbox → audit
  mcp/               MCP client (modelcontextprotocol/go-sdk)
  workflow/          Pattern learner (freq-itemset over command history)
  models/            Shared domain types
configs/             Default config template
docs/research/       Architecture research and decisions
```

---

## Safety

Nebula runs a five-layer safety pipeline before executing any AI-suggested command:

1. **Parse & classify** — read-only vs mutating; hard-deny interpreter bypass vectors
2. **Approval engine** — allow/ask/deny with persistent per-pattern rules
3. **Dry-run mode** — `--dry-run` shows exactly what would run
4. **OS sandbox** — bubblewrap (Linux) / sandbox-exec (macOS)
5. **Runtime controls** — timeouts, doom-loop detection, audit log

---

## Development

```bash
go test ./...
go build ./cmd/nebula
```

---

## License

MIT — see [LICENSE](LICENSE)

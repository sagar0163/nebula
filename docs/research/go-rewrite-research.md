# Nebula Go Rewrite — Research & Suggestions

> Research conducted: 2026-09-18
> Context: Rewriting Nebula-CLI (Node.js, legacy at github.com/sagar0163/Nebula_cli) in Go from scratch.

---

## 1. Project Structure

```
nebula/
  cmd/
    nebula/
      main.go           # entrypoint, cobra root
  internal/
    agent/              # core agent loop (run, intercept, heal)
    ai/                 # LLM providers + router
    memory/             # SQLite + vector store
    pty/                # terminal/PTY handling
    plugins/            # plugin host
    mcp/                # MCP client
    safety/             # dry-run, risk scoring, sandboxing
    config/             # viper config management
    tui/                # bubbletea UI components
  pkg/
    provider/           # exported AI provider interfaces (reusable)
  data/
    patterns/           # community fix patterns (embedded via go:embed)
  .goreleaser.yaml
  go.mod                # module: github.com/sagar0163/nebula
```

`internal/` is private to this module — enforces clean boundaries. `pkg/` only for things you'd publish as a library.

---

## 2. PTY & Terminal Handling

| Library | Notes |
|---|---|
| `github.com/creack/pty` | Gold standard, used by VS Code. Simple, reliable. **Use this.** |
| `github.com/aymanbagabas/go-pty` | Cross-platform including Windows. Worth it if you want Windows support. |
| `golang.org/x/term` | Raw mode, terminal size detection. Use alongside creack/pty. |

**Pattern:** wrap PTY in a multiplexer — capture stdout/stderr for AI analysis while still streaming to the user's terminal simultaneously. Use `io.MultiWriter`.

---

## 3. TUI Framework

**Use `github.com/charmbracelet/bubbletea`** — no contest for an interactive agent REPL.

Full charm stack:
- `bubbletea` — Elm-style state machine, handles keystrokes, async commands
- `github.com/charmbracelet/lipgloss` — layout, colors, borders
- `github.com/charmbracelet/bubbles` — pre-built components: spinner, text input, viewport, list, progress bar
- `github.com/charmbracelet/huh` — forms and prompts (setup wizard, confirmations)

**Key pattern for agent REPL:** use bubbletea's `tea.Cmd` for async LLM streaming — each token arrives as a `tea.Msg`, keeping the UI responsive while the model streams.

Alternatives to skip: `tview` (good but imperative, fights async), `gocui` (too low-level).

---

## 4. AI/LLM Integration

**Multi-provider routing pattern:**

```go
// internal/ai/provider.go
type Provider interface {
    Complete(ctx context.Context, req Request) (<-chan Token, error)
    Name() string
    Available() bool  // health check
}
```

Concrete implementations per provider, router tries in priority order with fallback:

| Provider | Library |
|---|---|
| Gemini | `github.com/google/generative-ai-go` (official) |
| Groq | `github.com/sashabaranov/go-openai` pointed at Groq base URL (OpenAI-compatible) |
| Ollama | `github.com/ollama/ollama/api` (official Go client) |
| OpenAI (future) | `github.com/sashabaranov/go-openai` |

**Streaming:** use SSE parsing via `github.com/r3labs/sse/v2` or handle chunked HTTP manually. Always stream — never buffer full responses for a CLI agent.

**Context window management:** track token counts, summarize old memory when approaching limits. Implement from day one — retrofitting is painful.

---

## 5. Memory & Storage

**SQLite:**
- `modernc.org/sqlite` — **pure Go, no CGO**. Best for distribution (no gcc needed). Slightly slower than CGO but irrelevant for a CLI. **Recommended.**
- `github.com/mattn/go-sqlite3` — CGO, faster, but complicates cross-compilation.

Use `modernc.org/sqlite` via `github.com/jmoiron/sqlx` for ergonomics.

**Vector/Semantic Memory:**
- `github.com/asg017/sqlite-vec` — SQLite extension for vector search. Keeps everything in one SQLite file.
- `github.com/philippgille/chromem-go` — embedded vector DB, pure Go, no external process. **Best option for simplicity.**

**Schema design:**
- `commands` — raw history
- `patterns` — learned fixes
- `sessions` — context
- `embeddings` — vectors

Index on command hash for fast dedup.

---

## 6. Plugin System

**Avoid `plugin.Open()` (Go built-in)** — requires exact Go version match, no cross-platform, practically unusable in prod.

| Approach | Library | Tradeoff |
|---|---|---|
| WASM plugins | `github.com/tetratelabs/wazero` | Pure Go, sandboxed, cross-platform. Best for untrusted/community plugins. |
| RPC plugins | `github.com/hashicorp/go-plugin` | Used by Terraform/Vault. Plugins are separate binaries over gRPC. Any language. Battle-tested. |
| Script plugins | `github.com/yuin/gopher-lua` | Lightweight Lua, good for simple event hooks. |

**Recommendation:** go-plugin for power users, wazero for sandboxed community plugins, Lua for simple hooks.

---

## 7. MCP (Model Context Protocol)

| Library | Notes |
|---|---|
| `github.com/mark3labs/mcp-go` | Most complete Go MCP implementation (server + client). **Use this.** |
| `github.com/metoro-io/mcp-golang` | Alternative, active development. |

Implement as a thin client in `internal/mcp/` — discover local MCP servers from config, connect via stdio/SSE transport, expose tools to the AI router.

---

## 8. Safety & Sandboxing

**Layered safety model:**

1. **Risk scorer** — classify commands: `safe` / `warn` / `dangerous` based on patterns (`rm`, `sudo`, `curl | sh`, etc.)
2. **Dry-run mode** — `--dry-run` flag + `NEBULA_DRY_RUN=1` env var
3. **Confirmation prompts** — `huh` for `[y/n/edit/explain]` on AI-suggested commands
4. **Context scrubbing** — strip secrets before sending to LLM (regex for `AWS_`, `sk-`, etc.)
5. **Audit log** — append-only SQLite table of every AI suggestion + user decision

**Key insight from Warp terminal:** never auto-execute without explicit user confirmation. Show the suggestion inline, let user edit before accepting.

---

## 9. Distribution

**goreleaser** (`github.com/goreleaser/goreleaser`):
- Cross-compilation: linux/mac/windows, amd64/arm64
- GitHub releases with checksums
- Homebrew tap auto-generation
- deb/rpm/apk packages

**Auto-update:**
- `github.com/creativeprojects/go-selfupdate` — robust, verifies checksums. **Recommended.**
- `github.com/muesli/selfupdate` — simpler alternative.

**Install script:** `curl -fsSL https://get.nebula.sh | sh` pattern — auto-generated by goreleaser.

---

## 10. Industry Patterns (2025-2026)

| Pattern | Where seen | What to adopt |
|---|---|---|
| **Inline diff display** | GitHub Copilot CLI, Cursor | Show suggested command highlighted vs what user typed |
| **Streaming in terminal** | Warp, Claude CLI | Stream LLM tokens directly into TUI as they arrive |
| **Context awareness** | Warp | Auto-detect project type (git, Docker, k8s) and inject into prompts |
| **Session replay** | All modern tools | Record full session, let user replay or share |
| **Telemetry opt-in** | All | Anonymous usage stats, explicit opt-in on first run |
| **Config as code** | Modern CLIs | `~/.config/nebula/config.toml` via viper |
| **Shell integration** | Fig/Warp | Shell hooks (`eval "$(nebula init bash)"`) for deeper interception |
| **Agent tool calls** | Copilot, Cursor | Let LLM call structured tools (read file, search, run safe cmd), not just suggest text |

---

## 11. What NOT to Do

- **Don't use `os/exec` directly for PTY** — you lose terminal control characters, colors break, interactive programs fail
- **Don't use CGO** — kills cross-compilation, complicates CI. Use `modernc.org/sqlite` instead
- **Don't store secrets in SQLite** — use OS keychain via `github.com/zalando/go-keyring`
- **Don't block the main goroutine on LLM calls** — everything async via channels/goroutines
- **Don't parse shell yourself** — use `mvdan.cc/sh` for proper POSIX shell AST analysis
- **Don't hardcode provider logic** — interface from day one, providers are pluggable
- **Don't skip context cancellation** — every LLM call needs `context.WithTimeout`
- **Don't use `log.Fatal` in a TUI** — bypasses bubbletea cleanup, corrupts the terminal

---

## Recommended Library Stack

```
CLI framework:     github.com/spf13/cobra
TUI:               github.com/charmbracelet/bubbletea + lipgloss + bubbles + huh
PTY:               github.com/creack/pty
SQLite:            modernc.org/sqlite + github.com/jmoiron/sqlx
Vector memory:     github.com/philippgille/chromem-go
AI providers:      github.com/google/generative-ai-go
                   github.com/sashabaranov/go-openai (Groq + OpenAI)
                   github.com/ollama/ollama/api
MCP client:        github.com/mark3labs/mcp-go
Plugin host:       github.com/hashicorp/go-plugin + github.com/tetratelabs/wazero
Shell parser:      mvdan.cc/sh
Keyring:           github.com/zalando/go-keyring
Config:            github.com/spf13/viper
Release:           github.com/goreleaser/goreleaser
Self-update:       github.com/creativeprojects/go-selfupdate
```

# Nebula Go Rewrite — Opencode Research (big-pickle model)

> Research conducted: 2026-09-18  
> Model: opencode/big-pickle (web-search grounded, mid-2026 ecosystem)  
> Context: Rewriting Nebula-CLI (Node.js legacy) in Go from scratch.

---

## 1. Project Structure

```
cmd/nebula/
internal/
  agent/       # orchestration core: prompt build → tool loop → recovery
  cli/         # cobra/kong commands, flags, config loading
  pty/         # PTY spawn, sentinel hook, transcript ring buffer
  tui/         # bubbletea app + all views
  llm/         # Provider interface, router, providers, streaming
  state/       # session lifecycle, working dir tracking, thread/rollback
  memory/      # SQLite access, migrations, embeddings, retrieval
  safety/      # permission engine, classifiers, dry-run, sandbox launcher
  tools/       # built-in tool registry (exec, read, edit, search)
  mcp/         # MCP client wrapper + session manager
  workflow/    # pattern learner / workflow recognition (freq-itemset over history)
  models/      # pure domain types shared across packages
pkg/           # only if exposing a public API — else skip
configs/       # default TOML/YAML config, prompt templates (go:embed)
```

**Key conventions:**
- Define small interfaces at the consumer, not the producer: `llm.Provider`, `tools.Tool`, `memory.Store`, `safety.Policy`
- CLI: `github.com/spf13/cobra` (heavier, plugin ecosystem) vs `github.com/alecthomas/kong` (declarative, cleaner for subcommand-heavy CLIs)
- Config: `github.com/knadh/koanf` (merge files+env+flags) or `spf13/viper`

---

## 2. PTY & Terminal Handling

| Library | Notes |
|---|---|
| `github.com/creack/pty` (v1.1.24) | Gold standard, Unix-only. `pty.Start(cmd)` + raw mode via `golang.org/x/term.MakeRaw`. |
| `github.com/charmbracelet/x/xpty` | Cross-platform (includes Windows ConPTY). Charm-maintained, integrates with bubbletea. **Use if Windows support needed.** |
| `golang.org/x/term` | Raw mode, terminal size detection. Use alongside either PTY lib. |

**Critical architectural pattern:** Launch shell with a **startup sentinel hook** (e.g. `PROMPT_COMMAND` printing structured exit codes to PTY stream) to detect command boundaries and failure exit codes. Scan the PTY stream for sentinels to get structured failure detection — PTY alone doesn't expose command boundaries.

Design PTY as: spawn + raw-mode → **ring buffer** that tees to (a) TUI viewport, (b) rolling transcript for AI context, (c) SQLite session log.

---

## 3. TUI Framework

**Use `github.com/charmbracelet/bubbletea`** — maps beautifully onto an agent loop.

Full Charm stack:
- `bubbletea` — ELM-style model/update/view, `tea.Cmd` for async
- `charmbracelet/lipgloss` — layout, colors, borders
- `charmbracelet/bubbles` — spinner, textinput, viewport, list, progress
- `charmbracelet/huh` — forms for setup/confirmations
- `charmbracelet/log` — structured logging

**Key decision:** bubbletea is NOT a terminal emulator — live raw ANSI reflow is its weak point. For a self-healing agent, use chat/transcript-style display (command + output) rather than pixel-perfect PTY reflow. The diagnosis matters more than curses rendering.

**Always defer `term.Restore`** — a user's terminal left in raw mode is a cardinal sin.

---

## 4. AI/LLM Multi-Provider Routing with Streaming

**Almost everything speaks OpenAI wire format in 2026:**

| Provider | Library |
|---|---|
| OpenAI | `github.com/openai/openai-go` (official v3.x, `stream.Next()/Current()`) |
| Groq | OpenAI-compat — point `openai-go` at `https://api.groq.com/openai/v1` |
| Ollama | OpenAI-compat `/v1` or `github.com/ollama/ollama/api` (native) |
| Gemini | OpenAI-compat on AI Studio, or `github.com/google/generative-ai-go` |
| OpenRouter / vLLM / LM Studio | OpenAI-compat via `openai-go` + baseURL |

**One `openai-go` client + per-provider baseURL + key covers most routing.**

Alternative unified routing:
- `github.com/open-ai-sdk/ai-go` — unified multi-provider, `GenerateText`/`StreamText`, tool-loop orchestration
- `github.com/bluefunda/llmrouter` — provider registry + middleware (retry/timeout/circuit-breaker) + fallback

**Streaming essentials:**
- Always pass cancellable `ctx` — token streams must die on user interrupt
- Surface three channels to TUI: content deltas, tool-call deltas, final usage/cost
- **Workload-specialized routing**: cheap fast model for failure diagnosis, stronger model for workflow learning, local Ollama for offline/privacy mode

**Note:** `openai-go` v3.45+ requires Go ≥1.25 — pin `v3.44.0` if targeting older Go.

---

## 5. Memory & Storage

**SQLite driver landscape (mid-2026):**

| Driver | Path | CGO | Notes |
|---|---|---|---|
| mattn | `github.com/mattn/go-sqlite3` | yes | Fastest (~20-30% faster writes), exact upstream SQLite, cross-compile pain |
| modernc | `modernc.org/sqlite` (v1.52+) | no | Transpiled C, CGO_ENABLED=0, conservative pick |
| ncruces | `github.com/ncruces/go-sqlite3` (v0.35+) | no | WASM-embedded, **2-5x faster than modernc**, more features |

**Recommendation: `ncruces/go-sqlite3`** — best performance while staying CGO-free.

**Operational rules (non-negotiable):**
- Enable WAL: `PRAGMA journal_mode=WAL`
- Set `busy_timeout` (default off = `SQLITE_BUSY` failures)
- One dedicated write connection + pool discipline
- Migrations via `github.com/pressly/goose`

**Vector/Semantic Memory:**
- Start with **brute-force cosine over `float32` slice** — at CLI scale (few thousand vectors) this is fast and zero-dependency
- Scale-up: `github.com/asg017/sqlite-vec` (C extension, pairs with ncruces WASM binding; pre-v1, breaking changes expected)
- Pure-Go alternative: `github.com/viant/sqlite-vec` (vtab on modernc, newer)
- Embeddings: Ollama's `nomic-embed-text`/`mxbai-embed-large` for local; cache in DB

**Schema:** `sessions`, `messages`, `commands`, `workflows`, `patterns` (w/ embeddings), `permissions`, `threads`

**Use two-phase memory** (extract → consolidate) — beats single-shot summarization for long-lived memory.

---

## 6. Plugin System

**DO NOT use Go stdlib `plugin.so`** — toolchain-identical builds required, no Windows, no sandbox, host-crash on panic.

| Approach | Library | Use case |
|---|---|---|
| RPC plugins | `github.com/hashicorp/go-plugin` (gRPC mode) | Trusted third-party plugins, any language. Proven by Terraform/Vault. |
| WASM plugins | `github.com/tetratelabs/wazero` | Untrusted/community plugins. True sandbox, pure Go, no CGO. 2-10x slower CPU-bound. |
| Embedded Lua | `github.com/yuin/gopher-lua` | User-authored prompt hooks/recipes. Lightweight, in-process. |

**2026 recommendation:** Use **MCP servers as your plugin layer** (language-agnostic, the consensus extension surface). Add go-plugin/wazero only when you have real third-party authors. Keep the plugin contract minimal: `Tool`/`Provider`/`Hook` interfaces only.

---

## 7. MCP Client

| Library | Notes |
|---|---|
| `github.com/modelcontextprotocol/go-sdk` | **Official.** Maintained with Google, v1.x, spec 2026-07-28. Stdio/SSE/Streamable HTTP, middleware, OAuth. **New default.** |
| `github.com/mark3labs/mcp-go` (v0.54.x) | Pre-official standard, spec 2025-11-25. ~400 importers. Still viable. |

**Implementation shape for `internal/mcp/`:**
- Config block listing servers (type: stdio/http + command/URL + env)
- `session.Manager` spawning stdio processes, running init handshake, caching `tool name → (server, schema)`
- Unified `CallTool` exposed to agent's tool registry (namespace: `server::tool`)
- Reconnect with backoff on server crash
- **Lazy tool discovery** — don't dump all tool schemas into context at boot. Use `tool_search`/BM25 for huge tool surfaces.

**Note:** `roots`/`sampling`/`logging` are deprecated in spec 2026-07-28 — don't build on `roots`.

---

## 8. Safety & Sandboxing

**Five independent defense-in-depth layers (2026 consensus from Codex CLI, Claude Code, OpenDev):**

1. **Parse-and-classify (pre-execution):** split on pipes/`&&`/`||`/`;`, classify read-only vs mutating. Unknown segments → conservative. Hard-deny interpreter vectors: `bash -c`, `sh -c`, `python -c`, `node -e`, `env`, `sudo` — these bypass per-command review.

2. **Approval engine (interactive/auto):** allow / ask / deny with **persistent permission rules in SQLite** — exact-command, prefix-pattern, category rules. Auto-approve read-only; ask for everything else. Support `--dangerously-skip-permissions` escape hatch (name it to advertise danger).

3. **Dry-run / plan mode:** read-only mode where agent proposes mutations as preview diff; nothing side-effecting executes; user approves plan. Also plain `--dry-run` flag.

4. **OS-level sandbox (isolation, not authorization — both required):**
   - Linux: **bubblewrap** (`bwrap`) — `--unshare-user --unshare-pid --unshare-net`, `--ro-bind / /`, explicit writable paths. Or **Landlock** via `github.com/landlock-lsm/go-landlock` (lighter).
   - macOS: Seatbelt `sandbox-exec` with declarative profile.
   - Windows: no real sandbox — if policy requires sandbox and none exists, **refuse to run** rather than silently degrade.

5. **Runtime controls:** execution timeouts (hard cap), output size caps + tail-truncation, background-task promotion for long runs, **doom-loop detection** (fingerprint repeated identical tool calls → warn → force approval), git-snapshot undo for file mutations, cooperative cancellation (Ctrl+C → cancel stream → kill PTY child without orphans).

**For Nebula specifically:** sandbox is non-negotiable for auto-fix retries — when the agent retries a fixed command, it retries inside the same policy as the original failure.

---

## 9. Distribution

- **goreleaser** (`github.com/goreleaser/goreleaser`) — standard. Multi-platform + deb/rpm/brew tap/scoop/AUR, checksums, Cosign signing + SBOM + SLSA provenance (important for a tool that executes AI-suggested commands).
- **Auto-update:** `github.com/creativeprojects/go-selfupdate` — current standard (maintained, GitHub/GitLab releases, signature verification). Never self-update mid-session while PTY child is alive.
- **Single static binary:** `CGO_ENABLED=0` (ncruces/modernc SQLite, wazero), `go:embed` everything (prompts, schemas, migration SQL), stamp version via ldflags.
- Cross-compile CI: run goreleaser `--snapshot` matrix (linux amd64/arm64, darwin, windows amd64/arm64) on every PR.

---

## 10. Industry Patterns (2025-2026)

| Tool | Key patterns to adopt |
|---|---|
| **OpenAI Codex CLI** (Rust) | bubblewrap sandbox; exec policy rule engine; four sandbox modes; SQLite threads with fork; two-phase memory; JSON-RPC app-server (decouple runtime from UI) |
| **Claude Code** (TS) | Full BashTool lifecycle: classify → permission → sandbox-decision → exec → output → background-promotion → tool_result |
| **Goose / AAIF** (Rust) | Providers-as-plugins; everything via MCP; profile/toolkit separation |
| **OpenDev** (research, 2026) | Workload-specialized routing; planner/executor dual-agent split; five-layer safety; lazy MCP discovery; progressive context compaction (5-stage); git-snapshot undo |

**Transferable patterns:**
1. MCP as the extension portability layer
2. Multi-provider first with workload routing
3. SQLite sessions with fork/rollback
4. Permission + sandbox as two distinct mechanisms, both mandatory
5. Context compaction for long sessions
6. "App-server" decoupling (JSON-RPC over stdio) so core powers TUI + integrations
7. Lazy tool discovery + tool search for large MCP surfaces
8. Log every (failure, suggestion, applied?, success?) — self-healing is measurable, not marketing

---

## 11. What NOT to Do

1. Ship Go stdlib `plugin` (toolchain pins, no Windows, no isolation)
2. Build your own LLM SDK / streaming parser — use `openai-go` + layer router on top
3. Run AI-proposed commands unsandboxed — sandbox + approval gate by default
4. Treat PTY output as durable state — persist structured transcripts to SQLite
5. Forget terminal hygiene — no SIGWINCH, no `term.Restore` defer = broken user shells
6. Block TUI on network calls — every LLM/exec/MCP call must be a `tea.Cmd`
7. Use `context.Background()` everywhere — timeouts on all LLM/exec calls
8. Naive read-only classification trusting prefixes (misses `fd --exec`, `xargs`, redirections)
9. Leak secrets into context/transcripts/logs — strip tokens before sending to LLM
10. Over-engineer schema early — brute-force cosine over few thousand rows is correct at CLI scale
11. Ignore Windows platform decision until too late — PTY/sandbox/shell behavior all fork there
12. Version-pin accidents: `openai-go` v3.45+ needs Go ≥1.25; sqlite-vec pre-v1 breaking changes
13. Skip evals of the self-healing loop — log every (failure, suggestion, applied?, success?) triage

---

## Build Order Recommendation

```
TUI (chat + transcript)
→ PTY harness (sentinel + ring buffer)
→ Safety (sandbox + approval pipeline)
→ Prompt/context compaction
→ LLM Router (Gemini/Groq/Ollama via openai-go)
→ Memory (SQLite via ncruces, brute-force cosine)
→ MCP client (official go-sdk)
→ Workflow learner
```

Safety belongs before anything AI-proposes can execute — it's the one piece that cannot be retrofitted painlessly.

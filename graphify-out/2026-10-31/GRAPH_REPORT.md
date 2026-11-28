# Graph Report - nebula  (2026-10-31)

## Corpus Check
- 115 files · ~83,362 words
- Verdict: corpus is large enough that graph structure adds value.

## Summary
- 1292 nodes · 2705 edges · 81 communities (55 shown, 26 thin omitted)
- Extraction: 86% EXTRACTED · 14% INFERRED · 0% AMBIGUOUS · INFERRED: 374 edges (avg confidence: 0.8)
- Token cost: 0 input · 0 output

## Graph Freshness
- Built from commit: `d344335f`
- Run `git rev-parse HEAD` and compare to check if the graph is stale.
- Run `graphify update .` after code changes (no API cost).

## Community Hubs (Navigation)
- NewRouter
- runner_test.go
- Runner
- SQLiteStore
- safety_test.go
- workflow_test.go
- router_test.go
- GroqProvider
- Command
- buildDiagnosePrompt
- Model
- core_tools.go
- Router
- TestResult
- eval.go
- types.go
- shared.go
- .DoGoal
- Nebula Harness — Intelligence & Efficiency Benchmark
- ContextManager
- agents.go
- Manager
- Orchestrator
- GeminiProvider
- triggers.go
- recordingProvider
- .Execute
- Nebula Go Rewrite — Research & Suggestions
- Nebula Go Rewrite — Opencode Research (big-pickle model)
- Request
- NvidiaProvider
- WebFetchTool
- setup.go
- Nebula
- DetectProjectContext
- Nebula — Complete Harness Upgrade Plan
- MemoryVectorStore
- Registry
- StructuredLogger
- stubProvider
- tools.go
- Context
- ParameterDef
- StrReplaceTool
- wfStubProvider
- Performance Optimization Benchmarks
- main.go
- Indexer
- GrepTool
- ReadFileTool
- ShellTool
- WriteFileTool
- sandbox.go
- GoToDefinitionTool
- SearchCodeTool
- SemanticSearchTool
- loadTrajectory
- provider.go
- bench_test.go
- integration_test.sh
- GetContextWindow
- killProcessGroup
- killProcessGroup
- TestWatchResize
- watchResize
- watchResize
- interface.go
- create-test-repo.sh
- download-swebench.sh
- run_eval.sh
- setup.sh
- setup.sh
- setup.sh
- setup.sh
- setup.sh
- github.com/sagar0163/nebula

## God Nodes (most connected - your core abstractions)
1. `NewRouter()` - 56 edges
2. `Router` - 45 edges
3. `NewHarness()` - 45 edges
4. `Request` - 36 edges
5. `Workload` - 36 edges
6. `newTestStore()` - 35 edges
7. `Command` - 28 edges
8. `New()` - 26 edges
9. `Token` - 25 edges
10. `SQLiteStore` - 23 edges

## Surprising Connections (you probably didn't know these)
- `TestRecallPatternChain()` --calls--> `New()`  [INFERRED]
  internal/agent/planner_test.go → internal/agent/agent.go
- `TestAdaptiveTurnBudget()` --calls--> `progressCheck()`  [INFERRED]
  internal/agent/agent_test.go → internal/agent/agent.go
- `BenchmarkProgressCheck()` --calls--> `progressCheck()`  [INFERRED]
  internal/agent/bench_test.go → internal/agent/agent.go
- `TestDetectDomain()` --calls--> `detectDomain()`  [INFERRED]
  internal/agent/agent_test.go → internal/agent/agent.go
- `TestDiagnosePromptDoesNotScrubSecrets()` --calls--> `buildDiagnosePrompt()`  [INFERRED]
  internal/agent/agent_test.go → internal/agent/planner.go

## Import Cycles
- None detected.

## Communities (81 total, 26 thin omitted)

### Community 0 - "NewRouter"
Cohesion: 0.10
Nodes (72): pipelineOpts, New(), doomFingerprint(), Agent, T, newPipelineAgent(), newTestStore(), stdinTTY() (+64 more)

### Community 1 - "runner_test.go"
Cohesion: 0.08
Nodes (59): AgentRunner, Case, Config, fakeRunner, Meter, ModelUsage, rate, Report (+51 more)

### Community 2 - "Runner"
Cohesion: 0.07
Nodes (42): Context, Duration, Report, NewSWEBenchCmd(), printReport(), runGenerate(), runHarness(), extractModifiedFiles() (+34 more)

### Community 3 - "SQLiteStore"
Cohesion: 0.08
Nodes (35): DB, buildDSN(), Context, New(), T, newTestStore(), recentByRaw(), TestEfficiencyScoreOrdering() (+27 more)

### Community 4 - "safety_test.go"
Cohesion: 0.07
Nodes (40): Config, Executor, RunOptions, RunResult, Buffer, containsAny(), detectDomain(), Agent (+32 more)

### Community 5 - "workflow_test.go"
Cohesion: 0.09
Nodes (47): checkQueue(), Agent, Context, Watch(), List(), Load(), parse(), skillsDir() (+39 more)

### Community 6 - "router_test.go"
Cohesion: 0.09
Nodes (31): Int32, drainTokens(), fanoutTestConfig(), Context, Duration, T, TestIsRL(), TestRouter_RateLimitRetry() (+23 more)

### Community 7 - "GroqProvider"
Cohesion: 0.06
Nodes (28): ChatCompletionMessageParamUnion, ChatCompletionNewParams, applyResponseFormat(), buildOpenAIMessages(), Client, Context, NewGroq(), Client (+20 more)

### Community 8 - "Command"
Cohesion: 0.09
Nodes (42): CodebaseIndex, FileMatch, agentAdapter, IndexCodebase(), SearchCodebase(), isKnownProvider(), maskKey(), newAskCmd() (+34 more)

### Community 9 - "buildDiagnosePrompt"
Cohesion: 0.07
Nodes (37): Planner, F, BenchmarkExtractRelevantFiles(), BenchmarkParseSuggestion_Fallback(), BenchmarkParseSuggestion_JSON(), BenchmarkProgressCheck(), BenchmarkStringBuilder(), BenchmarkStringConcat() (+29 more)

### Community 10 - "Model"
Cohesion: 0.09
Nodes (27): Builder, Cmd, Msg, NewChat(), CancelFunc, Cmd, Context, Msg (+19 more)

### Community 11 - "core_tools.go"
Cohesion: 0.06
Nodes (9): Context, GitTool, GrepTool, ReadFileTool, ShellTool, StrReplaceTool, WriteFileTool, StrReplaceInFile() (+1 more)

### Community 12 - "Router"
Cohesion: 0.21
Nodes (15): CancelFunc, Context, Duration, Mutex, Time, isRL(), pump(), routingConfig() (+7 more)

### Community 13 - "TestResult"
Cohesion: 0.12
Nodes (19): Context, Context, atoi(), Duration, parseCargoTestOutput(), parseGenericOutput(), parseGoTestOutput(), parseJestOutput() (+11 more)

### Community 14 - "eval.go"
Cohesion: 0.15
Nodes (27): evalOptions, evalCaseReason(), firstAvailableProvider(), formatCost(), formatEvalDuration(), formatPercent(), formatTokens(), Duration (+19 more)

### Community 15 - "types.go"
Cohesion: 0.10
Nodes (26): Agent, AgentInput, AgentOutput, FileReference, MemorySnapshot, ModelConfig, ModelRouting, ParameterDef (+18 more)

### Community 16 - "shared.go"
Cohesion: 0.10
Nodes (26): Duration, FileReference, MemorySnapshot, ParameterDef, PastFix, Time, TokenBudget, ToolDefinition (+18 more)

### Community 17 - ".DoGoal"
Cohesion: 0.13
Nodes (16): GoalPlanner, Agent, Context, summarizeArgs(), toStringMap(), extractJSON(), Context, Registry (+8 more)

### Community 18 - "Nebula Harness — Intelligence & Efficiency Benchmark"
Cohesion: 0.09
Nodes (22): Achieved State — 2026-09-26, Bugs fixed, Current State: Token Efficiency Audit, Final market comparison (v0.4.0), Fuzz Results (v0.4.2), Hot-Path Benchmarks (v0.4.2, Intel i5-7300U), Key findings, Layer 1 — Clean the prompt (token efficiency) (+14 more)

### Community 19 - "ContextManager"
Cohesion: 0.13
Nodes (10): ContextManager, VectorStore, Context, FileReference, Phase, RWMutex, VectorMatch, NewContextManager() (+2 more)

### Community 20 - "agents.go"
Cohesion: 0.18
Nodes (12): CriticAgent, ExecutorAgent, PlannerAgent, VerifierAgent, Agent, Phase, Registry, NewCriticAgent() (+4 more)

### Community 21 - "Manager"
Cohesion: 0.22
Nodes (12): Content, envSlice(), extractText(), Client, Context, RWMutex, NewManager(), CallResult (+4 more)

### Community 22 - "Orchestrator"
Cohesion: 0.20
Nodes (10): Orchestrator, applyTestPatch(), deleteTrajectory(), extractFailingTests(), AgentInput, Context, Duration, Registry (+2 more)

### Community 23 - "GeminiProvider"
Cohesion: 0.21
Nodes (7): Context, NewGemini(), toGeminiParts(), Message, Part, GeminiConfig, GeminiProvider

### Community 24 - "triggers.go"
Cohesion: 0.19
Nodes (10): Context, Duration, NewCronTrigger(), NewFileWatchTrigger(), parseDuration(), Ticker, CronTrigger, FileWatchTrigger (+2 more)

### Community 25 - "recordingProvider"
Cohesion: 0.19
Nodes (4): recordingProvider, stubProvider, Context, Mutex

### Community 26 - ".Execute"
Cohesion: 0.26
Nodes (7): AgentOutput, Plan, extractJSON(), AgentInput, Context, RawMessage, toStringSlice()

### Community 27 - "Nebula Go Rewrite — Research & Suggestions"
Cohesion: 0.14
Nodes (13): 10. Industry Patterns (2025-2026), 11. What NOT to Do, 1. Project Structure, 2. PTY & Terminal Handling, 3. TUI Framework, 4. AI/LLM Integration, 5. Memory & Storage, 6. Plugin System (+5 more)

### Community 28 - "Nebula Go Rewrite — Opencode Research (big-pickle model)"
Cohesion: 0.14
Nodes (13): 10. Industry Patterns (2025-2026), 11. What NOT to Do, 1. Project Structure, 2. PTY & Terminal Handling, 3. TUI Framework, 4. AI/LLM Multi-Provider Routing with Streaming, 5. Memory & Storage, 6. Plugin System (+5 more)

### Community 29 - "Request"
Cohesion: 0.26
Nodes (6): meteredProvider, countRequestTokens(), countTokens(), Context, Request, Tool

### Community 30 - "NvidiaProvider"
Cohesion: 0.24
Nodes (5): Client, Context, NewNvidia(), NvidiaConfig, NvidiaProvider

### Community 31 - "WebFetchTool"
Cohesion: 0.19
Nodes (4): Context, stripHTML(), WebFetchTool, WebSearchTool

### Community 32 - "setup.go"
Cohesion: 0.36
Nodes (8): setupResult, configFileExists(), configFilePath(), geminiForm(), groqForm(), ollamaForm(), runSetupWizard(), safetyPolicies()

### Community 33 - "Nebula"
Cohesion: 0.17
Nodes (11): API Keys, Architecture, Development, License, Nebula, Quick Start, Roadmap — General Agent (v1.0), Safety (+3 more)

### Community 34 - "DetectProjectContext"
Cohesion: 0.29
Nodes (8): contextEntry, ProjectContext, DetectProjectContext(), Time, T, TestDetectProjectContext(), TestDetectProjectContextCacheHit(), TestDetectProjectContextCacheTTL()

### Community 35 - "Nebula — Complete Harness Upgrade Plan"
Cohesion: 0.20
Nodes (9): Acceptance Criteria, Architecture (Current → Target), Completed Tasks ✅, File Targets, Gap Analysis — What's Missing, Nebula — Complete Harness Upgrade Plan, Remaining Tasks 🚧, What's Already Built ✅ (+1 more)

### Community 36 - "MemoryVectorStore"
Cohesion: 0.36
Nodes (6): entry, MemoryVectorStore, SearchResult, cosineSimilarity(), Context, NewMemoryVectorStore()

### Community 37 - "Registry"
Cohesion: 0.39
Nodes (4): Registry, Tool, NewRegistry(), ToolDef

### Community 38 - "StructuredLogger"
Cohesion: 0.32
Nodes (5): Encoder, LogEvent, StructuredLogger, File, NewStructuredLogger()

### Community 40 - "tools.go"
Cohesion: 0.54
Nodes (4): Registry, Tool, NewDefaultRegistry(), NewRegistry()

### Community 41 - "Context"
Cohesion: 0.29
Nodes (3): Context, parseTestOutput(), RunTestsTool

### Community 42 - "ParameterDef"
Cohesion: 0.29
Nodes (3): ParameterDef, GitTool, ToolDefinition

### Community 45 - "Performance Optimization Benchmarks"
Cohesion: 0.29
Nodes (6): 1. LLM Token Accumulation, 2. Workflow Template Parsing, 3. Database Scanning (Implemented), Benchmark Results, Benchmark Results (1000 tokens), Performance Optimization Benchmarks

### Community 46 - "main.go"
Cohesion: 0.50
Nodes (4): Client, main(), testModel(), providerSpec

### Community 47 - "Indexer"
Cohesion: 0.50
Nodes (3): Indexer, Context, NewIndexer()

### Community 52 - "sandbox.go"
Cohesion: 0.90
Nodes (4): wrapBwrap(), WrapCommand(), wrapSandboxExec(), SandboxLevel

### Community 56 - "loadTrajectory"
Cohesion: 0.67
Nodes (3): Trajectory, loadTrajectory(), saveTrajectory()

### Community 57 - "provider.go"
Cohesion: 0.50
Nodes (3): Tool, ToolCall, Usage

### Community 58 - "bench_test.go"
Cohesion: 0.67
Nodes (3): BenchmarkTemplateParsePerRun(), BenchmarkTemplatePreParsed(), B

### Community 59 - "integration_test.sh"
Cohesion: 0.83
Nodes (3): fail(), pass(), integration_test.sh script

## Knowledge Gaps
- **99 isolated node(s):** `providerSpec`, `github.com/sagar0163/nebula`, `pipelineOpts`, `Agent`, `Resetter` (+94 more)
  These have ≤1 connection - possible missing edges or undocumented components.
- **26 thin communities (<3 nodes) omitted from report** — run `graphify query` to explore isolated nodes.

## Suggested Questions
_Questions this graph is uniquely positioned to answer:_

- **Why does `Router` connect `Router` to `NewRouter`, `Runner`, `safety_test.go`, `Command`, `buildDiagnosePrompt`, `.DoGoal`, `ContextManager`, `agents.go`, `Orchestrator`?**
  _High betweenness centrality (0.170) - this node is a cross-community bridge._
- **Why does `Command` connect `Command` to `.DoGoal`, `Runner`, `SQLiteStore`, `Orchestrator`?**
  _High betweenness centrality (0.093) - this node is a cross-community bridge._
- **Why does `NewRouter()` connect `NewRouter` to `Runner`, `workflow_test.go`, `router_test.go`, `Command`, `Router`?**
  _High betweenness centrality (0.072) - this node is a cross-community bridge._
- **Are the 54 inferred relationships involving `NewRouter()` (e.g. with `newPipelineAgent()` and `TestAskEmptyAndWhitespace()`) actually correct?**
  _`NewRouter()` has 54 INFERRED edges - model-reasoned connections that need verification._
- **Are the 43 inferred relationships involving `NewHarness()` (e.g. with `newPipelineAgent()` and `TestAskEmptyAndWhitespace()`) actually correct?**
  _`NewHarness()` has 43 INFERRED edges - model-reasoned connections that need verification._
- **What connects `providerSpec`, `github.com/sagar0163/nebula`, `pipelineOpts` to the rest of the system?**
  _99 weakly-connected nodes found - possible documentation gaps or missing edges._
- **Should `NewRouter` be split into smaller, more focused modules?**
  _Cohesion score 0.10252904989747096 - nodes in this community are weakly interconnected._
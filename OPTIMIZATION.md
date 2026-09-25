# Performance Optimization Benchmarks

This document details the performance benchmarks and optimizations identified for the Nebula agent, specifically targeting CPU overhead and Garbage Collector (GC) pressure.

## 1. LLM Token Accumulation
**Location:** `internal/agent/agent.go` and `internal/agent/planner.go`

**Issue:** During token streaming, the agent accumulates the response using naive string concatenation (`response += t.Text`) inside a loop. Because Go strings are immutable, this operation forces continuous memory reallocation and string copying on *every single token* received.

**Solution:** Use `strings.Builder` and `builder.WriteString(t.Text)` to efficiently accumulate the text response in a pre-allocated buffer.

### Benchmark Results (1000 tokens)
| Method | Speed (ns/op) | Memory Allocated (B/op) | Allocations/op |
|---|---|---|---|
| Naive String Concat (`+=`) | 621,945 | 1,262,136 (1.2 MB) | 999 |
| `strings.Builder` | 21,837 | 8,440 (8 KB) | 11 |

**Improvement:**
*   **~28x faster** execution speed
*   **~150x reduction** in memory allocations, vastly reducing GC pressure during long LLM responses.

---

## 2. Workflow Template Parsing
**Location:** `internal/workflow/workflow.go`

**Issue:** When background workflows execute, `template.New("prompt").Parse(...)` is invoked dynamically on every single step to render the context. This requires the Go templating engine to repeatedly parse the same string into an AST.

**Solution:** Pre-parse Go templates once when the workflow is initially loaded into memory, and only call `Execute()` during the run phase.

### Benchmark Results
| Method | Speed (ns/op) | Memory Allocated (B/op) | Allocations/op |
|---|---|---|---|
| Parse Per Run | 10,218 | 3,952 (3.9 KB) | 50 |
| Pre-Parsed | 1,599 | 432 (0.4 KB) | 9 |

**Improvement:**
*   **~6.3x faster** execution speed
*   **~9x reduction** in memory allocation overhead per workflow step.

---

## 3. Database Scanning (Implemented)
**Location:** `internal/memory/store.go` (Resolved in TASK-023 and TASK-024)

**Issue:** Functions like `FindSimilarPatterns` and `FindPermission` were doing full table scans—pulling thousands of rows from the database into memory and instantiating a new `gob.Decoder` on every single query to parse embeddings and evaluate glob strings.

**Solution:** Entirely removed these dead code paths and broken interfaces to prevent unbounded execution latency as the database grows.

**Improvement:**
*   Moved from **O(N) execution time to O(1)** for execution setup. The latency of running a command is now flat and instant.

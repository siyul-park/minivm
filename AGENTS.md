# AGENTS.md

Agent guide for this repo (`Claude Code`, `Codex`, `Cursor`, etc.).

## Quick Commands

```bash
make init          # install goimports/godoc and go install ./...
make test          # go test -race ./...
make benchmark     # package benches + ./benchmarks module benches
make lint          # goimports -w . && go vet ./...
make coverage      # CI-style full test run with coverage.out
make build         # build ./dist/minivm

go test -race ./...
go test -race -run TestFoo ./interp/...
go test -race -run 'TestInterpreter_WithDebugger|TestDebugger_Breakpoints' ./interp

./dist/minivm      # interactive assembly REPL
```

## Agent Workflow

1. `git status --short`; don't overwrite unrelated changes.
2. **For code exploration, prefer `codegraph` MCP tools over grep/read** (see Code Exploration below).
3. **Read task-relevant docs from Documentation Index before writing code or tests.**
4. **Before modifying code, read `docs/coding-patterns.md` and follow relevant sections.**
5. Mirror nearby tests; follow Test Conventions (one func per exported symbol, sub-cases as `t.Run`).
6. Update docs when behavior, invariants, commands, or pitfalls change.
7. On a fresh environment or CI-like run, start with `make init`; CI does this before lint/coverage.
8. Run narrow tests first, then `go test ./...`; use `make coverage` when you want the same broad validation CI runs.
9. `make benchmark` also runs the separate `benchmarks/` Go module. Use `make benchmark test-options="-count=2"` to match CI's comparison workflow.
10. For debugger, stepping, or breakpoint work, read `docs/debugging.md` and verify in `./interp`; `interp.WithDebugger` forces `WithTick(1)` and disables JIT.
11. Before reporting done, re-read every new/modified file against the pre-finish checklist in `.claude/CLAUDE.md` (or the equivalent in your agent's instruction file).

## Code Exploration

Prefer `codegraph` MCP tools over grep/read for structural questions. It is a tree-sitter knowledge graph of every symbol, edge, and file (sub-millisecond reads). Fall back to grep/read only for literal text (string contents, comments, log messages) or to confirm a specific detail codegraph didn't cover.

| Question | Tool |
|---|---|
| Where is symbol X defined? | `codegraph_search` |
| Focused context for a task/area | `codegraph_context` |
| How does X reach Y? / trace the flow | `codegraph_trace` |
| What calls Y? | `codegraph_callers` |
| What does Y call? | `codegraph_callees` |
| What breaks if I change Z? | `codegraph_impact` |
| Show Y's signature/source | `codegraph_node` |
| Survey several related symbols' source | `codegraph_explore` |
| What files exist under path/ | `codegraph_files` |
| Is the index healthy? | `codegraph_status` |

If `codegraph` reports the index is stale or not initialized, fall back to grep/read for affected files.

## Local Hooks

`.codex/hooks.json` runs `goimports` after `Edit`/`MultiEdit`/`Write` on `.go` files. Hooks best-effort; run `make lint` before finishing.

## Task Router

| Task | Read | Usually edit | Verify |
|---|---|---|---|
| Opcode semantics | `docs/instruction-set.md`, `docs/guides/add-opcode.md` | `instr/`, `interp/threaded.go`, `interp/jit_arm64.go` | `go test ./instr ./interp` |
| Runtime/stack/frame bug | `docs/architecture.md`, `docs/memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| Ref/GC/host function | `docs/memory-model.md`, `docs/value-representation.md` | `interp/host.go`, `interp/threaded.go`, `types/` | `go test ./interp ./types` |
| JIT/ARM64 backend | `docs/jit-internals.md`, `docs/value-representation.md` | `interp/jit*.go`, `asm/`, `asm/arm64/` | `go test ./asm/... ./interp` |
| Optimizer/pass | `docs/pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/` | `go test ./analysis ./transform ./optimize ./pass` |
| Bytecode verification / untrusted input | `docs/verification.md` | `program/verify.go`, `instr/type.go` | `go test ./program ./interp` |
| REPL/CLI | `docs/guides/repl.md` | `cli/`, `cmd/minivm/`, `instr/parse.go` | `go test ./cli/... ./cmd/minivm ./instr` |
| Debugger / stepping | `docs/debugging.md`, `docs/profile.md` | `interp/debugger.go`, `cli/repl.go` | `go test -race -run 'TestInterpreter_WithDebugger|TestDebugger_Breakpoints' ./interp` |
| Style-only change | `docs/coding-patterns.md` | touched package | package tests |
| Concurrent VM use | `docs/architecture.md` (`interp/`) | `interp/pool.go` | `go test -race ./interp` |

## Documentation Index

The Task Router above routes by task; this catalogs what each doc covers. Read only relevant docs.

| Document | Covers |
|---|---|
| `docs/architecture.md` | component map, package boundaries, ownership, execution flow |
| `docs/value-representation.md` | NaN-boxed `Boxed`, kind encoding, I64 heap spilling, dynamic `ref` |
| `docs/memory-model.md` | heap layout, reference counting, mark-and-sweep GC, invariants |
| `docs/profile.md` | sampling profiles, tick cadence, JIT thresholds, metrics |
| `docs/instruction-set.md` | full opcode reference: stack effects, operand widths, JIT status |
| `docs/jit-internals.md` | trace JIT contracts: tracer, lowerer, frame journal, calls, loops |
| `docs/pass-system.md` | analysis manager, transform pipeline, optimizer levels |
| `docs/verification.md` | static bytecode validator: checks, error sentinels, limits |
| `docs/coding-patterns.md` | style authority: naming, file layout, errors, testing |
| `docs/guides/add-opcode.md` | end-to-end checklist for adding an instruction |
| `docs/guides/add-architecture.md` | checklist for adding a JIT backend |
| `docs/guides/repl.md` | REPL commands, bytecode debugging, branch syntax |
| `docs/compatibility.md` | Go version, platform matrix, CGO, build tags, `unsafe` usage |
| `docs/host-integration.md` | `HostFunction`, `Marshal`/`Unmarshal`, host objects |
| `docs/benchmarks.md` | measured performance, cross-runtime comparison, methodology |
| `docs/debugging.md` | debugger API, breakpoints, stepping, inspection |

## Architecture Overview

minivm: bytecode VM + adaptive JIT.

```text
program.Program → threadedCompiler → []func(*Interpreter) → Interpreter.Run()
                                                        ├─ threaded closures
                                                        └─ hot segments promoted to native ARM64
```

Hot-segment compilation:

- profile samples record `(function, ip, opcode)` every 128 instructions
- JIT threshold defaults to 4096 ticks = 32 profile samples
- compiled native handlers replace threaded closures in-place

## Package Responsibilities

| Package | Responsibility |
|---|---|
| `program/` | bytecode + constants container |
| `instr/` | opcode definitions, encoding, parsing, formatting |
| `types/` | boxed values, arrays, structs, strings, NaN boxing |
| `interp/` | interpreter, threaded compiler, JIT driver |
| `asm/` | virtual-register IR, register allocation, executable buffers |
| `asm/arm64/` | ARM64 encoder, ABI, trampolines |
| `pass/` | generic pass pipeline |
| `analysis/` | shared analysis passes |
| `transform/` | optimization transforms |
| `optimize/` | optimization pipeline wiring |
| `cli/` | CLI command tree, REPL, and shared value formatting (`Root`, `NewRunCommand`, `NewREPL`, `WriteFS`, `OS`) |
| `cmd/minivm/` | CLI entrypoint |

## Key Invariants

Violations cause silent corruption or invalid execution.

### Runtime

- Heap index `0` is permanently `Null`.
- `release()` must stay iterative, never recursive.
- Threaded closure errors should `panic`; `interp.Run()` recovers and annotates `at=<ip>`.
- A `frame` separates `addr` (template/code index for `i.code`/`i.instrs`/profiler/JIT) from `ref` (heap index released on `RETURN`). They differ for closures; every frame-creating `CALL`/fused path must set both, and non-closure paths must reset `upvals = nil`.
- `closure.new` takes the function ref from the stack top (like `call`) and transfers ownership of the function ref plus its upvals into the closure.

### Threaded Compiler

- Advance `c.ip` during compile time.
- Advance `f.ip` during runtime execution.
- Missing either → invalid execution or infinite loops.

### JIT

- JIT handlers return `true` only after lowering the opcode and advancing `s.ip` by its exact width.
- On type mismatch or unsupported lowering, return `false` without mutating IR, stack, params, facts, or labels.
- Branch termination is determined by the trace boundary; branch handlers return `true` after successful terminal emission.
- Compile each entry IP at most once per JIT attempt; natural-fallthrough traces may expose compatible internal entries.
- Completed JIT segments emit when `count >= emit` default `8`; truncated and branch-terminated segments use the same cutoff.

### Executable Buffers

Always:

```text
Unseal → Append → Seal → Call
```

Incorrect ordering crashes on Apple Silicon.
`Seal()` must sync instruction cache (Darwin/ARM64); missing flushes → intermittent `SIGILL`.

### Optimization

- `ConstantFoldingPass` right-aligns folded instructions, pads left with NOPs.
- Preserve folded ranges until `DeadCodeEliminationPass` compacts bytecode and rewrites branches.
- Threaded NOP handlers absorb consecutive gaps with one runtime dispatch.
- Most passes preserve byte offsets; `GlobalValueNumberingPass` (O3) and `DeadCodeEliminationPass` are the exceptions. GVN uses the `transform.rewriter` to grow/shrink code, which re-derives every branch operand and handler offset, bails on int16 branch overflow, and bumps handler `Depth` by the number of locals it allocates (allocating a local shifts the operand-stack base). New local indexes must stay below 256. DCE compacts bytecode and likewise remaps branch and handler offsets; both write the root body's repaired code and handlers back to `prog`.

## Coding Expectations

- Apply `docs/coding-patterns.md` for every code change.
- Error design: explicit errors with context, preserve sentinels for `errors.Is`, panic only in interpreter-threaded paths recovered by `Run`.
- Test design: describe behavior, cover error paths + boundaries, organize under exported symbol.
- Match existing package structure and naming.
- Prefer small, cohesive packages.
- Avoid unnecessary abstractions.
- Keep opcode handlers explicit and predictable.
- Preserve interpreter/JIT behavioral parity.
- Avoid hidden control flow.

### Frequent Style Traps

These traps have been seen in past refactors and are easy to repeat. Each maps to a `docs/coding-patterns.md` section. Run through them before reporting a change as done.

- **Private package function with one-type use is a method (§1.5).** Even when the receiver is unused. Strategy callbacks pass as method values (`t.fn`), not as bare package functions. Package functions survive only when ≥2 types use them, the helper is reusable public utility, the helper is a constructor, or the helper has no struct in its call graph at all.
- **Single-call helpers stay inline (§1.4 + §1.5 counter-rule).** Do not extract a tiny helper to satisfy the method rule. ≥2 same-type call sites → method; 1 call site → inline at the caller, even if the result is a 15-line bit-packing block.
- **Private names use role words, not subsystem prefixes (§1.2).** If file, package, or receiver already says JIT/trace/cache, don't repeat it in every type: prefer `lowering`, `activation`, `value`, `step`, `compiler`, `module` over `jitContext`, `jitFrame`, `jitOperand`, `traceOperation`, `jitCompiler`, `jitModule`.
- **Don't expose interpreter internals through a callback interface to a downstream package (§0.5).** When pkg B integrates pkg A's output, A defines plain-value input/output structs; B fills the input, hands them over once, applies the output. Never define a `View`/`Engine` interface so A can reach back into B's mutable state.
- **Slot order moves with conversions (§2.4).** When a package function becomes a method per §1.5, move its declaration up into method territory. The reverse is also true — don't let a private function linger among methods. Constructors are the exception: private `newFoo` constructors stay above methods with public constructors.
- **Constructors above their `With*` options (§1.3).** `New` is the orchestrator; `WithX` are lower-level. The functional-options template puts `With*` first in many codebases — this repo does not.
- **Struct field layering distinguishes bridge state from runtime state (§2.5).** A "jitted" map that tracks integration with another package is infrastructure, not runtime state. Plain integer config (threshold, cutoff, tick, fuel) is read-only config near the bottom, not policy at the top.
- **Refactor steps that prove non-viable get recorded, not silently dropped.** If a plan step turns out to be the wrong call (e.g. extracting a shared tail when the middles diverge too much, or moving arch-local state when the owning type is a singleton), say so in the final summary with the reason. Future passes re-derive the same conclusion if you don't.
- **No test helpers, including white-box introspection (§6.1, §6.8).** Inline program construction, the run sequence (`Reset`/`Push`/`Run`/`Pop`), and any tracer-state scan into each `t.Run`. Do not extract a `branchTreeProgram()`, a `runEvalI32()`, or a `traceReturnsConst()` scan, even when the scan nests several loops and repeats across subtests. Do not add production API to tracer types just to shorten a test (§0.5).
- **One helper owns a branch-or-fallback decision, not every call site (§1.4).** When a helper already signals "ineligible/failed", don't re-state its precondition at each caller to choose between it and a fallback. JIT branch lowering routes `brIf`/`brTable` through one `branchOrExit` (try `continuation`, else `exit`) instead of repeating `branches[ip] != nil && len(frames) == 1 && !marked(...)` per target.

## Test Conventions

**Before writing/modifying tests, read relevant docs from Documentation Index and Task Router.**
**Read `docs/coding-patterns.md` §6, follow test-design rules.**

**One test func per exported symbol.** Sub-cases as `t.Run`, not separate top-level funcs.

```go
// CORRECT
func TestAssembler_Take(t *testing.T) {
    t.Run("from stack", func(t *testing.T) { ... })
    t.Run("fresh alloc", func(t *testing.T) { ... })
    t.Run("type mismatch", func(t *testing.T) { ... })
}

// WRONG — do not split into multiple top-level functions
func TestAssembler_Take_FromStack(t *testing.T)    { ... }
func TestAssembler_Take_FreshAlloc(t *testing.T)  { ... }
func TestAssembler_Take_TypeMismatch(t *testing.T) { ... }
```

- Name: `Test<Type>_<Method>` for methods, `Test<Func>` for functions.
- Use table-driven loops inside `t.Run` for repetitive cases.

## Documentation Maintenance

Update docs when invariant caused bug, command outdated, architecture changed, pitfall found, or doc needs indexing.

Keep edits terse + factual; document current behavior only; no speculative notes; preserve formatting; verify Markdown.

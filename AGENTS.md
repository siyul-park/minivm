# AGENTS.md

Agent guide for this repo (`Claude Code`, `Codex`, `Cursor`, etc.).

## Quick Commands

```bash
make test          # go test -race ./...
make benchmark     # go test -run="-" -bench=".*" -benchmem ./...
make lint          # goimports -w . && go vet ./...
make coverage      # generate coverage.out
make build         # build ./dist/minivm

go test -race ./...
go test -race -run TestFoo ./interp/...

./dist/minivm      # interactive assembly REPL
```

## Agent Workflow

1. `git status --short`; don't overwrite unrelated changes.
2. **For code exploration, prefer `codegraph` MCP tools over grep/read** (see Code Exploration below).
3. **Read task-relevant docs from Documentation Index before writing code or tests.**
4. **Before modifying code, read `docs/coding-patterns.md` and follow relevant sections.**
5. Mirror nearby tests; follow Test Conventions (one func per exported symbol, sub-cases as `t.Run`).
6. Update docs when behavior, invariants, commands, or pitfalls change.
7. Run narrow tests first, then `go test ./...` or `make test` for broad coverage.
8. Before reporting done, re-read every new/modified file against the pre-finish checklist in `.claude/CLAUDE.md` (or the equivalent in your agent's instruction file).

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
| REPL/CLI | `docs/guides/repl.md` | `cli/`, `cmd/minivm/`, `instr/parse.go` | `go test ./cli/... ./cmd/minivm ./instr` |
| Style-only change | `docs/coding-patterns.md` | touched package | package tests |
| Concurrent VM use | `docs/architecture.md` (`interp/`) | `interp/pool.go` | `go test -race ./interp` |

## Documentation Index

Read only relevant docs.

| Document | Read when… |
|---|---|
| `docs/architecture.md` | tracing execution or debugging across packages |
| `docs/value-representation.md` | modifying boxed values or JIT passing |
| `docs/memory-model.md` | touching refs, closures, GC, host functions |
| `docs/profile.md` | modifying profiling or tick cadence |
| `docs/instruction-set.md` | adding or debugging opcodes |
| `docs/jit-internals.md` | modifying threaded/JIT compilation |
| `docs/pass-system.md` | adding optimization or analysis passes |
| `docs/coding-patterns.md` | writing new code |
| `docs/guides/add-opcode.md` | implementing a new instruction |
| `docs/guides/add-architecture.md` | adding a new JIT backend |
| `docs/guides/repl.md` | extending or debugging the REPL |
| `docs/compatibility.md` | Go version, platform support, CGO, build tags |
| `docs/host-integration.md` | Marshal/Unmarshal, HostFunction, Go↔VM value conversion |
| `docs/benchmarks.md` | measured performance, cross-runtime comparison, JIT notes |

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

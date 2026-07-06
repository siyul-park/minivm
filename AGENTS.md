# AGENTS.md

Common agent guide for this repo. Applies to Claude Code and Codex.

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
4. **Before modifying code or tests, read `docs/coding-patterns.md` through its Fast Path:** always apply §0, then the task-relevant sections from its When to Read table.
5. Mirror nearby tests; follow Test Conventions and `docs/coding-patterns.md` §6.
6. Update docs using `docs/coding-patterns.md` §8 when behavior, invariants, commands, pitfalls, workflow, or conventions change.
7. On a fresh environment or CI-like run, start with `make init`; CI does this before lint/coverage.
8. Run narrow tests first, then `go test ./...`; use `make coverage` when you want the same broad validation CI runs.
9. `make benchmark` also runs the separate `benchmarks/` Go module. Use `make benchmark test-options="-count=2"` to match CI's comparison workflow.
10. For debugger, stepping, or breakpoint work, read `docs/debugging.md` and verify in `./interp`; `interp.WithDebugger` forces `WithTick(1)` and disables JIT.
11. **Before reporting done, perform the Agent Completion Gate below.**

## Agent Completion Gate

Do not report done, open/update a PR, or summarize a change as complete until every item below is true.

1. Every touched code/test file was re-read against `docs/coding-patterns.md` §0.7-§0.9 and the task-specific sections.
2. Every touched symbol has a current reason to exist.
3. Removable symbols were removed, inlined, merged, narrowed, made private, renamed by role, or replaced by direct local code.
4. A simpler algorithm or control flow was considered; the chosen shape is the simplest correct option found.
5. Another simplification pass found no safe improvement.
6. Declaration order follows `docs/coding-patterns.md` §1.3 and §2.4: callers before callees, except `With*` option functions may sit immediately above the constructor they configure.
7. Tests follow `docs/coding-patterns.md` §6.
8. PR/commit/docs expectations follow `docs/coding-patterns.md` §7-§8.
9. Any intentionally skipped simplification is recorded in the final summary with the reason.

For Claude Code, also apply `.claude/CLAUDE.md`. For Codex, this `AGENTS.md` is the required agent instruction source.

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

`.codex/hooks.json` runs `goimports` after `Edit`/`MultiEdit`/`Write` on `.go` files. Hooks are best-effort only. They do not replace the Agent Completion Gate; run `make lint` before finishing.

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
| `docs/coding-patterns.md` | style authority: principles, symbol review, naming, file layout, APIs, errors, tests, PR/docs rules |
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
program.Program -> threader -> []func(*Interpreter) -> Interpreter.Run()
                                                        |- threaded closures
                                                        `- hot segments promoted to native ARM64
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
- Missing either -> invalid execution or infinite loops.

### JIT

- JIT handlers return `true` only after lowering the opcode and advancing `s.ip` by its exact width.
- On type mismatch or unsupported lowering, return `false` without mutating IR, stack, params, facts, or labels.
- Branch termination is determined by the trace boundary; branch handlers return `true` after successful terminal emission.
- Compile each entry IP at most once per JIT attempt; natural-fallthrough traces may expose compatible internal entries.
- Completed JIT segments emit when `count >= emit` default `8`; truncated and branch-terminated segments use the same cutoff.

### Executable Buffers

Always:

```text
Unseal -> Append -> Seal -> Call
```

Incorrect ordering crashes on Apple Silicon.
`Seal()` must sync instruction cache (Darwin/ARM64); missing flushes -> intermittent `SIGILL`.

### Optimization

- `ConstantFoldingPass` right-aligns folded instructions, pads left with NOPs.
- Preserve folded ranges until `DeadCodeEliminationPass` compacts bytecode and rewrites branches.
- Threaded NOP handlers absorb consecutive gaps with one runtime dispatch.
- Most passes preserve byte offsets; `GlobalValueNumberingPass` (O3) and `DeadCodeEliminationPass` are the exceptions. GVN uses the `transform.rewriter` to grow/shrink code, which re-derives every branch operand and handler offset, bails on int16 branch overflow, and bumps handler `Depth` by the number of locals it allocates (allocating a local shifts the operand-stack base). New local indexes must stay below 256. DCE compacts bytecode and likewise remaps branch and handler offsets; both write the root body's repaired code and handlers back to `prog`.

## Coding Pattern Usage

`docs/coding-patterns.md` is the authority. This section routes agents to the right parts; it is not a replacement.

| Need | Read in `docs/coding-patterns.md` |
|---|---|
| Before any code/test edit | When to Read, §0 |
| Removing unnecessary structure | §0.1, §0.7-§0.9 |
| Naming, helper extraction, method ownership | §1.2, §1.4, §1.5 |
| File order, type/interface shape, struct fields | §2.1-§2.5 |
| Public API, options, builders, parsers | §3 |
| Errors, panic, recover | §4 |
| Architecture build tags | §5 |
| Tests | §6 |
| Commits, PRs, final review | §7 |
| Documentation updates | §8 |

Claude-specific reminders live in `.claude/CLAUDE.md`. Codex-specific enforcement lives in this file through the Agent Workflow and Agent Completion Gate.

## Test Conventions

Before writing or modifying tests, read relevant docs from Documentation Index and Task Router, then apply `docs/coding-patterns.md` §6.

Core reminders:

- One top-level test per public symbol: `Test<Func>` or `Test<Type>_<Method>`.
- Put sub-cases under `t.Run`; do not split them into parallel top-level tests.
- Inline setup, run sequence, and assertions unless §6.8 allows a helper.
- Use `require`, not `assert`.

## Documentation Maintenance

Update docs when behavior, invariants, commands, architecture, pitfalls, workflow, or conventions change. Use the owner matrix in `docs/coding-patterns.md` §8:

- workflow / convention rules -> update both `AGENTS.md` and `.claude/CLAUDE.md`
- invariants / pitfalls -> update `docs/architecture.md`
- opcode semantics / JIT status -> update `docs/instruction-set.md`
- JIT contracts / assembler APIs -> update `docs/jit-internals.md`

Keep edits terse and factual; document current behavior only; preserve formatting; verify Markdown.

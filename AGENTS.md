# AGENTS.md

Repository instructions for Codex and Claude Code.

This file is the common agent contract. Codex reads `AGENTS.md` directly. Claude Code loads `.claude/CLAUDE.md`, which imports this file and adds Claude-specific reminders.

Keep this file terse and actionable. Put detailed coding rules in `docs/coding-patterns.md`, not here.

## Instruction Priority

1. Follow the user's latest explicit request first.
2. Follow the closest applicable repository instruction file.
3. Use this file as the root repository contract.
4. Use `docs/coding-patterns.md` as the coding-style authority.
5. Match nearby code when it is stricter than this guide.

If instructions conflict, choose the more specific instruction and mention the conflict in the final summary.

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

## Required Workflow

1. Run `git status --short`; never overwrite unrelated user changes.
2. Prefer `codegraph` MCP tools for structural exploration; fall back to grep/read only for literal text or stale indexes.
3. Read task-relevant docs from the Task Router before changing code or tests.
4. Read `docs/coding-patterns.md` through its Fast Path: always apply §0, then the task-specific sections from its When to Read table.
5. Make the smallest correct change. Avoid speculative cleanup outside the task.
6. Validate with the narrowest relevant tests first, then broader tests when the change warrants it.
7. Run the Completion Gate before reporting done, opening a PR, or updating a PR.

## Completion Gate

Do not call work complete until every item is true:

1. Every touched code/test file was re-read against `docs/coding-patterns.md` §0.7-§0.9 plus the task-specific sections.
2. Every touched symbol has a current reason to exist.
3. Removable symbols were removed, inlined, merged, narrowed, made private, renamed by role, or replaced by direct local code.
4. A simpler algorithm or control flow was considered; the chosen shape is the simplest correct option found.
5. Another simplification pass found no safe improvement.
6. Declaration order follows `docs/coding-patterns.md` §1.3 and §2.4: callers before callees, except `With*` option functions may sit immediately above the constructor they configure.
7. Tests follow `docs/coding-patterns.md` §6 and assert behavior rather than private shape.
8. PR, commit, and documentation expectations follow `docs/coding-patterns.md` §7-§8.
9. Any intentionally skipped simplification is recorded in the final summary with the reason.

## Coding Pattern Map

`docs/coding-patterns.md` is the authority. Use this map only to choose what to read.

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

Read only docs relevant to the task.

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
| `docs/testing.md` | executable specification layers, API ownership, opcode coverage |
| `docs/benchmarks.md` | measured performance, cross-runtime comparison, methodology |
| `docs/debugging.md` | debugger API, breakpoints, stepping, inspection |

## Code Exploration

Prefer `codegraph` MCP tools over grep/read for structural questions.

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

## Project Map

```text
program.Program -> threader -> []func(*Interpreter) -> Interpreter.Run()
                                                        |- threaded closures
                                                        `- hot segments promoted to native ARM64
```

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
| `cli/` | CLI command tree, REPL, and shared value formatting |
| `cmd/minivm/` | CLI entrypoint |

## Key Invariants

Violations cause silent corruption or invalid execution.

- Heap index `0` is permanently `Null`.
- `release()` must stay iterative, never recursive.
- Threaded closure errors should `panic`; `interp.Run()` recovers and annotates `at=<ip>`.
- A `frame` separates `addr` (template/code index for code/profiler/JIT) from `ref` (heap index released on `RETURN`). They differ for closures; every frame-creating `CALL`/fused path must set both, and non-closure paths must reset `upvals = nil`.
- `closure.new` takes the function ref from the stack top and transfers ownership of the function ref plus upvals into the closure.
- Compile-time threaded code advances `c.ip`; runtime threaded execution advances `f.ip`.
- JIT handlers return `true` only after lowering the opcode and advancing `s.ip` by its exact width.
- On JIT type mismatch or unsupported lowering, return `false` without mutating IR, stack, params, facts, or labels.
- Executable buffers must follow `Unseal -> Append -> Seal -> Call`; `Seal()` must sync the instruction cache on Darwin/ARM64.
- Offset-preserving passes must preserve byte offsets; `GlobalValueNumberingPass` and `DeadCodeEliminationPass` are the known exceptions and must repair branches/handlers.
- `asm.Relaxer.Relax` implementations must return a replacement sequence that is already in range; `asm.Assembler.encode`'s fixpoint loop relies on this to relax each branch at most once and terminate.
- A JIT trace fragment's own `kind` (outcome), not the root trace's, decides whether `arm64Lowerer.walk` may lower it as a normal completion when its ops run out; only `aborted` must never fall through as completion.
- `tree.branchIPs()` excludes `aborted` branches from continuation-inlining eligibility; a fragment that recorded a partial, unsupported prefix must never be inlined into a parent trace.
- `interp/jit.go`'s `spillSafe` must scan the whole trace tree (root plus every branch it may inline), not just the root's last op, before deciding a trace is safe to spill.

## Tests

Use `docs/testing.md` for ownership and opcode coverage status. Before writing or modifying tests, read relevant docs from the Task Router and apply `docs/coding-patterns.md` §6.

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

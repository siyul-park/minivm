# AGENTS.md

Repository instructions for Codex and Claude Code.

This file is the common agent contract. Codex reads `AGENTS.md` directly. Claude Code loads `.claude/CLAUDE.md`, which imports this file and adds Claude-specific reminders.

Keep this file terse and actionable. Put detailed coding rules in `docs/coding-patterns.md`, not here.

## Instruction Priority

1. Follow the user's latest explicit request first.
2. Follow the closest applicable repository instruction file.
3. Use this file as the root repository workflow contract.
4. Apply `docs/coding-patterns.md` as the normative coding specification.
5. Match nearby code only when it is more specific and specification-compliant.

If instructions conflict, choose the more specific instruction and record the conflict in the final summary.

## Quick Commands

```bash
make init              # install goimports/godoc and go install ./...
make test              # go test -race ./...
make benchmark-pr      # quick pull-request benchmark report
make benchmark-core    # full canonical package + VM kernel suite
make benchmark-compare # optional external runtime comparisons
make fuzz              # bounded trust-boundary fuzz smoke
make lint              # goimports -w . && go vet ./...
make coverage          # CI-style full test run with coverage.out
make coverage-check    # enforce recorded total-coverage baseline
make build             # build ./dist/minivm

go test -race ./...
go test -race -run TestFoo ./interp/...
go test -race -run 'TestInterpreter_WithDebugger|TestDebugger_Breakpoints' ./interp

./dist/minivm          # interactive assembly REPL
```

## Required Workflow

1. Run `git status --short`; never overwrite or commit unrelated user changes.
2. Prefer structural tools for symbol ownership and call-flow exploration; use grep/read for literal text and final verification.
3. Read task-relevant docs from the Task Router before changing code or tests.
4. Apply `docs/coding-patterns.md` §2 and §16 to every code/test change, plus sections selected by §1.3.
5. Review top-down from package contract to mechanics and bottom-up across every affected symbol. Repository-wide refactors MUST inventory every production and test symbol.
6. Make the smallest correct change. Do not add speculative structure or preserve obsolete compatibility without an explicit contract.
7. Validate the narrowest relevant behavior first, then race, static, generated, architecture, and benchmark checks warranted by the change.
8. Run the Completion Gate before reporting done, committing a logical stage, opening a PR, or updating a PR.

## Completion Gate

Do not call work complete until every item is true:

1. Every changed file was re-read against `docs/coding-patterns.md` §2 and the task-specific sections.
2. Top-down ownership and bottom-up symbol reviews are complete at the requested scope.
3. Every affected symbol has a current reason to exist; removable symbols were removed, inlined, merged, narrowed, privatized, or renamed by role.
4. The chosen algorithm and control flow are the simplest correct options found without a measured performance regression.
5. Another simplification pass found no safe improvement.
6. Declarations form a caller-before-callee staircase and follow specification §9.
7. Tests follow specification §12 and assert only public observable behavior.
8. Performance claims include the reproducible evidence required by specification §14.
9. Commits and documentation follow specification §15, and unrelated user changes are absent.
10. Any intentionally skipped simplification or validation is recorded with its reason.

## Coding Standard Map

`docs/coding-patterns.md` is normative. Use this map only for routing.

| Need | Read in `docs/coding-patterns.md` |
|---|---|
| Every code/test change | §2, §16 |
| Functions, helpers, naming | §3-§4 |
| Types, constructors, public APIs | §5 |
| Package and runtime ownership | §6-§8 |
| Declaration and field order | §9 |
| Errors and panic/recover | §10 |
| Concurrency and lifecycle | §11 |
| Tests and public specifications | §12 |
| Generated and architecture code | §13 |
| Performance and benchmarks | §14 |
| Commits and documentation | §15 |

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
- Strings carry no identity invariant: every string comparison and every `string`-keyed map compares content, so equal contents may occupy different refs. Only the constant pool deduplicates, at load time.
- `string.concat` never mutates a published string. Joins share one append-only buffer per interpreter and always publish a new ref; growth only writes above every published length, `Reset` clears the buffer, and a speculative trace clone must start its own.
- Compile-time threaded code advances `c.ip`; runtime threaded execution advances `f.ip`.
- JIT handlers return `true` only after lowering the opcode and advancing `s.ip` by its exact width.
- On JIT type mismatch or unsupported lowering, return `false` without mutating IR, stack, params, facts, or labels.
- Executable buffers publish each code block from a fresh mapping inside `asm.Buffer.install`; it must sync the instruction cache on Darwin/ARM64 before sealing, and published mappings remain immutable and executable until `Buffer.Free`.
- Offset-preserving passes must preserve byte offsets; `GVNPass` and `DCEPass` are the known exceptions and must repair branches/handlers.
- `asm.Relaxer.Relax` implementations must return a replacement sequence that is already in range; `asm.Assembler.encode`'s fixpoint loop relies on this to relax each branch at most once and terminate.
- A JIT trace fragment's own `status`, not the root trace's, decides how its ops lower when they run out; `tracePlan` must skip any `aborted` root or branch, so a fragment that recorded a partial, unsupported prefix is never planned or inlined into a parent trace.
- `noSpill` (`interp/jit_plan.go`) must scan the whole plan (every block, not just the root's tail) before allowing spilling; when it rejects, `noSpillArch` forces `asm.Build` to fail instead of inserting a spill frame.
- A deferred (slot-backed or const) ref operand carries no retain of its own; it must be owned or redeemed before any path that hands the flushed operand stack to the interpreter (ownership transfer, guard exit stub, trap-fallback/module-completion redeem, or a real call), and a committing (loop back-edge) flush rejects any live deferred ref.
- Eligible call-free native loops keep up to seven read-written inline scalar locals authoritative in X19-X25 across the back-edge; every guard exit, fallback, yield, completion, state barrier, or continuation that can expose or reload VM slots must commit or preserve those registers first, and ineligible plans keep the per-iteration slot commit.
- Hoisted container registers are valid only within one native loop entry: the prologue re-guards tag and itab on every entry, hoist eligibility requires a call-free loop plan with no store to the container local, `asm.OpPseudoUse` keeps the derived registers live across the native back-edge, and a loop fallback that resumes at the header must run the shadowed threaded handler once (the header slot holds the native stub, so redispatching it would livelock).

## Tests

Use `docs/testing.md` for ownership and opcode coverage status. Before writing or modifying tests, read relevant docs from the Task Router and apply `docs/coding-patterns.md` §12.

- One top-level test per public symbol: `Test<Func>` or `Test<Type>_<Method>`.
- Every test package uses the production package name plus `_test` and acts as an importing client.
- Put sub-cases under `t.Run`; do not split them into parallel top-level tests.
- Keep setup, execution, and assertions visible unless specification §12 permits a real reusable abstraction.
- Tests access no private symbol or representation; internal invariants are asserted through public observable behavior, generated output, or executable boundaries.
- Use `require`, not `assert`.

## Documentation Maintenance

Update docs when behavior, invariants, commands, architecture, pitfalls, workflow, or conventions change. Use the owner matrix in `docs/coding-patterns.md` §15:

- workflow / convention rules -> update both `AGENTS.md` and `.claude/CLAUDE.md`
- invariants / pitfalls -> update `docs/architecture.md`
- opcode semantics / JIT status -> update `docs/instruction-set.md`
- JIT contracts / assembler APIs -> update `docs/jit-internals.md`

Keep edits terse and factual; document current behavior only; preserve formatting; verify Markdown.

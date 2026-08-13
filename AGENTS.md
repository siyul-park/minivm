# AGENTS.md

Repository instructions for coding agents. Codex reads this file directly; Claude Code loads `.claude/CLAUDE.md`, which imports it.

`docs/coding-patterns.md` is the normative coding specification and is binding: a change that violates it is not complete, however well it works. Read the section that governs a change before writing it, and match nearby code only after confirming that precedent is specification-compliant. When instructions conflict, follow the more specific one and record the conflict in the final summary.

## Commands

```bash
make init              # install goimports/godoc and go install ./...
make test              # go test -race ./...
make lint              # goimports -w . && go vet ./...
make generate          # regenerate interp/threaded.go from internal/cmd/geninterp
make check-generated   # fail if the generated file is stale
make build             # build ./dist/minivm
make fuzz              # bounded trust-boundary fuzz smoke
make coverage-check    # enforce recorded total-coverage baseline
make benchmark-pr      # quick pull-request benchmark report
make benchmark-core    # full canonical package + VM kernel suite
make benchmark-compare # external runtime comparisons (compare build tag)
```

Run a subset with `go test -race -run TestFoo ./interp/...`. `./dist/minivm` starts the interactive assembly REPL.

## Architecture

```text
program.Program -> threader -> []func(*Interpreter) -> Interpreter.Run()
                                                        |- threaded closures
                                                        `- hot segments promoted to native ARM64
```

A `program.Program` is threaded into one closure per instruction; a profiler promotes hot segments to native ARM64 and falls back to the threaded closures for anything the backend cannot lower. `docs/architecture.md` owns package boundaries and execution flow; `docs/README.md` indexes every topic document.

**`interp/threaded.go` is generated.** Edit the emitters in `internal/cmd/geninterp/lower.go` (fusion patterns in `pattern.go`), then run `make generate`. Never hand-edit the generated file.

## Workflow

1. Run `git status --short`; never overwrite or commit unrelated user changes.
2. Read the Task Router docs for the area before changing code or tests.
3. Apply `docs/coding-patterns.md` §2 and §16 to every code/test change, plus the sections its §1.3 selects.
4. Review top-down from package contract to mechanics, and bottom-up across every affected symbol. Repository-wide refactors MUST inventory every production and test symbol.
5. Validate the narrowest relevant behavior first, then the race, static, generated, and benchmark checks the change warrants.

Prefer `codegraph` MCP tools over grep for structural questions (definitions, callers, call flow, impact).

### Completion Gate

Do not report work complete until all of these hold:

1. Every changed file was re-read against `docs/coding-patterns.md` §2 and the task-specific sections.
2. Every affected symbol still has a reason to exist; removable ones were removed, inlined, merged, narrowed, privatized, or renamed by role.
3. A further simplification pass found no safe improvement.
4. Tests follow §12 and sit with the owner `docs/testing.md` assigns.
5. Performance claims carry the reproducible before/after evidence §14 requires.
6. Generated output was regenerated, not hand-edited, and `make check-generated` passes.
7. Documentation was updated per the §15 owner matrix and unrelated user changes are absent.
8. Any intentionally skipped simplification or validation is recorded with its reason.

## Task Router

| Task | Read | Usually edit | Verify |
|---|---|---|---|
| Opcode semantics | `docs/instruction-set.md`, `docs/guides/add-opcode.md` | `internal/cmd/geninterp/`, `instr/`, `interp/jit_arm64.go` | `go test ./instr ./interp` |
| Runtime/stack/frame bug | `docs/architecture.md`, `docs/memory-model.md` | `interp/`, `types/` | `go test ./interp ./types` |
| Ref/GC/host function | `docs/memory-model.md`, `docs/value-representation.md` | `interp/host.go`, `types/` | `go test ./interp ./types` |
| JIT/ARM64 backend | `docs/jit-internals.md`, `docs/value-representation.md` | `interp/jit*.go`, `asm/`, `asm/arm64/` | `go test ./asm/... ./interp` |
| Optimizer/pass | `docs/pass-system.md` | `analysis/`, `transform/`, `optimize/`, `pass/` | `go test ./analysis ./transform ./optimize ./pass` |
| Bytecode verification / untrusted input | `docs/verification.md` | `program/verify.go`, `instr/type.go` | `go test ./program ./interp` |
| REPL/CLI | `docs/guides/repl.md` | `cli/`, `cmd/minivm/`, `instr/parse.go` | `go test ./cli/... ./cmd/minivm ./instr` |
| Debugger / stepping | `docs/debugging.md`, `docs/profile.md` | `interp/debugger.go`, `cli/repl.go` | `go test -race -run 'TestInterpreter_WithDebugger\|TestDebugger_Breakpoints' ./interp` |
| Concurrent VM use | `docs/architecture.md` (`interp/`) | `interp/pool.go` | `go test -race ./interp` |

## Key Invariants

Violations cause silent corruption or invalid execution. `docs/architecture.md` §Key Invariants holds the full list; `docs/jit-internals.md` holds the JIT contracts.

- Heap index `0` is permanently `Null`, and `release()` must stay iterative, never recursive.
- A `frame` separates `addr` (template/code index) from `ref` (heap index released on `RETURN`); they differ for closures, so every frame-creating `CALL`/fused path sets both and non-closure paths reset `upvals = nil`.
- Strings compare by content, never identity; `string.concat` publishes a new ref and never mutates a published string.
- Threaded closure errors `panic`; `interp.Run()` recovers and annotates `at=<ip>`. Compile-time threading advances `c.ip`, runtime execution advances `f.ip`.
- A JIT handler returns `true` only after lowering the opcode and advancing `s.ip` by its exact width; on type mismatch or unsupported lowering it returns `false` without mutating IR, stack, params, facts, or labels.
- Any JIT path that can hand state back to the interpreter — guard exit, fallback, spill decision, loop back-edge, hoisted container — has an exact contract in `docs/jit-internals.md`. Read it before touching one.
- Offset-preserving passes must preserve byte offsets; `GVNPass` and `DCEPass` are the known exceptions and must repair branches/handlers.

## Tests

Apply `docs/coding-patterns.md` §12; before adding a **new test file**, read §12.1 and the layer/owner table in `docs/testing.md`, because most new coverage belongs in an existing owner.

- Each test file MUST match the production file owning the symbol: `foo_test.go` requires `foo.go`. Only the per-package `fuzz_test.go` and `example_test.go` conventions are exempt; catch-all concept files MUST NOT be created.
- A change touching threaded, fused, optimized, or JIT paths MUST cover every applicable mode, or state in the final summary why a mode is not applicable.
- One top-level test per public symbol: `Test<Func>` or `Test<Type>_<Method>`; sub-cases go under `t.Run`.
- Every test package uses the production package name plus `_test`, acts as an importing client, and accesses no private symbol or representation. Assert internal invariants through public behavior, generated output, or executable boundaries.
- Keep setup, execution, and assertions visible unless §12 permits a real reusable abstraction, and use `require`, not `assert`.

## Documentation Maintenance

Update docs when behavior, invariants, commands, architecture, workflow, or conventions change, using the owner matrix in `docs/coding-patterns.md` §15:

- workflow / convention rules -> `AGENTS.md` and `.claude/CLAUDE.md`
- invariants / pitfalls -> `docs/architecture.md`
- opcode semantics / JIT status -> `docs/instruction-set.md`
- JIT contracts / assembler APIs -> `docs/jit-internals.md`

Keep edits terse and factual, document current behavior only, and preserve formatting.

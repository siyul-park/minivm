# AGENTS.md

Repository instructions for Codex and Claude Code. Codex reads this file directly; Claude Code loads `.claude/CLAUDE.md`, which imports it.

`docs/coding-patterns.md` is the normative coding specification. Keep detailed rules there, not here. When instructions conflict, choose the more specific one and record the conflict in the final summary.

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

Run a single test or a subset:

```bash
go test -race -run TestFoo ./interp/...
go test -race -run 'TestInterpreter_WithDebugger|TestDebugger_Breakpoints' ./interp
```

`./dist/minivm` starts the interactive assembly REPL.

## Architecture

```text
program.Program -> threader -> []func(*Interpreter) -> Interpreter.Run()
                                                        |- threaded closures
                                                        `- hot segments promoted to native ARM64
```

Execution is a closure-threaded interpreter with an adaptive trace JIT. A `program.Program` is threaded into one closure per instruction; a profiler promotes hot segments to native ARM64 and falls back to the threaded closures for anything the backend cannot lower.

| Package | Responsibility |
|---|---|
| `program/` | bytecode + constants container |
| `instr/` | opcode definitions, encoding, parsing, formatting |
| `types/` | boxed values, arrays, structs, strings, NaN boxing |
| `interp/` | interpreter, threaded compiler, JIT driver |
| `asm/`, `asm/arm64/` | virtual-register IR, register allocation, executable buffers, ARM64 encoder/ABI |
| `pass/`, `analysis/`, `transform/`, `optimize/` | pass pipeline, analyses, transforms, optimizer wiring |
| `cli/`, `cmd/minivm/` | CLI command tree, REPL, entrypoint |

**`interp/threaded.go` is generated.** Edit the emitters in `internal/cmd/geninterp/lower.go` (and fusion patterns in `pattern.go`), then run `make generate`. Never hand-edit the generated file.

## Workflow

1. Run `git status --short`; never overwrite or commit unrelated user changes.
2. Read the Task Router docs for the area before changing code or tests.
3. Apply `docs/coding-patterns.md` §2 and §16 to every code/test change, plus the sections its §1.3 selects.
4. Review top-down from package contract to mechanics, and bottom-up across every affected symbol. Repository-wide refactors MUST inventory every production and test symbol.
5. Validate the narrowest relevant behavior first, then the race, static, generated, and benchmark checks the change warrants.

### Completion Gate

Do not report work complete until all of these hold:

1. Every changed file was re-read against `docs/coding-patterns.md` §2 and the task-specific sections.
2. Every affected symbol still has a reason to exist; removable ones were removed, inlined, merged, narrowed, privatized, or renamed by role.
3. A further simplification pass found no safe improvement.
4. Tests follow §12 and assert only public observable behavior.
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

`docs/README.md` indexes the full documentation set. Prefer `codegraph` MCP tools over grep for structural questions (definitions, callers, call flow, impact).

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

Apply `docs/coding-patterns.md` §12; `docs/testing.md` carries ownership and opcode coverage status.

- One top-level test per public symbol: `Test<Func>` or `Test<Type>_<Method>`; sub-cases go under `t.Run`.
- Every test package uses the production package name plus `_test` and acts as an importing client.
- Tests access no private symbol or representation. Assert internal invariants through public behavior, generated output, or executable boundaries.
- Keep setup, execution, and assertions visible unless §12 permits a real reusable abstraction.
- Use `require`, not `assert`.

## Documentation Maintenance

Update docs when behavior, invariants, commands, architecture, workflow, or conventions change, using the owner matrix in `docs/coding-patterns.md` §15:

- workflow / convention rules -> `AGENTS.md` and `.claude/CLAUDE.md`
- invariants / pitfalls -> `docs/architecture.md`
- opcode semantics / JIT status -> `docs/instruction-set.md`
- JIT contracts / assembler APIs -> `docs/jit-internals.md`

Keep edits terse and factual, document current behavior only, and preserve formatting.

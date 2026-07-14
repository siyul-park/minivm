# Coding Patterns

Default coding style for minivm contributors and agents.

Use this before writing or changing code. Match nearby code first; use this document when local style is unclear or a new pattern is needed.

## When to Read

Read only what the change touches.

| Change | Read |
|---|---|
| function shape, helper extraction, naming | 0. Principles, 1. Functions |
| types, interfaces, fields, constructors | 2. Types |
| public APIs, options, builders, parsers | 3. APIs |
| errors, panic, recover | 4. Errors |
| architecture-specific files | 5. Build Tags |
| tests | 6. Tests |
| commits and PRs | 7. Git and PRs |
| documentation changes | 8. Docs |

## Source of Truth

| Concern | Source |
|---|---|
| formatting | `gofmt` |
| package style | nearby code in the same package |
| public API shape | existing public APIs and tests |
| documentation shape | `docs/README.md` |
| architectural ownership | `docs/architecture.md` |

## 0. Principles

### 0.1 Simpler is Better

If two designs provide the same behavior, choose the simpler one: fewer files, types, functions, methods, arguments, names, indirections, and more local code.

Add abstraction only when it improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### 0.2 Keep Public Surfaces Small

Push complexity down. Prefer simple APIs over exposed mechanisms, explicit behavior over hidden magic, local complexity over distributed state, and predictable structure over clever abstraction.

A difficult behavior should have one clear implementation, not many partially difficult call sites.

### 0.3 Read Top-Down

Put important behavior first and details later.

```text
Run
  parse
  exec
  commit
```

Readers should not jump around to reconstruct the main path.

### 0.4 Be Obvious

Prefer mechanically obvious code over clever code. Slightly longer code is better when it avoids hidden state, improves debugging, preserves interpreter/JIT symmetry, makes control flow explicit, or keeps behavior easy to verify.

### 0.5 Preserve Symmetry

Keep interpreter and JIT paths structurally similar when possible. Symmetry matters more than small local optimizations because it makes behavior easier to compare, test, and maintain.

### 0.6 Keep Related Code Close

Keep state, validation, mutation, and cleanup for one behavior near each other. Split only when ownership or readability clearly improves.

### 0.7 Review Every Symbol

Every file, type, interface, struct, field, function, method, parameter, result, constant, and variable needs a reason to exist.

For every touched symbol, ask whether it can be removed, inlined, merged with an existing owner, narrowed in scope, made private, renamed by role, or replaced by direct local code. Review nearby old symbols exposed by the change too.

A refactor is incomplete if it adds structure while leaving now-obvious dead fields, arguments, results, helpers, or wrapper files behind. If the only reason is future flexibility, symmetry without behavior, shorter code, or one-call-site convenience, remove it.

### 0.8 Prefer Simpler Algorithms

Before adding structure, look for a simpler or more efficient algorithm.

Prefer one direct pass over coordinated passes, local state over global maps, exact ownership over cleanup protocols, and data flow matching the runtime model. Do not optimize by hiding behavior; keep correctness, interpreter/JIT parity, and verifier/runtime invariants obvious.

Performance claims need benchmark evidence.

### 0.9 Iterate Until Stable

Simplify in passes. Removing one symbol, helper, field, pass, or branch often exposes the next simplification.

Each pass checks: removable symbols, narrower ownership or visibility, simpler control flow, simpler algorithms, then tests/docs matching the final shape. Stop only when another pass finds no safe improvement.

Record intentionally non-viable simplifications in the final summary so future work does not re-derive them silently.

## 1. Functions

### 1.1 Use One Abstraction Level

Each function should operate at one conceptual level. Do not mix orchestration with parsing details, policy with arithmetic, or high-level flow with byte/index mutation.

Good functions read like behavior:

```go
func (r *REPL) Run(ctx context.Context) error {
    for {
        line, err := r.read()
        if err != nil { return err }

        inst, err := r.parse(line)
        if err != nil { return err }

        if err := r.exec(ctx, inst); err != nil { return err }
        r.commit(inst)
    }
}
```

If comments explain transitions between unrelated steps, the function likely mixes levels.

### 1.2 Name by Role

Names describe caller-visible behavior, not implementation mechanics. Use the shortest standard name that is still clear; prefer one word when package, file, receiver, or context already provides meaning.

| Avoid | Prefer |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `appendInstrAndUpdateLen` | `commit` |
| `checkEmptyAndFormatProg` | `show` |
| `jitContext` | `lowering` |
| `jitFrame` | `activation` |
| `jitOperand` | `value` |
| `traceOperation` | `step` |

Receiver context counts: `r.show()` and `r.commit()` are clear on `*REPL`.

Avoid names that repeat package/file/subsystem context, non-standard abbreviations, one-letter names outside tight scopes, and implementation-step names when a role name is enough.

Allowed abbreviations: common domain terms such as `ID`, `IP`, `ABI`, `JIT`, `VM`, and `CPU`.

Good role words: `value`, `step`, `state`, `frame`, `module`, `target`, `source`, `lowering`, `activation`, `compiler`, `builder`, `cache`, `trace`, `root`, `exit`.

Short and clear is the goal; cryptic is not.

### 1.3 Declare Callers Before Callees

Within a file and section, place high-level functions before helpers they call. Functional options may appear immediately before the constructor they configure so read order matches call sites like `New(prog, WithTick(1))`.

### 1.4 Extract Only When Useful

Inline simple single-use logic.

Extract only when a helper removes real duplication, isolates real complexity, names meaningful behavior, or keeps the caller at one abstraction level. Do not extract tiny helpers that hide a short switch, predicate, or loop used once.

Let one helper own a branch-or-fallback decision. If it can decide whether it applies and return failure/fallback, callers should not duplicate its preconditions.

### 1.5 Methods Show Ownership

A function used by one type belongs on that type, even when the receiver is not used directly. Callbacks should also be methods when ownership belongs to a type.

```go
func (l Lowerer) cmp(c *Context) bool { return l.compare(c, l.sign32) }
```

Package functions are for constructors, functions used by multiple types, public general utilities, or helpers used only by other package-level functions.

Do not extract a tiny single-use method just to satisfy ownership; inline it instead.

### 1.6 Constructors Are Functions

Constructors are standalone functions, never methods.

```go
func newCompiler(...) *compiler
func NewOptimizer(...) *Optimizer
```

Public concrete types use `NewType`; private concrete types use `newType`.

### 1.7 Keep Methods with Their Type

A file should contain methods for one main type. Split a large type across files by concern if needed, but do not place methods for type A in type B's file just for locality.

For JIT code, `interp/jit.go` owns JIT-neutral compiler/module/trace orchestration, `interp/jit_arm64.go` owns ARM64 lowering, and `*Interpreter` JIT bridge behavior stays with `*Interpreter`.

## 2. Types

Add a type only when it owns data with behavior, names a real domain concept, or prevents repeated error-prone structure. Do not add one to group temporary values, hide a simple tuple, or create future abstraction.

### 2.1 Define Interfaces Where Consumed

Interfaces belong in the package that consumes behavior, not the package that implements it. They describe what the caller needs.

### 2.2 Prefer Private Type, Public Instance

When there is one meaningful implementation, use an unexported concrete type with an exported value.

```go
type i32Type struct{}
var TypeI32 = i32Type{}
```

### 2.3 Keep Interfaces Small

Do not create an interface until a consumer needs it. Do not add methods for later.

Assert compliance near the related type, in the private value section:

```go
var _ Traceable = (*Struct)(nil)
```

### 2.4 File Layout

Use this order in every `.go` file:

1. public types
2. private types
3. public constants
4. private constants
5. public variables
6. private variables
7. constructors
8. public functions
9. public methods
10. private methods
11. private functions

Within each group, keep callers before callees. When a package function becomes a method, move it into method territory; constructors stay in the constructor section.

### 2.5 Order Struct Fields by Meaning

Order fields by how readers understand the type:

1. lifecycle and policy objects
2. infrastructure
3. program data
4. runtime state
5. mutable counters
6. read-only config
7. sync primitives

Separate layers with a blank line. Put rich behavioral objects near the top, mutable counters above read-only config, plain numeric config near the bottom, and `sync.Mutex` last. Struct literals follow field declaration order.

Field names should be short and clear; prefer one word when possible.

## 3. APIs

Public APIs should make the common path obvious, keep advanced behavior explicit, and avoid exposing internal representation without a stable caller need.

### 3.1 Constructors

Constructor names use `New<Type>` for public types and `new<Type>` for private types. Constructors establish invariants; if a value has no invariants, a struct literal may be clearer.

### 3.2 Parsers

Parser names:

| Function | Meaning |
|---|---|
| `Parse` | package primary type |
| `Parse<Type>` | secondary type |
| `ParseAll` | multiple values, usually from `io.Reader` |

```go
func Parse(s string) (Type, error)
func ParseFunction(lines []string) (*Function, error)
func ParseAll(r io.Reader) ([]Instruction, error)
```

### 3.3 Options

Prefer functional options over config structs. Use direct arguments for required behavior and options for rare configuration. Apply defaults first, then options.

```go
func New(prog *program.Program, opts ...func(*option)) *Interpreter {
    opt := option{stack: 1024, heap: 128, threshold: 4096}
    for _, fn := range opts { fn(&opt) }
    ...
}
```

### 3.4 Builders

Builders are mutable; built values are treated as immutable. Discard builders after `Build()`.

```go
fn := types.NewFunctionBuilder(&types.FunctionType{}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0)).
    Build()
```

A builder constructs one thing, validates near construction, and must not become a general mutable config store.

### 3.5 Avoid Premature API Surface

Do not add public methods, options, interfaces, or exported fields unless a real caller needs them. Smaller APIs are easier to maintain, test, and keep compatible.

## 4. Errors

Return errors for expected failure. Panic only for internal invariants.

### 4.1 Errors Are API

Sentinel errors are stable semantic categories, not implementation details.

```go
var (
    ErrUnknownOpcode = errors.New("unknown opcode")
    ErrStackOverflow = errors.New("stack overflow")
)
```

Keep error values stable when callers can reasonably branch on them.

### 4.2 Wrap Errors with `%w`

Use `%w` whenever returning an error with context.

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

### 4.3 Panic Only for Invariants

Panic is allowed only for violated internal invariants, usually in hot paths. Normal control flow returns errors.

Recover at the execution boundary, not throughout the codebase. Recovery must be local, documented, and tied to a specific boundary.

Use structured error types only when callers need more than a message.

## 5. Build Tags

Keep architecture-specific code isolated behind build tags, with matching stubs and mirrored test tags.

```go
//go:build arm64
```

```go
//go:build !arm64
```

Portable behavior belongs in the default implementation. Architecture files provide only the narrow part that must differ.

When adding an architecture path, update `docs/compatibility.md` and the relevant implementation guide.

## 6. Tests

Tests cover behavior, not private shape, unless the shape is the protected contract. If a change touches interpreter and JIT paths, test both or explain why one is not applicable.

### 6.1 Tests Are Executable Documentation

Tests should show setup, execution, and expectation in one visible flow. Avoid fixture builders, test-only run wrappers, assertion helpers, and hidden setup helpers.

Duplicated setup is acceptable when it keeps the tested API visible.

```go
t.Run("threaded", func(t *testing.T) {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    calls := 0
    i := New(prog, WithTick(1), WithHook(func(i *Interpreter) error {
        calls++
        cancel()
        return nil
    }))
    defer i.Close()

    require.ErrorIs(t, i.Run(ctx), context.Canceled)
    require.Equal(t, 1, calls)
})
```

### 6.2 Test Public Behavior

Top-level tests target public symbols.

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

Do not name top-level tests after private helpers. Test private behavior through the public API that owns the observable behavior.

### 6.3 Match Test Files to Production Files

Use matching names: `buffer.go` -> `buffer_test.go`, `assembler.go` -> `assembler_test.go`, `jit_arm64.go` -> `jit_arm64_test.go`.

Tests for a public symbol belong in the test file matching the file that defines the owning type or constructor.

### 6.4 Keep Nesting Shallow

Aim for at most one `t.Run` level. Do not add wrapper subtests just to group cases.

Use table tests when setup and assertions share one shape. Use explicit subtests when cases need different setup or clearer labels. Do not mix styles at the same nesting level.

### 6.5 Use `require`

Always use `require`, not `assert`.

```go
require.NoError(t, err)
require.ErrorIs(t, err, ErrFoo)
require.Equal(t, want, got)
```

### 6.6 Clean Up Immediately

Defer cleanup right after successful allocation.

```go
b, err := NewBuffer(64)
require.NoError(t, err)
defer b.Free()
```

### 6.7 Keep Fixtures Small

A test should make important bytecode or runtime behavior visible near the assertion. Prefer table tests for repeated behavior and focused tests for subtle control flow.

### 6.8 No Test Helpers by Default

Do not add test helpers for fixtures, programs, contexts, configured objects, assertions, or white-box introspection by default.

Before adding a helper, ask: can this be inlined, can this be a table, or does this belong in production code?

Only add a helper when it is clearly better than visible, local test flow.

### 6.9 Shared Opcode Tests

For `interp.Interpreter.Run`, opcode examples live in the package-level `runTests` table. Each row shows bytecode, required constants/types/locals, and final stack values or expected error.

Behavior that does not fit one row, such as entry-frame `YIELD` resume behavior, should be an explicit subtest after the table loop.

### 6.10 Benchmarks

Benchmarks follow the same public owner and hierarchy as correctness tests. Use `BenchmarkNewFoo` for `NewFoo` and `BenchmarkFoo_Bar` for `Foo.Bar`; put workloads and execution modes below that owner.

Validate each fixture once before timing and validate the final result or checksum after timing. Use deterministic inputs and `b.Loop()`. Keep construction, verification, expected-result computation, reset, cleanup, result checks, and warmup outside the timer unless one is the named operation.

Name lifecycle states explicitly and consistently within their owner hierarchy, such as `Threaded`/`JITWarm` for interpreter API benchmarks and `threaded`/`jit_warm` for VM kernels. Do not label interpreter fallback as JIT throughput. Prove native emission and entry before timing a warm path when the package can observe them.

Keep direct interpreter costs in `interp/interp_test.go`, runtime-neutral multi-opcode kernels in `benchmarks/`, and external comparisons behind the `compare` build tag. Do not create benchmark DSLs, service-domain canonical workloads, golden timings, or aggregate scores.

## 7. Git and PRs

Keep commits focused. A commit should have one reason to exist.

### 7.1 Branch and Commit Types

| Change | Branch | Commit |
|---|---|---|
| bug | `hotfix/<desc>` | `fix` |
| feature | `feature/<desc>` | `feat` |
| performance | `feature/<desc>` | `perf` |
| refactor | - | `refactor` |
| test | - | `test` |
| docs | - | `docs` |

Use lowercase, concise, hyphen-separated names.

### 7.2 Commit Format

Use `<type>(scope): <summary>`.

```text
feat(interp): add trace jit support
fix(asm): correct register allocation
feat!: change bytecode format
```

Rules: imperative mood, at most 72 characters, one logical concern per commit. Breaking changes include `BREAKING CHANGE: ...`.

### 7.3 Performance Changes

Performance claims require benchmark evidence:

```text
before: ...
after:  ...
conclusion: ...
```

### 7.4 Self-Review Checklist

Before opening a PR, check:

- issue is fully resolved; no unrelated changes
- every touched symbol has a reason to exist
- removable symbols were removed, inlined, merged, narrowed, or made private
- the algorithm is the simplest correct option found
- repeated review passes find no safe simplification
- names are short, standard, and consistent
- public surface is minimal
- invariants are preserved
- tests cover behavior
- docs are updated when conventions change

### 7.5 Pull Requests

Follow the existing PR template. Explain what changed, why it changed, how it was tested, and benchmark impact if relevant. PR titles follow commit-summary style.

## 8. Docs

Documentation is part of the codebase. Each topic should have one owner; other docs summarize and link instead of repeating full explanations.

Use the standard shape from `docs/README.md`:

1. title and short purpose
2. `When to Read`
3. `Source of Truth` when relevant
4. main content
5. `Maintenance Notes`
6. `Related Docs`

Keep wording direct and standard. Prefer `minivm`, `threaded interpreter`, `JIT`, `trace`, `opcode`, `value`, `ref`, and `heap` consistently.

Agent instruction files are routing and enforcement surfaces. Keep `AGENTS.md` as the common Claude Code / Codex contract, keep `.claude/CLAUDE.md` as a short Claude overlay that imports `AGENTS.md`, and keep detailed coding rules in their owner docs.

A convention-changing code change is incomplete without the matching documentation update.

| Change | Update |
|---|---|
| style, naming, structure | `docs/coding-patterns.md` |
| architecture, ownership, boundaries | `docs/architecture.md` |
| opcode semantics, stack effects, JIT status | `docs/instruction-set.md` |
| JIT contracts, assembler APIs | `docs/jit-internals.md` |
| workflow / convention rules | `AGENTS.md` and `.claude/CLAUDE.md` |
| invariants, pitfalls | `docs/architecture.md` |

## Agent Rule of Thumb

When unsure, choose the smallest correct change.

Prefer local code over a helper, one clear function over fragments, one short role name over mechanism names, one cohesive type over interfaces, explicit flow over clever indirection, one direct algorithm over coordinated state, and nearby style over a new pattern.

The best design keeps behavior obvious, names few things, and leaves the next reader with less to understand.

## Maintenance Notes

When changing coding patterns, keep rules readable from nearby code, avoid process that prevents no real mistakes, preserve useful historical rules unless they conflict with current code, keep terminology aligned with `docs/`, and update `docs/README.md` if the documentation shape changes.

## Related Docs

- `docs/README.md` - documentation ownership and format
- `docs/architecture.md` - package boundaries and ownership
- `docs/instruction-set.md` - opcode semantics, stack effects, and JIT status
- `docs/jit-internals.md` - JIT contracts and backend expectations
- `docs/guides/add-opcode.md` - opcode implementation workflow
- `docs/guides/add-architecture.md` - architecture backend workflow

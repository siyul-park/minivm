# Coding Patterns

Default coding style for minivm contributors and agents.

Use this before writing or changing code. The goal is safe, consistent changes with minimal unnecessary structure.

## When to Read

Use this document before writing or changing code, especially when local style is unclear or a new pattern is needed.

Match nearby code first. Use these rules to resolve ambiguity, not to override a clear local pattern.

## Source of Truth

| Concern | Source |
|---|---|
| formatting | `gofmt` |
| package-specific style | nearby code in the same package |
| public API shape | existing public APIs and tests |
| documentation shape | `docs/README.md` |
| architectural ownership | `docs/architecture.md` |

## Fast Path

Read only the sections relevant to the change.

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

## 0. Principles

### 0.1 Simpler is Better

If two designs provide the same behavior, choose the simpler one.

Prefer fewer files, fewer types, fewer functions, fewer methods, fewer arguments, fewer names, less indirection, and more local code.

Do not add abstraction only because code can be split. Add abstraction when it clearly improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### 0.2 Keep Public Surfaces Small

Push complexity down. Public APIs should stay simple even when the implementation is difficult.

Prefer simple APIs over exposed mechanisms, explicit behavior over hidden behavior, local complexity over distributed state, and predictable structure over clever abstraction.

A difficult behavior should have one clear implementation, not many partially difficult call sites.

### 0.3 Read Top-Down

Important behavior comes first. Details follow later.

Readers should understand the flow by reading downward:

```text
Run
  parse
  exec
  commit
```

Avoid forcing readers to jump around to reconstruct the main path.

### 0.4 Be Obvious

Prefer mechanically obvious code over clever code.

A slightly longer implementation is better when it avoids hidden state, improves debugging, preserves interpreter/JIT symmetry, makes control flow explicit, or keeps behavior easy to verify.

### 0.5 Preserve Symmetry

Interpreter and JIT paths should stay structurally similar when possible.

Symmetry matters more than small local optimizations because it keeps behavior easier to compare, test, and maintain.

### 0.6 Keep Related Code Close

Keep state, validation, mutation, and cleanup for one behavior near each other.

Avoid splitting logic across files or helpers unless the split has a clear ownership or readability benefit.

### 0.7 Review Every Symbol

Every file, type, interface, struct, field, function, method, parameter, result, constant, and variable should have a reason to exist.

For every touched symbol, ask whether it can be removed, inlined, merged with an existing owner, narrowed in scope, made private, renamed by role, or replaced by direct local code.

Review both new symbols and nearby old symbols exposed by the change. A refactor is incomplete if it adds structure while leaving now-obvious dead fields, arguments, results, helpers, or wrapper files behind.

If the only reason is future flexibility, symmetry without behavior, shorter code, or convenience for one call site, prefer removing the symbol.

### 0.8 Prefer Simpler Algorithms

For each non-trivial change, look for a simpler or more efficient algorithm before committing to structure.

Prefer one direct pass over several coordinated passes, local state over global maps, exact ownership over cleanup protocols, and data flow that matches the runtime model.

Do not optimize by hiding behavior. Algorithmic improvements must keep correctness, interpreter/JIT parity, and verifier/runtime invariants obvious.

Performance-oriented algorithm changes need evidence. Use benchmarks when claiming speed, allocation, or complexity improvements.

### 0.9 Iterate Until Stable

Review simplification in loops. Removing one symbol, helper, field, pass, or branch often exposes the next simplification.

A review pass checks, in order:

1. removable symbols
2. narrower ownership or visibility
3. simpler control flow
4. simpler or more efficient algorithms
5. tests and docs matching the final shape

Stop only when another pass finds no safe symbol removal, no clearer ownership, no simpler control flow, and no algorithmic improvement with evidence.

Record intentionally non-viable simplifications in the final summary so the same path is not re-derived silently later.

## 1. Functions

### 1.1 Use One Abstraction Level

Each function should operate at one conceptual level.

Do not mix orchestration with parsing details, policy with arithmetic, or high-level flow with byte/index mutation.

Good functions read like a short behavior description:

```go
func (r *REPL) Run(ctx context.Context) error {
    for {
        line, err := r.read()
        if err != nil {
            return err
        }

        inst, err := r.parse(line)
        if err != nil {
            return err
        }

        if err := r.exec(ctx, inst); err != nil {
            return err
        }

        r.commit(inst)
    }
}
```

If comments are needed to explain transitions between unrelated steps, the function likely mixes abstraction levels.

### 1.2 Name by Role

Names describe caller-visible behavior, not implementation mechanics.

Use the shortest standard name that is still clear. Prefer one word when package, receiver, or surrounding context already provides meaning.

| Avoid | Prefer |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `appendInstrAndUpdateLen` | `commit` |
| `checkEmptyAndFormatProg` | `show` |
| `jitContext` | `lowering` |
| `jitFrame` | `activation` |
| `jitOperand` | `value` |
| `traceOperation` | `step` |

Receiver context counts. For example, `r.show()` and `r.commit()` are clear on `*REPL`.

Avoid private names that repeat the package, file, or subsystem; non-standard abbreviations; one-letter names outside tight local scopes; and implementation-step names when a role name is enough.

Allowed abbreviations are common domain terms such as `ID`, `IP`, `ABI`, `JIT`, `VM`, and `CPU`.

Good role words include `value`, `step`, `state`, `frame`, `module`, `target`, `source`, `lowering`, `activation`, `compiler`, `builder`, `cache`, `trace`, `root`, and `exit`.

Do not force one-word names when clarity suffers. Short and clear is the goal; cryptic is not.

### 1.3 Declare Callers Before Callees

Within a file and section, place high-level functions before the helpers they call. This makes the file read from policy to mechanics.

Functional options may appear immediately before the constructor they configure so read order matches call sites such as `New(prog, WithTick(1))`.

### 1.4 Extract Only When Useful

Inline simple single-use logic.

Extract a helper only when it removes real duplication, isolates real complexity, names a meaningful behavior, or keeps the caller at one abstraction level.

Do not extract tiny helpers that only hide a short switch, predicate, or loop used once.

Do not duplicate a helper's eligibility checks at every call site. If a helper can decide whether it applies and return failure or fallback, let it own that decision.

### 1.5 Methods Show Ownership

A function used by one type belongs on that type, even if the receiver is not used directly.

Package-level functions are for constructors, functions used by multiple types, public general utilities, and helpers used only by other package-level functions.

Callbacks should also be methods when ownership belongs to a type:

```go
func (l Lowerer) cmp(c *Context) bool {
    return l.compare(c, l.sign32)
}
```

Do not extract a tiny single-use method just to satisfy ownership. Inline it instead.

### 1.6 Constructors Are Functions

Constructors are standalone functions, never methods.

Use:

```go
func newCompiler(...) *compiler
func NewOptimizer(...) *Optimizer
```

Do not use:

```go
func (c *compiler) newCompiler(...) *compiler
```

Public concrete types use `NewType`. Private concrete types use `newType`.

### 1.7 Keep Methods with Their Type

A file should contain methods for one main type.

Split a large type across files by concern if needed, but do not place methods for type A in type B's file just for locality.

For JIT code:

- `interp/jit.go` owns JIT-neutral compiler/module/trace orchestration.
- `interp/jit_arm64.go` owns ARM64 lowering.
- `*Interpreter` JIT bridge behavior stays with `*Interpreter`.

## 2. Types

Add a type when it owns data with behavior, names a real domain concept, or prevents repeated error-prone structure.

Do not add a type only to group two values temporarily, hide a simple tuple, or create an abstraction before it is needed.

### 2.1 Define Interfaces Where Consumed

Interfaces belong in the package that consumes the behavior, not the package that implements it.

Interfaces describe what the caller needs, not what the implementation is.

### 2.2 Prefer Private Type, Public Instance

When there is one meaningful implementation, use an unexported concrete type with an exported value.

```go
type i32Type struct{}

var TypeI32 = i32Type{}
```

### 2.3 Keep Interfaces Small

Do not create an interface until there is a consumer that needs it.

Do not add methods for later. Add only the behavior required now.

Assert interface compliance near the related type:

```go
var _ Traceable = (*Struct)(nil)
```

Place assertions after the related types, in the private value section.

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

Within each group, keep top-down flow: callers before callees.

When a package function becomes a method, move it from private functions into method territory. Constructors remain in the constructor section.

### 2.5 Order Struct Fields by Meaning

Struct fields should follow how readers understand the type:

1. lifecycle and policy objects
2. infrastructure
3. program data
4. runtime state
5. mutable counters
6. read-only config
7. sync primitives

Separate layers with a blank line.

```go
type Interpreter struct {
    ctx       context.Context
    tracer    *Tracer
    hook      func(*Interpreter) error
    marshaler Marshaler

    compiler *compiler
    cache    *Cache

    types     []types.Type
    constants []types.Boxed

    frames []frame
    stack  []types.Boxed

    fp int
    sp int

    threshold int64
    cutoff    int
    tick      int
    fuel      int64

    mu sync.Mutex
}
```

Rich behavioral objects go near the top; mutable counters go above read-only config; plain numeric config goes near the bottom; `sync.Mutex` goes last. Struct literals follow field declaration order.

Field names should be short and clear. Prefer one word when possible.

## 3. APIs

Public APIs should make the common path obvious and keep advanced behavior explicit.

Do not expose internal representation unless callers have a stable reason to depend on it.

### 3.1 Constructors

Constructor names use `New<Type>` for public types and `new<Type>` for private types.

```go
func NewOptimizer(level Level) *Optimizer
func newCompiler(...) *compiler
```

Constructors should establish invariants. If a value has no invariants, a struct literal may be clearer.

### 3.2 Parsers

Parser names follow this pattern:

| Function | Meaning |
|---|---|
| `Parse` | parses the package's primary type |
| `Parse<Type>` | parses a secondary type |
| `ParseAll` | parses multiple values, usually from `io.Reader` |

Examples:

```go
func Parse(s string) (Type, error)
func ParseFunction(lines []string) (*Function, error)
func ParseAll(r io.Reader) ([]Instruction, error)
```

### 3.3 Options

Prefer functional options over config structs.

Use direct arguments for required behavior and options for rare configuration.

Apply defaults first, then options.

```go
func New(prog *program.Program, opts ...func(*option)) *Interpreter {
    opt := option{
        stack:     1024,
        heap:      128,
        threshold: 4096,
    }

    for _, fn := range opts {
        fn(&opt)
    }

    ...
}
```

### 3.4 Builders

Builders are mutable. Built values should be treated as immutable.

```go
fn := types.NewFunctionBuilder(&types.FunctionType{}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0)).
    Build()
```

Discard builders after `Build()`.

Keep builders focused. A builder should construct one thing, validate inputs near construction, and avoid becoming a general mutable configuration store.

### 3.5 Avoid Premature API Surface

Do not add public methods, options, interfaces, or exported fields unless a real caller needs them.

A smaller API is easier to maintain, test, and keep compatible.

## 4. Errors

Return errors for expected failure. Panic only for internal invariants that indicate a bug.

### 4.1 Errors Are API

Sentinel errors are stable semantic categories.

```go
var (
    ErrUnknownOpcode = errors.New("unknown opcode")
    ErrStackOverflow = errors.New("stack overflow")
    ErrDivideByZero  = errors.New("divide by zero")
)
```

Do not expose implementation details as sentinel errors. Keep error values stable when callers can reasonably branch on them.

### 4.2 Wrap Errors with `%w`

Use `%w` whenever returning an error with context.

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

### 4.3 Panic Only for Invariants

Panic is allowed only for violated internal invariants, usually in hot paths.

Normal control flow must return errors.

Recover at the execution boundary, not throughout the codebase. Do not recover broadly. Recovery should be local, documented, and tied to a specific boundary.

Use structured error types only when callers need more than a message.

## 5. Build Tags

Keep architecture-specific files isolated behind build tags.

Architecture-specific files must have matching stubs.

```go
//go:build arm64
```

```go
//go:build !arm64
```

Tests must mirror production build tags.

Portable behavior belongs in the default implementation. Architecture-specific files should provide the narrow part that must differ.

When adding a new architecture path, update `docs/compatibility.md` and the relevant implementation guide.

## 6. Tests

Tests should cover behavior, not internal shape, unless the internal shape is the contract being protected.

When a change touches interpreter and JIT paths, test both paths or explain why one path is not applicable.

### 6.1 Tests Are Executable Documentation

Tests should show setup, execution, and expectation in one visible flow.

Avoid hiding important behavior behind helpers.

Prefer:

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

Avoid fixture builders, test-only run wrappers, assertion helpers, and hidden setup helpers.

Duplicated setup is acceptable when it makes the tested API visible.

### 6.2 Test Public Behavior

Top-level tests target public symbols.

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

Do not name top-level tests after private helpers. Test private behavior through the public API that owns the observable behavior.

### 6.3 Match Test Files to Production Files

Use matching names:

```text
buffer.go      -> buffer_test.go
assembler.go   -> assembler_test.go
jit_arm64.go   -> jit_arm64_test.go
```

Tests for a public symbol belong in the test file matching the file that defines the owning type or constructor.

### 6.4 Keep Nesting Shallow

Aim for at most one `t.Run` level.

Do not add wrapper subtests just to group cases.

Use table-driven tests when setup and assertions share one shape. Use explicit subtests when cases need different setup or clearer labels.

Do not mix table-driven and explicit subtest styles at the same nesting level.

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

A test should make the important bytecode or runtime behavior visible near the assertion.

Prefer table tests for repeated behavior and focused tests for subtle control flow.

### 6.8 No Test Helpers by Default

Do not add test helpers for fixtures, programs, contexts, configured objects, assertions, or white-box introspection by default.

Before adding a helper, ask:

1. Can this be inlined?
2. Can this be a table?
3. Does this behavior belong in production code instead?

Only add a helper when it is clearly better than visible, local test flow.

### 6.9 Shared Opcode Tests

For `interp.Interpreter.Run`, opcode examples live in the package-level `runTests` table.

Each row should show bytecode program, constants/types/locals needed, and final stack values or expected error.

`TestInterpreter_Run` iterates this table. Runtime behavior that does not fit one table row, such as entry-frame `YIELD` resume behavior, should be an explicit subtest after the table loop.

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

Examples:

```text
feat(interp): add trace jit support
fix(asm): correct register allocation
```

Rules: imperative mood, at most 72 characters, and one logical concern per commit.

Breaking changes use:

```text
feat!: change bytecode format
```

and include:

```text
BREAKING CHANGE: ...
```

### 7.3 Performance Changes

Performance claims require benchmark evidence.

Include:

```text
before: ...
after:  ...
conclusion: ...
```

### 7.4 Self-Review Checklist

Before opening a PR, check that:

- the issue is fully resolved
- no unrelated changes were made
- each touched symbol still has a clear reason to exist
- removable symbols were removed, inlined, merged, narrowed, or made private
- the algorithm is the simplest correct option found
- repeated review passes no longer find safe simplifications
- code is simpler or clearly justified
- names are short, standard, and consistent
- public surface is minimal
- invariants are preserved
- tests cover behavior
- docs are updated when conventions change

### 7.5 Pull Requests

Follow the existing PR template.

Explain what changed, why it changed, how it was tested, and benchmark impact if relevant.

PR titles should follow the same style as commit summaries.

## 8. Docs

Documentation is part of the codebase.

Documentation should have one owner for each topic. Other documents should summarize and link to that owner instead of repeating the full explanation.

Use the standard document shape from `docs/README.md`:

1. title and short purpose
2. `When to Read`
3. `Source of Truth` when relevant
4. main content
5. `Maintenance Notes`
6. `Related Docs`

Keep wording direct and standard. Prefer `minivm`, `threaded interpreter`, `JIT`, `trace`, `opcode`, `value`, `ref`, and `heap` consistently.

A convention-changing code change is incomplete without the matching documentation update.

| Change | Update |
|---|---|
| style, naming, structure | `docs/coding-patterns.md` |
| architecture, ownership, boundaries | `docs/architecture.md` |
| opcode semantics, stack effects, JIT status | `docs/instruction-set.md` |
| JIT contracts, assembler APIs | `docs/jit-internals.md` |
| invariants, pitfalls | `AGENTS.md` or `docs/architecture.md` |

## Agent Rule of Thumb

When unsure, choose the smallest correct change.

Prefer:

1. local code over a new helper
2. one clear function over many small fragments
3. one short standard name over a long mechanism name
4. one cohesive type over extra interfaces
5. one explicit flow over clever indirection
6. one direct algorithm over coordinated state
7. existing nearby style over a new pattern

The best design is usually the one that keeps behavior obvious, names few things, and leaves the next reader with less to understand.

## Maintenance Notes

When changing coding patterns:

- prefer rules that can be checked by reading nearby code
- avoid adding process that does not prevent real mistakes
- keep this document shorter than the code it governs
- update `docs/README.md` if the documentation shape changes
- keep terminology aligned with the rest of `docs/`
- preserve useful historical rules unless they conflict with current code

## Related Docs

- `docs/README.md` - documentation ownership and format
- `docs/architecture.md` - package boundaries and ownership
- `docs/instruction-set.md` - opcode semantics, stack effects, and JIT status
- `docs/jit-internals.md` - JIT contracts and backend expectations
- `docs/guides/add-opcode.md` - opcode implementation workflow
- `docs/guides/add-architecture.md` - architecture backend workflow

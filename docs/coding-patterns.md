# Coding Patterns

Read this before writing or changing code.

This document defines the default style for minivm code. It is optimized for agents and contributors who need to make changes safely, consistently, and with minimal unnecessary structure.

## Fast Path

Read only the sections relevant to the change.

| Change                                    | Read                        |
| ----------------------------------------- | --------------------------- |
| Function shape, helper extraction, naming | 0. Principles, 1. Functions |
| Types, interfaces, fields, constructors   | 2. Types                    |
| Public APIs, options, builders, parsers   | 3. APIs                     |
| Errors, panic, recover                    | 4. Errors                   |
| Architecture-specific files               | 5. Build Tags               |
| Tests                                     | 6. Tests                    |
| Commits and PRs                           | 7. Git and PRs              |
| Documentation changes                     | 8. Docs                     |

Default rule: **match nearby code first**. Use this document when local style is unclear or a new pattern is needed.

## 0. Principles

### 0.1 Simpler is better

If two designs provide the same behavior, choose the simpler one.

Prefer:

* fewer files
* fewer types
* fewer functions
* fewer methods
* fewer arguments
* fewer names
* less indirection
* more local code

Do not add abstraction just because code can be split. Add abstraction only when it clearly improves readability, removes real duplication, isolates real complexity, or names a meaningful domain step.

### 0.2 Keep public surfaces small

Push complexity down. Public APIs should stay simple even when the implementation is hard.

Prefer:

* simple APIs over exposed mechanisms
* explicit behavior over hidden magic
* local complexity over distributed state
* predictable structure over clever abstraction

A difficult behavior should have one clear implementation, not many partially difficult call sites.

### 0.3 Read top-down

Important behavior comes first. Details follow later.

Readers should understand the flow by reading downward:

```text
Run
  parse
  exec
  commit
```

Avoid forcing readers to jump around to reconstruct the main path.

### 0.4 Be obvious

Prefer mechanically obvious code over clever code.

A slightly longer implementation is better when it:

* avoids hidden state
* improves debugging
* preserves interpreter/JIT symmetry
* makes control flow explicit
* keeps behavior easy to verify

### 0.5 Preserve symmetry

Interpreter and JIT paths should stay structurally similar when possible.

Symmetry matters more than small local optimizations because it keeps behavior easier to compare, test, and maintain.

### 0.6 Keep related code close

Keep state, validation, mutation, and cleanup for one behavior near each other.

Avoid splitting logic across files or helpers unless the split has a clear ownership or readability benefit.

## 1. Functions

### 1.1 Use one abstraction level

Each function should operate at one conceptual level.

Do not mix orchestration with parsing details, policy with arithmetic, or high-level flow with byte/index manipulation.

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

### 1.2 Name by role

Names describe caller-visible behavior, not implementation mechanics.

Use the shortest standard name that is still clear. Prefer one word when package, receiver, or surrounding context already provides meaning.

Examples:

| Avoid                     | Prefer       |
| ------------------------- | ------------ |
| `rewriteBranchAbsolute`   | `normalize`  |
| `appendInstrAndUpdateLen` | `commit`     |
| `checkEmptyAndFormatProg` | `show`       |
| `jitContext`              | `lowering`   |
| `jitFrame`                | `activation` |
| `jitOperand`              | `value`      |
| `traceOperation`          | `step`       |

Receiver context counts. For example, `r.show()` and `r.commit()` are clear on `*REPL`.

Avoid:

* private names that repeat the package, file, or subsystem
* non-standard abbreviations
* one-letter names outside tight local scopes
* implementation-step names when a role name is enough

Allowed abbreviations are common domain terms such as `ID`, `IP`, `ABI`, `JIT`, `VM`, and `CPU`.

### 1.3 Prefer one-word names

When clear, prefer one short, standard, consistent word.

Good role words:

```text
value
step
state
frame
module
target
source
lowering
activation
compiler
builder
cache
trace
root
exit
```

Do not force one-word names when clarity suffers. Short and clear is the goal; cryptic is not.

### 1.4 Declare callers before callees

Within a file and section, place high-level functions before the helpers they call.

This makes the file read from policy to mechanics.

### 1.5 Extract only when useful

Inline simple single-use logic.

Extract a helper only when it:

* removes real duplication
* isolates real complexity
* names a meaningful behavior
* keeps the caller at one abstraction level

Do not extract tiny helpers that only hide a short switch, predicate, or loop used once.

### 1.6 Let one function own a decision

Do not duplicate a helper’s eligibility checks at every call site.

If a helper can decide whether it applies and return failure or fallback, let it own that decision. Callers should not repeat the helper’s internal preconditions unless doing so changes behavior.

### 1.7 Methods show ownership

A function used by one type belongs on that type, even if the receiver is not used directly.

Package-level functions are for:

* constructors
* functions used by multiple types
* public general utilities
* helpers used only by other package-level functions

Callbacks should also be methods when ownership belongs to a type:

```go
func (l Lowerer) cmp(c *Context) bool {
    return l.compare(c, l.sign32)
}
```

Do not extract a tiny single-use method just to satisfy ownership. Inline it instead.

### 1.8 Constructors are functions

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

### 1.9 Keep methods with their type

A file should contain methods for one main type.

Split a large type across files by concern if needed, but do not place methods for type A in type B’s file just for locality.

For JIT code:

* `interp/jit.go` owns JIT-neutral compiler/module/trace orchestration.
* `interp/jit_arm64.go` owns ARM64 lowering.
* `*Interpreter` JIT bridge behavior stays with `*Interpreter`.

## 2. Types

### 2.1 Define interfaces where consumed

Interfaces belong in the package that consumes the behavior, not the package that implements it.

Interfaces describe what the caller needs, not what the implementation is.

### 2.2 Prefer private type, public instance

When there is one meaningful implementation, use an unexported concrete type with an exported value.

```go
type i32Type struct{}

var TypeI32 = i32Type{}
```

### 2.3 Keep interfaces small

Do not create an interface until there is a consumer that needs it.

Do not add methods “for later.” Add only the behavior required now.

### 2.4 Assert interface compliance near the type

Use:

```go
var _ Traceable = (*Struct)(nil)
```

Place assertions after the related types, in the private value section.

### 2.5 File layout

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

### 2.6 Order struct fields by meaning

Struct fields should follow how readers understand the type:

1. lifecycle and policy objects
2. infrastructure
3. program data
4. runtime state
5. mutable counters
6. read-only config
7. sync primitives

Separate layers with a blank line.

Example:

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

Rules:

* rich behavioral objects go near the top
* mutable counters go above read-only config
* plain numeric config goes near the bottom
* `sync.Mutex` goes last
* struct literals follow field declaration order

Field names should be short and clear. Prefer one word when possible.

## 3. APIs

### 3.1 Constructors

Constructor names use `New<Type>` for public types and `new<Type>` for private types.

```go
func NewOptimizer(level Level) *Optimizer
func newCompiler(...) *compiler
```

### 3.2 Parsers

Parser names follow this pattern:

| Function      | Meaning                                          |
| ------------- | ------------------------------------------------ |
| `Parse`       | parses the package’s primary type                |
| `Parse<Type>` | parses a secondary type                          |
| `ParseAll`    | parses multiple values, usually from `io.Reader` |

Examples:

```go
func Parse(s string) (Type, error)
func ParseFunction(lines []string) (*Function, error)
func ParseAll(r io.Reader) ([]Instruction, error)
```

### 3.3 Options

Prefer functional options over config structs.

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

### 3.5 Avoid premature API surface

Do not add public methods, options, interfaces, or exported fields unless a real caller needs them.

A smaller API is easier to maintain, test, and keep compatible.

## 4. Errors

### 4.1 Errors are API

Sentinel errors are stable semantic categories.

```go
var (
    ErrUnknownOpcode = errors.New("unknown opcode")
    ErrStackOverflow = errors.New("stack overflow")
    ErrDivideByZero  = errors.New("divide by zero")
)
```

Do not expose implementation details as sentinel errors.

### 4.2 Wrap errors with `%w`

Use `%w` whenever returning an error with context.

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

### 4.3 Panic only for invariants

Panic is allowed only for violated internal invariants, usually in hot paths.

Normal control flow must return errors.

Recover at the execution boundary, not throughout the codebase.

## 5. Build Tags

Architecture-specific files must have matching stubs.

```go
//go:build arm64
```

```go
//go:build !arm64
```

Tests must mirror production build tags.

## 6. Tests

### 6.1 Tests are executable documentation

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

Avoid:

* fixture builders
* test-only run wrappers
* assertion helpers
* hidden setup helpers

Duplicated setup is acceptable when it makes the tested API visible.

### 6.2 Test public behavior

Top-level tests target public symbols.

| Symbol      | Test          |
| ----------- | ------------- |
| `Foo`       | `TestFoo`     |
| `NewFoo`    | `TestNewFoo`  |
| `(Foo).Bar` | `TestFoo_Bar` |

Do not name top-level tests after private helpers. Test private behavior through the public API that owns the observable behavior.

### 6.3 Match test files to production files

Use matching names:

```text
buffer.go      → buffer_test.go
assembler.go   → assembler_test.go
jit_arm64.go   → jit_arm64_test.go
```

Tests for a public symbol belong in the test file matching the file that defines the owning type or constructor.

### 6.4 Keep nesting shallow

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

### 6.6 Clean up immediately

Defer cleanup right after successful allocation.

```go
b, err := NewBuffer(64)
require.NoError(t, err)
defer b.Free()
```

### 6.7 No test helpers by default

Do not add test helpers for fixtures, programs, contexts, configured objects, or assertions.

Before adding a helper, ask:

1. Can this be inlined?
2. Can this be a table?
3. Does this behavior belong in production code instead?

Only add a helper when it is clearly better than visible, local test flow.

### 6.8 Shared opcode tests

For `interp.Interpreter.Run`, opcode examples live in the package-level `runTests` table.

Each row should show:

* bytecode program
* constants/types/locals needed
* final stack values or expected error

`TestInterpreter_Run` iterates this table. Runtime behavior that does not fit one table row, such as entry-frame `YIELD` resume behavior, should be an explicit subtest after the table loop.

## 7. Git and PRs

### 7.1 Branch and commit types

| Change      | Branch           | Commit     |
| ----------- | ---------------- | ---------- |
| Bug         | `hotfix/<desc>`  | `fix`      |
| Feature     | `feature/<desc>` | `feat`     |
| Performance | `feature/<desc>` | `perf`     |
| Refactor    | —                | `refactor` |
| Test        | —                | `test`     |
| Docs        | —                | `docs`     |

Use lowercase, concise, hyphen-separated names.

### 7.2 Commit format

```text
<type>(scope): <summary>
```

Examples:

```text
feat(interp): add trace jit support
fix(asm): correct register allocation
```

Rules:

* imperative mood
* at most 72 characters
* one logical concern per commit

Breaking changes use:

```text
feat!: change bytecode format
```

and include:

```text
BREAKING CHANGE: ...
```

### 7.3 Performance changes

Performance claims require benchmark evidence.

Include:

```text
before: ...
after:  ...
conclusion: ...
```

### 7.4 Self-review checklist

Before opening a PR, check:

* issue is fully resolved
* no unrelated changes
* code is simpler or clearly justified
* names are short, standard, and consistent
* public surface is minimal
* invariants are preserved
* tests cover behavior
* docs are updated when conventions change

### 7.5 Pull requests

Follow the existing PR template.

Explain:

* what changed
* why it changed
* how it was tested
* benchmark impact, if relevant

## 8. Docs

Documentation is part of the codebase.

Update docs in the same commit when code establishes or changes a convention.

| Change                              | Update                           |
| ----------------------------------- | -------------------------------- |
| style, naming, structure            | `docs/coding-patterns.md`        |
| architecture, ownership, boundaries | `docs/architecture.md`           |
| JIT contracts, assembler APIs       | `docs/jit-internals.md`          |
| invariants, pitfalls                | `AGENTS.md` or `architecture.md` |

A convention-changing code change is incomplete without the matching documentation update.

## Agent Rule of Thumb

When unsure, choose the smallest correct change.

Prefer:

1. local code over a new helper
2. one clear function over many small fragments
3. one short standard name over a long mechanism name
4. one cohesive type over extra interfaces
5. one explicit flow over clever indirection
6. existing nearby style over a new pattern

The best design is usually the one that keeps behavior obvious, names few things, and leaves the next reader with less to understand.

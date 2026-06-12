# Coding Patterns

> Read before writing new code.

## Agent Fast Path

Style authority. Read only task-relevant sections.

| Change | Read |
|---|---|
| Function shape/helper extraction | 0. Core Principles, 1. Function Design |
| Public APIs, constructors, builders, parsers | 2. Type & Interface Design, 3. API Design |
| Errors or panic/recover | 4. Error Design |
| Architecture-specific files | 5. Build Tags |
| Tests | 6. Testing |
| Commits/PR text | 7. Git & PR Workflow |
| Docs | 8. Documentation Maintenance |

Default: match nearby code first; use this doc when local style unclear or new pattern needed.

Priorities:

- readability over cleverness
- explicit behavior over hidden magic
- stable structure over local optimization
- top-down flow over fragmented logic
- intention-revealing APIs over implementation exposure

Code reads like a behavior specification. Readers quickly understand what system does, where complexity lives, why structure exists — without simulating low-level mechanics.

## 0. Core Principles

### 0.1 Complexity placement

Push complexity downward. Public APIs stay simple even when implementation hard.

Prefer:

- complex implementation behind simple interfaces
- explicit control flow over implicit magic
- localized complexity over distributed state
- predictable structure over clever abstraction

Avoid spreading complexity across layers. Difficult operation → one difficult implementation, not many partially difficult call sites.

### 0.2 Top-down readability

Put important logic first. Reading downward reveals detail progressively: policy above, mechanics below. Readers rarely jump backward.

### 0.3 Avoid cleverness

Prefer short, standard names, but do not make code cryptic. Prefer mechanically obvious code, even if longer, when it reduces hidden state, avoids surprising flow, improves debuggability, or preserves interpreter/JIT symmetry.

### 0.4 Behavioral symmetry

Interpreter and JIT paths stay structurally similar when possible. Symmetry matters more than local optimization.

### 0.5 Minimal surface and locality

Minimize file count, type count, function count, method count, and argument count while keeping behavior explicit. Avoid splitting code merely because it can be split.

Every file, type, function, method, and argument has design and maintenance cost. Prefer inline logic when a helper is used once and does not name a distinct domain operation. Extract only when it removes real duplication, isolates real complexity, or gives a behavior-level name callers need.

Keep logically related code close together: shared state, validation, mutation, and cleanup for one behavior should be easy to see in one local area. Keep unrelated code apart so each file, type, function, and method has one cohesive reason to exist.

## 1. Function Design

### 1.1 Single abstraction level

Every statement sits at same conceptual height. Don't mix parsing with domain logic, policy with arithmetic, orchestration with buffer mutation, or business flow with index manipulation.

Functions read like a narrative. Callers understand behavior without decoding parsing, temporary state, or low-level mechanics. Put mechanics in intent-named helpers.

```go
// ✗ mixed abstraction levels
func (r *REPL) Run(ctx context.Context) error {
    scanner := bufio.NewScanner(r.in)

    for {
        fmt.Fprint(r.out, "> ")
        scanner.Scan()

        line := strings.TrimSpace(scanner.Text())

        if strings.HasPrefix(fields[1], "@") {
            abs, _ := strconv.ParseInt(fields[1][1:], 0, 64)
            rel := int(abs) - (ip + 3)
            line = fmt.Sprintf("br %d", rel)
        }

        instr.Parse(line)
    }
}

// ✓ consistent abstraction level
func (r *REPL) Run(ctx context.Context) error {
    scanner := bufio.NewScanner(r.in)

    for {
        fmt.Fprint(r.out, prompt)

        if !scanner.Scan() {
            return scanner.Err()
        }

        line := strings.TrimSpace(scanner.Text())

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

Comments explaining transitions between statements signal mixed abstraction levels.

### 1.2 Names express intent

Names describe caller-visible outcomes, not internal mechanisms.

Use the shortest standard name that clearly expresses the role. Use at most one word when the receiver, package, or surrounding code already provides context. Do not encode implementation steps in the name.

Public names follow the same rule: keep them concise and role-based, but still clear to package users.

Avoid one-letter abbreviations for types, fields, functions, and methods. Avoid other abbreviations unless they are standard in the domain or broadly recognized (`ID`, `IP`, `ABI`, `JIT`, `VM`, `CPU`). Prefer one short word over private shorthand.

| ✗ Mechanism | ✓ Intent |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `makeAndCopyInstructions` | `build` |
| `nilOutFieldsAndPrint` | `reset` |
| `checkEmptyAndFormatProg` | `show` |
| `appendInstrAndUpdateLen` | `commit` |

Receiver context counts: `r.show()`, `r.commit()` clear on `*REPL`.

### 1.3 Top-down structure

Declare callers above callees. Reading downward follows execution flow.

```text
Run
  command → reset / show / readConst / readType
               readType → block / addType
  exec    → printStack → format
  parse   → normalize → parseInt
```

Readers discover detail progressively, not reconstruct from scattered helpers.

### 1.4 Small responsibilities

Function does one conceptual thing: orchestrate, transform, validate, emit, or normalize.

Split when abstraction level changes, naming hard, comments explain sections, or temporary state lives too long.

Do not split tiny single-use helpers out of nearby logic unless the helper names a meaningful behavior. A short helper that only hides one switch, loop, or predicate usually makes code harder to maintain because readers must chase another symbol.

### 1.5 Methods vs package-level functions

Behavior belongs with the type owning required context. Receiver syntax marks ownership even when the receiver itself is unused.

**Rule**: A function used by only one type is a method on that type, regardless of whether it touches receiver state. Package-level functions are reserved for one of:

- Used by ≥2 types
- Public and general enough to be reused outside this package
- Constructor (see exception)
- Consumed only by other package-level functions — i.e. no struct ever participates

```go
// ✗ package-level helper (used only by Compiler)
func mergeBlockInputs(inputs map[int][]VReg, blocks []*BasicBlock, ...) bool { ... }

// ✓ ownership is explicit, even though receiver is unused
func (*Compiler) merge(inputs map[int][]VReg, blocks []*BasicBlock, ...) bool { ... }
```

**Strategy callbacks pass as method values, not package functions.** If a helper is passed as a `func(...)` argument, it still becomes a method; the call site uses the method value `t.fn`:

```go
// ✓ method + method value at pass site
func (Lowerer) sign32(c *Context, v VReg) VReg { ... }

func (l Lowerer) cmp(c *Context) bool {
    return l.compare(c, l.sign32)   // bound method value
}

// ✗ package function dropped in because the call shape looks like a callback
func sign32(c *Context, v VReg) VReg { ... }
func (l Lowerer) cmp(c *Context) bool { return l.compare(c, sign32) }
```

**Counter-rule (single-use inline)**: Do not extract a tiny helper used by exactly one call site just to satisfy this rule. If the body is ≤~15 straight-line lines (no looping branch logic worth a name), inline it at the one caller. §1.4 "do not split single-use helpers" outranks the method-conversion rule when the only call site is the helper's only justification.

```go
// ✗ method with unused receiver, used only once at construction
func (*caller) validateOptions(opts options) error { /* 20 lines validation */ }
func newCaller(opts options) (*caller, error) {
    c := &caller{...}
    return c, c.validateOptions(opts)
}

// ✓ inline at the single call site
func newCaller(opts options) (*caller, error) {
    if opts.limit < 0 { ... }
    for _, rule := range opts.rules { ... }
    return &caller{limit: opts.limit, rules: opts.rules}, nil
}
```

**Constructor-helper pattern (2-phase init)**: When a constructor needs a value computed by a method on the type, AND the helper is large enough to extract (not subject to the counter-rule), build the struct first with the field zero, call the method, assign, return:

```go
func newRewriter(info RegInfo, insts []Instruction, pins map[int32]PReg) *rewriter {
    r := &rewriter{pool: ..., pins: pins, assigned: ..., widths: ...}
    r.last = r.scanLastUses(insts)   // extracted because the scan loop names a real operation
    return r
}
```

**Exception**: Constructors themselves remain standalone, never methods:

```go
// ✓ standalone constructor
func newCompiler(arch *Arch, p program) *compiler { ... }

// ✗ "new" + receiver is contradictory
func (a *Assembler) newCompiler(arch *Arch, p program) *compiler { ... }
```

Match type visibility and constructor visibility unless construction is intentionally restricted. Public concrete type with a normal constructor uses `NewType` in the same file as the type. Private type uses `newType`.

**Method expressions for callbacks**: When a callback's body is exactly one method call on its argument, pass the method expression rather than a wrapper:

```go
// ✓ method expression
b.grow(end, memory.executable)   // func(memory) error

// ✗ wrapper that adds no value
func sealRegion(m memory) error { return m.executable() }
b.grow(end, sealRegion)
```

**A file's methods belong to one type.** Split a large type across several files by concern, but every one of those files still holds that one type's methods — never another type's. `*Interpreter` is spread over `interp.go`, `threaded.go`, `fuse.go`, `host.go`, `marshal.go`, `pool.go`; each holds only `*Interpreter` methods. Do not move a type's methods into a *different* type's file to chase "locality": code that operates on type A's state is a method on A, declared in A's file, even when it integrates with subsystem B. If the behavior truly operates on B's state instead, make it a method on a B type — passing the other type in as an argument is the smell that you picked the wrong owner (see §0.5 cross-boundary structs).

For JIT this means: `interp/jit.go` owns the `jitCompiler` / `jitContext` pipeline (arch-neutral block planning, segment selection, linking) and their methods; `interp/jit_arm64.go` owns the `lowerer` (`arm64JIT`) — opcode handlers and ISA-specific helpers. The `Interpreter`'s own JIT bridge (`jit`, `install`, `entry`, `segment`, `deopt`, `scratch`, `hot`, `function`) is `*Interpreter` behavior, so it stays in `interp.go` with the rest of the type — not in `jit.go`.

## 2. Type & Interface Design

### 2.1 Interface-first

Define interfaces in consuming package, not implementing package.

```go
// asm/caller.go
type Caller interface {
    Call(params []Value, scratch *[]uint64) ([]Value, error)
}
```

Interfaces represent required behavior, not implementation ownership.

### 2.2 Private type, public instance

One meaningful implementation → unexported concrete type with one exported instance.

```go
type i32Type struct{}

var TypeI32 = i32Type{}

func (i32Type) Kind() Kind             { return KindI32 }
func (i32Type) String() string         { return "i32" }
func (i32Type) Cast(other Type) bool   { return other == TypeI32 }
func (i32Type) Equals(other Type) bool { return other == TypeI32 }
```

### 2.3 Interface compliance assertions

`var _ Foo = (*Bar)(nil)`. Declare in slot 6 per §2.4 (private values), after the types they assert. Group several with one `var (...)` block when convenient.

```go
var _ Traceable = (*Struct)(nil)
var _ Type      = (*StructType)(nil)
```

### 2.4 File layout

Every `.go` file declares symbols in this fixed order:

1. public type (interface, struct, alias)
2. private type
3. public const
4. private const
5. public value (`var`)
6. private value (`var`) — includes interface compliance assertions
7. public function
8. public method
9. private method
10. private function

Constructors (`NewFoo`) are public functions (slot 7). Within each slot, follow §1.3 (callers above callees) and §4.1 (group sentinel errors in a single `var (...)` block).

### 2.5 Struct field ordering

Struct layout mirrors human reasoning: what the type IS and DOES before how it works internally.

| Level | Characteristic | Examples |
| --- | --- | --- |
| lifecycle / policy | rich behavioral objects; caller-supplied | `context.Context`, profile collectors, hooks, marshalers |
| infrastructure | stable complex objects; set at construction | assemblers, buffers, allocators, JIT compiler |
| program data | loaded at init; stable during execution | bytecode, constants, type tables |
| runtime state | mutated during execution | frames, stacks, heaps, caches |
| mutable counters | small integers written every step | `fp`, `sp` |
| read-only config | plain integers set at construction; never written back during execution | thresholds, cutoffs, tick intervals, fuel budgets |
| sync primitives | raw concurrency mechanics | `sync.Mutex`, `sync.RWMutex` |

Key rules:

- **Behavioral objects vs plain integers**: Rich objects (interfaces, callbacks, object pointers) are lifecycle/policy even when set once at construction. Plain numeric parameters (`int`, `int64`) are read-only config, not lifecycle/policy — they go near the bottom.
- **Mutable counters above read-only config**: `fp`/`sp` are written every operation, so they sit above threshold/cutoff/tick/fuel which are set once and only read.
- **`sync.Mutex` always last**: It is a concurrency mechanic, not a semantic field.
- **Separate layers with a blank line.**

```go
// ✗ plain ints mixed into policy, mutable and read-only counters merged
type Interpreter struct {
    ctx       context.Context
    threshold int64
    cutoff    int
    compiler  *jitCompiler
    frames    []frame
    fp        int
    tick      int
}

// ✓ layered correctly
type Interpreter struct {
    ctx       context.Context         // lifecycle: behavioral objects
    prof      *prof.Stats
    hook      func(*Interpreter) error
    marshaler Marshaler

    compiler *jitCompiler             // infrastructure

    types     []types.Type            // program data
    constants []types.Boxed

    frames []frame                    // runtime state
    stack  []types.Boxed

    fp int                            // mutable counters (written every step)
    sp int

    threshold int64                   // read-only config (plain ints, set once)
    cutoff    int
    tick      int
    fuel      int64
}

// ✓ sync.Mutex at absolute bottom when a type owns synchronization:
// put data fields first, read-only config next, and the mutex last.
```

Struct literals preserve declaration order. Zero-value fields may omit, but remaining stay ordered.

Field names stay as short as clarity allows. Prefer one short word. Avoid one-letter field names unless the domain convention is already that short. Exported API fields follow the same rule and keep enough meaning for package users.

## 3. API Design

### 3.1 Constructor naming

Constructors use `New<Type>`.

```go
func NewOptimizer(level Level) *Optimizer
func NewBasicBlocksPass() pass.Pass[[]*BasicBlock]
func NewCaller(chunk *Chunk) (Caller, error)
```

### 3.2 Parser naming

`Parse` handles primary package type.

```go
func Parse(s string) (Type, error)
func ParseFunction(lines []string) (*Function, error)
func ParseAll(r io.Reader) ([]Instruction, error)
```

Rules:

- `Parse` → primary package type
- `Parse<Type>` → secondary parse target
- `ParseAll` → batch parse from `io.Reader`

### 3.3 Functional options

Prefer functional options over config structs.

```go
type option struct {
    stack     int
    heap      int
    threshold int
}

func WithStack(val int) func(*option) {
    return func(o *option) {
        o.stack = val
    }
}

func New(prog *program.Program, opts ...func(*option)) *Interpreter {
    opt := option{
        stack:     1024,
        heap:      128,
        threshold: 4096,
    }

    for _, o := range opts {
        o(&opt)
    }

    ...
}
```

Apply defaults before options.

### 3.4 Builder pattern

Builders are mutable; built results are immutable.

```go
fn := types.NewFunctionBuilder(&types.FunctionType{}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0)).
    Build()
```

Discard builders after `Build()`.

## 4. Error Design

### 4.1 Errors are API surface

Errors are package behavior. Sentinel errors: stable semantic categories, not implementation details.

```go
var (
    ErrUnknownOpcode     = errors.New("unknown opcode")
    ErrSegmentationFault = errors.New("segmentation fault")
    ErrStackOverflow     = errors.New("stack overflow")
    ErrDivideByZero      = errors.New("divide by zero")
)
```

### 4.2 Always wrap with `%w`

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return nil, fmt.Errorf("%w: %w", ErrMmapFailed, err)
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

### 4.3 Panic strategy

Panic only for violated internal invariants in hot paths. Normal control flow stays explicit. Recover once at execution boundary.

## 5. Build Tags

Architecture-specific files require matching stubs.

```go
//go:build arm64
```

```go
//go:build !arm64
```

## 6. Testing

### 6.1 Tests are executable documentation

Tests show setup, execution, and expectation in one visible flow — no helper indirection.

**No test helpers that wrap test logic.** Inline everything inside each `t.Run` body. When a test uses an abstraction, readers must chase the helper to understand what the API does and what the test asserts. Tests serve two purposes: verification and documentation of API usage. Both require full call sequence visible at the point of the test.

```go
// WRONG: hides what the API does
runCancel := func(t *testing.T, prog *program.Program, opts ...func(*option)) {
    t.Helper()
    ctx, cancel := context.WithCancel(context.Background())
    // ...
}
t.Run("threaded", func(t *testing.T) { runCancel(t, prog, WithTick(1)) })

// CORRECT: full call visible, no indirection
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

### 6.2 File naming

```text
buffer.go      → buffer_test.go
assembler.go   → assembler_test.go
jit_arm64.go   → jit_arm64_test.go
```

Default to matching production files with corresponding `_test.go` files.
Tests for a public symbol live in the `_test.go` file matching the production
file that defines that symbol's owning type or constructor. `Cache.Close` belongs
in `cache_test.go`, not in a neighboring pool or interpreter test file.

When implementation file only supports another type's public API, put tests in owner type's test file. E.g., interpreter option functions and `Interpreter` methods belong in `interp_test.go`, even if implemented in smaller supporting file.

### 6.3 One test per public symbol

Test targets are public API only. Do not create top-level tests named after private helpers or implementation details. Exercise private behavior through the public function or method that owns the observable behavior.

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

All cases for a symbol belong in one test function.

### 6.4 Test structure

**Minimize nesting depth.** Each `t.Run` level adds reading, filtering, debugging overhead. Aim for at most one subtest level. Wrapper subtests that only group cases add depth without value — hoist directly.

At each nesting level, use **one** pattern — table-driven or explicit `t.Run`. Don't mix both.

**Table-driven** — when all cases share identical setup and assertion shape:

```go
func TestBoxed_Kind(t *testing.T) {
    tests := []struct {
        val  Boxed
        kind Kind
    }{
        {BoxI32(0), KindI32},
        {BoxI64(0), KindI64},
    }

    for _, tt := range tests {
        t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
            require.Equal(t, tt.kind, tt.val.Kind())
        })
    }
}
```

No `name string` field when subtest name derives from primary input. Use `fmt.Sprint(tt.input)` instead. `name` field allowed only when inputs don't produce readable name (e.g. program bytecode, complex configs).

**Explicit subtests** — when cases differ in setup, have side effects, or benefit from a descriptive label:

```go
func TestBuffer_Append(t *testing.T) {
    t.Run("normal", func(t *testing.T) { ... })
    t.Run("overflow", func(t *testing.T) { ... })
}
```

**No grouping wrappers** unless wrapper does meaningful shared setup that can't be inlined. Wrapper that only groups adds depth without gain — hoist to parent level.

Shared setup across few cases: inline it at each case. Do not extract a
test-only data builder such as `smallProgram()` or `pooledArithmeticProgram()`;
the duplicated setup is documentation of the API under test.

```go
// WRONG: extra level just for grouping
t.Run("with auth", func(t *testing.T) {
    t.Run("valid token", func(t *testing.T) { ... })
    t.Run("expired token", func(t *testing.T) { ... })
})

// CORRECT: hoisted, setup visible in each case
t.Run("valid token", func(t *testing.T) { ctx := context.WithValue(...); ... })
t.Run("expired token", func(t *testing.T) { ctx := context.WithValue(...); ... })
```

### 6.5 Assertions

Always use `require`; never use `assert`.

```go
require.NoError(t, err)
require.ErrorIs(t, err, ErrFoo)
```

### 6.6 Resource cleanup

Clean up immediately after allocation.

```go
b, err := NewBuffer(64)
require.NoError(t, err)

defer b.Free()
```

### 6.7 Architecture-specific tests

Tests must mirror production build tags.

```go
//go:build arm64

package arm64
```

### 6.8 Test helper policy

No test helpers. Tests stay self-contained; don't hide setup, assertions, or execution flow.
Test-only helpers that return fixtures, programs, contexts, or configured
objects are not allowed. Inline setup in the `t.Run` body or use a table when
all cases share one assertion shape.

## 7. Git & PR Workflow

### 7.1 Branch & commit types

| Issue | Branch | Commit |
|---|---|---|
| Bug | `hotfix/<desc>` | `fix` |
| Feature | `feature/<desc>` | `feat` |
| Performance | `feature/<desc>` | `perf` |
| Refactor | — | `refactor` |
| Test | — | `test` |
| Docs | — | `docs` |

Use lowercase, concise, hyphen-separated names.

### 7.2 Commit format

```text
<type>(scope): <summary>
```

```text
feat(interp): add trace jit support
fix(asm): correct register allocation bug
```

Rules:

- imperative mood
- ≤ 72 characters
- one logical concern per commit

Breaking changes:

```text
feat!: ...
```

with:

```text
BREAKING CHANGE: ...
```

### 7.3 Performance changes

Performance claims require benchmarks.

```text
before: ...
after:  ...
conclusion: ...
```

### 7.4 Self-review checklist

Before opening a PR:

- [ ] issue fully resolved
- [ ] no unrelated changes
- [ ] code remains readable
- [ ] invariants preserved
- [ ] tests sufficient
- [ ] documentation updated if conventions changed

### 7.5 Pull requests

Follow existing PR template, explain changes clearly, include benchmark results when relevant.

## 8. Documentation Maintenance

Documentation is part of codebase. New code conventions require same-commit doc updates.

| Change | Update |
|---|---|
| style / naming / structure | `docs/coding-patterns.md` |
| architecture / boundaries | `docs/architecture.md` |
| JIT contracts / assembler APIs | `docs/jit-internals.md` |
| invariants / pitfalls | `AGENTS.md` or `architecture.md` |

Code changes establishing new convention are incomplete without matching doc update.

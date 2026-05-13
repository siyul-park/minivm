# Coding Patterns

> Read before writing new code.

## Agent Fast Path

Use this as style authority, but read only task-relevant sections.

| Change | Read |
|---|---|
| Function shape/helper extraction | 0. Core Principles, 1. Function Design |
| Public APIs, constructors, builders, parsers | 2. Type & Interface Design, 3. API Design |
| Errors or panic/recover | 4. Error Design |
| Architecture-specific files | 5. Build Tags |
| Tests | 6. Testing |
| Commits/PR text | 7. Git & PR Workflow |
| Docs | 8. Documentation Maintenance |

Default: match nearby code first; use this document when local style is unclear or a new pattern is needed.

Priorities:

- readability over cleverness
- explicit behavior over hidden magic
- stable structure over local optimization
- top-down flow over fragmented logic
- intention-revealing APIs over implementation exposure

Code should read like a behavior specification. Readers should quickly understand what the system does, where complexity lives, and why the structure exists without simulating low-level mechanics.

## 0. Core Principles

### 0.1 Complexity placement

Push complexity downward. Public APIs stay simple even when implementation is hard.

Prefer:

- complex implementation behind simple interfaces
- explicit control flow over implicit magic
- localized complexity over distributed state
- predictable structure over clever abstraction

Avoid spreading complexity across layers. A difficult operation should have one difficult implementation, not many partially difficult call sites.

### 0.2 Top-down readability

Put important logic first. Reading downward should reveal detail progressively: policy above, mechanics below. Readers should rarely jump backward.

### 0.3 Avoid cleverness

Do not optimize for brevity. Prefer mechanically obvious code, even if longer, when it reduces hidden state, avoids surprising flow, improves debuggability, or preserves interpreter/JIT symmetry.

### 0.4 Behavioral symmetry

Interpreter and JIT paths should stay structurally similar when possible. Symmetry matters more than local optimization.

## 1. Function Design

### 1.1 Single abstraction level

Every statement in a function should sit at the same conceptual height. Do not mix parsing with domain logic, policy with arithmetic, orchestration with buffer mutation, or business flow with index manipulation.

Functions should read like a narrative. Callers should understand behavior without decoding parsing, temporary state, or low-level mechanics. Put mechanics in intent-named helpers.

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

If comments are needed to explain transitions between statements, abstraction levels are probably mixed.

### 1.2 Names express intent

Names should describe caller-visible outcomes, not internal mechanisms.

| ✗ Mechanism | ✓ Intent |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `makeAndCopyInstructions` | `build` |
| `nilOutFieldsAndPrint` | `reset` |
| `checkEmptyAndFormatProg` | `show` |
| `appendInstrAndUpdateLen` | `commit` |

Receiver context counts: `r.show()` and `r.commit()` are clear on `*REPL`.

### 1.3 Top-down structure

Declare callers above callees. Reading downward should follow execution flow.

```text
Run
  command → reset / show / readConst / readType
               readType → block / addType
  exec    → printStack → format
  parse   → normalize → parseInt
```

Readers should discover detail progressively, not reconstruct behavior from scattered helpers.

### 1.4 Small responsibilities

A function should do one conceptual thing: orchestrate, transform, validate, emit, or normalize.

Split when abstraction level changes, naming becomes hard, comments explain sections, or temporary state lives too long.

### 1.5 Methods vs package-level functions

Behavior belongs with the type that owns required context. Repeatedly passing the same receiver usually means the helper should be a method.

```go
// ✗ package-level helper
func makeBranchClosure(fn Caller, sig *Signature) func(*Interpreter) {
    ...
}

// ✓ ownership is explicit
func (c *jitCompiler) branchClosure(fn Caller, sig *Signature) func(*Interpreter) {
    ...
}
```

## 2. Type & Interface Design

### 2.1 Interface-first

Define interfaces in the consuming package, not the implementing package.

```go
// asm/caller.go
type Caller interface {
    Params() []RegType
    Returns() []RegType
    Call(args []uint64) ([]uint64, error)
}
```

Interfaces represent required behavior, not implementation ownership.

### 2.2 Private type, public instance

When only one meaningful implementation exists, use an unexported concrete type with one exported instance.

```go
type i32Type struct{}

var TypeI32 = i32Type{}

func (i32Type) Kind() Kind             { return KindI32 }
func (i32Type) String() string         { return "i32" }
func (i32Type) Cast(other Type) bool   { return other == TypeI32 }
func (i32Type) Equals(other Type) bool { return other == TypeI32 }
```

### 2.3 Interface compliance assertions

Declare immediately after the type definition.

```go
var _ Traceable = (*Struct)(nil)
var _ Type      = (*StructType)(nil)
```

### 2.4 File layout

Order declarations from policy to mechanism:

1. exported interfaces/types
2. exported errors
3. interface assertions
4. exported constructors
5. exported methods
6. unexported helpers

### 2.5 Struct field ordering

Struct layout should mirror human reasoning:

| Level | Examples |
|---|---|
| lifecycle / policy | `context.Context`, profiles, options |
| infrastructure | assemblers, buffers, allocators |
| program data | bytecode, constants, type tables |
| runtime state | frames, stacks, heaps |
| raw counters | pointers, ticks, indices |

```go
// ✗ mixed ordering
type Interpreter struct {
    ctx    context.Context
    buffer *asm.Buffer
    code   [][]func(*Interpreter)
    prof   *prof.Profile
    frames []frame
    sp     int
}

// ✓ layered ordering
type Interpreter struct {
    ctx    context.Context
    prof   *prof.Profile

    buffer *asm.Buffer

    code   [][]func(*Interpreter)

    frames []frame

    sp     int
}
```

Struct literals must preserve declaration order. Zero-value fields may be omitted, but remaining fields must stay ordered.

## 3. API Design

### 3.1 Constructor naming

Constructors use `New<Type>`.

```go
func NewOptimizer(level Level) *Optimizer
func NewBasicBlocksPass() pass.Pass[[]*BasicBlock]
func NewCaller(sig *Signature, chunk *Chunk) (Caller, error)
```

### 3.2 Parser naming

`Parse` handles the primary package type.

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

Errors are package behavior. Sentinel errors represent stable semantic categories, not implementation details.

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

Panic only for violated internal invariants in hot paths. Normal control flow must stay explicit. Recover once at the execution boundary.

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

Tests should show setup, execution, and expectation in one visible flow so behavior is clear without helper indirection.

### 6.2 File naming — mandatory 1:1 mapping

```text
buffer.go      → buffer_test.go
assembler.go   → assembler_test.go
jit_arm64.go   → jit_arm64_test.go
```

Every `.go` file must have a matching `_test.go`.

### 6.3 One test per public symbol

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

All cases for a symbol belong in one test function.

### 6.4 Test structure

Prefer table-driven tests.

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

Use explicit subtests when tables reduce readability.

```go
func TestBuffer_Append(t *testing.T) {
    t.Run("normal", func(t *testing.T) { ... })
    t.Run("overflow", func(t *testing.T) { ... })
}
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

Test helpers are not allowed. Tests must stay self-contained and not hide setup, assertions, or execution flow.

Exception only if all are true:

1. logic repeats across multiple files
2. duplication harms readability
3. abstraction models a general use case

Even then, prefer improving the production API.

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

Follow the existing PR template, explain changes clearly, and include benchmark results when relevant.

## 8. Documentation Maintenance

Documentation is part of the codebase. New code conventions require same-commit doc updates.

| Change | Update |
|---|---|
| style / naming / structure | `docs/coding-patterns.md` |
| architecture / boundaries | `docs/architecture.md` |
| JIT contracts / assembler APIs | `docs/jit-internals.md` |
| invariants / pitfalls | `AGENTS.md` or `architecture.md` |

Code changes that establish a new convention are incomplete without the matching documentation update.

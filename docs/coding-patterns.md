# Coding Patterns

Conventions used throughout this codebase. Read before writing any new code.

---

# 1. Package & Type Design

## 1.1 Interface-first

Define interfaces in the consuming package, not the implementing one.

* Callers depend on interfaces
* Implementations remain replaceable
* Dependency direction is explicit

```go
// asm/caller.go — defined where it is used, implemented in asm/arm64/
type Caller interface {
    Params() []RegType
    Returns() []RegType
    Call(args []uint64) ([]uint64, error)
}
```

---

## 1.2 Private type, public instance

When a type has exactly one meaningful implementation:

* Use an unexported struct
* Expose a single exported instance

```go
type i32Type struct{}
var TypeI32 = i32Type{}

func (i32Type) Kind() Kind             { return KindI32 }
func (i32Type) String() string         { return "i32" }
func (i32Type) Cast(other Type) bool   { return other == TypeI32 }
func (i32Type) Equals(other Type) bool { return other == TypeI32 }
```

---

## 1.3 Interface compliance assertions

Declare compliance immediately after type definition.

```go
type Struct struct { ... }

var _ Traceable = (*Struct)(nil)
var _ Type      = (*StructType)(nil)
```

---

## 1.4 File layout

Order declarations by abstraction level:

1. Exported interfaces and types
2. Exported error variables
3. Interface compliance assertions
4. Exported constructors and functions
5. Exported methods
6. Unexported types and helpers

---

# 2. API Design

## 2.1 Constructor naming

All constructors follow `New<Type>`.

```go
func NewOptimizer(level Level) *Optimizer
func NewBasicBlocksPass() pass.Pass[[]*BasicBlock]
func NewCaller(sig *Signature, chunk *Chunk) (Caller, error)
```

Rules:

* Return concrete type or primary interface
* Do not expose raw pointers without context

---

## 2.1b Parser naming

Text-to-value parsers use `Parse` (not `Parse<Type>`).

```go
// types/parse.go
func Parse(s string) (Type, error)          // parses any Type.String() output
func ParseFunction(lines []string) (*Function, error)  // parses Function.String() lines

// instr/parse.go
func Parse(line string) (Instruction, error)
func ParseAll(r io.Reader) ([]Instruction, error)

// program/parse.go
func Parse(r io.Reader) (*Program, error)   // round-trips Program.String()
```

Rules:

* The base name `Parse` parses the primary type of the package (e.g. `types.Parse` → `Type`)
* Use `Parse<Specific>` only when the package has multiple distinct parseable types
  (e.g. `types.ParseFunction` because `types.Parse` already handles the `Type` interface)
* `ParseAll` is the batch variant — reads from `io.Reader`, skips blank lines

---

## 2.2 Functional options

Use functional options for optional configuration.

```go
type option struct {
    stack     int
    heap      int
    threshold int
}

func WithStack(val int) func(*option) {
    return func(o *option) { o.stack = val }
}

func New(prog *program.Program, opts ...func(*option)) *Interpreter {
    opt := option{stack: 1024, heap: 128, threshold: 4096}
    for _, o := range opts { o(&opt) }
    // ...
}
```

Rules:

* Do not use config structs
* Apply defaults before options

---

## 2.3 Builder pattern

Use a builder for incremental configuration.

```go
fn := types.NewFunctionBuilder(&types.FunctionType{}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0)).
    Build()
```

Rules:

* Builder is mutable
* Result is immutable
* Builder is discarded after `Build()`

---

# 3. Error Design

## 3.1 Sentinel errors

Declare errors at package level.

```go
var (
    ErrUnknownOpcode     = errors.New("unknown opcode")
    ErrSegmentationFault = errors.New("segmentation fault")
    ErrStackOverflow     = errors.New("stack overflow")
    ErrDivideByZero      = errors.New("divide by zero")
)
```

---

## 3.2 Error wrapping

Always wrap errors using `%w`.

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return nil, fmt.Errorf("%w: %w", ErrMmapFailed, err)
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

---

## 3.3 Panic strategy

* Use panic in execution hot paths only
* Recover once at the boundary (e.g. `interp.Run`)
* Do not use panic in general logic

---

# 4. Build Tags

## 4.1 Architecture-specific code

```go
//go:build arm64

package interp
```

---

## 4.2 Stub requirement

```go
//go:build !arm64

package arm64
```

Every architecture-specific file must include a corresponding stub.

---

# 5. Testing Patterns

---

## 5.1 File Naming Rules

### Go file ↔ Test file 1:1 mapping (mandatory)

Each implementation file must have a corresponding test file with the exact same prefix.

```
buffer.go        → buffer_test.go
assembler.go     → assembler_test.go
jit_arm64.go     → jit_arm64_test.go
```

### Rules

* `_test.go` must use the exact same prefix as its target `.go` file
* One `_test.go` must not cover multiple `.go` files
* Every `.go` file must have a corresponding `_test.go` file

---

## 5.2 One test function per public symbol

Each public symbol must have exactly one test function.

| Symbol    | Test        |
| --------- | ----------- |
| Foo       | TestFoo     |
| NewFoo    | TestNewFoo  |
| (Foo).Bar | TestFoo_Bar |

### Rules

* Multiple test functions per symbol are not allowed
* All test cases must be handled within a single test function

---

## 5.3 Test case structure

### Preferred: Table-driven tests

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

---

### Fallback: explicit subtests

```go
func TestBuffer_Append(t *testing.T) {
    t.Run("normal", func(t *testing.T) {
        b, err := NewBuffer(64)
        require.NoError(t, err)
        defer b.Free()

        require.NoError(t, b.Append([]byte{1}))
    })

    t.Run("overflow", func(t *testing.T) {
        b, err := NewBuffer(1)
        require.NoError(t, err)
        defer b.Free()

        require.Error(t, b.Append([]byte{1, 2}))
    })
}
```

---

## 5.4 Subtest naming

* Use `fmt.Sprint(input)` for table-driven tests
* Use descriptive strings for explicit subtests

---

## 5.5 Assertions

* Always use `require`
* Do not use `assert`

---

## 5.6 Error coverage

* Success cases must call `require.NoError`
* Failure cases must use `require.Error` or `require.ErrorIs`

---

## 5.7 Resource cleanup

Always clean up immediately after allocation.

```go
b, err := NewBuffer(64)
require.NoError(t, err)
defer b.Free()
```

---

## 5.8 Shared test tables

Allowed only within the same `_test.go` file.

---

## 5.9 Architecture-specific tests

Tests must include matching build tags.

```go
//go:build arm64

package arm64
```

---

# 6. Test Helper Policy

## 6.1 Principle

Test helper functions are not allowed.

Tests must function as self-contained documentation and prioritize:

* explicitness
* readability
* visibility of execution flow

---

## 6.2 Disallowed

* helper functions for specific test cases
* helper functions scoped to a single test file
* any abstraction that hides test logic

---

## 6.3 Guidelines

* prefer explicit code over deduplication
* keep setup, execution, and assertions visible
* allow controlled duplication when it improves readability

---

## 6.4 Exception

A helper may be introduced only if all conditions are met:

1. the same logic is repeated across multiple test files
2. the duplication significantly harms readability
3. the logic represents a general use case

In this case:

* do not introduce a test helper
* improve the production API instead

---

## 6.5 Direction

Do not hide complexity in tests.
Eliminate it through better API design.

---

# 7. Git & PR Workflow

---

## 7.1 Issue Type

Each change must be classified as one of:

* bug
* feature
* performance

---

## 7.2 Branch Naming

```
bug         → hotfix/<short-description>
feature     → feature/<short-description>
performance → feature/<short-description>
```

Rules:

* lowercase only
* hyphen-separated
* concise and descriptive

---

## 7.3 Commit Rules

### Atomic commits (mandatory)

Each commit must:

* represent a single logical unit
* not mix unrelated concerns
* be independently understandable

---

### Commit type mapping

| Change type   | Commit type |
| ------------- | ----------- |
| Bug           | fix         |
| Feature       | feat        |
| Performance   | perf        |
| Refactor      | refactor    |
| Test          | test        |
| Documentation | docs        |

---

### Commit format

```
<type>(scope): <summary>
```

Examples:

```
feat(interp): add trace jit support
fix(asm): correct register allocation bug
```

Rules:

* use imperative mood
* keep summary within 72 characters
* clearly describe intent

---

## 7.4 Breaking Changes

If a change introduces incompatible API or behavior:

* use `feat!` or `fix!`
* include a `BREAKING CHANGE:` section in the commit body

---

## 7.5 Diff Splitting

When preparing commits:

* group changes by responsibility
* separate feature, fix, refactor, test, and performance changes
* split changes within a file if they represent different concerns

---

## 7.6 Performance Changes

If performance-related:

* benchmarks are required
* compare before and after results

Format:

```
before: ...
after: ...
conclusion: ...
```

---

## 7.7 Self Review

Before creating a PR, verify:

* the issue is fully resolved
* no unnecessary changes are included
* code is consistent and readable
* no hidden bugs exist
* no regressions are introduced
* tests are sufficient

If any issue is found, fix it before proceeding.

---

## 7.8 Pull Request Rules

A PR must:

* follow the existing template
* clearly describe the changes
* include benchmark results if applicable

---

## 7.9 Hard Rules

* no unrelated changes
* no large mixed commits
* do not open a PR if self-review fails

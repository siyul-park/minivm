# Coding Patterns

Conventions used throughout this codebase. Read before writing any new code.

## Package & Type Design

### Interface-first

Define interfaces in the consuming package, not the implementing one. Callers depend on interfaces; packages expose interfaces.

```go
// asm/caller.go — defined where it is used, implemented in asm/arm64/
type Caller interface {
    Params() []RegType
    Returns() []RegType
    Call(args []uint64) ([]uint64, error)
}
```

### Private type, public instance

When a type has exactly one meaningful implementation and no fields callers should access, use an unexported struct with an exported singleton variable. This freezes the API surface and prevents external instantiation.

```go
// types/primitive.go
type i32Type struct{}          // unexported — callers cannot construct it
var TypeI32 = i32Type{}        // exported — the one canonical instance

func (i32Type) Kind() Kind          { return KindI32 }
func (i32Type) String() string      { return "i32" }
func (i32Type) Cast(other Type) bool  { return other == TypeI32 }
func (i32Type) Equals(other Type) bool { return other == TypeI32 }
```

### Interface compliance assertions

Declare `var _ Interface = (*Type)(nil)` immediately after the type declaration. Use value form for value types (`var _ Value = Boxed(0)`). Produces a compile-time error if the type drifts out of compliance.

```go
type Struct struct { ... }

var _ Traceable = (*Struct)(nil)
var _ Type      = (*StructType)(nil)
```

### File layout within a package

Order declarations top-to-bottom by abstraction level:
1. Exported interfaces and types
2. Exported error variables
3. Interface compliance assertions (`var _ ...`)
4. Exported constructors and functions
5. Exported methods (grouped by receiver type)
6. Unexported types, variables, and helpers

---

## API Design

### Constructor naming

All constructors are `New<Type>`. They return either the concrete type or its primary interface — never a raw pointer without a matching interface.

```go
func NewOptimizer(level Level) *Optimizer                           // concrete
func NewBasicBlocksPass() pass.Pass[[]*BasicBlock]                  // interface
func NewCaller(sig *Signature, chunk *Chunk) (Caller, error)        // interface + error
```

### Functional options

Use an unexported `option` struct with `With*` functions for optional configuration. Apply defaults in the constructor before options. Never use a config struct parameter.

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

### Builder pattern

Use a builder for objects requiring multiple optional configuration steps. The builder accumulates state; `Build()` validates and produces the final immutable value. The builder is discarded after `Build()`.

```go
fn := types.NewFunctionBuilder(&types.FunctionType{...}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0), ...).
    Build()
```

---

## Error Design

### Package-level sentinel errors

Declare all errors a package can return as package-level `var Err* = errors.New(...)`. Group them together near the top of the relevant file.

```go
var (
    ErrUnknownOpcode       = errors.New("unknown opcode")
    ErrSegmentationFault   = errors.New("segmentation fault")
    ErrStackOverflow       = errors.New("stack overflow")
    ErrDivideByZero        = errors.New("divide by zero")
)
```

### Error wrapping

Wrap sentinel errors with context using `fmt.Errorf`. Always use `%w` for the base error to enable `errors.Is`.

```go
// wrap with value context
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))

// wrap with underlying error (both unwrappable)
return nil, fmt.Errorf("%w: %w", ErrMmapFailed, err)

// wrap with positional context
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

### Panic in hot paths, recover at boundaries

The interpreter's execution loop uses `panic` for runtime errors (stack overflow, divide by zero, unreachable). A single `recover()` in `interp.Run`'s deferred function converts panics to errors via `i.error(r)`, which appends `at=<ip>`. Do not use `panic` outside execution hot paths.

---

## Build Tags for Architecture-Specific Code

Platform-specific files use `//go:build` on the first line (before `package`, separated by a blank line):

```go
// interp/jit_arm64.go
//go:build arm64

package interp
```

Every architecture-specific file must have a complementary stub for other platforms:

```go
// asm/arm64/abi_stub.go
//go:build !arm64

package arm64
```

The stub exists so `go build` succeeds on all platforms. Without it, the linker fails to find exported symbols on non-target architectures.

---

## Testing Patterns

### One test function per public symbol

| Symbol kind | Test function name |
|---|---|
| Package-level function `Foo` | `TestFoo` |
| Constructor `NewFoo` | `TestNewFoo` |
| Method `(Foo).Bar` | `TestFoo_Bar` |

All cases for that symbol live inside a single table, not across multiple test functions.

### Table-driven tests

Define `tests` as an anonymous struct slice immediately inside the test function. Use the minimum fields needed.

```go
func TestBoxed_Kind(t *testing.T) {
    tests := []struct {
        val  Boxed
        kind Kind
    }{
        {val: BoxI32(0), kind: KindI32},
        {val: BoxI64(0), kind: KindI64},
        {val: BoxF32(0), kind: KindF32},
        {val: BoxF64(0), kind: KindF64},
    }
    for _, tt := range tests {
        t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
            require.Equal(t, tt.kind, tt.val.Kind())
        })
    }
}
```

### Subtest naming

Use `fmt.Sprint(tt.<primary_input>)` as the subtest name. This produces unique, self-describing names without manual formatting.

Use a `name string` field in the struct only when the input is not printable or when the test has multiple independent dimensions.

### `require` over `assert`

Always use `github.com/stretchr/testify/require`. It fails fast on the first assertion failure. Never use `testify/assert` in this codebase.

### Error path coverage

Every case that expects success must call `require.NoError(t, err)` before asserting on the result. Every case that expects failure must use `require.Error(t, err)` or `require.ErrorIs(t, err, ErrX)`.

```go
tests := []struct {
    name    string
    sealed  bool
    wantErr bool
}{
    {name: "unsealed", sealed: false, wantErr: false},
    {name: "sealed",   sealed: true,  wantErr: true},
}
for _, tt := range tests {
    t.Run(tt.name, func(t *testing.T) {
        b, err := NewBuffer(64)
        require.NoError(t, err)
        defer b.Free()

        if tt.sealed {
            require.NoError(t, b.Seal())
        }
        _, err = b.Append([]byte{0x01, 0x02, 0x03})
        if tt.wantErr {
            require.Error(t, err)
        } else {
            require.NoError(t, err)
        }
    })
}
```

### Resource cleanup

Always `defer resource.Free()` or `defer resource.Close()` immediately after a successful allocation — before any subsequent logic that could fail.

```go
b, err := NewBuffer(64)
require.NoError(t, err)
defer b.Free()   // ← immediately after, not at the bottom of the function
```

### Shared test tables

When multiple test functions in the same file exercise the same program set (e.g. `interp_test.go`), declare a package-level `var tests = []struct{...}{...}` and range over it in each `Test*` function. This avoids duplication without introducing shared mutable state between tests.

### Architecture-specific tests

Tests for architecture-specific code must carry the same build tag as the implementation:

```go
//go:build arm64

package arm64
```

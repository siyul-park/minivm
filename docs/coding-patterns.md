# Coding Patterns

> Read before writing any new code.

---

## 0. Function Design

### 0.1 Single abstraction level

Every statement in a function must sit at the same conceptual height. Never mix low-level operations (indexing, arithmetic) with high-level ones (method calls, domain logic).

```go
// ✗ mixed levels
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

// ✓ consistent level
func (r *REPL) Run(ctx context.Context) error {
    scanner := bufio.NewScanner(r.in)
    for {
        fmt.Fprint(r.out, prompt)
        if !scanner.Scan() { ... }
        line := strings.TrimSpace(scanner.Text())
        inst, err := r.parse(line)
        if err := r.exec(ctx, inst); err != nil { ... }
        r.commit(inst)
    }
}
```

Details like branch-address normalization belong inside `parse`, not in `Run`.

---

### 0.2 Names hide implementation

Name functions by **what** they achieve, not **how** they do it.

| ✗ Exposes mechanism | ✓ Describes outcome |
|---|---|
| `rewriteBranchAbsolute` | `normalize` |
| `makeAndCopyInstructions` | `build` |
| `nilOutFieldsAndPrint` | `reset` |
| `checkEmptyAndFormatProg` | `show` |
| `appendInstrAndUpdateLen` | `commit` |

The receiver provides the missing noun — `r.show()`, `r.build()` are unambiguous on `*REPL`.

---

### 0.3 Top-down structure

Declare callers above callees. Reading downward should follow the logic without scrolling back up.

```
Run
  command → reset / show / readConst / readType
               readType → block / addType
  exec    → printStack → format
  parse   → normalize → parseInt
```

---

### 0.4 Methods vs. package-level functions

If a function is only used by one receiver type, make it a method.

```go
// ✗ package-level, only used by jitCompiler
func makeBranchClosure(fn Caller, sig *Signature) func(*Interpreter) { ... }

// ✓ method — ownership and context are explicit
func (c *jitCompiler) branchClosure(fn Caller, sig *Signature) func(*Interpreter) { ... }
```

---

## 1. Type & Interface Design

### 1.1 Interface-first

Define interfaces in the consuming package, not the implementing one.

```go
// asm/caller.go — defined where used, implemented in asm/arm64/
type Caller interface {
    Params() []RegType
    Returns() []RegType
    Call(args []uint64) ([]uint64, error)
}
```

---

### 1.2 Private type, public instance

When a type has exactly one meaningful implementation, use an unexported struct with a single exported instance.

```go
type i32Type struct{}
var TypeI32 = i32Type{}

func (i32Type) Kind() Kind             { return KindI32 }
func (i32Type) String() string         { return "i32" }
func (i32Type) Cast(other Type) bool   { return other == TypeI32 }
func (i32Type) Equals(other Type) bool { return other == TypeI32 }
```

---

### 1.3 Interface compliance assertions

Declare immediately after the type definition.

```go
var _ Traceable = (*Struct)(nil)
var _ Type      = (*StructType)(nil)
```

---

### 1.4 File layout

Order declarations by abstraction level:

1. Exported interfaces and types
2. Exported error variables
3. Interface compliance assertions
4. Exported constructors and functions
5. Exported methods
6. Unexported types and helpers

---

### 1.5 Struct field ordering

Order fields from highest to lowest abstraction level within each struct.

| Level | Examples |
|---|---|
| Highest — lifecycle / policy | `context.Context`, `*prof.Profile`, config/option structs |
| High — infrastructure | `*asm.Assembler`, `*asm.Buffer`, JIT or optimizer handles |
| Mid — program data | `[]byte` bytecode, compiled closures, type tables, constant pools |
| Low — runtime state | call frames, value stack, heap slices |
| Lowest — raw counters / pointers | `fp`, `sp`, `tick`, `threshold`, free-list indices |

```go
// ✗ prof buried among low-level fields
type Interpreter struct {
    ctx       context.Context
    buffer    *asm.Buffer
    instrs    [][]byte
    code      [][]func(*Interpreter)
    prof      *prof.Profile        // ← wrong: policy field after raw data
    frames    []frame
    ...
}

// ✓ higher-abstraction fields first
type Interpreter struct {
    ctx       context.Context      // lifecycle
    prof      *prof.Profile        // adaptive policy
    buffer    *asm.Buffer          // JIT infrastructure
    instrs    [][]byte             // program data
    code      [][]func(*Interpreter)
    frames    []frame              // runtime state
    ...
}
```

Apply the same rule to option/config structs: policy fields (e.g. `profile`) precede capacity fields (e.g. `frame`, `stack`, `heap`).

Struct literals must list fields in the same order as the struct declaration. Fields with zero values may be omitted, but the relative order of the remaining fields must be preserved.

---

## 2. API Design

### 2.1 Constructor naming — `New<Type>`

```go
func NewOptimizer(level Level) *Optimizer
func NewBasicBlocksPass() pass.Pass[[]*BasicBlock]
func NewCaller(sig *Signature, chunk *Chunk) (Caller, error)
```

---

### 2.2 Parser naming — `Parse`

```go
// base name parses the primary type of the package
func Parse(s string) (Type, error)
func ParseFunction(lines []string) (*Function, error)  // distinct type → needs qualifier
func ParseAll(r io.Reader) ([]Instruction, error)       // batch variant
```

Rules: `Parse` handles the primary type. Use `Parse<Specific>` only when multiple distinct parseable types exist. `ParseAll` reads from `io.Reader` and skips blank lines.

---

### 2.3 Functional options

```go
type option struct{ stack, heap, threshold int }

func WithStack(val int) func(*option) {
    return func(o *option) { o.stack = val }
}

func New(prog *program.Program, opts ...func(*option)) *Interpreter {
    opt := option{stack: 1024, heap: 128, threshold: 4096} // apply defaults first
    for _, o := range opts { o(&opt) }
}
```

Do not use config structs.

---

### 2.4 Builder pattern

```go
fn := types.NewFunctionBuilder(&types.FunctionType{}).
    WithParams(types.TypeI32).
    WithLocals(types.TypeI32).
    Emit(instr.New(instr.LOCAL_GET, 0)).
    Build()
```

Builder is mutable; result is immutable; builder is discarded after `Build()`.

---

## 3. Error Design

### 3.1 Sentinel errors — package level

```go
var (
    ErrUnknownOpcode     = errors.New("unknown opcode")
    ErrSegmentationFault = errors.New("segmentation fault")
    ErrStackOverflow     = errors.New("stack overflow")
    ErrDivideByZero      = errors.New("divide by zero")
)
```

---

### 3.2 Always wrap with `%w`

```go
return nil, fmt.Errorf("%w: %d", ErrTooManyParams, len(sig.Params))
return nil, fmt.Errorf("%w: %w", ErrMmapFailed, err)
return fmt.Errorf("%w: at=%d", ErrInvalidJump, ip)
```

---

### 3.3 Panic strategy

Use panic only in execution hot paths. Recover once at the boundary (e.g. `interp.Run`). Do not use panic in general logic.

---

## 4. Build Tags

```go
//go:build arm64     // architecture-specific file
//go:build !arm64    // required stub for other platforms
```

Every architecture-specific file must have a corresponding stub.

---

## 5. Testing

### 5.1 File naming — 1:1 mapping (mandatory)

```
buffer.go      → buffer_test.go
assembler.go   → assembler_test.go
jit_arm64.go   → jit_arm64_test.go
```

One `_test.go` must not cover multiple `.go` files. Every `.go` file must have a corresponding `_test.go`.

---

### 5.2 One test function per public symbol

| Symbol | Test |
|---|---|
| `Foo` | `TestFoo` |
| `NewFoo` | `TestNewFoo` |
| `(Foo).Bar` | `TestFoo_Bar` |

All cases for a symbol go inside a single test function.

---

### 5.3 Test structure

Prefer table-driven tests:

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

Fall back to explicit subtests when inputs don't fit a table:

```go
func TestBuffer_Append(t *testing.T) {
    t.Run("normal", func(t *testing.T) { ... })
    t.Run("overflow", func(t *testing.T) { ... })
}
```

Subtest names: `fmt.Sprint(input)` for table-driven; descriptive strings for explicit.

---

### 5.4 Assertions & coverage

- Always use `require`, never `assert`.
- Success cases: `require.NoError`. Failure cases: `require.Error` or `require.ErrorIs`.

---

### 5.5 Resource cleanup

Clean up immediately after allocation.

```go
b, err := NewBuffer(64)
require.NoError(t, err)
defer b.Free()
```

---

### 5.6 Architecture-specific tests

Include matching build tags.

```go
//go:build arm64

package arm64
```

---

## 6. Test Helper Policy

Test helpers are **not allowed**. Tests must be self-contained — setup, execution, and assertions must all be visible.

**Disallowed:** helpers scoped to a single test or file; any abstraction that hides test logic. Shared test tables are allowed only within the same `_test.go` file.

**Exception:** a helper may be introduced only if all three hold:
1. The same logic repeats across multiple test files.
2. The duplication significantly harms readability.
3. The logic represents a general use case.

Even then — improve the production API instead.

---

## 7. Git & PR Workflow

### 7.1 Branch & commit types

| Issue type | Branch prefix | Commit type |
|---|---|---|
| Bug | `hotfix/<desc>` | `fix` |
| Feature | `feature/<desc>` | `feat` |
| Performance | `feature/<desc>` | `perf` |
| Refactor | — | `refactor` |
| Test | — | `test` |
| Docs | — | `docs` |

Branch names: lowercase, hyphen-separated, concise.

---

### 7.2 Commit format

```
<type>(scope): <summary>
```

```
feat(interp): add trace jit support
fix(asm): correct register allocation bug
```

- Imperative mood. Summary ≤ 72 characters.
- Each commit represents a single logical unit. Do not mix unrelated concerns.
- Split changes within a file if they represent different concerns.
- Breaking changes: use `feat!` / `fix!` with a `BREAKING CHANGE:` section in the body.

---

### 7.3 Performance changes

Benchmarks are required. Include before/after results and a conclusion:

```
before: ...
after:  ...
conclusion: ...
```

---

### 7.4 Self-review checklist

Before opening a PR, verify:

- [ ] Issue is fully resolved
- [ ] No unnecessary or unrelated changes
- [ ] Code is consistent and readable
- [ ] No hidden bugs or regressions
- [ ] Tests are sufficient

Do not open a PR if self-review fails.

---

### 7.5 Pull request rules

- Follow the existing PR template.
- Clearly describe the changes.
- Include benchmark results if applicable.

---

## 8. Documentation Maintenance

When code style or quality feedback results in a change to how code is written
in this repository, the relevant document **must be updated in the same commit**:

- Style / naming / structure feedback → update `docs/coding-patterns.md`
- Architecture or package-boundary feedback → update `docs/architecture.md`
- JIT handler contract or assembler API changes → update `docs/jit-internals.md`
- Key invariants or known gaps → update `.claude/CLAUDE.md` (invariants) or `docs/architecture.md` (gaps)

**Code changes that encode a new convention are incomplete without the corresponding doc change.**
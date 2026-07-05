# Guide: Adding a New JIT Architecture

Checklist for adding a native backend for a new CPU architecture.

## When to Read

Use this guide when adding a new JIT backend. For the runtime model, journal layout, and fallback contracts, use `docs/jit-internals.md` as the canonical reference.

## Source of Truth

| Concern | File or package |
|---|---|
| Generic native-code interfaces | `asm/` |
| ARM64 reference backend | `asm/arm64/`, `interp/jit_arm64.go` |
| Architecture-neutral JIT driver | `interp/jit.go` |
| Trace recording | `interp/trace.go` |
| Platform and CGO support | `docs/compatibility.md` |
| JIT contracts | `docs/jit-internals.md` |

Do not change ARM64 behavior unless the shared `asm` contract requires it.

## Step 1 — Create `asm/<arch>/`

Mirror the ARM64 package shape where it applies:

- physical register identifiers
- instruction encoder
- ABI bridge
- callable entry adapter
- optional spill frame support

The callable adapter contract is:

1. `Callable.Call(ctx uintptr) error` receives `&i.journal[0]`.
2. The adapter passes `ctx` in the architecture's first integer argument register.
3. It preserves every callee-saved register used by the backend allocator.
4. It calls the native chunk and returns control to Go.

There is no VM parameter/return ABI. Native traces read and write VM state through the journal.

Return `nil` from `Arch.Frame()` when the backend has no spill-frame support. If spilling is supported, keep the frame implementation as a separate private type instead of adding frame methods to the architecture type.

## Step 2 — Add `interp/jit_<arch>.go`

Add an architecture-specific lowerer in package `interp`, then provide a build-tagged `newCompiler` implementation for the architecture.

```go
type archLowerer struct{}
```

Lowering rules:

- return `false` without mutating published state when an opcode, kind, or observed heap shape is unsupported
- load the context journal into pinned scratch registers before entering the trace body
- materialize live symbolic state on guard exits
- write `journalSP`, `journalNextIP`, and frame records before returning to Go
- handle entry-frame `RETURN` by writing boxed returns and returning with `trapNone`
- stitch inlined callee returns into the caller's symbolic stack
- lower `CALL` only when the target, frame budget, refs, host behavior, stack bounds, and write-barrier constraints are safe

Scratch slot order is shared with the existing ARM64 backend:

| Slot | Purpose |
|---|---|
| `scratchStack` | `&i.stack[0]` |
| `scratchGlobals` | `&i.globals[0]` |
| `scratchBP` | current frame base pointer |
| `scratchSP` | interpreter stack pointer input |
| `scratchCtrl` | `&i.journal[0]` |

## Step 3 — Keep Platform Behavior Explicit

Update `docs/compatibility.md` when backend support changes.

Document:

- supported `GOOS/GOARCH` pairs
- whether CGO is required
- executable-memory support
- instruction-cache synchronization requirements
- build tags and stubs

Normal users should not need manual build tags.

## Step 4 — Implement Opcode Coverage Incrementally

Prioritize small, common, low-risk paths first:

1. stack operations: `NOP`, `DROP`, `DUP`, `SWAP`
2. constants: `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
3. numeric arithmetic and comparison
4. numeric conversions
5. locals and globals
6. branches
7. entry-frame `RETURN`
8. RC-neutral refs such as `REF_NULL`, `REF_IS_NULL`, and `REF_EQ`

More complex paths, such as calls, ref-counted stores, heap reads, loops, and coroutine suspension, should be added only after the basic backend is stable.

## Step 5 — Verify

```bash
go test ./asm/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

A nonzero `vm_jit_emits_total` metric after running a hot arithmetic function confirms end-to-end native emission.

## Related Docs

- `docs/jit-internals.md` — trace ABI, journal layout, guards, calls, loops, and fallback
- `docs/compatibility.md` — platform matrix, CGO, build tags, and executable memory
- `docs/instruction-set.md` — opcode-level JIT status

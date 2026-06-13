# Guide: Adding a New JIT Architecture

Checklist for adding a native backend for a new CPU architecture.

## Agent Summary

Adding an architecture is cross-cutting. Keep edits explicit:

- `asm/<arch>/`: physical register IDs, encoder, ABI, trampoline/caller.
- `interp/jit_<arch>.go`: backend constants, `newCompiler`, and recorded-trace opcode handlers.
- docs: this guide plus `jit-internals.md` if backend contracts change.

Do not change ARM64 behavior unless a shared `asm/` contract requires it.

## Before You Start

Read `docs/jit-internals.md`, especially Trace ABI and Frame Journal. Use `asm/arm64/` and `interp/jit_arm64.go` as reference implementations.

## Step 1 - Create `asm/<arch>/`

Mirror `asm/arm64/`: implement `asm.Arch`, `asm.Encoder`, `asm.ABI`, optional `asm.Frame`, and the native callable trampoline.

`asm.ABI` exposes `NewCallable`. The trampoline contract is:

1. `Callable.Call(ctx uintptr) error` receives `&i.journal[0]`.
2. The trampoline passes `ctx` in the architecture's first integer argument register.
3. It preserves any callee-saved registers the backend allocator may use.
4. It calls the native chunk.

There is no param/return ABI, no `asm.Value`, and no header packing.

Return nil from `Arch.Frame()` when the backend has no spill-frame support. Backends that support spilling should keep the frame implementation as a separate private type rather than adding frame methods to `arch`.

## Step 2 - Add `interp/jit_<arch>.go`

Add a concrete `lowerer` in package `interp`, then provide a build-tagged `newCompiler` implementation for the architecture.

```go
type archJIT struct{}
```

Handler rules:

- `trace` returns `false` without mutating published state when an opcode, kind, or observed shape is unsupported.
- External entry loads the context journal into pinned registers before entering the trace head.
- Guard exits materialize live symbolic state into `i.stack`, write `journalSP` and `journalNextIP`, append frame records, then return.
- Entry-frame `RETURN` writes boxed returns and returns with `trapNone`; inlined callee returns stitch values into the caller's symbolic stack.
- `CALL` may lower only when the recorded target, frame budget, refs, host functions, stack bounds, and Go write-barrier rules remain safe.

Scratch slot order is fixed:

| Slot | Purpose |
|---|---|
| `scratchStack` | `&i.stack[0]` |
| `scratchGlobals` | `&i.globals[0]` |
| `scratchBP` | current frame base pointer |
| `scratchSP` | interpreter stack pointer input |
| `scratchCtrl` | `&i.journal[0]` |

## Step 3 - Verify

```bash
go test ./asm/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

A nonzero trace counter (`p.Snapshot().JIT.Emits > 0`) after running a hot arithmetic function confirms end-to-end emission.

## Opcode Priority

Implement in this order to get meaningful benchmark gains early:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
3. Numeric arithmetic and comparison ops
4. Conversions
5. `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`
6. `BR`, `BR_IF`, `BR_TABLE`
7. `RETURN` for entry traces
8. RC-neutral refs: `REF_NULL`, `REF_IS_NULL`, `REF_EQ`

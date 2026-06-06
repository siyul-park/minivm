# Guide: Adding a New JIT Architecture

Checklist for adding a native backend for a new CPU architecture.

## Agent Summary

Adding an architecture is cross-cutting. Keep edits explicit:

- `asm/<arch>/`: physical register IDs, encoder, ABI, trampoline/caller.
- `interp/jit_<arch>.go`: opcode handlers, wired directly from `jit.go`.
- docs: this guide plus `jit-internals.md` if backend contracts change.

Do not change ARM64 behavior unless a shared `asm/` contract requires it.

## Before You Start

Read `docs/jit-internals.md`, especially Segment ABI and Assembler Pipeline. Use `asm/arm64/` and `interp/jit_arm64.go` as reference implementations.

## Step 1 - Create `asm/<arch>/`

Mirror `asm/arm64/`: implement `asm.Arch`, `asm.Encoder`, `asm.ABI`, optional `asm.Frame`, and the native callable trampoline.

`asm.ABI` exposes only scratch registers plus `NewCallable`. The trampoline contract is:

1. `Callable.Call(argv []uint64) error` receives at least the entry's declared scratch count.
2. The trampoline loads `argv[0..4]` into the backend scratch registers.
3. It calls the native chunk.
4. It stores those scratch registers back into `argv[0..4]`.

`Signature{Scratch}` declares the scratch registers used by the entry. There is no param/return ABI, no `asm.Value`, and no header packing.

Return nil from `Arch.Frame()` when the backend has no spill-frame support. Backends that support spilling should keep the frame implementation as a separate private type rather than adding frame methods to `arch`.

## Step 2 - Add `interp/jit_<arch>.go`

Add a concrete emitter in package `interp`, then provide a build-tagged `newJITCompiler` implementation for the architecture.

```go
type archJIT struct{}
```

Handler rules:

- `lower` returns `false` without mutating `jitContext` to reject an opcode.
- `prologue` loads declared live-ins from the VM stack.
- `exitIP` materializes shadow stack values into `i.stack`, writes `scratchSP` and `scratchNext`, then returns.
- `RETURN` must check `c.whole`; partial segments leave frame teardown to threaded code.
- `CALL` stays threaded unless a future design preserves frame setup, refs, host functions, stack bounds, and Go write barriers safely.

Scratch slot order is fixed:

| Slot | Purpose |
|---|---|
| `scratchStack` | `&i.stack[0]` |
| `scratchGlobals` | `&i.globals[0]` |
| `scratchBP` | current frame base pointer |
| `scratchSP` | interpreter stack pointer in/out |
| `scratchNext` | next interpreter IP |

## Step 3 - Verify

```bash
go test ./asm/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

A compiled segment counter (`p.Snapshot().JIT.Emits > 0`) after running a hot arithmetic loop confirms end-to-end emission.

## Opcode Priority

Implement in this order to get meaningful benchmark gains early:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
3. Numeric arithmetic and comparison ops
4. Conversions
5. `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`
6. `BR`, `BR_IF`, `BR_TABLE`
7. `RETURN` for whole-function Entry
8. RC-neutral refs: `REF_NULL`, `REF_IS_NULL`, `REF_EQ`

# Guide: Adding a New JIT Architecture

Checklist for adding a JIT backend for a new CPU architecture, e.g. x86-64.

## Agent Summary

Adding an architecture is cross-cutting. Keep edits explicit:

- `asm/<arch>/`: physical register IDs, encoder, ABI, trampoline/caller
- `jit/<arch>/`: `Lowerer` implementation, opcode handlers, blank-importable `init()`
- docs: this guide plus `jit-internals.md` if backend contracts change

Do not change ARM64 behavior unless a shared `asm/` contract requires it.

## Before You Start

Read `docs/jit-internals.md`, especially Segment ABI, Assembler Pipeline, and the `jit.Lowerer` interface contract. Use `asm/arm64/` and `jit/arm64/` as reference implementations.

## Overview

A new architecture needs:

1. `asm/<arch>/` implementing `asm.Arch`, `asm.Encoder`, `asm.ABI`, and a native callable
2. `jit/<arch>/lowerer.go` implementing `jit.Lowerer`
3. `jit/<arch>/register.go` with a blank-importable `init()` that calls `jit.Register`
4. Non-target platform stubs for any assembly files

## Step 1 — Create `asm/<arch>/`

Mirror `asm/arm64/`.

### `arch.go`

```go
package <arch>

import "github.com/siyul-park/minivm/asm"

type arch struct {
    enc *Encoder
    abi abi
}

var _ asm.Arch = arch{}

func New() asm.Arch {
    return arch{enc: &Encoder{}}
}

func (a arch) Registers() asm.RegInfo { return newRegInfo() }
func (a arch) Encoder()   asm.Encoder { return a.enc }
func (a arch) ABI()       asm.ABI     { return a.abi }
```

### `reg.go`

Define physical register IDs. Exclude reserved registers (SP, FP, LR).

```go
const (
    RAX uint8 = 0
    RCX uint8 = 1
    // ...
)

func newRegInfo() asm.RegInfo {
    return asm.NewRegInfo(
        asm.NewRegMask([]uint8{RAX, RCX, /* allocatable int regs */}),
        asm.NewRegMask([]uint8{/* allocatable float regs */}),
        []asm.PReg{/* scratch regs: ScratchStack, ScratchGlobals, ScratchBP, ScratchNext */},
    )
}
```

The last four scratch registers map to `jit.ScratchStack` through `jit.ScratchNext` in order. `Scratch()` must return at least `jit.ScratchCount` elements.

### `encoder.go`

Implement `asm.Encoder`. Reject unsupported type/width combinations with `asm.ErrInvalidOperand`.

```go
type Encoder struct{}

func (e *Encoder) Encode(inst asm.Instruction) ([]byte, error) { ... }
```

### `instr.go`

Provide instruction constructors returning `asm.Instruction`.

```go
func ADD(dst, src1, src2 asm.Reg) asm.Instruction { ... }
func MOV(dst, src asm.Reg)        asm.Instruction { ... }
func LDI(dst asm.Reg, imm uint64) []asm.Instruction { ... }
func RET()                        asm.Instruction { ... }
```

### `abi.go` and `abi_<arch>.s` / `abi_stub.go`

Implement `asm.ABI`. The trampoline must:

1. Marshal `[]asm.Value` params into native calling-convention registers
2. Call the native chunk
3. Collect register returns into `[]asm.Value`

```go
type abi struct{}

var _ asm.ABI = abi{}

func (abi) MaxArgs() int    { return 8 }
func (abi) MaxReturns() int { return 8 }
func (abi) Arg(idx int, t asm.RegType, w asm.RegWidth) asm.PReg    { ... }
func (abi) Return(idx int, t asm.RegType, w asm.RegWidth) asm.PReg { ... }
func (abi) Scratch() []asm.PReg                                     { ... }
func (abi) NewCallable(sig asm.Signature, addr unsafe.Pointer) (asm.Callable, error) { ... }
```

Non-target stub (`abi_stub.go`):

```go
//go:build !<arch>

package <arch>

func invoke(addr uintptr, argv uintptr) {}
```

### Tests

Prove `asm.New(arch)` + `asm.LinkAll` produces a callable chunk for simple addition. Cover internal entry points and invalid operands.

## Step 2 — Create `jit/<arch>/`

### `lowerer.go`

Implement `jit.Lowerer`. See `jit/arm64/lowerer.go` for reference.

```go
//go:build <arch>

package <arch>

import (
    "github.com/siyul-park/minivm/asm"
    asm<arch> "github.com/siyul-park/minivm/asm/<arch>"
    "github.com/siyul-park/minivm/instr"
    "github.com/siyul-park/minivm/jit"
)

type Lowerer struct{}

var _ jit.Lowerer = Lowerer{}

func (Lowerer) Arch() asm.Arch { return asm<arch>.New() }

func (l Lowerer) Prologue(c *jit.Context, fn *types.Function) {
    l.bind(c)
    for _, v := range c.Inputs {
        c.Assembler.Emit(asm<arch>.MOV(v, v))
    }
}

func (Lowerer) Epilogue(_ *jit.Context) {}

func (l Lowerer) Exit(c *jit.Context, nextIP int) {
    rNext := c.Scratch[jit.ScratchNext]
    // pin stack values to ABI returns, then:
    vNext := c.Assembler.Reg(asm.RegTypeInt, asm.Width64)
    _ = c.Assembler.Pin(vNext, rNext)
    c.Assembler.Emit(asm<arch>.LDI(vNext, uint64(nextIP))...)
    c.Assembler.Emit(asm<arch>.RET())
}

func (l Lowerer) Lower(c *jit.Context, op instr.Opcode) bool {
    if op != instr.CALL {
        c.Target = -1
    }
    switch op {
    case instr.NOP:
        return l.nop(c)
    case instr.RETURN:
        return l.ret(c)
    // ... register remaining opcodes
    }
    return false
}

func (Lowerer) ret(c *jit.Context) bool {
    if !c.Whole {
        return false  // frame teardown stays in Go wrapper for partial segments
    }
    c.IP += instr.Instruction(c.Code[c.IP:]).Width()
    c.Stop = true
    return true
}
```

Handler rules:

- `Lower` returns `false` without mutating `Context` to reject an opcode
- `RETURN` must check `c.Whole` and return false if not set
- Branch terminators set `c.Stop = true` and optionally `c.Successor`; `Exit` is called by the compiler afterward unless `c.Closed` is true
- `CALL` lowering checks `c.Target` (set by the preceding `CONST_GET`) and falls back to threaded when target is unknown or not yet jitted

### `register.go`

```go
//go:build <arch>

package <arch>

import "github.com/siyul-park/minivm/jit"

func init() {
    jit.Register("<arch>", Lowerer{})
}
```

### `register_stub.go`

```go
//go:build !<arch>

package <arch>
```

### `lowerer_stub.go`

```go
//go:build !<arch>

package <arch>

import (
    "github.com/siyul-park/minivm/asm"
    "github.com/siyul-park/minivm/instr"
    "github.com/siyul-park/minivm/jit"
    "github.com/siyul-park/minivm/types"
)

type Lowerer struct{}

var _ jit.Lowerer = Lowerer{}

func (Lowerer) Arch() asm.Arch                             { return nil }
func (Lowerer) Prologue(_ *jit.Context, _ *types.Function) {}
func (Lowerer) Epilogue(_ *jit.Context)                    {}
func (Lowerer) Exit(_ *jit.Context, _ int)                 {}
func (Lowerer) Lower(_ *jit.Context, _ instr.Opcode) bool  { return false }
```

## Step 3 — Wire the blank import

In `cmd/minivm/main.go` and any test binaries:

```go
import _ "github.com/siyul-park/minivm/jit/<arch>"
```

## Step 4 — Verify

```bash
go test ./asm/<arch>/... ./jit/<arch>/... ./interp/...
GOOS=linux GOARCH=<arch> go build ./...
```

Non-nil `jit.Active()` after blank import confirms registration. A compiled segment counter (`p.Snapshot().JIT.Emits > 0`) after running a hot arithmetic loop confirms end-to-end emission.

## Opcode Priority

Implement in this order to get meaningful benchmark gains early:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`, `CONST_GET`
3. All `I32_*` arithmetic and comparison ops
4. All `I64_*` arithmetic and comparison ops
5. All `F32_*` and `F64_*` ops
6. Conversions: `I32_TO_I64_*`, `I32_TO_F32/F64_*`, etc.
7. `LOCAL_GET/SET/TEE`, `GLOBAL_GET/SET/TEE`
8. `BR`, `BR_IF`, `BR_TABLE`
9. `RETURN` (whole-function Entry path)
10. `CALL` (direct-BL via `jit.Slots`)
11. Phase B: `REF_NULL`, `REF_IS_NULL`, `REF_EQ`

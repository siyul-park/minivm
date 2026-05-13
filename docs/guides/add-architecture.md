# Guide: Adding a New JIT Architecture

Checklist for adding a JIT backend for a new CPU architecture, e.g. x86-64.

## Agent Summary

Adding an architecture is cross-cutting. Keep edits explicit:

- `asm/<arch>/`: register model, encoder, ABI, trampoline/caller, stubs, tests
- `interp/jit_<arch>.go`: arch selection, prologue/epilogue, opcode handlers
- docs: this guide plus `jit-internals.md` if backend contracts change

Do not change ARM64 behavior unless a shared `asm/` contract requires it.

## Before You Start

Read `docs/jit-internals.md`, especially Assembler API, Buffer lifecycle, and JIT handler contract. Use `asm/arm64/` as the reference implementation.

## Overview

A new architecture needs:

1. `asm/<arch>/` implementing `asm.Encoder`, `asm.ABI`, and `asm.Caller`
2. an `asm/<arch>/Arch` singleton
3. non-target platform stubs
4. `interp/jit_<arch>.go` setting `arch` and registering opcode handlers

## Step 1 — Create `asm/<arch>/`

Mirror `asm/arm64/`.

### `arch.go`

```go
//go:build <arch>

package <arch>

import "github.com/siyul-park/minivm/asm"

var Arch = &asm.Arch{
    Registers: NewRegInfo(),
    Encoder:   NewEncoder(),
    ABI:       NewABI(),

    // Caller-saved metadata registers, e.g. next interpreter IP.
    // Must not overlap ABI params/returns.
    // Leave at least 2 registers outside Scratch for trampoline temporaries after BL.
    Scratch: asm.NewRegMask([]uint8{...}),
}
```

### `reg.go`

Define physical register IDs. Only caller-saved registers belong in allocatable sets; exclude reserved registers such as stack pointer, frame pointer, and link register.

```go
const (
    RAX uint8 = 0
    RCX uint8 = 1
    RDX uint8 = 2
    RSI uint8 = 3
    RDI uint8 = 4

    XMM0 uint8 = 0
    XMM1 uint8 = 1
)

func NewRegInfo() asm.RegInfo {
    intAllocatable := []uint8{RAX, RCX, RDX, RSI, RDI}
    fltAllocatable := []uint8{XMM0, XMM1, XMM2, XMM3, XMM4, XMM5, XMM6, XMM7}

    return asm.NewRegInfo(
        16,
        16,
        intAllocatable,
        fltAllocatable,
    )
}
```

### `encoder.go`

Implement `asm.Encoder`.

```go
var _ asm.Encoder = (*Encoder)(nil)

type Encoder struct{}

func NewEncoder() *Encoder { return &Encoder{} }

func (e *Encoder) Encode(inst asm.Instruction) ([]byte, error) {
    // dispatch on inst.Op and encode bytes
}
```

### `instr.go`

Provide instruction constructors returning `asm.Instruction`.

```go
func ADD(dst, src1, src2 asm.Operand) asm.Instruction { ... }
func MOV(dst, src asm.Operand) asm.Instruction { ... }
func RET() asm.Instruction { ... }
```

### `abi.go` and `abi_<arch>.go` / `abi_<arch>.s`

Implement `asm.ABI` and `asm.Caller`. The trampoline must:

1. marshal `params []uint64` into native calling-convention registers
2. call the chunk
3. collect register returns into `[]uint64`

```go
var _ asm.ABI = (*ABI)(nil)

const (
    maxParams  = 8
    maxReturns = 8
)

type ABI struct{}

func (a *ABI) NewCaller(sig *asm.Signature, chunk *asm.Chunk) (asm.Caller, error) {
    return NewCaller(sig, chunk)
}

func (a *ABI) MaxParams() int  { return maxParams }
func (a *ABI) MaxReturns() int { return maxReturns }
```

### `abi_stub.go`

Non-target stubs must declare every exported symbol from build-tagged files.

```go
//go:build !<arch>

package <arch>

import "github.com/siyul-park/minivm/asm"

type ABI struct{}

var Arch *asm.Arch

func NewABI() *ABI { return nil }

func (a *ABI) NewCaller(_ *asm.Signature, _ *asm.Chunk) (asm.Caller, error) {
    return nil, nil
}

func (a *ABI) MaxParams() int  { return 0 }
func (a *ABI) MaxReturns() int { return 0 }

func NewEncoder() *Encoder  { return nil }
func NewRegInfo() asm.RegInfo { return asm.RegInfo{} }
```

### Tests

Create build-tagged tests, at minimum proving `Assembler.Compile()` + `Link()` returns a callable chunk for simple addition.

```go
//go:build <arch>

package <arch>

func TestAssembler_Compile(t *testing.T) {
    buffer, err := asm.NewBuffer(256)
    require.NoError(t, err)
    defer buffer.Free()

    a := asm.NewAssembler(Arch, buffer)

    left, _ := a.Take(asm.RegTypeInt, asm.Width64)
    right, _ := a.Take(asm.RegTypeInt, asm.Width64)

    result := a.NewVReg(asm.RegTypeInt, asm.Width64)
    a.Push(result)
    a.Emit(ADD(result, left, right))
    a.Emit(RET())

    obj, err := a.Compile()
    require.NoError(t, err)

    callers, err := a.Link([]*asm.RelocObject{obj})
    require.NoError(t, err)
    require.NotNil(t, callers[0])

    out, err := callers[0].Call([]uint64{3, 5}, nil)
    require.NoError(t, err)
    require.Equal(t, []uint64{8}, out)
}
```

## Step 2 — Create `interp/jit_<arch>.go`

```go
//go:build <arch>

package interp

import (
    "github.com/siyul-park/minivm/asm"
    "<module>/asm/<arch>"
    "github.com/siyul-park/minivm/instr"
)

func init() {
    arch = <arch>.Arch

    jit[_PROLOGUE] = func(c *jitCompiler) (bool, bool) {
        c.assembler.Emits(<arch>.LDI(c.scratch[rNext], uint64(c.end))...)
        return true, false
    }

    jit[_EPILOGUE] = func(c *jitCompiler) (bool, bool) {
        c.assembler.Emits(<arch>.LDI(c.scratch[rNext], uint64(c.end))...)
        c.assembler.Emit(<arch>.RET())
        return true, false
    }

    jit[instr.NOP] = func(c *jitCompiler) (bool, bool) {
        c.ip++
        return true, false
    }

    jit[instr.I32_ADD] = func(c *jitCompiler) (bool, bool) {
        c.ip++

        r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
        if !ok {
            return false, false
        }

        r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
        if !ok {
            return false, false
        }

        dst := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
        c.assembler.Emit(<arch>.ADD(dst, r0, r1))
        c.assembler.Push(dst)

        return true, false
    }

    // register remaining opcodes
}
```

Handler rules:

- prologue loads segment exit IP into scratch next register
- epilogue reloads possibly truncated exit IP, then returns
- normal success returns `(true, false)`
- failure returns `(false, false)`
- branch terminators return `(true, true)` and emit their own `RET`

Opcode priority:

1. `NOP`, `DROP`, `DUP`, `SWAP`
2. `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`
3. all `I32_*` arithmetic/comparison ops
4. all `I64_*` arithmetic/comparison ops
5. all `F32_*` and `F64_*` ops
6. conversions: `I32_TO_I64_*`, etc.
7. `CONST_GET`

## Step 3 — Verify

```bash
make test
make lint

GOARCH=<arch> go test -race -v ./interp/... -run TestInterpreter_Run
```

A non-nil buffer after a hot-function threshold confirms JIT activity.

Interpreter sampling happens every `WithTick` instructions, default `128`. The same cadence drives context cancellation, `WithFuel`, and `WithHook`. JIT activates when aggregate function samples reach `WithThreshold / WithTick`, default `4096 / 128 = 32`, and compiles only sampled basic blocks.

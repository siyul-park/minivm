# Guide: Adding a New JIT Architecture

Step-by-step checklist for adding a JIT backend for a new CPU architecture (e.g. x86-64).

## Before You Start

Read [docs/jit-internals.md](../jit-internals.md) — particularly the Assembler API, Buffer lifecycle, and JIT handler contract sections.  
Study `asm/arm64/` as the reference implementation to mirror.

---

## Overview

Adding a new architecture requires:
1. An `asm/<arch>/` package implementing three interfaces: `asm.Encoder`, `asm.ABI`, `asm.Caller`
2. An `asm/<arch>/Arch` singleton wiring them together
3. A stub file for non-target platforms
4. A `interp/jit_<arch>.go` file setting `arch` and registering all opcode handlers

---

## Step 1 — Create `asm/<arch>/`

Mirror the structure of `asm/arm64/`. Required files:

### `arch.go`

```go
//go:build <arch>

package <arch>

import "github.com/siyul-park/minivm/asm"

var Arch = &asm.Arch{
    Registers: NewRegInfo(),
    Encoder:   NewEncoder(),
    ABI:       NewABI(),
    // Scratch lists caller-saved registers reserved for out-of-band JIT metadata
    // (e.g., the next interpreter IP). These must NOT overlap with the ABI
    // param/return range. Leave at least 2 registers outside Scratch free for
    // use as temporaries in the invoke trampoline after BL.
    Scratch: asm.NewRegMask([]uint8{...}),
}
```

### `reg.go`

Define physical register IDs as constants. Only caller-saved registers go into the allocatable set; reserved registers (stack pointer, frame pointer, link register) must be excluded.

```go
const (
    RAX uint8 = 0
    RCX uint8 = 1
    RDX uint8 = 2
    RSI uint8 = 3
    RDI uint8 = 4
    // ... additional GPRs
    XMM0 uint8 = 0
    XMM1 uint8 = 1
    // ... additional XMM registers
)

func NewRegInfo() asm.RegInfo {
    // Only caller-saved registers — RSP and RBP are reserved, do not include them.
    intAllocatable := []uint8{RAX, RCX, RDX, RSI, RDI}
    fltAllocatable := []uint8{XMM0, XMM1, XMM2, XMM3, XMM4, XMM5, XMM6, XMM7}
    return asm.NewRegInfo(
        16,              // total integer register count
        16,              // total float register count
        intAllocatable, // allocatable integer registers
        fltAllocatable, // allocatable float registers
    )
}
```

### `encoder.go`

Implement `asm.Encoder`:

```go
var _ asm.Encoder = (*Encoder)(nil)

type Encoder struct{}

func NewEncoder() *Encoder { return &Encoder{} }

func (e *Encoder) Encode(inst asm.Instruction) ([]byte, error) {
    // dispatch on inst.Op and encode to bytes
}
```

### `instr.go`

Define instruction constructors that produce `asm.Instruction` values for common operations:

```go
func ADD(dst, src1, src2 asm.Operand) asm.Instruction { ... }
func MOV(dst, src asm.Operand) asm.Instruction { ... }
func RET() asm.Instruction { ... }
// etc.
```

### `abi.go` and `abi_<arch>.go` (or `abi_<arch>.s`)

Implement `asm.ABI` and `asm.Caller`. The caller invokes native chunks via a trampoline that:
1. Marshals `params []uint64` into the native calling convention registers
2. Calls the chunk
3. Collects return values from registers into a `[]uint64`

```go
var _ asm.ABI = (*ABI)(nil)

const (
    maxParams  = 8  // adjust per ABI
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

Keeps the package compilable on non-target platforms. Must declare every exported symbol that the build-tagged files export, so the linker succeeds on all platforms:

```go
//go:build !<arch>

package <arch>

import "github.com/siyul-park/minivm/asm"

// ABI must be declared here too, matching the build-tagged abi.go.
type ABI struct{}

var Arch *asm.Arch

func NewABI() *ABI                                                      { return nil }
func (a *ABI) NewCaller(_ *asm.Signature, _ *asm.Chunk) (asm.Caller, error) { return nil, nil }
func (a *ABI) MaxParams() int                                           { return 0 }
func (a *ABI) MaxReturns() int                                         { return 0 }

func NewEncoder() *Encoder { return nil }
func NewRegInfo() asm.RegInfo  { return asm.RegInfo{} }
```

### Tests

Create `arch_test.go` with `//go:build <arch>`. At minimum, test that `Assembler.Compile()` + `Link()` produces a callable chunk that computes a simple addition:

```go
//go:build <arch>

package <arch>

func TestAssembler_Compile(t *testing.T) {
    buffer, err := asm.NewBuffer(256)
    require.NoError(t, err)
    defer buffer.Free()

    a := asm.NewAssembler(Arch, buffer)
    left, _  := a.Take(asm.RegTypeInt, asm.Width64)
    right, _ := a.Take(asm.RegTypeInt, asm.Width64)
    result   := a.NewVReg(asm.RegTypeInt, asm.Width64)
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

---

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

    // PROLOGUE: emitted at the start of each segment.
    // Load the segment's exit IP into the reserved next register so it is always set
    // on return, even if the segment is later truncated.
    jit[_PROLOGUE] = func(c *jitCompiler) (bool, bool) {
        c.assembler.Emits(<arch>.LDI(c.next, uint64(c.blockEnd))...)
        return true, false
    }

    // EPILOGUE: emitted at the end of each non-terminating segment.
    // Reload the (possibly truncated) exit IP into next, then return.
    jit[_EPILOGUE] = func(c *jitCompiler) (bool, bool) {
        c.assembler.Emits(<arch>.LDI(c.next, uint64(c.blockEnd))...)
        c.assembler.Emit(<arch>.RET())
        return true, false
    }

    // Register opcode handlers.
    // Return (true, false) on success, (false, false) on failure.
    // Branch terminators return (true, true) and emit their own RET.
    jit[instr.NOP] = func(c *jitCompiler) (bool, bool) {
        c.ip++
        return true, false
    }

    jit[instr.I32_ADD] = func(c *jitCompiler) (bool, bool) {
        c.ip++
        r1, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
        if !ok { return false, false }
        r0, ok := c.assembler.Take(asm.RegTypeInt, asm.Width32)
        if !ok { return false, false }
        dst := c.assembler.NewVReg(asm.RegTypeInt, asm.Width32)
        c.assembler.Emit(<arch>.ADD(dst, r0, r1))
        c.assembler.Push(dst)
        return true, false
    }

    // ... register remaining opcodes ...
}
```

**Priority order for opcode handlers** (highest return on investment):
1. `NOP`, `DROP`, `DUP`, `SWAP`
2. `I32_CONST`, `I64_CONST`, `F32_CONST`, `F64_CONST`
3. All `I32_*` arithmetic and comparison ops
4. All `I64_*` arithmetic and comparison ops
5. All `F32_*` and `F64_*` ops
6. Type conversions (`I32_TO_I64_*`, etc.)
7. `CONST_GET`

---

## Step 3 — Verify

```bash
# On the target architecture:
make test
make lint

# Check that JIT is active (buffer should be non-nil after hot-block threshold):
GOARCH=<arch> go test -race -v ./interp/... -run TestInterpreter_Run
```

The JIT activates after a function's hot-block counter crosses the threshold (default 4096 ticks). Tests that run programs in a loop will exercise the JIT path.

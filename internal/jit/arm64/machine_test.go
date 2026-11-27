package arm64_test

import (
	"fmt"
	"math"
	"math/rand/v2"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	target "github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// regs names value v with the register its type is represented in; every
// exit it is asked for is exit, and every release resumes at resume.
type regs map[ssa.Value]ssa.Type

// fused is regs whose every value is the condition of the branch right
// after its operation.
type fused struct{ regs }

// known is regs where an OpConst defines each value of words and objects.
type known struct {
	compile.Site
	words   map[ssa.Value]uint64
	objects map[ssa.Value]transform.Object
}

// table is the state of a types.Map's table, read through reflection (snapshot).
type table struct {
	ctrl, slots, order     []uint64
	pos                    []uint64
	mask, shift, seed, gen uint64
	count, used            int
	capacity               int
}

const (
	exit   = asm.Label(99)
	resume = asm.Label(98)
)

func (r regs) Slot(s ssa.Slot) ssa.Type {
	if s.Space == ssa.SpaceGlobal || s.Space == ssa.SpaceUpval {
		return ssa.TypeRef
	}
	return 0
}

func (r regs) Fuse(ssa.Value) bool { return false }

func (r regs) Const(ssa.Value) (uint64, bool) { return 0, false }

func (r regs) Object(ssa.Value) (transform.Object, bool) { return transform.Object{}, false }

func (r regs) Deopt() asm.Label { return exit }

func (r regs) Trap() asm.Label { return exit }

func (r regs) Bridge() (asm.Label, asm.Label) { return exit, resume }

func (r regs) Release(asm.VReg) (asm.Label, asm.Label) { return exit, resume }

func (r regs) Box(asm.VReg) (asm.Label, asm.Label) { return exit, resume }

func (fused) Fuse(ssa.Value) bool { return true }

func (k known) Const(v ssa.Value) (uint64, bool) {
	word, ok := k.words[v]
	return word, ok
}

func (k known) Object(v ssa.Value) (transform.Object, bool) {
	o, ok := k.objects[v]
	return o, ok
}

func (r regs) Reg(v ssa.Value) asm.VReg {
	switch r[v] {
	case ssa.TypeI64, ssa.TypeRef:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width64)
	case ssa.TypeF32:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width32)
	case ssa.TypeF64:
		return asm.NewVReg(int32(v), asm.RegTypeFloat, asm.Width64)
	default:
		return asm.NewVReg(int32(v), asm.RegTypeInt, asm.Width32)
	}
}

func (r regs) Type(v ssa.Value) ssa.Type { return r[v] }

// fibonacciOf is fibonacci's own result for n, computed in Go.
func fibonacciOf(n int32) int32 {
	if n < 2 {
		return n
	}
	return fibonacciOf(n-1) + fibonacciOf(n-2)
}

// frame is a function without code whose slots are params then locals.
func frame(params, locals []types.Type) *types.Function {
	return &types.Function{Typ: &types.FunctionType{Params: params}, Locals: locals}
}

func state(b *ssa.Builder, block int, stack ...ssa.Operand) ssa.Value {
	v := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 1, Stack: stack}}, Results: []ssa.Value{v}})
	return v
}

func operands(ctx *jit.Context, f jit.Frame) []uint64 {
	var out []uint64
	for _, o := range f.Stack {
		out = append(out, read(ctx, o.Value))
	}
	return out
}

// read is the raw native value v names in the suspended activation.
func read(ctx *jit.Context, v jit.Value) uint64 {
	if v.Loc.Spilled {
		return ctx.Slot(v.Loc.Slot)
	}
	return ctx.Reg(v.Loc.Reg)
}

// arrays is the identity of m's backing arrays: a rehash changes it.
func arrays(m any) [2]uintptr {
	v := reflect.ValueOf(m).Elem().FieldByName("table")
	return [2]uintptr{v.FieldByName("ctrl").Pointer(), v.FieldByName("slots").Pointer()}
}

func snapshot(m any) table {
	v := reflect.ValueOf(m).Elem().FieldByName("table")
	t := table{
		mask: v.FieldByName("mask").Uint(), shift: v.FieldByName("shift").Uint(), seed: v.FieldByName("seed").Uint(), gen: v.FieldByName("gen").Uint(),
		count: int(v.FieldByName("count").Int()), used: int(v.FieldByName("used").Int()),
	}
	ctrl, slots, order, pos := v.FieldByName("ctrl"), v.FieldByName("slots"), v.FieldByName("order"), v.FieldByName("pos")
	for i := range ctrl.Len() {
		t.ctrl = append(t.ctrl, ctrl.Index(i).Uint())
	}
	for i := range slots.Len() {
		t.slots = append(t.slots, slots.Index(i).Uint())
	}
	for i := range order.Len() {
		t.order = append(t.order, order.Index(i).Uint())
	}
	for i := range pos.Len() {
		t.pos = append(t.pos, pos.Index(i).Uint())
	}
	t.capacity = ctrl.Len()
	return t
}

// canonical is the rows that replace a positive NaN in X16 with
// types.BoxedNaN.
func canonical() []asm.Instruction {
	return slices.Concat(
		target.LDI(target.X17, uint64(types.BoxedNaN)-1),
		[]asm.Instruction{target.CMP(target.X16, target.X17), target.CSINC(target.X16, target.X16, target.X17, target.CondLE)},
	)
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// scalars is the keys of mirror for a scalar key kind: one word per key.
func scalars[K types.Scalar](box func(K) types.Boxed, keyOf func(int) K) func(*types.Heap, int) []types.Boxed {
	return func(_ *types.Heap, n int) []types.Boxed { return []types.Boxed{box(keyOf(n))} }
}

func TestNew(t *testing.T) {
	testenv.MustHaveArch(t, "arm64")

	t.Run("runs a translated function over the VM stack", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32(), types.TypeI32()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL)
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(6), types.BoxI32(7)}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(43), stack[0])
		require.Zero(t, ctx.Level())
	})

	t.Run("suspends at the loop safepoint until the budget is refilled", func(t *testing.T) {
		stack := []types.Boxed{types.BoxI32(10), types.BoxI32(99), types.BoxI32(99)}
		// sum(n) = 0 + 1 + ... + n-1 over one parameter and two locals.
		fn := function(t, []types.Type{types.TypeI32()}, []types.Type{types.TypeI32(), types.TypeI32()}, func(b *instr.Builder) {
			loop, done := b.Label(), b.Label()
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
		})
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Budget = 3

		safepoints := 0
		for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
			require.Equal(t, jit.TrapBridge, trap)
			require.Equal(t, jit.ExitSafepoint, exits[ctx.Exit()].Kind)
			safepoints++
			ctx.Budget = 3
		}
		require.Equal(t, types.BoxI32(45), stack[0])
		require.Equal(t, 3, safepoints)
	})

	t.Run("deopts a division by zero with the operands of its instruction", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32(), types.TypeI32()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(6), types.BoxI32(0)}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.Equal(t, 2*instr.MustNew(instr.LOCAL_GET, 0).Width(), exit.Frame.IP)
		require.Equal(t, []uint64{6, 0}, operands(ctx, exit.Frame))
	})

	t.Run("boxes and stores a wide i64 through a resumable exit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI64)
		shift := b.Value(ssa.TypeI64)
		wide := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(50), Results: []ssa.Value{shift}})
		at := state(b, entry, ssa.Operand{Value: one}, ssa.Operand{Value: shift})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntShl, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		store := state(b, entry, ssa.Operand{Value: wide})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{wide}, State: store})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn})

		// The local slot is TypeI64, which Return also releases before it
		// zeroes the slot: rc=1 on the box means that release is the last
		// reference, so it takes its own resumable ExitRelease.
		rc := []int{0, 0, 0, 1}
		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI64()}), nil, 0, false)
		ctx := enter(t, stack)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitBox, exit.Kind)
		require.Equal(t, uint64(1<<50), read(ctx, exit.Word))
		ctx.Results[0] = uint64(types.BoxRef(3))

		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		exit = exits[ctx.Exit()]
		require.Equal(t, jit.ExitRelease, exit.Kind)
		require.Equal(t, uint64(types.BoxRef(3)), read(ctx, exit.Word))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Zero(t, stack[0])
	})

	t.Run("boxes and returns a wide i64 through a resumable exit", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI64)
		shift := b.Value(ssa.TypeI64)
		wide := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(50), Results: []ssa.Value{shift}})
		at := state(b, entry, ssa.Operand{Value: one}, ssa.Operand{Value: shift})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntShl, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		ret := state(b, entry, ssa.Operand{Value: wide})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{wide}, State: ret})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI32()}), nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitBox, exit.Kind)
		require.Equal(t, uint64(1<<50), read(ctx, exit.Word))
		ctx.Results[0] = uint64(types.BoxRef(3))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, types.BoxRef(3), stack[0])
	})

	t.Run("releases an i64 slot's old occupant on store, as threaded LOCAL_SET does", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI64()}, nil, func(b *instr.Builder) {
			b.Emit(instr.I64_CONST, 5).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		})
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		rc := []int{0, 0, 0, 1}
		stack := []types.Boxed{types.BoxRef(3)}
		ctx := enter(t, stack)
		ctx.RC = address(t, rc)

		// rc=1 on the old occupant: store's release is the last reference,
		// so it takes its own resumable exit; native code never decrements
		// past 1 itself, so this simulates the interpreter's own release.
		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitRelease, exit.Kind)
		require.Equal(t, uint64(types.BoxRef(3)), read(ctx, exit.Word))
		rc[3] = 0
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Zero(t, rc[3])
	})

	t.Run("unboxes an inline i64 slot and a heap-promoted one, deopts on a ref to a non-I64", func(t *testing.T) {
		// Three params keep this function out of the register convention
		// (compile.arguments admits at most two), so slot 0's LOCAL_GET
		// reaches guard.kind's own heap-unbox logic instead of a register
		// capture's raw move.
		fn := function(t, []types.Type{types.TypeI64(), types.TypeI32(), types.TypeI32()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_ADD).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI64()}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		// A register-convention i64 result reaches the slot raw.
		inline := []types.Boxed{types.BoxI64(-42)}
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, inline)))
		raw := int64(-41)
		require.Equal(t, types.Boxed(uint64(raw)), inline[0])

		// A heap-promoted i64 (a wide value, from a threaded caller) unboxes
		// by borrow: no refcount change from the read itself. RETURN still
		// releases the slot's own occupant, so rc starts above one.
		promoted := []types.Boxed{types.BoxRef(3)}
		heap := []types.Value{nil, nil, nil, types.I64(1 << 50)}
		rc := []int{0, 0, 0, 2}
		ctx := enter(t, promoted)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		raw = int64(1<<50) + 1
		require.Equal(t, types.Boxed(uint64(raw)), promoted[0])
		require.Equal(t, 1, rc[3])

		mismatched := []types.Boxed{types.BoxRef(3)}
		badHeap := []types.Value{nil, nil, nil, types.I32(5)}
		badCtx := enter(t, mismatched)
		badCtx.Heap = address(t, badHeap)
		require.Equal(t, jit.TrapDeopt, jit.Enter(code, badCtx))
		require.Zero(t, exits[badCtx.Exit()].Frame.IP)
	})

	t.Run("bridges an operation it does not lower", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		table := b.Value(ssa.TypeRef)
		key := b.Value(ssa.TypeI32)
		got := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(5)), Results: []ssa.Value{table}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{key}})
		at := state(b, entry, ssa.Operand{Value: table}, ssa.Operand{Value: key})
		b.Add(entry, ssa.Operation{Op: ssa.OpMapGet, Args: []ssa.Value{table, key}, State: at, Results: []ssa.Value{got}})
		after := state(b, entry, ssa.Operand{Value: got})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{got}, State: after})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), &types.Function{Typ: &types.FunctionType{}, Locals: []types.Type{types.TypeI32()}, Code: instr.Marshal([]instr.Instruction{instr.MustNew(instr.MAP_GET)})}, nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitBridge, exit.Kind)
		require.Equal(t, instr.MAP_GET, exit.Code)
		require.Equal(t, []types.Kind{types.KindRef}, exit.Results)
		require.Equal(t, []uint64{uint64(types.BoxRef(5)), 2}, operands(ctx, exit.Frame))

		ctx.Results[0] = uint64(types.BoxRef(9))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, types.BoxRef(9), stack[0])
	})

	t.Run("counts references through the Context and exits on the last release", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{ref}})
		at := state(b, entry)
		b.Add(entry, ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{ref}})
		for range 3 {
			b.Add(entry, ssa.Operation{Op: ssa.OpRefRelease, Args: []ssa.Value{ref}, State: at})
		}
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn})

		rc := []int{0, 0, 2}
		stack := []types.Boxed{types.BoxRef(2)}
		code, exits := lower(t, arm64.New(), b.Build(), frame([]types.Type{types.TypeString()}, nil), nil, 0, false)
		ctx := enter(t, stack)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitRelease, exit.Kind)
		require.Equal(t, uint64(types.BoxRef(2)), read(ctx, exit.Word))
		require.Equal(t, []int{0, 0, 1}, rc)

		rc[2] = 2
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("releases and clears the references its slots hold on return", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeString()}, nil, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		})
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		rc := []int{0, 0, 0, 2}
		stack := []types.Boxed{types.BoxRef(3)}
		ctx := enter(t, stack)
		ctx.RC = address(t, rc)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, []int{0, 0, 0, 1}, rc)
		require.Equal(t, types.BoxI32(1), stack[0])

		stack[0] = types.BoxRef(3)
		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, uint64(types.BoxRef(3)), read(ctx, exits[ctx.Exit()].Word))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, types.BoxI32(1), stack[0])
	})

	t.Run("calls its own entry directly, bypassing the natives table", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, _, err := transform.Translate(module, nil, 2, fib, 0)
		require.NoError(t, err)
		code, _ := lower(t, arm64.New(), f, fib, module.Objects, 2, false)

		// natives[2] is deliberately wrong: a self call never reads it.
		natives := []uintptr{0, 0, 0xdead}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = address(t, natives)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(55), stack[0])
		require.Zero(t, ctx.Level())
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("records a nested native call's return address in its own code", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, _, err := transform.Translate(module, nil, 2, fib, 0)
		require.NoError(t, err)
		assembly, err := compile.Lower(f, compile.Unit{Function: fib, Module: transform.Module{Objects: module.Objects}, Tier: jit.Baseline}, arm64.New())
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(target.New(), len(assembly.Bytes))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		code, err := buffer.Link(assembly.Bytes)
		require.NoError(t, err)

		natives := []uintptr{0, 0, code}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = address(t, natives)
		ctx.RC = address(t, rc)
		ctx.SetEdge(2)

		require.Equal(t, jit.TrapBridge, jit.Enter(code+uintptr(assembly.Entry), ctx))
		require.Equal(t, jit.ExitCall, assembly.Exits[ctx.Exit()].Kind)
		require.Equal(t, 2, ctx.Level())
		require.GreaterOrEqual(t, ctx.Records[1].PC, code)
		require.Less(t, ctx.Records[1].PC, code+uintptr(len(assembly.Bytes)))
	})

	t.Run("resumes a call to a function without native code with the result the interpreter places", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, _, err := transform.Translate(module, nil, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0, false)

		natives := []uintptr{0, 0, 0}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = address(t, natives)
		ctx.RC = address(t, rc)

		var args []int32
		trap := jit.Enter(code, ctx)
		for trap == jit.TrapBridge {
			exit := exits[ctx.Exit()]
			require.Equal(t, jit.ExitCall, exit.Kind)
			require.Equal(t, 2, exit.Callee)
			require.Equal(t, []types.Kind{types.KindI32}, exit.Results)
			require.Equal(t, instr.CALL, instr.Opcode(fib.Code[exit.Frame.IP-1]))
			// The boxed argument sits at the callee frame base: past the
			// caller's one slot and the operands below the call.
			n := stack[1+len(exit.Frame.Stack)].I32()
			args = append(args, n)
			ctx.Results[0] = uint64(uint32(fibonacciOf(n)))
			trap = jit.Resume(ctx)
		}
		require.Equal(t, jit.TrapReturn, trap)
		require.Equal(t, []int32{9, 8}, args)
		require.Equal(t, types.BoxI32(55), stack[0])
		// The borrowed callee's retain is never emitted, so a served call
		// costs no RC movement.
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("bridges a call past the depth limit or the stack top", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, _, err := transform.Translate(module, nil, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0, false)
		natives := []uintptr{0, 0, code}
		rc := []int{0, 0, 1}

		for _, limit := range []func(ctx *jit.Context, stack []types.Boxed){
			func(ctx *jit.Context, _ []types.Boxed) { ctx.SetEdge(1) },
			func(ctx *jit.Context, stack []types.Boxed) { ctx.Top = uintptr(unsafe.Pointer(&stack[1])) },
		} {
			stack := make([]types.Boxed, 64)
			stack[0] = types.BoxI32(10)
			ctx := enter(t, stack)
			ctx.Natives = address(t, natives)
			ctx.RC = address(t, rc)
			limit(ctx, stack)

			require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
			require.Equal(t, jit.ExitCall, exits[ctx.Exit()].Kind)
			require.Equal(t, 1, ctx.Level())
		}
	})

	t.Run("returns every kind boxed", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		consts := []types.Boxed{types.BoxI8(-1), types.BoxI1(true), types.BoxF64(1.5), types.BoxF32(-2), types.BoxI64(-3), types.BoxRef(4)}
		var args []ssa.Value
		for _, c := range consts {
			v := b.Value(ssa.TypeOf(c.Kind()))
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.Word(), Results: []ssa.Value{v}})
			args = append(args, v)
		}
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: args, State: at})

		stack := make([]types.Boxed, len(consts))
		code, _ := lower(t, arm64.New(), b.Build(), frame(nil, slices.Repeat([]types.Type{types.TypeI32()}, len(consts))), nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, consts, stack)
	})

	t.Run("returns each kind through Return's X0 and the Go entry stub's boxing", func(t *testing.T) {
		for _, c := range []struct {
			word types.Boxed
			typ  types.Type
		}{
			{types.BoxI1(true), types.TypeI1()}, {types.BoxI8(-1), types.TypeI8()}, {types.BoxI32(-7), types.TypeI32()},
			{types.BoxF32(-2), types.TypeF32()}, {types.BoxF64(1.5), types.TypeF64()}, {types.BoxRef(4), types.TypeString()},
		} {
			b := ssa.New("f")
			entry := b.Block()
			v := b.Value(ssa.TypeOf(c.word.Kind()))
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.word.Word(), Results: []ssa.Value{v}})
			at := state(b, entry)
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{v}, State: at})

			fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{c.typ}}}
			stack := make([]types.Boxed, 1)
			code, _ := lower(t, arm64.New(), b.Build(), fn, nil, 0, false)
			require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
			require.Equal(t, c.word, stack[0])
		}
	})

	t.Run("returns two register-convention results through X0 and X1", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Value(ssa.TypeF64)
		y := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: math.Float64bits(2.5), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(4)), Results: []ssa.Value{y}})
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{x, y}, State: at})

		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64(), types.TypeString()}}}
		stack := make([]types.Boxed, 2)
		code, _ := lower(t, arm64.New(), b.Build(), fn, nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, []types.Boxed{types.BoxF64(2.5), types.BoxRef(4)}, stack)
	})

	t.Run("a native call reads a register-convention result and forwards it through its own return", func(t *testing.T) {
		// The callee address (7) differs from the caller's lowering address
		// (0), so Call reaches it through the natives table, exercising
		// Call's DEF/MOV read after a real BLR, not the self-call branch.
		callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32()}}}
		b := ssa.New("callee")
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 42, Results: []ssa.Value{v}})
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{v}, State: at})
		// natives[address] holds the body, not the Go entry stub.
		calleeAssembly, err := compile.Lower(b.Build(), compile.Unit{Function: callee, Address: 7, Tier: jit.Baseline}, arm64.New())
		require.NoError(t, err)
		calleeBuffer, err := asm.NewBuffer(target.New(), len(calleeAssembly.Bytes))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, calleeBuffer.Free()) })
		calleeBody, err := calleeBuffer.Link(calleeAssembly.Bytes)
		require.NoError(t, err)

		// exit() reads the caller's own bytecode to find the CALL's width
		// for the exit map's IP, even though the CALL op below is built
		// directly as SSA: Code must hold a real CALL at IP 0.
		caller := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32()}},
			Code: instr.Marshal([]instr.Instruction{instr.MustNew(instr.CALL)}),
		}
		cb := ssa.New("caller")
		centry := cb.Block()
		callee32 := cb.Value(ssa.TypeRef)
		got := cb.Value(ssa.TypeI32)
		cb.Add(centry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(7)), Results: []ssa.Value{callee32}})
		// Its state does not own the callee, so Call neither retains nor
		// releases it (no Context.RC needed here).
		cat := cb.Value(ssa.TypeState)
		cb.Add(centry, ssa.Operation{
			Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 1, Stack: []ssa.Operand{{Value: callee32}}}},
			Results: []ssa.Value{cat},
		})
		cb.Add(centry, ssa.Operation{Op: ssa.OpCall, Args: []ssa.Value{callee32}, State: cat, Results: []ssa.Value{got}})
		rat := state(cb, centry)
		cb.Term(centry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{got}, State: rat})

		objects := transform.Objects{7: {Function: callee}}
		code, _ := lower(t, arm64.New(), cb.Build(), caller, objects, 0, false)

		natives := []uintptr{0, 0, 0, 0, 0, 0, 0, calleeBody}
		stack := make([]types.Boxed, 1)
		ctx := enter(t, stack)
		ctx.Natives = address(t, natives)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(42), stack[0])
	})

	t.Run("lowers each function afresh on one machine", func(t *testing.T) {
		m := arm64.New()
		first := function(t, []types.Type{types.TypeI32(), types.TypeI32()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		second := function(t, []types.Type{types.TypeI32()}, nil, func(b *instr.Builder) {
			other := b.Label()
			b.Emit(instr.LOCAL_GET, 0).BrIf(other)
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
			b.Bind(other).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
		})
		lower(t, m, translate(t, first), first, nil, 0, false)

		stack := []types.Boxed{types.BoxI32(0)}
		code, _ := lower(t, m, translate(t, second), second, nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, types.BoxI32(1), stack[0])
	})

	t.Run("sums a typed i32 array through a guarded loop", func(t *testing.T) {
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32())}, []types.Type{types.TypeI32(), types.TypeI32()}, func(b *instr.Builder) {
			header, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
			b.Bind(header)
			b.Emit(instr.LOCAL_GET, 1)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN)
			b.Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET)
			b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(header)
			b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
		})

		heap := []types.Value{nil, types.TypedArray[int32]{10, 20, 30, 40}}
		stack := []types.Boxed{types.BoxRef(1)}
		// RETURN releases every reference-capable slot, including the array
		// param itself; rc[1] starts above one so that release never falls
		// to the last reference and bridges.
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(100), stack[0])
		require.Zero(t, ctx.Level())
	})

	t.Run("reads a sliced array across bridge and safepoint resumes that relocate Context.Heap", func(t *testing.T) {
		// sum(a[i] + len(m)) over i < len(a): the bridged map.len (a map keyed by an array type gets no
		// guard and keeps the interpreter's map) keeps the loop quiet, so the hoist pass slices a.
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32()), types.NewMapType(types.NewArrayType(types.TypeI32()), types.TypeI32())}, []types.Type{types.TypeI32(), types.TypeI32()}, func(b *instr.Builder) {
			header, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
			b.Bind(header)
			b.Emit(instr.LOCAL_GET, 2)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN)
			b.Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 3)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET)
			b.Emit(instr.I32_ADD)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LEN)
			b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Br(header)
			b.Bind(done).Emit(instr.LOCAL_GET, 3).Emit(instr.RETURN)
		})
		f := translate(t, fn)
		for _, p := range []pass.Pass[*ssa.Function]{transform.NewPromotePass(), transform.NewLICMPass(), transform.NewDCEPass()} {
			_, err := p.Run(pass.NewManager(), f)
			require.NoError(t, err)
		}
		require.Contains(t, ssa.Format(f), "array.view")

		heap := []types.Value{nil, types.TypedArray[int32]{10, 20, 30, 40, 50, 60, 70}, types.NewMap(nil, types.NewMapType(types.NewArrayType(types.TypeI32()), types.TypeI32()), 0)}
		stack := []types.Boxed{types.BoxRef(1), types.BoxRef(2), 0, 0}
		// RETURN releases both reference params; counts above one keep
		// those releases from reaching the last reference.
		rc := []int{0, 2, 2}
		code, exits := lower(t, arm64.New(), f, fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)
		ctx.Budget = 2

		bridges, safepoints := 0, 0
		for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
			require.Equal(t, jit.TrapBridge, trap)
			switch exit := exits[ctx.Exit()]; exit.Kind {
			case jit.ExitBridge:
				require.Equal(t, instr.MAP_LEN, exit.Code)
				ctx.Results[0] = 3
				bridges++
			case jit.ExitSafepoint:
				ctx.Budget = 2
				safepoints++
			default:
				require.Fail(t, "unexpected exit", exit.Kind)
			}
			moved := slices.Clone(heap)
			clear(heap)
			heap = moved
			ctx.Heap = address(t, heap)
		}
		require.Equal(t, types.BoxI32(280+7*3), stack[0])
		require.Equal(t, 7, bridges)
		require.Positive(t, safepoints)
	})

	t.Run("sums a string's length in a loop with no exit", func(t *testing.T) {
		// n × len(s) over a string parameter s and a count n.
		fn := function(t, []types.Type{types.TypeString(), types.TypeI32()}, []types.Type{types.TypeI32(), types.TypeI32()}, func(b *instr.Builder) {
			header, done := b.Label(), b.Label()
			b.Bind(header)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_LEN).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Br(header)
			b.Bind(done).Emit(instr.LOCAL_GET, 3).Emit(instr.RETURN)
		})
		heap := []types.Value{nil, types.String("héllo")}
		stack := []types.Boxed{types.BoxRef(1), types.BoxI32(5), 0, 0}
		// Each LOCAL_GET retains s and its string.len releases it; RETURN
		// releases the parameter once. A count of 2 keeps every release
		// short of the last reference.
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(5*6), stack[0])
		require.Equal(t, 1, rc[1])
	})

	t.Run("deopts string.len of a non-string as the operation's own trap", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeString()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		})
		heap := []types.Value{nil, types.TypedArray[int32]{1}}
		stack := []types.Boxed{types.BoxRef(1)}
		rc := []int{0, 2}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.True(t, exit.Trap)
	})

	t.Run("retains a ref element a guarded array.get reads, through Context.RC", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeAny())}, Returns: []types.Type{types.TypeAny()}},
			Code: instr.Marshal(insts),
		}

		array := types.NewArray(nil, types.NewArrayType(types.TypeAny()), 0)
		array.Append(types.BoxRef(2))
		heap := []types.Value{nil, array, nil}
		rc := []int{0, 2, 1}
		stack := []types.Boxed{types.BoxRef(1)}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxRef(2), stack[0])
		require.Equal(t, 2, rc[2])
	})

	t.Run("reads and writes a struct field through its resolved record", func(t *testing.T) {
		record := types.NewStructType(types.NewStructField(types.TypeI32(), types.FieldWithName("x")))
		b := instr.NewBuilder()
		b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).
			Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).
			Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).
			Emit(instr.STRUCT_SET).
			Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).
			Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32()}},
			Code: instr.Marshal(insts),
		}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Struct: record}},
		}
		f, _, err := transform.Translate(m, nil, 1, fn, 0)
		require.NoError(t, err)

		s := types.NewStruct(nil, record, types.BoxI32(5))
		heap := []types.Value{nil, nil, s}
		stack := make([]types.Boxed, 4)
		code, _ := lower(t, arm64.New(), f, fn, m.Objects, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(6), stack[0])
		require.Equal(t, uint64(6), s.Raw(0))
	})

	t.Run("struct.set stores an i8 field sign-extended to 32 bits and zero-extended to 64, matching Struct.SetField", func(t *testing.T) {
		// types.(*Struct).SetField for KindI8 computes
		// uint64(uint32(int32(val.I8()))): sign-extend to 32 bits, then
		// zero-extend to 64. A raw 64-bit sign extension (SBFX into a
		// 64-bit destination) diverges for any negative int8.
		record := types.NewStructType(types.NewStructField(types.TypeI8(), types.FieldWithName("x")))
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(1)), Results: []ssa.Value{ref}})
		guarded := b.Value(ssa.TypeRef)
		guardState := state(b, entry, ssa.Operand{Value: ref})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct}, Args: []ssa.Value{ref}, State: guardState, Results: []ssa.Value{guarded}})
		idx := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{idx}})
		val := b.Value(ssa.TypeI8)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0xFFFFFFFF, Results: []ssa.Value{val}})
		at := state(b, entry, ssa.Operand{Value: guarded}, ssa.Operand{Value: idx}, ssa.Operand{Value: val})
		b.Add(entry, ssa.Operation{Op: ssa.OpStructSet, Args: []ssa.Value{guarded, idx, val}, State: at})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn})

		s := types.NewStruct(nil, record, types.BoxI32(0))
		heap := []types.Value{nil, s}
		stack := []types.Boxed{0}
		code, _ := lower(t, arm64.New(), b.Build(), frame(nil, nil), nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		// int8(-1) -> int32(-1) -> uint32(0xFFFFFFFF) -> uint64(0x00000000FFFFFFFF).
		require.Equal(t, uint64(0xFFFFFFFF), s.Raw(0))
	})

	t.Run("deopts a struct.set whose value kind differs from the field's declared kind", func(t *testing.T) {
		// Threaded SetField converts by the declared kind: i32 2 into an i1 field stores 1.
		record := types.NewStructType(types.NewStructField(types.TypeI1(), types.FieldWithName("x")))
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(1)), Results: []ssa.Value{ref}})
		guarded := b.Value(ssa.TypeRef)
		guardState := state(b, entry, ssa.Operand{Value: ref})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct}, Args: []ssa.Value{ref}, State: guardState, Results: []ssa.Value{guarded}})
		idx := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{idx}})
		val := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{val}})
		at := state(b, entry, ssa.Operand{Value: guarded}, ssa.Operand{Value: idx}, ssa.Operand{Value: val})
		b.Add(entry, ssa.Operation{Op: ssa.OpStructSet, Args: []ssa.Value{guarded, idx, val}, State: at})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn})

		s := types.NewStruct(nil, record, types.BoxI1(false))
		heap := []types.Value{nil, s}
		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, nil), nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, jit.ExitDeopt, exits[ctx.Exit()].Kind)
		require.Equal(t, uint64(0), s.Raw(0))
	})

	t.Run("sorts a typed i32 array in place via a guarded insertion-sort loop", func(t *testing.T) {
		b := instr.NewBuilder()
		outerHeader, outerDone := b.Label(), b.Label()
		innerHeader, innerDone := b.Label(), b.Label()

		b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 1) // i = 1
		b.Bind(outerHeader)
		b.Emit(instr.LOCAL_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN)
		b.Emit(instr.I32_GE_S).BrIf(outerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.LOCAL_SET, 3) // key = a[i]
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 2)   // j = i - 1
		b.Bind(innerHeader)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 0).Emit(instr.I32_LT_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.I32_LE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.ARRAY_GET)
		b.Emit(instr.ARRAY_SET) // a[j+1] = a[j]
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 2)
		b.Br(innerHeader)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 3)
		b.Emit(instr.ARRAY_SET) // a[j+1] = key
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(outerHeader)
		b.Bind(outerDone)
		b.Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32())}},
			Locals: []types.Type{types.TypeI32(), types.TypeI32(), types.TypeI32()},
			Code:   instr.Marshal(insts),
		}

		heap := []types.Value{nil, types.TypedArray[int32]{5, 3, 9, 1, 7, 2}}
		stack := []types.Boxed{types.BoxRef(1)}
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.TypedArray[int32]{1, 2, 3, 5, 7, 9}, heap[1])
	})

	t.Run("array.set stores a 4-byte i32 element without clobbering its neighbor", func(t *testing.T) {
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, uint64(0xFFFFFFFF)).Emit(instr.ARRAY_SET)
			b.Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		})

		heap := []types.Value{nil, types.TypedArray[int32]{0, 42}}
		stack := []types.Boxed{types.BoxRef(1)}
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.TypedArray[int32]{-1, 42}, heap[1])
	})

	t.Run("array.set stores a 4-byte f32 element without clobbering its neighbor", func(t *testing.T) {
		fn := function(t, []types.Type{types.NewArrayType(types.TypeF32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.F32_CONST, uint64(math.Float32bits(-1.5))).Emit(instr.ARRAY_SET)
			b.Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		})

		heap := []types.Value{nil, types.TypedArray[float32]{0, 42.5}}
		stack := []types.Boxed{types.BoxRef(1)}
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.TypedArray[float32]{-1.5, 42.5}, heap[1])
	})

	t.Run("array.set narrows an i32 value into a []i1 element, matching threaded array.get", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI1())}, Returns: []types.Type{types.TypeI1()}},
			Code: instr.Marshal(insts),
		}

		heap := []types.Value{nil, types.TypedArray[bool]{false}}
		stack := []types.Boxed{types.BoxRef(1)}
		rc := []int{0, 2}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI1(true), stack[0])
	})

	t.Run("deopts an out-of-bounds array.get, matching threaded ErrIndexOutOfRange", func(t *testing.T) {
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
		})

		heap := []types.Value{nil, types.TypedArray[int32]{1, 2}}
		stack := []types.Boxed{types.BoxRef(1)}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, jit.ExitDeopt, exits[ctx.Exit()].Kind)
	})

	t.Run("enters at a loop header, loading its operand-stack parameter and leaving other locals unset by the prologue", func(t *testing.T) {
		// acc lives on the operand stack across the header (never stored to
		// a local), n and bonus are locals: n is this function's own
		// parameter, bonus an ordinary local an entry-0 prologue would clear.
		fn := function(t, []types.Type{types.TypeI32()}, []types.Type{types.TypeI32(), types.TypeI32()}, func(b *instr.Builder) {
			header, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(header)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(header)
			b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.RETURN)
		})
		entry := instr.MustNew(instr.I32_CONST, 0).Width()*2 + instr.MustNew(instr.LOCAL_SET, 1).Width()
		f, _, err := transform.Translate(transform.Module{}, nil, 1, fn, entry)
		require.NoError(t, err)
		require.Len(t, f.Block(0).Params, 1)

		code, _ := lower(t, arm64.New(), f, fn, nil, 1, true)
		// n=5, i=3 (partway through the loop), bonus=100: acc starts at 3
		// (the value the interpreter left on the operand stack, slot 3).
		stack := []types.Boxed{types.BoxI32(5), types.BoxI32(3), types.BoxI32(100), types.BoxI32(3)}
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		// A cleared bonus (an entry-0 prologue's mistake here) would return 2.
		require.Equal(t, types.BoxI32(105), stack[0])
		require.Zero(t, ctx.Level())
	})

	t.Run("deopts an OSR entry's own body with the right IP and values, locals left as the interpreter set them", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32()}, []types.Type{types.TypeI32()}, func(b *instr.Builder) {
			join := b.Label()
			b.Emit(instr.LOCAL_GET, 0).BrIf(join)
			b.Bind(join)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		entry := instr.MustNew(instr.LOCAL_GET, 0).Width() + instr.MustNew(instr.BR_IF, 0).Width()
		f, _, err := transform.Translate(transform.Module{}, nil, 1, fn, entry)
		require.NoError(t, err)
		require.Empty(t, f.Block(0).Params)

		code, exits := lower(t, arm64.New(), f, fn, nil, 1, true)

		ok := []types.Boxed{types.BoxI32(6), types.BoxI32(3)}
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, ok)))
		require.Equal(t, types.BoxI32(2), ok[0])

		bad := []types.Boxed{types.BoxI32(6), types.BoxI32(0)}
		ctx := enter(t, bad)
		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.Equal(t, entry+instr.MustNew(instr.LOCAL_GET, 0).Width()*2, exit.Frame.IP)
		require.Equal(t, []uint64{6, 0}, operands(ctx, exit.Frame))
	})

	t.Run("completes module code from a header entry", func(t *testing.T) {
		b := instr.NewBuilder()
		join := b.Label()
		b.Emit(instr.I32_CONST, 1).BrIf(join)
		b.Bind(join).Emit(instr.I32_CONST, 42)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Code: instr.Marshal(code)}

		entry := instr.MustNew(instr.I32_CONST, 0).Width() + instr.MustNew(instr.BR_IF, 0).Width()
		f, _, err := transform.Translate(transform.Module{}, nil, 0, fn, entry)
		require.NoError(t, err)
		require.Empty(t, f.Block(0).Params)

		native, exits := lower(t, arm64.New(), f, fn, nil, 0, true)
		require.Empty(t, exits)
		stack := []types.Boxed{0}
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapReturn, jit.Enter(native, ctx))
		require.Equal(t, types.BoxI32(42), stack[0])
	})

	t.Run("counts an entry at an address beyond the load/store imm12 range", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32(), types.TypeI32()}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(1), types.BoxI32(2)}
		const at = 4096 // 8*at exceeds the unsigned-offset LDR/STR imm12 (0xFFF) scaled range
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, at, false)

		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.FB = address(t, stack)
		ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
		ctx.SetEdge(len(ctx.Records))
		ctx.Budget = 1000
		entries := make([]int64, at+1)
		ctx.Entries = address(t, entries)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(3), stack[0])
		require.Equal(t, int64(1), entries[at])
	})
}

func TestMachine_Arch(t *testing.T) {
	require.Equal(t, target.New(), arm64.New().Arch())
}

func TestMachine_Reserve(t *testing.T) {
	t.Run("reserves the runtime and Go registers", func(t *testing.T) {
		require.Equal(t, target.X26, arm64.Ctx)
		require.Equal(t, []asm.PReg{target.X16, target.X17, target.X24, target.X25, target.X26, target.X27, target.X28}, arm64.New().Reserve())
	})

	t.Run("preserves the allocatable register bank and preference order", func(t *testing.T) {
		m := arm64.New()
		reserved := m.Reserve()
		var available []asm.PReg
		for _, r := range target.New().Registers(asm.RegTypeInt) {
			if !slices.Contains(reserved, r) {
				available = append(available, r)
			}
		}
		require.Equal(t, []asm.PReg{
			target.X0, target.X1, target.X2, target.X3, target.X4, target.X5, target.X6, target.X7,
			target.X8, target.X9, target.X10, target.X11, target.X12, target.X13, target.X14, target.X15,
			target.X19, target.X20, target.X21, target.X22, target.X23,
		}, available)
		for _, r := range target.New().Registers(asm.RegTypeFloat) {
			require.NotContains(t, reserved, r)
		}
	})

	t.Run("returns independently owned reservations", func(t *testing.T) {
		m := arm64.New()
		reserved := m.Reserve()
		reserved[0] = target.X0
		require.NotContains(t, m.Reserve(), target.X0)
		require.Contains(t, m.Reserve(), target.X16)
	})
}

func TestMachine_Prologue(t *testing.T) {
	t.Run("counts entry and starts locals at their zeros", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Prologue(a, 3, true, compile.Layout{Kinds: []types.Kind{types.KindI32, types.KindI64, types.KindRef}, Zeros: []types.Boxed{types.BoxI64(0), types.BoxedNull}}, nil)
		require.Equal(t, []asm.Instruction{
			target.ADDI(target.X16, target.SP, 0),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			{Op: uint16(target.OpSUBI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
			target.STP(target.X25, target.X16, target.X27, int16(jit.RecordFB)),
			target.STR(target.LR, target.X27, int16(jit.RecordPC)),
			target.ADDI(target.X27, target.X27, uint16(jit.SizeofRecord)),
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetEntries)),
			target.LDR(target.X17, target.X16, 24),
			target.ADDI(target.X17, target.X17, 1),
			target.STR(target.X17, target.X16, 24),
			target.MOVZ(target.X16, 0x7ff4, 48),
			target.STR(target.X16, target.X25, 8),
			target.MOVZ(target.X16, 0x7ff6, 48),
			target.STR(target.X16, target.X25, 16),
		}, a.Rows())
	})

	t.Run("loads each distinct zero once and stores the zero word from XZR", func(t *testing.T) {
		a := asm.New(target.New())
		layout := compile.Layout{
			Kinds: []types.Kind{types.KindI32, types.KindI32, types.KindF64, types.KindI32, types.KindF64},
			Zeros: []types.Boxed{types.BoxI32(0), types.BoxF64(0), types.BoxI32(0), types.BoxF64(0)},
		}
		arm64.New().Prologue(a, 0, false, layout, nil)
		require.Equal(t, []asm.Instruction{
			target.MOVZ(target.X16, 0x7ff8, 48),
			target.STR(target.X16, target.X25, 8),
			target.STR(target.X16, target.X25, 24),
			target.STR(target.XZR, target.X25, 16),
			target.STR(target.XZR, target.X25, 32),
		}, a.Rows()[7:])
	})

	t.Run("counts entry through a register when the offset exceeds imm12", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Prologue(a, 4096, true, compile.Layout{Kinds: []types.Kind{types.KindI32, types.KindI64, types.KindRef}, Zeros: []types.Boxed{types.BoxI64(0), types.BoxedNull}}, nil)
		require.Equal(t, []asm.Instruction{
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetEntries)),
			target.MOVZ(target.X17, 0x8000, 0),
			target.ADD(target.X16, target.X16, target.X17),
			target.LDR(target.X17, target.X16, 0),
			target.ADDI(target.X17, target.X17, 1),
			target.STR(target.X17, target.X16, 0),
			target.MOVZ(target.X16, 0x7ff4, 48),
			target.STR(target.X16, target.X25, 8),
			target.MOVZ(target.X16, 0x7ff6, 48),
			target.STR(target.X16, target.X25, 16),
		}, a.Rows()[7:])
	})

	t.Run("skips entry count", func(t *testing.T) {
		a := asm.New(target.New())
		arm64.New().Prologue(a, 3, false, compile.Layout{Kinds: []types.Kind{types.KindI32, types.KindI64, types.KindRef}, Zeros: []types.Boxed{types.BoxI64(0), types.BoxedNull}}, nil)
		require.Len(t, a.Rows(), 11)
		require.NotContains(t, a.Rows(), target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetEntries)))
	})

	t.Run("moves register-passed parameters into their registers once the frame is built", func(t *testing.T) {
		a := asm.New(target.New())
		args := []asm.VReg{asm.NewVReg(1, asm.RegTypeInt, asm.Width32), asm.NewVReg(2, asm.RegTypeInt, asm.Width64)}
		arm64.New().Prologue(a, 0, false, compile.Layout{Kinds: []types.Kind{types.KindI32, types.KindRef}, Arguments: []types.Kind{types.KindI32, types.KindRef}}, args)
		require.Equal(t, []asm.Instruction{
			target.DEF(target.X0),
			target.DEF(target.X1),
			target.MOVW(args[0], target.X0),
			target.MOV(args[1], target.X1),
		}, a.Rows()[7:])
	})

	t.Run("loads the upvals base after the register-passed parameters", func(t *testing.T) {
		a := asm.New(target.New())
		args := []asm.VReg{asm.NewVReg(1, asm.RegTypeInt, asm.Width32)}
		arm64.New().Prologue(a, 0, false, compile.Layout{Kinds: []types.Kind{types.KindI32}, Arguments: []types.Kind{types.KindI32}, Upvals: true}, args)
		require.Equal(t, []asm.Instruction{
			target.DEF(target.X0),
			target.MOVW(args[0], target.X0),
			target.LDR(vr(1), arm64.Ctx, int16(jit.OffsetUpvals)),
		}, a.Rows()[7:])
	})

	t.Run("moves a float register-passed parameter by FMOV and an i64 by a raw 64-bit MOV", func(t *testing.T) {
		for _, c := range []struct {
			kinds []types.Kind
			args  []asm.VReg
			move  func(dst, src asm.Reg) asm.Instruction
		}{
			{
				[]types.Kind{types.KindF32, types.KindF64},
				[]asm.VReg{asm.NewVReg(1, asm.RegTypeFloat, asm.Width32), asm.NewVReg(2, asm.RegTypeFloat, asm.Width64)},
				target.FMOV,
			},
			{
				[]types.Kind{types.KindI64, types.KindRef},
				[]asm.VReg{asm.NewVReg(1, asm.RegTypeInt, asm.Width64), asm.NewVReg(2, asm.RegTypeInt, asm.Width64)},
				target.MOV,
			},
		} {
			a := asm.New(target.New())
			arm64.New().Prologue(a, 0, false, compile.Layout{Kinds: c.kinds, Arguments: c.kinds}, c.args)
			require.Equal(t, []asm.Instruction{
				target.DEF(target.X0),
				target.DEF(target.X1),
				c.move(c.args[0], target.X0),
				c.move(c.args[1], target.X1),
			}, a.Rows()[7:])
		}
	})
}

func TestMachine_Epilogue(t *testing.T) {
	pop := []asm.Instruction{
		target.SUBI(target.X27, target.X27, uint16(jit.SizeofRecord)),
		{Op: uint16(target.OpADDI), Dst: asm.Physical(target.SP), Src1: asm.Physical(target.SP), Src2: asm.Slots()},
		target.LDR(target.LR, target.SP, 8),
		target.ADDI(target.SP, target.SP, 16),
		target.RET(),
	}

	m, a := arm64.New(), asm.New(target.New())
	m.Prologue(a, 0, true, compile.Layout{}, nil)
	start := len(a.Rows())
	m.Epilogue(a)
	require.Equal(t, pop, a.Rows()[start:])
}

func TestMachine_Enter(t *testing.T) {
	t.Run("loads the pinned registers, calls the body, and returns with no results", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		m.Epilogue(a)
		start := len(a.Rows())
		label := m.Enter(a, compile.Layout{})

		entry := asm.Label(1)
		require.Equal(t, []asm.Instruction{
			target.LDR(target.X25, arm64.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, arm64.Ctx, int16(jit.OffsetNext)),
			target.LDR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(entry),
			target.STR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}, a.Rows()[start:])

		code, err := a.Build()
		require.NoError(t, err)
		off, ok := a.Offset(label)
		require.True(t, ok)
		require.Equal(t, start*4, off)
		require.Len(t, code, len(a.Rows())*4)
	})

	t.Run("boxes results by kind: a narrow one by tag in slot 0, a ref raw in slot 1, a wide one raw", func(t *testing.T) {
		head := []asm.Instruction{
			target.LDR(target.X25, arm64.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, arm64.Ctx, int16(jit.OffsetNext)),
			target.LDR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.STR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
		}
		tail := []asm.Instruction{
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}
		for _, c := range []struct {
			results []types.Kind
			body    []asm.Instruction
		}{
			{[]types.Kind{types.KindI32, types.KindRef}, slices.Concat(
				[]asm.Instruction{target.UXTW(target.X16, target.X0)},
				target.LDI(target.X17, types.Tag(types.KindI32)),
				[]asm.Instruction{
					target.ORR(target.X16, target.X16, target.X17),
					target.STR(target.X16, target.X25, 0),
					target.STR(target.X1, target.X25, 8),
				},
			)},
			{[]types.Kind{types.KindF64}, slices.Concat(
				[]asm.Instruction{target.MOV(target.X16, target.X0)},
				canonical(),
				[]asm.Instruction{target.STR(target.X16, target.X25, 0)},
			)},
			{[]types.Kind{types.KindI64}, []asm.Instruction{target.STR(target.X0, target.X25, 0)}},
		} {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, 0, true, compile.Layout{}, nil)
			m.Epilogue(a)
			start := len(a.Rows())
			m.Enter(a, compile.Layout{Registers: c.results})
			require.Equal(t, slices.Concat(head, c.body, tail), a.Rows()[start:])
		}
	})

	t.Run("loads and unboxes register-passed parameters from their slots before the body", func(t *testing.T) {
		pin := []asm.Instruction{
			target.LDR(target.X25, arm64.Ctx, int16(jit.OffsetFB)),
			target.LDR(target.X27, arm64.Ctx, int16(jit.OffsetNext)),
			target.LDR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
		}
		call := []asm.Instruction{
			target.SUBI(target.SP, target.SP, 16),
			target.STR(target.LR, target.SP, 8),
			target.BLLabel(1),
			target.STR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
			target.LDR(target.LR, target.SP, 8),
			target.ADDI(target.SP, target.SP, 16),
			target.RET(),
		}
		narrow := []asm.Instruction{
			target.LDR(asm.NewPReg(target.X0.ID(), asm.RegTypeInt, asm.Width32), target.X25, 0),
			target.LDR(target.X1, target.X25, 8),
		}
		for _, c := range []struct {
			arguments []types.Kind
			loads     []asm.Instruction
		}{
			{[]types.Kind{types.KindI32, types.KindRef}, narrow},
			{[]types.Kind{types.KindF32, types.KindF64}, narrow},
			{[]types.Kind{types.KindI64}, []asm.Instruction{
				target.LDR(target.X0, target.X25, 0),
				target.SBFX(target.X0, target.X0, 0, 49),
			}},
		} {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, 0, true, compile.Layout{}, nil)
			m.Epilogue(a)
			start := len(a.Rows())
			m.Enter(a, compile.Layout{Arguments: c.arguments})
			require.Equal(t, slices.Concat(pin, c.loads, call), a.Rows()[start:])
		}
	})
}

func TestMachine_Lower(t *testing.T) {
	type emit2 = func(dst, src asm.Reg) asm.Instruction
	type emit3 = func(dst, src1, src2 asm.Reg) asm.Instruction
	type test struct {
		name string
		regs regs
		// layout is the function shape the prologue before op starts.
		layout compile.Layout
		// guard, when set, lowers a shape guard first, unmeasured, so op's
		// own rows can assume it already ran.
		guard *ssa.Operation
		// slice, then bound, when set, lower after guard, unmeasured.
		slice, bound *ssa.Operation
		// consts are the values an OpConst defines, by word.
		consts map[ssa.Value]uint64
		// objects are the facts the module knows of constant references.
		objects map[ssa.Value]transform.Object
		op      ssa.Operation
		// fuse lowers op through a Site that fuses its result into the
		// branch right after it.
		fuse  bool
		rows  []asm.Instruction
		lower bool
	}
	i32, i64, f32, f64 := ssa.TypeI32, ssa.TypeI64, ssa.TypeF32, ssa.TypeF64
	reg := func(typ ssa.Type, v ssa.Value) asm.VReg { return regs{v: typ}.Reg(v) }
	exec := func(op ssa.Op, cond ssa.Cond, args ...ssa.Value) ssa.Operation {
		return ssa.Operation{Op: op, Cond: cond, Args: args, Results: []ssa.Value{ssa.Value(len(args) + 1)}}
	}
	boxed := func(k types.Kind, rows ...asm.Instruction) []asm.Instruction {
		rows = append(rows, target.LDI(target.X17, types.Tag(k))...)
		return append(rows, target.ORR(target.X16, target.X16, target.X17))
	}
	// cell is the tail of a scalar ref.test or ref.cast: a word of no inline
	// match must refer to a heap cell of value's type, else it misses.
	cell := func(value types.Value, miss asm.Label) []asm.Instruction {
		rows := target.LDI(target.X17, types.Tag(types.KindRef)>>49)
		rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss),
			target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(1), target.X16, vr(1)),
			target.LDR(target.X16, vr(1), 0))
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(value)))...)
		return append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss))
	}
	local := func(i int) ssa.Slot { return ssa.Slot{Space: ssa.SpaceLocal, Index: i} }
	globals := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	old := asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)
	word, prior := old, asm.NewVReg(-4, asm.RegTypeInt, asm.Width64)
	heap := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
	counter := []asm.Instruction{
		target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetRC)),
		target.LSLI(target.X17, target.X17, 3),
		target.ADD(target.X16, target.X16, target.X17),
		target.LDR(target.X17, target.X16, 0),
	}

	// strcmp is the rows of the shared string comparison over the sizes and data
	// pointers size and ptr, with rest the byte count left: equality relations
	// leave early on unequal sizes; the flags are those of the first differing
	// word or byte, else of the sizes.
	strcmp := func(size, ptr [2]asm.VReg, rest asm.VReg, out, tail, step, words, bytes asm.Label, eq bool) []asm.Instruction {
		rows := []asm.Instruction{target.CMP(size[0], size[1])}
		if eq {
			rows = append(rows, target.BCondLabel(target.OpBNE, out))
		}
		return append(rows,
			target.CSEL(rest, size[0], size[1], target.CondCC),
			target.CMP(ptr[0], ptr[1]), target.BCondLabel(target.OpBEQ, tail),
			target.CMPI(rest, 8), target.BCondLabel(target.OpBCC, bytes),
			target.LDR(target.X16, ptr[0], 0), target.LDR(target.X17, ptr[1], 0),
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, step),
			target.REV(target.X16, target.X16), target.REV(target.X17, target.X17),
			target.CMP(target.X16, target.X17), target.BLabel(out),
			target.ADDI(ptr[0], ptr[0], 8), target.ADDI(ptr[1], ptr[1], 8),
			target.SUBI(rest, rest, 8), target.BLabel(words),
			target.CBZLabel(rest, tail),
			target.LDRB(target.X16, ptr[0], 0), target.LDRB(target.X17, ptr[1], 0),
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, out),
			target.ADDI(ptr[0], ptr[0], 1), target.ADDI(ptr[1], ptr[1], 1),
			target.SUBI(rest, rest, 1), target.BLabel(bytes),
			target.CMP(size[0], size[1]),
		)
	}
	// text is the rows that check a ref is a String, branching to miss when it is
	// not, and load the String's data pointer and size through the registers addr
	// (its heap slot), data, size, and ptr.
	text := func(ref asm.Reg, addr, data, size, ptr asm.VReg, miss asm.Label) []asm.Instruction {
		rows := []asm.Instruction{
			target.SBFX(addr, ref, 0, 32), target.LSLI(addr, addr, 4),
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
			target.ADD(addr, target.X16, addr),
			target.LDR(target.X16, addr, 0),
		}
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(types.String(""))))...)
		return append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss),
			target.LDR(data, addr, int16(jit.OffsetData)),
			target.LDR(size, data, int16(jit.OffsetStringLen)),
			target.LDR(ptr, data, 0),
		)
	}
	// stringCompare is the rows the six string relations lower to: cond reads
	// the flags of the comparison.
	stringCompare := func(cond uint8, eq bool) []asm.Instruction {
		const out, tail, step, words, bytes = 2, 3, 4, 5, 6
		var rows []asm.Instruction
		for i, addr := range []int{1, 5} {
			rows = append(rows, text(reg(ssa.TypeRef, ssa.Value(i+1)), vr(addr), vr(addr+1), vr(addr+2), vr(addr+3), exit)...)
		}
		rows = append(rows, strcmp([2]asm.VReg{vr(3), vr(7)}, [2]asm.VReg{vr(4), vr(8)}, vr(9), out, tail, step, words, bytes, eq)...)
		return append(rows, target.CSET(reg(ssa.TypeI1, 3), cond))
	}
	binIndex, farIndex := 3, 5000
	var tests []test
	for _, c := range []struct {
		op   ssa.Op
		cond ssa.Cond
		typ  ssa.Type
		emit emit3
	}{
		{ssa.OpIntAdd, 0, i32, target.ADD}, {ssa.OpIntSub, 0, i32, target.SUB}, {ssa.OpIntMul, 0, i32, target.MUL},
		{ssa.OpIntAnd, 0, i32, target.AND}, {ssa.OpIntOr, 0, i32, target.ORR}, {ssa.OpIntXor, 0, i32, target.EOR},
		{ssa.OpIntShl, 0, i32, target.LSL}, {ssa.OpIntAShr, 0, i32, target.ASR}, {ssa.OpIntLShr, 0, i32, target.LSR},
		{ssa.OpIntRotr, 0, i32, target.ROR},
		{ssa.OpIntAdd, 0, i64, target.ADD}, {ssa.OpIntSub, 0, i64, target.SUB}, {ssa.OpIntMul, 0, i64, target.MUL},
		{ssa.OpIntAnd, 0, i64, target.AND}, {ssa.OpIntOr, 0, i64, target.ORR}, {ssa.OpIntXor, 0, i64, target.EOR},
		{ssa.OpIntShl, 0, i64, target.LSL}, {ssa.OpIntAShr, 0, i64, target.ASR}, {ssa.OpIntLShr, 0, i64, target.LSR},
		{ssa.OpIntRotr, 0, i64, target.ROR},
		{ssa.OpFloatAdd, 0, f32, target.FADD}, {ssa.OpFloatSub, 0, f32, target.FSUB}, {ssa.OpFloatMul, 0, f32, target.FMUL},
		{ssa.OpFloatDiv, 0, f32, target.FDIV}, {ssa.OpFloatMin, 0, f32, target.FMIN}, {ssa.OpFloatMax, 0, f32, target.FMAX},
		{ssa.OpFloatAdd, 0, f64, target.FADD}, {ssa.OpFloatSub, 0, f64, target.FSUB}, {ssa.OpFloatMul, 0, f64, target.FMUL},
		{ssa.OpFloatDiv, 0, f64, target.FDIV}, {ssa.OpFloatMin, 0, f64, target.FMIN}, {ssa.OpFloatMax, 0, f64, target.FMAX},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ, 3: c.typ},
			op:   exec(c.op, c.cond, 1, 2),
			rows: []asm.Instruction{c.emit(reg(c.typ, 3), reg(c.typ, 1), reg(c.typ, 2))}, lower: true,
		})
	}
	for _, c := range []struct {
		op   ssa.Op
		cond ssa.Cond
		typ  ssa.Type
		flag uint8
	}{
		{ssa.OpRefCmp, ssa.CondEQ, ssa.TypeRef, target.CondEQ}, {ssa.OpRefCmp, ssa.CondNE, ssa.TypeRef, target.CondNE},
		{ssa.OpIntCmp, ssa.CondEQ, i32, target.CondEQ}, {ssa.OpIntCmp, ssa.CondNE, i32, target.CondNE},
		{ssa.OpIntCmp, ssa.CondSLT, i32, target.CondLT}, {ssa.OpIntCmp, ssa.CondULT, i32, target.CondCC},
		{ssa.OpIntCmp, ssa.CondSGT, i32, target.CondGT}, {ssa.OpIntCmp, ssa.CondUGT, i32, target.CondHI},
		{ssa.OpIntCmp, ssa.CondSLE, i32, target.CondLE}, {ssa.OpIntCmp, ssa.CondULE, i32, target.CondLS},
		{ssa.OpIntCmp, ssa.CondSGE, i32, target.CondGE}, {ssa.OpIntCmp, ssa.CondUGE, i32, target.CondCS},
		{ssa.OpIntCmp, ssa.CondEQ, i64, target.CondEQ}, {ssa.OpIntCmp, ssa.CondNE, i64, target.CondNE},
		{ssa.OpIntCmp, ssa.CondSLT, i64, target.CondLT}, {ssa.OpIntCmp, ssa.CondULT, i64, target.CondCC},
		{ssa.OpIntCmp, ssa.CondSGT, i64, target.CondGT}, {ssa.OpIntCmp, ssa.CondUGT, i64, target.CondHI},
		{ssa.OpIntCmp, ssa.CondSLE, i64, target.CondLE}, {ssa.OpIntCmp, ssa.CondULE, i64, target.CondLS},
		{ssa.OpIntCmp, ssa.CondSGE, i64, target.CondGE}, {ssa.OpIntCmp, ssa.CondUGE, i64, target.CondCS},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ, 3: ssa.TypeI1},
			op:   exec(c.op, c.cond, 1, 2),
			rows: []asm.Instruction{target.CMP(reg(c.typ, 1), reg(c.typ, 2)), target.CSET(reg(ssa.TypeI1, 3), c.flag)}, lower: true,
		})
	}
	for _, c := range []struct {
		op   ssa.Op
		cond ssa.Cond
		typ  ssa.Type
		flag uint8
	}{
		{ssa.OpFloatCmp, ssa.CondOEQ, f32, target.CondEQ}, {ssa.OpFloatCmp, ssa.CondUNE, f32, target.CondNE},
		{ssa.OpFloatCmp, ssa.CondOLT, f32, target.CondMI}, {ssa.OpFloatCmp, ssa.CondOGT, f32, target.CondGT},
		{ssa.OpFloatCmp, ssa.CondOLE, f32, target.CondLS}, {ssa.OpFloatCmp, ssa.CondOGE, f32, target.CondGE},
		{ssa.OpFloatCmp, ssa.CondOEQ, f64, target.CondEQ}, {ssa.OpFloatCmp, ssa.CondUNE, f64, target.CondNE},
		{ssa.OpFloatCmp, ssa.CondOLT, f64, target.CondMI}, {ssa.OpFloatCmp, ssa.CondOGT, f64, target.CondGT},
		{ssa.OpFloatCmp, ssa.CondOLE, f64, target.CondLS}, {ssa.OpFloatCmp, ssa.CondOGE, f64, target.CondGE},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ, 3: ssa.TypeI1},
			op:   exec(c.op, c.cond, 1, 2),
			rows: []asm.Instruction{target.FCMP(reg(c.typ, 1), reg(c.typ, 2)), target.CSET(reg(ssa.TypeI1, 3), c.flag)}, lower: true,
		})
	}
	for _, c := range []struct {
		op       ssa.Op
		cond     ssa.Cond
		from, to ssa.Type
		emit     emit2
	}{
		{ssa.OpIntSExt8, 0, i32, i32, target.SXTB}, {ssa.OpIntSExt16, 0, i32, i32, target.SXTH},
		{ssa.OpIntClz, 0, i32, i32, target.CLZ}, {ssa.OpIntClz, 0, i64, i64, target.CLZ},
		{ssa.OpIntSExt8, 0, i64, i64, target.SXTB}, {ssa.OpIntSExt16, 0, i64, i64, target.SXTH},
		{ssa.OpIntSExt32, 0, i64, i64, target.SXTW},
		{ssa.OpFloatAbs, 0, f32, f32, target.FABS}, {ssa.OpFloatNeg, 0, f32, f32, target.FNEG}, {ssa.OpFloatSqrt, 0, f32, f32, target.FSQRT},
		{ssa.OpFloatCeil, 0, f32, f32, target.FRINTP}, {ssa.OpFloatFloor, 0, f32, f32, target.FRINTM},
		{ssa.OpFloatTrunc, 0, f32, f32, target.FRINTZ}, {ssa.OpFloatNearest, 0, f32, f32, target.FRINTN},
		{ssa.OpFloatAbs, 0, f64, f64, target.FABS}, {ssa.OpFloatNeg, 0, f64, f64, target.FNEG}, {ssa.OpFloatSqrt, 0, f64, f64, target.FSQRT},
		{ssa.OpFloatCeil, 0, f64, f64, target.FRINTP}, {ssa.OpFloatFloor, 0, f64, f64, target.FRINTM},
		{ssa.OpFloatTrunc, 0, f64, f64, target.FRINTZ}, {ssa.OpFloatNearest, 0, f64, f64, target.FRINTN},
		{ssa.OpIntSExt, 0, i32, i64, target.SXTW}, {ssa.OpIntZExt, 0, i32, i64, target.UXTW},
		{ssa.OpIntWrap, 0, i64, i32, target.MOVW},
		{ssa.OpIntSIToFP, 0, i32, f32, target.SCVTF}, {ssa.OpIntUIToFP, 0, i32, f32, target.UCVTF},
		{ssa.OpIntSIToFP, 0, i32, f64, target.SCVTF}, {ssa.OpIntUIToFP, 0, i32, f64, target.UCVTF},
		{ssa.OpIntSIToFP, 0, i64, f32, target.SCVTF}, {ssa.OpIntUIToFP, 0, i64, f32, target.UCVTF},
		{ssa.OpIntSIToFP, 0, i64, f64, target.SCVTF}, {ssa.OpIntUIToFP, 0, i64, f64, target.UCVTF},
		{ssa.OpFloatFPToSISat, 0, f32, i32, target.FCVTZS}, {ssa.OpFloatFPToUISat, 0, f32, i32, target.FCVTZU},
		{ssa.OpFloatFPToSISat, 0, f32, i64, target.FCVTZS}, {ssa.OpFloatFPToUISat, 0, f32, i64, target.FCVTZU},
		{ssa.OpFloatFPToSISat, 0, f64, i32, target.FCVTZS}, {ssa.OpFloatFPToUISat, 0, f64, i32, target.FCVTZU},
		{ssa.OpFloatFPToSISat, 0, f64, i64, target.FCVTZS}, {ssa.OpFloatFPToUISat, 0, f64, i64, target.FCVTZU},
		{ssa.OpFloatFPExt, 0, f32, f64, target.FCVT}, {ssa.OpFloatFPTrunc, 0, f64, f32, target.FCVT},
		{ssa.OpIntBitcast, 0, f32, i32, target.FMOV}, {ssa.OpFloatBitcast, 0, i32, f32, target.FMOV},
		{ssa.OpIntBitcast, 0, f64, i64, target.FMOV}, {ssa.OpFloatBitcast, 0, i64, f64, target.FMOV},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.from, 2: c.to},
			op:   exec(c.op, c.cond, 1),
			rows: []asm.Instruction{c.emit(reg(c.to, 2), reg(c.from, 1))}, lower: true,
		})
	}
	overflowing := func(op ssa.Op) ssa.Operation {
		return ssa.Operation{Op: op, Args: []ssa.Value{1, 2}, Results: []ssa.Value{3, 4}}
	}
	for _, typ := range []ssa.Type{i32, i64} {
		x, y, dst, flag := reg(typ, 1), reg(typ, 2), reg(typ, 3), reg(ssa.TypeI1, 4)
		signedProduct := []asm.Instruction{
			target.SMULH(target.X16, x, y), target.MUL(dst, x, y), target.ASRI(target.X17, dst, 63),
			target.CMP(target.X16, target.X17), target.CSET(flag, target.CondNE),
		}
		unsignedProduct := []asm.Instruction{
			target.UMULH(target.X16, x, y), target.MUL(dst, x, y), target.CMPI(target.X16, 0), target.CSET(flag, target.CondNE),
		}
		if typ == i32 {
			signedProduct = []asm.Instruction{
				target.SMULL(target.X16, x, y), target.SXTW(target.X17, target.W16),
				target.CMP(target.X16, target.X17), target.MOVW(dst, target.X16), target.CSET(flag, target.CondNE),
			}
			unsignedProduct = []asm.Instruction{
				target.UMULL(target.X16, x, y), target.UXTW(target.X17, target.W16),
				target.CMP(target.X16, target.X17), target.MOVW(dst, target.X16), target.CSET(flag, target.CondNE),
			}
		}
		for _, c := range []struct {
			op   ssa.Op
			rows []asm.Instruction
		}{
			{ssa.OpIntSAddOvf, []asm.Instruction{target.ADDS(dst, x, y), target.CSET(flag, target.CondVS)}},
			{ssa.OpIntSSubOvf, []asm.Instruction{target.SUBS(dst, x, y), target.CSET(flag, target.CondVS)}},
			{ssa.OpIntSMulOvf, signedProduct},
			{ssa.OpIntUAddOvf, []asm.Instruction{target.ADDS(dst, x, y), target.CSET(flag, target.CondCS)}},
			{ssa.OpIntUSubOvf, []asm.Instruction{target.SUBS(dst, x, y), target.CSET(flag, target.CondCC)}},
			{ssa.OpIntUMulOvf, unsignedProduct},
		} {
			tests = append(tests, test{
				name: c.op.String() + " " + typ.String(),
				regs: regs{1: typ, 2: typ, 3: typ, 4: ssa.TypeI1},
				op:   overflowing(c.op),
				rows: c.rows, lower: true,
			})
		}
	}
	for _, op := range []ssa.Op{ssa.OpIntSAddOvf, ssa.OpIntSSubOvf, ssa.OpIntSMulOvf, ssa.OpIntUAddOvf, ssa.OpIntUSubOvf, ssa.OpIntUMulOvf} {
		tests = append(tests,
			test{name: op.String() + " declines mixed lanes", regs: regs{1: i32, 2: i64, 3: i32, 4: ssa.TypeI1}, op: overflowing(op)},
			test{name: op.String() + " declines a wide flag", regs: regs{1: i64, 2: i64, 3: i64, 4: i64}, op: overflowing(op)},
			test{name: op.String() + " declines a lone result", regs: regs{1: i32, 2: i32, 3: i32}, op: ssa.Operation{Op: op, Args: []ssa.Value{1, 2}, Results: []ssa.Value{3}}},
		)
	}
	for _, typ := range []ssa.Type{i32, i64} {
		tests = append(tests, test{
			name:   "int.cmp eq with zero " + typ.String(),
			regs:   regs{1: typ, 2: typ, 3: ssa.TypeI1},
			consts: map[ssa.Value]uint64{2: 0},
			op:     exec(ssa.OpIntCmp, ssa.CondEQ, 1, 2),
			rows:   []asm.Instruction{target.CMPI(reg(typ, 1), 0), target.CSET(reg(ssa.TypeI1, 3), target.CondEQ)}, lower: true,
		})
	}

	// immediates are add, sub and compare rows over a constant operand: the
	// constant becomes an immediate only when it fits imm12 at the op's width.
	for _, c := range []struct {
		name string
		typ  ssa.Type
		word uint64
		// code is the opcode lowered, with the constant second unless first.
		op    ssa.Op
		cond  ssa.Cond
		first bool
		fuse  bool
		rows  func(x, y asm.VReg) []asm.Instruction
	}{
		{"i32.add 7", i32, 7, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i32, 3), x, 7)} }},
		{"i32.add 0", i32, 0, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i32, 3), x, 0)} }},
		{"i32.add 4095", i32, 4095, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i32, 3), x, 4095)} }},
		{"i32.add 4096", i32, 4096, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i32, 3), x, y)} }},
		{"i32.add 1<<20", i32, 1 << 20, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i32, 3), x, y)} }},
		{"i32.add -1 wraps at 32 bits", i32, 0xFFFFFFFF, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUBI(reg(i32, 3), x, 1)} }},
		{"i32.add -4095", i32, 0xFFFFF001, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUBI(reg(i32, 3), x, 4095)} }},
		{"i32.add -4096", i32, 0xFFFFF000, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i32, 3), x, y)} }},
		{"i32.add first", i32, 7, ssa.OpIntAdd, 0, true, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i32, 3), y, 7)} }},
		{"i32.sub 7", i32, 7, ssa.OpIntSub, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUBI(reg(i32, 3), x, 7)} }},
		{"i32.sub -1 wraps at 32 bits", i32, 0xFFFFFFFF, ssa.OpIntSub, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i32, 3), x, 1)} }},
		{"i32.sub 4096", i32, 4096, ssa.OpIntSub, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUB(reg(i32, 3), x, y)} }},
		{"i32.sub first", i32, 7, ssa.OpIntSub, 0, true, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUB(reg(i32, 3), x, y)} }},
		{"i64.add 4095", i64, 4095, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i64, 3), x, 4095)} }},
		{"i64.add 4096", i64, 4096, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i64, 3), x, y)} }},
		{"i64.add 0xFFFFFFFF is not -1", i64, 0xFFFFFFFF, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i64, 3), x, y)} }},
		{"i64.add -1", i64, math.MaxUint64, ssa.OpIntAdd, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUBI(reg(i64, 3), x, 1)} }},
		{"i64.sub -4095", i64, 0xFFFFFFFFFFFFF001, ssa.OpIntSub, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADDI(reg(i64, 3), x, 4095)} }},
		{"i64.sub -4096", i64, 0xFFFFFFFFFFFFF000, ssa.OpIntSub, 0, false, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.SUB(reg(i64, 3), x, y)} }},
		{"i64.add first", i64, 1 << 20, ssa.OpIntAdd, 0, true, false, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.ADD(reg(i64, 3), x, y)} }},
		{"i32.lt_s 2", i32, 2, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 2), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.lt_s 0", i32, 0, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 0), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.lt_s 4095", i32, 4095, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 4095), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.lt_s 4096", i32, 4096, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.lt_s 1<<20", i32, 1 << 20, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.lt_s -1 wraps at 32 bits", i32, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMNI(x, 1), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.ge_s -4095", i32, 0xFFFFF001, ssa.OpIntCmp, ssa.CondSGE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMNI(x, 4095), target.CSET(reg(ssa.TypeI1, 3), target.CondGE)}
		}},
		{"i32.lt_s -4096", i32, 0xFFFFF000, ssa.OpIntCmp, ssa.CondSLT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i32.eq -1", i32, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondEQ, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMNI(x, 1), target.CSET(reg(ssa.TypeI1, 3), target.CondEQ)}
		}},
		{"i32.lt_u 2", i32, 2, ssa.OpIntCmp, ssa.CondULT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 2), target.CSET(reg(ssa.TypeI1, 3), target.CondCC)}
		}},
		{"i32.lt_u -1 stays a register compare", i32, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondULT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondCC)}
		}},
		{"i32.ge_u -1 stays a register compare", i32, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondUGE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondCS)}
		}},
		{"i32.lt_s first", i32, 2, ssa.OpIntCmp, ssa.CondSLT, true, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondLT)}
		}},
		{"i64.gt_s 4095", i64, 4095, ssa.OpIntCmp, ssa.CondSGT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 4095), target.CSET(reg(ssa.TypeI1, 3), target.CondGT)}
		}},
		{"i64.gt_s 0xFFFFFFFF is not -1", i64, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondSGT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondGT)}
		}},
		{"i64.le_s -1", i64, math.MaxUint64, ssa.OpIntCmp, ssa.CondSLE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMNI(x, 1), target.CSET(reg(ssa.TypeI1, 3), target.CondLE)}
		}},
		{"i64.ne -4096", i64, 0xFFFFFFFFFFFFF000, ssa.OpIntCmp, ssa.CondNE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondNE)}
		}},
		{"i64.gt_u -1 stays a register compare", i64, math.MaxUint64, ssa.OpIntCmp, ssa.CondUGT, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondHI)}
		}},
		{"i64.le_u 9", i64, 9, ssa.OpIntCmp, ssa.CondULE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 9), target.CSET(reg(ssa.TypeI1, 3), target.CondLS)}
		}},
		{"ref.eq 3", ssa.TypeRef, 3, ssa.OpRefCmp, ssa.CondEQ, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMPI(x, 3), target.CSET(reg(ssa.TypeI1, 3), target.CondEQ)}
		}},
		{"ref.ne boxed", ssa.TypeRef, uint64(types.BoxRef(5)), ssa.OpRefCmp, ssa.CondNE, false, false, func(x, y asm.VReg) []asm.Instruction {
			return []asm.Instruction{target.CMP(x, y), target.CSET(reg(ssa.TypeI1, 3), target.CondNE)}
		}},
		{"i32.lt_s 2 fused", i32, 2, ssa.OpIntCmp, ssa.CondSLT, false, true, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.CMPI(x, 2)} }},
		{"i32.lt_s -1 fused", i32, 0xFFFFFFFF, ssa.OpIntCmp, ssa.CondSLT, false, true, func(x, y asm.VReg) []asm.Instruction { return []asm.Instruction{target.CMNI(x, 1)} }},
	} {
		// The constant is the second operand, or the first; add and sub
		// keep their operand type, a comparison yields i1.
		operand, result := ssa.Value(2), ssa.TypeI1
		if c.first {
			operand = 1
		}
		if slices.Contains([]ssa.Op{ssa.OpIntAdd, ssa.OpIntSub}, c.op) {
			result = c.typ
		}
		tests = append(tests, test{
			name:   c.name + " immediate",
			regs:   regs{1: c.typ, 2: c.typ, 3: result},
			consts: map[ssa.Value]uint64{operand: c.word},
			op:     exec(c.op, c.cond, 1, 2), fuse: c.fuse,
			rows: c.rows(reg(c.typ, 1), reg(c.typ, 2)), lower: true,
		})
	}

	x := func(v ssa.Value) asm.VReg { return reg(i64, v) }
	w := func(v ssa.Value) asm.VReg { return reg(i32, v) }
	tests = append(tests, []test{
		{
			name: "i32.lt_s leaves a fused condition in the flags",
			regs: regs{1: i32, 2: i32, 3: ssa.TypeI1},
			op:   exec(ssa.OpIntCmp, ssa.CondSLT, 1, 2), fuse: true,
			rows: []asm.Instruction{target.CMP(reg(i32, 1), reg(i32, 2))}, lower: true,
		},
		{
			name: "f64.le leaves a fused condition in the flags",
			regs: regs{1: f64, 2: f64, 3: ssa.TypeI1},
			op:   exec(ssa.OpFloatCmp, ssa.CondOLE, 1, 2), fuse: true,
			rows: []asm.Instruction{target.FCMP(reg(f64, 1), reg(f64, 2))}, lower: true,
		},
		{
			name:   "i64.eqz leaves a fused condition in the flags",
			regs:   regs{1: i64, 2: i64, 3: ssa.TypeI1},
			consts: map[ssa.Value]uint64{2: 0},
			op:     exec(ssa.OpIntCmp, ssa.CondEQ, 1, 2), fuse: true,
			rows: []asm.Instruction{target.CMPI(reg(i64, 1), 0)}, lower: true,
		},
		{
			name: "i64.div_s fails on a zero divisor",
			regs: regs{1: i64, 2: i64, 3: i64},
			op:   exec(ssa.OpIntSDiv, 0, 1, 2),
			rows: []asm.Instruction{target.CBZLabel(x(2), exit), target.SDIV(x(3), x(1), x(2))}, lower: true,
		},
		{
			name: "i32.div_u fails on a zero divisor",
			regs: regs{1: i32, 2: i32, 3: i32},
			op:   exec(ssa.OpIntUDiv, 0, 1, 2),
			rows: []asm.Instruction{target.CBZLabel(w(2), exit), target.UDIV(w(3), w(1), w(2))}, lower: true,
		},
		{
			name: "i32.rem_u subtracts the quotient",
			regs: regs{1: i32, 2: i32, 3: i32},
			op:   exec(ssa.OpIntURem, 0, 1, 2),
			rows: []asm.Instruction{
				target.CBZLabel(w(2), exit), target.UDIV(target.W16, w(1), w(2)), target.MSUB(w(3), target.W16, w(2), w(1)),
			}, lower: true,
		},
		{
			name: "i64.rem_s subtracts the quotient",
			regs: regs{1: i64, 2: i64, 3: i64},
			op:   exec(ssa.OpIntSRem, 0, 1, 2),
			rows: []asm.Instruction{
				target.CBZLabel(x(2), exit), target.SDIV(target.X16, x(1), x(2)), target.MSUB(x(3), target.X16, x(2), x(1)),
			}, lower: true,
		},
		{
			name: "select takes the first operand on a nonzero condition",
			regs: regs{1: i64, 2: i64, 3: i32, 4: i64},
			op:   exec(ssa.OpSelect, 0, 1, 2, 3),
			rows: []asm.Instruction{target.CMPI(w(3), 0), target.CSEL(x(4), x(1), x(2), target.CondNE)}, lower: true,
		},
		{
			name: "select chooses between float registers",
			regs: regs{1: f64, 2: f64, 3: i32, 4: f64},
			op:   exec(ssa.OpSelect, 0, 1, 2, 3),
			rows: []asm.Instruction{target.CMPI(w(3), 0), target.FCSEL(reg(f64, 4), reg(f64, 1), reg(f64, 2), target.CondNE)}, lower: true,
		},
		{
			name: "const i1 is its truth value",
			regs: regs{1: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{1}},
			rows: target.LDI(w(1), 1), lower: true,
		},
		{
			name: "const i8 is its sign-extended lane",
			regs: regs{1: ssa.TypeI8},
			op:   ssa.Operation{Op: ssa.OpConst, Const: 0xffffffff, Results: []ssa.Value{1}},
			rows: target.LDI(w(1), 0xffffffff), lower: true,
		},
		{
			name: "const i64 is its value",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpConst, Const: uint64(0xfffffffffffffffd), Results: []ssa.Value{1}},
			rows: target.LDI(x(1), uint64(0xfffffffffffffffd)), lower: true,
		},
		{
			name: "const f32 moves its bits through scratch",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpConst, Const: uint64(math.Float32bits(1.5)), Results: []ssa.Value{1}},
			rows: append(target.LDI(target.X16, uint64(math.Float32bits(1.5))), target.FMOV(reg(f32, 1), target.W16)), lower: true,
		},
		{
			name: "const f64 moves its bits through scratch",
			regs: regs{1: f64},
			op:   ssa.Operation{Op: ssa.OpConst, Const: math.Float64bits(1.5), Results: []ssa.Value{1}},
			rows: append(target.LDI(target.X16, math.Float64bits(1.5)), target.FMOV(reg(f64, 1), target.X16)), lower: true,
		},
		{
			name: "const ref is its boxed word",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(4)), Results: []ssa.Value{1}},
			rows: target.LDI(x(1), uint64(types.BoxRef(4))), lower: true,
		},
		{
			name: "load i64 keeps the slot word for its guard",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(x(1), target.X25, 16)}, lower: true,
		},
		{
			name: "guard.kind unboxes an inline i64 or a heap-promoted one, deopts on a ref to another object",
			regs: regs{1: i64, 2: i64},
			op:   ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: slices.Concat(
				[]asm.Instruction{
					target.LSRI(target.X16, x(1), 49),
					target.TSTI(target.X16, 1),
					target.BCondLabel(target.OpBEQ, 2),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, 2),
					target.SBFX(heap, x(1), 0, 32),
					target.LSLI(heap, heap, 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(heap, target.X16, heap),
					target.LDR(target.X16, heap, 0),
				},
				target.LDI(target.X17, uint64(jit.Itab(types.I64(0)))),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, exit),
					target.LDR(x(2), heap, int16(jit.OffsetData)),
					target.LDR(x(2), x(2), 0),
					target.BLabel(3),
					target.SBFX(x(2), x(1), 0, 49),
				},
			), lower: true,
		},
		{
			name: "guard.value compares its argument with the admitted word and deopts on any other",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardValue, Args: []ssa.Value{1, 2}, Results: []ssa.Value{3}},
			rows: []asm.Instruction{
				target.CMP(x(1), x(2)),
				target.BCondLabel(target.OpBNE, exit),
				target.MOV(x(3), x(1)),
			}, lower: true,
		},
		{
			name: "retain counts any reference up",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRefRetain, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, x(1), 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, 2),
					target.SBFX(target.X17, x(1), 0, 32),
				},
				counter,
				[]asm.Instruction{target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0)},
			), lower: true,
		},
		{
			name: "release counts a non-null reference down and exits on the last",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRefRelease, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, x(1), 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, x(1), 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
				},
			), lower: true,
		},
		{
			name: "load f32 reads the low word",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(reg(f32, 1), target.X25, 16)}, lower: true,
		},
		{
			name: "load ref keeps its boxed word",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: local(2), Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(x(1), target.X25, 16)}, lower: true,
		},
		{
			name: "load global addresses the globals base",
			regs: regs{1: i32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 2}, Results: []ssa.Value{1}},
			rows: []asm.Instruction{target.LDR(globals, arm64.Ctx, int16(jit.OffsetGlobals)), target.LDR(w(1), globals, 16)}, lower: true,
		},
		{
			name: "store i8 tags its lane",
			regs: regs{1: ssa.TypeI8},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(2), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindI8, target.UXTW(target.X16, w(1))), target.STR(target.X16, target.X25, 16)), lower: true,
		},
		{

			name: "store i64 boxes outside the inline range",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindI64, append(target.LDI(target.X16, 1<<48),
				target.ADD(target.X17, x(1), target.X16),
				target.LSRI(target.X17, target.X17, 49),
				target.CBNZLabel(target.X17, exit),
				target.ANDI(target.X16, x(1), types.VMask),
			)...), target.STR(target.X16, target.X25, 8)), lower: true,
		},
		{
			name: "store f32 tags its bits",
			regs: regs{1: f32},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: append(boxed(types.KindF32, target.FMOV(target.W16, reg(f32, 1))), target.STR(target.X16, target.X25, 8)), lower: true,
		},
		{
			name: "store f64 writes its bits, a positive NaN canonical",
			regs: regs{1: f64},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: local(1), Args: []ssa.Value{1}},
			rows: slices.Concat([]asm.Instruction{target.FMOV(target.X16, reg(f64, 1))}, canonical(), []asm.Instruction{target.STR(target.X16, target.X25, 8)}), lower: true,
		},
		{
			name: "store global addresses the globals base",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 1}, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{
					target.LDR(globals, arm64.Ctx, int16(jit.OffsetGlobals)),
					target.LDR(old, globals, 8),
					target.LSRI(target.X16, old, 49),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, old, 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(x(1), globals, 8),
				},
			), lower: true,
		},
		{
			name: "store global boxes an i64 before releasing the old word",
			regs: regs{1: i64},
			op:   ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceGlobal, Index: 1}, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{target.LDR(globals, arm64.Ctx, int16(jit.OffsetGlobals))},
				boxed(types.KindI64, append(target.LDI(target.X16, 1<<48),
					target.ADD(target.X17, x(1), target.X16),
					target.LSRI(target.X17, target.X17, 49),
					target.CBNZLabel(target.X17, exit),
					target.ANDI(target.X16, x(1), types.VMask),
				)...),
				[]asm.Instruction{
					target.MOV(word, target.X16),
					target.LDR(prior, globals, 8),
					target.LSRI(target.X16, prior, 49),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, prior, 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(word, globals, 8),
				},
			), lower: true,
		},
		{
			name:   "load upval addresses the upvals base",
			regs:   regs{1: i32},
			layout: compile.Layout{Upvals: true},
			op:     ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceUpval, Index: 2}, Results: []ssa.Value{1}},
			rows:   []asm.Instruction{target.LDR(w(1), vr(1), 16)}, lower: true,
		},
		{
			name:   "store upval releases the old word through the upvals base",
			regs:   regs{1: ssa.TypeRef},
			layout: compile.Layout{Upvals: true},
			op:     ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceUpval, Index: 1}, Args: []ssa.Value{1}},
			rows: slices.Concat(
				[]asm.Instruction{
					target.LDR(vr(2), vr(1), 8),
					target.LSRI(target.X16, vr(2), 49),
				},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, vr(2), 0, 32),
					target.CBZLabel(target.X17, resume),
				},
				counter,
				[]asm.Instruction{
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(x(1), vr(1), 8),
				},
			), lower: true,
		},
		{
			name: "declines an upval slot of a function that never loads its upvals base",
			regs: regs{1: i32},
			op:   ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceUpval}, Results: []ssa.Value{1}},
		},
		{
			name: "declines an opcode without a lowering",
			regs: regs{1: ssa.TypeRef, 2: i32, 3: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpExec, Code: instr.MAP_KEYS, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
		},
	}...)

	i32array := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	i8array := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI8}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	i1array := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI1}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	refarray := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindRef}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	structShape := ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct, Type: 0x10}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}

	tests = append(tests, []test{
		{
			name: "admits a matching array and passes the same word through",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				}
				rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(types.TypedArray[int32](nil))))...)
				return append(rows,
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)),
				)
			}(),
			lower: true,
		},
		{
			name: "admits a map of the exact key and additionally checks the declared key and element kinds in one halfword",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormMap, Key: types.KindI32, Elem: types.KindRef}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				}
				rows = append(rows, target.LDI(target.X17, uint64(jit.Itab((*types.Map)(nil))))...)
				rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit))
				rows = append(rows,
					target.LDR(target.X16, vr(1), int16(jit.OffsetData)),
					target.LDR(target.X16, target.X16, int16(jit.OffsetMapTyp)),
					target.LDRH(target.W16, target.X16, int16(jit.OffsetMapTypeKinds)),
				)
				rows = append(rows, target.LDI(target.X17, uint64(types.KindI32)|uint64(types.KindRef)<<8)...)
				return append(rows,
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)),
				)
			}(),
			lower: true,
		},
		{
			name: "admits a struct of the exact type and additionally checks Typ",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct, Type: 0x2a}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				}
				rows = append(rows, target.LDI(target.X17, uint64(jit.Itab((*types.Struct)(nil))))...)
				rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit))
				rows = append(rows,
					target.LDR(target.X16, vr(1), int16(jit.OffsetData)),
					target.LDR(target.X16, target.X16, int16(jit.OffsetStructTyp)),
				)
				rows = append(rows, target.LDI(target.X17, uint64(0x2a))...)
				return append(rows,
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)),
				)
			}(),
			lower: true,
		},
		{
			name: "admits any struct generically when Type is unset",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormStruct}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				}
				rows = append(rows, target.LDI(target.X17, uint64(jit.Itab((*types.Struct)(nil))))...)
				return append(rows,
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)),
				)
			}(),
			lower: true,
		},
		{
			name: "admits a closure over the function, of its type, holding its captures",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormClosure, Function: 3, Type: 0x2a, Captures: 1}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, reg(ssa.TypeRef, 1), 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				},
				target.LDI(target.X17, uint64(jit.Itab((*types.Closure)(nil)))),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.LDR(vr(2), vr(1), int16(jit.OffsetData)),
					target.LDR(target.W16, vr(2), int16(jit.OffsetClosureFn)),
				},
				target.LDI(target.X17, 3),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.LDR(target.X16, vr(2), int16(jit.OffsetClosureTyp)),
				},
				target.LDI(target.X17, 0x2a),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.LDR(target.X16, vr(2), int16(jit.OffsetClosureUpvals+jit.OffsetSliceLen)),
					target.CMPI(target.X16, 1), target.BCondLabel(target.OpBCC, exit),
					target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)),
				},
			),
			lower: true,
		},
		{
			name: "declines a shape naming no representation",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormArray, Elem: types.Kind(99)}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
		},
		{
			name:  "array.len loads the element count",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32},
			guard: &i32array,
			op:    ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{2}, Results: []ssa.Value{3}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.MOVW(reg(i32, 3), vr(5)),
			},
			lower: true,
		},
		{
			name:  "array.get loads a 4-byte scalar element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &i32array,
			op:    ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.LDRR(reg(i32, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "array.get loads a signed 1-byte element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8},
			guard: &i8array,
			op:    ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.LDRSBR(reg(ssa.TypeI8, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "array.get retains a loaded ref element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeRef},
			guard: &refarray,
			op:    ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.ADDI(vr(3), vr(3), uint16(jit.OffsetArrayElems)),
					target.LDR(vr(4), vr(3), 0),
					target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.LDRR(reg(ssa.TypeRef, 4), vr(4), vr(6)),
				}
				return append(rows, retainRows(reg(ssa.TypeRef, 4))...)
			}(),
			lower: true,
		},
		{
			name:  "array.set releases the old element and stores a new ref, adopting it",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeRef},
			guard: &refarray,
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.ADDI(vr(3), vr(3), uint16(jit.OffsetArrayElems)),
					target.LDR(vr(4), vr(3), 0),
					target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.LDRR(vr(7), vr(4), vr(6)),
					target.STRR(reg(ssa.TypeRef, 4), vr(4), vr(6)),
				}
				return append(rows, releaseRows(vr(7))...)
			}(),
			lower: true,
		},
		{
			name:  "array.set boxes a scalar written into a []any element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &refarray,
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.ADDI(vr(3), vr(3), uint16(jit.OffsetArrayElems)),
					target.LDR(vr(4), vr(3), 0),
					target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.UXTW(target.X16, reg(i32, 4)),
				}
				rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32))...)
				rows = append(rows,
					target.ORR(target.X16, target.X16, target.X17),
					target.LDRR(vr(7), vr(4), vr(6)),
					target.STRR(target.X16, vr(4), vr(6)),
				)
				return append(rows, releaseRows(vr(7))...)
			}(),
			lower: true,
		},
		{
			name:  "array.set stores a 4-byte i32 element with the narrow STRW form",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &i32array,
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.STRWR(reg(i32, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "array.set stores a 1-byte scalar element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8},
			guard: &i8array,
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.STRBR(reg(ssa.TypeI8, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "array.set narrows an i32 value to a 0/1 byte for an i1 element",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &i1array,
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.CMPI(reg(i32, 4), 0), target.CSET(vr(7), target.CondNE),
				target.STRBR(vr(7), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "slice loads a guarded array's element pointer and length and passes the same word through",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 5: ssa.TypeRef},
			guard: &i32array,
			op:    ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.MOV(reg(ssa.TypeRef, 5), reg(ssa.TypeRef, 2)),
			},
			lower: true,
		},
		{
			name: "declines a slice of a container that carries no shape guard",
			regs: regs{1: ssa.TypeRef, 5: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{1}, Results: []ssa.Value{5}},
		},
		{
			name:  "array.get through a slice reads its element pointer and length, no heap word",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32, 5: ssa.TypeRef},
			guard: &i32array,
			slice: &ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			op:    ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{5, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.LDRR(reg(i32, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:  "array.len through a slice moves its length",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 5: ssa.TypeRef},
			guard: &i32array,
			slice: &ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			op:    ssa.Operation{Op: ssa.OpArrayLen, Args: []ssa.Value{5}, Results: []ssa.Value{3}},
			rows:  []asm.Instruction{target.MOVW(reg(i32, 3), vr(5))},
			lower: true,
		},
		{
			name:  "bound marks an index within a slice and emits no rows",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 5: ssa.TypeRef, 6: i32},
			guard: &i32array,
			slice: &ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			op:    ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{3, 5}, Results: []ssa.Value{6}},
			rows:  []asm.Instruction{},
			lower: true,
		},
		{
			name:  "array.get at a bound index loads through its sign-extended register with no bounds check",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32, 5: ssa.TypeRef, 6: i32},
			guard: &i32array,
			slice: &ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			bound: &ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{3, 5}, Results: []ssa.Value{6}},
			op:    ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{5, 6}, Results: []ssa.Value{4}},
			rows:  []asm.Instruction{target.LDRR(reg(i32, 4), vr(4), reg(i32, 3))},
			lower: true,
		},
		{
			name:  "array.set at a bound index stores through its sign-extended register with no bounds check",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8, 5: ssa.TypeRef, 6: i32},
			guard: &i8array,
			slice: &ssa.Operation{Op: ssa.OpArrayView, Args: []ssa.Value{2}, Results: []ssa.Value{5}},
			bound: &ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{3, 5}, Results: []ssa.Value{6}},
			op:    ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{5, 6, 4}},
			rows:  []asm.Instruction{target.STRBR(reg(ssa.TypeI8, 4), vr(4), reg(i32, 3))},
			lower: true,
		},
		{
			name: "declines a bound within a container no slice names",
			regs: regs{1: ssa.TypeRef, 3: i32, 6: i32},
			op:   ssa.Operation{Op: ssa.OpArrayBound, Args: []ssa.Value{3, 1}, Results: []ssa.Value{6}},
		},
		{
			name: "string.len loads a string's length after checking its representation",
			regs: regs{1: ssa.TypeRef, 2: i32},
			op:   ssa.Operation{Op: ssa.OpStringLen, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(1), reg(ssa.TypeRef, 1), 0, 32), target.LSLI(vr(1), vr(1), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(1), target.X16, vr(1)),
					target.LDR(target.X16, vr(1), 0),
				}
				rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(types.String(""))))...)
				return append(rows,
					target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, exit),
					target.LDR(vr(2), vr(1), int16(jit.OffsetData)),
					target.LDR(reg(i32, 2), vr(2), int16(jit.OffsetStringLen)),
				)
			}(),
			lower: true,
		},
		{
			name:  "string.eq compares lengths, then data pointers, then eight bytes at a time and the tail byte by byte",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondEQ, 1, 2),
			rows:  stringCompare(target.CondEQ, true),
			lower: true,
		},
		{
			name:  "string.ne reads the flags string.eq does, for not equal",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondNE, 1, 2),
			rows:  stringCompare(target.CondNE, true),
			lower: true,
		},
		{
			name:  "string.lt reads the first differing byte unsigned, else the lengths",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondULT, 1, 2),
			rows:  stringCompare(target.CondCC, false),
			lower: true,
		},
		{
			name:  "string.gt is string.lt with the greater condition",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondUGT, 1, 2),
			rows:  stringCompare(target.CondHI, false),
			lower: true,
		},
		{
			name:  "string.le is string.lt with the lower-or-same condition",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondULE, 1, 2),
			rows:  stringCompare(target.CondLS, false),
			lower: true,
		},
		{
			name:  "string.ge is string.lt with the higher-or-same condition",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: ssa.TypeI1},
			op:    exec(ssa.OpStringCmp, ssa.CondUGE, 1, 2),
			rows:  stringCompare(target.CondCS, false),
			lower: true,
		},
		{
			name: "guard.bounds deopts unless its limit is at most the length, signed",
			regs: regs{1: i32, 2: i32},
			op:   ssa.Operation{Op: ssa.OpGuardBounds, Args: []ssa.Value{1, 2}},
			rows: []asm.Instruction{
				target.CMP(reg(i32, 1), reg(i32, 2)), target.BCondLabel(target.OpBGT, exit),
			},
			lower: true,
		},
		{
			name:   "array.get at a constant index checks the length against it and loads at an immediate offset",
			regs:   regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			consts: map[ssa.Value]uint64{3: 4095},
			guard:  &i32array,
			op:     ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.CMPI(vr(5), 4095), target.BCondLabel(target.OpBLS, exit),
				target.LDR(reg(i32, 4), vr(4), 4*4095),
			},
			lower: true,
		},
		{
			name:   "array.set at a constant index past the immediate range indexes through its register",
			regs:   regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			consts: map[ssa.Value]uint64{3: 4096},
			guard:  &i32array,
			op:     ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.STRWR(reg(i32, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:   "array.set at a negative constant index indexes through its register, which traps",
			regs:   regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8},
			consts: map[ssa.Value]uint64{3: uint64(uint32(math.MaxUint32))},
			guard:  &i8array,
			op:     ssa.Operation{Op: ssa.OpArraySet, Args: []ssa.Value{2, 3, 4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), 0),
				target.LDR(vr(5), vr(3), int16(jit.OffsetSliceLen)),
				target.SXTW(vr(6), reg(i32, 3)),
				target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
				target.STRBR(reg(ssa.TypeI8, 4), vr(4), vr(6)),
			},
			lower: true,
		},
		{
			name:   "struct.get at a constant index loads its field at an immediate offset",
			regs:   regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8},
			consts: map[ssa.Value]uint64{3: 2},
			guard:  &structShape,
			op:     ssa.Operation{Op: ssa.OpStructGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), int16(jit.OffsetStructData)),
				target.LDRSB(reg(ssa.TypeI8, 4), vr(4), 16),
			},
			lower: true,
		},
		{
			name: "declines a container that carries no shape guard",
			regs: regs{1: ssa.TypeRef, 3: i32, 4: i32},
			op:   ssa.Operation{Op: ssa.OpArrayGet, Args: []ssa.Value{1, 3}, Results: []ssa.Value{4}},
		},
		{
			name: "struct.new_default takes the last ref of its type's bin or bridges when it is empty",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpStructNewDefault, Shape: ssa.Shape{Form: ssa.FormStruct, Index: binIndex}, Results: []ssa.Value{1}},
			rows: []asm.Instruction{
				target.LDR(vr(1), arm64.Ctx, int16(jit.OffsetBins)),
				target.LDR(vr(2), vr(1), int16(3*jit.Classes*int(jit.SizeofBin)+int(jit.OffsetBinN))),
				target.CBZLabel(vr(2), exit),
				target.SUBI(vr(2), vr(2), 1),
				target.STR(vr(2), vr(1), int16(3*jit.Classes*int(jit.SizeofBin)+int(jit.OffsetBinN))),
				target.LDR(vr(3), vr(1), int16(3*jit.Classes*int(jit.SizeofBin))),
				target.LSLI(vr(4), vr(2), 3), target.ADD(vr(4), vr(3), vr(4)),
				target.LDR(reg(ssa.TypeRef, 1), vr(4), 0),
			},
			lower: true,
		},
		{
			name: "struct.new_default reaches a bin entry past the immediate range through an added offset",
			regs: regs{1: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpStructNewDefault, Shape: ssa.Shape{Form: ssa.FormStruct, Index: farIndex}, Results: []ssa.Value{1}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LDR(vr(1), arm64.Ctx, int16(jit.OffsetBins))}
				rows = append(rows, target.LDI(target.X16, uint64(5000*jit.Classes*int(jit.SizeofBin)))...)
				return append(rows,
					target.ADD(vr(1), vr(1), target.X16),
					target.LDR(vr(2), vr(1), int16(jit.OffsetBinN)),
					target.CBZLabel(vr(2), exit),
					target.SUBI(vr(2), vr(2), 1),
					target.STR(vr(2), vr(1), int16(jit.OffsetBinN)),
					target.LDR(vr(3), vr(1), 0),
					target.LSLI(vr(4), vr(2), 3), target.ADD(vr(4), vr(3), vr(4)),
					target.LDR(reg(ssa.TypeRef, 1), vr(4), 0),
				)
			}(),
			lower: true,
		},
		{
			name: "array.new_default takes the last ref of its length class's bin, with a last class of one exact length, or bridges",
			regs: regs{1: ssa.TypeRef, 2: i32},
			op:   ssa.Operation{Op: ssa.OpArrayNewDefault, Shape: ssa.Shape{Form: ssa.FormArray, Index: binIndex}, Args: []ssa.Value{2}, Results: []ssa.Value{1}},
			rows: []asm.Instruction{
				target.LDR(vr(1), arm64.Ctx, int16(jit.OffsetBins)),
				target.SXTW(vr(4), reg(i32, 2)),
				target.LDI(target.X17, jit.Classes-1)[0], target.CMPI(vr(4), jit.Classes-1),
				target.CSEL(vr(6), vr(4), target.X17, target.CondCC),
				target.LDI(target.X16, uint64(jit.SizeofBin))[0], target.MADD(vr(1), vr(6), target.X16, vr(1)),
				target.LDR(vr(2), vr(1), int16(3*jit.Classes*int(jit.SizeofBin)+int(jit.OffsetBinN))),
				target.CBZLabel(vr(2), exit),
				target.LDR(vr(5), vr(1), int16(3*jit.Classes*int(jit.SizeofBin)+int(jit.OffsetBinLen))),
				target.CMP(vr(4), vr(5)),
				target.BCondLabel(target.OpBNE, exit),
				target.SUBI(vr(2), vr(2), 1),
				target.STR(vr(2), vr(1), int16(3*jit.Classes*int(jit.SizeofBin)+int(jit.OffsetBinN))),
				target.LDR(vr(3), vr(1), int16(3*jit.Classes*int(jit.SizeofBin))),
				target.LSLI(vr(7), vr(2), 3), target.ADD(vr(7), vr(3), vr(7)),
				target.LDR(reg(ssa.TypeRef, 1), vr(7), 0),
			},
			lower: true,
		},
		{
			name: "array.new_default reaches a bin entry past the immediate range through an added offset",
			regs: regs{1: ssa.TypeRef, 2: i32},
			op:   ssa.Operation{Op: ssa.OpArrayNewDefault, Shape: ssa.Shape{Form: ssa.FormArray, Index: farIndex}, Args: []ssa.Value{2}, Results: []ssa.Value{1}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LDR(vr(1), arm64.Ctx, int16(jit.OffsetBins))}
				rows = append(rows, target.LDI(target.X16, uint64(5000*jit.Classes*int(jit.SizeofBin)))...)
				return append(rows,
					target.ADD(vr(1), vr(1), target.X16),
					target.SXTW(vr(4), reg(i32, 2)),
					target.LDI(target.X17, jit.Classes-1)[0], target.CMPI(vr(4), jit.Classes-1),
					target.CSEL(vr(6), vr(4), target.X17, target.CondCC),
					target.LDI(target.X16, uint64(jit.SizeofBin))[0], target.MADD(vr(1), vr(6), target.X16, vr(1)),
					target.LDR(vr(2), vr(1), int16(jit.OffsetBinN)),
					target.CBZLabel(vr(2), exit),
					target.LDR(vr(5), vr(1), int16(jit.OffsetBinLen)),
					target.CMP(vr(4), vr(5)),
					target.BCondLabel(target.OpBNE, exit),
					target.SUBI(vr(2), vr(2), 1),
					target.STR(vr(2), vr(1), int16(jit.OffsetBinN)),
					target.LDR(vr(3), vr(1), 0),
					target.LSLI(vr(7), vr(2), 3), target.ADD(vr(7), vr(3), vr(7)),
					target.LDR(reg(ssa.TypeRef, 1), vr(7), 0),
				)
			}(),
			lower: true,
		},
		{
			name:  "struct.get loads a 4-byte field with no bounds check",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &structShape,
			op:    ssa.Operation{Op: ssa.OpStructGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: []asm.Instruction{
				target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(vr(2), target.X16, vr(2)),
				target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
				target.LDR(vr(4), vr(3), int16(jit.OffsetStructData)),
				target.SXTW(vr(5), reg(i32, 3)),
				target.LSLI(vr(6), vr(5), 3), target.ADD(vr(6), vr(4), vr(6)),
				target.LDR(reg(i32, 4), vr(6), 0),
			},
			lower: true,
		},
		{
			name:  "struct.get retains a ref field",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeRef},
			guard: &structShape,
			op:    ssa.Operation{Op: ssa.OpStructGet, Args: []ssa.Value{2, 3}, Results: []ssa.Value{4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.LDR(vr(4), vr(3), int16(jit.OffsetStructData)),
					target.SXTW(vr(5), reg(i32, 3)),
					target.LSLI(vr(6), vr(5), 3), target.ADD(vr(6), vr(4), vr(6)),
					target.LDR(reg(ssa.TypeRef, 4), vr(6), 0),
				}
				return append(rows, retainRows(reg(ssa.TypeRef, 4))...)
			}(),
			lower: true,
		},
		{
			name:  "struct.set stores a 4-byte field, bounds-checked at Typ.Fields' own length",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: i32},
			guard: &structShape,
			op:    ssa.Operation{Op: ssa.OpStructSet, Args: []ssa.Value{2, 3, 4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
					target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.LDR(vr(7), vr(4), int16(jit.OffsetStructTypeFields)),
				}
				rows = append(rows, target.LDI(target.X16, uint64(jit.SizeofStructField))...)
				return append(rows,
					target.MUL(vr(8), vr(6), target.X16), target.ADD(vr(8), vr(7), vr(8)),
					target.LDRB(target.X16, vr(8), int16(jit.OffsetStructFieldKind)),
					target.CMPI(target.X16, uint16(types.KindI32)), target.BCondLabel(target.OpBNE, exit),
					target.LDR(vr(9), vr(3), int16(jit.OffsetStructData)),
					target.LSLI(vr(10), vr(6), 3), target.ADD(vr(10), vr(9), vr(10)),
					target.UXTW(target.X16, reg(i32, 4)),
					target.STR(target.X16, vr(10), 0),
				)
			}(),
			lower: true,
		},
		{
			name:  "struct.set widens and sign-extends a 1-byte field",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeI8},
			guard: &structShape,
			op:    ssa.Operation{Op: ssa.OpStructSet, Args: []ssa.Value{2, 3, 4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
					target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.LDR(vr(7), vr(4), int16(jit.OffsetStructTypeFields)),
				}
				rows = append(rows, target.LDI(target.X16, uint64(jit.SizeofStructField))...)
				return append(rows,
					target.MUL(vr(8), vr(6), target.X16), target.ADD(vr(8), vr(7), vr(8)),
					target.LDRB(target.X16, vr(8), int16(jit.OffsetStructFieldKind)),
					target.CMPI(target.X16, uint16(types.KindI8)), target.BCondLabel(target.OpBNE, exit),
					target.LDR(vr(9), vr(3), int16(jit.OffsetStructData)),
					target.LSLI(vr(10), vr(6), 3), target.ADD(vr(10), vr(9), vr(10)),
					target.SBFX(target.W16, reg(ssa.TypeI8, 4), 0, 8),
					target.STR(target.X16, vr(10), 0),
				)
			}(),
			lower: true,
		},
		{
			name:  "struct.set releases the old ref field and adopts the new one",
			regs:  regs{1: ssa.TypeRef, 2: ssa.TypeRef, 3: i32, 4: ssa.TypeRef},
			guard: &structShape,
			op:    ssa.Operation{Op: ssa.OpStructSet, Args: []ssa.Value{2, 3, 4}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{
					target.SBFX(vr(2), reg(ssa.TypeRef, 2), 0, 32), target.LSLI(vr(2), vr(2), 4),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
					target.ADD(vr(2), target.X16, vr(2)),
					target.LDR(vr(3), vr(2), int16(jit.OffsetData)),
					target.LDR(vr(4), vr(3), int16(jit.OffsetStructTyp)),
					target.LDR(vr(5), vr(4), int16(jit.OffsetStructTypeFields+jit.OffsetSliceLen)),
					target.SXTW(vr(6), reg(i32, 3)),
					target.CMP(vr(6), vr(5)), target.BCondLabel(target.OpBCS, exit),
					target.LDR(vr(7), vr(4), int16(jit.OffsetStructTypeFields)),
				}
				rows = append(rows, target.LDI(target.X16, uint64(jit.SizeofStructField))...)
				rows = append(rows,
					target.MUL(vr(8), vr(6), target.X16), target.ADD(vr(8), vr(7), vr(8)),
					target.LDRB(target.X16, vr(8), int16(jit.OffsetStructFieldKind)),
					target.CMPI(target.X16, uint16(types.KindRef)), target.BCondLabel(target.OpBNE, exit),
					target.LDR(vr(9), vr(3), int16(jit.OffsetStructData)),
					target.LSLI(vr(10), vr(6), 3), target.ADD(vr(10), vr(9), vr(10)),
					target.LDR(vr(11), vr(10), 0),
					target.STR(reg(ssa.TypeRef, 4), vr(10), 0),
				)
				return append(rows, releaseRows(vr(11))...)
			}(),
			lower: true,
		},
		{
			name: "ref.is_null tests the low word of a boxed ref, no guard required",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefIsNull, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: []asm.Instruction{
				target.SBFX(target.X16, reg(ssa.TypeRef, 1), 0, 32),
				target.CMPI(target.X16, 0),
				target.CSET(reg(ssa.TypeI1, 2), target.CondEQ),
			},
			lower: true,
		},
		{
			name: "ref.test of an i32 matches its inline tag or a heap types.I32 cell, yielding 1 on a match and 0 otherwise",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefTest, Shape: ssa.Shape{Elem: types.KindI32, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LSRI(target.X16, reg(ssa.TypeRef, 1), 49)}
				rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32)>>49)...)
				rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, asm.Label(4)))
				rows = append(rows, cell(types.I32(0), asm.Label(2))...)
				return append(rows, target.LDI(reg(ssa.TypeI1, 2), 1)[0], target.BLabel(asm.Label(3)),
					target.LDI(reg(ssa.TypeI1, 2), 0)[0])
			}(),
			lower: true,
		},
		{
			name: "ref.test of an f64 matches every word but the seven tagged positive NaNs, or a heap types.F64 cell",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefTest, Shape: ssa.Shape{Elem: types.KindF64, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LSRI(target.X16, reg(ssa.TypeRef, 1), 49)}
				rows = append(rows, target.LDI(target.X17, 0x3FF9)...)
				rows = append(rows, target.SUB(target.X16, target.X16, target.X17), target.CMPI(target.X16, 6), target.BCondLabel(target.OpBHI, asm.Label(4)),
					target.ADD(target.X16, target.X16, target.X17))
				rows = append(rows, cell(types.F64(0), asm.Label(2))...)
				return append(rows, target.LDI(reg(ssa.TypeI1, 2), 1)[0], target.BLabel(asm.Label(3)),
					target.LDI(reg(ssa.TypeI1, 2), 0)[0])
			}(),
			lower: true,
		},
		{
			name: "ref.test of an i64 matches its inline tag or a heap types.I64, promoted or a cell",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefTest, Shape: ssa.Shape{Elem: types.KindI64, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LSRI(target.X16, reg(ssa.TypeRef, 1), 49)}
				rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI64)>>49)...)
				rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, asm.Label(4)))
				rows = append(rows, cell(types.I64(0), asm.Label(2))...)
				return append(rows, target.LDI(reg(ssa.TypeI1, 2), 1)[0], target.BLabel(asm.Label(3)),
					target.LDI(reg(ssa.TypeI1, 2), 0)[0])
			}(),
			lower: true,
		},
		{
			name: "ref.cast of an i8 traps unless the word is an inline i8 or a heap types.I8 cell, and keeps the word",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRefCast, Shape: ssa.Shape{Elem: types.KindI8, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
			rows: func() []asm.Instruction {
				rows := []asm.Instruction{target.LSRI(target.X16, reg(ssa.TypeRef, 1), 49)}
				rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI8)>>49)...)
				rows = append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, asm.Label(2)))
				rows = append(rows, cell(types.I8(0), exit)...)
				return append(rows, target.MOV(reg(ssa.TypeRef, 2), reg(ssa.TypeRef, 1)))
			}(),
			lower: true,
		},
		{
			name: "ref.test declines a type that is not scalar",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefTest, Shape: ssa.Shape{Elem: types.KindRef, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
		},
		{
			name: "ref.cast declines a type that is not scalar",
			regs: regs{1: ssa.TypeRef, 2: ssa.TypeRef},
			op:   ssa.Operation{Op: ssa.OpRefCast, Shape: ssa.Shape{Elem: types.KindRef, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
		},
		{
			name: "ref.test declines an operand that is not a ref",
			regs: regs{1: i32, 2: ssa.TypeI1},
			op:   ssa.Operation{Op: ssa.OpRefTest, Shape: ssa.Shape{Elem: types.KindI32, Index: 3}, Args: []ssa.Value{1}, Results: []ssa.Value{2}},
		},
	}...)

	iv := func(n int, w asm.RegWidth) asm.VReg { return asm.NewVReg(int32(-2-n), asm.RegTypeInt, w) }
	fv := func(n int, w asm.RegWidth) asm.VReg { return asm.NewVReg(int32(-2-n), asm.RegTypeFloat, w) }
	for _, c := range []struct {
		op    ssa.Op
		cond  ssa.Cond
		typ   ssa.Type
		width asm.RegWidth
	}{
		{ssa.OpIntCtz, 0, i32, asm.Width32}, {ssa.OpIntCtz, 0, i64, asm.Width64},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ},
			op:   exec(c.op, c.cond, 1),
			rows: []asm.Instruction{
				target.RBIT(iv(0, c.width), reg(c.typ, 1)),
				target.CLZ(reg(c.typ, 2), iv(0, c.width)),
			}, lower: true,
		})
	}
	for _, c := range []struct {
		op    ssa.Op
		cond  ssa.Cond
		typ   ssa.Type
		width asm.RegWidth
	}{
		{ssa.OpIntPopcnt, 0, i32, asm.Width32}, {ssa.OpIntPopcnt, 0, i64, asm.Width64},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ},
			op:   exec(c.op, c.cond, 1),
			rows: []asm.Instruction{
				target.FMOV(fv(0, c.width), reg(c.typ, 1)),
				target.CNT(fv(1, c.width), fv(0, c.width)),
				target.ADDV(fv(2, c.width), fv(1, c.width)),
				target.FMOV(reg(c.typ, 2), fv(2, c.width)),
			}, lower: true,
		})
	}
	for _, c := range []struct {
		op    ssa.Op
		cond  ssa.Cond
		typ   ssa.Type
		width asm.RegWidth
	}{
		{ssa.OpIntRotl, 0, i32, asm.Width32}, {ssa.OpIntRotl, 0, i64, asm.Width64},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ, 3: c.typ},
			op:   exec(c.op, c.cond, 1, 2),
			rows: []asm.Instruction{
				target.NEG(iv(0, c.width), reg(c.typ, 2)),
				target.ROR(reg(c.typ, 3), reg(c.typ, 1), iv(0, c.width)),
			}, lower: true,
		})
	}
	for _, c := range []struct {
		op      ssa.Op
		cond    ssa.Cond
		typ     ssa.Type
		width   asm.RegWidth
		signBit uint64
		magMask uint64
	}{
		{ssa.OpFloatCopysign, 0, f32, asm.Width32, 1 << 31, 1<<31 - 1},
		{ssa.OpFloatCopysign, 0, f64, asm.Width64, 1 << 63, 1<<63 - 1},
	} {
		tests = append(tests, test{
			name: c.op.String() + " " + c.cond.String(),
			regs: regs{1: c.typ, 2: c.typ, 3: c.typ},
			op:   exec(c.op, c.cond, 1, 2),
			rows: []asm.Instruction{
				target.FMOV(iv(0, c.width), reg(c.typ, 1)),
				target.FMOV(iv(1, c.width), reg(c.typ, 2)),
				target.ANDI(iv(0, c.width), iv(0, c.width), c.magMask),
				target.ANDI(iv(1, c.width), iv(1, c.width), c.signBit),
				target.ORR(iv(0, c.width), iv(0, c.width), iv(1, c.width)),
				target.FMOV(reg(c.typ, 3), iv(0, c.width)),
			}, lower: true,
		})
	}

	// Map operations over a guarded integer-keyed map, written from the table's
	// contract: a key's home slot is ((key ^ seed) * hash >> shift) & mask, a
	// lookup walks ctrl[i] from it (0 empty, 1 dead, 0x80 with the tag full)
	// comparing the key word at slots[2i], and a set that must rehash leaves the
	// table untouched and bridges. The helpers number the virtual registers in the order a lowering
	// takes them: the heap slot address, the table, the key word when it is not
	// the operand itself, then ctrl, slots, mask, the home slot index, the first
	// dead slot a set remembers, and the probe's state byte, slot address, and
	// slot key word.
	// The shape guard lowered ahead of each operation takes vr(1) for the heap slot
	// address it reads, so the operation's own registers count from vr(2).
	guarded := vr
	vr := func(n int) asm.VReg { return guarded(n + 1) }
	type walk struct {
		data, ctrl, slots, mask, index, tag, free, state, slot, word int
		step                                                         asm.Label
	}
	// keyRegs is the registers a key's rows derive: an i64 key is its own register,
	// and its rows only check it (three rows); an i8 key of an any map takes two.
	keyRegs := func(rows []asm.Instruction) int {
		switch {
		case len(rows) == 0, rows[0].Op == uint16(target.OpSBFX) && len(rows) == 3:
			return 0
		case rows[0].Op == uint16(target.OpSXTB):
			return 2
		}
		return 1
	}
	// layout numbers a walk's registers; a set takes one more, free, before the
	// probe's.
	layout := func(data, ctrl int, set bool) walk {
		w := walk{data: data, ctrl: ctrl, slots: ctrl + 1, mask: ctrl + 2, index: ctrl + 3, tag: ctrl + 4, state: ctrl + 5}
		if set {
			w.free, w.state = w.state, w.state+1
		}
		w.slot, w.word = w.state+1, w.state+2
		return w
	}
	intMap := func(key, elem types.Kind) *ssa.Operation {
		return &ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Form: ssa.FormMap, Key: key, Elem: elem}, Args: []ssa.Value{1}, Results: []ssa.Value{2}}
	}
	offset := func(o uintptr) int16 { return int16(o) }
	tableOf := func(ref asm.VReg, addr int) []asm.Instruction {
		return []asm.Instruction{
			target.SBFX(vr(addr), ref, 0, 32), target.LSLI(vr(addr), vr(addr), 4),
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(addr), target.X16, vr(addr)),
			target.LDR(vr(addr+1), vr(addr), int16(jit.OffsetData)),
		}
	}
	words := func(w walk) []asm.Instruction {
		return []asm.Instruction{
			target.LDR(vr(w.ctrl), vr(w.data), offset(jit.OffsetMapCtrl)),
			target.LDR(vr(w.slots), vr(w.data), offset(jit.OffsetMapSlots)),
			target.LDR(vr(w.mask), vr(w.data), offset(jit.OffsetMapMask)),
		}
	}
	// home hashes the key into the home slot (w.index) and the control byte a full
	// slot holding it carries (w.tag): the seven hash bits below the slot's.
	home := func(w walk, key asm.Reg) []asm.Instruction {
		rows := []asm.Instruction{target.LDR(target.X16, vr(w.data), offset(jit.OffsetMapSeed)), target.EOR(vr(w.tag), key, target.X16)}
		rows = append(rows, target.LDI(target.X17, 0x9E3779B97F4A7C15)...)
		return append(rows,
			target.MUL(vr(w.tag), vr(w.tag), target.X17),
			target.LDR(target.X16, vr(w.data), offset(jit.OffsetMapShift)),
			target.LSR(vr(w.index), vr(w.tag), target.X16),
			target.AND(vr(w.index), vr(w.index), vr(w.mask)),
			target.SUBI(target.X16, target.X16, 7),
			target.LSR(vr(w.tag), vr(w.tag), target.X16),
			target.ANDI(vr(w.tag), vr(w.tag), 0x7F),
			target.ORRI(vr(w.tag), vr(w.tag), 0x80),
		)
	}
	// probe walks from the home slot. An empty slot goes to miss, a slot whose
	// control byte is the tag and whose key matches to hit, after the content
	// check a String key adds for a different word; any other slot, a
	// dead one included, is passed over. A set's probe also keeps the first dead
	// slot it passes in free.
	probe := func(w walk, key asm.Reg, loop, next, hit, miss asm.Label, content ...asm.Instruction) []asm.Instruction {
		rows := []asm.Instruction{
			target.LDRBR(vr(w.state), vr(w.ctrl), vr(w.index)), target.CBZLabel(vr(w.state), miss),
			target.CMP(vr(w.state), vr(w.tag)), target.BCondLabel(target.OpBNE, next),
			target.LSLI(vr(w.slot), vr(w.index), 4), target.ADD(vr(w.slot), vr(w.slots), vr(w.slot)),
			target.LDR(vr(w.word), vr(w.slot), 0), target.CMP(vr(w.word), key), target.BCondLabel(target.OpBEQ, hit),
		}
		rows = append(rows, content...)
		if w.free != 0 {
			step := next + 1
			if w.step != 0 {
				step = w.step
			}
			rows = append(rows,
				target.CMPI(vr(w.state), jit.SlotDead), target.BCondLabel(target.OpBNE, step),
				target.CMPI(vr(w.free), 0), target.BCondLabel(target.OpBGE, step),
				target.MOV(vr(w.free), vr(w.index)),
			)
		}
		return append(rows, target.ADDI(vr(w.index), vr(w.index), 1), target.AND(vr(w.index), vr(w.index), vr(w.mask)), target.BLabel(loop))
	}
	retained := func(ref asm.VReg, skip asm.Label) []asm.Instruction {
		return slices.Concat(
			[]asm.Instruction{target.LSRI(target.X16, ref, 49)},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, skip),
				target.SBFX(target.X17, ref, 0, 32),
			},
			counter,
			[]asm.Instruction{target.ADDI(target.X17, target.X17, 1), target.STR(target.X17, target.X16, 0)},
		)
	}
	released := func(old asm.VReg) []asm.Instruction {
		return slices.Concat(
			[]asm.Instruction{target.LSRI(target.X16, old, 49)},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, resume),
				target.SBFX(target.X17, old, 0, 32),
				target.CBZLabel(target.X17, resume),
			},
			counter,
			[]asm.Instruction{
				target.CMPI(target.X17, 1),
				target.BCondLabel(target.OpBLE, exit),
				target.SUBI(target.X17, target.X17, 1),
				target.STR(target.X17, target.X16, 0),
			},
		)
	}
	// get is map.get or map.lookup; hit, miss, and done are labels 2, 3, and 4, and
	// the probe's loop and next 5 and 6. The heap slot is vr(1), the table vr(2).
	get := func(ref, dst asm.VReg, key asm.Reg, keyRows []asm.Instruction, flag asm.VReg, extra []asm.Instruction) []asm.Instruction {
		w := layout(2, 3+keyRegs(keyRows), false)
		rows := slices.Concat(tableOf(ref, 1), keyRows, words(w), home(w, key), probe(w, key, 5, 6, 2, 3))
		rows = append(rows, target.LDR(dst, vr(w.slot), 8))
		if flag != (asm.VReg{}) {
			rows = append(rows, target.LDI(flag, 1)...)
		}
		rows = append(rows, target.BLabel(4), target.LDR(dst, vr(2), offset(jit.OffsetMapZero)))
		if flag != (asm.VReg{}) {
			rows = append(rows, target.LDI(flag, 0)...)
		}
		return append(rows, extra...)
	}
	// set is map.set. A value that boxes into a register (valueRows) is vr(1), so
	// the heap slot follows it; labels hit and empty are 2 and 3, and the probe's
	// loop, next, and the dead-slot check's step 4, 5, and 6. The table is only
	// changed once the order has room for the insert, else the set bridges
	// untouched; an insert fills the first dead slot the probe passed, else the
	// empty slot that ended it, with the tag, the key, and the value, appends the
	// slot to the order, and points the slot back at its position.
	set := func(ref asm.VReg, key asm.Reg, keyRows, valueRows []asm.Instruction, value asm.Reg, release bool) []asm.Instruction {
		first := 1 + min(len(valueRows), 1)
		w := layout(first+1, first+2+keyRegs(keyRows), true)
		used, want, limit, order, pos, count := w.word+1, w.word+2, w.word+3, w.word+4, w.word+5, w.word+6
		rows := slices.Concat(valueRows, tableOf(ref, first), keyRows, words(w), home(w, key), []asm.Instruction{target.MOVN(vr(w.free), 0, 0)}, probe(w, key, 4, 5, 2, 3))
		rows = append(rows,
			target.LDR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)), target.ADDI(vr(want), vr(used), 1), target.LSLI(vr(want), vr(want), 2),
			target.ADDI(vr(limit), vr(w.mask), 1), target.LSLI(target.X16, vr(limit), 1), target.ADD(vr(limit), vr(limit), target.X16),
			target.CMP(vr(want), vr(limit)), target.BCondLabel(target.OpBGT, exit),
			target.CMPI(vr(w.free), 0), target.CSEL(vr(w.index), vr(w.free), vr(w.index), target.CondGE),
			target.STRBR(vr(w.tag), vr(w.ctrl), vr(w.index)),
			target.LSLI(vr(w.slot), vr(w.index), 4), target.ADD(vr(w.slot), vr(w.slots), vr(w.slot)),
			target.STR(key, vr(w.slot), 0), target.STR(value, vr(w.slot), 8),
			target.LDR(vr(order), vr(w.data), offset(jit.OffsetMapOrder)), target.STRWR(vr(w.index), vr(order), vr(used)),
			target.LDR(vr(pos), vr(w.data), offset(jit.OffsetMapPos)), target.STRWR(vr(used), vr(pos), vr(w.index)),
			target.ADDI(vr(used), vr(used), 1), target.STR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.ADDI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
			target.BLabel(resume),
		)
		if !release {
			return append(rows, target.STR(value, vr(w.slot), 8))
		}
		old := vr(count + 1)
		rows = append(rows, target.LDR(old, vr(w.slot), 8), target.STR(value, vr(w.slot), 8))
		return append(rows, released(old)...)
	}
	// del is map.delete; hit and done are labels 2 and 3, the probe's loop and next 4 and 5.
	del := func(ref asm.VReg, key asm.Reg, keyRows []asm.Instruction, release bool) []asm.Instruction {
		w := layout(2, 3+keyRegs(keyRows), false)
		rows := slices.Concat(tableOf(ref, 1), keyRows, words(w), home(w, key), probe(w, key, 4, 5, 2, 3))
		state, count := w.word+1, w.word+2
		rows = append(rows, target.LDI(vr(state), jit.SlotDead)...)
		rows = append(rows,
			target.STRBR(vr(state), vr(w.ctrl), vr(w.index)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.SUBI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
		)
		if !release {
			return rows
		}
		old := vr(count + 1)
		return append(append(rows, target.LDR(old, vr(w.slot), 8)), released(old)...)
	}
	// wide is the check that bridges an i64 key outside the inline range: the
	// table holds it as a heap cell native code neither compares nor creates.
	wide := func(key asm.VReg) []asm.Instruction {
		return []asm.Instruction{target.SBFX(target.X16, key, 0, types.VBits), target.CMP(target.X16, key), target.BCondLabel(target.OpBNE, exit)}
	}
	zext := func(key asm.VReg, into int) []asm.Instruction { return []asm.Instruction{target.UXTW(vr(into), key)} }
	// float is the word of an f32 or f64 key: a NaN branches to nan, else the
	// bits fold -0 to the word of +0 by zeroing a word whose low bits below the
	// sign are all clear.
	float := func(key asm.VReg, nan asm.Label, into int) []asm.Instruction {
		move, mask := target.FMOV(target.X16, key), uint64(1<<63-1)
		if key.Width() == asm.Width32 {
			move, mask = target.FMOV(target.W16, key), 1<<31-1
		}
		return []asm.Instruction{
			target.FCMP(key, key), target.BCondLabel(target.OpBVS, nan), move,
			target.TSTI(target.X16, mask), target.CSEL(vr(into), target.XZR, target.X16, target.CondEQ),
		}
	}
	box32 := func(value asm.VReg) []asm.Instruction {
		return append(boxed(types.KindI32, target.UXTW(target.X16, value)), target.MOV(vr(1), target.X16))
	}
	// checkRef branches to miss unless ref is a reference word.
	checkRef := func(ref asm.Reg, miss asm.Label) []asm.Instruction {
		rows := []asm.Instruction{target.LSRI(target.X16, ref, types.VBits)}
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindRef)>>types.VBits)...)
		return append(rows, target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBNE, miss))
	}
	// hashRows is String.Hash of the size n bytes at ptr, through the eleven
	// registers from from on, the last of them the hash: a loop of 16-byte mixes
	// over the first n-8 bytes of a string of 8 or more, then a final mix of its
	// last 8 bytes; two 4-byte loads for 4 to 7 bytes; and three bytes for 1 to 3.
	// Every load stays inside the string.
	hashRows := func(ptr, n asm.VReg, from int, loop, last, short, tiny, fold asm.Label) []asm.Instruction {
		k0, k1, h, a, b, e, q, t, u, v, base := vr(from), vr(from+1), vr(from+2), vr(from+3), vr(from+4), vr(from+5), vr(from+6), vr(from+7), vr(from+8), vr(from+9), vr(from+10)
		rows := slices.Concat(target.LDI(k0, types.HashMix0), target.LDI(k1, types.HashMix1), target.LDI(target.X17, types.Hashkey()))
		return append(rows,
			target.EOR(h, target.X17, n), target.MOVZ(a, 0, 0), target.MOVZ(b, 0, 0),
			target.CMPI(n, 8), target.BCondLabel(target.OpBCC, short),
			target.ADD(e, ptr, n), target.SUBI(e, e, 8), target.MOV(q, ptr),
			target.CMP(q, e), target.BCondLabel(target.OpBCS, last),
			target.LDR(t, q, 0), target.EOR(t, t, k1), target.EOR(u, h, k0), target.MUL(v, t, u), target.UMULH(h, t, u), target.EOR(h, h, v),
			target.ADDI(q, q, 8), target.BLabel(loop),
			target.LDR(a, e, 0), target.BLabel(fold),
			target.CMPI(n, 4), target.BCondLabel(target.OpBCC, tiny),
			target.LDR(target.W16, ptr, 0), target.MOV(a, target.X16),
			target.ADD(t, ptr, n), target.SUBI(t, t, 4), target.LDR(target.W16, t, 0), target.MOV(b, target.X16), target.BLabel(fold),
			target.CBZLabel(n, fold),
			target.LDRB(a, ptr, 0), target.LSLI(a, a, 16),
			target.LSRI(t, n, 1), target.LDRBR(t, ptr, t), target.LSLI(t, t, 8), target.ORR(a, a, t),
			target.SUBI(t, n, 1), target.LDRBR(t, ptr, t), target.ORR(a, a, t),
			target.EOR(a, a, k1), target.EOR(b, b, h), target.EOR(b, b, k0),
			target.MUL(v, a, b), target.UMULH(t, a, b), target.EOR(base, v, t),
		)
	}
	// stringKey derives a String key: it bridges a word that is no reference or
	// no String, loads the bytes through the registers from from on (heap slot,
	// data, size, pointer) and hashes them through the eleven that follow; the
	// hash is vr(from+14).
	stringKey := func(ref asm.Reg, from int, labels [5]asm.Label) []asm.Instruction {
		return slices.Concat(
			checkRef(ref, exit),
			text(ref, vr(from), vr(from+1), vr(from+2), vr(from+3), exit),
			hashRows(vr(from+3), vr(from+2), from+4, labels[0], labels[1], labels[2], labels[3], labels[4]),
		)
	}
	// constKey derives a constant String key: it loads the bytes through the
	// registers from from on (heap slot, data, size, pointer) with no check, and
	// the hash is the immediate in vr(from+4).
	constKey := func(ref asm.Reg, from int, hash uint64) []asm.Instruction {
		rows := []asm.Instruction{
			target.SBFX(vr(from), ref, 0, 32), target.LSLI(vr(from), vr(from), 4),
			target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
			target.ADD(vr(from), target.X16, vr(from)),
			target.LDR(vr(from+1), vr(from), int16(jit.OffsetData)),
			target.LDR(vr(from+2), vr(from+1), int16(jit.OffsetStringLen)),
			target.LDR(vr(from+3), vr(from+1), 0),
		}
		return append(rows, target.LDI(vr(from+4), hash)...)
	}
	// content is the probe's check of a tag hit whose word differs from the key's:
	// a slot holding a String of equal content is a hit.
	content := func(word asm.VReg, from int, ptr, size asm.VReg, next, hit asm.Label, labels [5]asm.Label) []asm.Instruction {
		return slices.Concat(
			checkRef(word, next),
			text(word, vr(from), vr(from+1), vr(from+2), vr(from+3), next),
			[]asm.Instruction{target.MOV(vr(from+4), ptr)},
			strcmp([2]asm.VReg{vr(from + 2), size}, [2]asm.VReg{vr(from + 3), vr(from + 4)}, vr(from+5), labels[0], labels[1], labels[2], labels[3], labels[4], true),
			[]asm.Instruction{target.BCondLabel(target.OpBEQ, hit)},
		)
	}
	// stringGet is map.get or map.lookup of a String key in a map of i32
	// elements: the key takes registers 3 to 17, the table 18 to 20, the home
	// slot 21 and 22, and the probe 23 to 25, then the content check 26 to 31.
	// Labels hit, miss, and done are 2 to 4, the hash 5 to 9, the probe 10 and
	// 11, and the string comparison 12 to 16.
	stringGet := func(ref, dst asm.VReg, key asm.Reg, flag asm.VReg, keyRows []asm.Instruction, base, ctrl int, loop asm.Label) []asm.Instruction {
		w := layout(2, ctrl, false)
		rows := slices.Concat(tableOf(ref, 1), keyRows, words(w), home(w, vr(base)),
			probe(w, key, loop, loop+1, 2, 3, content(vr(w.word), w.word+1, vr(6), vr(5), loop+1, 2, [5]asm.Label{loop + 2, loop + 3, loop + 4, loop + 5, loop + 6})...))
		rows = append(rows, target.LDR(dst, vr(w.slot), 8))
		if flag != (asm.VReg{}) {
			rows = append(rows, target.LDI(flag, 1)...)
		}
		rows = append(rows, target.BLabel(4), target.LDR(dst, vr(2), offset(jit.OffsetMapZero)))
		if flag != (asm.VReg{}) {
			rows = append(rows, target.LDI(flag, 0)...)
		}
		return rows
	}
	// hashedGet is stringGet of a String key hashed inline (see stringKey).
	hashedGet := func(ref, dst asm.VReg, key asm.Reg, flag asm.VReg) []asm.Instruction {
		return stringGet(ref, dst, key, flag, stringKey(key, 3, [5]asm.Label{5, 6, 7, 8, 9}), 17, 18, 10)
	}
	// constGet is stringGet of a constant String key (see constKey): the key
	// takes registers 3 to 7, the table 8 to 10, and the labels are the
	// probe's 5 and 6 and the string comparison's 7 to 11.
	constGet := func(ref, dst asm.VReg, key asm.Reg, flag asm.VReg, hash uint64) []asm.Instruction {
		return stringGet(ref, dst, key, flag, constKey(key, 3, hash), 7, 8, 5)
	}
	// stringSet is map.set of a String key and an i32 value in vr(1): the key
	// takes registers 4 to 18, the table 19 to 21, the home slot 22 and 23, free
	// 24, and the probe 25 to 27, then the content check 28 to 33. A miss counts
	// one more reference to the key. Labels hit and empty are 2 and 3, the hash
	// 4 to 8, the probe 9 and 10, the string comparison 11 to 15, the dead-slot
	// step 16, and the retain's skip 17.
	stringSet := func(ref asm.VReg, key asm.Reg, value asm.Reg, keyRows []asm.Instruction, base, ctrl int, loop asm.Label) []asm.Instruction {
		w := layout(3, ctrl, true)
		w.step = loop + 7
		used, want, limit, order, pos, count := w.word+7, w.word+8, w.word+9, w.word+10, w.word+11, w.word+12
		rows := slices.Concat(box32(reg(i32, 4)), tableOf(ref, 2), keyRows, words(w), home(w, vr(base)),
			[]asm.Instruction{target.MOVN(vr(w.free), 0, 0)},
			probe(w, key, loop, loop+1, 2, 3, content(vr(w.word), w.word+1, vr(7), vr(6), loop+1, 2, [5]asm.Label{loop + 2, loop + 3, loop + 4, loop + 5, loop + 6})...))
		rows = append(rows,
			target.LDR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)), target.ADDI(vr(want), vr(used), 1), target.LSLI(vr(want), vr(want), 2),
			target.ADDI(vr(limit), vr(w.mask), 1), target.LSLI(target.X16, vr(limit), 1), target.ADD(vr(limit), vr(limit), target.X16),
			target.CMP(vr(want), vr(limit)), target.BCondLabel(target.OpBGT, exit),
			target.CMPI(vr(w.free), 0), target.CSEL(vr(w.index), vr(w.free), vr(w.index), target.CondGE),
			target.STRBR(vr(w.tag), vr(w.ctrl), vr(w.index)),
			target.LSLI(vr(w.slot), vr(w.index), 4), target.ADD(vr(w.slot), vr(w.slots), vr(w.slot)),
			target.STR(key, vr(w.slot), 0), target.STR(value, vr(w.slot), 8),
			target.LDR(vr(order), vr(w.data), offset(jit.OffsetMapOrder)), target.STRWR(vr(w.index), vr(order), vr(used)),
			target.LDR(vr(pos), vr(w.data), offset(jit.OffsetMapPos)), target.STRWR(vr(used), vr(pos), vr(w.index)),
			target.ADDI(vr(used), vr(used), 1), target.STR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.ADDI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
		)
		rows = append(rows, retained(key.(asm.VReg), loop+8)...)
		return append(rows, target.BLabel(resume), target.STR(value, vr(w.slot), 8))
	}
	// hashedSet is stringSet of a String key hashed inline (see stringKey).
	hashedSet := func(ref asm.VReg, key asm.Reg, value asm.Reg) []asm.Instruction {
		return stringSet(ref, key, value, stringKey(key, 4, [5]asm.Label{4, 5, 6, 7, 8}), 18, 19, 9)
	}
	// constSet is stringSet of a constant String key (see constKey): the key
	// takes registers 4 to 8, the table 9 to 11, and the labels are the probe's
	// 4 and 5, the string comparison's 6 to 10, the dead-slot step 11, and the
	// retain's skip 12.
	constSet := func(ref asm.VReg, key asm.Reg, value asm.Reg, hash uint64) []asm.Instruction {
		return stringSet(ref, key, value, constKey(key, 4, hash), 8, 9, 4)
	}
	// stringDelete is map.delete of a String key, which the operation adopts: a
	// hit releases the stored key, then the removed element when release, and
	// either way the incoming key last. The key takes registers 3 to 17, as in
	// stringGet, and the labels are hit and done 2 and 3, the hash 4 to 8, the
	// probe 9 and 10, and the string comparison 11 to 15.
	stringDelete := func(ref asm.VReg, key asm.VReg, release bool) []asm.Instruction {
		w := layout(2, 18, false)
		state, count, stored, old := w.word+7, w.word+8, w.word+9, w.word+10
		rows := slices.Concat(tableOf(ref, 1), stringKey(key, 3, [5]asm.Label{4, 5, 6, 7, 8}), words(w), home(w, vr(17)),
			probe(w, key, 9, 10, 2, 3, content(vr(w.word), w.word+1, vr(6), vr(5), 10, 2, [5]asm.Label{11, 12, 13, 14, 15})...))
		rows = append(rows, target.LDI(vr(state), jit.SlotDead)...)
		rows = append(rows,
			target.STRBR(vr(state), vr(w.ctrl), vr(w.index)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.SUBI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
			target.LDR(vr(stored), vr(w.slot), 0),
		)
		rows = append(rows, released(vr(stored))...)
		if release {
			rows = append(rows, target.LDR(vr(old), vr(w.slot), 8))
			rows = append(rows, released(vr(old))...)
		}
		return append(rows, released(key)...)
	}
	// anyWord is the word of a key operand of a scalar kind in an any map, as the
	// interpreter boxes it: the integer kinds retag to an i32, a float keeps its
	// bits (an f32 with its tag), and an inline i64 is its payload and tag.
	anyWord := func(tag types.Kind, rows ...asm.Instruction) []asm.Instruction {
		rows = append(rows, target.LDI(target.X17, types.Tag(tag))...)
		return append(rows, target.ORR(vr(3), vr(3), target.X17))
	}
	// anyKey derives the key of a reference operand of an any map, which is a
	// boxed word of unknown kind: the registers from from on are the word, the
	// base hash, the pointer, and the size (-1 for a key that is no String), then
	// the String's heap slot, data, size, and pointer, and the eleven of its hash.
	// The labels are the dispatch's six (the i1 path's last), then the hash's five; nan is where a NaN goes.
	anyKey := func(ref asm.Reg, from int, labels [6]asm.Label, hashLabels [5]asm.Label, nan asm.Label) []asm.Instruction {
		word, base, ptr, size := vr(from), vr(from+1), vr(from+2), vr(from+3)
		small, refer, same, other, done, flag := labels[0], labels[1], labels[2], labels[3], labels[4], labels[5]
		rows := []asm.Instruction{
			target.MOVN(size, 0, 0), target.MOVZ(ptr, 0, 0),
			target.LSRI(target.X16, ref, 49),
		}
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32)>>49)...)
		rows = append(rows,
			target.SUB(target.X16, target.X16, target.X17),
			target.CMPI(target.X16, 1), target.BCondLabel(target.OpBLS, small),
			target.CMNI(target.X16, 1), target.BCondLabel(target.OpBEQ, refer),
			target.CMNI(target.X16, 2), target.BCondLabel(target.OpBEQ, same),
			target.CMNI(target.X16, 3), target.BCondLabel(target.OpBEQ, exit),
			target.CMPI(target.X16, 2), target.BCondLabel(target.OpBEQ, flag),
			target.LSLI(target.X16, ref, 1),
		)
		rows = append(rows, target.LDI(target.X17, 0xFFE0<<48)...)
		rows = append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBHI, nan),
			target.CMPI(target.X16, 0), target.CSEL(word, target.XZR, ref, target.CondEQ),
			target.MOV(base, word), target.BLabel(done),
			// small
			target.UXTW(word, ref),
		)
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32))...)
		rows = append(rows,
			target.ORR(word, word, target.X17), target.MOV(base, word), target.BLabel(done),
			// i1
			target.TSTI(ref, types.VMask), target.CSET(word, target.CondNE),
		)
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32))...)
		rows = append(rows,
			target.ORR(word, word, target.X17), target.MOV(base, word), target.BLabel(done),
			// same
			target.MOV(word, ref), target.MOV(base, ref), target.BLabel(done),
			// reference
			target.SBFX(target.X16, ref, 0, 32), target.CBZLabel(target.X16, exit),
		)
		rows = append(rows, text(ref, vr(from+4), vr(from+5), vr(from+6), vr(from+7), other)...)
		rows = append(rows, target.MOV(ptr, vr(from+7)), target.MOV(size, vr(from+6)), target.MOV(word, ref))
		rows = append(rows, hashRows(vr(from+7), vr(from+6), from+8, hashLabels[0], hashLabels[1], hashLabels[2], hashLabels[3], hashLabels[4])...)
		rows = append(rows, target.MOV(base, vr(from+18)), target.BLabel(done))
		rows = append(rows, target.LDI(target.X17, uint64(jit.Itab(types.I64(0))))...)
		return append(rows,
			target.CMP(target.X16, target.X17), target.BCondLabel(target.OpBEQ, exit),
			target.MOV(word, ref), target.MOV(base, ref),
		)
	}
	// anyContent is the probe's content check of an any map: a key that is no
	// String has the size -1 and matches by word alone.
	anyContent := func(word asm.VReg, from int, ptr, size asm.VReg, next, hit asm.Label, labels [5]asm.Label) []asm.Instruction {
		return append([]asm.Instruction{target.CMPI(size, 0), target.BCondLabel(target.OpBLT, next)}, content(word, from, ptr, size, next, hit, labels)...)
	}
	// anyGet is map.get of a reference operand in an any map: the key takes
	// registers 3 to 21 and its labels 5 to 15, the table 22 to 24, the home slot
	// 25 and 26, and the probe 27 to 29, then the content check 30 to 35; the
	// probe's labels are 16 and 17 and the string comparison's 18 to 22.
	anyGet := func(ref, dst asm.VReg, key asm.Reg) []asm.Instruction {
		w := layout(2, 22, false)
		rows := slices.Concat(tableOf(ref, 1), anyKey(key, 3, [6]asm.Label{5, 6, 7, 8, 9, 10}, [5]asm.Label{11, 12, 13, 14, 15}, 3), words(w), home(w, vr(4)),
			probe(w, vr(3), 16, 17, 2, 3, anyContent(vr(w.word), w.word+1, vr(5), vr(6), 17, 2, [5]asm.Label{18, 19, 20, 21, 22})...))
		return append(rows, target.LDR(dst, vr(w.slot), 8), target.BLabel(4), target.LDR(dst, vr(2), offset(jit.OffsetMapZero)))
	}
	// anySet is map.set of a reference operand in an any map and an i32 value in
	// vr(1): the key takes registers 4 to 22 and the labels 4 to 14, the table 23
	// to 25, the probe's labels are 15 and 16, the string comparison's 17 to 21, the
	// dead-slot step 22, and the retain's skip 23. A miss counts one more reference
	// to the key word.
	anySet := func(ref asm.VReg, key asm.Reg, value asm.Reg) []asm.Instruction {
		w := layout(3, 23, true)
		w.step = 22
		used, want, limit, order, pos, count := w.word+7, w.word+8, w.word+9, w.word+10, w.word+11, w.word+12
		rows := slices.Concat(box32(reg(i32, 4)), tableOf(ref, 2), anyKey(key, 4, [6]asm.Label{4, 5, 6, 7, 8, 9}, [5]asm.Label{10, 11, 12, 13, 14}, exit), words(w), home(w, vr(5)),
			[]asm.Instruction{target.MOVN(vr(w.free), 0, 0)},
			probe(w, vr(4), 15, 16, 2, 3, anyContent(vr(w.word), w.word+1, vr(6), vr(7), 16, 2, [5]asm.Label{17, 18, 19, 20, 21})...))
		rows = append(rows,
			target.LDR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)), target.ADDI(vr(want), vr(used), 1), target.LSLI(vr(want), vr(want), 2),
			target.ADDI(vr(limit), vr(w.mask), 1), target.LSLI(target.X16, vr(limit), 1), target.ADD(vr(limit), vr(limit), target.X16),
			target.CMP(vr(want), vr(limit)), target.BCondLabel(target.OpBGT, exit),
			target.CMPI(vr(w.free), 0), target.CSEL(vr(w.index), vr(w.free), vr(w.index), target.CondGE),
			target.STRBR(vr(w.tag), vr(w.ctrl), vr(w.index)),
			target.LSLI(vr(w.slot), vr(w.index), 4), target.ADD(vr(w.slot), vr(w.slots), vr(w.slot)),
			target.STR(vr(4), vr(w.slot), 0), target.STR(value, vr(w.slot), 8),
			target.LDR(vr(order), vr(w.data), offset(jit.OffsetMapOrder)), target.STRWR(vr(w.index), vr(order), vr(used)),
			target.LDR(vr(pos), vr(w.data), offset(jit.OffsetMapPos)), target.STRWR(vr(used), vr(pos), vr(w.index)),
			target.ADDI(vr(used), vr(used), 1), target.STR(vr(used), vr(w.data), offset(jit.OffsetMapUsed)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.ADDI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
		)
		rows = append(rows, retained(vr(4), 23)...)
		return append(rows, target.BLabel(resume), target.STR(value, vr(w.slot), 8))
	}
	// anyDelete is map.delete of a reference operand in an any map: a hit releases
	// the stored key, then the incoming operand it adopts, which a miss releases too.
	anyDelete := func(ref asm.VReg, key asm.VReg) []asm.Instruction {
		w := layout(2, 22, false)
		state, count, stored := w.word+7, w.word+8, w.word+9
		rows := slices.Concat(tableOf(ref, 1), anyKey(key, 3, [6]asm.Label{4, 5, 6, 7, 8, 9}, [5]asm.Label{10, 11, 12, 13, 14}, 3), words(w), home(w, vr(4)),
			probe(w, vr(3), 15, 16, 2, 3, anyContent(vr(w.word), w.word+1, vr(5), vr(6), 16, 2, [5]asm.Label{17, 18, 19, 20, 21})...))
		rows = append(rows, target.LDI(vr(state), jit.SlotDead)...)
		rows = append(rows,
			target.STRBR(vr(state), vr(w.ctrl), vr(w.index)),
			target.LDR(vr(count), vr(w.data), offset(jit.OffsetMapCount)), target.SUBI(vr(count), vr(count), 1), target.STR(vr(count), vr(w.data), offset(jit.OffsetMapCount)),
			target.LDR(vr(stored), vr(w.slot), 0),
		)
		rows = append(rows, released(vr(stored))...)
		return append(rows, released(key)...)
	}
	literal := types.String("constant-key")
	ref, mapped := ssa.TypeRef, func(kind ssa.Op, results int, args ...ssa.Value) ssa.Operation {
		op := ssa.Operation{Op: kind, Args: args}
		for i := range results {
			op.Results = append(op.Results, ssa.Value(len(args)+2+i))
		}
		return op
	}
	tests = append(tests, []test{
		{
			name: "map.len loads the count of the guarded map",
			regs: regs{1: ref, 2: ref, 3: i32}, guard: intMap(types.KindI32, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapLen, Args: []ssa.Value{2}, Results: []ssa.Value{3}},
			rows: append(tableOf(reg(ref, 2), 1), target.LDR(reg(i32, 3), vr(2), offset(jit.OffsetMapCount))), lower: true,
		},
		{
			name: "map.get of an i32 key zero-extends it, probes, and returns the element or the map's zero",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i32}, guard: intMap(types.KindI32, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), zext(reg(i32, 3), 3), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an i8 key keeps its low byte",
			regs: regs{1: ref, 2: ref, 3: ssa.TypeI8, 4: f64}, guard: intMap(types.KindI8, types.KindF64),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(f64, 4), vr(3), []asm.Instruction{target.UXTB(vr(3), reg(ssa.TypeI8, 3))}, asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an i1 key compares it against zero",
			regs: regs{1: ref, 2: ref, 3: ssa.TypeI1, 4: f32}, guard: intMap(types.KindI1, types.KindF32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(f32, 4), vr(3), []asm.Instruction{target.CMPI(reg(ssa.TypeI1, 3), 0), target.CSET(vr(3), target.CondNE)}, asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an f64 key sends a NaN to the miss and folds -0 into the word of +0",
			regs: regs{1: ref, 2: ref, 3: f64, 4: i32}, guard: intMap(types.KindF64, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), float(reg(f64, 3), 3, 3), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.lookup of an f32 key moves its 32 bits and folds -0 above the sign",
			regs: regs{1: ref, 2: ref, 3: f32, 4: f64, 5: ssa.TypeI1}, guard: intMap(types.KindF32, types.KindF64),
			op:   mapped(ssa.OpMapLookup, 2, 2, 3),
			rows: get(reg(ref, 2), reg(f64, 4), vr(3), float(reg(f32, 3), 3, 3), reg(ssa.TypeI1, 5), nil), lower: true,
		},
		{
			name: "map.set of an f64 key bridges a NaN before it touches the table",
			regs: regs{1: ref, 2: ref, 3: f64, 4: i32}, guard: intMap(types.KindF64, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: set(reg(ref, 2), vr(4), float(reg(f64, 3), exit, 4), box32(reg(i32, 4)), vr(1), false), lower: true,
		},
		{
			name: "map.set of an f32 key bridges a NaN before it touches the table",
			regs: regs{1: ref, 2: ref, 3: f32, 4: i32}, guard: intMap(types.KindF32, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: set(reg(ref, 2), vr(4), float(reg(f32, 3), exit, 4), box32(reg(i32, 4)), vr(1), false), lower: true,
		},
		{
			name: "map.delete of an f64 key ends at a NaN, which no slot holds",
			regs: regs{1: ref, 2: ref, 3: f64}, guard: intMap(types.KindF64, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: del(reg(ref, 2), vr(3), float(reg(f64, 3), 3, 3), false), lower: true,
		},
		{
			name: "map.get declines an f64 operand of an f32 map",
			regs: regs{1: ref, 2: ref, 3: f64, 4: i32}, guard: intMap(types.KindF32, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.get of an i64 key bridges outside the inline range and else hashes the register itself",
			regs: regs{1: ref, 2: ref, 3: i64, 4: ssa.TypeI1}, guard: intMap(types.KindI64, types.KindI1),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(ssa.TypeI1, 4), reg(i64, 3), wide(reg(i64, 3)), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of a ref element retains the result",
			regs: regs{1: ref, 2: ref, 3: i32, 4: ref}, guard: intMap(types.KindI32, types.KindRef),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(ref, 4), vr(3), zext(reg(i32, 3), 3), asm.VReg{}, retained(reg(ref, 4), 7)), lower: true,
		},
		{
			name: "map.lookup also returns whether the key was present",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i32, 5: ssa.TypeI1}, guard: intMap(types.KindI32, types.KindI32),
			op:   mapped(ssa.OpMapLookup, 2, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), zext(reg(i32, 3), 3), reg(ssa.TypeI1, 5), nil), lower: true,
		},
		{
			name: "map.set stores the boxed value in the first dead slot of the probe or the empty slot that ended it and appends it to the order, bridging when the table must rehash",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i32}, guard: intMap(types.KindI32, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: set(reg(ref, 2), vr(4), zext(reg(i32, 3), 4), box32(reg(i32, 4)), vr(1), false), lower: true,
		},
		{
			name: "map.set of a ref element adopts the value and releases the element it replaced",
			regs: regs{1: ref, 2: ref, 3: i32, 4: ref}, guard: intMap(types.KindI32, types.KindRef),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: set(reg(ref, 2), vr(3), zext(reg(i32, 3), 3), nil, reg(ref, 4), true), lower: true,
		},
		{
			name: "map.set of an i64 key bridges outside the inline range before it touches the table",
			regs: regs{1: ref, 2: ref, 3: i64, 4: i32}, guard: intMap(types.KindI64, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: set(reg(ref, 2), reg(i64, 3), wide(reg(i64, 3)), box32(reg(i32, 4)), vr(1), false), lower: true,
		},
		{
			name: "map.delete marks the slot dead and counts it out",
			regs: regs{1: ref, 2: ref, 3: i32}, guard: intMap(types.KindI32, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: del(reg(ref, 2), vr(3), zext(reg(i32, 3), 3), false), lower: true,
		},
		{
			name: "map.delete of a ref element also releases the removed element",
			regs: regs{1: ref, 2: ref, 3: i64}, guard: intMap(types.KindI64, types.KindRef),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: del(reg(ref, 2), reg(i64, 3), wide(reg(i64, 3)), true), lower: true,
		},
		{
			name: "map.get of a string key checks and hashes its bytes, and compares content on a tag hit of another word",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindRef, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: hashedGet(reg(ref, 2), reg(i32, 4), reg(ref, 3), asm.VReg{}), lower: true,
		},
		{
			name: "map.lookup of a string key also yields whether it was found",
			regs: regs{1: ref, 2: ref, 3: ref, 4: f64, 5: ssa.TypeI1}, guard: intMap(types.KindRef, types.KindF64),
			op:   mapped(ssa.OpMapLookup, 2, 2, 3),
			rows: hashedGet(reg(ref, 2), reg(f64, 4), reg(ref, 3), reg(ssa.TypeI1, 5)), lower: true,
		},
		{
			name: "map.set of a string key stores its word and counts one more reference to it on a miss only",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindRef, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: hashedSet(reg(ref, 2), reg(ref, 3), vr(1)), lower: true,
		},
		{
			name: "map.get of a constant string key loads its bytes unchecked and takes its hash as an immediate",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindRef, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3), objects: map[ssa.Value]transform.Object{3: {String: &literal}},
			rows: constGet(reg(ref, 2), reg(i32, 4), reg(ref, 3), asm.VReg{}, literal.Hash()), lower: true,
		},
		{
			name: "map.set of a constant string key loads its bytes unchecked and takes its hash as an immediate",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindRef, types.KindI32),
			op: ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}}, objects: map[ssa.Value]transform.Object{3: {String: &literal}},
			rows: constSet(reg(ref, 2), reg(ref, 3), vr(1), literal.Hash()), lower: true,
		},
		{
			name: "map.get of a constant string key in an any map skips the dispatch",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3), objects: map[ssa.Value]transform.Object{3: {String: &literal}},
			rows: constGet(reg(ref, 2), reg(i32, 4), reg(ref, 3), asm.VReg{}, literal.Hash()), lower: true,
		},
		{
			name: "map.delete of a string key releases the stored key, then the incoming key it adopts",
			regs: regs{1: ref, 2: ref, 3: ref}, guard: intMap(types.KindRef, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: stringDelete(reg(ref, 2), reg(ref, 3), false), lower: true,
		},
		{
			name: "map.delete of a string key releases the stored key, then the removed element, then the incoming key",
			regs: regs{1: ref, 2: ref, 3: ref}, guard: intMap(types.KindRef, types.KindRef),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: stringDelete(reg(ref, 2), reg(ref, 3), true), lower: true,
		},
		{
			name: "map.get of an i1 key in an any map retags it to an i32 holding 0 or 1",
			regs: regs{1: ref, 2: ref, 3: ssa.TypeI1, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), anyWord(types.KindI32, target.CMPI(reg(ssa.TypeI1, 3), 0), target.CSET(vr(3), target.CondNE)), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an i8 key in an any map sign-extends its low byte, then retags it to an i32",
			regs: regs{1: ref, 2: ref, 3: ssa.TypeI8, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), anyWord(types.KindI32, target.SXTB(iv(4, asm.Width32), reg(ssa.TypeI8, 3)), target.UXTW(vr(3), iv(4, asm.Width32))), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an i32 key in an any map zero-extends it and retags it to an i32",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), anyWord(types.KindI32, target.UXTW(vr(3), reg(i32, 3))), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an i64 key in an any map bridges outside the inline range and else is its payload with the i64 tag",
			regs: regs{1: ref, 2: ref, 3: i64, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), anyWord(types.KindI64, slices.Concat(wide(reg(i64, 3)), []asm.Instruction{target.ANDI(vr(3), reg(i64, 3), types.VMask)})...), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an f32 key in an any map folds -0 and tags the bits as an f32",
			regs: regs{1: ref, 2: ref, 3: f32, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), anyWord(types.KindF32, float(reg(f32, 3), 3, 3)...), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of an f64 key in an any map is the float map's word",
			regs: regs{1: ref, 2: ref, 3: f64, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: get(reg(ref, 2), reg(i32, 4), vr(3), float(reg(f64, 3), 3, 3), asm.VReg{}, nil), lower: true,
		},
		{
			name: "map.get of a reference key in an any map dispatches on its tag and compares content only for a String",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   mapped(ssa.OpMapGet, 1, 2, 3),
			rows: anyGet(reg(ref, 2), reg(i32, 4), reg(ref, 3)), lower: true,
		},
		{
			name: "map.set of a reference key in an any map counts one more reference to its word on a miss only",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindAny, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
			rows: anySet(reg(ref, 2), reg(ref, 3), vr(1)), lower: true,
		},
		{
			name: "map.delete of a reference key in an any map releases the stored key, then the incoming key it adopts",
			regs: regs{1: ref, 2: ref, 3: ref}, guard: intMap(types.KindAny, types.KindI32),
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: anyDelete(reg(ref, 2), reg(ref, 3)), lower: true,
		},
		{
			name: "map.delete of a scalar key in an any map does not release the stored key",
			regs: regs{1: ref, 2: ref, 3: i32}, guard: intMap(types.KindAny, types.KindI32),
			op: ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{2, 3}},
			rows: func() []asm.Instruction {
				w := layout(2, 4, false)
				rows := slices.Concat(tableOf(reg(ref, 2), 1), anyWord(types.KindI32, target.UXTW(vr(3), reg(i32, 3))), words(w), home(w, vr(3)), probe(w, vr(3), 4, 5, 2, 3))
				rows = append(rows, target.LDI(vr(w.word+1), jit.SlotDead)...)
				rows = append(rows,
					target.STRBR(vr(w.word+1), vr(w.ctrl), vr(w.index)),
					target.LDR(vr(w.word+2), vr(w.data), offset(jit.OffsetMapCount)), target.SUBI(vr(w.word+2), vr(w.word+2), 1), target.STR(vr(w.word+2), vr(w.data), offset(jit.OffsetMapCount)),
				)
				return rows
			}(), lower: true,
		},
		{
			name: "map.get declines a scalar operand of a string map",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i32}, guard: intMap(types.KindRef, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.get declines a reference operand of an i32 map",
			regs: regs{1: ref, 2: ref, 3: ref, 4: i32}, guard: intMap(types.KindI32, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.get declines a map that carries no shape guard",
			regs: regs{1: ref, 3: i32, 4: i32},
			op:   mapped(ssa.OpMapGet, 1, 1, 3),
		},
		{
			name: "map.get declines a key whose type is not the map's key kind",
			regs: regs{1: ref, 2: ref, 3: i64, 4: i32}, guard: intMap(types.KindI32, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.get declines an i64 element, which is not a raw word",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i64}, guard: intMap(types.KindI32, types.KindI64),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.get declines a result of another kind than the map's element",
			regs: regs{1: ref, 2: ref, 3: i32, 4: f64}, guard: intMap(types.KindI32, types.KindI32),
			op: mapped(ssa.OpMapGet, 1, 2, 3),
		},
		{
			name: "map.set declines an i64 value, whose boxing may exit",
			regs: regs{1: ref, 2: ref, 3: i32, 4: i64}, guard: intMap(types.KindI32, types.KindRef),
			op: ssa.Operation{Op: ssa.OpMapSet, Args: []ssa.Value{2, 3, 4}},
		},
		{
			name: "map.delete declines a map that carries no shape guard",
			regs: regs{1: ref, 3: i32},
			op:   ssa.Operation{Op: ssa.OpMapDelete, Args: []ssa.Value{1, 3}},
		},
		{
			name: "map.len declines an array's shape",
			regs: regs{1: ref, 2: ref, 3: i32}, guard: &i32array,
			op: ssa.Operation{Op: ssa.OpMapLen, Args: []ssa.Value{2}, Results: []ssa.Value{3}},
		},
	}...)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, 0, true, tt.layout, nil)
			if tt.guard != nil {
				require.True(t, m.Lower(a, *tt.guard, tt.regs))
			}
			if tt.slice != nil {
				require.True(t, m.Lower(a, *tt.slice, tt.regs))
			}
			if tt.bound != nil {
				require.True(t, m.Lower(a, *tt.bound, tt.regs))
			}
			var s compile.Site = tt.regs
			if tt.fuse {
				s = fused{tt.regs}
			}
			if tt.consts != nil || tt.objects != nil {
				s = known{Site: s, words: tt.consts, objects: tt.objects}
			}
			start := len(a.Rows())
			require.Equal(t, tt.lower, m.Lower(a, tt.op, s))
			if tt.lower {
				require.Equal(t, tt.rows, a.Rows()[start:])
			}
		})
	}

	t.Run("map operations leave the table byte-identical to the Go table's", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		t.Run("i32 keys", func(t *testing.T) {
			mirror(t, types.TypeI32(), scalars(types.BoxI32, func(n int) int32 { return int32(n*7919 - 20000) }))
		})
		t.Run("i8 keys", func(t *testing.T) {
			mirror(t, types.TypeI8(), scalars(types.BoxI8, func(n int) int8 { return int8(n*37 - 90) }))
		})
		t.Run("i1 keys", func(t *testing.T) {
			mirror(t, types.TypeI1(), scalars(types.BoxI1, func(n int) bool { return n%2 == 0 }))
		})
		t.Run("i64 keys", func(t *testing.T) {
			mirror(t, types.TypeI64(), scalars(types.BoxI64, func(n int) int64 { return int64(n) << 33 }))
		})
		t.Run("f64 keys including -0 and +0", func(t *testing.T) {
			mirror(t, types.TypeF64(), scalars(types.BoxF64, func(n int) float64 {
				if n%4 < 2 {
					return math.Copysign(0, float64(1-n%4*2))
				}
				return float64(n)*1.5 - 20
			}))
		})
		t.Run("f32 keys including -0 and +0", func(t *testing.T) {
			mirror(t, types.TypeF32(), scalars(types.BoxF32, func(n int) float32 {
				if n%4 < 2 {
					return float32(math.Copysign(0, float64(1-n%4*2)))
				}
				return float32(n)*1.5 - 20
			}))
		})
		t.Run("string keys behind two refs of equal content", func(t *testing.T) {
			mirror(t, types.TypeString(), func(h *types.Heap, n int) []types.Boxed {
				text := strings.Repeat("k", n%19) + strconv.Itoa(n)
				return []types.Boxed{types.BoxRef(h.Add(types.String(text))), types.BoxRef(h.Add(types.String(strings.Clone(text))))}
			})
		})
		t.Run("constant string keys of every length", func(t *testing.T) {
			for _, typ := range []types.Type{types.TypeString(), types.TypeAny()} {
				mapType := types.NewMapType(typ, types.TypeI32())
				for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 24, 25, 33, 64} {
					text := edge(t, strings.Repeat("k", n)+strconv.Itoa(n))
					m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {String: &text}}}
					fn := function(t, []types.Type{mapType, types.TypeI32()}, nil, func(b *instr.Builder) {
						b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
					})
					f, _, err := transform.Translate(m, nil, 1, fn, 0)
					require.NoError(t, err)
					set, exits := lower(t, arm64.New(), f, fn, m.Objects, 0, false)

					for range 4 {
						heap := types.NewHeap(32, 0, types.Hooks{})
						heap.Add(nil)
						native := types.NewMap(heap, mapType, 1<<12)
						twin := native.Clone()
						addr := heap.Add(native)
						key := types.BoxRef(heap.Add(text))
						heap.Inc(addr, 1<<20)
						heap.Retain(key)
						twin.Store(key, types.BoxI32(1))

						stack := []types.Boxed{types.BoxRef(addr), types.BoxI32(1), 0}
						ctx := enter(t, stack)
						ctx.Heap, ctx.RC = heap.Bases()
						for trap := jit.Enter(set, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
							require.Fail(t, "native set bridged", "%v", exits[ctx.Exit()].Kind)
						}
						require.Equal(t, snapshot(twin), snapshot(native), "the constant hash of %q differs from String.Hash", string(text))
					}
				}
			}
		})
		t.Run("any keys of every dispatched kind", func(t *testing.T) {
			mirror(t, types.TypeAny(), func(h *types.Heap, n int) []types.Boxed {
				switch n % 7 {
				case 0:
					return []types.Boxed{types.BoxI32(int32(n/6 - 3))}
				case 1:
					return []types.Boxed{types.BoxI8(int8(n/6 - 3))}
				case 2:
					return []types.Boxed{types.BoxF64(math.Copysign(float64(n/6%2), float64(n/6%3-1)))}
				case 3:
					text := strings.Repeat("k", n%19) + strconv.Itoa(n)
					return []types.Boxed{types.BoxRef(h.Add(types.String(text))), types.BoxRef(h.Add(types.String(strings.Clone(text))))}
				case 4:
					return []types.Boxed{types.BoxRef(h.Add(types.TypedArray[int32]{int32(n)}))}
				case 5:
					return []types.Boxed{types.BoxI64(int64(n) << 33)}
				default:
					if n/7%2 == 0 {
						return []types.Boxed{types.BoxI1(true), types.Box(2, types.KindI1)}
					}
					return []types.Boxed{types.BoxI1(false), types.Box(0, types.KindI1)}
				}
			})
		})
	})

	t.Run("a typed key of an any map derives the word Map.Store keys by", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		type key struct {
			word   types.Boxed
			twin   types.Boxed
			bridge bool
		}
		denormal32 := math.Float32frombits(1)
		tests := []struct {
			name string
			typ  types.Type
			keys []key
		}{
			{"i8", types.TypeI8(), []key{
				{types.BoxI8(-128), types.BoxI8(-128), false}, {types.BoxI8(-3), types.BoxI8(-3), false},
				{types.BoxI8(0), types.BoxI8(0), false}, {types.BoxI8(127), types.BoxI8(127), false},
			}},
			{"i1", types.TypeI1(), []key{
				{types.BoxI1(true), types.BoxI1(true), false}, {types.BoxI1(false), types.BoxI1(false), false},
			}},
			{"i32", types.TypeI32(), []key{
				{types.BoxI32(-1), types.BoxI32(-1), false}, {types.BoxI32(math.MinInt32), types.BoxI32(math.MinInt32), false},
			}},
			{"i64", types.TypeI64(), []key{
				{types.BoxI64(-5), types.BoxI64(-5), false}, {types.BoxI64(1 << 40), types.BoxI64(1 << 40), false},
				{types.BoxI64(-(1 << 47)), types.BoxI64(-(1 << 47)), false},
			}},
			{"f32", types.TypeF32(), []key{
				{types.BoxF32(0), types.BoxF32(0), false}, {types.BoxF32(math.Float32frombits(1 << 31)), types.BoxF32(math.Float32frombits(1 << 31)), false},
				{types.BoxF32(-1.5), types.BoxF32(-1.5), false}, {types.BoxF32(float32(math.Inf(-1))), types.BoxF32(float32(math.Inf(-1))), false},
				{types.BoxF32(denormal32), types.BoxF32(denormal32), false}, {types.BoxF32(float32(math.NaN())), types.BoxF32(float32(math.NaN())), true},
			}},
			{"f64", types.TypeF64(), []key{
				{types.BoxF64(0), types.BoxF64(0), false}, {types.BoxF64(math.Copysign(0, -1)), types.BoxF64(math.Copysign(0, -1)), false},
				{types.BoxF64(math.Inf(1)), types.BoxF64(math.Inf(1)), false}, {types.BoxF64(5e-324), types.BoxF64(5e-324), false},
				{types.BoxF64(math.NaN()), types.BoxF64(math.NaN()), true},
			}},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(types.TypeAny(), types.TypeI32())
				run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
					fn := function(t, []types.Type{typ, tt.typ, types.TypeI32()}, nil, emit)
					fn.Typ.Returns = returns
					return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
				}
				set, setExits := run(func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
				}, types.TypeI32())
				lookup, lookupExits := run(func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
				}, types.TypeI32(), types.TypeI1())

				heap := types.NewHeap(16, 0, types.Hooks{})
				heap.Add(nil)
				heap.Add(types.TypedArray[int32]{1})
				heap.Add(types.I64(1 << 60))
				want := types.NewMap(heap, typ, 0)
				got := want.Clone()
				wantRef, gotRef := heap.Add(want), heap.Add(got)
				heap.Inc(wantRef, 1<<20)
				heap.Inc(gotRef, 1<<20)

				call := func(code uintptr, exits []jit.Exit, op instr.Opcode, k types.Boxed, v int32, serve func(), results ...uint64) ([]types.Boxed, int) {
					stack := []types.Boxed{types.BoxRef(gotRef), k, types.BoxI32(v), 0, 0}
					ctx := enter(t, stack)
					objects, rc := heap.Bases()
					ctx.Heap, ctx.RC = objects, rc
					bridges := 0
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						require.Equal(t, op, exit.Code)
						serve()
						copy(ctx.Results[:], results)
						bridges++
					}
					return stack, bridges
				}

				for _, k := range tt.keys {
					for _, v := range []int32{7, 8} {
						want.Store(k.twin, types.BoxI32(v))
						_, bridges := call(set, setExits, instr.MAP_SET, k.word, v, func() { got.Store(k.twin, types.BoxI32(v)) })
						require.Equal(t, btoi(k.bridge), bridges, "set of %v", k.word)
						require.Equal(t, snapshot(want), snapshot(got), "set of %v", k.word)
					}
					value, ok := want.Load(k.twin)
					if !ok {
						value = want.Zero
					}
					out, bridges := call(lookup, lookupExits, instr.MAP_LOOKUP, k.word, 0, func() {}, uint64(value), uint64(types.BoxI1(ok)))
					wantBridges := btoi(k.bridge && k.twin.Kind() == types.KindRef)
					require.Equal(t, wantBridges, bridges, "get of %v", k.word)
					require.Equal(t, []types.Boxed{value, types.BoxI1(ok)}, out[:2], "get of %v", k.word)
					require.Equal(t, snapshot(want), snapshot(got), "get of %v", k.word)
				}
			})
		}
	})

	t.Run("the map guard admits exactly the declared key kind, element kind, and class", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		fn := function(t, []types.Type{types.NewMapType(types.TypeI32(), types.TypeI32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI32()}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		tests := []struct {
			name  string
			key   types.Type
			elem  types.Type
			admit bool
		}{
			{"the declared map", types.TypeI32(), types.TypeI32(), true},
			{"an f64 key", types.TypeF64(), types.TypeI32(), false},
			{"a string key", types.TypeString(), types.TypeI32(), false},
			{"an any key", types.TypeAny(), types.TypeI32(), false},
			{"an i8 key", types.TypeI8(), types.TypeI32(), false},
			{"another element kind", types.TypeI32(), types.TypeF64(), false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stack := []types.Boxed{types.BoxRef(1), 0}
				ctx := enter(t, stack)
				ctx.Heap = address(t, []types.Value{nil, types.NewMap(nil, types.NewMapType(tt.key, tt.elem), 0)})
				ctx.RC = address(t, []int{0, 2})
				want := jit.TrapReturn
				if !tt.admit {
					want = jit.TrapDeopt
				}
				require.Equal(t, want, jit.Enter(code, ctx))
			})
		}
	})

	t.Run("the string map guard admits a map of another reference type and no map of another key kind", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		fn := function(t, []types.Type{types.NewMapType(types.TypeString(), types.TypeI32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI32()}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		tests := []struct {
			name  string
			key   types.Type
			admit bool
		}{
			{"the declared string map", types.TypeString(), true},
			{"an array key, whose map guards no reference type and serves only String keys", types.NewArrayType(types.TypeI32()), true},
			{"an any key", types.TypeAny(), false},
			{"an i32 key", types.TypeI32(), false},
			{"an f64 key", types.TypeF64(), false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				stack := []types.Boxed{types.BoxRef(1), 0}
				ctx := enter(t, stack)
				ctx.Heap = address(t, []types.Value{nil, types.NewMap(nil, types.NewMapType(tt.key, types.TypeI32()), 0)})
				ctx.RC = address(t, []int{0, 2})
				want := jit.TrapReturn
				if !tt.admit {
					want = jit.TrapDeopt
				}
				require.Equal(t, want, jit.Enter(code, ctx))
			})
		}
	})

	t.Run("the any map guard admits an any map and no map of another key kind", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		fn := function(t, []types.Type{types.NewMapType(types.TypeAny(), types.TypeI32())}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI32()}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)

		for _, tt := range []struct {
			name  string
			key   types.Type
			admit bool
		}{
			{"the declared any map", types.TypeAny(), true},
			{"a string key", types.TypeString(), false},
			{"an i32 key", types.TypeI32(), false},
			{"an f64 key", types.TypeF64(), false},
		} {
			t.Run(tt.name, func(t *testing.T) {
				stack := []types.Boxed{types.BoxRef(1), 0}
				ctx := enter(t, stack)
				ctx.Heap = address(t, []types.Value{nil, types.NewMap(nil, types.NewMapType(tt.key, types.TypeI32()), 0)})
				ctx.RC = address(t, []int{0, 2})
				want := jit.TrapReturn
				if !tt.admit {
					want = jit.TrapDeopt
				}
				require.Equal(t, want, jit.Enter(code, ctx))
			})
		}
	})

	t.Run("a key of an any map that native code does not lower bridges before the table is touched", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		typ := types.NewMapType(types.TypeAny(), types.TypeI32())
		params := []types.Type{typ, types.TypeAny(), types.TypeI32()}
		run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
			fn := function(t, params, nil, emit)
			fn.Typ.Returns = returns
			return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		}
		set, setExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		del, delExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		lookup, lookupExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
		}, types.TypeI32(), types.TypeI1())

		// The heap holds null, an array, a wide i64 cell, a string, and the map.
		tests := []struct {
			name   string
			key    types.Boxed
			bridge bool
		}{
			{"null", types.BoxedNull, true},
			{"an I64 cell", types.BoxRef(2), true},
			{"an f32", types.BoxF32(1.5), true},
			{"an i32", types.BoxI32(1), false},
			{"an inline i64", types.BoxI64(1 << 40), false},
			{"an f64", types.BoxF64(1.5), false},
			{"an array", types.BoxRef(1), false},
			{"a string", types.BoxRef(3), false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				heap := types.NewHeap(16, 0, types.Hooks{})
				heap.Add(nil)
				heap.Add(types.TypedArray[int32]{1})
				heap.Add(types.I64(1 << 60))
				heap.Add(types.String("key"))
				m := types.NewMap(heap, typ, 0)
				m.Store(types.BoxRef(heap.Add(types.String("seed"))), types.BoxI32(7))
				heap.Add(m)
				for _, addr := range []int{1, 2, 3, 5} {
					heap.Inc(addr, 1<<20)
				}
				before := snapshot(m)
				call := func(code uintptr, exits []jit.Exit, op instr.Opcode) (bridges int) {
					stack := []types.Boxed{types.BoxRef(5), tt.key, types.BoxI32(9), 0, 0}
					ctx := enter(t, stack)
					ctx.Heap, ctx.RC = heap.Bases()
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						require.Equal(t, op, exit.Code)
						require.Equal(t, before, snapshot(m), "native code wrote the table before it bridged")
						bridges++
					}
					return bridges
				}
				require.Equal(t, btoi(tt.bridge), call(del, delExits, instr.MAP_DELETE))
				require.Equal(t, btoi(tt.bridge), call(lookup, lookupExits, instr.MAP_LOOKUP))
				if tt.bridge {
					require.Equal(t, 1, call(set, setExits, instr.MAP_SET))
				}
			})
		}
	})

	t.Run("a NaN key of an any map bridges a set before the table is touched and never hits a get or delete", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		typ := types.NewMapType(types.TypeAny(), types.TypeI32())
		params := []types.Type{typ, types.TypeAny(), types.TypeI32()}
		run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
			fn := function(t, params, nil, emit)
			fn.Typ.Returns = returns
			return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		}
		set, setExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		del, _ := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		lookup, _ := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
		}, types.TypeI32(), types.TypeI1())

		heap := types.NewHeap(8, 0, types.Hooks{})
		heap.Add(nil)
		m := types.NewMap(heap, typ, 0)
		addr := heap.Add(m)
		heap.Inc(addr, 1<<20)
		m.Store(types.BoxI32(1), types.BoxI32(7))
		before := snapshot(m)
		call := func(code uintptr, exits []jit.Exit) (stack []types.Boxed, bridges int) {
			stack = []types.Boxed{types.BoxRef(addr), types.BoxedNaN, types.BoxI32(9), 0, 0}
			ctx := enter(t, stack)
			ctx.Heap, ctx.RC = heap.Bases()
			for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
				require.Equal(t, jit.TrapBridge, trap)
				require.Equal(t, instr.MAP_SET, exits[ctx.Exit()].Code)
				require.Equal(t, before, snapshot(m), "native code wrote the table before it bridged")
				bridges++
			}
			return stack, bridges
		}
		_, bridges := call(del, nil)
		require.Zero(t, bridges)
		out, bridges := call(lookup, nil)
		require.Zero(t, bridges)
		require.Equal(t, []types.Boxed{m.Zero, types.BoxI1(false)}, out[:2])
		_, bridges = call(set, setExits)
		require.Equal(t, 1, bridges)
	})

	t.Run("a key no string map accepts bridges before the table is touched", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		typ := types.NewMapType(types.TypeString(), types.TypeI32())
		params := []types.Type{typ, types.TypeAny(), types.TypeI32()}
		run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
			fn := function(t, params, nil, emit)
			fn.Typ.Returns = returns
			return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		}
		set, setExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		del, delExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		lookup, lookupExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
		}, types.TypeI32(), types.TypeI1())

		// The heap holds null, an array, the string key, a stored key, and the map.
		tests := []struct {
			name   string
			key    types.Boxed
			bridge bool
		}{
			{"an i32", types.BoxI32(1), true},
			{"an f64", types.BoxF64(1.5), true},
			{"null", types.BoxedNull, true},
			{"an array", types.BoxRef(1), true},
			{"a string", types.BoxRef(2), false},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				heap := types.NewHeap(16, 0, types.Hooks{})
				heap.Add(nil)
				heap.Add(types.TypedArray[int32]{1})
				heap.Add(types.String("key"))
				m := types.NewMap(heap, typ, 0)
				m.Store(types.BoxRef(heap.Add(types.String("seed"))), types.BoxI32(7))
				heap.Add(m)
				// The function owns its reference params and releases them as it returns.
				for _, addr := range []int{1, 2, 4} {
					heap.Inc(addr, 1<<20)
				}
				before := snapshot(m)
				call := func(code uintptr, exits []jit.Exit, op instr.Opcode) (bridges int) {
					stack := []types.Boxed{types.BoxRef(4), tt.key, types.BoxI32(9), 0, 0}
					ctx := enter(t, stack)
					ctx.Heap, ctx.RC = heap.Bases()
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						require.Equal(t, op, exit.Code)
						require.Equal(t, before, snapshot(m), "native code wrote the table before it bridged")
						bridges++
					}
					return bridges
				}
				require.Equal(t, btoi(tt.bridge), call(del, delExits, instr.MAP_DELETE))
				require.Equal(t, btoi(tt.bridge), call(lookup, lookupExits, instr.MAP_LOOKUP))
				require.Equal(t, btoi(tt.bridge), call(set, setExits, instr.MAP_SET))
			})
		}
	})

	t.Run("a NaN float key bridges a set before the table is touched and never hits a get or delete", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		for _, tt := range []struct {
			name string
			key  types.Type
			nan  types.Boxed
			one  types.Boxed
		}{
			{"f64", types.TypeF64(), types.BoxF64(math.NaN()), types.BoxF64(1)},
			{"f32", types.TypeF32(), types.BoxF32(float32(math.NaN())), types.BoxF32(1)},
		} {
			t.Run(tt.name, func(t *testing.T) {
				typ := types.NewMapType(tt.key, types.TypeI32())
				run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
					fn := function(t, []types.Type{typ, tt.key, types.TypeI32()}, nil, emit)
					fn.Typ.Returns = returns
					return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
				}
				set, setExits := run(func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
				}, types.TypeI32())
				del, delExits := run(func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
				}, types.TypeI32())
				lookup, lookupExits := run(func(b *instr.Builder) {
					b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
				}, types.TypeI32(), types.TypeI1())

				m := types.NewMap(nil, typ, 0)
				m.Store(tt.one, types.BoxI32(7))
				before := snapshot(m)
				call := func(code uintptr, exits []jit.Exit, op instr.Opcode) (stack []types.Boxed, bridges int) {
					stack = []types.Boxed{types.BoxRef(1), tt.nan, types.BoxI32(9), 0, 0}
					ctx := enter(t, stack)
					ctx.Heap = address(t, []types.Value{nil, m})
					ctx.RC = address(t, []int{0, 1 << 20})
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						require.Equal(t, op, exit.Code)
						require.Equal(t, before, snapshot(m), "native code wrote the table before it bridged")
						bridges++
					}
					return stack, bridges
				}

				_, bridges := call(del, delExits, instr.MAP_DELETE)
				require.Zero(t, bridges)
				out, bridges := call(lookup, lookupExits, instr.MAP_LOOKUP)
				require.Zero(t, bridges)
				require.Equal(t, []types.Boxed{m.Zero, types.BoxI1(false)}, out[:2])
				require.Equal(t, before, snapshot(m))
				_, bridges = call(set, setExits, instr.MAP_SET)
				require.Equal(t, 1, bridges)
				require.Equal(t, before, snapshot(m))
			})
		}
	})

	t.Run("an i64 key outside the inline range bridges before the table is touched", func(t *testing.T) {
		testenv.MustHaveArch(t, "arm64")

		typ := types.NewMapType(types.TypeI64(), types.TypeI32())
		key := func(b *instr.Builder) { b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I64_ADD) }
		params := []types.Type{typ, types.TypeI64(), types.TypeI64(), types.TypeI32()}
		run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
			fn := function(t, params, nil, emit)
			fn.Typ.Returns = returns
			return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		}
		set, setExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0)
			key(b)
			b.Emit(instr.LOCAL_GET, 3).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		del, delExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0)
			key(b)
			b.Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}, types.TypeI32())
		lookup, lookupExits := run(func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0)
			key(b)
			b.Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
		}, types.TypeI32(), types.TypeI1())

		const top, bottom = 1<<48 - 1, -(1 << 48)
		tests := []struct {
			name   string
			base   int64
			offset int64
			bridge bool
		}{
			{"the largest inline key", top, 0, false},
			{"the smallest inline key", bottom, 0, false},
			{"one above the inline range", top, 1, true},
			{"one below the inline range", bottom, -1, true},
		}
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				m := types.NewMap(nil, typ, 0)
				m.Store(types.BoxI64(1), types.BoxI32(7))
				before := snapshot(m)
				call := func(code uintptr, exits []jit.Exit, op instr.Opcode, results ...uint64) (stack []types.Boxed, bridges int) {
					stack = []types.Boxed{types.BoxRef(1), types.BoxI64(tt.base), types.BoxI64(tt.offset), types.BoxI32(9), 0, 0}
					ctx := enter(t, stack)
					ctx.Heap = address(t, []types.Value{nil, m})
					ctx.RC = address(t, []int{0, 1 << 20})
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						require.Equal(t, op, exit.Code)
						require.Equal(t, before, snapshot(m), "native code wrote the table before it bridged")
						copy(ctx.Results[:], results)
						bridges++
					}
					return stack, bridges
				}

				_, bridges := call(del, delExits, instr.MAP_DELETE)
				require.Equal(t, btoi(tt.bridge), bridges)
				out, bridges := call(lookup, lookupExits, instr.MAP_LOOKUP, 5, 1)
				require.Equal(t, btoi(tt.bridge), bridges)
				if tt.bridge {
					require.Equal(t, []types.Boxed{types.BoxI32(5), types.BoxI1(true)}, out[:2])
				} else {
					require.Equal(t, []types.Boxed{m.Zero, types.BoxI1(false)}, out[:2])
				}
				require.Equal(t, before, snapshot(m))
				_, bridges = call(set, setExits, instr.MAP_SET)
				require.Equal(t, btoi(tt.bridge), bridges)
				if !tt.bridge {
					require.Equal(t, 2, m.Len())
					v, ok := m.Load(types.BoxI64(tt.base + tt.offset))
					require.True(t, ok)
					require.Equal(t, types.BoxI32(9), v)
				}
			})
		}
	})
}

// mirror runs the same random operations on a Go table and on its clone, the
// clone through native code that serves a bridge with the Go method, and
// requires the two tables to agree word for word after each: ctrl, slots, order,
// pos, count, used, and the arrays' identity, so a native rehash would show. It
// also requires native code to bridge exactly the sets the Go table rehashes on,
// and an insert to reuse a dead slot, so no more slots are taken than there are
// distinct keys. Both maps live in one heap, where keyOf makes every word that
// names the n-th key (a String has two refs of equal content, one of which each
// step picks at random), and the reference count of each such ref must be its
// own plus one for each table that holds it.
func mirror(t *testing.T, key types.Type, keyOf func(h *types.Heap, n int) []types.Boxed) {
	typ := types.NewMapType(key, types.TypeI32())
	mapType := typ
	run := func(emit func(*instr.Builder), returns ...types.Type) (uintptr, []jit.Exit) {
		fn := function(t, []types.Type{mapType, key, types.TypeI32()}, nil, emit)
		fn.Typ.Returns = returns
		return lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
	}
	set, setExits := run(func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
	}, types.TypeI32())
	del, delExits := run(func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_DELETE).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
	}, types.TypeI32())
	lookup, lookupExits := run(func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.MAP_LOOKUP).Emit(instr.RETURN)
	}, types.TypeI32(), types.TypeI1())
	length, lengthExits := run(func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN)
	}, types.TypeI32())

	for _, initial := range []int{0, 5, 40} {
		for _, space := range []int{3, 12, 50} {
			t.Run(fmt.Sprintf("capacity %d over %d keys", initial, space), func(t *testing.T) {
				heap := types.NewHeap(1024, 0, types.Hooks{})
				heap.Add(nil)
				want := types.NewMap(heap, typ, initial)
				got := want.Clone()
				wantRef, gotRef := heap.Add(want), heap.Add(got)
				heap.Inc(wantRef, 1<<20)
				heap.Inc(gotRef, 1<<20)
				keys := make([][]types.Boxed, space)
				distinct := types.NewMap(heap, typ, 0)
				for n := range space {
					keys[n] = keyOf(heap, n)
					heap.Retain(keys[n][0])
					distinct.Store(keys[n][0], 0)
				}
				rng := rand.New(rand.NewPCG(uint64(initial), uint64(space)))

				// call runs code on got with key k and value v, serving each bridge it
				// takes with serve, and returns the result words and the bridges served.
				call := func(code uintptr, exits []jit.Exit, k types.Boxed, v int32, serve func(jit.Exit)) ([]types.Boxed, int) {
					// The function owns its reference params and releases them as it returns.
					heap.Retain(k)
					stack := []types.Boxed{types.BoxRef(gotRef), k, types.BoxI32(v), 0}
					ctx := enter(t, stack)
					objects, rc := heap.Bases()
					ctx.Heap, ctx.RC = objects, rc
					bridges := 0
					for trap := jit.Enter(code, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
						require.Equal(t, jit.TrapBridge, trap)
						exit := exits[ctx.Exit()]
						require.Equal(t, jit.ExitBridge, exit.Kind)
						serve(exit)
						bridges++
					}
					return stack, bridges
				}
				never := func(jit.Exit) { require.Fail(t, "bridged an operation native code serves") }
				bridged := 0

				for step := range 1500 {
					same := keys[rng.IntN(space)]
					k, v := same[rng.IntN(len(same))], int32(rng.IntN(1000))
					switch n := rng.IntN(10); {
					case n < 5:
						before := arrays(want)
						heap.Retain(k)
						want.Store(k, types.BoxI32(v))
						rehashed := arrays(want) != before
						before = arrays(got)
						_, bridges := call(set, setExits, k, v, func(exit jit.Exit) {
							require.Equal(t, instr.MAP_SET, exit.Code)
							require.Equal(t, before, arrays(got), "native code must not touch the arrays before it bridges")
							heap.Retain(k)
							got.Store(k, types.BoxI32(v))
						})
						require.Equal(t, rehashed, arrays(got) != before)
						require.Equal(t, btoi(rehashed), bridges, "step %d: native set and Go set disagree on rehashing", step)
						bridged += bridges
					case n < 8:
						want.Delete(k)
						_, bridges := call(del, delExits, k, v, never)
						require.Zero(t, bridges)
					case n < 9:
						value, ok := want.Load(k)
						if !ok {
							value = want.Zero
						}
						out, bridges := call(lookup, lookupExits, k, v, never)
						require.Zero(t, bridges)
						require.Equal(t, []types.Boxed{value, types.BoxI1(ok)}, out[:2], "step %d", step)
					default:
						out, bridges := call(length, lengthExits, k, v, never)
						require.Zero(t, bridges)
						require.Equal(t, types.BoxI32(int32(want.Len())), out[0], "step %d", step)
					}
					snap := snapshot(got)
					require.Equal(t, snapshot(want), snap, "step %d", step)
					taken := 0
					held := map[uint64]int{}
					for i, state := range snap.ctrl {
						taken += btoi(state != 0)
						if state&0x80 != 0 {
							held[snap.slots[2*i]]++
						}
					}
					require.LessOrEqual(t, taken, distinct.Len(), "step %d: an insert passed over a dead slot", step)
					for _, same := range keys {
						for _, k := range same {
							if k.Kind() == types.KindRef {
								require.Equal(t, 1+2*held[uint64(k)]+btoi(k == same[0]), heap.Count(k.Ref()), "step %d: reference count of key %v", step, k)
							}
						}
					}
				}
				if distinct.Len() > 6 && initial < 40 {
					require.Positive(t, bridged, "the sequence never reached the load factor")
				}
			})
		}
	}
}

// FuzzStringHashParity requires the hash native code emits to be String.Hash:
// native code stores a key in four tables of different seeds, each beside its
// clone in which Go stores the same key, and their words must agree. A table
// places a key by its home slot and tag, about 19 bits each, so four seeds
// compare about 76 bits of the hash. The key sits at the end of a page, so no
// load may pass its last byte.
func FuzzStringHashParity(f *testing.F) {
	testenv.MustHaveArch(f, "arm64")

	for _, n := range []int{0, 1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 24, 25, 31, 32, 33, 64} {
		for _, fill := range []byte{0x00, 0xFF, 0x5A} {
			f.Add(slices.Repeat([]byte{fill}, n))
		}
		pattern := make([]byte, n)
		for i := range pattern {
			pattern[i] = byte(i*37 + 11)
		}
		f.Add(pattern)
	}

	typ := types.NewMapType(types.TypeString(), types.TypeI32())
	fn := function(f, []types.Type{typ, types.TypeString(), types.TypeI32()}, nil, func(b *instr.Builder) {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.MAP_SET).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
	})
	set, exits := lower(f, arm64.New(), translate(f, fn), fn, nil, 0, false)

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 512 {
			t.Skip()
		}
		heap := types.NewHeap(32, 0, types.Hooks{})
		heap.Add(nil)
		key := heap.Add(edge(t, string(data)))
		for range 4 {
			native := types.NewMap(heap, typ, 1<<12)
			twin := native.Clone()
			addr := heap.Add(native)
			heap.Inc(addr, 1<<20)
			heap.Retain(types.BoxRef(key))
			twin.Store(types.BoxRef(key), types.BoxI32(1))

			stack := []types.Boxed{types.BoxRef(addr), types.BoxRef(key), types.BoxI32(1), 0}
			ctx := enter(t, stack)
			ctx.Heap, ctx.RC = heap.Bases()
			for trap := jit.Enter(set, ctx); trap != jit.TrapReturn; trap = jit.Resume(ctx) {
				require.Fail(t, "native set bridged", "%v", exits[ctx.Exit()].Kind)
			}
			require.Equal(t, snapshot(twin), snapshot(native), "the hash of %q differs from String.Hash", data)
		}
	})
}

// edge is s placed so that its last byte ends a page whose next page is
// unreadable, so a load past the string faults.
func edge(t testing.TB, s string) types.String {
	t.Helper()
	page := syscall.Getpagesize()
	mem, err := syscall.Mmap(-1, 0, 2*page, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_ANON|syscall.MAP_PRIVATE)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, syscall.Munmap(mem)) })
	require.NoError(t, syscall.Mprotect(mem[page:], syscall.PROT_NONE))
	data := mem[page-len(s) : page]
	copy(data, s)
	return types.String(unsafe.String(unsafe.SliceData(data), len(s)))
}

func TestMachine_Branch(t *testing.T) {
	r := regs{1: ssa.TypeI32}
	index := r.Reg(1)
	br := ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{1}}

	t.Run("jumps to a label that is not next and falls through to next", func(t *testing.T) {
		a := asm.New(target.New())
		to, next := a.Label(), a.Label()
		arm64.New().Branch(a, ssa.Terminator{Term: ssa.TermJump}, r, []asm.Label{to}, next)
		require.Equal(t, []asm.Instruction{target.BLabel(to)}, a.Rows())

		a = asm.New(target.New())
		next = a.Label()
		arm64.New().Branch(a, ssa.Terminator{Term: ssa.TermJump}, r, []asm.Label{next}, next)
		require.Empty(t, a.Rows())
	})

	t.Run("branches on nonzero, inverting to fall through to whichever label is next", func(t *testing.T) {
		a := asm.New(target.New())
		yes, no, next := a.Label(), a.Label(), a.Label()
		arm64.New().Branch(a, br, r, []asm.Label{yes, no}, next)
		require.Equal(t, []asm.Instruction{target.CBNZLabel(index, yes), target.BLabel(no)}, a.Rows())

		a = asm.New(target.New())
		yes, no = a.Label(), a.Label()
		arm64.New().Branch(a, br, r, []asm.Label{yes, no}, no)
		require.Equal(t, []asm.Instruction{target.CBNZLabel(index, yes)}, a.Rows())

		a = asm.New(target.New())
		yes, no = a.Label(), a.Label()
		arm64.New().Branch(a, br, r, []asm.Label{yes, no}, yes)
		require.Equal(t, []asm.Instruction{target.CBZLabel(index, no)}, a.Rows())
	})

	t.Run("branches on the flags of a fused compare, inverting to fall through to the nonzero label", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		s := fused{regs{1: ssa.TypeI1, 2: ssa.TypeI32, 3: ssa.TypeI32}}
		yes, no := a.Label(), a.Label()
		require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpIntCmp, Cond: ssa.CondSLT, Args: []ssa.Value{2, 3}, Results: []ssa.Value{1}}, s))
		m.Branch(a, br, s, []asm.Label{yes, no}, no)
		require.Equal(t, []asm.Instruction{target.CMP(s.Reg(2), s.Reg(3)), target.BCondLabel(target.OpBLT, yes)}, a.Rows())

		m, a = arm64.New(), asm.New(target.New())
		s = fused{regs{1: ssa.TypeI1, 2: ssa.TypeF64, 3: ssa.TypeF64}}
		yes, no = a.Label(), a.Label()
		require.True(t, m.Lower(a, ssa.Operation{Op: ssa.OpFloatCmp, Cond: ssa.CondOLT, Args: []ssa.Value{2, 3}, Results: []ssa.Value{1}}, s))
		m.Branch(a, br, s, []asm.Label{yes, no}, yes)
		require.Equal(t, []asm.Instruction{target.FCMP(s.Reg(2), s.Reg(3)), target.BCondLabel(target.OpBPL, no)}, a.Rows())
	})

	t.Run("takes the last label for an index out of range, falling through when it is next", func(t *testing.T) {
		a := asm.New(target.New())
		first, second, rest, next := a.Label(), a.Label(), a.Label(), a.Label()
		arm64.New().Branch(a, ssa.Terminator{Term: ssa.TermBrTable, Args: []ssa.Value{1}}, r, []asm.Label{first, second, rest}, next)
		require.Equal(t, []asm.Instruction{
			target.CMPI(index, 0), target.BCondLabel(target.OpBEQ, first),
			target.CMPI(index, 1), target.BCondLabel(target.OpBEQ, second),
			target.BLabel(rest),
		}, a.Rows())

		a = asm.New(target.New())
		first, rest = a.Label(), a.Label()
		arm64.New().Branch(a, ssa.Terminator{Term: ssa.TermBrTable, Args: []ssa.Value{1}}, r, []asm.Label{first, rest}, rest)
		require.Equal(t, []asm.Instruction{target.CMPI(index, 0), target.BCondLabel(target.OpBEQ, first)}, a.Rows())
	})

	t.Run("loads a case index through a register past CMPI's imm12 range", func(t *testing.T) {
		a := asm.New(target.New())
		labels := make([]asm.Label, 0xFFF+3)
		for i := range labels {
			labels[i] = a.Label()
		}
		arm64.New().Branch(a, ssa.Terminator{Term: ssa.TermBrTable, Args: []ssa.Value{1}}, r, labels, a.Label())
		rows := a.Rows()
		want := append([]asm.Instruction{target.MOVZ(target.W16, 0x1000, 0)},
			target.CMP(index, target.W16), target.BCondLabel(target.OpBEQ, labels[0x1000]),
			target.BLabel(labels[len(labels)-1]))
		require.Equal(t, want, rows[len(rows)-len(want):])
	})
}

func TestMachine_Return(t *testing.T) {
	r := regs{1: ssa.TypeI32}
	rows := func(slot int16) []asm.Instruction {
		rows := []asm.Instruction{target.UXTW(target.X16, r.Reg(1))}
		rows = append(rows, target.LDI(target.X17, types.Tag(types.KindI32))...)
		return append(rows, target.ORR(target.X16, target.X16, target.X17), target.STR(target.X16, target.X25, slot), target.SUBI(target.X24, target.X24, 1), target.BLabel(0))
	}
	scalars := []types.Kind{types.KindI32, types.KindF64}

	t.Run("stores results from slot zero and completed operands past the locals", func(t *testing.T) {
		for _, c := range []struct {
			op   ssa.Term
			slot int16
		}{{ssa.TermReturn, 0}, {ssa.TermComplete, 16}} {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, 0, true, compile.Layout{Kinds: scalars}, nil)
			start := len(a.Rows())
			m.Return(a, ssa.Terminator{Term: c.op, Args: []ssa.Value{1}}, r)
			require.Equal(t, rows(c.slot), a.Rows()[start:])
		}
	})

	t.Run("releases and clears every slot that can hold a reference first", func(t *testing.T) {
		sweep := func(slot int16, word asm.VReg) []asm.Instruction {
			return slices.Concat(
				[]asm.Instruction{target.LDR(word, target.X25, slot), target.LSRI(target.X16, word, 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, word, 0, 32),
					target.CBZLabel(target.X17, resume),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetRC)),
					target.LSLI(target.X17, target.X17, 3),
					target.ADD(target.X16, target.X16, target.X17),
					target.LDR(target.X17, target.X16, 0),
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
					target.STR(target.XZR, target.X25, slot),
				},
			)
		}

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{Kinds: []types.Kind{types.KindI32, types.KindRef, types.KindI8, types.KindI64}, Zeros: []types.Boxed{types.BoxedNull, types.BoxI8(0), types.BoxI64(0)}}, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{1}}, r)
		require.Equal(t, slices.Concat(
			sweep(8, asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)),
			sweep(24, asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)),
			rows(0),
		), a.Rows()[start:])
	})

	t.Run("releases borrowed parameters only at depth 1", func(t *testing.T) {
		release := func(word asm.VReg) []asm.Instruction {
			return slices.Concat(
				[]asm.Instruction{target.LSRI(target.X16, word, 49)},
				target.LDI(target.X17, types.Tag(types.KindRef)>>49),
				[]asm.Instruction{
					target.CMP(target.X16, target.X17),
					target.BCondLabel(target.OpBNE, resume),
					target.SBFX(target.X17, word, 0, 32),
					target.CBZLabel(target.X17, resume),
					target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetRC)),
					target.LSLI(target.X17, target.X17, 3),
					target.ADD(target.X16, target.X16, target.X17),
					target.LDR(target.X17, target.X16, 0),
					target.CMPI(target.X17, 1),
					target.BCondLabel(target.OpBLE, exit),
					target.SUBI(target.X17, target.X17, 1),
					target.STR(target.X17, target.X16, 0),
				},
			)
		}
		word2, word3 := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64), asm.NewVReg(-3, asm.RegTypeInt, asm.Width64)

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{Kinds: []types.Kind{types.KindRef, types.KindRef}, Borrows: []bool{true, false}}, nil)

		skip := asm.Label(2)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Term: ssa.TermReturn}, r)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.ADDI(target.X16, arm64.Ctx, uint16(jit.OffsetRecords+jit.SizeofRecord)), target.CMP(target.X27, target.X16), target.BCondLabel(target.OpBNE, skip)},
			[]asm.Instruction{target.LDR(word2, target.X25, 0)},
			release(word2),
			[]asm.Instruction{target.STR(target.XZR, target.X25, 0)},
			[]asm.Instruction{target.LDR(word3, target.X25, 8)},
			release(word3),
			[]asm.Instruction{target.STR(target.XZR, target.X25, 8)},
			[]asm.Instruction{target.SUBI(target.X24, target.X24, 1), target.BLabel(0)},
		), a.Rows()[start:])
	})

	t.Run("emits no depth-1 guard when no parameter is borrowed", func(t *testing.T) {

		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{Kinds: []types.Kind{types.KindI32}, Borrows: []bool{false}}, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Term: ssa.TermReturn}, r)
		require.Equal(t, []asm.Instruction{target.SUBI(target.X24, target.X24, 1), target.BLabel(0)}, a.Rows()[start:])
	})

	t.Run("moves register-convention results to X0 and X1 instead of boxing them, a float by FMOV", func(t *testing.T) {
		r2 := regs{1: ssa.TypeF64, 2: ssa.TypeRef}
		for _, c := range []struct {
			regs    regs
			results []types.Kind
			args    []ssa.Value
			rows    []asm.Instruction
		}{
			{r, []types.Kind{types.KindI32}, []ssa.Value{1}, []asm.Instruction{
				target.MOV(target.W0, r.Reg(1)),
				target.USE(target.X0),
				target.SUBI(target.X24, target.X24, 1),
				target.BLabel(0),
			}},
			{r2, []types.Kind{types.KindF64, types.KindRef}, []ssa.Value{1, 2}, []asm.Instruction{
				target.FMOV(target.X0, r2.Reg(1)),
				target.MOV(target.X1, r2.Reg(2)),
				target.USE(target.X0),
				target.USE(target.X1),
				target.SUBI(target.X24, target.X24, 1),
				target.BLabel(0),
			}},
		} {
			m, a := arm64.New(), asm.New(target.New())
			m.Prologue(a, 0, true, compile.Layout{Kinds: scalars, Registers: c.results}, nil)
			start := len(a.Rows())
			m.Return(a, ssa.Terminator{Term: ssa.TermReturn, Args: c.args}, c.regs)
			require.Equal(t, c.rows, a.Rows()[start:])
		}
	})

	t.Run("keeps TermComplete boxing to slots even under the register convention", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{Kinds: scalars, Registers: []types.Kind{types.KindI32}}, nil)
		start := len(a.Rows())
		m.Return(a, ssa.Terminator{Term: ssa.TermComplete, Args: []ssa.Value{1}}, r)
		require.Equal(t, rows(16), a.Rows()[start:])
	})
}

func TestMachine_Budget(t *testing.T) {
	m, a := arm64.New(), asm.New(target.New())
	m.Prologue(a, 0, true, compile.Layout{}, nil)
	start := len(a.Rows())
	m.Budget(a, exit)
	require.Equal(t, []asm.Instruction{
		target.SUBSI(target.X24, target.X24, 1),
		target.BCondLabel(target.OpBLE, exit),
	}, a.Rows()[start:])
}

func TestMachine_Exit(t *testing.T) {
	uses := []asm.VReg{asm.NewVReg(1, asm.RegTypeInt, asm.Width64), asm.NewVReg(2, asm.RegTypeFloat, asm.Width64)}
	rows := func(id uint64, trap jit.Trap) []asm.Instruction {
		return slices.Concat(
			[]asm.Instruction{
				target.STR(target.X27, arm64.Ctx, int16(jit.OffsetNext)),
				target.STR(target.X24, arm64.Ctx, int16(jit.OffsetBudget)),
			},
			target.LDI(target.X16, id),
			[]asm.Instruction{target.STR(target.X16, arm64.Ctx, int16(jit.OffsetExit))},
			target.LDI(target.X16, uint64(trap)),
			[]asm.Instruction{
				target.STR(target.X16, arm64.Ctx, int16(jit.OffsetTrap)),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetStub)),
				target.EXIT(target.X16),
				target.USE(uses[0]),
				target.USE(uses[1]),
			},
		)
	}

	for _, c := range []struct {
		name string
		id   int
		kind jit.Kind
		trap jit.Trap
		tail asm.Instruction
	}{
		{"suspends a resumable exit", 3, jit.ExitBridge, jit.TrapBridge, target.LDR(target.X24, arm64.Ctx, int16(jit.OffsetBudget))},
		{"never returns from a deopt", 4, jit.ExitDeopt, jit.TrapDeopt, target.BRK(0)},
		{"suspends a call for the interpreter to run", 5, jit.ExitCall, jit.TrapBridge, target.LDR(target.X24, arm64.Ctx, int16(jit.OffsetBudget))},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := asm.New(target.New())
			arm64.New().Exit(a, c.id, c.kind, uses)
			require.Equal(t, append(rows(uint64(c.id), c.trap), c.tail), a.Rows())
		})
	}
}

func TestMachine_Spill(t *testing.T) {
	a := asm.New(target.New())
	w := asm.NewVReg(1, asm.RegTypeInt, asm.Width32)
	arm64.New().Spill(a, w, 2)
	require.Equal(t, []asm.Instruction{target.New().Spill(w, 2)}, a.Rows())
}

func TestMachine_Results(t *testing.T) {
	a := asm.New(target.New())
	w := asm.NewVReg(1, asm.RegTypeInt, asm.Width32)
	d := asm.NewVReg(2, asm.RegTypeFloat, asm.Width64)
	arm64.New().Results(a, []asm.VReg{w, d})
	require.Equal(t, []asm.Instruction{
		target.LDR(w, arm64.Ctx, int16(jit.OffsetResults)),
		target.LDR(d, arm64.Ctx, int16(jit.OffsetResults)+8),
	}, a.Rows())
}

func TestMachine_Call(t *testing.T) {
	r := regs{1: ssa.TypeI32, 2: ssa.TypeRef, 3: ssa.TypeF64}
	live := asm.NewVReg(9, asm.RegTypeInt, asm.Width32)

	t.Run("calls through the natives table and bridges when it cannot", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Safepoint: safe, Resume: next, Return: back, Owned: true,
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		callee := r.Reg(2)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, arm64.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LSRI(target.X16, callee, 49),
			},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, resume),
				target.SBFX(target.X17, callee, 0, 32),
				target.CBZLabel(target.X17, resume),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetRC)),
				target.LSLI(target.X17, target.X17, 3),
				target.ADD(target.X16, target.X16, target.X17),
				target.LDR(target.X17, target.X16, 0),
				target.CMPI(target.X17, 1),
				target.BCondLabel(target.OpBLE, exit),
				target.SUBI(target.X17, target.X17, 1),
				target.STR(target.X17, target.X16, 0),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("stores its arguments and always branches to its bridge when the callee is unknown", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge, join := a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 1, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Join: join, Owned: true, Generic: true,
		}, r))

		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.BLabel(bridge),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("passes a closure callee's upvals base through the context", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Safepoint: safe, Resume: next, Return: back, Upvals: true,
		}, r))

		code, closure, upvals := vr(1), vr(2), vr(3)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, arm64.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.SBFX(closure, r.Reg(2), 0, 32),
				target.LSLI(closure, closure, 4),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetHeap)),
				target.ADD(closure, target.X16, closure),
				target.LDR(upvals, closure, int16(jit.OffsetData)),
				target.LDR(upvals, upvals, int16(jit.OffsetClosureUpvals)),
				target.STR(upvals, arm64.Ctx, int16(jit.OffsetUpvals)),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("borrows a callee it does not own", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Safepoint: safe, Resume: next, Return: back,
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, arm64.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("reads a register-convention result from X0 instead of the slot", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Safepoint: safe, Resume: next, Return: back,
			Registers: []types.Kind{types.KindF64},
		}, r))

		code := asm.NewVReg(-2, asm.RegTypeInt, asm.Width64)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.LDR(code, arm64.Ctx, int16(jit.OffsetNatives)),
			},
			target.LDI(target.X16, 5),
			[]asm.Instruction{
				target.LDRR(code, code, target.X16),
				target.CBZLabel(code, bridge),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.BLR(code),
				target.DEF(target.X0),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.FMOV(r.Reg(3), target.X0),
			},
		), a.Rows()[start:])
	})

	t.Run("calls its own entry directly when it is a self call", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Results: []ssa.Value{3},
			Base: 4, Size: 3, Exit: 7, Live: []asm.VReg{live}, Stub: bridge, Safepoint: safe, Resume: next, Return: back, Owned: true, Self: true,
		}, r))

		entry := asm.Label(1)
		callee := r.Reg(2)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.BLLabel(entry),
				target.USE(live),
				target.SUBI(target.X25, target.X25, 32),
				target.LSRI(target.X16, callee, 49),
			},
			target.LDI(target.X17, types.Tag(types.KindRef)>>49),
			[]asm.Instruction{
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBNE, resume),
				target.SBFX(target.X17, callee, 0, 32),
				target.CBZLabel(target.X17, resume),
				target.LDR(target.X16, arm64.Ctx, int16(jit.OffsetRC)),
				target.LSLI(target.X17, target.X17, 3),
				target.ADD(target.X16, target.X16, target.X17),
				target.LDR(target.X17, target.X16, 0),
				target.CMPI(target.X17, 1),
				target.BCondLabel(target.OpBLE, exit),
				target.SUBI(target.X17, target.X17, 1),
				target.STR(target.X17, target.X16, 0),
				target.LDR(r.Reg(3), target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("moves register-passed arguments into X0/X1 on top of their boxed slot store", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1, 3}, Base: 4, Size: 3, Exit: 7,
			Stub: bridge, Safepoint: safe, Resume: next, Return: back, Self: true,
			Arguments: []types.Kind{types.KindI32, types.KindF64},
		}, r))

		entry := asm.Label(1)
		require.Equal(t, slices.Concat(
			[]asm.Instruction{target.UXTW(target.X16, r.Reg(1))},
			target.LDI(target.X17, types.Tag(types.KindI32)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
			},
			[]asm.Instruction{target.FMOV(target.X16, r.Reg(3))},
			canonical(),
			[]asm.Instruction{
				target.STR(target.X16, target.X25, 40),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.MOV(asm.NewPReg(target.X0.ID(), asm.RegTypeInt, asm.Width32), r.Reg(1)),
				target.FMOV(target.X1, r.Reg(3)),
				target.USE(target.X0),
				target.USE(target.X1),
				target.BLLabel(entry),
				target.SUBI(target.X25, target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("moves a register-passed i64 argument by a raw 64-bit MOV, on top of its boxed slot store", func(t *testing.T) {
		r64 := regs{1: ssa.TypeI64}
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		bridge := a.Label()
		safe, next, back := a.Label(), a.Label(), a.Label()
		start := len(a.Rows())
		require.True(t, m.Call(a, compile.Call{
			Callee: 5, Target: 2, Args: []ssa.Value{1}, Base: 4, Size: 3, Exit: 7,
			Stub: bridge, Safepoint: safe, Resume: next, Return: back, Self: true,
			Arguments: []types.Kind{types.KindI64},
		}, r64))

		entry := asm.Label(1)
		require.Equal(t, slices.Concat(
			target.LDI(target.X16, 1<<48),
			[]asm.Instruction{
				target.ADD(target.X17, r64.Reg(1), target.X16),
				target.LSRI(target.X17, target.X17, 49),
				target.CBNZLabel(target.X17, exit),
				target.ANDI(target.X16, r64.Reg(1), types.VMask),
			},
			target.LDI(target.X17, types.Tag(types.KindI64)),
			[]asm.Instruction{
				target.ORR(target.X16, target.X16, target.X17),
				target.STR(target.X16, target.X25, 32),
				target.ADDI(target.X16, target.X25, 56),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetTop)),
				target.CMP(target.X16, target.X17),
				target.BCondLabel(target.OpBHI, bridge),
				target.LDR(target.X17, arm64.Ctx, int16(jit.OffsetEdge)),
				target.CMP(target.X27, target.X17),
				target.BCondLabel(target.OpBCS, bridge),
				target.SUBSI(target.X24, target.X24, 1),
				target.BCondLabel(target.OpBLE, safe),
				target.ADDI(target.X25, target.X25, 32),
				target.MOV(target.X0, r64.Reg(1)),
				target.USE(target.X0),
				target.BLLabel(entry),
				target.SUBI(target.X25, target.X25, 32),
			},
		), a.Rows()[start:])
	})

	t.Run("declines a frame beyond the reach of an immediate", func(t *testing.T) {
		m, a := arm64.New(), asm.New(target.New())
		m.Prologue(a, 0, true, compile.Layout{}, nil)
		require.False(t, m.Call(a, compile.Call{Callee: 5, Target: 2, Base: 510, Size: 2}, r))
	})
}

func TestMachine_Move(t *testing.T) {
	a := asm.New(target.New())
	m := arm64.New()
	w1, w2 := asm.NewVReg(1, asm.RegTypeInt, asm.Width32), asm.NewVReg(2, asm.RegTypeInt, asm.Width32)
	d1, d2 := asm.NewVReg(3, asm.RegTypeFloat, asm.Width64), asm.NewVReg(4, asm.RegTypeFloat, asm.Width64)
	m.Move(a, w1, w2)
	m.Move(a, d1, d2)
	require.Equal(t, []asm.Instruction{target.MOV(w1, w2), target.FMOV(d1, d2)}, a.Rows())
}

func TestMachine_Const(t *testing.T) {
	w := asm.NewVReg(1, asm.RegTypeInt, asm.Width32)
	x := asm.NewVReg(2, asm.RegTypeInt, asm.Width64)
	f := asm.NewVReg(3, asm.RegTypeFloat, asm.Width32)
	d := asm.NewVReg(4, asm.RegTypeFloat, asm.Width64)
	for _, c := range []struct {
		name string
		dst  asm.VReg
		word uint64
		rows []asm.Instruction
	}{
		{"i1", w, 1, target.LDI(w, 1)},
		{"i8", w, uint64(uint32(0xFFFFFFFE)), target.LDI(w, uint64(uint32(0xFFFFFFFE)))},
		{"i32", w, uint64(uint32(0xFFFFFFF9)), target.LDI(w, uint64(uint32(0xFFFFFFF9)))},
		{"i64", x, 1 << 40, target.LDI(x, 1<<40)},
		{"wide i64", x, 0xCBF29CE484222325, []asm.Instruction{
			target.MOVZ(x, 0x2325, 0),
			target.MOVK(x, 0x8422, 16),
			target.MOVK(x, 0x9CE4, 32),
			target.MOVK(x, 0xCBF2, 48),
		}},
		{"f32", f, uint64(math.Float32bits(1.5)), append(target.LDI(target.X16, uint64(math.Float32bits(1.5))), target.FMOV(f, target.W16))},
		{"f32 by bank", f, uint64(math.Float32bits(1.5)) | 0xDEAD<<32, append(target.LDI(target.X16, uint64(math.Float32bits(1.5))), target.FMOV(f, target.W16))},
		{"f64", d, math.Float64bits(1.5), append(target.LDI(target.X16, math.Float64bits(1.5)), target.FMOV(d, target.X16))},
		{"ref", x, uint64(types.BoxRef(3)), target.LDI(x, uint64(types.BoxRef(3)))},
	} {
		t.Run(c.name, func(t *testing.T) {
			a := asm.New(target.New())
			arm64.New().Const(a, c.dst, c.word)
			require.Equal(t, c.rows, a.Rows())
		})
	}
}

// fibonacci is fib(n) = n < 2 ? n : fib(n-1) + fib(n-2) at function
// address 2, calling itself through constant 0.
func fibonacci(t *testing.T) (*types.Function, transform.Module) {
	fib := function(t, []types.Type{types.TypeI32()}, nil, func(b *instr.Builder) {
		small := b.Label()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_LT_S).BrIf(small)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.I32_ADD).Emit(instr.RETURN)
		b.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	})
	return fib, transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: fib}}}
}

func function(t testing.TB, params, locals []types.Type, emit func(*instr.Builder)) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: params, Returns: []types.Type{types.TypeI32()}},
		Locals: locals,
		Code:   instr.Marshal(code),
	}
}

func translate(t testing.TB, fn *types.Function) *ssa.Function {
	t.Helper()
	f, _, err := transform.Translate(transform.Module{}, nil, 1, fn, 0)
	require.NoError(t, err)
	return f
}

// lower builds f, the translation of fn at address (an OSR unit when osr),
// with m and publishes it. It returns the Go entry stub's address: what a
// fresh jit.Enter crosses into native code through, not the body's own
// address at offset 0 (what natives[address] holds for a native-to-native
// call: see the "records a nested native call" case, which links directly).
func lower(t testing.TB, m compile.Machine, f *ssa.Function, fn *types.Function, objects transform.Objects, address int, osr bool) (uintptr, []jit.Exit) {
	t.Helper()
	assembly, err := compile.Lower(f, compile.Unit{Function: fn, Module: transform.Module{Objects: objects}, Address: address, OSR: osr, Tier: jit.Baseline}, m)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(target.New(), len(assembly.Bytes))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	body, err := buffer.Link(assembly.Bytes)
	require.NoError(t, err)
	return body + uintptr(assembly.Entry), assembly.Exits
}

// enter is a context whose next activation has its frame at stack[0].
func enter(t testing.TB, stack []types.Boxed) *jit.Context {
	t.Helper()
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = address(t, stack)
	ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
	ctx.SetEdge(len(ctx.Records))
	ctx.Budget = 1000
	// entries backs the prologue's own entry count, indexed by address;
	// none of these cases uses past 7.
	entries := make([]int64, 8)
	ctx.Entries = address(t, entries)
	return ctx
}

// address is the base of s kept on the heap for the test's life: native code
// holds it as a uintptr, which a copied goroutine stack would leave dangling.
func address[T any](t testing.TB, s []T) uintptr {
	t.Cleanup(func() { runtime.KeepAlive(s) })
	return uintptr(unsafe.Pointer(&s[0]))
}

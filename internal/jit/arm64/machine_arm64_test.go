package arm64_test

import (
	"slices"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

func TestNew(t *testing.T) {
	t.Run("runs a translated function over the VM stack", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL)
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(6), types.BoxI32(7)}
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(43), stack[0])
		require.Zero(t, ctx.Depth)
	})

	t.Run("suspends at the loop safepoint until the budget is refilled", func(t *testing.T) {
		stack := []types.Boxed{types.BoxI32(10), types.BoxI32(99), types.BoxI32(99)}
		fn := sum(t)
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0)
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
		require.Zero(t, ctx.Depth)
	})

	t.Run("deopts a division by zero with the operands of its instruction", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(6), types.BoxI32(0)}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.Equal(t, 2*instr.New(instr.LOCAL_GET, 0).Width(), exit.Frames[0].IP)
		require.Equal(t, []uint64{6, 0}, operands(ctx, exit.Frames[0]))
	})

	t.Run("deopts storing an i64 outside the inline range", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI64)
		shift := b.Value(ssa.TypeI64)
		wide := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(50), Results: []ssa.Value{shift}})
		at := state(b, entry, ssa.Operand{Value: one}, ssa.Operand{Value: shift})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_SHL, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		store := state(b, entry, ssa.Operand{Value: wide})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{wide}, State: store})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI64}), nil, 0)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.Equal(t, types.KindI64, exit.Frames[0].Stack[0].Kind)
		require.Equal(t, []uint64{1 << 50}, operands(ctx, exit.Frames[0]))
		require.Zero(t, stack[0])
	})

	t.Run("deopts returning an i64 outside the inline range", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		one := b.Value(ssa.TypeI64)
		shift := b.Value(ssa.TypeI64)
		wide := b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(50), Results: []ssa.Value{shift}})
		at := state(b, entry, ssa.Operand{Value: one}, ssa.Operand{Value: shift})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_SHL, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		ret := state(b, entry, ssa.Operand{Value: wide})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{wide}, State: ret})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI32}), nil, 0)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, []uint64{1 << 50}, operands(ctx, exits[ctx.Exit()].Frames[0]))
	})

	t.Run("unboxes an inline i64 slot and deopts on a promoted one", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI64}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_ADD).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI64}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0)

		inline := []types.Boxed{types.BoxI64(-42)}
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, inline)))
		require.Equal(t, types.BoxI64(-41), inline[0])

		promoted := []types.Boxed{types.BoxRef(3)}
		ctx := enter(t, promoted)
		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Zero(t, exits[ctx.Exit()].Frames[0].IP)
	})

	t.Run("bridges an operation it does not lower", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		table := b.Value(ssa.TypeRef)
		key := b.Value(ssa.TypeI32)
		got := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxRef(5), Results: []ssa.Value{table}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{key}})
		at := state(b, entry, ssa.Operand{Value: table}, ssa.Operand{Value: key})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.MAP_GET, Args: []ssa.Value{table, key}, State: at, Results: []ssa.Value{got}})
		after := state(b, entry, ssa.Operand{Value: got})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{got}, State: after})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI32}), nil, 0)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitBridge, exit.Kind)
		require.Equal(t, instr.MAP_GET, exit.Code)
		require.Equal(t, []types.Kind{types.KindRef}, exit.Results)
		require.Equal(t, []uint64{uint64(types.BoxRef(5)), 2}, operands(ctx, exit.Frames[0]))

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
		b.Add(entry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{ref}})
		for range 3 {
			b.Add(entry, ssa.Operation{Op: ssa.OpRelease, Args: []ssa.Value{ref}, State: at})
		}
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		rc := []int{0, 0, 2}
		stack := []types.Boxed{types.BoxRef(2)}
		code, exits := lower(t, arm64.New(), b.Build(), frame([]types.Type{types.TypeString}, nil), nil, 0)
		ctx := enter(t, stack)
		ctx.RC = uintptr(unsafe.Pointer(&rc[0]))

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitRelease, exit.Kind)
		require.Equal(t, uint64(types.BoxRef(2)), read(ctx, exit.Release))
		require.Equal(t, []int{0, 0, 1}, rc)

		rc[2] = 2
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("releases and clears the references its slots hold on return", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeString}, nil, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		})
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0)

		rc := []int{0, 0, 0, 2}
		stack := []types.Boxed{types.BoxRef(3)}
		ctx := enter(t, stack)
		ctx.RC = uintptr(unsafe.Pointer(&rc[0]))
		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, []int{0, 0, 0, 1}, rc)
		require.Equal(t, types.BoxI32(1), stack[0])

		stack[0] = types.BoxRef(3)
		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, uint64(types.BoxRef(3)), read(ctx, exits[ctx.Exit()].Release))
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, types.BoxI32(1), stack[0])
	})

	t.Run("calls its own entry directly, bypassing the natives table", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		code, _ := lower(t, arm64.New(), f, fib, module.Objects, 2)

		// natives[2] is deliberately wrong: a self call never reads it.
		natives := []uintptr{0, 0, 0xdead}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = uintptr(unsafe.Pointer(&natives[0]))
		ctx.RC = uintptr(unsafe.Pointer(&rc[0]))

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(55), stack[0])
		require.Zero(t, ctx.Depth)
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("records a nested native call's return address in its own code", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		bytes, exits, err := compile.Lower(f, arm64.New(), fib, module.Objects, 0)
		require.NoError(t, err)
		buffer, err := asm.NewBuffer(len(bytes))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, buffer.Free()) })
		code, err := asm.Link(buffer, bytes)
		require.NoError(t, err)

		natives := []uintptr{0, 0, code}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = uintptr(unsafe.Pointer(&natives[0]))
		ctx.RC = uintptr(unsafe.Pointer(&rc[0]))
		ctx.Limit = 2

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		require.Equal(t, jit.ExitCall, exits[ctx.Exit()].Kind)
		require.Equal(t, uint64(2), ctx.Depth)
		require.GreaterOrEqual(t, ctx.Records[1].PC, code)
		require.Less(t, ctx.Records[1].PC, code+uintptr(len(bytes)))
	})

	t.Run("bridges a call to a function without native code", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0)

		natives := []uintptr{0, 0, 0}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = uintptr(unsafe.Pointer(&natives[0]))
		ctx.RC = uintptr(unsafe.Pointer(&rc[0]))

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitCall, exit.Kind)
		require.Equal(t, 2, exit.Callee)
		require.Equal(t, []types.Kind{types.KindI32}, exit.Results)
		require.Equal(t, instr.CALL, instr.Opcode(fib.Code[exit.Frames[0].IP-1]))
		require.Empty(t, exit.Frames[0].Stack)
		require.Equal(t, types.BoxI32(9), stack[1])

		stack[1] = types.BoxI32(34)
		require.Equal(t, jit.TrapBridge, jit.Resume(ctx))
		require.Equal(t, []uint64{34}, operands(ctx, exits[ctx.Exit()].Frames[0]))
		require.Equal(t, types.BoxI32(8), stack[2])

		stack[2] = types.BoxI32(21)
		require.Equal(t, jit.TrapReturn, jit.Resume(ctx))
		require.Equal(t, types.BoxI32(55), stack[0])
		// fib's own call site retains it exactly once and it feeds only that
		// call, so it is borrowed: rc never moves off the pool's own count,
		// even across three suspended bridges.
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("bridges a call past the depth limit or the stack top", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0)
		natives := []uintptr{0, 0, code}
		rc := []int{0, 0, 1}

		for _, limit := range []func(ctx *jit.Context, stack []types.Boxed){
			func(ctx *jit.Context, _ []types.Boxed) { ctx.Limit = 1 },
			func(ctx *jit.Context, stack []types.Boxed) { ctx.Top = uintptr(unsafe.Pointer(&stack[1])) },
		} {
			stack := make([]types.Boxed, 64)
			stack[0] = types.BoxI32(10)
			ctx := enter(t, stack)
			ctx.Natives = uintptr(unsafe.Pointer(&natives[0]))
			ctx.RC = uintptr(unsafe.Pointer(&rc[0]))
			limit(ctx, stack)

			require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
			require.Equal(t, jit.ExitCall, exits[ctx.Exit()].Kind)
			require.Equal(t, uint64(1), ctx.Depth)
		}
	})

	t.Run("returns every kind boxed", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		consts := []types.Boxed{types.BoxI8(-1), types.BoxI1(true), types.BoxF64(1.5), types.BoxF32(-2), types.BoxI64(-3), types.BoxRef(4)}
		var args []ssa.Value
		for _, c := range consts {
			v := b.Value(ssa.TypeOf(c.Kind()))
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c, Results: []ssa.Value{v}})
			args = append(args, v)
		}
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: args, State: at})

		stack := make([]types.Boxed, len(consts))
		code, _ := lower(t, arm64.New(), b.Build(), frame(nil, slices.Repeat([]types.Type{types.TypeI32}, len(consts))), nil, 0)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, consts, stack)
	})

	t.Run("lowers each function afresh on one machine", func(t *testing.T) {
		m := arm64.New()
		first := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		second := function(t, []types.Type{types.TypeI32}, nil, func(b *instr.Builder) {
			other := b.Label()
			b.Emit(instr.LOCAL_GET, 0).BrIf(other)
			b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
			b.Bind(other).Emit(instr.I32_CONST, 2).Emit(instr.RETURN)
		})
		lower(t, m, translate(t, first), first, nil, 0)

		stack := []types.Boxed{types.BoxI32(0)}
		code, _ := lower(t, m, translate(t, second), second, nil, 0)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, types.BoxI32(1), stack[0])
	})
}

// sum is sum(n) = 0 + 1 + ... + n-1 over one parameter and two locals.
func sum(t *testing.T) *types.Function {
	return function(t, []types.Type{types.TypeI32}, []types.Type{types.TypeI32, types.TypeI32}, func(b *instr.Builder) {
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
	})
}

// fibonacci is fib(n) = n < 2 ? n : fib(n-1) + fib(n-2) at function
// address 2, calling itself through constant 0.
func fibonacci(t *testing.T) (*types.Function, transform.Module) {
	fib := function(t, []types.Type{types.TypeI32}, nil, func(b *instr.Builder) {
		small := b.Label()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_LT_S).BrIf(small)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.I32_ADD).Emit(instr.RETURN)
		b.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	})
	return fib, transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: fib}}}
}

// frame is a function without code whose slots are params then locals.
func frame(params, locals []types.Type) *types.Function {
	return &types.Function{Typ: &types.FunctionType{Params: params}, Locals: locals}
}

func function(t *testing.T, params, locals []types.Type, emit func(*instr.Builder)) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: params, Returns: []types.Type{types.TypeI32}},
		Locals: locals,
		Code:   instr.Marshal(code),
	}
}

func translate(t *testing.T, fn *types.Function) *ssa.Function {
	t.Helper()
	f, err := transform.Translate(transform.Module{}, 1, fn, 0)
	require.NoError(t, err)
	return f
}

func state(b *ssa.Builder, block int, stack ...ssa.Operand) ssa.Value {
	v := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 1, Stack: stack}}, Results: []ssa.Value{v}})
	return v
}

// lower builds f, the translation of fn at address, with m and publishes it.
func lower(t *testing.T, m compile.Machine, f *ssa.Function, fn *types.Function, objects transform.Objects, address int) (uintptr, []jit.Exit) {
	t.Helper()
	code, exits, err := compile.Lower(f, m, fn, objects, address)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	entry, err := asm.Link(buffer, code)
	require.NoError(t, err)
	return entry, exits
}

// enter is a context whose next activation has its frame at stack[0].
func enter(t *testing.T, stack []types.Boxed) *jit.Context {
	t.Helper()
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))
	ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
	ctx.Limit = uint64(len(ctx.Records))
	ctx.Budget = 1000
	return ctx
}

// read is the raw native value v names in the suspended activation.
func read(ctx *jit.Context, v jit.Value) uint64 {
	if v.Loc.Spilled {
		return ctx.Slot(v.Loc.Slot)
	}
	return ctx.Reg(v.Loc.Reg)
}

func operands(ctx *jit.Context, f jit.Frame) []uint64 {
	var out []uint64
	for _, o := range f.Stack {
		out = append(out, read(ctx, o.Value))
	}
	return out
}

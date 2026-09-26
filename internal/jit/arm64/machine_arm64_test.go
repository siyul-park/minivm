package arm64_test

import (
	"math"
	"runtime"
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
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(43), stack[0])
		require.Zero(t, ctx.Depth)
	})

	t.Run("suspends at the loop safepoint until the budget is refilled", func(t *testing.T) {
		stack := []types.Boxed{types.BoxI32(10), types.BoxI32(99), types.BoxI32(99)}
		fn := sum(t)
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
		fn := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(6), types.BoxI32(0)}
		code, exits := lower(t, arm64.New(), translate(t, fn), fn, nil, 0, false)
		ctx := enter(t, stack)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitDeopt, exit.Kind)
		require.Equal(t, 2*instr.New(instr.LOCAL_GET, 0).Width(), exit.Frames[0].IP)
		require.Equal(t, []uint64{6, 0}, operands(ctx, exit.Frames[0]))
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
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_SHL, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		store := state(b, entry, ssa.Operand{Value: wide})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{wide}, State: store})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		// The local slot is TypeI64, which Return also releases before it
		// zeroes the slot: rc=1 on the box means that release is the last
		// reference, so it takes its own resumable ExitRelease.
		rc := []int{0, 0, 0, 1}
		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI64}), nil, 0, false)
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
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_SHL, Args: []ssa.Value{one, shift}, State: at, Results: []ssa.Value{wide}})
		ret := state(b, entry, ssa.Operand{Value: wide})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{wide}, State: ret})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI32}), nil, 0, false)
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
		fn := function(t, []types.Type{types.TypeI64}, nil, func(b *instr.Builder) {
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
		fn := function(t, []types.Type{types.TypeI64, types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_ADD).Emit(instr.RETURN)
		})
		fn.Typ.Returns = []types.Type{types.TypeI64}
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
		require.Zero(t, exits[badCtx.Exit()].Frames[0].IP)
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
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.MAP_GET, Args: []ssa.Value{table, key}, State: at, Results: []ssa.Value{got}})
		after := state(b, entry, ssa.Operand{Value: got})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Args: []ssa.Value{got}, State: after})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, []types.Type{types.TypeI32}), nil, 0, false)
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
		code, exits := lower(t, arm64.New(), b.Build(), frame([]types.Type{types.TypeString}, nil), nil, 0, false)
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
		fn := function(t, []types.Type{types.TypeString}, nil, func(b *instr.Builder) {
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
		f, err := transform.Translate(module, 2, fib, 0)
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
		require.Zero(t, ctx.Depth)
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("records a nested native call's return address in its own code", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		bytes, exits, stub, err := compile.Lower(f, arm64.New(), fib, module.Objects, 0, false, true)
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
		ctx.Natives = address(t, natives)
		ctx.RC = address(t, rc)
		ctx.Limit = 2

		require.Equal(t, jit.TrapBridge, jit.Enter(code+uintptr(stub), ctx))
		require.Equal(t, jit.ExitCall, exits[ctx.Exit()].Kind)
		require.Equal(t, uint64(2), ctx.Depth)
		require.GreaterOrEqual(t, ctx.Records[1].PC, code)
		require.Less(t, ctx.Records[1].PC, code+uintptr(len(bytes)))
	})

	t.Run("bridges a call to a function without native code", func(t *testing.T) {
		// An ExitCall never resumes (interp deoptimizes it), so this stops
		// at the bridge: the exit map and the slot-boxed argument.
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0, false)

		natives := []uintptr{0, 0, 0}
		rc := []int{0, 0, 1}
		stack := make([]types.Boxed, 64)
		stack[0] = types.BoxI32(10)
		ctx := enter(t, stack)
		ctx.Natives = address(t, natives)
		ctx.RC = address(t, rc)

		require.Equal(t, jit.TrapBridge, jit.Enter(code, ctx))
		exit := exits[ctx.Exit()]
		require.Equal(t, jit.ExitCall, exit.Kind)
		require.Equal(t, 2, exit.Callee)
		require.Equal(t, []types.Kind{types.KindI32}, exit.Results)
		require.Equal(t, instr.CALL, instr.Opcode(fib.Code[exit.Frames[0].IP-1]))
		require.Empty(t, exit.Frames[0].Stack)
		require.Equal(t, types.BoxI32(9), stack[1])
		// The borrowed callee's retain is never emitted, so bridging costs
		// no RC movement.
		require.Equal(t, []int{0, 0, 1}, rc)
	})

	t.Run("bridges a call past the depth limit or the stack top", func(t *testing.T) {
		fib, module := fibonacci(t)
		f, err := transform.Translate(module, 2, fib, 0)
		require.NoError(t, err)
		code, exits := lower(t, arm64.New(), f, fib, module.Objects, 0, false)
		natives := []uintptr{0, 0, code}
		rc := []int{0, 0, 1}

		for _, limit := range []func(ctx *jit.Context, stack []types.Boxed){
			func(ctx *jit.Context, _ []types.Boxed) { ctx.Limit = 1 },
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
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: ssa.Word(c), Results: []ssa.Value{v}})
			args = append(args, v)
		}
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: args, State: at})

		stack := make([]types.Boxed, len(consts))
		code, _ := lower(t, arm64.New(), b.Build(), frame(nil, slices.Repeat([]types.Type{types.TypeI32}, len(consts))), nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, consts, stack)
	})

	// returns runs a function returning c through Return's X0 and the Go
	// entry stub's boxing.
	returns := func(t *testing.T, c types.Boxed) {
		b := ssa.New("f")
		entry := b.Block()
		v := b.Value(ssa.TypeOf(c.Kind()))
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: ssa.Word(c), Results: []ssa.Value{v}})
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{v}, State: at})

		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{kindType(t, c.Kind())}}}
		stack := make([]types.Boxed, 1)
		code, _ := lower(t, arm64.New(), b.Build(), fn, nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, c, stack[0])
	}
	t.Run("returns i1 through the Go entry stub", func(t *testing.T) { returns(t, types.BoxI1(true)) })
	t.Run("returns i8 through the Go entry stub", func(t *testing.T) { returns(t, types.BoxI8(-1)) })
	t.Run("returns i32 through the Go entry stub", func(t *testing.T) { returns(t, types.BoxI32(-7)) })
	t.Run("returns f32 through the Go entry stub", func(t *testing.T) { returns(t, types.BoxF32(-2)) })
	t.Run("returns f64 through the Go entry stub", func(t *testing.T) { returns(t, types.BoxF64(1.5)) })
	t.Run("returns ref through the Go entry stub", func(t *testing.T) { returns(t, types.BoxRef(4)) })

	t.Run("returns two register-convention results through X0 and X1", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Value(ssa.TypeF64)
		y := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: math.Float64bits(2.5), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(4)), Results: []ssa.Value{y}})
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{x, y}, State: at})

		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64, types.TypeString}}}
		stack := make([]types.Boxed, 2)
		code, _ := lower(t, arm64.New(), b.Build(), fn, nil, 0, false)
		require.Equal(t, jit.TrapReturn, jit.Enter(code, enter(t, stack)))
		require.Equal(t, []types.Boxed{types.BoxF64(2.5), types.BoxRef(4)}, stack)
	})

	t.Run("a native call reads a register-convention result and forwards it through its own return", func(t *testing.T) {
		// The callee address (7) differs from the caller's lowering address
		// (0), so Call reaches it through the natives table, exercising
		// Call's DEF/MOV read after a real BLR, not the self-call branch.
		callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
		b := ssa.New("callee")
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 42, Results: []ssa.Value{v}})
		at := state(b, entry)
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{v}, State: at})
		// natives[address] holds the body, not the Go entry stub.
		calleeCode, _, _, err := compile.Lower(b.Build(), arm64.New(), callee, nil, 7, false, true)
		require.NoError(t, err)
		calleeBuffer, err := asm.NewBuffer(len(calleeCode))
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, calleeBuffer.Free()) })
		calleeBody, err := asm.Link(calleeBuffer, calleeCode)
		require.NoError(t, err)

		// exit() reads the caller's own bytecode to find the CALL's width
		// for the exit map's IP, even though the CALL op below is built
		// directly as SSA: Code must hold a real CALL at IP 0.
		caller := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal([]instr.Instruction{instr.New(instr.CALL)}),
		}
		cb := ssa.New("caller")
		centry := cb.Block()
		callee32 := cb.Value(ssa.TypeRef)
		got := cb.Value(ssa.TypeI32)
		cb.Add(centry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(7)), Results: []ssa.Value{callee32}})
		// Retained once and used only as this call's callee: borrowed, so
		// Call neither retains nor releases it (no Context.RC needed here).
		cb.Add(centry, ssa.Operation{Op: ssa.OpRetain, Args: []ssa.Value{callee32}})
		cat := cb.Value(ssa.TypeState)
		cb.Add(centry, ssa.Operation{
			Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Returns: 1, Stack: []ssa.Operand{{Value: callee32}}}},
			Results: []ssa.Value{cat},
		})
		cb.Add(centry, ssa.Operation{Op: ssa.OpExec, Code: instr.CALL, Args: []ssa.Value{callee32}, State: cat, Results: []ssa.Value{got}})
		rat := state(cb, centry)
		cb.Term(centry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{got}, State: rat})

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
		first := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		second := function(t, []types.Type{types.TypeI32}, nil, func(b *instr.Builder) {
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
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32)}, []types.Type{types.TypeI32, types.TypeI32}, func(b *instr.Builder) {
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
		require.Zero(t, ctx.Depth)
	})

	t.Run("retains a ref element a guarded array.get reads, through Context.RC", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
		insts, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeAny)}, Returns: []types.Type{types.TypeAny}},
			Code: instr.Marshal(insts),
		}

		array := &types.Array{Typ: types.NewArrayType(types.TypeAny), Elems: []types.Boxed{types.BoxRef(2)}}
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
		record := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("x")))
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
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(insts),
		}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Struct: record}},
		}
		f, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)

		s := types.NewStruct(record, types.BoxI32(5))
		heap := []types.Value{nil, nil, s}
		stack := make([]types.Boxed, 4)
		code, _ := lower(t, arm64.New(), f, fn, m.Objects, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(6), stack[0])
		require.Equal(t, uint64(6), s.Data[0])
	})

	t.Run("struct.set stores an i8 field sign-extended to 32 bits and zero-extended to 64, matching Struct.SetField", func(t *testing.T) {
		// types.(*Struct).SetField for KindI8 computes
		// uint64(uint32(int32(val.I8()))): sign-extend to 32 bits, then
		// zero-extend to 64. A raw 64-bit sign extension (SBFX into a
		// 64-bit destination) diverges for any negative int8.
		record := types.NewStructType(types.NewStructField(types.TypeI8, types.FieldWithName("x")))
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(1)), Results: []ssa.Value{ref}})
		guarded := b.Value(ssa.TypeRef)
		guardState := state(b, entry, ssa.Operand{Value: ref})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true}, Args: []ssa.Value{ref}, State: guardState, Results: []ssa.Value{guarded}})
		idx := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{idx}})
		val := b.Value(ssa.TypeI8)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0xFFFFFFFF, Results: []ssa.Value{val}})
		at := state(b, entry, ssa.Operand{Value: guarded}, ssa.Operand{Value: idx}, ssa.Operand{Value: val})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_SET, Args: []ssa.Value{guarded, idx, val}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		s := types.NewStruct(record, types.BoxI32(0))
		heap := []types.Value{nil, s}
		stack := []types.Boxed{0}
		code, _ := lower(t, arm64.New(), b.Build(), frame(nil, nil), nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		// int8(-1) -> int32(-1) -> uint32(0xFFFFFFFF) -> uint64(0x00000000FFFFFFFF).
		require.Equal(t, uint64(0xFFFFFFFF), s.Data[0])
	})

	t.Run("deopts a struct.set whose value kind differs from the field's declared kind", func(t *testing.T) {
		// Threaded SetField converts by the declared kind: i32 2 into an i1 field stores 1.
		record := types.NewStructType(types.NewStructField(types.TypeI1, types.FieldWithName("x")))
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(types.BoxRef(1)), Results: []ssa.Value{ref}})
		guarded := b.Value(ssa.TypeRef)
		guardState := state(b, entry, ssa.Operand{Value: ref})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardShape, Shape: ssa.Shape{Struct: true}, Args: []ssa.Value{ref}, State: guardState, Results: []ssa.Value{guarded}})
		idx := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{idx}})
		val := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{val}})
		at := state(b, entry, ssa.Operand{Value: guarded}, ssa.Operand{Value: idx}, ssa.Operand{Value: val})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.STRUCT_SET, Args: []ssa.Value{guarded, idx, val}, State: at})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		s := types.NewStruct(record, types.BoxI1(false))
		heap := []types.Value{nil, s}
		stack := []types.Boxed{0}
		code, exits := lower(t, arm64.New(), b.Build(), frame(nil, nil), nil, 0, false)
		ctx := enter(t, stack)
		ctx.Heap = address(t, heap)

		require.Equal(t, jit.TrapDeopt, jit.Enter(code, ctx))
		require.Equal(t, jit.ExitDeopt, exits[ctx.Exit()].Kind)
		require.Equal(t, uint64(0), s.Data[0])
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
			Typ:    &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}},
			Locals: []types.Type{types.TypeI32, types.TypeI32, types.TypeI32},
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
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32)}, nil, func(b *instr.Builder) {
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
		fn := function(t, []types.Type{types.NewArrayType(types.TypeF32)}, nil, func(b *instr.Builder) {
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
			Typ:  &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI1)}, Returns: []types.Type{types.TypeI1}},
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
		fn := function(t, []types.Type{types.NewArrayType(types.TypeI32)}, nil, func(b *instr.Builder) {
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
		fn := function(t, []types.Type{types.TypeI32}, []types.Type{types.TypeI32, types.TypeI32}, func(b *instr.Builder) {
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
		entry := instr.New(instr.I32_CONST, 0).Width()*2 + instr.New(instr.LOCAL_SET, 1).Width()
		f, err := transform.Translate(transform.Module{}, 1, fn, entry)
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
		require.Zero(t, ctx.Depth)
	})

	t.Run("deopts an OSR entry's own body with the right IP and values, locals left as the interpreter set them", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32}, []types.Type{types.TypeI32}, func(b *instr.Builder) {
			join := b.Label()
			b.Emit(instr.LOCAL_GET, 0).BrIf(join)
			b.Bind(join)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
		})
		entry := instr.New(instr.LOCAL_GET, 0).Width() + instr.New(instr.BR_IF, 0).Width()
		f, err := transform.Translate(transform.Module{}, 1, fn, entry)
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
		require.Equal(t, entry+instr.New(instr.LOCAL_GET, 0).Width()*2, exit.Frames[0].IP)
		require.Equal(t, []uint64{6, 0}, operands(ctx, exit.Frames[0]))
	})

	t.Run("completes module code from a header entry", func(t *testing.T) {
		b := instr.NewBuilder()
		join := b.Label()
		b.Emit(instr.I32_CONST, 1).BrIf(join)
		b.Bind(join).Emit(instr.I32_CONST, 42)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Code: instr.Marshal(code)}

		entry := instr.New(instr.I32_CONST, 0).Width() + instr.New(instr.BR_IF, 0).Width()
		f, err := transform.Translate(transform.Module{}, 0, fn, entry)
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
		fn := function(t, []types.Type{types.TypeI32, types.TypeI32}, nil, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(1), types.BoxI32(2)}
		const at = 4096 // 8*at exceeds the unsigned-offset LDR/STR imm12 (0xFFF) scaled range
		code, _ := lower(t, arm64.New(), translate(t, fn), fn, nil, at, false)

		ctx, err := jit.NewContext(4096)
		require.NoError(t, err)
		ctx.FB = address(t, stack)
		ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
		ctx.Limit = uint64(len(ctx.Records))
		ctx.Budget = 1000
		entries := make([]int64, at+1)
		ctx.Entries = address(t, entries)

		require.Equal(t, jit.TrapReturn, jit.Enter(code, ctx))
		require.Equal(t, types.BoxI32(3), stack[0])
		require.Equal(t, int64(1), entries[at])
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

// kindType is a representative types.Type for k, for a Returns declaration
// a test builds directly (types.Kinds goes the other way).
func kindType(t *testing.T, k types.Kind) types.Type {
	t.Helper()
	switch k {
	case types.KindI1:
		return types.TypeI1
	case types.KindI8:
		return types.TypeI8
	case types.KindI32:
		return types.TypeI32
	case types.KindF32:
		return types.TypeF32
	case types.KindF64:
		return types.TypeF64
	case types.KindRef:
		return types.TypeString
	default:
		t.Fatalf("kindType: unsupported kind %v", k)
		return nil
	}
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

// lower builds f, the translation of fn at address (an OSR unit when osr),
// with m and publishes it. It returns the Go entry stub's address: what a
// fresh jit.Enter crosses into native code through, not the body's own
// address at offset 0 (what natives[address] holds for a native-to-native
// call: see the "records a nested native call" case, which links directly).
func lower(t *testing.T, m compile.Machine, f *ssa.Function, fn *types.Function, objects transform.Objects, address int, osr bool) (uintptr, []jit.Exit) {
	t.Helper()
	code, exits, stub, err := compile.Lower(f, m, fn, objects, address, osr, !osr)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	body, err := asm.Link(buffer, code)
	require.NoError(t, err)
	return body + uintptr(stub), exits
}

// enter is a context whose next activation has its frame at stack[0].
func enter(t *testing.T, stack []types.Boxed) *jit.Context {
	t.Helper()
	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = address(t, stack)
	ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
	ctx.Limit = uint64(len(ctx.Records))
	ctx.Budget = 1000
	// entries backs the prologue's own entry count, indexed by address;
	// none of these cases uses past 7.
	entries := make([]int64, 8)
	ctx.Entries = address(t, entries)
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

// address is the base of s kept on the heap for the test's life: native code
// holds it as a uintptr, which a copied goroutine stack would leave dangling.
func address[T any](t *testing.T, s []T) uintptr {
	t.Cleanup(func() { runtime.KeepAlive(s) })
	return uintptr(unsafe.Pointer(&s[0]))
}

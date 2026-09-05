package frontend_test

import (
	"fmt"
	"math/rand"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/frontend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"

	"github.com/stretchr/testify/require"
)

func TestStatic(t *testing.T) {
	for _, tc := range corpus(t) {
		t.Run(tc.name, func(t *testing.T) {
			planned, emitted := agrees(t, tc.input)
			require.Equal(t, planned, emitted, "every root the plan takes must still be planned")
		})
	}

	t.Run("agrees with the plan over generated functions", func(t *testing.T) {
		rnd := rand.New(rand.NewSource(1))
		emitted := 0
		for range 4000 {
			_, n := agrees(t, generated(t, rnd))
			emitted += n
		}
		require.NotZero(t, emitted)
	})

	t.Run("emits one block per anchor, with its live operands and its guards", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				head, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
				b.Bind(head)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_LE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
				b.Br(head)
				b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			}),
		}

		out, err := frontend.Static(&jit.Input{Address: 1, Function: fn}, jit.Anchor{Addr: 1})
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		// The declared element kind resolves the container identity the access is
		// compiled against, which is a runtime address rather than a constant.
		shape, ok := jit.ElemShapeByKind(types.KindI32)
		require.True(t, ok)
		require.Equal(t, fmt.Sprintf(`func 1:0
blk0: ()
	v1:i32 = const 0
	v2:state = state {addr=1 base=0 ip=5 returns=1 stack=[v1]}
	store local[1], v1 state v2
	jump blk1()
blk1: () <-- (blk0, blk3)
	v3:ref = load local[0]
	v4:i32 = array.len v3
	v5:i32 = load local[1]
	v6:i1 = i32.le_s v4, v5
	br v6, blk2(), blk3()
blk2: () <-- (blk1)
	v7:i32 = load local[1]
	return v7
blk3: () <-- (blk1)
	v8:ref = load local[0]
	v9:i32 = load local[1]
	v11:state = state {addr=1 base=0 ip=20 returns=1 stack=[v8, v9]}
	v10:ref = guard.shape v8 itab 0x%x state v11
	v12:i32 = array.get v10, v9
	v13:i32 = load local[1]
	v14:i32 = i32.add v12, v13
	v15:state = state {addr=1 base=0 ip=24 returns=1 stack=[v14]}
	store local[1], v14 state v15
	jump blk1()
`, shape.Itab), ssa.Format(out))
	})
}

func TestBody(t *testing.T) {
	t.Run("translates a whole function", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				head, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
				b.Bind(head)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
				b.Br(head)
				b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			}),
		}

		out, err := frontend.Body(frontend.Module{}, 1, fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		static, err := frontend.Static(&jit.Input{Address: 1, Function: fn}, jit.Anchor{Addr: 1})
		require.NoError(t, err)
		require.Equal(t, ssa.Format(static), ssa.Format(out), "the entry root translates to the same function either way")
	})

	t.Run("translates module code the native calling convention refuses", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
		input := &jit.Input{
			Address: 0,
			Function: &types.Function{
				Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CALL) }),
			},
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   jit.Objects{2: {Fn: callee}},
		}

		static, err := frontend.Static(input, jit.Anchor{})
		require.NoError(t, err)
		require.Nil(t, static, "module entry does not implement the framed native-call ABI")

		out, err := frontend.Body(frontend.Module{Constants: input.Constants, Objects: input.Objects}, 0, input.Function)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
	})

	t.Run("declines what bytecode alone cannot resolve", func(t *testing.T) {
		for name, fn := range map[string]*types.Function{
			"no code": {Typ: &types.FunctionType{}},
			"a protected region": {
				Typ:      &types.FunctionType{Returns: []types.Type{types.TypeI32}},
				Handlers: []instr.Handler{{Start: 0, End: 5, Catch: 5}},
				Code:     assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) }),
			},
			"an unresolved callee": {
				Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
				Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.REF_NULL).Emit(instr.CALL).Emit(instr.RETURN) }),
			},
		} {
			t.Run(name, func(t *testing.T) {
				out, err := frontend.Body(frontend.Module{}, 1, fn)
				require.NoError(t, err)
				require.Nil(t, out)
			})
		}
	})
}

// agrees checks that the frontend plans no root the plan does not, that every
// function it emits verifies, that its blocks reach each other exactly as the
// plan's do, and that it records the ownership the plan does. It returns how
// many roots each side planned, so a caller holding bytecode program.Verify
// accepts can require the reverse as well: the frontend declines an operand of
// a kind its opcode cannot pop, which the plan never checks and verified
// bytecode never contains.
func agrees(t *testing.T, input *jit.Input) (int, int) {
	t.Helper()

	plans, planned := jit.StaticPlan(input)
	roots := map[jit.Anchor]jit.Plan{}
	for _, plan := range plans {
		roots[plan.Anchor] = plan
	}
	emitted := 0
	for _, anchor := range anchors(input) {
		fn, err := frontend.Static(input, anchor)
		require.Equal(t, planned == nil, err == nil, "anchor %+v", anchor)
		if err != nil || fn == nil {
			continue
		}
		emitted++
		plan, ok := roots[anchor]
		require.True(t, ok, "anchor %+v is not a root the plan takes\n%s", anchor, instr.Format(input.Function.Code))
		require.NoError(t, ssa.Verify(fn), "anchor %+v\n%s", anchor, ssa.Format(fn))
		blocks, want := graph(plan)
		ids, got := breadth(0, fn.Len(), fn.Succ)
		require.Equal(t, want, got, "anchor %+v\n%s", anchor, ssa.Format(fn))
		owns(t, plan, blocks, fn, ids)
		adopts(t, fn)
		targets(t, fn)
	}
	return len(roots), emitted
}

// owns checks that the frontend records the ownership the plan resolved. The
// two forms state it in different places - jit.Block.State on the plan, a stack
// entry of every ssa.Frame a deopt materializes in the SSA - so they are read
// where they name the same operand: a block's own parameters, paired by the
// breadth-first order both graphs are numbered in. An entry the plan backs with
// the operand-stack copy itself (jit.BackingStack) carries the reference count
// the interpreter adopts on resuming; one deferred to a local, a global, an
// upvalue, or a constant does not. Only the states before the block first
// retains that parameter say anything about its entry ownership: after a
// retain, the entry owns a count the plan's entry state never had.
func owns(t *testing.T, plan jit.Plan, blocks []int, fn *ssa.Function, ids []int) {
	t.Helper()
	for n, block := range blocks {
		if block >= len(plan.Blocks) || n >= len(ids) || ids[n] >= fn.Len() {
			continue
		}
		state, blk := plan.Blocks[block].State, fn.Block(ids[n])
		require.Len(t, blk.Params, len(state), "blk%d is entered with a different stack\n%s", ids[n], ssa.Format(fn))
		for i, slot := range state {
			if slot.Kind != types.KindRef {
				continue
			}
			want, param := slot.Backing == jit.BackingStack, blk.Params[i]
			for _, op := range blk.Ops {
				if op.Op == ssa.OpRetain && op.Args[0] == param {
					break
				}
				for _, frame := range op.Frames {
					for _, operand := range frame.Stack {
						if operand.Value == param {
							require.Equal(t, want, operand.Owned, "blk%d v%d\n%s", ids[n], param, ssa.Format(fn))
						}
					}
				}
			}
		}
	}
}

// adopts checks the rule every handoff out of native code shares: a bridge, a
// call, and a deopt hand the flushed operand stack to code that adopts every
// reference on it and later releases each one, so none may still be borrowed
// there. It is the barrier the plan emits at its cold path instead - ownRefs
// before a call, retainDeferred before a fallback or a bridge - stated in the
// IR here.
func adopts(t *testing.T, fn *ssa.Function) {
	t.Helper()
	states := map[ssa.Value]ssa.Operation{}
	for id := 0; id < fn.Len(); id++ {
		for _, op := range fn.Block(id).Ops {
			if op.Op == ssa.OpState {
				states[op.Results[0]] = op
			}
		}
	}
	owned := func(state ssa.Value) {
		for _, frame := range states[state].Frames {
			for _, operand := range frame.Stack {
				if fn.Type(operand.Value) == ssa.TypeRef {
					require.True(t, operand.Owned, "v%d is handed over borrowed\n%s", operand.Value, ssa.Format(fn))
				}
			}
		}
	}
	for id := 0; id < fn.Len(); id++ {
		blk := fn.Block(id)
		for _, op := range blk.Ops {
			if op.Op == ssa.OpBridge || (op.Op == ssa.OpExec && op.Code.Writes(instr.Frame)) {
				owned(op.State)
			}
		}
		if blk.Term.Op == ssa.OpExit {
			owned(blk.Term.State)
		}
	}
}

// targets checks the rule every call shares: its callee operand resolves to a
// compile-time reference, which is the whole of what a lowering needs to reach
// the function it enters. A static call carries the constant itself; a
// speculated one carries the observed reference through the guard admitting it,
// so both are read through the one question asked here.
func targets(t *testing.T, fn *ssa.Function) {
	t.Helper()
	for id := 0; id < fn.Len(); id++ {
		for _, op := range fn.Block(id).Ops {
			if op.Op != ssa.OpExec || !op.Code.Writes(instr.Frame) {
				continue
			}
			require.NotEmpty(t, op.Args, "a call names no callee\n%s", ssa.Format(fn))
			// The callee is popped first, so it is the last of the arguments
			// an operation holds bottom of the stack first.
			at := op.Args[len(op.Args)-1]
			callee, ok := defines(fn, at)
			require.True(t, ok, "v%d is a block parameter, not a callee\n%s", at, ssa.Format(fn))
			if callee.Op == ssa.OpGuardValue {
				callee, ok = defines(fn, callee.Args[1])
				require.True(t, ok, "v%d admits a block parameter\n%s", at, ssa.Format(fn))
			}
			require.Equal(t, ssa.OpConst, callee.Op, "v%d does not resolve to a reference\n%s", at, ssa.Format(fn))
			require.Equal(t, types.KindRef, callee.Const.Kind(), "v%d does not resolve to a reference\n%s", at, ssa.Format(fn))
			require.Positive(t, callee.Const.Ref(), "v%d resolves to no heap cell\n%s", at, ssa.Format(fn))
		}
	}
}

// defines returns the operation defining v, and false for a block parameter,
// which no operation defines.
func defines(fn *ssa.Function, v ssa.Value) (ssa.Operation, bool) {
	for id := 0; id < fn.Len(); id++ {
		for _, op := range fn.Block(id).Ops {
			for _, result := range op.Results {
				if result == v {
					return op, true
				}
			}
		}
	}
	return ssa.Operation{}, false
}

// fixture is one planning input together with the name its case runs under.
type fixture struct {
	name  string
	input *jit.Input
}

// corpus is the set of functions the differential runs over: the shapes the
// static planner accepts, one shape per rule it rejects on, and the kernel
// shapes the benchmarks exercise.
func corpus(t *testing.T) []fixture {
	t.Helper()

	loop := func(b *instr.Builder) {
		head, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(head)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(head)
		b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	}
	counter := func() *types.Function {
		return &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   assemble(t, loop),
		}
	}
	callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
	record := types.NewStructType(types.NewStructField(types.TypeF64))
	array := types.TypedArray[int32]{1, 2, 3}
	arrayType := types.NewArrayType(types.TypeI32)

	var out []fixture
	add := func(name string, input *jit.Input) {
		out = append(out, fixture{name: name, input: input})
	}

	add("straight line", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 2).Emit(instr.I32_CONST, 3).Emit(instr.I32_ADD).Emit(instr.RETURN)
		}),
	}})

	add("branch", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			other, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 1).BrIf(other)
			b.Emit(instr.I32_CONST, 2).Br(done)
			b.Bind(other).Emit(instr.I32_CONST, 3)
			b.Bind(done).Emit(instr.RETURN)
		}),
	}})

	add("branch table", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			one, two, done := b.Label(), b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 1).BrTable(done, one, two)
			b.Bind(one).Emit(instr.I32_CONST, 1).Br(done)
			b.Bind(two).Emit(instr.I32_CONST, 2)
			b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
		}),
	}})

	add("loop", &jit.Input{Address: 1, Function: counter()})
	add("loop already installed", &jit.Input{Address: 1, Function: counter(), Installed: true})

	add("nested loops", &jit.Input{Address: 1, Function: &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32, types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			outer, inner, mid, done := b.Label(), b.Label(), b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(outer)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(inner)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(mid)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(inner)
			b.Bind(mid)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(outer)
			b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		}),
	}})

	add("float kernel", &jit.Input{Address: 1, Function: &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeF64}},
		Locals: []types.Type{types.TypeF64, types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			head, done := b.Label(), b.Label()
			b.Emit(instr.F64_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
			b.Bind(head)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_F64_S).Emit(instr.F64_ADD).Emit(instr.LOCAL_SET, 0)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(head)
			b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		}),
	}})

	add("stack shuffles", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2).Emit(instr.SWAP).Emit(instr.DUP)
			b.Emit(instr.DROP).Emit(instr.I32_ADD).Emit(instr.RETURN)
		}),
	}})

	add("select", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_CONST, 0).Emit(instr.SELECT).Emit(instr.RETURN)
		}),
	}})

	add("direct call", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN) }),
		},
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Fn: callee}},
	})

	add("unknown callee", &jit.Input{Address: 1, Function: &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.REF_NULL).Emit(instr.CALL).Emit(instr.RETURN) }),
	}})

	add("module", &jit.Input{Address: 0, Function: &types.Function{
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.DROP) }),
	}})

	add("module holding a call", &jit.Input{
		Address: 0,
		Function: &types.Function{
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP) }),
		},
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Fn: callee}},
	})

	add("constant array read", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			}),
		},
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Array: jit.Itab(array)}},
	})

	add("constant struct read", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
			}),
		},
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Typ: record}},
	})

	declaredArray := func(extra func(b *instr.Builder)) *types.Function {
		return &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{arrayType}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				head, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
				b.Bind(head)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_LE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.DROP)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
				b.Br(head)
				b.Bind(done)
				if extra != nil {
					extra(b)
				}
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			}),
		}
	}
	add("declared array read", &jit.Input{Address: 1, Function: declaredArray(nil)})
	add("declared array read beside a call", &jit.Input{
		Address:   1,
		Function:  declaredArray(func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP) }),
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Fn: callee}},
	})

	// A reference live across a branch is what makes a block's entry ownership
	// observable: both successors take it as a parameter, borrowed from the slot
	// it was loaded out of in one shape and owned by the stack in the other.
	add("borrowed array across a branch", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Params: []types.Type{arrayType, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			done := b.Label()
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).BrIf(done)
			b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			b.Bind(done).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
		}),
	}})

	add("owned array across a branch", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				done := b.Label()
				b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.REF_CAST, 0)
				b.Emit(instr.LOCAL_GET, 0).BrIf(done)
				b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
				b.Bind(done).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			}),
		},
		Decl: []types.Type{arrayType},
	})

	add("array store", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Params: []types.Type{arrayType}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_SET).Emit(instr.RETURN)
		}),
	}})

	add("bridged allocation", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.ARRAY_LEN).Emit(instr.RETURN)
		}),
	}})

	add("bridged allocation inside a loop", &jit.Input{Address: 1, Function: &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32},
		Code: assemble(t, func(b *instr.Builder) {
			head, done := b.Label(), b.Label()
			b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
			b.Bind(head)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.DROP)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(head)
			b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		}),
	}})

	add("bridged allocation at the end", &jit.Input{Address: 1, Function: &types.Function{
		Typ:  &types.FunctionType{Returns: []types.Type{types.TypeAny}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 2).Emit(instr.ARRAY_NEW_DEFAULT, 0) }),
	}})

	add("struct allocation", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.F64_CONST, 0).Emit(instr.STRUCT_NEW, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
			}),
		},
		Decl: []types.Type{record},
	})

	add("closure allocation", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeAny}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.RETURN) }),
		},
		Constants: []types.Boxed{types.BoxRef(2)},
		Objects:   jit.Objects{2: {Fn: callee}},
	})

	add("map read", &jit.Input{Address: 1, Function: &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LEN).Emit(instr.RETURN) }),
	}})

	add("reference locals", &jit.Input{Address: 1, Function: &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeAny}},
		Locals: []types.Type{types.TypeAny},
		Code: assemble(t, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_TEE, 1).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
		}),
	}})

	add("global and upvalue", &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ:      &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Captures: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.UPVAL_GET, 0).Emit(instr.GLOBAL_GET, 0).Emit(instr.I32_ADD).Emit(instr.GLOBAL_SET, 0)
				b.Emit(instr.GLOBAL_GET, 0).Emit(instr.RETURN)
			}),
		},
		Globals: []types.Kind{types.KindI32},
	})

	add("unreachable block", &jit.Input{Address: 1, Function: &types.Function{
		Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Code: assemble(t, func(b *instr.Builder) {
			done := b.Label()
			b.Emit(instr.I32_CONST, 1).Br(done)
			b.Emit(instr.I32_CONST, 2).Emit(instr.DROP)
			b.Bind(done).Emit(instr.RETURN)
		}),
	}})

	add("handlers", &jit.Input{Address: 1, Function: &types.Function{
		Typ:      &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Handlers: []instr.Handler{{Start: 0, End: 5, Catch: 5}},
		Code:     assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) }),
	}})

	add("empty", &jit.Input{Address: 1, Function: &types.Function{Typ: &types.FunctionType{}}})

	return out
}

// anchors is every entry a plan could be asked for: the function entry, the
// start of every instruction, and one offset that starts none.
func anchors(input *jit.Input) []jit.Anchor {
	out := []jit.Anchor{{Addr: input.Address}}
	code := input.Function.Code
	for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
		if ip > 0 {
			out = append(out, jit.Anchor{Addr: input.Address, IP: ip})
		}
	}
	return append(out, jit.Anchor{Addr: input.Address, IP: len(code) + 1})
}

// graph is a plan's blocks in breadth-first order from its root, and the
// adjacency list that order numbers them by - the one numbering both forms
// share. A bridging block names no edge - resuming from the interpreter is a
// fresh entry rather than a branch - so the block planned right after it is
// its successor.
func graph(plan jit.Plan) ([]int, [][]int) {
	succs := func(id int) []int {
		block := plan.Blocks[id]
		if block.Term.Kind == jit.TerminateBridge {
			return []int{id + 1}
		}
		out := make([]int, 0, len(block.Term.Edges))
		for _, edge := range block.Term.Edges {
			out = append(out, edge.Index)
		}
		return out
	}
	return breadth(plan.Root, len(plan.Blocks), succs)
}

// breadth renumbers a reachable subgraph by the order a breadth-first walk from
// root reaches it, so two graphs are equal exactly when they have the same
// blocks and the same edges between them. It returns that order beside the
// adjacency list, because the order is what pairs one form's block with the
// other's. An edge naming no block leaves the unit entirely and meets one node
// standing for outside, which is where a plan puts a branch past the end of the
// code and the SSA puts the block it lays out for it.
func breadth(root, size int, succs func(int) []int) ([]int, [][]int) {
	next := func(block int) []int {
		if block >= size {
			return nil
		}
		out := make([]int, 0, len(succs(block)))
		for _, succ := range succs(block) {
			if succ < 0 || succ >= size {
				succ = size
			}
			out = append(out, succ)
		}
		return out
	}
	id := make([]int, size+1)
	for i := range id {
		id[i] = -1
	}
	id[root] = 0
	order := []int{root}
	for n := 0; n < len(order); n++ {
		for _, succ := range next(order[n]) {
			if id[succ] < 0 {
				id[succ] = len(order)
				order = append(order, succ)
			}
		}
	}
	out := make([][]int, len(order))
	for n, block := range order {
		for _, succ := range next(block) {
			out[n] = append(out[n], id[succ])
		}
	}
	return order, out
}

// assemble builds the code of one function.
func assemble(t *testing.T, emit func(b *instr.Builder)) []byte {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	instructions, err := b.Assemble()
	require.NoError(t, err)
	return instr.Marshal(instructions)
}

// generated builds one random function out of stack-neutral phrases over the
// opcodes a static plan has a rule for, with every branch aimed at a phrase
// boundary. It exists so the differential runs over shapes nobody wrote down:
// loops nobody laid out, a bridge in an odd place, an arity that resolves only
// sometimes, a branch past the end of the code.
func generated(t *testing.T, rnd *rand.Rand) *jit.Input {
	t.Helper()

	i32 := func() instr.Instruction {
		return []instr.Instruction{
			instr.New(instr.I32_CONST, uint64(rnd.Intn(4))),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.GLOBAL_GET, 0),
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.CONST_GET, 2),
		}[rnd.Intn(5)]
	}
	ref := func() instr.Instruction {
		return []instr.Instruction{
			instr.New(instr.REF_NULL),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.GLOBAL_GET, 1),
			instr.New(instr.CONST_GET, 1),
		}[rnd.Intn(5)]
	}
	array := func() instr.Instruction {
		if rnd.Intn(2) == 0 {
			return instr.New(instr.LOCAL_GET, 0)
		}
		return instr.New(instr.CONST_GET, 1)
	}
	// Every phrase leaves the operand stack as it found it, so a branch to any
	// boundary between them meets a stack the other paths agree with.
	phrases := []func() []instr.Instruction{
		func() []instr.Instruction { return []instr.Instruction{instr.New(instr.NOP)} },
		func() []instr.Instruction { return []instr.Instruction{instr.New(instr.UNREACHABLE)} },
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.LOCAL_SET, 1)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.LOCAL_TEE, 1), instr.New(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.GLOBAL_SET, 0)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.New(instr.LOCAL_SET, 2)} },
		func() []instr.Instruction { return []instr.Instruction{ref(), instr.New(instr.DROP)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.I32_ADD), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.I32_LT_S), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.DUP), instr.New(instr.I32_ADD), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), instr.New(instr.SWAP), instr.New(instr.DROP), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), i32(), i32(), instr.New(instr.SELECT), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), instr.New(instr.ARRAY_LEN), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), i32(), instr.New(instr.ARRAY_GET), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), i32(), i32(), instr.New(instr.ARRAY_SET)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{array(), i32(), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_APPEND), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.ARRAY_NEW_DEFAULT, 1), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.STRUCT_NEW, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), instr.New(instr.REF_CAST, 1), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), instr.New(instr.REF_IS_NULL), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), instr.New(instr.MAP_LEN), instr.New(instr.DROP)}
		},
		func() []instr.Instruction {
			return []instr.Instruction{ref(), ref(), instr.New(instr.MAP_GET), instr.New(instr.DROP)}
		},
		func() []instr.Instruction { return []instr.Instruction{i32(), instr.New(instr.THROW)} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.CONST_GET, 0), instr.New(instr.RETURN_CALL)}
		},
		func() []instr.Instruction { return []instr.Instruction{instr.New(instr.BR, uint64(rnd.Intn(8)))} },
		func() []instr.Instruction {
			return []instr.Instruction{i32(), instr.New(instr.BR_IF, uint64(rnd.Intn(8)))}
		},
	}

	var code []instr.Instruction
	var bounds []int
	width := 0
	for n := 2 + rnd.Intn(9); len(bounds) < n; {
		bounds = append(bounds, width)
		for _, inst := range phrases[rnd.Intn(len(phrases))]() {
			code = append(code, inst)
			width += inst.Width()
		}
	}
	bounds = append(bounds, width)
	tail := []instr.Instruction{i32(), instr.New(instr.RETURN)}
	code = append(code, tail...)
	// A branch names a boundary rather than a byte, and the last boundary is one
	// past the end of the code, the virtual exit no block is laid out for. The
	// operand it carries is the signed distance from the instruction after it.
	for ip, at := 0, 0; at < len(code); at++ {
		inst := code[at]
		next := ip + inst.Width()
		if inst.Opcode() == instr.BR || inst.Opcode() == instr.BR_IF {
			inst.SetOperand(0, uint64(uint16(int16(bounds[int(inst.Operand(0))%len(bounds)]-next))))
		}
		ip = next
	}

	record := types.NewStructType(types.NewStructField(types.TypeI32))
	return &jit.Input{
		Address: 1,
		Function: &types.Function{
			Typ:      &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Locals:   []types.Type{types.TypeI32, types.TypeAny},
			Captures: []types.Type{types.TypeI32},
			Code:     instr.Marshal(code),
		},
		Constants: []types.Boxed{types.BoxRef(2), types.BoxRef(3), types.BoxI32(7)},
		Globals:   []types.Kind{types.KindI32, types.KindRef},
		Objects: jit.Objects{
			2: {Fn: &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}}},
			3: {Array: jit.Itab(types.TypedArray[int32]{1})},
		},
		Decl: []types.Type{record, types.NewArrayType(types.TypeI32)},
	}
}

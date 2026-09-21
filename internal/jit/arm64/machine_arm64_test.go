package arm64_test

import (
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
		ctx := run(t, arm64.New(), translate(t, fn), 2, 0, stack)
		require.Equal(t, types.BoxI32(43), stack[0])
		require.Zero(t, ctx.Depth)
	})

	t.Run("runs a translated loop through its budget", func(t *testing.T) {
		fn := function(t, []types.Type{types.TypeI32}, []types.Type{types.TypeI32, types.TypeI32}, func(b *instr.Builder) {
			loop, done := b.Label(), b.Label()
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Br(loop)
			b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
		})
		stack := []types.Boxed{types.BoxI32(10), types.BoxI32(99), types.BoxI32(99)}
		ctx := run(t, arm64.New(), translate(t, fn), 1, 2, stack)
		require.Equal(t, types.BoxI32(45), stack[0])
		require.Equal(t, int64(1000-11), ctx.Budget)
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
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: args})

		stack := make([]types.Boxed, len(consts))
		run(t, arm64.New(), b.Build(), 0, len(consts), stack)
		require.Equal(t, consts, stack)
	})

	t.Run("copies a reference between locals", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		ref := b.Value(ssa.TypeRef)
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{ref}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, State: state})
		b.Add(entry, ssa.Operation{Op: ssa.OpStore, Slot: ssa.Slot{Space: ssa.SpaceLocal, Index: 1}, Args: []ssa.Value{ref}, State: state})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn})

		stack := []types.Boxed{types.BoxRef(7), 0}
		run(t, arm64.New(), b.Build(), 1, 1, stack)
		require.Equal(t, types.BoxRef(7), stack[1])
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
		_, err := compile.Lower(translate(t, first), m, 2, 0)
		require.NoError(t, err)

		stack := []types.Boxed{types.BoxI32(0)}
		run(t, m, translate(t, second), 1, 0, stack)
		require.Equal(t, types.BoxI32(1), stack[0])
	})
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

// run lowers f with m and enters it with its frame at stack[0].
func run(t *testing.T, m compile.Machine, f *ssa.Function, params, locals int, stack []types.Boxed) *jit.Context {
	t.Helper()
	code, err := compile.Lower(f, m, params, locals)
	require.NoError(t, err)
	buffer, err := asm.NewBuffer(len(code))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, buffer.Free()) })
	address, err := asm.Link(buffer, code)
	require.NoError(t, err)

	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))
	ctx.Budget = 1000
	require.Equal(t, jit.TrapReturn, jit.Enter(address, ctx))
	return ctx
}

package backend_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestCompiler_Exit(t *testing.T) {
	t.Run("resolves the frame chain into journal records innermost first", func(t *testing.T) {
		// The outer function occupies two stack slots - one parameter and one
		// local - and the inner one, so an operand's slot is its frame's base
		// plus that count plus its own position on that frame's stack.
		outer := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
		}
		inner := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}}

		b := ssa.New("f")
		entry := b.Block()
		var live [3]ssa.Value
		for i := range live {
			live[i] = b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(int32(i)), Results: []ssa.Value{live[i]}})
		}
		held := b.Value(ssa.TypeRef)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Index: 0}, Results: []ssa.Value{held}})
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Addr: 1, Base: 0, IP: 10, Returns: 1, Stack: []ssa.Operand{{Value: live[0]}, {Value: live[1]}}},
			{Addr: 2, Base: 4, IP: 20, Returns: 1, Stack: []ssa.Operand{{Value: live[2]}, {Value: held, Owned: true}}},
		}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{live[0]}, State: state, Results: []ssa.Value{b.Value(ssa.TypeI32)}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{guard: prof.ExitGuardKind, opcode: int(instr.ARRAY_GET)}
		input := &jit.Input{Objects: jit.Objects{1: {Fn: outer}, 2: {Fn: inner}}}
		code, ok := backend.Compile(m, asm.New(arm64.New()), input, jit.Anchor{}, f)
		require.True(t, ok)

		require.Equal(t, []backend.Deopt{{
			ID:     0,
			Resume: 20,
			SP:     7,
			Slots: []backend.Flush{
				{Value: live[0], Slot: 2},
				{Value: live[1], Slot: 3},
				{Value: live[2], Slot: 5},
				{Value: held, Slot: 6, Owned: true},
			},
			Frames: []backend.Record{
				{Addr: 2, BP: 4, IP: 20, Returns: 1},
				{Addr: 1, BP: 0, IP: 10, Returns: 1},
			},
		}}, m.deopts)
		require.Equal(t, []jit.Exit{{Reason: prof.ExitGuardKind, Opcode: int(instr.ARRAY_GET)}}, code.Exits)
	})

	t.Run("boxes a promoted local into the frame slot it came out of", func(t *testing.T) {
		// The function occupies two stack slots, so its own locals are slots 0
		// and 1 and its operands start at 2: a promoted local is written below
		// the operands it shares a frame with, which keeps the whole flush in
		// ascending slot order.
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
		}

		b := ssa.New("f")
		entry := b.Block()
		counter := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(7), Results: []ssa.Value{counter}})
		live := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(8), Results: []ssa.Value{live}})
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Addr: 1, IP: 5, Returns: 1, Stack: []ssa.Operand{{Value: live}}, Locals: []ssa.Local{{Index: 1, Value: counter}}},
		}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{live}, State: state, Results: []ssa.Value{b.Value(ssa.TypeI32)}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{guard: prof.ExitGuardKind, opcode: int(instr.ARRAY_GET)}
		input := &jit.Input{Objects: jit.Objects{1: {Fn: fn}}}
		_, ok := backend.Compile(m, asm.New(arm64.New()), input, jit.Anchor{}, f)
		require.True(t, ok)

		require.Equal(t, []backend.Flush{
			{Value: counter, Slot: 1},
			{Value: live, Slot: 2},
		}, m.deopts[0].Slots)
		require.Equal(t, 3, m.deopts[0].SP)
	})

	t.Run("registers no descriptor for an exit that reports none", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{v}})
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{
			{Addr: 1, Base: 0, IP: 7, Returns: 0, Stack: []ssa.Operand{{Value: v}}},
		}, Results: []ssa.Value{state}})
		b.Add(entry, ssa.Operation{Op: ssa.OpGuardKind, Args: []ssa.Value{v}, State: state, Results: []ssa.Value{b.Value(ssa.TypeI32)}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{guard: prof.ExitNone}
		input := &jit.Input{Objects: jit.Objects{1: {Fn: &types.Function{}}}}
		code, ok := backend.Compile(m, asm.New(arm64.New()), input, jit.Anchor{}, f)
		require.True(t, ok)

		require.Equal(t, []backend.Deopt{{ID: -1, Resume: 7, SP: 1, Slots: []backend.Flush{{Value: v, Slot: 0}}, Frames: []backend.Record{{Addr: 1, IP: 7}}}}, m.deopts)
		require.Empty(t, code.Exits)
	})

	t.Run("resolves nothing for a value that is not interpreter state", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		v := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{v}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})
		f := b.Build()
		require.NoError(t, ssa.Verify(f))

		m := &machine{}
		_, ok := backend.Compile(m, asm.New(arm64.New()), &jit.Input{}, jit.Anchor{}, f)
		require.True(t, ok)

		require.Equal(t, backend.Deopt{}, m.compiler.Exit(v, prof.ExitGuardKind, 0))
		require.Equal(t, backend.Deopt{}, m.compiler.Exit(ssa.NoValue, prof.ExitGuardKind, 0))
	})

	t.Run("refuses a compile whose frame names no function", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		state := b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, IP: 3}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpComplete})

		_, ok := backend.Compile(&machine{}, asm.New(arm64.New()), &jit.Input{}, jit.Anchor{}, b.Build())
		require.False(t, ok)
	})
}

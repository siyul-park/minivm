package transform_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestTranslate(t *testing.T) {
	t.Run("translates a whole function", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				header, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
				b.Bind(header)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
				b.Br(header)
				b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = const 0
	v2:state = state {addr=1 base=0 ip=5 returns=1 stack=[v1]}
	store local[0], v1 state v2
	jump blk1()
blk1: () <-- (blk0, blk3)
	v3:i32 = load local[0]
	v4:i32 = const 10
	v6:state = state {addr=1 base=0 ip=14 returns=1 stack=[v3, v4]}
	v5:i1 = i32.ge_s v3, v4 state v6
	br v5, blk2(), blk3()
blk2: () <-- (blk1)
	v7:i32 = load local[0]
	return v7
blk3: () <-- (blk1)
	v8:i32 = load local[0]
	v9:i32 = const 1
	v11:state = state {addr=1 base=0 ip=25 returns=1 stack=[v8, v9]}
	v10:i32 = i32.add v8, v9 state v11
	v12:state = state {addr=1 base=0 ip=26 returns=1 stack=[v10]}
	store local[0], v10 state v12
	jump blk1()
`, ssa.Format(out))
	})

	t.Run("merges facts at a join", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				join := b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).BrIf(join)
				b.Emit(instr.DROP).Emit(instr.I32_CONST, 2)
				b.Bind(join).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "return")
	})

	t.Run("translates module code the native calling convention refuses", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
		fn := &types.Function{
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.CONST_GET, 0).Emit(instr.CALL) })}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Function: callee}}}

		out, err := transform.Translate(m, 0, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 0:0
blk0: ()
	v1:ref = const 2
	retain v1
	v3:state = state {addr=0 base=0 ip=3 returns=0 stack=[v1 owned]}
	v2:i32 = call v1 state v3
	complete v2
`, ssa.Format(out))
	})

	t.Run("gives every operation the state at its own instruction", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2).Emit(instr.I32_ADD).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = const 1
	v2:i32 = const 2
	v4:state = state {addr=1 base=0 ip=10 returns=1 stack=[v1, v2]}
	v3:i32 = i32.add v1, v2 state v4
	return v3
`, ssa.Format(out))
	})

	t.Run("translates from a loop header", func(t *testing.T) {
		fn, header := loopFunction(t)

		root, err := transform.Translate(transform.Module{}, 1, fn, header)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(root))
		require.Equal(t, `func 1:2
blk0: (v1:ref) <-- (blk2)
	v2:i32 = load local[1]
	br v2, blk1(v1), blk2(v1)
blk1: (v3:ref) <-- (blk0)
	return v3
blk2: (v4:ref) <-- (blk0)
	v5:i32 = load local[1]
	v6:i32 = const 1
	v8:state = state {addr=1 base=0 ip=14 returns=1 stack=[v4 owned, v5, v6]}
	v7:i32 = i32.sub v5, v6 state v8
	v9:state = state {addr=1 base=0 ip=15 returns=1 stack=[v4 owned, v7]}
	store local[1], v7 state v9
	jump blk0(v4)
`, ssa.Format(root))

		entry, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(entry))
		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	jump blk1(v1)
blk1: (v2:ref) <-- (blk0, blk3)
	v3:i32 = load local[1]
	br v3, blk2(v2), blk3(v2)
blk2: (v4:ref) <-- (blk1)
	retain v4
	return v4
blk3: (v5:ref) <-- (blk1)
	v6:i32 = load local[1]
	v7:i32 = const 1
	v9:state = state {addr=1 base=0 ip=14 returns=1 stack=[v5, v6, v7]}
	v8:i32 = i32.sub v6, v7 state v9
	v10:state = state {addr=1 base=0 ip=15 returns=1 stack=[v5, v8]}
	store local[1], v8 state v10
	jump blk1(v5)
`, ssa.Format(entry))
	})

	t.Run("rejects an entry that starts no block", func(t *testing.T) {
		fn, _ := loopFunction(t)
		_, err := transform.Translate(transform.Module{}, 1, fn, 1)
		require.ErrorIs(t, err, transform.ErrEntry)
		require.EqualError(t, err, "entry starts no block: entry 1 starts no block")
	})

	t.Run("leaves out a header no path from the entry reaches", func(t *testing.T) {
		b := instr.NewBuilder()
		header := b.Label()
		b.Emit(instr.RETURN)
		headerOffset := instr.New(instr.RETURN).Width()
		b.Bind(header).Emit(instr.I32_CONST, 1).Emit(instr.DROP).Br(header)
		code, err := b.Assemble()
		require.NoError(t, err)

		fn := &types.Function{Code: instr.Marshal(code)}
		out, err := transform.Translate(transform.Module{}, 1, fn, headerOffset)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/no code", func(t *testing.T) {
		fn := &types.Function{Typ: &types.FunctionType{}}
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/a protected region", func(t *testing.T) {
		fn := &types.Function{
			Typ:      &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Handlers: []instr.Handler{{Start: 0, End: 5, Catch: 5}},
			Code:     assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/an unresolved callee", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.REF_NULL).Emit(instr.CALL).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/a constant read no object resolves", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{}}
		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines a function holding a suspension", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.YIELD).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("translates a tee of a reference", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeAny}},
			Locals: []types.Type{types.TypeAny},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.REF_NULL).Emit(instr.LOCAL_TEE, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
	})

	t.Run("guards an array load through a declared element kind", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				header, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
				b.Bind(header)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_LEN).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_LE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
				b.Br(header)
				b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = const 0
	v2:state = state {addr=1 base=0 ip=5 returns=1 stack=[v1]}
	store local[1], v1 state v2
	jump blk1()
blk1: () <-- (blk0, blk3)
	v3:ref = load local[0]
	v5:state = state {addr=1 base=0 ip=9 returns=1 stack=[v3]}
	v4:i32 = array.len v3 state v5
	v6:i32 = load local[1]
	v8:state = state {addr=1 base=0 ip=12 returns=1 stack=[v4, v6]}
	v7:i1 = i32.le_s v4, v6 state v8
	br v7, blk2(), blk3()
blk2: () <-- (blk1)
	v9:i32 = load local[1]
	return v9
blk3: () <-- (blk1)
	v10:ref = load local[0]
	v11:i32 = load local[1]
	v13:state = state {addr=1 base=0 ip=20 returns=1 stack=[v10, v11]}
	v12:ref = guard.shape v10 tag 0x3 state v13
	v14:i32 = array.get v12, v11 state v13
	v15:i32 = load local[1]
	v17:state = state {addr=1 base=0 ip=23 returns=1 stack=[v14, v15]}
	v16:i32 = i32.add v14, v15 state v17
	v18:state = state {addr=1 base=0 ip=24 returns=1 stack=[v16]}
	store local[1], v16 state v18
	jump blk1()
`, ssa.Format(out))
	})

	t.Run("guards an array store through the shape the stored value's own kind names", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_SET).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	v2:i32 = const 0
	v3:i32 = const 5
	v5:state = state {addr=1 base=0 ip=12 returns=0 stack=[v1, v2, v3]}
	v4:ref = guard.shape v1 tag 0x3 state v5
	array.set v4, v2, v3 state v5
	return
`, ssa.Format(out))
	})

	t.Run("guards a struct store through a generic struct shape", func(t *testing.T) {
		record := types.NewStructType(types.NewStructField(types.TypeI32))
		fn := &types.Function{
			Typ: &types.FunctionType{},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_NEW, 0).
					Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 5).Emit(instr.STRUCT_SET).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{Types: []types.Type{record}}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = const 0
	v3:state = state {addr=1 base=0 ip=5 returns=0 stack=[v1]}
	v2:ref = struct.new v1 state v3
	v4:i32 = const 0
	v5:i32 = const 5
	v7:state = state {addr=1 base=0 ip=18 returns=0 stack=[v2 owned, v4, v5]}
	v6:ref = guard.shape v2 tag 0x8 state v7
	struct.set v6, v4, v5 state v7
	release v6 state v7
	return
`, ssa.Format(out))
	})

	t.Run("reads a struct field through a constant cell's resolved record", func(t *testing.T) {
		record := types.NewStructType(types.NewStructField(types.TypeF64))
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Type: record}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "v3:ref = guard.shape v1 tag 0x8 type ")
		require.Contains(t, ssa.Format(out), "f64 = struct.get v3, v2")
	})

	t.Run("ends a tail call by leaving native execution", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}}
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.RETURN_CALL)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Function: callee}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = load local[0]
	v2:ref = const 2
	retain v2
	v3:state = state {addr=1 base=0 ip=5 returns=1 stack=[v1, v2 owned]}
	exit state v3
`, ssa.Format(out))
	})

	t.Run("guards an i64 slot load against a heap-promoted value", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:i64 = load local[0]
	v3:state = state {addr=1 base=0 ip=0 returns=1 stack=[]}
	v2:i64 = guard.kind v1 state v3
	return v2
`, ssa.Format(out))
	})
}

func loopFunction(t *testing.T) (*types.Function, int) {
	t.Helper()
	b := instr.NewBuilder()
	header, exit := b.Label(), b.Label()
	b.Emit(instr.LOCAL_GET, 0)
	b.Bind(header)
	b.Emit(instr.LOCAL_GET, 1).BrIf(exit)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 1).Br(header)
	b.Bind(exit).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeAny}},
		Locals: []types.Type{types.TypeAny, types.TypeI32},
		Code:   instr.Marshal(code),
	}, instr.New(instr.LOCAL_GET, 0).Width()
}

func assemble(t *testing.T, emit func(b *instr.Builder)) []byte {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	instructions, err := b.Assemble()
	require.NoError(t, err)
	return instr.Marshal(instructions)
}

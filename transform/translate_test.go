package transform_test

import (
	"fmt"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/pass"
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
	v8:state = state {addr=1 base=0 ip=33 returns=1 stack=[v7]}
	return v7 state v8
blk3: () <-- (blk1)
	v9:i32 = load local[0]
	v10:i32 = const 1
	v12:state = state {addr=1 base=0 ip=25 returns=1 stack=[v9, v10]}
	v11:i32 = i32.add v9, v10 state v12
	v13:state = state {addr=1 base=0 ip=26 returns=1 stack=[v11]}
	store local[0], v11 state v13
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
	v3:state = state {addr=0 base=0 ip=3 returns=0 stack=[v1]}
	v2:i32 = call v1 state v3
	v4:state = state {addr=0 base=0 ip=4 returns=0 stack=[v2]}
	complete v2 state v4
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
	v5:state = state {addr=1 base=0 ip=11 returns=1 stack=[v3]}
	return v3 state v5
`, ssa.Format(out))
	})

	t.Run("translates from a loop header", func(t *testing.T) {
		fn, header := loopFunction(t)

		root, err := transform.Translate(transform.Module{}, 1, fn, header)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(root))
		require.Equal(t, `func 1:2
blk0: (v1:ref)
	jump blk1(v1)
blk1: (v2:ref) <-- (blk0, blk3)
	v3:i32 = load local[1]
	br v3, blk2(v2), blk3(v2)
blk2: (v10:ref) <-- (blk1)
	v11:state = state {addr=1 base=0 ip=20 returns=1 stack=[v10 owned]}
	return v10 state v11
blk3: (v4:ref) <-- (blk1)
	v5:i32 = load local[1]
	v6:i32 = const 1
	v7:state = state {addr=1 base=0 ip=14 returns=1 stack=[v4 owned, v5, v6]}
	v8:i32 = i32.sub v5, v6 state v7
	v9:state = state {addr=1 base=0 ip=15 returns=1 stack=[v4 owned, v8]}
	store local[1], v8 state v9
	jump blk1(v4)
`, ssa.Format(root))
		// blk0 is a synthetic entry rotate() prepends because blk1, the
		// loop header this unit is rooted at, has a back-edge predecessor
		// (blk3): every later pass and Lower assume block 0 has none.
		require.Empty(t, root.Pred(0))

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
	v5:state = state {addr=1 base=0 ip=20 returns=1 stack=[v4 owned]}
	return v4 state v5
blk3: (v6:ref) <-- (blk1)
	v7:i32 = load local[1]
	v8:i32 = const 1
	v10:state = state {addr=1 base=0 ip=14 returns=1 stack=[v6, v7, v8]}
	v9:i32 = i32.sub v7, v8 state v10
	v11:state = state {addr=1 base=0 ip=15 returns=1 stack=[v6, v9]}
	store local[1], v9 state v11
	jump blk1(v6)
`, ssa.Format(entry))

		// The root's entry frame (address, header IP, return count) is what
		// a promoted i64 local's block-0 guard resumes into; it must survive
		// both rotate (the blk0 it prepends above) and a later pass's own
		// rebuild.
		require.Equal(t, ssa.Frame{Address: 1, IP: header, Returns: 1}, root.Entry())
		_, err = transform.NewFoldPass().Run(pass.NewManager(), root)
		require.NoError(t, err)
		require.Equal(t, ssa.Frame{Address: 1, IP: header, Returns: 1}, root.Entry())
	})

	t.Run("translates from a header reached with an empty operand stack", func(t *testing.T) {
		// A join with no live facts at all ("no block param needed") records
		// the same nil in analyze's states as a span analyze never reached;
		// translate must tell them apart through reachability, not content.
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code: assemble(t, func(b *instr.Builder) {
				join := b.Label()
				b.Emit(instr.LOCAL_GET, 0).BrIf(join)
				b.Bind(join).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
			})}
		header := instr.New(instr.LOCAL_GET, 0).Width() + instr.New(instr.BR_IF, 0).Width()

		out, err := transform.Translate(transform.Module{}, 1, fn, header)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Empty(t, out.Block(0).Params)
		require.Contains(t, ssa.Format(out), "i32.div_s")
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

	t.Run("translates a protected region without its catch block", func(t *testing.T) {
		fn, _ := protectedFunction(t)

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = load local[0]
	v2:i32 = load local[1]
	v4:state = state {addr=1 base=0 ip=4 returns=1 stack=[v1, v2]}
	v3:i32 = i32.div_s v1, v2 state v4
	jump blk1(v3)
blk1: (v5:i32) <-- (blk0)
	v6:state = state {addr=1 base=0 ip=5 returns=1 stack=[v5]}
	return v5 state v6
`, ssa.Format(out))
	})

	t.Run("declines an entry inside a catch block", func(t *testing.T) {
		fn, catch := protectedFunction(t)

		out, err := transform.Translate(transform.Module{}, 1, fn, catch)
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

	t.Run("guards an array load in a function that also calls", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{}}
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).
					Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Function: callee}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "guard.shape")
	})

	t.Run("leaves an array load at a refuted guard site unguarded", func(t *testing.T) {
		load := instr.New(instr.LOCAL_GET, 0)
		index := instr.New(instr.I32_CONST, 0)
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Append(load, index).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{Refuted: map[int]bool{load.Width() + index.Width(): true}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.NotContains(t, ssa.Format(out), "guard.shape")
		require.Contains(t, ssa.Format(out), "array.get")
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
	v4:ref = guard.shape v3 kind i32 state v5
	v6:i32 = array.len v4 state v5
	v7:i32 = load local[1]
	v9:state = state {addr=1 base=0 ip=12 returns=1 stack=[v6, v7]}
	v8:i1 = i32.le_s v6, v7 state v9
	br v8, blk2(), blk3()
blk2: () <-- (blk1)
	v10:i32 = load local[1]
	v11:state = state {addr=1 base=0 ip=31 returns=1 stack=[v10]}
	return v10 state v11
blk3: () <-- (blk1)
	v12:ref = load local[0]
	v13:i32 = load local[1]
	v15:state = state {addr=1 base=0 ip=20 returns=1 stack=[v12, v13]}
	v14:ref = guard.shape v12 kind i32 state v15
	v16:i32 = array.get v14, v13 state v15
	v17:i32 = load local[1]
	v19:state = state {addr=1 base=0 ip=23 returns=1 stack=[v16, v17]}
	v18:i32 = i32.add v16, v17 state v19
	v20:state = state {addr=1 base=0 ip=24 returns=1 stack=[v18]}
	store local[1], v18 state v20
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
	v4:ref = guard.shape v1 kind i32 state v5
	array.set v4, v2, v3 state v5
	v6:state = state {addr=1 base=0 ip=13 returns=0 stack=[]}
	return state v6
`, ssa.Format(out))
	})

	t.Run("guards an array store of an i32 value through a declared i1 element kind", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI1)}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	v2:i32 = const 0
	v3:i32 = const 1
	v5:state = state {addr=1 base=0 ip=12 returns=0 stack=[v1, v2, v3]}
	v4:ref = guard.shape v1 kind i1 state v5
	array.set v4, v2, v3 state v5
	v6:state = state {addr=1 base=0 ip=13 returns=0 stack=[]}
	return state v6
`, ssa.Format(out))
	})

	t.Run("guards an array load through a constant cell's resolved element type", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Array: types.NewArrayType(types.TypeI32)}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "guard.shape")
	})

	t.Run("attaches array.new_default's declared element type to its result", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0).
					Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{Types: []types.Type{types.NewArrayType(types.TypeI32)}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "guard.shape")
	})

	t.Run("guards a []any array store through the container's element kind, not the stored value's own", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0).
					Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_SET).Emit(instr.RETURN)
			})}
		m := transform.Module{Types: []types.Type{types.NewArrayType(types.TypeAny)}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "kind ref")
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
	v6:ref = guard.shape v2 struct type 0x0 state v7
	struct.set v6, v4, v5 state v7
	release v6 state v7
	v8:state = state {addr=1 base=0 ip=19 returns=0 stack=[]}
	return state v8
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
			Objects:   transform.Objects{2: {Struct: record}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "v3:ref = guard.shape v1 struct type ")
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

	t.Run("loops a self tail call", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeAny},
			Code: assemble(t, func(b *instr.Builder) {
				base := b.Label()
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_EQ).BrIf(base)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_ADD)
				b.Emit(instr.CONST_GET, 0).Emit(instr.RETURN_CALL)
				b.Bind(base).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(1)},
			Objects:   transform.Objects{1: {Function: fn}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	jump blk1()
blk1: () <-- (blk0, blk3)
	v1:i32 = load local[0]
	v2:i32 = const 0
	v3:state = state {addr=1 base=0 ip=7 returns=1 stack=[v1, v2]}
	v4:i1 = i32.eq v1, v2 state v3
	br v4, blk2(), blk3()
blk2: () <-- (blk1)
	v16:i32 = load local[1]
	v17:state = state {addr=1 base=0 ip=30 returns=1 stack=[v16]}
	return v16 state v17
blk3: () <-- (blk1)
	v5:i32 = load local[0]
	v6:i32 = const 1
	v7:state = state {addr=1 base=0 ip=18 returns=1 stack=[v5, v6]}
	v8:i32 = i32.sub v5, v6 state v7
	v9:i32 = load local[1]
	v10:i32 = load local[0]
	v11:state = state {addr=1 base=0 ip=23 returns=1 stack=[v8, v9, v10]}
	v12:i32 = i32.add v9, v10 state v11
	v13:ref = const 1
	v14:state = state {addr=1 base=0 ip=27 returns=1 stack=[v8, v12, v13]}
	store local[1], v12 state v14
	store local[0], v8 state v14
	v15:ref = const 0
	store local[2], v15 state v14
	jump blk1()
`, ssa.Format(out))
	})

	t.Run("exits a tail call whose frame it cannot reuse", func(t *testing.T) {
		wide := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}}}
		wide.Code = assemble(t, func(b *instr.Builder) {
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.RETURN_CALL)
		})
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(1)},
			Objects:   transform.Objects{1: {Function: wide}}}

		out, err := transform.Translate(m, 1, wide, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Contains(t, ssa.Format(out), "exit state")
	})

	t.Run("speculates a dynamic callee its feedback observed", func(t *testing.T) {
		fn, ips := indirectRecursiveFib(t)
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(1)},
			Objects:   transform.Objects{1: {Function: fn}},
			Callees:   map[int]transform.Callee{ips[0]: {Function: 1}, ips[1]: {Function: 1}},
		}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, fmt.Sprintf(`func 1:0
blk0: ()
	v1:i32 = load local[0]
	v2:i32 = const 2
	v4:state = state {addr=1 base=0 ip=7 returns=1 stack=[v1, v2]}
	v3:i1 = i32.lt_s v1, v2 state v4
	br v3, blk1(), blk2()
blk1: () <-- (blk0)
	v5:i32 = load local[0]
	v6:state = state {addr=1 base=0 ip=41 returns=1 stack=[v5]}
	return v5 state v6
blk2: () <-- (blk0)
	v7:i32 = load local[0]
	v8:i32 = const 1
	v10:state = state {addr=1 base=0 ip=18 returns=1 stack=[v7, v8]}
	v9:i32 = i32.sub v7, v8 state v10
	v11:ref = load local[1]
	v12:ref = load local[1]
	v13:ref = const 1
	v15:state = state {addr=1 base=0 ip=%[1]d returns=1 stack=[v9, v11, v12]}
	v14:ref = guard.value v12, v13 state v15
	v16:i32 = call v9, v11, v13 state v15
	v17:i32 = load local[0]
	v18:i32 = const 2
	v20:state = state {addr=1 base=0 ip=31 returns=1 stack=[v16, v17, v18]}
	v19:i32 = i32.sub v17, v18 state v20
	v21:ref = load local[1]
	v22:ref = load local[1]
	v23:ref = const 1
	v25:state = state {addr=1 base=0 ip=%[2]d returns=1 stack=[v16, v19, v21, v22]}
	v24:ref = guard.value v22, v23 state v25
	v26:i32 = call v19, v21, v23 state v25
	v28:state = state {addr=1 base=0 ip=37 returns=1 stack=[v16, v26]}
	v27:i32 = i32.add v16, v26 state v28
	v29:state = state {addr=1 base=0 ip=38 returns=1 stack=[v27]}
	return v27 state v29
`, ips[0], ips[1]), ssa.Format(out))
	})

	t.Run("resolves a closure built in the same unit as a static callee and lends it from its local", func(t *testing.T) {
		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Captures: []types.Type{types.TypeI32}}
		fn := &types.Function{
			Locals: []types.Type{types.TypeAny},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 0)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.CALL)
			}),
		}
		m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: target}}}

		out, err := transform.Translate(m, 0, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, fmt.Sprintf(`func 0:0
blk0: ()
	v1:i32 = const 5
	v2:ref = const 2
	v4:state = state {addr=0 base=0 ip=8 returns=0 stack=[v1, v2]}
	v3:ref = closure.new v1, v2 state v4
	v5:state = state {addr=0 base=0 ip=9 returns=0 stack=[v3 owned]}
	store local[0], v3 state v5
	v6:ref = load local[0]
	v8:state = state {addr=0 base=0 ip=13 returns=0 stack=[v6]}
	v7:i32 = call v6 closure 2 type 0x%x captures 1 state v8
	v9:state = state {addr=0 base=0 ip=14 returns=0 stack=[v7]}
	complete v7 state v9
`, uintptr(unsafe.Pointer(target.Typ))), ssa.Format(out))
	})

	t.Run("does not call a closure through a local another path re-stores", func(t *testing.T) {
		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI1, types.TypeAny}},
			Locals: []types.Type{types.TypeAny},
			Code: assemble(t, func(b *instr.Builder) {
				skip := b.Label()
				b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 2)
				b.Emit(instr.LOCAL_GET, 0).BrIf(skip)
				b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_SET, 2)
				b.Bind(skip).Emit(instr.LOCAL_GET, 2).Emit(instr.CALL).Emit(instr.DROP).Emit(instr.RETURN)
			}),
		}
		m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: target}}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NotContains(t, ssa.Format(out), "call")
	})

	t.Run("speculates a closure callee its feedback observed and lends it from its local", func(t *testing.T) {
		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Captures: []types.Type{types.TypeI32}}
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
			}),
		}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Function: target}},
			Callees:   map[int]transform.Callee{2: {Function: 2, Closure: true}},
		}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		typ := uintptr(unsafe.Pointer(target.Typ))
		require.Equal(t, fmt.Sprintf(`func 1:0
blk0: ()
	v1:ref = load local[0]
	v3:state = state {addr=1 base=0 ip=2 returns=1 stack=[v1]}
	v2:ref = guard.shape v1 closure 2 type 0x%x captures 1 state v3
	v4:i32 = call v2 closure 2 type 0x%x captures 1 state v3
	v5:state = state {addr=1 base=0 ip=3 returns=1 stack=[v4]}
	return v4 state v5
`, typ, typ), ssa.Format(out))
	})

	t.Run("lends a global-backed argument by owning it and releasing it after the call", func(t *testing.T) {
		callee := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}}}
		fn := &types.Function{
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			}),
		}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Function: callee}},
			Globals:   []types.Kind{types.KindRef},
		}

		out, err := transform.Translate(m, 0, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 0:0
blk0: ()
	v1:ref = load global[0]
	v2:ref = const 2
	retain v1
	v4:state = state {addr=0 base=0 ip=6 returns=0 stack=[v1 owned, v2]}
	v3:i32 = call v1, v2 state v4
	release v1 state v4
	v5:state = state {addr=0 base=0 ip=7 returns=0 stack=[v3]}
	complete v3 state v5
`, ssa.Format(out))
	})

	t.Run("keeps a string op's operands with the translated code, since the op stores none of them", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
			}),
		}
		m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	v2:ref = const 2
	v4:state = state {addr=1 base=0 ip=5 returns=1 stack=[v1, v2]}
	v3:ref = string.concat v1, v2 state v4
	v5:state = state {addr=1 base=0 ip=6 returns=1 stack=[v3 owned]}
	return v3 state v5
`, ssa.Format(out))
	})

	t.Run("ends a never-executed dynamic call's block in an exit and leaves out what only follows it", func(t *testing.T) {
		fn, ip := coldCallFunction(t)
		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, fmt.Sprintf(`func 1:0
blk0: ()
	v1:i32 = load local[0]
	br v1, blk1(), blk2()
blk1: () <-- (blk0)
	v2:ref = load local[1]
	retain v2
	v3:state = state {addr=1 base=0 ip=%d returns=1 stack=[v2 owned]}
	exit state v3
blk2: () <-- (blk0)
	v4:i32 = load local[0]
	v5:state = state {addr=1 base=0 ip=7 returns=1 stack=[v4]}
	return v4 state v5
`, ip), ssa.Format(out))
	})

	t.Run("calls a dynamic callee of one shared signature through an owned, signature-carrying call", func(t *testing.T) {
		fn, ip := coldCallFunction(t)
		sig := fn.Typ.Params[1].(*types.FunctionType)
		m := transform.Module{Callees: map[int]transform.Callee{ip: {Type: sig}}}
		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, fmt.Sprintf(`func 1:0
blk0: ()
	v1:i32 = load local[0]
	br v1, blk1(), blk2()
blk1: () <-- (blk0)
	v2:ref = load local[1]
	retain v2
	v4:state = state {addr=1 base=0 ip=%d returns=1 stack=[v2 owned]}
	v3:i32 = call v2 callee type 0x%x state v4
	br v3, blk3(), blk4()
blk2: () <-- (blk0)
	v5:i32 = load local[0]
	v6:state = state {addr=1 base=0 ip=7 returns=1 stack=[v5]}
	return v5 state v6
blk3: () <-- (blk1)
	v7:i32 = const 9
	v8:state = state {addr=1 base=0 ip=25 returns=1 stack=[v7]}
	return v7 state v8
blk4: () <-- (blk1)
	v9:i32 = const 7
	v10:state = state {addr=1 base=0 ip=19 returns=1 stack=[v9]}
	return v9 state v10
`, ip, uintptr(unsafe.Pointer(sig))), ssa.Format(out))
	})

	t.Run("declines a dynamic callee whose feedback shares no signature", func(t *testing.T) {
		fn, ip := coldCallFunction(t)
		m := transform.Module{Callees: map[int]transform.Callee{ip: {}}}
		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines feedback naming no function", func(t *testing.T) {
		fn, ips := indirectRecursiveFib(t)
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(1)},
			Objects:   transform.Objects{1: {Function: fn}, 7: {Struct: &types.StructType{}}},
			Callees:   map[int]transform.Callee{ips[0]: {Function: 7}, ips[1]: {Function: 7}},
		}

		out, err := transform.Translate(m, 1, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines to speculate an owned callee", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.REF_CAST, 0).Emit(instr.CALL).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(code),
		}
		ip := calls(fn.Code)[0]
		target := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(1)},
			Objects:   transform.Objects{1: {Function: target}},
			Callees:   map[int]transform.Callee{ip: {Function: 1}},
		}

		out, err := transform.Translate(m, 0, fn, 0)
		require.NoError(t, err)
		require.Nil(t, out)
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
	v4:state = state {addr=1 base=0 ip=2 returns=1 stack=[v2]}
	return v2 state v4
`, ssa.Format(out))
	})

	t.Run("reads a map entry as its declared element kind", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewMapType(types.TypeI32, types.TypeI32)}, Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 3).Emit(instr.MAP_GET)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 4).Emit(instr.MAP_LOOKUP).Emit(instr.DROP)
				b.Emit(instr.I32_ADD).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	v2:i32 = const 3
	v4:state = state {addr=1 base=0 ip=7 returns=1 stack=[v1, v2]}
	v3:i32 = map.get v1, v2 state v4
	v5:ref = load local[0]
	v6:i32 = const 4
	v9:state = state {addr=1 base=0 ip=15 returns=1 stack=[v3, v5, v6]}
	v7:i32, v8:i1 = map.lookup v5, v6 state v9
	v11:state = state {addr=1 base=0 ip=17 returns=1 stack=[v3, v7]}
	v10:i32 = i32.add v3, v7 state v11
	v12:state = state {addr=1 base=0 ip=18 returns=1 stack=[v10]}
	return v10 state v12
`, ssa.Format(out))
	})

	t.Run("retains the ref select picks before releasing both operands", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeAny}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.REF_NULL).Emit(instr.REF_NULL).Emit(instr.I32_CONST, 1).Emit(instr.SELECT).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn, 0)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = const 0
	v2:ref = const 0
	v3:i32 = const 1
	v5:state = state {addr=1 base=0 ip=7 returns=1 stack=[v1 owned, v2 owned, v3]}
	v4:ref = select v1, v2, v3 state v5
	retain v4
	release v1 state v5
	release v2 state v5
	v6:state = state {addr=1 base=0 ip=8 returns=1 stack=[v4 owned]}
	return v4 state v6
`, ssa.Format(out))
	})
}

func TestAdopts(t *testing.T) {
	tests := []struct {
		name    string
		code    instr.Opcode
		pops    int
		results int
		want    int
	}{
		{name: "a call adopts every operand", code: instr.CALL, pops: 3, results: 1, want: 3},
		{name: "a heap write with no result adopts the stored value", code: instr.MAP_SET, pops: 3, results: 0, want: 1},
		{name: "a heap write with a result adopts nothing", code: instr.ARRAY_APPEND, pops: 3, results: 1, want: 0},
		{name: "a heap read adopts nothing", code: instr.MAP_GET, pops: 2, results: 1, want: 0},
		{name: "an allocation adopts nothing", code: instr.STRING_CONCAT, pops: 2, results: 1, want: 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, transform.Adopts(tt.code, tt.pops, tt.results))
		})
	}
}

func TestBorrows(t *testing.T) {
	tests := []struct {
		name string
		fn   *types.Function
		want []bool
	}{
		{
			name: "an IRF-shaped func(i32, any) never writes either param",
			fn: types.NewFunction(
				&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}},
				nil,
				[]instr.Instruction{instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN)},
			),
			want: []bool{false, true},
		},
		{
			name: "a ref parameter written by local.set",
			fn: types.NewFunction(
				&types.FunctionType{Params: []types.Type{types.TypeAny}},
				nil,
				[]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 0), instr.New(instr.RETURN)},
			),
			want: []bool{false},
		},
		{
			name: "a ref parameter written by local.tee",
			fn: types.NewFunction(
				&types.FunctionType{Params: []types.Type{types.TypeAny}},
				nil,
				[]instr.Instruction{instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_TEE, 0), instr.New(instr.RETURN)},
			),
			want: []bool{false},
		},
		{
			name: "a ref parameter in a function that tail calls",
			fn: types.NewFunction(
				&types.FunctionType{Params: []types.Type{types.TypeAny}},
				nil,
				[]instr.Instruction{instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.RETURN_CALL)},
			),
			want: []bool{false},
		},
		{
			name: "an i64 parameter",
			fn: types.NewFunction(
				&types.FunctionType{Params: []types.Type{types.TypeI64}},
				nil,
				[]instr.Instruction{instr.New(instr.RETURN)},
			),
			want: []bool{false},
		},
		{
			name: "nil",
			fn:   nil,
			want: nil,
		},
		{
			name: "Typ == nil",
			fn:   &types.Function{},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, transform.Borrows(tt.fn))
		})
	}
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

// indirectRecursiveFib builds func(i32, any) i32 calling itself through
// param 1 (the callee arrives on the stack, not by CONST_GET) twice, and
// reports both dynamic CALLs' own ip in source order.
func indirectRecursiveFib(t *testing.T) (*types.Function, []int) {
	t.Helper()
	b := instr.NewBuilder()
	base := b.Label()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_LT_S).BrIf(base)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_SUB)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
	b.Emit(instr.I32_ADD).Emit(instr.RETURN)
	b.Bind(base).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	fn := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
	return fn, calls(fn.Code)
}

// coldCallFunction builds func(i32, func() i32) i32 whose cold branch calls
// param 1 and branches on its result, and reports that CALL's own ip. The
// two blocks after the call are reached only through it.
func coldCallFunction(t *testing.T) (*types.Function, int) {
	t.Helper()
	b := instr.NewBuilder()
	cold, done := b.Label(), b.Label()
	b.Emit(instr.LOCAL_GET, 0).BrIf(cold)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	b.Bind(cold).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).BrIf(done)
	b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
	b.Bind(done).Emit(instr.I32_CONST, 9).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	fn := &types.Function{
		Typ: &types.FunctionType{
			Params:  []types.Type{types.TypeI32, &types.FunctionType{Returns: []types.Type{types.TypeI32}}},
			Returns: []types.Type{types.TypeI32},
		},
		Code: instr.Marshal(code),
	}
	return fn, calls(fn.Code)[0]
}

// calls returns the offset of every CALL in code, in order.
func calls(code []byte) []int {
	var out []int
	for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
		if instr.Instruction(code[ip:]).Opcode() == instr.CALL {
			out = append(out, ip)
		}
	}
	return out
}

func assemble(t *testing.T, emit func(b *instr.Builder)) []byte {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	instructions, err := b.Assemble()
	require.NoError(t, err)
	return instr.Marshal(instructions)
}

// protectedFunction divides its parameters inside a protected region whose
// catch block returns 0, and reports the catch block's offset.
func protectedFunction(t *testing.T) (*types.Function, int) {
	t.Helper()
	b := instr.NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	b.Bind(start).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_DIV_S)
	b.Bind(end).Emit(instr.RETURN)
	b.Bind(catch).Emit(instr.DROP).Emit(instr.I32_CONST, 0).Emit(instr.RETURN)
	b.Try(start, end, catch, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	at := instr.New(instr.LOCAL_GET, 0).Width()*2 + instr.New(instr.I32_DIV_S).Width() + instr.New(instr.RETURN).Width()
	return &types.Function{
		Typ:      &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code:     instr.Marshal(code),
		Handlers: b.Handlers(),
	}, at
}

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
				head, done := b.Label(), b.Label()
				b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
				b.Bind(head)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 10).Emit(instr.I32_GE_S).BrIf(done)
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
				b.Br(head)
				b.Bind(done).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn)
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
	v5:i1 = i32.ge_s v3, v4
	br v5, blk2(), blk3()
blk2: () <-- (blk1)
	v6:i32 = load local[0]
	return v6
blk3: () <-- (blk1)
	v7:i32 = load local[0]
	v8:i32 = const 1
	v9:i32 = i32.add v7, v8
	v10:state = state {addr=1 base=0 ip=26 returns=1 stack=[v9]}
	store local[0], v9 state v10
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

		out, err := transform.Translate(transform.Module{}, 1, fn)
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
			Objects:   transform.Objects{2: {Fn: callee}}}

		out, err := transform.Translate(m, 0, fn)
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

	t.Run("declines what bytecode alone cannot resolve/no code", func(t *testing.T) {
		fn := &types.Function{Typ: &types.FunctionType{}}
		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/a protected region", func(t *testing.T) {
		fn := &types.Function{
			Typ:      &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Handlers: []instr.Handler{{Start: 0, End: 5, Catch: 5}},
			Code:     assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/an unresolved callee", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.REF_NULL).Emit(instr.CALL).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines what bytecode alone cannot resolve/a constant read no object resolves", func(t *testing.T) {
		// Object no longer carries the concrete container identity a constant
		// typed array's cell would resolve to: a read against a reference
		// straight off the constant pool, with no declared array type behind
		// it, is bytecode the translation cannot resolve a shape for and
		// declines instead of guessing one.
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{}}
		out, err := transform.Translate(m, 1, fn)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	t.Run("declines a function holding a suspension", func(t *testing.T) {
		fn := &types.Function{
			Typ:  &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Code: assemble(t, func(b *instr.Builder) { b.Emit(instr.I32_CONST, 1).Emit(instr.YIELD).Emit(instr.RETURN) })}
		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.Nil(t, out)
	})

	// LOCAL_TEE and GLOBAL_TEE of a reference used to be refused whole: the
	// slot's overwritten count and the surviving stack copy's count both need
	// resolving, and nothing at IR build time could tell a self-store from an
	// ordinary one. dup composed with store resolves both through the same
	// own/detach machinery an ordinary ref store already uses.
	t.Run("translates a tee of a reference", func(t *testing.T) {
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeAny}},
			Locals: []types.Type{types.TypeAny},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.REF_NULL).Emit(instr.LOCAL_TEE, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.NotNil(t, out)
		require.NoError(t, ssa.Verify(out))
	})

	t.Run("guards an array load through a declared element kind", func(t *testing.T) {
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
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		// itab 0x3 is this translation's own guard identity for an i32 array
		// element (see walk.go's shapeArrayI32): a token distinguishing one
		// container shape from another within this SSA, not a runtime type.
		require.Equal(t, `func 1:0
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
	v10:ref = guard.shape v8 itab 0x3 state v11
	v12:i32 = array.get v10, v9
	v13:i32 = load local[1]
	v14:i32 = i32.add v12, v13
	v15:state = state {addr=1 base=0 ip=24 returns=1 stack=[v14]}
	store local[1], v14 state v15
	jump blk1()
`, ssa.Format(out))
	})

	t.Run("guards an array store through the shape the stored value's own kind names", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 5).Emit(instr.ARRAY_SET).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		require.Equal(t, `func 1:0
blk0: ()
	v1:ref = load local[0]
	v2:i32 = const 0
	v3:i32 = const 5
	v5:state = state {addr=1 base=0 ip=12 returns=0 stack=[v1, v2, v3]}
	v4:ref = guard.shape v1 itab 0x3 state v5
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

		out, err := transform.Translate(transform.Module{Decl: []types.Type{record}}, 1, fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))

		// itab 0x8 is this translation's own guard identity for any struct
		// (see walk.go's shapeStruct); the field's own declared record narrows
		// it further only where STRUCT_GET resolves one.
		require.Equal(t, `func 1:0
blk0: ()
	v1:i32 = const 0
	v3:state = state {addr=1 base=0 ip=5 returns=0 stack=[v1]}
	v2:ref = bridge struct.new v1 state v3
	jump blk1(v2)
blk1: (v4:ref) <-- (blk0)
	v5:i32 = const 0
	v6:i32 = const 5
	v8:state = state {addr=1 base=0 ip=18 returns=0 stack=[v4 owned, v5, v6]}
	v7:ref = guard.shape v4 itab 0x8 state v8
	struct.set v7, v5, v6 state v8
	release v7 state v8
	return
`, ssa.Format(out))
	})

	// A struct field's kind and guard both resolve through the record a
	// constant reference's resolved object carries, the same way a call
	// resolves its target: the reference is the whole answer, so no cast or
	// declared type has to stand in for one a constant cell already names.
	t.Run("reads a struct field through a constant cell's resolved record", func(t *testing.T) {
		record := types.NewStructType(types.NewStructField(types.TypeF64))
		fn := &types.Function{
			Typ: &types.FunctionType{Returns: []types.Type{types.TypeF64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.CONST_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.STRUCT_GET).Emit(instr.RETURN)
			})}
		m := transform.Module{
			Constants: []types.Boxed{types.BoxRef(2)},
			Objects:   transform.Objects{2: {Typ: record}}}

		out, err := transform.Translate(m, 1, fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(out))
		// The guard's Typ names the record itself, a real Go pointer no golden
		// string can fix; every other fact - the field's f64 kind, the generic
		// struct itab - is checked structurally instead.
		require.Contains(t, ssa.Format(out), "v3:ref = guard.shape v1 itab 0x8 type ")
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
			Objects:   transform.Objects{2: {Fn: callee}}}

		out, err := transform.Translate(m, 1, fn)
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

	// An i64 slot may hold either an inline value or a reference to one
	// Interpreter.boxI64 heap-promoted, and only the tag on the loaded word
	// tells them apart. load states that as an OpGuardKind immediately after
	// the OpLoad, carrying the interpreter state OpLoad itself cannot resume
	// into.
	t.Run("guards an i64 slot load against a heap-promoted value", func(t *testing.T) {
		fn := &types.Function{
			Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Code: assemble(t, func(b *instr.Builder) {
				b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
			})}

		out, err := transform.Translate(transform.Module{}, 1, fn)
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

// assemble builds the code of one function.
func assemble(t *testing.T, emit func(b *instr.Builder)) []byte {
	t.Helper()
	b := instr.NewBuilder()
	emit(b)
	instructions, err := b.Assemble()
	require.NoError(t, err)
	return instr.Marshal(instructions)
}

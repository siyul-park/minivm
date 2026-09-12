package transform_test

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/ssa/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// deoptState gives code the ssa.NoValue verify.go admits for most opcodes, or
// a fresh, otherwise-empty OpState for one ssa.OverflowsI64 names, which can
// overflow the boxed 49-bit payload and so always resumes into one (see
// verify.go's operation and frontend/walk.go's exec).
func deoptState(b *ssa.Builder, block int, code instr.Opcode) ssa.Value {
	if !ssa.OverflowsI64(code) {
		return ssa.NoValue
	}
	state := b.Value(ssa.TypeState)
	b.Add(block, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1}}, Results: []ssa.Value{state}})
	return state
}

func TestNewFoldPass(t *testing.T) {
	t.Run("returns a pass over ssa.Function", func(t *testing.T) {
		var p pass.Pass[*ssa.Function] = transform.NewFoldPass()
		require.NotNil(t, p)
	})
}

func TestFoldPass_Run(t *testing.T) {
	t.Run("folds a pure operation whose arguments are both constants", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)
		require.Contains(t, before, "v3:i32 = i32.add v1, v2")

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		after := ssa.Format(fn)
		require.NotContains(t, after, "i32.add")
		require.Contains(t, after, "v3:i32 = const 5")
		require.Equal(t, sum, ssa.Value(3))
	})

	t.Run("reports PreserveAll and leaves the function untouched when nothing folds", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		param := b.Param(entry, ssa.TypeI32)
		one, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(1), Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{param, one}, Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		before := ssa.Format(fn)

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a constant divisor of zero unfolded", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, zero, quotient := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(10), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{zero}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{x, zero}, Results: []ssa.Value{quotient}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{quotient}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Contains(t, ssa.Format(fn), "i32.div_s")
	})

	t.Run("leaves an i64 result that overflows the boxed constant range unfolded", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, product := b.Value(ssa.TypeI64), b.Value(ssa.TypeI64), b.Value(ssa.TypeI64)
		// Each factor fits the 49-bit boxed payload; their product does not.
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(1 << 30), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI64(1 << 30), Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I64_MUL, Args: []ssa.Value{x, y}, State: deoptState(b, entry, instr.I64_MUL), Results: []ssa.Value{product}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{product}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Contains(t, ssa.Format(fn), "i64.mul")
	})

	t.Run("folds a value a deopt frame references without disturbing what the frame names", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, sum, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(2), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(3), Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, y}, Results: []ssa.Value{sum}})
		// sum has no other use; it is only live because a deopt frame names it.
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Addr: 1, Stack: []ssa.Operand{{Value: sum}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveNone(), preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, ssa.Format(fn), "stack=[v3]")
		require.Contains(t, ssa.Format(fn), "v3:i32 = const 5")
	})

	identities := []struct {
		name string
		code instr.Opcode
		typ  ssa.Type
		with types.Boxed
	}{
		{"i32 add zero", instr.I32_ADD, ssa.TypeI32, types.BoxI32(0)},
		{"i32 sub zero", instr.I32_SUB, ssa.TypeI32, types.BoxI32(0)},
		{"i32 or zero", instr.I32_OR, ssa.TypeI32, types.BoxI32(0)},
		{"i32 xor zero", instr.I32_XOR, ssa.TypeI32, types.BoxI32(0)},
		{"i32 shl zero", instr.I32_SHL, ssa.TypeI32, types.BoxI32(0)},
		{"i32 shr_s zero", instr.I32_SHR_S, ssa.TypeI32, types.BoxI32(0)},
		{"i32 shr_u zero", instr.I32_SHR_U, ssa.TypeI32, types.BoxI32(0)},
		{"i32 and minus one", instr.I32_AND, ssa.TypeI32, types.BoxI32(-1)},
		{"i32 mul one", instr.I32_MUL, ssa.TypeI32, types.BoxI32(1)},
		{"i32 div_s one", instr.I32_DIV_S, ssa.TypeI32, types.BoxI32(1)},
		{"i32 div_u one", instr.I32_DIV_U, ssa.TypeI32, types.BoxI32(1)},
		{"i64 add zero", instr.I64_ADD, ssa.TypeI64, types.BoxI64(0)},
		{"i64 and minus one", instr.I64_AND, ssa.TypeI64, types.BoxI64(-1)},
		{"i64 mul one", instr.I64_MUL, ssa.TypeI64, types.BoxI64(1)},
		{"i64 div_u one", instr.I64_DIV_U, ssa.TypeI64, types.BoxI64(1)},
	}
	// The left argument has to be a value an opcode computed, so the identity
	// hands back a representation its static type names.
	seed := func(t ssa.Type) instr.Opcode {
		if t == ssa.TypeI64 {
			return instr.I64_REM_S
		}
		return instr.I32_REM_S
	}
	for _, c := range identities {
		t.Run("reduces "+c.name+" to its left argument", func(t *testing.T) {
			b := ssa.New("f")
			entry := b.Block()
			param := b.Param(entry, c.typ)
			x, right, result := b.Value(c.typ), b.Value(c.typ), b.Value(c.typ)
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: seed(c.typ), Args: []ssa.Value{param, param}, State: deoptState(b, entry, seed(c.typ)), Results: []ssa.Value{x}})
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.with, Results: []ssa.Value{right}})
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: c.code, Args: []ssa.Value{x, right}, State: deoptState(b, entry, c.code), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err)
			require.Equal(t, pass.PreserveNone(), preserved)
			require.NoError(t, ssa.Verify(fn))
			out := ssa.Format(fn)
			require.NotContains(t, out, instr.TypeOf(c.code).Mnemonic)
			left := regexp.MustCompile(`(v\d+):\S+ = ` + instr.TypeOf(seed(c.typ)).Mnemonic).FindStringSubmatch(out)
			require.Len(t, left, 2)
			require.Contains(t, out, "return "+left[1])
		})
	}

	t.Run("leaves an identity over a value read out of a slot", func(t *testing.T) {
		// A slot is zero-filled rather than written with its declared type's
		// zero, so an unwritten i32 local reads back as a raw Boxed(0) whose
		// kind is f64. The addition this rewrite would remove is what re-tags
		// it as the i32 the slot was declared to hold.
		b := ssa.New("f")
		entry := b.Block()
		x, right, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(0), Results: []ssa.Value{right}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_ADD, Args: []ssa.Value{x, right}, Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Contains(t, ssa.Format(fn), "i32.add")
	})

	reductions := []struct {
		name string
		code instr.Opcode
		typ  ssa.Type
		with types.Boxed
		want instr.Opcode
		by   int
	}{
		{"i32 multiply", instr.I32_MUL, ssa.TypeI32, types.BoxI32(8), instr.I32_SHL, 3},
		{"i32 unsigned divide", instr.I32_DIV_U, ssa.TypeI32, types.BoxI32(16), instr.I32_SHR_U, 4},
		{"i64 multiply", instr.I64_MUL, ssa.TypeI64, types.BoxI64(4), instr.I64_SHL, 2},
		{"i64 unsigned divide", instr.I64_DIV_U, ssa.TypeI64, types.BoxI64(2), instr.I64_SHR_U, 1},
	}
	for _, c := range reductions {
		t.Run("reduces an "+c.name+" by a power of two to a shift", func(t *testing.T) {
			b := ssa.New("f")
			entry := b.Block()
			x := b.Param(entry, c.typ)
			right, result := b.Value(c.typ), b.Value(c.typ)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.with, Results: []ssa.Value{right}})
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: c.code, Args: []ssa.Value{x, right}, State: deoptState(b, entry, c.code), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err)
			require.Equal(t, pass.PreserveNone(), preserved)
			require.NoError(t, ssa.Verify(fn))
			out := ssa.Format(fn)
			require.NotContains(t, out, instr.TypeOf(c.code).Mnemonic)
			require.Contains(t, out, instr.TypeOf(c.want).Mnemonic)
			require.Contains(t, out, fmt.Sprintf("const %d", c.by))
		})
	}

	t.Run("leaves a signed divide by a power of two alone", func(t *testing.T) {
		// An arithmetic shift right rounds toward negative infinity where the
		// divide rounds toward zero.
		b := ssa.New("f")
		entry := b.Block()
		x := b.Param(entry, ssa.TypeI32)
		right, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(8), Results: []ssa.Value{right}})
		b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: instr.I32_DIV_S, Args: []ssa.Value{x, right}, Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.Equal(t, pass.PreserveAll(), preserved)
		require.Contains(t, ssa.Format(fn), "i32.div_s")
	})

	cases := []struct {
		name string
		code instr.Opcode
		args []types.Boxed
		want types.Boxed
	}{
		{"i32 add", instr.I32_ADD, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(17)},
		{"i32 sub", instr.I32_SUB, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(7)},
		{"i32 mul", instr.I32_MUL, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(60)},
		{"i32 div_s", instr.I32_DIV_S, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
		{"i32 div_u", instr.I32_DIV_U, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
		{"i32 rem_s", instr.I32_REM_S, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
		{"i32 rem_u", instr.I32_REM_U, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
		{"i32 shl", instr.I32_SHL, []types.Boxed{types.BoxI32(1), types.BoxI32(3)}, types.BoxI32(8)},
		{"i32 shr_s", instr.I32_SHR_S, []types.Boxed{types.BoxI32(8), types.BoxI32(2)}, types.BoxI32(2)},
		{"i32 shr_u", instr.I32_SHR_U, []types.Boxed{types.BoxI32(8), types.BoxI32(2)}, types.BoxI32(2)},
		{"i32 xor", instr.I32_XOR, []types.Boxed{types.BoxI32(0b110), types.BoxI32(0b011)}, types.BoxI32(0b101)},
		{"i32 and", instr.I32_AND, []types.Boxed{types.BoxI32(0b110), types.BoxI32(0b011)}, types.BoxI32(0b010)},
		{"i32 or", instr.I32_OR, []types.Boxed{types.BoxI32(0b100), types.BoxI32(0b001)}, types.BoxI32(0b101)},
		{"i32 eq", instr.I32_EQ, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
		{"i32 ne", instr.I32_NE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
		{"i32 lt_s", instr.I32_LT_S, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
		{"i32 lt_u", instr.I32_LT_U, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
		{"i32 gt_s", instr.I32_GT_S, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
		{"i32 gt_u", instr.I32_GT_U, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
		{"i32 le_s", instr.I32_LE_S, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
		{"i32 le_u", instr.I32_LE_U, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
		{"i32 ge_s", instr.I32_GE_S, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
		{"i32 ge_u", instr.I32_GE_U, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
		{"i32 eqz", instr.I32_EQZ, []types.Boxed{types.BoxI32(0)}, types.BoxI1(true)},
		{"i32 to f32_s", instr.I32_TO_F32_S, []types.Boxed{types.BoxI32(3)}, types.BoxF32(3)},
		{"i32 to f32_u", instr.I32_TO_F32_U, []types.Boxed{types.BoxI32(3)}, types.BoxF32(3)},

		{"i64 add", instr.I64_ADD, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(17)},
		{"i64 sub", instr.I64_SUB, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(7)},
		{"i64 mul", instr.I64_MUL, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(60)},
		{"i64 div_s", instr.I64_DIV_S, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
		{"i64 div_u", instr.I64_DIV_U, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
		{"i64 rem_s", instr.I64_REM_S, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
		{"i64 rem_u", instr.I64_REM_U, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
		{"i64 shl", instr.I64_SHL, []types.Boxed{types.BoxI64(1), types.BoxI64(4)}, types.BoxI64(16)},
		{"i64 shr_s", instr.I64_SHR_S, []types.Boxed{types.BoxI64(8), types.BoxI64(2)}, types.BoxI64(2)},
		{"i64 shr_u", instr.I64_SHR_U, []types.Boxed{types.BoxI64(8), types.BoxI64(2)}, types.BoxI64(2)},
		{"i64 xor", instr.I64_XOR, []types.Boxed{types.BoxI64(0b110), types.BoxI64(0b011)}, types.BoxI64(0b101)},
		{"i64 and", instr.I64_AND, []types.Boxed{types.BoxI64(0b110), types.BoxI64(0b011)}, types.BoxI64(0b010)},
		{"i64 or", instr.I64_OR, []types.Boxed{types.BoxI64(0b100), types.BoxI64(0b001)}, types.BoxI64(0b101)},
		{"i64 eq", instr.I64_EQ, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
		{"i64 ne", instr.I64_NE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
		{"i64 lt_s", instr.I64_LT_S, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
		{"i64 lt_u", instr.I64_LT_U, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
		{"i64 gt_s", instr.I64_GT_S, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
		{"i64 gt_u", instr.I64_GT_U, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
		{"i64 le_s", instr.I64_LE_S, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
		{"i64 le_u", instr.I64_LE_U, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
		{"i64 ge_s", instr.I64_GE_S, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
		{"i64 ge_u", instr.I64_GE_U, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
		{"i64 eqz", instr.I64_EQZ, []types.Boxed{types.BoxI64(0)}, types.BoxI1(true)},
		{"i64 to i32", instr.I64_TO_I32, []types.Boxed{types.BoxI64(65536)}, types.BoxI32(65536)},
		{"i64 to f32_s", instr.I64_TO_F32_S, []types.Boxed{types.BoxI64(2)}, types.BoxF32(2)},
		{"i64 to f32_u", instr.I64_TO_F32_U, []types.Boxed{types.BoxI64(2)}, types.BoxF32(2)},
		{"i64 to f64_s", instr.I64_TO_F64_S, []types.Boxed{types.BoxI64(2)}, types.BoxF64(2)},
		{"i64 to f64_u", instr.I64_TO_F64_U, []types.Boxed{types.BoxI64(2)}, types.BoxF64(2)},

		{"f32 add", instr.F32_ADD, []types.Boxed{types.BoxF32(1.5), types.BoxF32(2.5)}, types.BoxF32(4)},
		{"f32 sub", instr.F32_SUB, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(4)},
		{"f32 mul", instr.F32_MUL, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(12)},
		{"f32 div", instr.F32_DIV, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(3)},
		{"f32 rem", instr.F32_REM, []types.Boxed{types.BoxF32(-5), types.BoxF32(3)}, types.BoxF32(-2)},
		{"f32 mod", instr.F32_MOD, []types.Boxed{types.BoxF32(-5), types.BoxF32(3)}, types.BoxF32(1)},
		{"f32 eq", instr.F32_EQ, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
		{"f32 ne", instr.F32_NE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
		{"f32 lt", instr.F32_LT, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
		{"f32 gt", instr.F32_GT, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
		{"f32 le", instr.F32_LE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
		{"f32 ge", instr.F32_GE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
		{"f32 to i32_s", instr.F32_TO_I32_S, []types.Boxed{types.BoxF32(3)}, types.BoxI32(3)},
		{"f32 to i32_u", instr.F32_TO_I32_U, []types.Boxed{types.BoxF32(3)}, types.BoxI32(3)},

		{"f64 add", instr.F64_ADD, []types.Boxed{types.BoxF64(1.5), types.BoxF64(2.5)}, types.BoxF64(4)},
		{"f64 sub", instr.F64_SUB, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(4)},
		{"f64 mul", instr.F64_MUL, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(12)},
		{"f64 div", instr.F64_DIV, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(3)},
		{"f64 rem", instr.F64_REM, []types.Boxed{types.BoxF64(-5), types.BoxF64(3)}, types.BoxF64(-2)},
		{"f64 mod", instr.F64_MOD, []types.Boxed{types.BoxF64(-5), types.BoxF64(3)}, types.BoxF64(1)},
		{"f64 eq", instr.F64_EQ, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
		{"f64 ne", instr.F64_NE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
		{"f64 lt", instr.F64_LT, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
		{"f64 gt", instr.F64_GT, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
		{"f64 le", instr.F64_LE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
		{"f64 ge", instr.F64_GE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
		{"f64 to i32_s", instr.F64_TO_I32_S, []types.Boxed{types.BoxF64(3)}, types.BoxI32(3)},
		{"f64 to i32_u", instr.F64_TO_I32_U, []types.Boxed{types.BoxF64(3)}, types.BoxI32(3)},
		{"f64 to i64_s", instr.F64_TO_I64_S, []types.Boxed{types.BoxF64(4)}, types.BoxI64(4)},
		{"f64 to i64_u", instr.F64_TO_I64_U, []types.Boxed{types.BoxF64(4)}, types.BoxI64(4)},
		{"f64 to f32", instr.F64_TO_F32, []types.Boxed{types.BoxF64(1.5)}, types.BoxF32(1.5)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := ssa.New("f")
			entry := b.Block()
			args := make([]ssa.Value, len(c.args))
			for i, a := range c.args {
				args[i] = b.Value(ssa.TypeOf(a.Kind()))
				b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: a, Results: []ssa.Value{args[i]}})
			}
			result := b.Value(ssa.TypeOf(c.want.Kind()))
			b.Add(entry, ssa.Operation{Op: ssa.OpExec, Code: c.code, Args: args, State: deoptState(b, entry, c.code), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Op: ssa.OpReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err)
			require.Equal(t, pass.PreserveNone(), preserved)
			require.NoError(t, ssa.Verify(fn))
			// The folded op is the last one this test added - ordinarily at
			// len(c.args), but one later for I64_ADD, whose own deoptState
			// above inserts an OpState ahead of it.
			ops := fn.Block(entry).Ops
			require.Equal(t, c.want, ops[len(ops)-1].Const)
		})
	}
}

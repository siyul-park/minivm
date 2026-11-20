package transform_test

import (
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/internal/transform"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

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
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))
		before := ssa.Format(fn)
		require.Contains(t, before, "v3:i32 = int.add v1, v2")

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		after := ssa.Format(fn)
		require.NotContains(t, after, "int.add")
		require.Contains(t, after, "v4:i32 = const 5")
		require.Equal(t, sum, ssa.Value(3))
	})

	t.Run("leaves the function untouched when nothing folds", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		param := b.Param(entry, ssa.TypeI32)
		one, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{param, one}, State: deoptState(b, entry), Results: []ssa.Value{sum}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		before := ssa.Format(fn)

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, before, ssa.Format(fn))
	})

	t.Run("leaves a constant divisor of zero unfolded", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, zero, quotient := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 10, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntSDiv, Args: []ssa.Value{x, zero}, State: deoptState(b, entry), Results: []ssa.Value{quotient}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{quotient}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Contains(t, ssa.Format(fn), "int.sdiv")
	})

	t.Run("folds a wide i64 result", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, product := b.Value(ssa.TypeI64), b.Value(ssa.TypeI64), b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(int64(1) << 30), Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: uint64(int64(1) << 30), Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntMul, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{product}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{product}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.NotContains(t, ssa.Format(fn), "int.mul")
		require.Contains(t, ssa.Format(fn), "const 1152921504606846976")
	})

	t.Run("folds a wide shl", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, n, result := b.Value(ssa.TypeI64), b.Value(ssa.TypeI64), b.Value(ssa.TypeI64)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 50, Results: []ssa.Value{n}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntShl, Args: []ssa.Value{x, n}, State: deoptState(b, entry), Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.NotContains(t, ssa.Format(fn), "int.shl")
		require.Contains(t, ssa.Format(fn), "const 1125899906842624")
	})

	t.Run("declines saturating float-to-int conversions", func(t *testing.T) {
		for _, c := range []struct {
			op     ssa.Op
			arg    ssa.Type
			value  float64
			result ssa.Type
		}{
			{ssa.OpFloatFPToSISat, ssa.TypeF32, math.NaN(), ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF32, 3e9, ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF32, -3e9, ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF32, math.NaN(), ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF32, -1, ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF32, 5e9, ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, math.NaN(), ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, 3e9, ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, -3e9, ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, math.NaN(), ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, -1, ssa.TypeI32},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, 5e9, ssa.TypeI32},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, math.NaN(), ssa.TypeI64},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, 1e19, ssa.TypeI64},
			{ssa.OpFloatFPToSISat, ssa.TypeF64, -1e19, ssa.TypeI64},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, math.NaN(), ssa.TypeI64},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, -1, ssa.TypeI64},
			{ssa.OpFloatFPToUISat, ssa.TypeF64, 2e19, ssa.TypeI64},
		} {
			// Threaded saturates these; Go's conversion is implementation-specific.
			word := math.Float64bits(c.value)
			if c.arg == ssa.TypeF32 {
				word = uint64(math.Float32bits(float32(c.value)))
			}
			b := ssa.New("f")
			entry := b.Block()
			x, result := b.Value(c.arg), b.Value(c.result)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: word, Results: []ssa.Value{x}})
			b.Add(entry, ssa.Operation{Op: c.op, Args: []ssa.Value{x}, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err, c.op)
			require.True(t, preserved, c.op)
			require.NoError(t, ssa.Verify(fn))
			require.Contains(t, ssa.Format(fn), c.op.String(), c.op)
		}
	})

	t.Run("folds a value a deopt frame references without disturbing what the activation names", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, y, sum, state := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeState)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 3, Results: []ssa.Value{y}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, y}, State: deoptState(b, entry), Results: []ssa.Value{sum}})
		b.Add(entry, ssa.Operation{Op: ssa.OpState, Frames: []ssa.Frame{{Address: 1, Stack: []ssa.Operand{{Value: sum}}}}, Results: []ssa.Value{state}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermExit, State: state})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, ssa.Format(fn), "stack=[v4]")
		require.Contains(t, ssa.Format(fn), "v4:i32 = const 5")
	})

	t.Run("reduces an identity to its left argument", func(t *testing.T) {
		for _, c := range []struct {
			name     string
			op       ssa.Op
			typ      ssa.Type
			constant types.Boxed
		}{
			{"i32 add zero", ssa.OpIntAdd, ssa.TypeI32, types.BoxI32(0)},
			{"i32 sub zero", ssa.OpIntSub, ssa.TypeI32, types.BoxI32(0)},
			{"i32 or zero", ssa.OpIntOr, ssa.TypeI32, types.BoxI32(0)},
			{"i32 xor zero", ssa.OpIntXor, ssa.TypeI32, types.BoxI32(0)},
			{"i32 shl zero", ssa.OpIntShl, ssa.TypeI32, types.BoxI32(0)},
			{"i32 shr_s zero", ssa.OpIntAShr, ssa.TypeI32, types.BoxI32(0)},
			{"i32 shr_u zero", ssa.OpIntLShr, ssa.TypeI32, types.BoxI32(0)},
			{"i32 and minus one", ssa.OpIntAnd, ssa.TypeI32, types.BoxI32(-1)},
			{"i32 mul one", ssa.OpIntMul, ssa.TypeI32, types.BoxI32(1)},
			{"i32 div_s one", ssa.OpIntSDiv, ssa.TypeI32, types.BoxI32(1)},
			{"i32 div_u one", ssa.OpIntUDiv, ssa.TypeI32, types.BoxI32(1)},
			{"i64 add zero", ssa.OpIntAdd, ssa.TypeI64, types.BoxI64(0)},
			{"i64 and minus one", ssa.OpIntAnd, ssa.TypeI64, types.BoxI64(-1)},
			{"i64 mul one", ssa.OpIntMul, ssa.TypeI64, types.BoxI64(1)},
			{"i64 div_u one", ssa.OpIntUDiv, ssa.TypeI64, types.BoxI64(1)},
		} {
			b := ssa.New("f")
			entry := b.Block()
			param := b.Param(entry, c.typ)
			x, right, result := b.Value(c.typ), b.Value(c.typ), b.Value(c.typ)
			seed := ssa.OpIntSRem
			b.Add(entry, ssa.Operation{Op: seed, Args: []ssa.Value{param, param}, State: deoptState(b, entry), Results: []ssa.Value{x}})
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.constant.Word(), Results: []ssa.Value{right}})
			b.Add(entry, ssa.Operation{Op: c.op, Args: []ssa.Value{x, right}, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err, c.name)
			require.False(t, preserved, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			out := ssa.Format(fn)
			require.NotContains(t, out, c.op.String(), c.name)
			left := regexp.MustCompile(`(v\d+):\S+ = ` + seed.String()).FindStringSubmatch(out)
			require.Len(t, left, 2, c.name)
			require.Contains(t, out, "return "+left[1], c.name)
		}
	})

	t.Run("reduces a multiply or unsigned divide by a power of two to a shift", func(t *testing.T) {
		for _, c := range []struct {
			name     string
			op       ssa.Op
			typ      ssa.Type
			constant types.Boxed
			want     ssa.Op
			amount   int
		}{
			{"an i32 multiply by a power of two to a shift", ssa.OpIntMul, ssa.TypeI32, types.BoxI32(8), ssa.OpIntShl, 3},
			{"an i32 unsigned divide by a power of two to a shift", ssa.OpIntUDiv, ssa.TypeI32, types.BoxI32(16), ssa.OpIntLShr, 4},
			{"an i64 multiply by a power of two to a shift", ssa.OpIntMul, ssa.TypeI64, types.BoxI64(4), ssa.OpIntShl, 2},
			{"an i64 unsigned divide by a power of two to a shift", ssa.OpIntUDiv, ssa.TypeI64, types.BoxI64(2), ssa.OpIntLShr, 1},
		} {
			b := ssa.New("f")
			entry := b.Block()
			x := b.Param(entry, c.typ)
			right, result := b.Value(c.typ), b.Value(c.typ)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.constant.Word(), Results: []ssa.Value{right}})
			b.Add(entry, ssa.Operation{Op: c.op, Args: []ssa.Value{x, right}, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err, c.name)
			require.False(t, preserved, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			out := ssa.Format(fn)
			require.NotContains(t, out, c.op.String(), c.name)
			require.Contains(t, out, c.want.String(), c.name)
			require.Contains(t, out, fmt.Sprintf("const %d", c.amount), c.name)
		}
	})

	t.Run("reduces commutative operations with a constant on either side or the same value on both", func(t *testing.T) {
		word := func(w uint64) *uint64 { return &w }
		for _, c := range []struct {
			name        string
			op          ssa.Op
			cond        ssa.Cond
			typ         ssa.Type
			left, right *uint64
			same        bool
			want        string // "x" aliases the seed; otherwise the folded const text
		}{
			{"i32 zero plus x", ssa.OpIntAdd, 0, ssa.TypeI32, word(0), nil, false, "x"},
			{"i64 zero plus x", ssa.OpIntAdd, 0, ssa.TypeI64, word(0), nil, false, "x"},
			{"i32 one times x", ssa.OpIntMul, 0, ssa.TypeI32, word(1), nil, false, "x"},
			{"i64 one times x", ssa.OpIntMul, 0, ssa.TypeI64, word(1), nil, false, "x"},
			{"i32 x times zero", ssa.OpIntMul, 0, ssa.TypeI32, nil, word(0), false, "const 0"},
			{"i32 zero times x", ssa.OpIntMul, 0, ssa.TypeI32, word(0), nil, false, "const 0"},
			{"i64 x times zero", ssa.OpIntMul, 0, ssa.TypeI64, nil, word(0), false, "const 0"},
			{"i64 zero times x", ssa.OpIntMul, 0, ssa.TypeI64, word(0), nil, false, "const 0"},
			{"i32 x and zero", ssa.OpIntAnd, 0, ssa.TypeI32, nil, word(0), false, "const 0"},
			{"i64 x and zero", ssa.OpIntAnd, 0, ssa.TypeI64, nil, word(0), false, "const 0"},
			{"i32 x or minus one", ssa.OpIntOr, 0, ssa.TypeI32, nil, word(types.BoxI32(-1).Word()), false, "const -1"},
			{"i32 minus one or x", ssa.OpIntOr, 0, ssa.TypeI32, word(types.BoxI32(-1).Word()), nil, false, "const -1"},
			{"i64 x or minus one", ssa.OpIntOr, 0, ssa.TypeI64, nil, word(math.MaxUint64), false, "const -1"},
			{"i32 x minus x", ssa.OpIntSub, 0, ssa.TypeI32, nil, nil, true, "const 0"},
			{"i64 x minus x", ssa.OpIntSub, 0, ssa.TypeI64, nil, nil, true, "const 0"},
			{"i32 x xor x", ssa.OpIntXor, 0, ssa.TypeI32, nil, nil, true, "const 0"},
			{"i64 x xor x", ssa.OpIntXor, 0, ssa.TypeI64, nil, nil, true, "const 0"},
			{"i32 x and x", ssa.OpIntAnd, 0, ssa.TypeI32, nil, nil, true, "x"},
			{"i64 x or x", ssa.OpIntOr, 0, ssa.TypeI64, nil, nil, true, "x"},
			{"i32 x equals x", ssa.OpIntCmp, ssa.CondEQ, ssa.TypeI32, nil, nil, true, "const true"},
			{"i64 x less or equal x", ssa.OpIntCmp, ssa.CondSLE, ssa.TypeI64, nil, nil, true, "const true"},
			{"i32 x unsigned greater or equal x", ssa.OpIntCmp, ssa.CondUGE, ssa.TypeI32, nil, nil, true, "const true"},
			{"i32 x less than x", ssa.OpIntCmp, ssa.CondSLT, ssa.TypeI32, nil, nil, true, "const false"},
			{"i64 x unsigned greater than x", ssa.OpIntCmp, ssa.CondUGT, ssa.TypeI64, nil, nil, true, "const false"},
			{"i32 x not equal x", ssa.OpIntCmp, ssa.CondNE, ssa.TypeI32, nil, nil, true, "const false"},
		} {
			b := ssa.New("f")
			entry := b.Block()
			param := b.Param(entry, c.typ)
			seed := ssa.OpIntSRem
			resultType := c.typ
			if strings.HasPrefix(c.want, "const t") || strings.HasPrefix(c.want, "const f") {
				resultType = ssa.TypeI1
			}
			result := b.Value(resultType)
			x := b.Value(c.typ)
			b.Add(entry, ssa.Operation{Op: seed, Args: []ssa.Value{param, param}, State: deoptState(b, entry), Results: []ssa.Value{x}})
			args := []ssa.Value{x, x}
			for i, w := range []*uint64{c.left, c.right} {
				if w != nil {
					args[i] = b.Value(c.typ)
					b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: *w, Results: []ssa.Value{args[i]}})
				}
			}
			b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: args, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err, c.name)
			require.False(t, preserved, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			out := ssa.Format(fn)
			require.NotContains(t, out, c.op.String(), c.name)
			if c.want == "x" {
				left := regexp.MustCompile(`(v\d+):\S+ = ` + seed.String()).FindStringSubmatch(out)
				require.Len(t, left, 2, c.name)
				require.Contains(t, out, "return "+left[1], c.name)
			} else {
				ret := regexp.MustCompile(`return (v\d+)`).FindStringSubmatch(out)
				require.Len(t, ret, 2, c.name)
				require.Regexp(t, "(?m)"+ret[1]+`:\S+ = `+regexp.QuoteMeta(c.want)+`$`, out, c.name)
			}
		}
	})

	t.Run("reduces a multiply by a left power-of-two constant to a shift", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Param(entry, ssa.TypeI32)
		left, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: types.BoxI32(8).Word(), Results: []ssa.Value{left}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntMul, Args: []ssa.Value{left, x}, State: deoptState(b, entry), Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		out := ssa.Format(fn)
		require.NotContains(t, out, "int.mul")
		require.Contains(t, out, "int.shl v1,")
		require.Contains(t, out, "const 3")
	})

	t.Run("leaves same-operand operations that trap or involve floats", func(t *testing.T) {
		for _, c := range []struct {
			name        string
			op          ssa.Op
			cond        ssa.Cond
			typ, result ssa.Type
			input       uint64
			want        string
		}{
			{"i32 zero divided by itself traps", ssa.OpIntSDiv, 0, ssa.TypeI32, ssa.TypeI32, 0, ""},
			{"i64 zero remainder itself traps", ssa.OpIntURem, 0, ssa.TypeI64, ssa.TypeI64, 0, ""},
			{"f64 infinity minus itself is NaN", ssa.OpFloatSub, 0, ssa.TypeF64, ssa.TypeF64, math.Float64bits(math.Inf(1)), "NaN"},
			{"f32 NaN equals itself is false", ssa.OpFloatCmp, ssa.CondOEQ, ssa.TypeF32, ssa.TypeI1, uint64(math.Float32bits(float32(math.NaN()))), "const false"},
		} {
			// Check the typed constant case before the unknown-operand identity.
			b := ssa.New("constant")
			entry := b.Block()
			x, result := b.Value(c.typ), b.Value(c.result)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: c.input, Results: []ssa.Value{x}})
			b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: []ssa.Value{x, x}, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)
			before := ssa.Format(fn)
			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)
			require.NoError(t, err, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			if c.want == "" {
				require.True(t, preserved, c.name)
				require.Equal(t, before, ssa.Format(fn), c.name)
			} else {
				require.False(t, preserved, c.name)
				require.Contains(t, ssa.Format(fn), c.want, c.name)
			}

			b = ssa.New("f")
			entry = b.Block()
			x, result = b.Param(entry, c.typ), b.Value(c.result)
			b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: []ssa.Value{x, x}, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn = b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)
			before = ssa.Format(fn)
			preserved, err = transform.NewFoldPass().Run(pass.NewManager(), fn)
			require.NoError(t, err, c.name)
			require.True(t, preserved, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			require.Equal(t, before, ssa.Format(fn), c.name)
		}
	})

	t.Run("folds every pure opcode over constant arguments", func(t *testing.T) {
		for _, c := range []struct {
			name  string
			op    ssa.Op
			cond  ssa.Cond
			boxes []types.Boxed
			want  types.Boxed
		}{
			{"i32 add", ssa.OpIntAdd, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(17)},
			{"i32 sub", ssa.OpIntSub, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(7)},
			{"i32 mul", ssa.OpIntMul, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(60)},
			{"i32 div_s", ssa.OpIntSDiv, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
			{"i32 div_u", ssa.OpIntUDiv, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
			{"i32 rem_s", ssa.OpIntSRem, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
			{"i32 rem_u", ssa.OpIntURem, 0, []types.Boxed{types.BoxI32(12), types.BoxI32(5)}, types.BoxI32(2)},
			{"i32 shl", ssa.OpIntShl, 0, []types.Boxed{types.BoxI32(1), types.BoxI32(3)}, types.BoxI32(8)},
			{"i32 shr_s", ssa.OpIntAShr, 0, []types.Boxed{types.BoxI32(8), types.BoxI32(2)}, types.BoxI32(2)},
			{"i32 shr_u", ssa.OpIntLShr, 0, []types.Boxed{types.BoxI32(8), types.BoxI32(2)}, types.BoxI32(2)},
			{"i32 xor", ssa.OpIntXor, 0, []types.Boxed{types.BoxI32(0b110), types.BoxI32(0b011)}, types.BoxI32(0b101)},
			{"i32 and", ssa.OpIntAnd, 0, []types.Boxed{types.BoxI32(0b110), types.BoxI32(0b011)}, types.BoxI32(0b010)},
			{"i32 or", ssa.OpIntOr, 0, []types.Boxed{types.BoxI32(0b100), types.BoxI32(0b001)}, types.BoxI32(0b101)},
			{"i32 eq", ssa.OpIntCmp, ssa.CondEQ, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
			{"i32 ne", ssa.OpIntCmp, ssa.CondNE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
			{"i32 lt_s", ssa.OpIntCmp, ssa.CondSLT, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
			{"i32 lt_u", ssa.OpIntCmp, ssa.CondULT, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
			{"i32 gt_s", ssa.OpIntCmp, ssa.CondSGT, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
			{"i32 gt_u", ssa.OpIntCmp, ssa.CondUGT, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
			{"i32 le_s", ssa.OpIntCmp, ssa.CondSLE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
			{"i32 le_u", ssa.OpIntCmp, ssa.CondULE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(true)},
			{"i32 ge_s", ssa.OpIntCmp, ssa.CondSGE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
			{"i32 ge_u", ssa.OpIntCmp, ssa.CondUGE, []types.Boxed{types.BoxI32(1), types.BoxI32(2)}, types.BoxI1(false)},
			{"i32 eqz", ssa.OpIntCmp, ssa.CondEQ, []types.Boxed{types.BoxI32(0), types.BoxI32(0)}, types.BoxI1(true)},
			{"i32 to f32_s", ssa.OpIntSIToFP, 0, []types.Boxed{types.BoxI32(3)}, types.BoxF32(3)},
			{"i32 to f32_u", ssa.OpIntUIToFP, 0, []types.Boxed{types.BoxI32(3)}, types.BoxF32(3)},
			{"i64 add", ssa.OpIntAdd, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(17)},
			{"i64 sub", ssa.OpIntSub, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(7)},
			{"i64 mul", ssa.OpIntMul, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(60)},
			{"i64 div_s", ssa.OpIntSDiv, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
			{"i64 div_u", ssa.OpIntUDiv, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
			{"i64 rem_s", ssa.OpIntSRem, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
			{"i64 rem_u", ssa.OpIntURem, 0, []types.Boxed{types.BoxI64(12), types.BoxI64(5)}, types.BoxI64(2)},
			{"i64 shl", ssa.OpIntShl, 0, []types.Boxed{types.BoxI64(1), types.BoxI64(4)}, types.BoxI64(16)},
			{"i64 shr_s", ssa.OpIntAShr, 0, []types.Boxed{types.BoxI64(8), types.BoxI64(2)}, types.BoxI64(2)},
			{"i64 shr_u", ssa.OpIntLShr, 0, []types.Boxed{types.BoxI64(8), types.BoxI64(2)}, types.BoxI64(2)},
			{"i64 xor", ssa.OpIntXor, 0, []types.Boxed{types.BoxI64(0b110), types.BoxI64(0b011)}, types.BoxI64(0b101)},
			{"i64 and", ssa.OpIntAnd, 0, []types.Boxed{types.BoxI64(0b110), types.BoxI64(0b011)}, types.BoxI64(0b010)},
			{"i64 or", ssa.OpIntOr, 0, []types.Boxed{types.BoxI64(0b100), types.BoxI64(0b001)}, types.BoxI64(0b101)},
			{"i64 eq", ssa.OpIntCmp, ssa.CondEQ, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
			{"i64 ne", ssa.OpIntCmp, ssa.CondNE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
			{"i64 lt_s", ssa.OpIntCmp, ssa.CondSLT, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
			{"i64 lt_u", ssa.OpIntCmp, ssa.CondULT, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
			{"i64 gt_s", ssa.OpIntCmp, ssa.CondSGT, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
			{"i64 gt_u", ssa.OpIntCmp, ssa.CondUGT, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
			{"i64 le_s", ssa.OpIntCmp, ssa.CondSLE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
			{"i64 le_u", ssa.OpIntCmp, ssa.CondULE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(true)},
			{"i64 ge_s", ssa.OpIntCmp, ssa.CondSGE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
			{"i64 ge_u", ssa.OpIntCmp, ssa.CondUGE, []types.Boxed{types.BoxI64(1), types.BoxI64(2)}, types.BoxI1(false)},
			{"i64 eqz", ssa.OpIntCmp, ssa.CondEQ, []types.Boxed{types.BoxI64(0), types.BoxI64(0)}, types.BoxI1(true)},
			{"i64 to i32", ssa.OpIntWrap, 0, []types.Boxed{types.BoxI64(65536)}, types.BoxI32(65536)},
			{"i64 to f32_s", ssa.OpIntSIToFP, 0, []types.Boxed{types.BoxI64(2)}, types.BoxF32(2)},
			{"i64 to f32_u", ssa.OpIntUIToFP, 0, []types.Boxed{types.BoxI64(2)}, types.BoxF32(2)},
			{"i64 to f64_s", ssa.OpIntSIToFP, 0, []types.Boxed{types.BoxI64(2)}, types.BoxF64(2)},
			{"i64 to f64_u", ssa.OpIntUIToFP, 0, []types.Boxed{types.BoxI64(2)}, types.BoxF64(2)},
			{"f32 add", ssa.OpFloatAdd, 0, []types.Boxed{types.BoxF32(1.5), types.BoxF32(2.5)}, types.BoxF32(4)},
			{"f32 sub", ssa.OpFloatSub, 0, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(4)},
			{"f32 mul", ssa.OpFloatMul, 0, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(12)},
			{"f32 div", ssa.OpFloatDiv, 0, []types.Boxed{types.BoxF32(6), types.BoxF32(2)}, types.BoxF32(3)},
			{"f32 eq", ssa.OpFloatCmp, ssa.CondOEQ, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
			{"f32 ne", ssa.OpFloatCmp, ssa.CondUNE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
			{"f32 lt", ssa.OpFloatCmp, ssa.CondOLT, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
			{"f32 gt", ssa.OpFloatCmp, ssa.CondOGT, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
			{"f32 le", ssa.OpFloatCmp, ssa.CondOLE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(true)},
			{"f32 ge", ssa.OpFloatCmp, ssa.CondOGE, []types.Boxed{types.BoxF32(1), types.BoxF32(2)}, types.BoxI1(false)},
			{"f32 to i32_s", ssa.OpFloatFPToSISat, 0, []types.Boxed{types.BoxF32(3)}, types.BoxI32(3)},
			{"f32 to i32_u", ssa.OpFloatFPToUISat, 0, []types.Boxed{types.BoxF32(3)}, types.BoxI32(3)},
			{"f64 add", ssa.OpFloatAdd, 0, []types.Boxed{types.BoxF64(1.5), types.BoxF64(2.5)}, types.BoxF64(4)},
			{"f64 sub", ssa.OpFloatSub, 0, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(4)},
			{"f64 mul", ssa.OpFloatMul, 0, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(12)},
			{"f64 div", ssa.OpFloatDiv, 0, []types.Boxed{types.BoxF64(6), types.BoxF64(2)}, types.BoxF64(3)},
			{"f64 eq", ssa.OpFloatCmp, ssa.CondOEQ, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
			{"f64 ne", ssa.OpFloatCmp, ssa.CondUNE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
			{"f64 lt", ssa.OpFloatCmp, ssa.CondOLT, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
			{"f64 gt", ssa.OpFloatCmp, ssa.CondOGT, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
			{"f64 le", ssa.OpFloatCmp, ssa.CondOLE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(true)},
			{"f64 ge", ssa.OpFloatCmp, ssa.CondOGE, []types.Boxed{types.BoxF64(1), types.BoxF64(2)}, types.BoxI1(false)},
			{"f64 to i32_s", ssa.OpFloatFPToSISat, 0, []types.Boxed{types.BoxF64(3)}, types.BoxI32(3)},
			{"f64 to i32_u", ssa.OpFloatFPToUISat, 0, []types.Boxed{types.BoxF64(3)}, types.BoxI32(3)},
			{"f64 to i64_s", ssa.OpFloatFPToSISat, 0, []types.Boxed{types.BoxF64(4)}, types.BoxI64(4)},
			{"f64 to i64_u", ssa.OpFloatFPToUISat, 0, []types.Boxed{types.BoxF64(4)}, types.BoxI64(4)},
			{"f64 to f32", ssa.OpFloatFPTrunc, 0, []types.Boxed{types.BoxF64(1.5)}, types.BoxF32(1.5)},
		} {
			b := ssa.New("f")
			entry := b.Block()
			args := make([]ssa.Value, len(c.boxes))
			for i, box := range c.boxes {
				args[i] = b.Value(ssa.TypeOf(box.Kind()))
				b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: box.Word(), Results: []ssa.Value{args[i]}})
			}
			result := b.Value(ssa.TypeOf(c.want.Kind()))
			b.Add(entry, ssa.Operation{Op: c.op, Cond: c.cond, Args: args, State: deoptState(b, entry), Results: []ssa.Value{result}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn), c.name)

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err, c.name)
			require.False(t, preserved, c.name)
			require.NoError(t, ssa.Verify(fn), c.name)
			ops := fn.Block(entry).Operations
			require.Equal(t, c.want.Word(), ops[len(ops)-1].Const, c.name)
		}
	})
	t.Run("leaves an identity over a value read out of a slot", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x, right, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpLoad, Slot: ssa.Slot{Space: ssa.SpaceLocal}, Results: []ssa.Value{x}})
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{right}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, right}, State: deoptState(b, entry), Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Contains(t, ssa.Format(fn), "int.add")
	})

	t.Run("leaves a signed divide by a power of two alone", func(t *testing.T) {
		b := ssa.New("f")
		entry := b.Block()
		x := b.Param(entry, ssa.TypeI32)
		right, result := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 8, Results: []ssa.Value{right}})
		b.Add(entry, ssa.Operation{Op: ssa.OpIntSDiv, Args: []ssa.Value{x, right}, State: deoptState(b, entry), Results: []ssa.Value{result}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{result}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Contains(t, ssa.Format(fn), "int.sdiv")
	})

	// selects builds entry -> op(const) over n targets that each return their own
	// index, runs the pass, and reports the entry's terminator, the index of the
	// target it now jumps to (-1 when it is not a jump), and whether the pass preserved.
	selects := func(t *testing.T, typ ssa.Type, w uint64, op ssa.Term, n int) (ssa.Terminator, int, bool) {
		b := ssa.New("f")
		entry := b.Block()
		cond := b.Value(typ)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: w, Results: []ssa.Value{cond}})
		edges := make([]ssa.Edge, n)
		for i := range edges {
			target := b.Block()
			ret := b.Value(ssa.TypeI32)
			b.Add(target, ssa.Operation{Op: ssa.OpConst, Const: uint64(i), Results: []ssa.Value{ret}})
			b.Term(target, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{ret}})
			edges[i] = ssa.Edge{Block: target}
		}
		b.Term(entry, ssa.Terminator{Term: op, Args: []ssa.Value{cond}, Edges: edges})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		term := fn.Block(0).Terminator
		if term.Term != ssa.TermJump {
			return term, -1, preserved
		}
		return term, int(fn.Block(term.Edges[0].Block).Operations[0].Const), preserved
	}

	t.Run("folds a branch on a true i1 to its taken edge", func(t *testing.T) {
		term, taken, preserved := selects(t, ssa.TypeI1, 1, ssa.TermBrIf, 2)
		require.False(t, preserved)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Empty(t, term.Args)
		require.Equal(t, 0, taken)
	})

	t.Run("folds a branch on a false i1 to its fall-through edge", func(t *testing.T) {
		term, taken, preserved := selects(t, ssa.TypeI1, 0, ssa.TermBrIf, 2)
		require.False(t, preserved)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, 1, taken)
	})

	t.Run("folds a branch on a nonzero i32 to its taken edge", func(t *testing.T) {
		term, taken, _ := selects(t, ssa.TypeI32, 7, ssa.TermBrIf, 2)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, 0, taken)
	})

	t.Run("folds a branch on a zero i32 to its fall-through edge", func(t *testing.T) {
		term, taken, _ := selects(t, ssa.TypeI32, 0, ssa.TermBrIf, 2)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, 1, taken)
	})

	t.Run("folds a table on an in-range index to that edge", func(t *testing.T) {
		term, taken, preserved := selects(t, ssa.TypeI32, 1, ssa.TermBrTable, 3)
		require.False(t, preserved)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Empty(t, term.Args)
		require.Equal(t, 1, taken)
	})

	t.Run("folds a table on an out-of-range index to the default edge", func(t *testing.T) {
		term, taken, _ := selects(t, ssa.TypeI32, 2, ssa.TermBrTable, 3)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, 2, taken)
	})

	t.Run("folds a table on a negative index to the default edge", func(t *testing.T) {
		term, taken, _ := selects(t, ssa.TypeI32, uint64(uint32(math.MaxUint32)), ssa.TermBrTable, 3)
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, 2, taken)
	})

	t.Run("folds a branch whose edges are identical", func(t *testing.T) {
		b := ssa.New("f")
		entry, next := b.Block(), b.Block()
		cond, arg := b.Param(entry, ssa.TypeI32), b.Param(entry, ssa.TypeI32)
		p := b.Param(next, ssa.TypeI32)
		b.Term(next, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: next, Args: []ssa.Value{arg}}, {Block: next, Args: []ssa.Value{arg}}}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.False(t, preserved)
		require.NoError(t, ssa.Verify(fn))
		term := fn.Block(0).Terminator
		require.Equal(t, ssa.TermJump, term.Term)
		require.Equal(t, []ssa.Edge{{Block: next, Args: []ssa.Value{arg}}}, term.Edges)
	})

	t.Run("keeps a branch on a non-constant", func(t *testing.T) {
		b := ssa.New("f")
		entry, left, right := b.Block(), b.Block(), b.Block()
		cond := b.Param(entry, ssa.TypeI32)
		zero := b.Value(ssa.TypeI32)
		b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(left, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{zero}})
		b.Term(right, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{zero}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
		fn := b.Build()

		preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.True(t, preserved)
		require.Equal(t, ssa.TermBrIf, fn.Block(0).Terminator.Term)
	})

	t.Run("leaves no untaken successor after folding, simplifying and dead code elimination", func(t *testing.T) {
		b := ssa.New("f")
		entry, taken, dead := b.Block(), b.Block(), b.Block()
		cond, one := b.Value(ssa.TypeI1), b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{cond}})
		b.Add(taken, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Term(taken, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{one}})
		other := b.Value(ssa.TypeI32)
		b.Add(dead, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{other}})
		b.Term(dead, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{other}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: taken}, {Block: dead}}})
		fn := b.Build()

		_, err := transform.NewFoldPass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		_, err = transform.NewSimplifyPass().Run(pass.NewManager(), fn)
		require.NoError(t, err)
		_, err = transform.NewDCEPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.Equal(t, 1, fn.Len())
	})

	// joined builds entry -> (a | b) -> m(x), where m returns x+1, runs the pass
	// and reports whether m's add is gone. cond is the entry branch's condition:
	// a constant when given, else a param.
	joined := func(t *testing.T, cond *uint64, left, right uint64) bool {
		b := ssa.New("f")
		entry, a, c, m := b.Block(), b.Block(), b.Block(), b.Block()
		var test ssa.Value
		if cond == nil {
			test = b.Param(entry, ssa.TypeI32)
		} else {
			test = b.Value(ssa.TypeI1)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: *cond, Results: []ssa.Value{test}})
		}
		b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{test}, Edges: []ssa.Edge{{Block: a}, {Block: c}}})
		x := b.Param(m, ssa.TypeI32)
		for _, arm := range []struct {
			block int
			w     uint64
		}{{a, left}, {c, right}} {
			v := b.Value(ssa.TypeI32)
			b.Add(arm.block, ssa.Operation{Op: ssa.OpConst, Const: arm.w, Results: []ssa.Value{v}})
			b.Term(arm.block, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: m, Args: []ssa.Value{v}}}})
		}
		one, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(m, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(m, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, one}, State: deoptState(b, m), Results: []ssa.Value{sum}})
		b.Term(m, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{sum}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		return !strings.Contains(ssa.Format(fn), "int.add")
	}

	t.Run("folds a block param every edge feeds the same constant", func(t *testing.T) {
		require.True(t, joined(t, nil, 5, 5))
	})

	t.Run("keeps a block param fed different constants", func(t *testing.T) {
		require.False(t, joined(t, nil, 5, 6))
	})

	t.Run("ignores an edge a collapsed branch drops", func(t *testing.T) {
		taken := uint64(1)
		require.True(t, joined(t, &taken, 5, 6))
	})

	t.Run("keeps a loop header param its back edge also feeds", func(t *testing.T) {
		b := ssa.New("f")
		entry, header, exit := b.Block(), b.Block(), b.Block()
		p := b.Param(entry, ssa.TypeI32)
		zero := b.Value(ssa.TypeI32)
		b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: 0, Results: []ssa.Value{zero}})
		b.Term(entry, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}}})
		x := b.Param(header, ssa.TypeI32)
		one, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
		b.Add(header, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
		b.Add(header, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{x, one}, State: deoptState(b, header), Results: []ssa.Value{sum}})
		b.Term(header, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{p}, Edges: []ssa.Edge{{Block: header, Args: []ssa.Value{zero}}, {Block: exit}}})
		b.Term(exit, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
		fn := b.Build()
		require.NoError(t, ssa.Verify(fn))

		_, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

		require.NoError(t, err)
		require.NoError(t, ssa.Verify(fn))
		require.Contains(t, ssa.Format(fn), "int.add")
	})

	t.Run("still folds operations when a folded branch enters an irreducible loop", func(t *testing.T) {
		for _, w := range []uint64{0, 1} {
			b := ssa.New("f")
			entry, left, right, exit := b.Block(), b.Block(), b.Block(), b.Block()
			p := b.Param(entry, ssa.TypeI32)
			cond := b.Value(ssa.TypeI32)
			b.Add(entry, ssa.Operation{Op: ssa.OpConst, Const: w, Results: []ssa.Value{cond}})
			b.Term(entry, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{cond}, Edges: []ssa.Edge{{Block: left}, {Block: right}}})
			one, two, sum := b.Value(ssa.TypeI32), b.Value(ssa.TypeI32), b.Value(ssa.TypeI32)
			b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: 1, Results: []ssa.Value{one}})
			b.Add(left, ssa.Operation{Op: ssa.OpConst, Const: 2, Results: []ssa.Value{two}})
			b.Add(left, ssa.Operation{Op: ssa.OpIntAdd, Args: []ssa.Value{one, two}, State: deoptState(b, left), Results: []ssa.Value{sum}})
			b.Term(left, ssa.Terminator{Term: ssa.TermJump, Edges: []ssa.Edge{{Block: right}}})
			b.Term(right, ssa.Terminator{Term: ssa.TermBrIf, Args: []ssa.Value{p}, Edges: []ssa.Edge{{Block: left}, {Block: exit}}})
			b.Term(exit, ssa.Terminator{Term: ssa.TermReturn, Args: []ssa.Value{p}})
			fn := b.Build()
			require.NoError(t, ssa.Verify(fn))

			preserved, err := transform.NewFoldPass().Run(pass.NewManager(), fn)

			require.NoError(t, err)
			require.False(t, preserved)
			require.NoError(t, ssa.Verify(fn))
			require.NotContains(t, ssa.Format(fn), "int.add")
			require.Contains(t, ssa.Format(fn), "const 3")
		}
	})
}

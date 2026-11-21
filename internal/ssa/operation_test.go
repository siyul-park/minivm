package ssa_test

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// representatives names, for each modelled operation, an opcode whose
// metadata it must match.
var representatives = map[ssa.Op]instr.Opcode{
	ssa.OpSelect: instr.SELECT, ssa.OpCall: instr.CALL,
	ssa.OpIntSAddOvf: instr.I32_ADD_OVF_S, ssa.OpIntSSubOvf: instr.I64_SUB_OVF_S, ssa.OpIntSMulOvf: instr.I32_MUL_OVF_S,
	ssa.OpIntUAddOvf: instr.I32_ADD_OVF_U, ssa.OpIntUSubOvf: instr.I64_SUB_OVF_U, ssa.OpIntUMulOvf: instr.I32_MUL_OVF_U,
	ssa.OpIntAdd: instr.I32_ADD, ssa.OpIntSDiv: instr.I32_DIV_S, ssa.OpIntUDiv: instr.I64_DIV_U,
	ssa.OpIntSRem: instr.I32_REM_S, ssa.OpIntURem: instr.I64_REM_U, ssa.OpIntCmp: instr.I32_EQ,
	ssa.OpIntClz: instr.I32_CLZ, ssa.OpIntSExt: instr.I32_TO_I64_S, ssa.OpIntBitcast: instr.I32_REINTERPRET_F32,
	ssa.OpFloatAdd: instr.F32_ADD, ssa.OpFloatMin: instr.F32_MIN, ssa.OpFloatCmp: instr.F32_EQ,
	ssa.OpFloatFPToSISat: instr.F32_TO_I32_S, ssa.OpFloatBitcast: instr.F32_REINTERPRET_I32,
	ssa.OpRefCmp: instr.REF_EQ, ssa.OpRefIsNull: instr.REF_IS_NULL,
	ssa.OpRefTest: instr.REF_TEST, ssa.OpRefCast: instr.REF_CAST,
	ssa.OpStringCmp: instr.STRING_EQ, ssa.OpStringLen: instr.STRING_LEN,
	ssa.OpArrayNewDefault: instr.ARRAY_NEW_DEFAULT, ssa.OpArrayGet: instr.ARRAY_GET, ssa.OpArraySet: instr.ARRAY_SET, ssa.OpArrayLen: instr.ARRAY_LEN,
	ssa.OpStructNewDefault: instr.STRUCT_NEW_DEFAULT, ssa.OpStructGet: instr.STRUCT_GET, ssa.OpStructSet: instr.STRUCT_SET,
	ssa.OpMapLen: instr.MAP_LEN, ssa.OpMapGet: instr.MAP_GET, ssa.OpMapLookup: instr.MAP_LOOKUP, ssa.OpMapSet: instr.MAP_SET, ssa.OpMapDelete: instr.MAP_DELETE,
}

func TestOperation_Reads(t *testing.T) {
	t.Run("mirrors the instr effect bits", func(t *testing.T) {
		require.Equal(t, ssa.EffectLocal, ssa.Effect(instr.Local))
		require.Equal(t, ssa.EffectGlobal, ssa.Effect(instr.Global))
		require.Equal(t, ssa.EffectUpval, ssa.Effect(instr.Upval))
		require.Equal(t, ssa.EffectHeap, ssa.Effect(instr.Heap))
		require.Equal(t, ssa.EffectFrame, ssa.Effect(instr.Frame))
		require.Equal(t, ssa.EffectBranch, ssa.Effect(instr.Branch))
	})
	t.Run("defers exec to its opcode", func(t *testing.T) {
		for code := instr.Opcode(0); instr.Valid(code); code++ {
			o := ssa.Operation{Op: ssa.OpExec, Code: code}
			require.Equal(t, ssa.Effect(instr.TypeOf(code).Reads), o.Reads(), instr.TypeOf(code).Mnemonic)
		}
	})
	t.Run("equals the representative opcode", func(t *testing.T) {
		for op, code := range representatives {
			require.Equal(t, ssa.Effect(instr.TypeOf(code).Reads), ssa.Operation{Op: op}.Reads(), op.String())
		}
	})
}

func TestOperation_Writes(t *testing.T) {
	t.Run("defers exec to its opcode", func(t *testing.T) {
		for code := instr.Opcode(0); instr.Valid(code); code++ {
			o := ssa.Operation{Op: ssa.OpExec, Code: code}
			require.Equal(t, ssa.Effect(instr.TypeOf(code).Writes), o.Writes(), instr.TypeOf(code).Mnemonic)
		}
	})
	t.Run("equals the representative opcode", func(t *testing.T) {
		for op, code := range representatives {
			require.Equal(t, ssa.Effect(instr.TypeOf(code).Writes), ssa.Operation{Op: op}.Writes(), op.String())
		}
	})
}

func TestOperation_Traps(t *testing.T) {
	t.Run("defers exec to its opcode", func(t *testing.T) {
		for code := instr.Opcode(0); instr.Valid(code); code++ {
			require.Equal(t, code.Traps(), ssa.Operation{Op: ssa.OpExec, Code: code}.Traps(), instr.TypeOf(code).Mnemonic)
		}
	})
	t.Run("equals the representative opcode", func(t *testing.T) {
		for op, code := range representatives {
			require.Equal(t, code.Traps(), ssa.Operation{Op: op}.Traps(), op.String())
		}
	})
}

func TestOperation_Pure(t *testing.T) {
	t.Run("defers exec to its opcode", func(t *testing.T) {
		for code := instr.Opcode(0); instr.Valid(code); code++ {
			require.Equal(t, code.IsPure(), ssa.Operation{Op: ssa.OpExec, Code: code}.Pure(), instr.TypeOf(code).Mnemonic)
		}
	})
	t.Run("equals the representative opcode when it yields a result", func(t *testing.T) {
		for op, code := range representatives {
			o := ssa.Operation{Op: op, Results: make([]ssa.Value, len(instr.TypeOf(code).Push))}
			require.Equal(t, code.IsPure(), o.Pure(), op.String())
		}
	})
	t.Run("rejects operations that observe or exit", func(t *testing.T) {
		for _, op := range []ssa.Op{ssa.OpLoad, ssa.OpStore, ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardBounds, ssa.OpGuardValue, ssa.OpRefRetain, ssa.OpRefRelease, ssa.OpState, ssa.OpArrayView, ssa.OpArrayBound} {
			require.False(t, ssa.Operation{Op: op, Results: []ssa.Value{1}}.Pure(), op.String())
		}
	})
	t.Run("accepts a constant", func(t *testing.T) {
		require.True(t, ssa.Operation{Op: ssa.OpConst, Results: []ssa.Value{1}}.Pure())
	})
}

func TestOp_Data(t *testing.T) {
	for op := ssa.OpConst; op <= ssa.OpMapDelete; op++ {
		switch op {
		case ssa.OpConst, ssa.OpExec, ssa.OpLoad, ssa.OpStore, ssa.OpGuardKind, ssa.OpGuardShape, ssa.OpGuardBounds, ssa.OpGuardValue,
			ssa.OpRefRetain, ssa.OpRefRelease, ssa.OpState, ssa.OpArrayView, ssa.OpArrayBound:
			require.False(t, op.Data(), op.String())
		default:
			require.True(t, op.Data(), op.String())
		}
	}
	require.False(t, (ssa.OpMapDelete + 1).Data())
}

func TestCond_Invert(t *testing.T) {
	pairs := [][2]ssa.Cond{
		{ssa.CondEQ, ssa.CondNE}, {ssa.CondSLT, ssa.CondSGE}, {ssa.CondSLE, ssa.CondSGT},
		{ssa.CondULT, ssa.CondUGE}, {ssa.CondULE, ssa.CondUGT},
		{ssa.CondOEQ, ssa.CondUNE}, {ssa.CondONE, ssa.CondUEQ}, {ssa.CondOLT, ssa.CondUNGE},
		{ssa.CondOLE, ssa.CondUNGT}, {ssa.CondOGT, ssa.CondUNLE}, {ssa.CondOGE, ssa.CondUNLT},
	}
	t.Run("inverts every pair both ways", func(t *testing.T) {
		for _, p := range pairs {
			require.Equal(t, p[1], p[0].Invert())
			require.Equal(t, p[0], p[1].Invert())
		}
	})
	t.Run("is an involution over every condition", func(t *testing.T) {
		for c := ssa.CondEQ; c <= ssa.CondUNGE; c++ {
			require.NotEqual(t, c, c.Invert())
			require.Equal(t, c, c.Invert().Invert())
		}
	})
	t.Run("keeps an invalid condition", func(t *testing.T) {
		require.Equal(t, ssa.Cond(0), ssa.Cond(0).Invert())
	})
}

func TestShape_Serves(t *testing.T) {
	i32 := ssa.Shape{Form: ssa.FormMap, Key: types.KindI32, Elem: types.KindRef}
	i64 := ssa.Shape{Form: ssa.FormMap, Key: types.KindI64, Elem: types.KindI64}
	tests := []struct {
		name   string
		shape  ssa.Shape
		code   ssa.Op
		key    types.Kind
		value  types.Kind
		served bool
	}{
		{"len of any map of an integer key", i64, ssa.OpMapLen, 0, 0, true},
		{"get of a reference element", i32, ssa.OpMapGet, types.KindI32, 0, true},
		{"lookup of a reference element", i32, ssa.OpMapLookup, types.KindI8, 0, true},
		{"get of an i64 element", i64, ssa.OpMapGet, types.KindI64, 0, false},
		{"lookup of an i64 element", i64, ssa.OpMapLookup, types.KindI64, 0, false},
		{"set of a one-register value", i32, ssa.OpMapSet, types.KindI32, types.KindF64, true},
		{"set of an i64 value", i32, ssa.OpMapSet, types.KindI32, types.KindI64, false},
		{"delete of an i64 key", i64, ssa.OpMapDelete, types.KindI64, 0, true},
		{"i64 key held in a 32-bit lane", i64, ssa.OpMapDelete, types.KindI32, 0, false},
		{"i32 key held in a 64-bit word", i32, ssa.OpMapDelete, types.KindI64, 0, false},
		{"float key", ssa.Shape{Form: ssa.FormMap, Key: types.KindF64, Elem: types.KindI32, Class: types.ClassFloat}, ssa.OpMapLen, 0, 0, false},
		{"string key", ssa.Shape{Form: ssa.FormMap, Key: types.KindRef, Elem: types.KindI32, Class: types.ClassText}, ssa.OpMapLen, 0, 0, false},
		{"any key", ssa.Shape{Form: ssa.FormMap, Key: types.KindRef, Elem: types.KindI32, Class: types.ClassAny}, ssa.OpMapDelete, types.KindRef, 0, false},
		{"array shape", ssa.Shape{Form: ssa.FormArray, Elem: types.KindI32}, ssa.OpMapLen, 0, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.served, tt.shape.Serves(tt.code, tt.key, tt.value))
		})
	}
}

func TestOp_String(t *testing.T) {
	for _, tt := range []struct {
		op   ssa.Op
		want string
	}{
		{ssa.OpConst, "const"}, {ssa.OpExec, "exec"}, {ssa.OpLoad, "load"}, {ssa.OpStore, "store"},
		{ssa.OpGuardKind, "guard.kind"}, {ssa.OpGuardShape, "guard.shape"}, {ssa.OpGuardBounds, "guard.bounds"}, {ssa.OpGuardValue, "guard.value"},
		{ssa.OpRefRetain, "ref.retain"}, {ssa.OpRefRelease, "ref.release"}, {ssa.OpState, "state"},
		{ssa.OpArrayView, "array.view"}, {ssa.OpArrayBound, "array.bound"}, {ssa.OpSelect, "select"}, {ssa.OpCall, "call"},
		{ssa.OpIntSAddOvf, "int.sadd_ovf"}, {ssa.OpIntSSubOvf, "int.ssub_ovf"}, {ssa.OpIntSMulOvf, "int.smul_ovf"}, {ssa.OpIntUAddOvf, "int.uadd_ovf"}, {ssa.OpIntUSubOvf, "int.usub_ovf"}, {ssa.OpIntUMulOvf, "int.umul_ovf"}, {ssa.OpIntAdd, "int.add"}, {ssa.OpIntSub, "int.sub"}, {ssa.OpIntMul, "int.mul"}, {ssa.OpIntSDiv, "int.sdiv"},
		{ssa.OpIntUDiv, "int.udiv"}, {ssa.OpIntSRem, "int.srem"}, {ssa.OpIntURem, "int.urem"}, {ssa.OpIntAnd, "int.and"},
		{ssa.OpIntOr, "int.or"}, {ssa.OpIntXor, "int.xor"}, {ssa.OpIntShl, "int.shl"}, {ssa.OpIntAShr, "int.ashr"},
		{ssa.OpIntLShr, "int.lshr"}, {ssa.OpIntRotl, "int.rotl"}, {ssa.OpIntRotr, "int.rotr"}, {ssa.OpIntClz, "int.clz"},
		{ssa.OpIntCtz, "int.ctz"}, {ssa.OpIntPopcnt, "int.popcnt"}, {ssa.OpIntSExt8, "int.sext8"}, {ssa.OpIntSExt16, "int.sext16"},
		{ssa.OpIntSExt32, "int.sext32"}, {ssa.OpIntCmp, "int.cmp"}, {ssa.OpIntSExt, "int.sext"}, {ssa.OpIntZExt, "int.zext"},
		{ssa.OpIntWrap, "int.wrap"}, {ssa.OpIntSIToFP, "int.sitofp"}, {ssa.OpIntUIToFP, "int.uitofp"}, {ssa.OpIntBitcast, "int.bitcast"},
		{ssa.OpFloatAdd, "float.add"}, {ssa.OpFloatSub, "float.sub"}, {ssa.OpFloatMul, "float.mul"}, {ssa.OpFloatDiv, "float.div"},
		{ssa.OpFloatMin, "float.min"}, {ssa.OpFloatMax, "float.max"}, {ssa.OpFloatCopysign, "float.copysign"}, {ssa.OpFloatNeg, "float.neg"},
		{ssa.OpFloatAbs, "float.abs"}, {ssa.OpFloatSqrt, "float.sqrt"}, {ssa.OpFloatCeil, "float.ceil"}, {ssa.OpFloatFloor, "float.floor"},
		{ssa.OpFloatTrunc, "float.trunc"}, {ssa.OpFloatNearest, "float.nearest"}, {ssa.OpFloatCmp, "float.cmp"},
		{ssa.OpFloatFPExt, "float.fpext"}, {ssa.OpFloatFPTrunc, "float.fptrunc"}, {ssa.OpFloatFPToSISat, "float.fptosi.sat"},
		{ssa.OpFloatFPToUISat, "float.fptoui.sat"}, {ssa.OpFloatBitcast, "float.bitcast"},
		{ssa.OpRefCmp, "ref.cmp"}, {ssa.OpRefIsNull, "ref.is_null"}, {ssa.OpRefTest, "ref.test"}, {ssa.OpRefCast, "ref.cast"}, {ssa.OpStringCmp, "string.cmp"}, {ssa.OpStringLen, "string.len"},
		{ssa.OpArrayNewDefault, "array.new_default"}, {ssa.OpArrayGet, "array.get"}, {ssa.OpArraySet, "array.set"}, {ssa.OpArrayLen, "array.len"},
		{ssa.OpStructNewDefault, "struct.new_default"}, {ssa.OpStructGet, "struct.get"}, {ssa.OpStructSet, "struct.set"},
		{ssa.OpMapLen, "map.len"}, {ssa.OpMapGet, "map.get"}, {ssa.OpMapLookup, "map.lookup"}, {ssa.OpMapSet, "map.set"}, {ssa.OpMapDelete, "map.delete"},
		{ssa.OpMapDelete + 1, "invalid"},
	} {
		t.Run("names "+tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.op.String())
		})
	}
}

func TestTerm_String(t *testing.T) {
	for _, tt := range []struct {
		term ssa.Term
		want string
	}{
		{ssa.TermJump, "jump"}, {ssa.TermBrIf, "brif"}, {ssa.TermBrTable, "br_table"},
		{ssa.TermReturn, "return"}, {ssa.TermComplete, "complete"}, {ssa.TermExit, "exit"},
		{ssa.TermExit + 1, "invalid"},
	} {
		t.Run("names "+tt.want, func(t *testing.T) {
			require.Equal(t, tt.want, tt.term.String())
		})
	}
}

func TestCond_String(t *testing.T) {
	for cond, want := range map[ssa.Cond]string{
		ssa.CondEQ: "eq", ssa.CondNE: "ne", ssa.CondSLT: "slt", ssa.CondSLE: "sle", ssa.CondSGT: "sgt", ssa.CondSGE: "sge",
		ssa.CondULT: "ult", ssa.CondULE: "ule", ssa.CondUGT: "ugt", ssa.CondUGE: "uge",
		ssa.CondOEQ: "oeq", ssa.CondONE: "one", ssa.CondOLT: "olt", ssa.CondOLE: "ole", ssa.CondOGT: "ogt", ssa.CondOGE: "oge",
		ssa.CondUEQ: "ueq", ssa.CondUNE: "une", ssa.CondUNLT: "ult", ssa.CondUNLE: "ule", ssa.CondUNGT: "ugt", ssa.CondUNGE: "uge",
		0: "invalid", ssa.CondUNGE + 1: "invalid",
	} {
		t.Run("names "+want, func(t *testing.T) {
			require.Equal(t, want, cond.String())
		})
	}
}

func TestForm_String(t *testing.T) {
	for form, want := range map[ssa.Form]string{
		ssa.FormNone: "none", ssa.FormArray: "array", ssa.FormMap: "map", ssa.FormStruct: "struct",
		ssa.FormClosure: "closure", ssa.FormSignature: "signature", ssa.FormSignature + 1: "invalid",
	} {
		t.Run("names "+want, func(t *testing.T) {
			require.Equal(t, want, form.String())
		})
	}
}

func TestSpace_String(t *testing.T) {
	for _, tt := range []struct {
		name  string
		space ssa.Space
		want  string
	}{
		{"local", ssa.SpaceLocal, "local"},
		{"global", ssa.SpaceGlobal, "global"},
		{"upval", ssa.SpaceUpval, "upval"},
		{"unknown space", ssa.SpaceUpval + 1, "invalid"},
	} {
		t.Run("names "+tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, tt.space.String())
		})
	}
}

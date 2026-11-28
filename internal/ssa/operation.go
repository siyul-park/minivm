package ssa

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Operation is one SSA instruction.
type Operation struct {
	// Op identifies the IR operation.
	Op Op
	// Cond is the condition of a compare operation.
	Cond Cond
	// Code identifies the guest opcode for OpExec.
	Code instr.Opcode
	// Slot identifies storage for OpLoad/OpStore.
	Slot Slot
	// Const is the value for OpConst: the result type's native word (i1 0/1,
	// i8/i32/f32 a zero-extended 32-bit lane, i64/f64 the full 64 bits, ref a
	// types.Boxed word).
	Const uint64
	// Shape is the admitted specialization.
	Shape Shape
	// Frames is the deoptimization frame chain.
	Frames []Frame

	// Args are operation inputs.
	Args []Value
	// State identifies the resume state.
	State Value
	// Source is the opaque front-end source id of the bytecode instruction the
	// operation came from, 0 for none (see instr.Position).
	Source uint32
	// Results are operation outputs.
	Results []Value
}

// Terminator ends a block and selects successors.
type Terminator struct {
	// Term identifies the terminator.
	Term Term
	// Args are values passed to the successor.
	Args []Value
	// State identifies the resume state.
	State Value
	// Source is the opaque front-end source id of the bytecode instruction the
	// terminator came from, 0 for none (see instr.Position).
	Source uint32
	// Edges identify successor blocks and arguments.
	Edges []Edge
}

// Edge names a successor and its block arguments.
type Edge struct {
	// Block is the successor id.
	Block int
	// Args are the values bound to the successor's params.
	Args []Value
}

// Op identifies an SSA operation.
type Op uint8

// Term identifies a block terminator.
type Term uint8

// Cond identifies the relation a compare operation tests; the zero Cond is
// none.
type Cond uint8

// Effect is a set of the machine parts an operation touches beyond its
// operands and results. Its bits mirror instr.Effect.
type Effect uint8

// Slot identifies one interpreter storage location.
type Slot struct {
	// Space identifies the storage class.
	Space Space
	// Base identifies the frame base.
	Base int
	// Index identifies the slot.
	Index int
}

// Shape describes the specialization a guard admits, a call carries, or a
// container allocation names. Form selects which fields apply: FormArray an
// array whose elements are of Elem in its canonical representation
// (types.TypedArray[T] for a scalar kind, *types.Array for KindRef);
// FormMap a *types.Map of key kind Key and element kind Elem;
// FormStruct a *types.Struct of struct type Type; FormClosure a
// *types.Closure over Function of function type Type holding at least
// Captures upvals. An array.new_default or struct.new_default names the
// declared type it allocates by a FormArray or FormStruct. A call whose
// callee is unknown carries FormSignature: Type alone, the function type
// every callee shares. A ref.test or ref.cast names the type it checks by
// Index and that type's kind by Elem. The zero Shape is none.
type Shape struct {
	// Form identifies the admitted representation.
	Form Form
	// Key is the key kind of a FormMap.
	Key types.Kind
	// Elem is the element kind of a FormArray or FormMap.
	Elem types.Kind
	// Type identifies the struct type of a FormStruct or the function type
	// of a FormClosure or FormSignature.
	Type uintptr
	// Function is the address of the function a FormClosure calls.
	Function int
	// Captures is the upvals count a FormClosure holds at least.
	Captures int
	// Index is the index of the declared type in the program's type table,
	// for the FormArray or FormStruct an allocation names.
	Index int
}

// Form identifies the representation a Shape admits.
type Form uint8

// Frame materializes one interpreter frame for deoptimization.
type Frame struct {
	// Address identifies the function slot.
	Address int
	// Base is the frame base offset.
	Base int
	// IP is the resume offset.
	IP int
	// Returns is the result count.
	Returns int
	// Stack is the frame operand stack.
	Stack []Operand
	// Locals are promoted slot values, saved only for deoptimization.
	Locals []Local
}

// Local is a promoted frame slot value.
type Local struct {
	// Index identifies the slot.
	Index int
	// Value is written back to the slot on deoptimization.
	Value Value
}

// Operand is one deoptimization stack entry.
type Operand struct {
	// Value is the stack value.
	Value Value
	// Owned reports whether the entry carries its reference count.
	Owned bool
}

// Space identifies interpreter storage.
type Space uint8

// effect is what a modelled operation touches; OpExec answers from its opcode.
type effect struct {
	reads, writes Effect
	traps         bool
}

const (
	// OpConst yields its Const word.
	OpConst Op = iota
	// OpExec performs the guest opcode Code, which no other operation models.
	OpExec
	OpLoad
	OpStore
	OpGuardKind
	OpGuardShape
	// OpGuardBounds deopts unless its first argument is at most its second,
	// signed: the bound a loop keeps its indexes below fits an array length.
	OpGuardBounds
	OpGuardValue
	OpRefRetain
	OpRefRelease
	OpState
	// OpArrayView is its argument, a guarded array, whose element pointer and
	// length array ops naming the result read as of the view: nothing
	// between them may resize or replace the array.
	OpArrayView
	// OpArrayBound is its first argument, an index proven within [0, length) of
	// the view its second argument names; array ops indexed by the result
	// check no bounds.
	OpArrayBound

	OpSelect
	OpCall

	OpIntAdd
	OpIntSub
	OpIntMul
	OpIntSDiv
	OpIntUDiv
	OpIntSRem
	OpIntURem
	OpIntAnd
	OpIntOr
	OpIntXor
	OpIntShl
	OpIntAShr
	OpIntLShr
	OpIntRotl
	OpIntRotr
	OpIntClz
	OpIntCtz
	OpIntPopcnt
	OpIntSExt8
	OpIntSExt16
	OpIntSExt32
	OpIntCmp
	OpIntSExt
	OpIntZExt
	OpIntWrap
	OpIntSIToFP
	OpIntUIToFP
	OpIntBitcast
	// OpIntSAddOvf, OpIntSSubOvf, and OpIntSMulOvf yield the wrapped result of
	// their two lanes and an i1 set when the exact signed result does not fit;
	// OpIntUAddOvf, OpIntUSubOvf, and OpIntUMulOvf the same for the unsigned.
	OpIntSAddOvf
	OpIntSSubOvf
	OpIntSMulOvf
	OpIntUAddOvf
	OpIntUSubOvf
	OpIntUMulOvf

	OpFloatAdd
	OpFloatSub
	OpFloatMul
	OpFloatDiv
	OpFloatMin
	OpFloatMax
	OpFloatCopysign
	OpFloatNeg
	OpFloatAbs
	OpFloatSqrt
	OpFloatCeil
	OpFloatFloor
	OpFloatTrunc
	OpFloatNearest
	OpFloatCmp
	OpFloatFPExt
	OpFloatFPTrunc
	OpFloatFPToSISat
	OpFloatFPToUISat
	OpFloatBitcast

	OpRefCmp
	OpRefIsNull
	OpRefTest
	OpRefCast

	OpStringCmp
	OpStringLen

	OpArrayNewDefault
	OpArrayGet
	OpArraySet
	OpArrayLen

	OpStructNewDefault
	OpStructGet
	OpStructSet

	OpMapLen
	OpMapGet
	OpMapLookup
	OpMapSet
	OpMapDelete
)

const (
	TermJump Term = iota
	TermBrIf
	TermBrTable
	TermReturn
	TermComplete
	TermExit
)

const (
	// CondEQ and the following integer conditions compare lanes; the S
	// conditions read them signed and the U conditions unsigned.
	CondEQ Cond = iota + 1
	CondNE
	CondSLT
	CondSLE
	CondSGT
	CondSGE
	CondULT
	CondULE
	CondUGT
	CondUGE

	// CondOEQ and the following float conditions are ordered: false when
	// either operand is NaN.
	CondOEQ
	CondONE
	CondOLT
	CondOLE
	CondOGT
	CondOGE
	// CondUEQ and the following float conditions are unordered: true when
	// either operand is NaN. They exist so every condition has an inverse.
	CondUEQ
	CondUNE
	CondUNLT
	CondUNLE
	CondUNGT
	CondUNGE
)

const (
	FormNone Form = iota
	FormArray
	FormMap
	FormStruct
	FormClosure
	FormSignature
)

const (
	// EffectLocal is the running frame's local slots.
	EffectLocal Effect = 1 << iota
	// EffectGlobal is the module's globals.
	EffectGlobal
	// EffectUpval is the running closure's captured upvalues.
	EffectUpval
	// EffectHeap is the contents of a heap container; allocating one writes it.
	EffectHeap
	// EffectFrame is the call frame chain: a call pushes a frame.
	EffectFrame
	// EffectBranch is the instruction pointer, moved other than by falling
	// through.
	EffectBranch
)

const (
	SpaceLocal Space = iota
	SpaceGlobal
	SpaceUpval
)

var opNames = [...]string{
	OpConst: "const", OpExec: "exec", OpLoad: "load", OpStore: "store",
	OpGuardKind: "guard.kind", OpGuardShape: "guard.shape", OpGuardBounds: "guard.bounds", OpGuardValue: "guard.value",
	OpRefRetain: "ref.retain", OpRefRelease: "ref.release", OpState: "state",
	OpArrayView: "array.view", OpArrayBound: "array.bound",
	OpSelect: "select", OpCall: "call",
	OpIntAdd: "int.add", OpIntSub: "int.sub", OpIntMul: "int.mul", OpIntSDiv: "int.sdiv", OpIntUDiv: "int.udiv",
	OpIntSRem: "int.srem", OpIntURem: "int.urem", OpIntAnd: "int.and", OpIntOr: "int.or", OpIntXor: "int.xor",
	OpIntShl: "int.shl", OpIntAShr: "int.ashr", OpIntLShr: "int.lshr", OpIntRotl: "int.rotl", OpIntRotr: "int.rotr",
	OpIntClz: "int.clz", OpIntCtz: "int.ctz", OpIntPopcnt: "int.popcnt",
	OpIntSExt8: "int.sext8", OpIntSExt16: "int.sext16", OpIntSExt32: "int.sext32",
	OpIntCmp: "int.cmp", OpIntSExt: "int.sext", OpIntZExt: "int.zext", OpIntWrap: "int.wrap",
	OpIntSIToFP: "int.sitofp", OpIntUIToFP: "int.uitofp", OpIntBitcast: "int.bitcast",
	OpIntSAddOvf: "int.sadd_ovf", OpIntSSubOvf: "int.ssub_ovf", OpIntSMulOvf: "int.smul_ovf",
	OpIntUAddOvf: "int.uadd_ovf", OpIntUSubOvf: "int.usub_ovf", OpIntUMulOvf: "int.umul_ovf",
	OpFloatAdd: "float.add", OpFloatSub: "float.sub", OpFloatMul: "float.mul", OpFloatDiv: "float.div",
	OpFloatMin: "float.min", OpFloatMax: "float.max", OpFloatCopysign: "float.copysign",
	OpFloatNeg: "float.neg", OpFloatAbs: "float.abs", OpFloatSqrt: "float.sqrt",
	OpFloatCeil: "float.ceil", OpFloatFloor: "float.floor", OpFloatTrunc: "float.trunc", OpFloatNearest: "float.nearest",
	OpFloatCmp: "float.cmp", OpFloatFPExt: "float.fpext", OpFloatFPTrunc: "float.fptrunc",
	OpFloatFPToSISat: "float.fptosi.sat", OpFloatFPToUISat: "float.fptoui.sat", OpFloatBitcast: "float.bitcast",
	OpRefCmp: "ref.cmp", OpRefIsNull: "ref.is_null", OpRefTest: "ref.test", OpRefCast: "ref.cast",
	OpStringCmp: "string.cmp", OpStringLen: "string.len",
	OpArrayNewDefault: "array.new_default", OpArrayGet: "array.get", OpArraySet: "array.set", OpArrayLen: "array.len",
	OpStructNewDefault: "struct.new_default", OpStructGet: "struct.get", OpStructSet: "struct.set",
	OpMapLen: "map.len", OpMapGet: "map.get", OpMapLookup: "map.lookup", OpMapSet: "map.set", OpMapDelete: "map.delete",
}

var termNames = [...]string{
	TermJump: "jump", TermBrIf: "brif", TermBrTable: "br_table",
	TermReturn: "return", TermComplete: "complete", TermExit: "exit",
}

var condNames = [...]string{
	CondEQ: "eq", CondNE: "ne", CondSLT: "slt", CondSLE: "sle", CondSGT: "sgt", CondSGE: "sge",
	CondULT: "ult", CondULE: "ule", CondUGT: "ugt", CondUGE: "uge",
	CondOEQ: "oeq", CondONE: "one", CondOLT: "olt", CondOLE: "ole", CondOGT: "ogt", CondOGE: "oge",
	CondUEQ: "ueq", CondUNE: "une", CondUNLT: "ult", CondUNLE: "ule", CondUNGT: "ugt", CondUNGE: "uge",
}

// inverses maps each condition to the one true exactly when it is false,
// NaN operands included.
var inverses = [...]Cond{
	CondEQ: CondNE, CondNE: CondEQ,
	CondSLT: CondSGE, CondSGE: CondSLT, CondSLE: CondSGT, CondSGT: CondSLE,
	CondULT: CondUGE, CondUGE: CondULT, CondULE: CondUGT, CondUGT: CondULE,
	CondOEQ: CondUNE, CondUNE: CondOEQ, CondONE: CondUEQ, CondUEQ: CondONE,
	CondOLT: CondUNGE, CondUNGE: CondOLT, CondOLE: CondUNGT, CondUNGT: CondOLE,
	CondOGT: CondUNLE, CondUNLE: CondOGT, CondOGE: CondUNLT, CondUNLT: CondOGE,
}

var effects = map[Op]effect{
	OpIntSDiv: {traps: true}, OpIntUDiv: {traps: true}, OpIntSRem: {traps: true}, OpIntURem: {traps: true},
	OpCall:             {reads: EffectGlobal | EffectUpval | EffectHeap, writes: EffectGlobal | EffectUpval | EffectHeap | EffectFrame | EffectBranch},
	OpRefTest:          {reads: EffectHeap},
	OpRefCast:          {reads: EffectHeap, traps: true},
	OpStringCmp:        {reads: EffectHeap},
	OpStringLen:        {reads: EffectHeap},
	OpArrayNewDefault:  {writes: EffectHeap},
	OpArrayGet:         {reads: EffectHeap},
	OpArraySet:         {reads: EffectHeap, writes: EffectHeap},
	OpArrayLen:         {reads: EffectHeap},
	OpStructNewDefault: {writes: EffectHeap},
	OpStructGet:        {reads: EffectHeap},
	OpStructSet:        {reads: EffectHeap, writes: EffectHeap},
	OpMapLen:           {reads: EffectHeap},
	OpMapGet:           {reads: EffectHeap},
	OpMapLookup:        {reads: EffectHeap},
	OpMapSet:           {reads: EffectHeap, writes: EffectHeap},
	OpMapDelete:        {reads: EffectHeap, writes: EffectHeap},
}

// Reads returns the machine parts o reads. OpLoad and OpStore touch the
// storage their Slot names, which Reads and Writes leave out.
func (o Operation) Reads() Effect {
	switch o.Op {
	case OpExec:
		return Effect(instr.TypeOf(o.Code).Reads)
	default:
		return effects[o.Op].reads
	}
}

// Writes returns the machine parts o writes.
func (o Operation) Writes() Effect {
	switch o.Op {
	case OpExec:
		return Effect(instr.TypeOf(o.Code).Writes)
	default:
		return effects[o.Op].writes
	}
}

// Traps reports whether o raises a trap for some operand values.
func (o Operation) Traps() bool {
	if o.Op == OpExec {
		return o.Code.Traps()
	}
	return effects[o.Op].traps
}

// Pure reports whether o computes its results from its operands alone: it
// touches no other part of the machine, yields a value, and observes no
// position, so two occurrences with equal operands compute equal results. A
// trapping operation can be pure; Traps tells whether it may be removed.
func (o Operation) Pure() bool {
	switch o.Op {
	case OpExec:
		return o.Code.IsPure()
	case OpGuardKind, OpGuardShape, OpGuardBounds, OpGuardValue, OpRefRetain, OpRefRelease, OpState, OpLoad, OpStore, OpArrayView, OpArrayBound:
		return false
	default:
		return len(o.Results) > 0 && o.Reads() == 0 && o.Writes() == 0
	}
}

// Data reports whether o is a data operation: OpSelect, OpCall, or one of the
// int, float, ref, string, array, struct, or map operations that mirror a guest
// opcode. OpExec performs the opcodes none of them models.
func (o Op) Data() bool {
	return o >= OpSelect && o <= OpMapDelete
}

// Invert returns the condition true exactly when c is false.
func (c Cond) Invert() Cond {
	if int(c) >= len(inverses) || inverses[c] == 0 {
		return c
	}
	return inverses[c]
}

// Serves reports whether native code lowers op on a map this shape admits,
// given the kinds of its key and value operands (ignored by an operation without
// them): the key kind is an integer, f32, f64, or reference held in its key
// kind's register class (a reference key is a string's), or any, which takes an
// operand of every kind, and an element or value fits one register, which
// excludes an i64, whose boxed word may be a reference to a heap-promoted value.
// Translation guards a map only for an operation Serves accepts, and the
// lowering lowers exactly those.
func (s Shape) Serves(op Op, key, value types.Kind) bool {
	if s.Form != FormMap {
		return false
	}
	switch s.Key {
	case types.KindI1, types.KindI8, types.KindI32, types.KindI64, types.KindF32, types.KindF64, types.KindRef, types.KindAny:
	default:
		return false
	}
	if op == OpMapLen {
		return true
	}
	switch s.Key {
	case types.KindAny:
	case types.KindI1, types.KindI8, types.KindI32:
		if key != types.KindI1 && key != types.KindI8 && key != types.KindI32 {
			return false
		}
	default:
		if key != s.Key {
			return false
		}
	}
	var held types.Kind
	switch op {
	case OpMapGet, OpMapLookup:
		held = s.Elem
	case OpMapSet:
		held = value
	default:
		return true
	}
	switch held {
	case types.KindI1, types.KindI8, types.KindI32, types.KindF32, types.KindF64, types.KindRef:
		return true
	}
	return false
}

// String returns the operation name.
func (o Op) String() string {
	if int(o) >= len(opNames) || opNames[o] == "" {
		return "invalid"
	}
	return opNames[o]
}

// String returns the terminator name.
func (t Term) String() string {
	if int(t) >= len(termNames) {
		return "invalid"
	}
	return termNames[t]
}

// String returns the condition name.
func (c Cond) String() string {
	if int(c) >= len(condNames) || condNames[c] == "" {
		return "invalid"
	}
	return condNames[c]
}

// String returns the form name.
func (f Form) String() string {
	switch f {
	case FormNone:
		return "none"
	case FormArray:
		return "array"
	case FormMap:
		return "map"
	case FormStruct:
		return "struct"
	case FormClosure:
		return "closure"
	case FormSignature:
		return "signature"
	default:
		return "invalid"
	}
}

// String returns the storage name.
func (s Space) String() string {
	switch s {
	case SpaceLocal:
		return "local"
	case SpaceGlobal:
		return "global"
	case SpaceUpval:
		return "upval"
	default:
		return "invalid"
	}
}

// float reports whether c compares floats.
func (c Cond) float() bool {
	return c >= CondOEQ && c <= CondUNGE
}

func (o Operation) name() string {
	switch o.Op {
	case OpExec:
		return "exec " + instr.TypeOf(o.Code).Mnemonic
	case OpIntCmp, OpFloatCmp, OpRefCmp, OpStringCmp:
		return o.Op.String() + " " + o.Cond.String()
	default:
		return o.Op.String()
	}
}

package ssa

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
)

type position struct {
	block int
	index int
}

var (
	// ErrForm reports malformed SSA structure.
	ErrForm = errors.New("malformed function")
	// ErrDefine reports invalid value definitions.
	ErrDefine = errors.New("value not defined exactly once")
	// ErrDominate reports an undominated use.
	ErrDominate = errors.New("use not dominated by its definition")
	// ErrType reports a type mismatch.
	ErrType = errors.New("type mismatch")
	// ErrState reports invalid deoptimization state.
	ErrState = errors.New("interpreter state missing or malformed")
)

// Verify rejects malformed, undefined, non-dominated, or mistyped SSA.
func Verify(function *Function) error {
	if function == nil || function.Len() == 0 {
		return fmt.Errorf("%w: no entry block", ErrForm)
	}
	dominance := graph.NewDominance(function)
	for id := range function.Len() {
		if !dominance.Dominates(0, id) {
			return fmt.Errorf("%w: blk%d is unreachable from the entry", ErrForm, id)
		}
	}
	sites, err := define(function)
	if err != nil {
		return err
	}
	for id := range function.Len() {
		block := function.Block(id)
		if err := params(function, id); err != nil {
			return fmt.Errorf("blk%d: %w", id, err)
		}
		for i, o := range block.Operations {
			if err := operation(function, sites, o); err != nil {
				return fmt.Errorf("blk%d operation %d: %w", id, i, err)
			}
			if err := uses(function, sites, dominance, position{id, i}, reads(o)); err != nil {
				return fmt.Errorf("blk%d operation %d: %w", id, i, err)
			}
		}
		if err := terminator(function, sites, block.Terminator); err != nil {
			return fmt.Errorf("blk%d term: %w", id, err)
		}
		if err := uses(function, sites, dominance, position{id, len(block.Operations)}, ends(block.Terminator)); err != nil {
			return fmt.Errorf("blk%d term: %w", id, err)
		}
	}
	return nil
}

func define(function *Function) ([]position, error) {
	sites := make([]position, function.Values())
	for i := range sites {
		sites[i] = position{-1, -1}
	}
	claim := func(v Value, s position) error {
		if !v.valid(len(sites)) {
			return fmt.Errorf("%w: v%d is not a value of this function", ErrDefine, v)
		}
		if sites[v].block >= 0 {
			return fmt.Errorf("%w: v%d is defined twice", ErrDefine, v)
		}
		sites[v] = s
		return nil
	}
	for id := range function.Len() {
		block := function.Block(id)
		for _, v := range block.Params {
			if err := claim(v, position{id, -1}); err != nil {
				return nil, err
			}
		}
		for i, o := range block.Operations {
			for _, v := range o.Results {
				if err := claim(v, position{id, i}); err != nil {
					return nil, err
				}
			}
		}
	}
	for v := 1; v < len(sites); v++ {
		if sites[v].block < 0 {
			return nil, fmt.Errorf("%w: v%d is never defined", ErrDefine, v)
		}
	}
	return sites, nil
}

func params(function *Function, block int) error {
	want := function.Block(block).Params
	for _, pred := range function.Pred(block) {
		for _, edge := range function.Block(pred).Terminator.Edges {
			if edge.Block != block {
				continue
			}
			if len(edge.Args) != len(want) {
				return fmt.Errorf("%w: blk%d passes %d arguments to %d parameters", ErrForm, pred, len(edge.Args), len(want))
			}
			for i, arg := range edge.Args {
				if function.Type(arg) != function.Type(want[i]) {
					return fmt.Errorf("%w: blk%d passes %s to parameter v%d of type %s", ErrType, pred, function.Type(arg), want[i], function.Type(want[i]))
				}
			}
		}
	}
	return nil
}

func operation(function *Function, sites []position, o Operation) error {
	args, results := len(o.Args), len(o.Results)
	deopts := false
	if err := attributes(o); err != nil {
		return err
	}
	switch o.Op {
	case OpConst:
		if args != 0 || results != 1 {
			return counted(o.name(), args, results)
		}
		if err := canonical(function, o); err != nil {
			return err
		}
	case OpLoad:
		if args != 0 || results != 1 {
			return counted(o.name(), args, results)
		}
	case OpExec:
		if err := performs(function, o); err != nil {
			return err
		}
		deopts = true
	case OpStore:
		if args != 1 || results != 0 {
			return counted(o.name(), args, results)
		}
		deopts = true
	case OpGuardKind, OpGuardShape:
		if args != 1 || results != 1 {
			return counted(o.name(), args, results)
		}
		if o.Op == OpGuardShape && o.Shape.Form == FormSignature {
			return fmt.Errorf("%w: %s admits a call signature", ErrForm, o.name())
		}
		deopts = true
	case OpGuardBounds:
		if args != 2 || results != 0 {
			return counted(o.name(), args, results)
		}
		deopts = true
	case OpGuardValue:
		if args != 2 || results != 1 {
			return counted(o.name(), args, results)
		}
		if !constant(function, sites, o.Args[1]) {
			return fmt.Errorf("%w: %s admits v%d, which is no observation", ErrForm, o.name(), o.Args[1])
		}
		deopts = true
	case OpArrayView:
		if args != 1 || results != 1 {
			return counted(o.name(), args, results)
		}
	case OpArrayBound:
		if args != 2 || results != 1 {
			return counted(o.name(), args, results)
		}
	case OpRefRetain, OpRefRelease:
		if args != 1 || results != 0 {
			return counted(o.name(), args, results)
		}
		deopts = o.Op == OpRefRelease
	case OpState:
		if args != 0 || results != 1 {
			return counted(o.name(), args, results)
		}
		if len(o.Frames) == 0 {
			return fmt.Errorf("%w: %s carries no frame", ErrState, o.name())
		}
		for _, frame := range o.Frames {
			for _, operand := range frame.Stack {
				if operand.Owned && function.Type(operand.Value) != TypeRef {
					return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), function.Type(operand.Value))
				}
			}
			for _, local := range frame.Locals {
				if local.Index < 0 {
					return fmt.Errorf("%w: %s has local %d", ErrState, o.name(), local.Index)
				}
				if function.Type(local.Value) == TypeRef {
					return fmt.Errorf("%w: %s has %s local", ErrType, o.name(), TypeRef)
				}
			}
		}
	default:
		if !o.Op.Data() {
			return fmt.Errorf("%w: %s is not an operation", ErrForm, o.Op)
		}
		if err := modelled(function, o); err != nil {
			return err
		}
		deopts = true
	}
	if err := resume(function, sites, o.State, o.name(), deopts); err != nil {
		return err
	}
	return typed(function, o)
}

// attributes rejects a field set on an operation that does not use it.
func attributes(o Operation) error {
	used := func(ok bool, field string) error {
		if !ok {
			return fmt.Errorf("%w: %s carries a stray %s", ErrForm, o.name(), field)
		}
		return nil
	}
	compares := o.Op == OpIntCmp || o.Op == OpFloatCmp || o.Op == OpRefCmp || o.Op == OpStringCmp
	callee := o.Op == OpExec || o.Op == OpCall
	shaped := o.Op == OpGuardShape || o.Op == OpArrayNewDefault || o.Op == OpStructNewDefault
	for _, err := range []error{
		used(compares || o.Cond == 0, "condition"),
		used(o.Op == OpExec || o.Code == 0, "opcode"),
		used(o.Op == OpLoad || o.Op == OpStore || o.Slot == Slot{}, "slot"),
		used(o.Op == OpConst || o.Const == 0, "constant"),
		used(o.Op == OpState || len(o.Frames) == 0, "frame"),
		used(callee || shaped || o.Shape == Shape{}, "shape"),
	} {
		if err != nil {
			return err
		}
	}
	return nil
}

// modelled checks the operand and result types of a data operation, which
// every exit-capable translation of a guest opcode carries a State for.
// OpExec of an opcode one of these models is not yet rejected.
func modelled(function *Function, o Operation) error {
	arg := func(i int) Type { return function.Type(o.Args[i]) }
	res := func(i int) Type { return function.Type(o.Results[i]) }
	bad := func(why string) error {
		return fmt.Errorf("%w: %s %s", ErrType, o.name(), why)
	}
	in, out := 2, 1
	switch {
	case o.Op >= OpIntClz && o.Op <= OpIntSExt32, o.Op >= OpFloatNeg && o.Op <= OpFloatNearest,
		o.Op == OpIntSExt, o.Op == OpIntZExt, o.Op == OpIntWrap, o.Op == OpIntSIToFP, o.Op == OpIntUIToFP, o.Op == OpIntBitcast,
		o.Op >= OpFloatFPExt && o.Op <= OpFloatBitcast, o.Op == OpRefIsNull, o.Op == OpStringLen,
		o.Op == OpArrayNewDefault, o.Op == OpArrayLen, o.Op == OpMapLen:
		in = 1
	case o.Op == OpSelect:
		in = 3
	case o.Op == OpCall:
		in, out = -1, -1
	case o.Op == OpStructNewDefault:
		in = 0
	case o.Op == OpArraySet, o.Op == OpStructSet, o.Op == OpMapSet:
		in, out = 3, 0
	case o.Op == OpMapDelete:
		in, out = 2, 0
	case o.Op == OpMapLookup:
		out = 2
	}
	if in >= 0 && len(o.Args) != in || out >= 0 && len(o.Results) != out {
		return counted(o.name(), len(o.Args), len(o.Results))
	}
	isInt := func(t Type) bool { return t >= TypeI1 && t <= TypeI64 }
	isFloat := func(t Type) bool { return t == TypeF32 || t == TypeF64 }
	switch {
	case o.Op >= OpIntAdd && o.Op <= OpIntRotr:
		if !isInt(arg(0)) || arg(0).Lane() != arg(1).Lane() || !isInt(arg(1)) || !isInt(res(0)) || res(0).Lane() != arg(0).Lane() {
			return bad("takes lanes that differ or are not integers")
		}
	case o.Op >= OpIntClz && o.Op <= OpIntSExt32:
		if !isInt(arg(0)) || !isInt(res(0)) || res(0).Lane() != arg(0).Lane() {
			return bad("changes the lane of an integer")
		}
	case o.Op == OpIntCmp:
		if !isInt(arg(0)) || !isInt(arg(1)) || arg(0).Lane() != arg(1).Lane() || res(0) != TypeI1 {
			return bad("compares lanes that differ or are not integers into a non-i1")
		}
		if o.Cond == 0 || o.Cond > CondUGE {
			return bad("takes a float condition")
		}
	case o.Op == OpIntSExt, o.Op == OpIntZExt:
		if !isInt(arg(0)) || arg(0).Lane() != TypeI32 || res(0) != TypeI64 {
			return bad("extends a 32-bit lane into i64 only")
		}
	case o.Op == OpIntWrap:
		if arg(0) != TypeI64 || !isInt(res(0)) || res(0).Lane() != TypeI32 {
			return bad("wraps an i64 into a 32-bit lane only")
		}
	case o.Op == OpIntSIToFP, o.Op == OpIntUIToFP:
		if !isInt(arg(0)) || !isFloat(res(0)) {
			return bad("converts an integer into a float only")
		}
	case o.Op == OpIntBitcast:
		if !isFloat(arg(0)) || !isInt(res(0)) || !sized(arg(0), res(0)) {
			return bad("reinterprets a float as an integer of its width only")
		}
	case o.Op >= OpFloatAdd && o.Op <= OpFloatCopysign:
		if !isFloat(arg(0)) || arg(0) != arg(1) || res(0) != arg(0) {
			return bad("takes floats that differ or are not floats")
		}
	case o.Op >= OpFloatNeg && o.Op <= OpFloatNearest:
		if !isFloat(arg(0)) || res(0) != arg(0) {
			return bad("takes a non-float or changes its width")
		}
	case o.Op == OpFloatCmp:
		if !isFloat(arg(0)) || arg(0) != arg(1) || res(0) != TypeI1 {
			return bad("compares floats that differ or are not floats into a non-i1")
		}
		if !o.Cond.float() {
			return bad("takes an integer condition")
		}
	case o.Op == OpFloatFPExt:
		if arg(0) != TypeF32 || res(0) != TypeF64 {
			return bad("widens f32 into f64 only")
		}
	case o.Op == OpFloatFPTrunc:
		if arg(0) != TypeF64 || res(0) != TypeF32 {
			return bad("narrows f64 into f32 only")
		}
	case o.Op == OpFloatFPToSISat, o.Op == OpFloatFPToUISat:
		if !isFloat(arg(0)) || !isInt(res(0)) || res(0).Lane() == TypeI1 {
			return bad("converts a float into an integer only")
		}
	case o.Op == OpFloatBitcast:
		if !isInt(arg(0)) || !isFloat(res(0)) || !sized(res(0), arg(0)) {
			return bad("reinterprets an integer as a float of its width only")
		}
	case o.Op == OpSelect:
		if arg(2).Lane() != TypeI32 || arg(0) != arg(1) || res(0) != arg(0) {
			return bad("selects between differing types on a non-lane condition")
		}
	case o.Op == OpCall:
		if len(o.Args) == 0 || arg(len(o.Args)-1) != TypeRef {
			return bad("takes no callee")
		}
	case o.Op == OpRefCmp:
		if arg(0) != TypeRef || arg(1) != TypeRef || res(0) != TypeI1 {
			return bad("compares non-refs into a non-i1")
		}
		if o.Cond != CondEQ && o.Cond != CondNE {
			return bad("compares refs by identity only")
		}
	case o.Op == OpRefIsNull:
		if arg(0) != TypeRef || res(0) != TypeI1 {
			return bad("tests a non-ref into a non-i1")
		}
	case o.Op == OpStringCmp:
		if arg(0) != TypeRef || arg(1) != TypeRef || res(0) != TypeI1 {
			return bad("compares non-strings into a non-i1")
		}
		switch o.Cond {
		case CondEQ, CondNE, CondULT, CondULE, CondUGT, CondUGE:
		default:
			return bad("compares bytes by equality or unsigned order only")
		}
	case o.Op == OpStringLen, o.Op == OpArrayLen, o.Op == OpMapLen:
		if arg(0) != TypeRef || res(0).Lane() != TypeI32 {
			return bad("measures a non-ref into a non-i32")
		}
	case o.Op == OpArrayNewDefault:
		if arg(0).Lane() != TypeI32 || res(0) != TypeRef || o.Shape.Form != FormArray {
			return bad("allocates an array of a non-array shape or length")
		}
	case o.Op == OpStructNewDefault:
		if res(0) != TypeRef || o.Shape.Form != FormStruct {
			return bad("allocates a struct of a non-struct shape")
		}
	case o.Op == OpArrayGet, o.Op == OpArraySet, o.Op == OpStructGet, o.Op == OpStructSet:
		if arg(0) != TypeRef || arg(1).Lane() != TypeI32 {
			return bad("addresses a non-ref or by a non-i32 index")
		}
	case o.Op == OpMapGet, o.Op == OpMapLookup, o.Op == OpMapSet, o.Op == OpMapDelete:
		if arg(0) != TypeRef {
			return bad("addresses a non-ref")
		}
		if o.Op == OpMapLookup && res(1) != TypeI1 {
			return bad("reports presence as a non-i1")
		}
	}
	return nil
}

// sized reports whether float f and integer i fill the same register width.
func sized(f, i Type) bool {
	return f == TypeF32 && i.Lane() == TypeI32 || f == TypeF64 && i == TypeI64
}

func performs(function *Function, o Operation) error {
	if !instr.Valid(o.Code) {
		return fmt.Errorf("%w: %s performs no opcode", ErrForm, o.Op)
	}
	if leaves(o.Code) {
		return fmt.Errorf("%w: %s performs a terminator", ErrForm, o.name())
	}
	typ := instr.TypeOf(o.Code)
	if typ.Pop == nil && typ.Push == nil {
		if o.Code.Writes(instr.Frame) && len(o.Args) == 0 {
			return counted(o.name(), len(o.Args), len(o.Results))
		}
		return nil
	}
	if len(o.Args) != len(typ.Pop) || len(o.Results) != len(typ.Push) {
		return counted(o.name(), len(o.Args), len(o.Results))
	}
	for i, want := range typ.Pop {
		if got := function.Type(o.Args[len(o.Args)-1-i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pops %s where it wants %s", ErrType, o.name(), got, want)
		}
	}
	for i, want := range typ.Push {
		if got := function.Type(o.Results[i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pushes %s where it yields %s", ErrType, o.name(), got, want)
		}
	}
	if o.Code == instr.SELECT && (function.Type(o.Args[0]) != function.Type(o.Args[1]) || function.Type(o.Results[0]) != function.Type(o.Args[0])) {
		return fmt.Errorf("%w: %s selects between %s and %s", ErrType, o.name(), function.Type(o.Args[0]), function.Type(o.Args[1]))
	}
	return nil
}

func leaves(code instr.Opcode) bool {
	switch code {
	case instr.BR, instr.BR_IF, instr.BR_TABLE, instr.RETURN, instr.RETURN_CALL:
		return true
	default:
		return false
	}
}

func typed(function *Function, o Operation) error {
	for _, v := range o.Results {
		if function.Type(v) == 0 {
			return fmt.Errorf("%w: result v%d has no type", ErrType, v)
		}
		if (function.Type(v) == TypeState) != (o.Op == OpState) {
			return fmt.Errorf("%w: %s cannot produce %s", ErrType, o.name(), function.Type(v))
		}
	}
	for _, v := range o.Args {
		if function.Type(v) == TypeState {
			return fmt.Errorf("%w: %s cannot consume %s", ErrType, o.name(), TypeState)
		}
	}
	switch o.Op {
	case OpGuardShape, OpGuardValue:
		if function.Type(o.Results[0]) != function.Type(o.Args[0]) {
			return fmt.Errorf("%w: %s refines %s into %s", ErrType, o.name(), function.Type(o.Args[0]), function.Type(o.Results[0]))
		}
	case OpArrayView:
		if function.Type(o.Args[0]) != TypeRef || function.Type(o.Results[0]) != TypeRef {
			return fmt.Errorf("%w: %s of %s", ErrType, o.name(), function.Type(o.Args[0]))
		}
	case OpArrayBound:
		if function.Type(o.Args[0]) != TypeI32 || function.Type(o.Args[1]) != TypeRef || function.Type(o.Results[0]) != TypeI32 {
			return fmt.Errorf("%w: %s of %s within %s", ErrType, o.name(), function.Type(o.Args[0]), function.Type(o.Args[1]))
		}
	case OpGuardBounds:
		if function.Type(o.Args[0]) != TypeI32 || function.Type(o.Args[1]) != TypeI32 {
			return fmt.Errorf("%w: %s of %s and %s", ErrType, o.name(), function.Type(o.Args[0]), function.Type(o.Args[1]))
		}
	case OpRefRetain, OpRefRelease:
		if function.Type(o.Args[0]) != TypeRef {
			return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), function.Type(o.Args[0]))
		}
	}
	return nil
}

func terminator(function *Function, sites []position, t Terminator) error {
	args, edges := len(t.Args), len(t.Edges)
	deopts := false
	switch t.Term {
	case TermJump:
		if args != 0 || edges != 1 {
			return counted(t.Term.String(), args, edges)
		}
	case TermBrIf:
		if args != 1 || edges != 2 {
			return counted(t.Term.String(), args, edges)
		}
	case TermBrTable:
		if args != 1 || edges == 0 {
			return counted(t.Term.String(), args, edges)
		}
	case TermReturn, TermComplete:
		if edges != 0 {
			return counted(t.Term.String(), args, edges)
		}
		deopts = t.State != NoValue
	case TermExit:
		if args != 0 || edges != 0 {
			return counted(t.Term.String(), args, edges)
		}
		deopts = true
	default:
		return fmt.Errorf("%w: %s is not a terminator", ErrForm, t.Term)
	}
	for _, edge := range t.Edges {
		if edge.Block < 0 || edge.Block >= function.Len() {
			return fmt.Errorf("%w: %s names blk%d", ErrForm, t.Term, edge.Block)
		}
	}
	return resume(function, sites, t.State, t.Term.String(), deopts)
}

func resume(function *Function, sites []position, v Value, name string, deopts bool) error {
	if !deopts {
		if v != NoValue {
			return fmt.Errorf("%w: %s cannot resume into v%d", ErrState, name, v)
		}
		return nil
	}
	if !v.valid(len(sites)) || function.Type(v) != TypeState {
		return fmt.Errorf("%w: %s resumes into v%d", ErrState, name, v)
	}
	def := sites[v]
	if def.index < 0 || function.Block(def.block).Operations[def.index].Op != OpState {
		return fmt.Errorf("%w: v%d is not a state", ErrState, v)
	}
	return nil
}

// canonical rejects an OpConst whose result type's 32-bit lane has a nonzero
// upper half, or whose i1 result is not 0 or 1.
func canonical(function *Function, o Operation) error {
	switch t := function.Type(o.Results[0]); t {
	case TypeI1:
		if o.Const > 1 {
			return fmt.Errorf("%w: i1 const %d is not 0 or 1", ErrType, o.Const)
		}
	case TypeI8, TypeI32, TypeF32:
		if o.Const>>32 != 0 {
			return fmt.Errorf("%w: %s const 0x%x has a nonzero upper half", ErrType, t, o.Const)
		}
	}
	return nil
}

func constant(function *Function, sites []position, v Value) bool {
	if !v.valid(len(sites)) {
		return false
	}
	def := sites[v]
	return def.index >= 0 && function.Block(def.block).Operations[def.index].Op == OpConst
}

func uses(function *Function, sites []position, dominance *graph.Dominance, at position, values []Value) error {
	for _, v := range values {
		if !v.valid(len(sites)) {
			return fmt.Errorf("%w: v%d is not a value of this function", ErrDefine, v)
		}
		def := sites[v]
		if def.block == at.block {
			if def.index >= at.index {
				return fmt.Errorf("%w: v%d", ErrDominate, v)
			}
			continue
		}
		if !dominance.Dominates(def.block, at.block) {
			return fmt.Errorf("%w: v%d", ErrDominate, v)
		}
	}
	return nil
}

func reads(o Operation) []Value {
	values := append([]Value(nil), o.Args...)
	for _, frame := range o.Frames {
		for _, operand := range frame.Stack {
			values = append(values, operand.Value)
		}
		for _, local := range frame.Locals {
			values = append(values, local.Value)
		}
	}
	if o.State != NoValue {
		values = append(values, o.State)
	}
	return values
}

func ends(t Terminator) []Value {
	values := append([]Value(nil), t.Args...)
	for _, edge := range t.Edges {
		values = append(values, edge.Args...)
	}
	if t.State != NoValue {
		values = append(values, t.State)
	}
	return values
}

func accepts(t Type, k instr.Kind) bool {
	return k == instr.KindAny || TypeOf(k).Lane() == t.Lane()
}

func counted(name string, in, out int) error {
	return fmt.Errorf("%w: %s takes %d and yields %d", ErrForm, name, in, out)
}

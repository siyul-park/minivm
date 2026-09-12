package ssa

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
)

// site is where a value is defined: the block holding its definition and the
// position within it, -1 for a block parameter and otherwise the index of the
// defining operation.
type site struct {
	block int
	index int
}

var (
	// ErrForm reports a structural defect: no entry block, an unreachable
	// block, an operand, result, or edge count an operation cannot have, an
	// operation used where it is not one, an undefined opcode, or an edge
	// naming a block that does not exist.
	ErrForm = errors.New("malformed function")
	// ErrDefine reports a value that is not defined exactly once.
	ErrDefine = errors.New("value not defined exactly once")
	// ErrDominate reports a use its definition does not dominate.
	ErrDominate = errors.New("use not dominated by its definition")
	// ErrType reports disagreeing types.
	ErrType = errors.New("type mismatch")
	// ErrState reports an interpreter state that is missing, malformed, or
	// named by an operation that cannot deoptimize.
	ErrState = errors.New("interpreter state missing or malformed")
)

// Verify reports the first defect that would make f unsound to lower: a
// structural one, a value defined other than once, a use its definition does
// not dominate, a block parameter its predecessors disagree with, or a
// deoptimizing operation with no interpreter state to resume into.
func Verify(f *Function) error {
	if f == nil || len(f.blocks) == 0 {
		return fmt.Errorf("%w: no entry block", ErrForm)
	}
	dom := graph.NewDominance(f)
	for id := range f.blocks {
		if !dom.Dominates(0, id) {
			return fmt.Errorf("%w: blk%d is unreachable from the entry", ErrForm, id)
		}
	}
	sites, err := define(f)
	if err != nil {
		return err
	}
	for id, block := range f.blocks {
		if err := params(f, id); err != nil {
			return fmt.Errorf("blk%d: %w", id, err)
		}
		for i, op := range block.Ops {
			if err := operation(f, sites, op); err != nil {
				return fmt.Errorf("blk%d op %d: %w", id, i, err)
			}
			if err := uses(f, sites, dom, site{id, i}, reads(op)); err != nil {
				return fmt.Errorf("blk%d op %d: %w", id, i, err)
			}
		}
		if err := terminator(f, sites, block.Term); err != nil {
			return fmt.Errorf("blk%d term: %w", id, err)
		}
		if err := uses(f, sites, dom, site{id, len(block.Ops)}, ends(block.Term)); err != nil {
			return fmt.Errorf("blk%d term: %w", id, err)
		}
	}
	return nil
}

// define records where each value is defined and rejects one defined twice,
// never, or outside f's value range.
func define(f *Function) ([]site, error) {
	sites := make([]site, len(f.types))
	for i := range sites {
		sites[i] = site{-1, -1}
	}
	claim := func(v Value, s site) error {
		if v <= NoValue || int(v) >= len(sites) {
			return fmt.Errorf("%w: v%d is not a value of this function", ErrDefine, v)
		}
		if sites[v].block >= 0 {
			return fmt.Errorf("%w: v%d is defined twice", ErrDefine, v)
		}
		sites[v] = s
		return nil
	}
	for id, block := range f.blocks {
		for _, v := range block.Params {
			if err := claim(v, site{id, -1}); err != nil {
				return nil, err
			}
		}
		for i, op := range block.Ops {
			for _, v := range op.Results {
				if err := claim(v, site{id, i}); err != nil {
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

// params checks that every edge into block hands its parameters one argument
// each, of the parameter's own type.
func params(f *Function, block int) error {
	want := f.blocks[block].Params
	for _, pred := range f.preds[block] {
		for _, edge := range f.blocks[pred].Term.Edges {
			if edge.Block != block {
				continue
			}
			if len(edge.Args) != len(want) {
				return fmt.Errorf("%w: blk%d passes %d arguments to %d parameters", ErrForm, pred, len(edge.Args), len(want))
			}
			for i, arg := range edge.Args {
				if f.Type(arg) != f.Type(want[i]) {
					return fmt.Errorf("%w: blk%d passes %s to parameter v%d of type %s", ErrType, pred, f.Type(arg), want[i], f.Type(want[i]))
				}
			}
		}
	}
	return nil
}

// operation checks o's operand, result, and state shape against what its
// operation admits. An operation the bytecode names is checked against that
// opcode's own stack effect; the rest are the shapes the IR itself defines.
//
// An operation resumes into an interpreter state exactly when control can leave
// it: a bridge runs in the interpreter, an opcode that enters a function runs
// code that can leave, and every overwrite of a slot or of heap contents
// releases the reference it replaced. The replaced value decides that, not the
// stored one, so the test is the storage written and never the operand's type.
func operation(f *Function, sites []site, o Operation) error {
	args, results := len(o.Args), len(o.Results)
	deopts := false
	switch o.Op {
	case OpConst, OpLoad:
		if args != 0 || results != 1 {
			return counted(o.name(), args, results)
		}
	case OpExec, OpBridge:
		if err := performs(f, o); err != nil {
			return err
		}
		// OverflowsI64 names the opcodes that can overflow the boxed 49-bit
		// payload (see its own doc comment for which and why). The five that
		// lower today (ADD, SUB, MUL, SHL, SHR_U) guard the result in their
		// own arm64 lowering and, on overflow, exit through this operation's
		// own state rather than the arithmetic's producing an
		// interpreter-visible effect the way a bridge or a frame/heap write
		// does (see frontend/walk.go's exec). DIV_S and DIV_U are also named
		// but have no arm64 lowering yet.
		deopts = o.Op == OpBridge || o.Code.Writes(instr.Frame) ||
			(o.Code.Reads(instr.Heap) && o.Code.Writes(instr.Heap)) ||
			OverflowsI64(o.Code)
	case OpStore:
		if args != 1 || results != 0 {
			return counted(o.name(), args, results)
		}
		deopts = true
	case OpGuardKind, OpGuardShape:
		if args != 1 || results != 1 {
			return counted(o.name(), args, results)
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
		if !constant(f, sites, o.Args[1]) {
			return fmt.Errorf("%w: %s admits v%d, which is no observation", ErrForm, o.name(), o.Args[1])
		}
		deopts = true
	case OpRetain, OpRelease:
		if args != 1 || results != 0 {
			return counted(o.name(), args, results)
		}
		deopts = o.Op == OpRelease
	case OpState:
		if args != 0 || results != 1 {
			return counted(o.name(), args, results)
		}
		if len(o.Frames) == 0 {
			return fmt.Errorf("%w: %s carries no frame", ErrState, o.name())
		}
		for _, frame := range o.Frames {
			for _, operand := range frame.Stack {
				if operand.Owned && f.Type(operand.Value) != TypeRef {
					return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), f.Type(operand.Value))
				}
			}
			for _, local := range frame.Locals {
				if local.Index < 0 {
					return fmt.Errorf("%w: %s promotes local %d", ErrState, o.name(), local.Index)
				}
				// A promoted local carries no ownership mark, so a reference
				// in one names a count nothing accounts for (see Local).
				if f.Type(local.Value) == TypeRef {
					return fmt.Errorf("%w: %s promotes %s", ErrType, o.name(), TypeRef)
				}
			}
		}
	default:
		return fmt.Errorf("%w: %s is not an operation", ErrForm, o.Op)
	}
	if _, err := resume(f, sites, o.State, o.name(), deopts); err != nil {
		return err
	}
	return typed(f, o)
}

// performs checks o against the stack effect of the opcode it performs: one
// argument per popped kind and one result per pushed kind, each of a type that
// kind accepts. Args run in the reverse of Pop, bottom of the stack first. An
// opcode with no statically fixed effect fixes no shape here either, exactly as
// program.Verify leaves it to context, so only a call's callee is required.
// A select is the one opcode with a rule of its own: SSA gives every value one
// type, so its arms and result must agree, which its KindAny effect cannot say.
func performs(f *Function, o Operation) error {
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
		if got := f.Type(o.Args[len(o.Args)-1-i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pops %s where it wants %s", ErrType, o.name(), got, want)
		}
	}
	for i, want := range typ.Push {
		if got := f.Type(o.Results[i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pushes %s where it yields %s", ErrType, o.name(), got, want)
		}
	}
	if o.Code == instr.SELECT && (f.Type(o.Args[0]) != f.Type(o.Args[1]) || f.Type(o.Results[0]) != f.Type(o.Args[0])) {
		return fmt.Errorf("%w: %s selects between %s and %s", ErrType, o.name(), f.Type(o.Args[0]), f.Type(o.Args[1]))
	}
	return nil
}

// leaves reports whether an opcode moves control out of the operations after
// it. Where it goes is what a Terminator and the edges under it say, so such an
// opcode is one of those and never an Operation: an operation the block carries
// on past cannot be one that ends the block, and a return, a branch, and a tail
// call each already have a terminator of their own. A throw is not one of them
// - it leaves through the interpreter, as the bridge performing it does.
func leaves(op instr.Opcode) bool {
	switch op {
	case instr.BR, instr.BR_IF, instr.BR_TABLE, instr.RETURN, instr.RETURN_CALL:
		return true
	default:
		return false
	}
}

// typed checks that o's results and operands carry types its operation can
// produce and consume, and that only OpState traffics in interpreter state.
func typed(f *Function, o Operation) error {
	for _, v := range o.Results {
		if f.Type(v) == 0 {
			return fmt.Errorf("%w: result v%d has no type", ErrType, v)
		}
		if (f.Type(v) == TypeState) != (o.Op == OpState) {
			return fmt.Errorf("%w: %s cannot produce %s", ErrType, o.name(), f.Type(v))
		}
	}
	for _, v := range o.Args {
		if f.Type(v) == TypeState {
			return fmt.Errorf("%w: %s cannot consume %s", ErrType, o.name(), TypeState)
		}
	}
	switch o.Op {
	case OpGuardShape, OpGuardValue:
		if f.Type(o.Results[0]) != f.Type(o.Args[0]) {
			return fmt.Errorf("%w: %s refines %s into %s", ErrType, o.name(), f.Type(o.Args[0]), f.Type(o.Results[0]))
		}
	case OpRetain, OpRelease:
		if f.Type(o.Args[0]) != TypeRef {
			return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), f.Type(o.Args[0]))
		}
	}
	return nil
}

// terminator checks t's operand, edge, and state shape, and that a suspension
// leaves from the frame that owns the coroutine rather than an inlined one.
func terminator(f *Function, sites []site, t Terminator) error {
	args, edges := len(t.Args), len(t.Edges)
	deopts := false
	switch t.Op {
	case OpJump:
		if args != 0 || edges != 1 {
			return counted(t.Op.String(), args, edges)
		}
	case OpBranch:
		if args != 1 || edges != 2 {
			return counted(t.Op.String(), args, edges)
		}
	case OpTable:
		if args != 1 || edges == 0 {
			return counted(t.Op.String(), args, edges)
		}
	case OpReturn:
		if edges != 0 {
			return counted(t.Op.String(), args, edges)
		}
	case OpComplete:
		if edges != 0 {
			return counted(t.Op.String(), args, edges)
		}
	case OpExit, OpSuspend:
		if args != 0 || edges != 0 {
			return counted(t.Op.String(), args, edges)
		}
		deopts = true
	default:
		return fmt.Errorf("%w: %s is not a terminator", ErrForm, t.Op)
	}
	for _, edge := range t.Edges {
		if edge.Block < 0 || edge.Block >= len(f.blocks) {
			return fmt.Errorf("%w: %s names blk%d", ErrForm, t.Op, edge.Block)
		}
	}
	state, err := resume(f, sites, t.State, t.Op.String(), deopts)
	if err != nil {
		return err
	}
	if t.Op == OpSuspend && len(state.Frames) != 1 {
		return fmt.Errorf("%w: %s resumes into %d frames", ErrState, t.Op, len(state.Frames))
	}
	return nil
}

// resume returns the state operation v names, requiring one exactly when the
// operation named name can deoptimize and rejecting one it cannot resume into.
func resume(f *Function, sites []site, v Value, name string, deopts bool) (Operation, error) {
	if !deopts {
		if v != NoValue {
			return Operation{}, fmt.Errorf("%w: %s cannot resume into v%d", ErrState, name, v)
		}
		return Operation{}, nil
	}
	if v <= NoValue || int(v) >= len(sites) || f.Type(v) != TypeState {
		return Operation{}, fmt.Errorf("%w: %s resumes into v%d", ErrState, name, v)
	}
	def := sites[v]
	if def.index < 0 || f.blocks[def.block].Ops[def.index].Op != OpState {
		return Operation{}, fmt.Errorf("%w: v%d is not a state", ErrState, v)
	}
	return f.blocks[def.block].Ops[def.index], nil
}

// constant reports whether v is a compile-time value. The value a guard admits
// is one by definition - a specialization is compiled against what was
// observed - and it is what makes the guard worth its deopt: everything after
// it reads an operand that certainly holds a constant, so a lowering resolves
// a speculated callee, index, or shape exactly where it resolves a static one.
func constant(f *Function, sites []site, v Value) bool {
	if v <= NoValue || int(v) >= len(sites) {
		return false
	}
	def := sites[v]
	return def.index >= 0 && f.blocks[def.block].Ops[def.index].Op == OpConst
}

// uses checks that every value at reaches is defined and dominated there.
func uses(f *Function, sites []site, dom *graph.Dominance, at site, vs []Value) error {
	for _, v := range vs {
		if v <= NoValue || int(v) >= len(sites) {
			return fmt.Errorf("%w: v%d is not a value of this function", ErrDefine, v)
		}
		def := sites[v]
		if def.block == at.block {
			if def.index >= at.index {
				return fmt.Errorf("%w: v%d", ErrDominate, v)
			}
			continue
		}
		if !dom.Dominates(def.block, at.block) {
			return fmt.Errorf("%w: v%d", ErrDominate, v)
		}
	}
	return nil
}

// reads returns every value o reads: its arguments, the stacks and promoted
// locals its frames hold, and the state it resumes into.
func reads(o Operation) []Value {
	vs := append([]Value(nil), o.Args...)
	for _, frame := range o.Frames {
		for _, operand := range frame.Stack {
			vs = append(vs, operand.Value)
		}
		for _, local := range frame.Locals {
			vs = append(vs, local.Value)
		}
	}
	if o.State != NoValue {
		vs = append(vs, o.State)
	}
	return vs
}

// ends returns every value t reads: its arguments, the arguments it passes on
// each edge, and the state it resumes into.
func ends(t Terminator) []Value {
	vs := append([]Value(nil), t.Args...)
	for _, edge := range t.Edges {
		vs = append(vs, edge.Args...)
	}
	if t.State != NoValue {
		vs = append(vs, t.State)
	}
	return vs
}

// accepts reports whether a value of type t may stand where an opcode's stack
// effect wants kind k. KindAny admits any type, and i1 and i8 are admitted
// wherever the i32 they are computed as is wanted, exactly as program.Verify
// admits them.
func accepts(t Type, k instr.Kind) bool {
	return k == instr.KindAny || repr(TypeOf(k)) == repr(t)
}

// repr reduces a type to the type it is computed and stored as, stating
// instr.Kind.Repr in the vocabulary a value is typed in.
func repr(t Type) Type {
	if t == TypeI1 || t == TypeI8 {
		return TypeI32
	}
	return t
}

// counted reports an operand, result, or edge count the operation named name
// cannot have.
func counted(name string, in, out int) error {
	return fmt.Errorf("%w: %s takes %d and yields %d", ErrForm, name, in, out)
}

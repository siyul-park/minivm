package ssa

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
)

// site is where a value is defined: the block holding its definition and the
// position within it, -1 for a block parameter and otherwise the index of the
// defining instruction.
type site struct {
	block int
	index int
}

var (
	// ErrForm reports a structural defect: no entry block, an unreachable
	// block, an operand, result, or edge count an operation cannot have, an
	// operation used where it is not one, or an edge naming a block that does
	// not exist.
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
		for i, in := range block.Insts {
			if err := instruction(f, sites, in); err != nil {
				return fmt.Errorf("blk%d inst %d: %w", id, i, err)
			}
			if err := uses(f, sites, dom, site{id, i}, operands(in)); err != nil {
				return fmt.Errorf("blk%d inst %d: %w", id, i, err)
			}
		}
		if err := terminator(f, sites, block.Term); err != nil {
			return fmt.Errorf("blk%d term: %w", id, err)
		}
		if err := uses(f, sites, dom, site{id, len(block.Insts)}, ends(block.Term)); err != nil {
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
		for i, in := range block.Insts {
			for _, v := range in.Results {
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

// instruction checks in's operand, result, and state shape against what its
// operation admits.
func instruction(f *Function, sites []site, in Instruction) error {
	args, results := len(in.Args), len(in.Results)
	deopts := false
	switch in.Op {
	case OpConst, OpLoad:
		if args != 0 || results != 1 {
			return counted(in.Op, args, results)
		}
	case OpPure:
		typ := instr.TypeOf(in.Code)
		if args == 0 || results != 1 || (typ.Pop != nil && args != len(typ.Pop)) {
			return counted(in.Op, args, results)
		}
	case OpSelect:
		if args != 3 || results != 1 {
			return counted(in.Op, args, results)
		}
	case OpStore:
		if args != 1 || results != 0 {
			return counted(in.Op, args, results)
		}
		deopts = f.Type(in.Args[0]) == TypeRef
	case OpRead:
		if args == 0 || results != 1 {
			return counted(in.Op, args, results)
		}
	case OpWrite:
		if args < 2 || results != 0 {
			return counted(in.Op, args, results)
		}
		deopts = f.Type(in.Args[args-1]) == TypeRef
	case OpCall:
		if args == 0 {
			return counted(in.Op, args, results)
		}
		deopts = true
	case OpBridge:
		deopts = true
	case OpGuardKind, OpGuardShape:
		if args != 1 || results != 1 {
			return counted(in.Op, args, results)
		}
		deopts = true
	case OpGuardValue:
		if args != 2 || results != 1 {
			return counted(in.Op, args, results)
		}
		deopts = true
	case OpGuardBounds:
		if args != 2 || results != 0 {
			return counted(in.Op, args, results)
		}
		deopts = true
	case OpRetain, OpRelease:
		if args != 1 || results != 0 {
			return counted(in.Op, args, results)
		}
		deopts = in.Op == OpRelease
	case OpState:
		if args != 0 || results != 1 {
			return counted(in.Op, args, results)
		}
		if len(in.Frames) == 0 {
			return fmt.Errorf("%w: %s carries no frame", ErrState, in.Op)
		}
	default:
		return fmt.Errorf("%w: %s is not an instruction", ErrForm, in.Op)
	}
	if _, err := resume(f, sites, in.State, in.Op, deopts); err != nil {
		return err
	}
	return typed(f, in)
}

// typed checks that in's results and operands carry types its operation can
// produce and consume, and that only OpState traffics in interpreter state.
func typed(f *Function, in Instruction) error {
	for _, v := range in.Results {
		if f.Type(v) == 0 {
			return fmt.Errorf("%w: result v%d has no type", ErrType, v)
		}
		if (f.Type(v) == TypeState) != (in.Op == OpState) {
			return fmt.Errorf("%w: %s cannot produce %s", ErrType, in.Op, f.Type(v))
		}
	}
	for _, v := range in.Args {
		if f.Type(v) == TypeState {
			return fmt.Errorf("%w: %s cannot consume %s", ErrType, in.Op, TypeState)
		}
	}
	switch in.Op {
	case OpGuardShape, OpGuardValue:
		if f.Type(in.Results[0]) != f.Type(in.Args[0]) {
			return fmt.Errorf("%w: %s refines %s into %s", ErrType, in.Op, f.Type(in.Args[0]), f.Type(in.Results[0]))
		}
	case OpSelect:
		if f.Type(in.Args[1]) != f.Type(in.Args[2]) || f.Type(in.Results[0]) != f.Type(in.Args[1]) {
			return fmt.Errorf("%w: %s selects between %s and %s", ErrType, in.Op, f.Type(in.Args[1]), f.Type(in.Args[2]))
		}
	case OpRetain, OpRelease:
		if f.Type(in.Args[0]) != TypeRef {
			return fmt.Errorf("%w: %s owns %s", ErrType, in.Op, f.Type(in.Args[0]))
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
			return counted(t.Op, args, edges)
		}
	case OpBranch:
		if args != 1 || edges != 2 {
			return counted(t.Op, args, edges)
		}
	case OpTable:
		if args != 1 || edges == 0 {
			return counted(t.Op, args, edges)
		}
	case OpReturn:
		if edges != 0 {
			return counted(t.Op, args, edges)
		}
	case OpComplete:
		if args != 0 || edges != 0 {
			return counted(t.Op, args, edges)
		}
	case OpExit, OpSuspend:
		if args != 0 || edges != 0 {
			return counted(t.Op, args, edges)
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
	state, err := resume(f, sites, t.State, t.Op, deopts)
	if err != nil {
		return err
	}
	if t.Op == OpSuspend && len(state.Frames) != 1 {
		return fmt.Errorf("%w: %s resumes into %d frames", ErrState, t.Op, len(state.Frames))
	}
	return nil
}

// resume returns the state instruction v names, requiring one exactly when op
// can deoptimize and rejecting one it cannot resume into.
func resume(f *Function, sites []site, v Value, op Op, deopts bool) (Instruction, error) {
	if !deopts {
		if v != NoValue {
			return Instruction{}, fmt.Errorf("%w: %s cannot resume into v%d", ErrState, op, v)
		}
		return Instruction{}, nil
	}
	if v <= NoValue || int(v) >= len(sites) || f.Type(v) != TypeState {
		return Instruction{}, fmt.Errorf("%w: %s resumes into v%d", ErrState, op, v)
	}
	def := sites[v]
	if def.index < 0 || f.blocks[def.block].Insts[def.index].Op != OpState {
		return Instruction{}, fmt.Errorf("%w: v%d is not a state", ErrState, v)
	}
	return f.blocks[def.block].Insts[def.index], nil
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

// operands returns every value in reads: its arguments, the stacks its frames
// hold, and the state it resumes into.
func operands(in Instruction) []Value {
	vs := append([]Value(nil), in.Args...)
	for _, frame := range in.Frames {
		vs = append(vs, frame.Stack...)
	}
	if in.State != NoValue {
		vs = append(vs, in.State)
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

// counted reports an operand, result, or edge count op cannot have.
func counted(op Op, in, out int) error {
	return fmt.Errorf("%w: %s takes %d and yields %d", ErrForm, op, in, out)
}

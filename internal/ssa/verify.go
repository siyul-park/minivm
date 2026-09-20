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
	if function == nil || len(function.blocks) == 0 {
		return fmt.Errorf("%w: no entry block", ErrForm)
	}
	dominance := graph.NewDominance(function)
	for id := range function.blocks {
		if !dominance.Dominates(0, id) {
			return fmt.Errorf("%w: blk%d is unreachable from the entry", ErrForm, id)
		}
	}
	sites, err := define(function)
	if err != nil {
		return err
	}
	for id, block := range function.blocks {
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
	sites := make([]position, len(function.types))
	for i := range sites {
		sites[i] = position{-1, -1}
	}
	claim := func(v Value, s position) error {
		if v <= NoValue || int(v) >= len(sites) {
			return fmt.Errorf("%w: v%d is not a value of this function", ErrDefine, v)
		}
		if sites[v].block >= 0 {
			return fmt.Errorf("%w: v%d is defined twice", ErrDefine, v)
		}
		sites[v] = s
		return nil
	}
	for id, block := range function.blocks {
		for _, v := range block.Params {
			if err := claim(v, position{id, -1}); err != nil {
				return nil, err
			}
		}
		for i, operation := range block.Operations {
			for _, v := range operation.Results {
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
	want := function.blocks[block].Params
	for _, pred := range function.preds[block] {
		for _, edge := range function.blocks[pred].Terminator.Edges {
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
	switch o.Op {
	case OpConst, OpLoad:
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
				if operand.Owned && function.Type(operand.Value) != TypeRef {
					return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), function.Type(operand.Value))
				}
			}
			for _, local := range frame.Locals {
				if local.Index < 0 {
					return fmt.Errorf("%w: %s promotes local %d", ErrState, o.name(), local.Index)
				}
				if function.Type(local.Value) == TypeRef {
					return fmt.Errorf("%w: %s promotes %s", ErrType, o.name(), TypeRef)
				}
			}
		}
	default:
		return fmt.Errorf("%w: %s is not an operation", ErrForm, o.Op)
	}
	if _, err := resume(function, sites, o.State, o.name(), deopts); err != nil {
		return err
	}
	return typed(function, o)
}

func performs(function *Function, o Operation) error {
	if !instr.Valid(o.Code) {
		return fmt.Errorf("%w: %s performs no opcode", ErrForm, o.Op)
	}
	if leaves(o.Code) {
		return fmt.Errorf("%w: %s performs a terminator", ErrForm, o.name())
	}
	operationType := instr.TypeOf(o.Code)
	if operationType.Pop == nil && operationType.Push == nil {
		if o.Code.Writes(instr.Frame) && len(o.Args) == 0 {
			return counted(o.name(), len(o.Args), len(o.Results))
		}
		return nil
	}
	if len(o.Args) != len(operationType.Pop) || len(o.Results) != len(operationType.Push) {
		return counted(o.name(), len(o.Args), len(o.Results))
	}
	for i, want := range operationType.Pop {
		if got := function.Type(o.Args[len(o.Args)-1-i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pops %s where it wants %s", ErrType, o.name(), got, want)
		}
	}
	for i, want := range operationType.Push {
		if got := function.Type(o.Results[i]); !accepts(got, want) {
			return fmt.Errorf("%w: %s pushes %s where it yields %s", ErrType, o.name(), got, want)
		}
	}
	if o.Code == instr.SELECT && (function.Type(o.Args[0]) != function.Type(o.Args[1]) || function.Type(o.Results[0]) != function.Type(o.Args[0])) {
		return fmt.Errorf("%w: %s selects between %s and %s", ErrType, o.name(), function.Type(o.Args[0]), function.Type(o.Args[1]))
	}
	return nil
}

func leaves(operation instr.Opcode) bool {
	switch operation {
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
	case OpRetain, OpRelease:
		if function.Type(o.Args[0]) != TypeRef {
			return fmt.Errorf("%w: %s owns %s", ErrType, o.name(), function.Type(o.Args[0]))
		}
	}
	return nil
}

func terminator(function *Function, sites []position, t Terminator) error {
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
		if edge.Block < 0 || edge.Block >= len(function.blocks) {
			return fmt.Errorf("%w: %s names blk%d", ErrForm, t.Op, edge.Block)
		}
	}
	state, err := resume(function, sites, t.State, t.Op.String(), deopts)
	if err != nil {
		return err
	}
	if t.Op == OpSuspend && len(state.Frames) != 1 {
		return fmt.Errorf("%w: %s resumes into %d frames", ErrState, t.Op, len(state.Frames))
	}
	return nil
}

func resume(function *Function, sites []position, v Value, name string, deopts bool) (Operation, error) {
	if !deopts {
		if v != NoValue {
			return Operation{}, fmt.Errorf("%w: %s cannot resume into v%d", ErrState, name, v)
		}
		return Operation{}, nil
	}
	if v <= NoValue || int(v) >= len(sites) || function.Type(v) != TypeState {
		return Operation{}, fmt.Errorf("%w: %s resumes into v%d", ErrState, name, v)
	}
	def := sites[v]
	if def.index < 0 || function.blocks[def.block].Operations[def.index].Op != OpState {
		return Operation{}, fmt.Errorf("%w: v%d is not a state", ErrState, v)
	}
	return function.blocks[def.block].Operations[def.index], nil
}

func constant(function *Function, sites []position, v Value) bool {
	if v <= NoValue || int(v) >= len(sites) {
		return false
	}
	def := sites[v]
	return def.index >= 0 && function.blocks[def.block].Operations[def.index].Op == OpConst
}

func uses(function *Function, sites []position, dominance *graph.Dominance, at position, values []Value) error {
	for _, v := range values {
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
	return k == instr.KindAny || representation(TypeOf(k)) == representation(t)
}

func representation(t Type) Type {
	if t == TypeI1 || t == TypeI8 {
		return TypeI32
	}
	return t
}

func counted(name string, in, out int) error {
	return fmt.Errorf("%w: %s takes %d and yields %d", ErrForm, name, in, out)
}

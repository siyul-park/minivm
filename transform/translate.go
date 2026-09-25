package transform

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/graph"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Module contains read-only module facts used by translation.
type Module struct {
	// Constants is the constant pool.
	Constants []types.Boxed
	// Globals is the declared global-kind table.
	Globals []types.Kind
	// Objects resolves constant references to known cells.
	Objects Objects
	// Types is the declared-type table.
	Types []types.Type
}

// Objects maps constant references to object facts.
type Objects map[int]Object

// Object contains facts known for one referenced cell.
type Object struct {
	// Function is the referenced function, when known.
	Function *types.Function
	// Struct is the referenced struct type, when known.
	Struct *types.StructType
	// Array is the referenced array's own type, when known.
	Array *types.ArrayType
}

// ErrEntry reports an entry offset that does not start a basic block.
var ErrEntry = errors.New("entry starts no block")

// Translate converts one bytecode function from entry to SSA.
// It returns ErrEntry for a non-block entry and nil when translation is unsupported.
func Translate(module Module, address int, function *types.Function, entry int) (*ssa.Function, error) {
	if function == nil {
		return nil, nil
	}
	f, err := translate(module, address, function, entry)
	if err != nil || f == nil {
		return nil, err
	}
	for id := 0; id < f.Len(); id++ {
		if f.Block(id).Terminator.Op == ssa.OpSuspend {
			return nil, nil
		}
	}
	return f, nil
}

// Adopts returns the number of popped operands transferred to the destination.
func Adopts(code instr.Opcode, pops int) int {
	switch {
	case code.Writes(instr.Frame):
		return pops
	case code.Reads(instr.Heap) && code.Writes(instr.Heap):
		return 1
	default:
		return 0
	}
}

func translate(module Module, address int, function *types.Function, entry int) (*ssa.Function, error) {
	if len(function.Code) == 0 {
		return nil, nil
	}
	f := facts{
		constants: module.Constants,
		globals:   module.Globals,
		objects:   module.Objects,
		types:     module.Types,
	}
	blocks, err := analysis.Blocks(function)
	if err != nil {
		return nil, err
	}
	spans, at := split(function.Code, blocks)
	root, ok := at[entry]
	if !ok {
		return nil, fmt.Errorf("%w: entry %d starts no block", ErrEntry, entry)
	}
	if spans[0].start != 0 {
		return nil, nil
	}
	activation := activation{function: function, address: address, slots: function.Declared()}
	states, seen, ok := f.analyze(activation, spans, 0, nil)
	if !ok {
		return nil, nil
	}
	if root != 0 {
		if !seen[root] {
			return nil, nil
		}
		states, _, ok = f.analyze(activation, spans, root, owned(states[root]))
		if !ok {
			return nil, nil
		}
	}
	built := f.build(activation, spans, states, root)
	if built != nil && len(built.Pred(0)) > 0 {
		built = rotate(built)
	}
	return built, nil
}

// rotate prepends an empty block ahead of f's own block 0 when it has a
// predecessor — root is a loop header, reached from both outside f and its
// own back edge — so block 0 has none: every later pass and Lower assume
// this. The new block carries block 0's own params, forwarded unchanged.
func rotate(f *ssa.Function) *ssa.Function {
	rebuilder := newRebuilder(f)
	first := rebuilder.builder.Block()
	origin := f.Block(0).Params
	args := make([]ssa.Value, len(origin))
	for i, p := range origin {
		args[i] = rebuilder.builder.Param(first, f.Type(p))
	}
	for _, block := range graph.Order(f) {
		id := rebuilder.block(block)
		b := f.Block(block)
		for _, param := range b.Params {
			rebuilder.alias(param, rebuilder.builder.Param(id, f.Type(param)))
		}
		for _, operation := range b.Operations {
			rebuilder.builder.Add(id, rebuilder.define(f, rebuilder.operation(operation)))
		}
		rebuilder.builder.Term(id, rebuilder.terminator(b.Terminator))
	}
	rebuilder.builder.Term(first, ssa.Terminator{Op: ssa.OpJump, Edges: []ssa.Edge{{Block: rebuilder.block(0), Args: args}}})
	return rebuilder.builder.Build()
}

func (o Objects) function(reference int) *types.Function {
	return o[reference].Function
}

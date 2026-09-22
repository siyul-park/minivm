package transform

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
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
	// Type is the referenced struct type, when known.
	Type *types.StructType
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

// Adopts is how many of the pops operands code takes ownership of: every one
// when it enters a frame, the stored value when it overwrites heap contents.
// The owned operands it does not adopt stay the translated code's to release.
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
	states, ok := f.analyze(activation, spans, 0, nil)
	if !ok {
		return nil, nil
	}
	if root != 0 {
		if states[root] == nil {
			return nil, nil
		}
		states, ok = f.analyze(activation, spans, root, owned(states[root]))
		if !ok {
			return nil, nil
		}
	}
	return f.build(activation, spans, states, root), nil
}

func (o Objects) function(reference int) *types.Function {
	return o[reference].Function
}

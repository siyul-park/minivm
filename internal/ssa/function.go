package ssa

// Function is one SSA control-flow graph.
type Function struct {
	name   string
	types  []Type
	blocks []Block
	succs  [][]int
	preds  [][]int
	entry  Frame
}

// Block is a straight-line sequence ending in a terminator.
type Block struct {
	// Params are values passed by predecessor edges.
	Params []Value
	// Operations are the block instructions.
	Operations []Operation
	// Terminator ends the block.
	Terminator Terminator
}

// Name returns the function name.
func (f *Function) Name() string {
	return f.name
}

// Len returns the block count.
func (f *Function) Len() int {
	return len(f.blocks)
}

// Succ returns block successors in edge order.
func (f *Function) Succ(block int) []int {
	return f.succs[block]
}

// Pred returns blocks that reach block.
func (f *Function) Pred(block int) []int {
	return f.preds[block]
}

// Block returns the block at id.
func (f *Function) Block(id int) Block {
	return f.blocks[id]
}

// Values bounds the value ids: every value of f is below it.
func (f *Function) Values() int {
	return len(f.types)
}

// Type returns v's type or zero for an invalid value.
func (f *Function) Type(v Value) Type {
	if v <= NoValue || int(v) >= len(f.types) {
		return 0
	}
	return f.types[v]
}

// Entry returns the frame f enters at (see Builder.Entry).
func (f *Function) Entry() Frame {
	return f.entry
}

func newFunction(name string, types []Type, blocks []Block, entry Frame) *Function {
	f := &Function{name: name, types: types, blocks: blocks, entry: entry}
	f.succs = make([][]int, len(blocks))
	f.preds = make([][]int, len(blocks))
	for id, block := range blocks {
		for _, edge := range block.Terminator.Edges {
			if edge.Block < 0 || edge.Block >= len(blocks) {
				continue
			}
			f.succs[id] = append(f.succs[id], edge.Block)
			if preds := f.preds[edge.Block]; len(preds) == 0 || preds[len(preds)-1] != id {
				f.preds[edge.Block] = append(preds, id)
			}
		}
	}
	return f
}

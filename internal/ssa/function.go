package ssa

// Function is one SSA control-flow graph.
type Function struct {
	name   string
	types  []Type
	blocks []Block
	succs  [][]int
	preds  [][]int
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

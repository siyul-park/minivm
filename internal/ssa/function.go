// Package ssa holds minivm's SSA intermediate representation: the form a JIT
// frontend lowers bytecode or a recorded trace into, an optimizer rewrites,
// and a backend emits native code from. Values are defined once and merge
// points take block parameters rather than phis, so control flow carries its
// own dataflow. It knows the opcode and value vocabularies and nothing about
// how they execute: it never imports interp, the JIT, or any assembler.
package ssa

// Function is one compilable unit in SSA form: a block graph whose entry is
// block 0, together with the type of every value the blocks define. It
// satisfies graph.Graph, so dominance and natural loop headers are computed
// over it directly.
type Function struct {
	name   string
	types  []Type
	blocks []Block
	succs  [][]int
	preds  [][]int
}

// Block is one straight-line run of operations ending in a Terminator. Params
// are the values its predecessors pass on their edges; the entry block's
// params are the operands live when the function is entered.
type Block struct {
	Params []Value
	Ops    []Operation
	Term   Terminator
}

// Len returns the number of blocks.
func (f *Function) Len() int {
	return len(f.blocks)
}

// Succ returns the blocks control may reach from block, edge order preserved,
// including a repeated target named by more than one edge.
func (f *Function) Succ(block int) []int {
	return f.succs[block]
}

// Pred returns the blocks control may reach block from, each named once.
func (f *Function) Pred(block int) []int {
	return f.preds[block]
}

// Block returns the block at id.
func (f *Function) Block(id int) Block {
	return f.blocks[id]
}

// Type returns v's type, or the invalid zero Type when v is not a value of
// this function.
func (f *Function) Type(v Value) Type {
	if v <= NoValue || int(v) >= len(f.types) {
		return 0
	}
	return f.types[v]
}

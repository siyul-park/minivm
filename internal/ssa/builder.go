// Package ssa defines the VM optimizer intermediate representation.
package ssa

// Builder constructs an SSA Function.
type Builder struct {
	name   string
	types  []Type
	blocks []Block
	entry  Frame
}

// New returns a builder for a named function.
func New(name string) *Builder {
	return &Builder{name: name, types: make([]Type, 1)}
}

// Block appends an empty block and returns its id.
func (b *Builder) Block() int {
	b.blocks = append(b.blocks, Block{})
	return len(b.blocks) - 1
}

// Param appends a block parameter of type t.
func (b *Builder) Param(block int, t Type) Value {
	v := b.Value(t)
	b.blocks[block].Params = append(b.blocks[block].Params, v)
	return v
}

// Value reserves a value of type t.
func (b *Builder) Value(t Type) Value {
	b.types = append(b.types, t)
	return Value(len(b.types) - 1)
}

// Type returns the type reserved for v.
func (b *Builder) Type(v Value) Type {
	if v <= NoValue || int(v) >= len(b.types) {
		return 0
	}
	return b.types[v]
}

// Add appends operation to block.
func (b *Builder) Add(block int, operation Operation) {
	b.blocks[block].Operations = append(b.blocks[block].Operations, operation)
}

// Term sets the terminator for block.
func (b *Builder) Term(block int, term Terminator) {
	b.blocks[block].Terminator = term
}

// Entry records the frame the function enters at: Address, IP and
// Returns; its Stack is block 0's params. Build copies it.
func (b *Builder) Entry(frame Frame) {
	b.entry = frame
}

// Build returns the function and resets the builder.
func (b *Builder) Build() *Function {
	f := &Function{
		name:   b.name,
		types:  b.types,
		blocks: b.blocks,
		succs:  make([][]int, len(b.blocks)),
		preds:  make([][]int, len(b.blocks)),
		entry:  b.entry,
	}
	b.types, b.blocks, b.entry = make([]Type, 1), nil, Frame{}
	for id, block := range f.blocks {
		for _, edge := range block.Terminator.Edges {
			if edge.Block < 0 || edge.Block >= len(f.blocks) {
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

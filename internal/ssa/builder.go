package ssa

// Builder assembles one Function. It assigns value numbers and wires the
// block graph; it does not judge the result, because a malformed function is
// exactly what Verify exists to reject and what a pass under test must be
// able to construct.
type Builder struct {
	name   string
	types  []Type
	blocks []Block
}

// New returns a builder for a function named name. The name identifies the
// function in a Format dump and carries no other meaning.
func New(name string) *Builder {
	return &Builder{name: name, types: make([]Type, 1)}
}

// Block appends an empty block and returns its id. The first block appended
// is the function's entry.
func (b *Builder) Block() int {
	b.blocks = append(b.blocks, Block{})
	return len(b.blocks) - 1
}

// Param appends a parameter of type t to block and returns it.
func (b *Builder) Param(block int, t Type) Value {
	v := b.Value(t)
	b.blocks[block].Params = append(b.blocks[block].Params, v)
	return v
}

// Value reserves a fresh value of type t for an instruction to define as one
// of its results.
func (b *Builder) Value(t Type) Value {
	b.types = append(b.types, t)
	return Value(len(b.types) - 1)
}

// Type returns the type v was reserved with, or the invalid zero Type when v
// is not a value of this builder.
func (b *Builder) Type(v Value) Type {
	if v <= NoValue || int(v) >= len(b.types) {
		return 0
	}
	return b.types[v]
}

// Add appends op to block.
func (b *Builder) Add(block int, op Operation) {
	b.blocks[block].Ops = append(b.blocks[block].Ops, op)
}

// Term ends block with term, replacing any terminator already set.
func (b *Builder) Term(block int, term Terminator) {
	b.blocks[block].Term = term
}

// Build returns the assembled function with its successor and predecessor
// lists resolved. It hands the function its storage and resets the builder,
// so no later call reaches into what was built.
func (b *Builder) Build() *Function {
	f := &Function{
		name:   b.name,
		types:  b.types,
		blocks: b.blocks,
		succs:  make([][]int, len(b.blocks)),
		preds:  make([][]int, len(b.blocks)),
	}
	b.types, b.blocks = make([]Type, 1), nil
	for id, block := range f.blocks {
		for _, edge := range block.Term.Edges {
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

// Package ssa defines the VM optimizer intermediate representation.
//
// Every data operation is named domain.verb, with domain one of int, float,
// ref, string, array, struct, map, or guard. Signedness is an s or u prefix on
// the verb (int.sdiv) or on a condition (slt), never in a type. Operations with
// no data domain stay bare: const, load, store, select, call, state, and exec,
// which performs a guest opcode no other operation models. A Go name mirrors
// the text: int.sdiv is OpIntSDiv.
//
// Operations follow these semantics:
//
//   - i1, i8, and i32 share one 32-bit lane, an i8 held sign-extended; i64 is
//     its own. Type.Lane maps a type to its lane. int operations work on lanes:
//     arithmetic and unary width is the result lane, int.cmp width the operand
//     lane, and casts use both.
//   - int.shl, int.ashr, int.lshr, int.rotl, and int.rotr mask the count by the
//     width; none is poison.
//   - int.sdiv, int.udiv, int.srem, and int.urem trap on a zero divisor only;
//     int.sdiv of the minimum value by -1 wraps.
//   - float.min and float.max propagate NaN. float.fptosi.sat and
//     float.fptoui.sat map NaN to zero and clamp an out-of-range value.
//   - int.and, int.or, and int.xor keep the tag typing of their operands: the
//     result type is what translation assigns, and Verify requires equal lanes
//     only.
//   - select tests its condition as a nonzero lane; ref.cmp compares identity,
//     so it admits eq and ne only.
//   - An operation that can exit the unit (trap, bridge, box an i64, call)
//     carries a State; Verify enforces it.
package ssa

// Builder constructs an SSA Function.
type Builder struct {
	name   string
	types  []Type
	blocks []Block
	entry  Frame
	source uint32
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

// At sets the source Add and Term give an operation or terminator that has
// none, until the next At or Build; 0 gives none.
func (b *Builder) At(source uint32) {
	b.source = source
}

// Add appends operation to block.
func (b *Builder) Add(block int, operation Operation) {
	if operation.Source == 0 {
		operation.Source = b.source
	}
	b.blocks[block].Operations = append(b.blocks[block].Operations, operation)
}

// Term sets the terminator for block.
func (b *Builder) Term(block int, term Terminator) {
	if term.Source == 0 {
		term.Source = b.source
	}
	b.blocks[block].Terminator = term
}

// Entry records the frame the function enters at: Address, IP and
// Returns; its Stack is block 0's params. Build copies it.
func (b *Builder) Entry(frame Frame) {
	b.entry = frame
}

// Build returns the function and resets the builder.
func (b *Builder) Build() *Function {
	f := newFunction(b.name, b.types, b.blocks, b.entry)
	b.types, b.blocks, b.entry, b.source = make([]Type, 1), nil, Frame{}, 0
	return f
}

// Type returns the type reserved for v.
func (b *Builder) Type(v Value) Type {
	if !v.valid(len(b.types)) {
		return 0
	}
	return b.types[v]
}

package fixtures

import (
	"github.com/tetratelabs/wabin/leb128"
	"github.com/tetratelabs/wabin/wasm"
)

type wasmCode []byte

func (c *wasmCode) op(op wasm.Opcode) {
	*c = append(*c, op)
}

func (c *wasmCode) u32(value uint32) {
	*c = append(*c, leb128.EncodeUint32(value)...)
}

func (c *wasmCode) i32(value int32) {
	c.op(wasm.OpcodeI32Const)
	*c = append(*c, leb128.EncodeInt32(value)...)
}

func (c *wasmCode) local(op wasm.Opcode, index uint32) {
	c.op(op)
	c.u32(index)
}

func (c *wasmCode) branch(op wasm.Opcode, depth uint32) {
	c.op(op)
	c.u32(depth)
}

func (c *wasmCode) memory(op wasm.Opcode, align, offset uint32) {
	c.op(op)
	c.u32(align)
	c.u32(offset)
}

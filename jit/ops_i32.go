package jit

import "github.com/siyul-park/minivm/types"

func (c *jitCompiler) compileI32Const() bool {
	asm := c.assembler

	if c.ip+4 >= len(c.code) {
		return false
	}

	val := int32(c.code[c.ip+1]) |
		int32(c.code[c.ip+2])<<8 |
		int32(c.code[c.ip+3])<<16 |
		int32(c.code[c.ip+4])<<24

	boxedVal := types.BoxI32(val)

	asm.emit(0x4d, 0x8b, 0x6c, 0x24)
	asm.emitInt32(int32(c.stackOff))

	asm.emit(0x45, 0x8b, 0xb4, 0x24)
	asm.emitInt32(int32(c.spOff))

	asm.emit(0x48, 0xb8)
	asm.emitInt64(int64(boxedVal))

	asm.emit(0x49, 0x89, 0x04, 0xf5)

	asm.emit(0x41, 0xff, 0xc6)

	asm.emit(0x45, 0x89, 0xb4, 0x24)
	asm.emitInt32(int32(c.spOff))

	c.ip += 5
	return true
}

func (c *jitCompiler) compileI32Add() bool {
	asm := c.assembler
	c.loadStackAndSP()
	asm.emit(0x41, 0x83, 0xfe, 0x02)
	c.loadTwoStackValues()
	c.unboxTwoI32Values()
	asm.emit(0x01, 0xd1)
	c.boxAndStoreI32Result()
	c.ip++
	return true
}

func (c *jitCompiler) compileI32Sub() bool {
	asm := c.assembler
	c.loadStackAndSP()
	c.loadTwoStackValues()
	c.unboxTwoI32Values()
	asm.emit(0x29, 0xd1)
	c.boxAndStoreI32Result()
	c.ip++
	return true
}

func (c *jitCompiler) compileI32Mul() bool {
	asm := c.assembler
	c.loadStackAndSP()
	c.loadTwoStackValues()
	c.unboxTwoI32Values()
	asm.emit(0x0f, 0xaf, 0xca)
	c.boxAndStoreI32Result()
	c.ip++
	return true
}

func (c *jitCompiler) loadStackAndSP() {
	asm := c.assembler
	asm.emit(0x4d, 0x8b, 0x6c, 0x24)
	asm.emitInt32(int32(c.stackOff))
	asm.emit(0x45, 0x8b, 0xb4, 0x24)
	asm.emitInt32(int32(c.spOff))
}

func (c *jitCompiler) loadTwoStackValues() {
	asm := c.assembler
	asm.emit(0x4d, 0x8d, 0x7e, 0xfe)
	asm.emit(0x49, 0x8b, 0x04, 0xef)
	asm.emit(0x4d, 0x8d, 0x7e, 0xff)
	asm.emit(0x49, 0x8b, 0x1c, 0xef)
}

func (c *jitCompiler) unboxTwoI32Values() {
	asm := c.assembler
	asm.emit(0x48, 0x89, 0xc1)
	asm.emit(0x48, 0x81, 0xe1)
	asm.emitInt32(0xFFFF)
	asm.emit(0x48, 0x81, 0xe1)
	asm.emitInt32(0x3FFFF)
	asm.emit(0x48, 0x89, 0xda)
	asm.emit(0x48, 0x81, 0xe2)
	asm.emitInt32(0xFFFF)
	asm.emit(0x48, 0x81, 0xe2)
	asm.emitInt32(0x3FFFF)
	asm.emit(0x63, 0xc9)
	asm.emit(0x63, 0xd2)
}

func (c *jitCompiler) boxAndStoreI32Result() {
	asm := c.assembler
	asm.emit(0x48, 0x63, 0xc1)
	asm.emit(0x48, 0x89, 0xc1)
	kindI32Tag := (uint64(types.KindI32) << 52) | (0x7FF << 52)
	asm.emit(0x48, 0xb8)
	asm.emitInt64(int64(kindI32Tag))
	asm.emit(0x48, 0x81, 0xe1)
	asm.emit(0xFF, 0xFF, 0xFF, 0xFF)
	asm.emit(0x48, 0x09, 0xc8)
	asm.emit(0x4d, 0x8d, 0x7e, 0xfe)
	asm.emit(0x49, 0x89, 0x04, 0xef)
	asm.emit(0x41, 0xff, 0xce)
	asm.emit(0x45, 0x89, 0xb4, 0x24)
	asm.emitInt32(int32(c.spOff))
}

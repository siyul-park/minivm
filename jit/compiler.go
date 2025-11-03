package jit

import (
	"syscall"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
)

const (
	pageSize = 4096
)

type jitCompiler struct {
	code      []byte
	ip        int
	assembler *assembler
	stackOff  uintptr
	spOff     uintptr
	fpOff     uintptr
	framesOff uintptr
}

func Compile(code []byte, offsets Offsets) (*Code, error) {
	asm := newAssembler()
	c := &jitCompiler{
		code:      code,
		ip:        0,
		assembler: asm,
		stackOff:  offsets.Stack,
		spOff:     offsets.SP,
		fpOff:     offsets.FP,
		framesOff: offsets.Frames,
	}

	asm.emit(0x53)
	asm.emit(0x55)
	asm.emit(0x41, 0x54)
	asm.emit(0x41, 0x55)
	asm.emit(0x41, 0x56)
	asm.emit(0x41, 0x57)
	asm.emit(0x49, 0x89, 0xfc)

	for c.ip < len(code) {
		op := instr.Opcode(code[c.ip])
		if !c.compileOpcode(op) {
			break
		}
	}

	asm.emit(0x41, 0x5f)
	asm.emit(0x41, 0x5e)
	asm.emit(0x41, 0x5d)
	asm.emit(0x41, 0x5c)
	asm.emit(0x5d)
	asm.emit(0x5b)
	asm.emit(0xc3)

	codeBytes := asm.finalize()
	alignedSize := (len(codeBytes) + pageSize - 1) &^ (pageSize - 1)

	mmapData, err := syscall.Mmap(-1, 0, alignedSize, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_PRIVATE|syscall.MAP_ANON)
	if err != nil {
		return nil, err
	}

	copy(mmapData, codeBytes)

	err = syscall.Mprotect(mmapData, syscall.PROT_READ|syscall.PROT_EXEC)
	if err != nil {
		syscall.Munmap(mmapData)
		return nil, err
	}

	return newCode(mmapData, unsafe.Pointer(&mmapData[0]), len(codeBytes)), nil
}

func (c *jitCompiler) compileOpcode(op instr.Opcode) bool {
	switch op {
	case instr.I32_CONST:
		return c.compileI32Const()
	case instr.I32_ADD:
		return c.compileI32Add()
	case instr.I32_SUB:
		return c.compileI32Sub()
	case instr.I32_MUL:
		return c.compileI32Mul()
	default:
		return false
	}
}

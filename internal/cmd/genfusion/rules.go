package main

import (
	"reflect"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

type guard struct {
	typeOf    reflect.Type
	negations int
}

type operation struct {
	op    instr.Opcode
	guard *guard
}

type fragment []operation

type declaration struct {
	pattern   []operation
	sources   []fragment
	consumers []fragment
	arm64     bool
}

type rule struct {
	pattern []operation
	arm64   bool
}

type numericFamily struct {
	sources  []fragment
	binary   []instr.Opcode
	compares []instr.Opcode
}

func (declaration declaration) withARM64() declaration {
	declaration.arm64 = true
	return declaration
}

func fuse(ops ...fragment) declaration {
	return declaration{pattern: flatten(ops)}
}

func product(sources, consumers []fragment) declaration {
	return declaration{sources: sources, consumers: consumers}
}

func sources(fragments ...fragment) []fragment {
	return fragments
}

func consumers(fragments ...fragment) []fragment {
	return fragments
}

func seq(ops ...fragment) fragment {
	return flatten(ops)
}

func op(op instr.Opcode, guards ...guard) fragment {
	result := operation{op: op}
	if len(guards) == 1 {
		result.guard = &guards[0]
	} else if len(guards) > 1 {
		result.guard = &guard{}
	}
	return fragment{result}
}

func typeFor[T types.Value]() guard {
	return guard{typeOf: reflect.TypeFor[T]()}
}

func not(value guard) guard {
	value.negations++
	return value
}

func flatten(fragments []fragment) []operation {
	var result []operation
	for _, fragment := range fragments {
		result = append(result, fragment...)
	}
	return result
}

func declarations() []declaration {
	result := []declaration{
		product(
			sources(op(instr.LOCAL_GET), op(instr.GLOBAL_GET), op(instr.UPVAL_GET)),
			consumers(op(instr.DROP), op(instr.REF_IS_NULL), seq(op(instr.REF_IS_NULL), op(instr.BR_IF))),
		).withARM64(),
		product(
			sources(op(instr.CONST_GET, not(typeFor[types.String]()))),
			consumers(op(instr.DROP), op(instr.REF_IS_NULL), seq(op(instr.REF_IS_NULL), op(instr.BR_IF))),
		).withARM64(),
		fuse(op(instr.REF_NULL), op(instr.DROP)).withARM64(),
		fuse(op(instr.REF_NULL), op(instr.REF_IS_NULL)).withARM64(),
		fuse(op(instr.REF_NULL), op(instr.REF_IS_NULL), op(instr.BR_IF)).withARM64(),
		fuse(op(instr.DUP), op(instr.DROP)).withARM64(),
		fuse(op(instr.DUP), op(instr.REF_IS_NULL)).withARM64(),
		fuse(op(instr.DUP), op(instr.REF_IS_NULL), op(instr.BR_IF)).withARM64(),
	}
	result = append(result,
		fuse(op(instr.CONST_GET), op(instr.CALL)).withARM64(),
		fuse(op(instr.CONST_GET), op(instr.RETURN_CALL)).withARM64(),
		fuse(op(instr.CONST_GET), op(instr.CLOSURE_NEW)),
	)
	return append(result, numericDeclarations()...)
}

func numericDeclarations() []declaration {
	var declarations []declaration
	for _, family := range numericFamilies() {
		binary := append(append([]instr.Opcode(nil), family.binary...), family.compares...)
		fragments := make([]fragment, 0, len(binary)+len(family.compares))
		for _, consumer := range binary {
			fragments = append(fragments, op(consumer))
		}
		for _, compare := range family.compares {
			fragments = append(fragments, seq(op(compare), op(instr.BR_IF)))
			declarations = append(declarations, fuse(op(compare), op(instr.BR_IF)))
		}
		declarations = append(declarations, product(sources(family.sources...), consumers(fragments...)))

		immediate := family.sources[len(family.sources)-1]
		locals := []fragment{immediate, op(instr.LOCAL_GET)}
		for _, rhs := range locals {
			for _, consumer := range binary {
				declarations = append(declarations, fuse(op(instr.LOCAL_GET), rhs, op(consumer)))
			}
			for _, compare := range family.compares {
				declarations = append(declarations, fuse(op(instr.LOCAL_GET), rhs, op(compare), op(instr.BR_IF)))
			}
		}

		secondary := []fragment{immediate, op(instr.LOCAL_GET), op(instr.GLOBAL_GET), op(instr.UPVAL_GET)}
		for _, lhs := range []fragment{op(instr.GLOBAL_GET), op(instr.UPVAL_GET)} {
			for _, rhs := range secondary {
				for _, consumer := range binary {
					declarations = append(declarations, fuse(lhs, rhs, op(consumer)))
				}
				for _, compare := range family.compares {
					declarations = append(declarations, fuse(lhs, rhs, op(compare), op(instr.BR_IF)))
				}
			}
		}
	}
	// ARRAY_GET and STRUCT_GET specialize immediate and compile-time constant
	// I32 indexes, not slot producers.
	declarations = append(declarations,
		product(
			sources(op(instr.I32_CONST), op(instr.CONST_GET, typeFor[types.I32]())),
			consumers(op(instr.ARRAY_GET), op(instr.STRUCT_GET)),
		),
	)
	declarations = append(declarations,
		fuse(op(instr.I32_EQZ), op(instr.BR_IF)),
		fuse(op(instr.I64_EQZ), op(instr.BR_IF)),
		fuse(op(instr.I32_CONST), op(instr.BR_IF)),
	)
	return declarations
}

func numericFamilies() []numericFamily {
	return []numericFamily{
		{sources: numericSources[types.I32](instr.I32_CONST), binary: []instr.Opcode{
			instr.I32_ADD, instr.I32_SUB, instr.I32_MUL, instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U, instr.I32_XOR, instr.I32_AND, instr.I32_OR, instr.I32_ROTL, instr.I32_ROTR,
		}, compares: []instr.Opcode{instr.I32_EQ, instr.I32_NE, instr.I32_LT_S, instr.I32_LT_U, instr.I32_GT_S, instr.I32_GT_U, instr.I32_LE_S, instr.I32_LE_U, instr.I32_GE_S, instr.I32_GE_U}},
		{sources: numericSources[types.I64](instr.I64_CONST), binary: []instr.Opcode{
			instr.I64_ADD, instr.I64_SUB, instr.I64_MUL, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U, instr.I64_XOR, instr.I64_AND, instr.I64_OR, instr.I64_ROTL, instr.I64_ROTR,
		}, compares: []instr.Opcode{instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LT_U, instr.I64_GT_S, instr.I64_GT_U, instr.I64_LE_S, instr.I64_LE_U, instr.I64_GE_S, instr.I64_GE_U}},
		{sources: numericSources[types.F32](instr.F32_CONST), binary: []instr.Opcode{
			instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV, instr.F32_REM, instr.F32_MOD, instr.F32_MIN, instr.F32_MAX, instr.F32_COPYSIGN,
		}, compares: []instr.Opcode{instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_GT, instr.F32_LE, instr.F32_GE}},
		{sources: numericSources[types.F64](instr.F64_CONST), binary: []instr.Opcode{
			instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV, instr.F64_REM, instr.F64_MOD, instr.F64_MIN, instr.F64_MAX, instr.F64_COPYSIGN,
		}, compares: []instr.Opcode{instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_GT, instr.F64_LE, instr.F64_GE}},
	}
}

func numericSources[T types.Value](immediate instr.Opcode) []fragment {
	return []fragment{
		op(instr.LOCAL_GET),
		op(instr.GLOBAL_GET),
		op(instr.UPVAL_GET),
		op(instr.CONST_GET, typeFor[T]()),
		op(immediate),
	}
}

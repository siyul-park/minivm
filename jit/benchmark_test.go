package jit

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkSimpleArithmetic(b *testing.B) {
	// (10 + 5) * 3 = 45
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	b.Run("Interpreter", func(b *testing.B) {
		ctx := context.Background()
		vm := interp.New(p)
		b.ResetTimer()
		for b.Loop() {
			err := vm.Run(ctx)
			require.NoError(b, err)
			vm.Clear()
		}
	})

	b.Run("JIT", func(b *testing.B) {
		compiler := NewCompiler()
		fn, err := compiler.Compile(p.Code)
		require.NoError(b, err)
		defer fn.Free()

		b.ResetTimer()
		for b.Loop() {
			state := &InterpreterState{
				Stack: make([]types.Boxed, 1024),
				SP:    new(int),
				BP:    0,
			}

			err = fn.Execute(state)
			require.NoError(b, err)
		}
	})
}

func BenchmarkSumArithmetic(b *testing.B) {
	// 0 + 1 + 2 + ... + 499 = 124750
	var instrs []instr.Instruction
	for i := range 500 {
		instrs = append(instrs, instr.New(instr.I32_CONST, uint64(int32(i))))
		if i > 0 {
			instrs = append(instrs, instr.New(instr.I32_ADD))
		}
	}
	p := program.New(instrs)

	b.Run("Interpreter", func(b *testing.B) {
		ctx := context.Background()
		vm := interp.New(p)
		b.ResetTimer()
		for b.Loop() {
			err := vm.Run(ctx)
			require.NoError(b, err)
			vm.Clear()
		}
	})

	b.Run("JIT", func(b *testing.B) {
		compiler := NewCompiler()
		fn, err := compiler.Compile(p.Code)
		require.NoError(b, err)
		defer fn.Free()

		b.ResetTimer()
		for b.Loop() {
			state := &InterpreterState{
				Stack: make([]types.Boxed, 1024),
				SP:    new(int),
				BP:    0,
			}

			err = fn.Execute(state)
			require.NoError(b, err)
		}
	})
}

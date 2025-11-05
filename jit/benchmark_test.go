package jit

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func BenchmarkInterpreter_SimpleArithmetic(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		vm := interp.New(p)
		err := vm.Run(ctx)
		require.NoError(b, err)
		result, err := vm.Pop()
		require.NoError(b, err)
		_ = result
	}
}

func BenchmarkJIT_SimpleArithmetic_FullCycle(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiler := NewCompiler()
		fn, err := compiler.Compile(p.Code)
		require.NoError(b, err)

		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}

		err = fn.Execute(state)
		require.NoError(b, err)

		fn.Free()
	}
}

func BenchmarkJIT_SimpleArithmetic_Amortized(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(b, err)
	defer fn.Free()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}

		err = fn.Execute(state)
		require.NoError(b, err)
	}
}

func BenchmarkJIT_SimpleArithmetic_Optimal(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
		instr.New(instr.I32_CONST, uint64(int32(3))),
		instr.New(instr.I32_MUL),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(b, err)
	defer fn.Free()

	state := &InterpreterState{
		Stack: make([]types.Boxed, 1024),
		SP:    new(int),
		BP:    0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		*state.SP = 0

		err = fn.Execute(state)
		require.NoError(b, err)
	}
}

func BenchmarkComplexArithmetic_100Ops(b *testing.B) {
	var instrs []instr.Instruction
	for i := 0; i < 100; i++ {
		instrs = append(instrs, instr.New(instr.I32_CONST, uint64(int32(i))))
		if i > 0 {
			instrs = append(instrs, instr.New(instr.I32_ADD))
		}
	}
	p := program.New(instrs)

	b.Run("Interpreter", func(b *testing.B) {
		ctx := context.Background()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			vm := interp.New(p)
			vm.Run(ctx)
			vm.Pop()
		}
	})

	b.Run("JIT_FullCycle", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			compiler := NewCompiler()
			fn, _ := compiler.Compile(p.Code)

			state := &InterpreterState{
				Stack: make([]types.Boxed, 1024),
				SP:    new(int),
				BP:    0,
			}

			fn.Execute(state)
			fn.Free()
		}
	})

	b.Run("JIT_Amortized", func(b *testing.B) {
		compiler := NewCompiler()
		fn, _ := compiler.Compile(p.Code)
		defer fn.Free()

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			state := &InterpreterState{
				Stack: make([]types.Boxed, 1024),
				SP:    new(int),
				BP:    0,
			}

			fn.Execute(state)
		}
	})

	b.Run("JIT_Optimal", func(b *testing.B) {
		compiler := NewCompiler()
		fn, _ := compiler.Compile(p.Code)
		defer fn.Free()

		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			*state.SP = 0
			fn.Execute(state)
		}
	})
}

func BenchmarkJIT_CompilationOverhead(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
	}
	p := program.New(instrs)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		compiler := NewCompiler()
		fn, err := compiler.Compile(p.Code)
		require.NoError(b, err)
		fn.Free()
	}
}

func BenchmarkJIT_StateAllocation(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}
		_ = state
	}
}

func BenchmarkJIT_MarshallingOverhead(b *testing.B) {
	instrs := []instr.Instruction{
		instr.New(instr.I32_CONST, uint64(int32(10))),
		instr.New(instr.I32_CONST, uint64(int32(5))),
		instr.New(instr.I32_ADD),
	}
	p := program.New(instrs)

	compiler := NewCompiler()
	fn, err := compiler.Compile(p.Code)
	require.NoError(b, err)
	defer fn.Free()

	b.Run("WithStateAllocation", func(b *testing.B) {
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			state := &InterpreterState{
				Stack: make([]types.Boxed, 1024),
				SP:    new(int),
				BP:    0,
			}
			fn.Execute(state)
		}
	})

	b.Run("WithoutStateAllocation", func(b *testing.B) {
		state := &InterpreterState{
			Stack: make([]types.Boxed, 1024),
			SP:    new(int),
			BP:    0,
		}
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			*state.SP = 0
			fn.Execute(state)
		}
	})
}

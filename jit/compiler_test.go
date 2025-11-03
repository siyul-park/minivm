package jit

import (
	"context"
	"reflect"
	"testing"
	"unsafe"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestSimpleArithmetic(t *testing.T) {
	p := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.I32_ADD),
		},
	)

	ctx := context.Background()
	i := interp.New(p)

	err := i.Run(ctx)
	require.NoError(t, err)

	val, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(30), val)

	i.Clear()

	tp := reflect.TypeOf((*interp.Interpreter)(nil))
	offsets := GetOffsets(tp)

	code := p.Code
	jit, err := Compile(code, offsets)
	require.NoError(t, err)
	defer jit.Release()

	jit.Execute(unsafe.Pointer(i))

	val, err = i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(30), val)
}

func TestOperations(t *testing.T) {
	p := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.I32_MUL),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
		},
	)

	ctx := context.Background()
	i := interp.New(p)

	err := i.Run(ctx)
	require.NoError(t, err)

	val, err := i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(17), val)

	i.Clear()

	tp := reflect.TypeOf((*interp.Interpreter)(nil))
	offsets := GetOffsets(tp)

	code := p.Code
	jit, err := Compile(code, offsets)
	require.NoError(t, err)
	defer jit.Release()

	jit.Execute(unsafe.Pointer(i))

	val, err = i.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(17), val)
}

func BenchmarkSimpleArithmetic(b *testing.B) {
	p := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.I32_ADD),
		},
	)

	tp := reflect.TypeOf((*interp.Interpreter)(nil))
	offsets := GetOffsets(tp)

	code := p.Code
	jit, err := Compile(code, offsets)
	if err != nil {
		b.Fatal(err)
	}
	defer jit.Release()

	ctx := context.Background()
	b.Run("Interpreter", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			_ = interp.Run(ctx)
			interp.Clear()
		}
	})

	b.Run("JIT", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			interp.Clear()
			jit.Execute(unsafe.Pointer(interp))
		}
	})
}

func BenchmarkOperations(b *testing.B) {
	p := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_CONST, 2),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 3),
			instr.New(instr.I32_SUB),
			instr.New(instr.I32_CONST, 4),
			instr.New(instr.I32_MUL),
			instr.New(instr.I32_CONST, 5),
			instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 6),
			instr.New(instr.I32_SUB),
		},
	)

	tp := reflect.TypeOf((*interp.Interpreter)(nil))
	offsets := GetOffsets(tp)

	code := p.Code
	jit, err := Compile(code, offsets)
	if err != nil {
		b.Fatal(err)
	}
	defer jit.Release()

	ctx := context.Background()
	b.Run("Interpreter", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			_ = interp.Run(ctx)
			interp.Clear()
		}
	})

	b.Run("JIT", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			interp.Clear()
			jit.Execute(unsafe.Pointer(interp))
		}
	})
}

func BenchmarkSingleOperation(b *testing.B) {
	p := program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
			instr.New(instr.I32_ADD),
		},
	)

	ctx := context.Background()
	b.Run("Interpreter", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			_ = interp.Run(ctx)
			interp.Clear()
		}
	})

	tp := reflect.TypeOf((*interp.Interpreter)(nil))
	offsets := GetOffsets(tp)

	b.Run("WithCompilation", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			code := p.Code
			jit, err := Compile(code, offsets)
			if err != nil {
				b.Fatal(err)
			}
			interp := interp.New(p)
			interp.Clear()
			jit.Execute(unsafe.Pointer(interp))
			jit.Release()
		}
	})

	b.Run("WithoutCompilation", func(b *testing.B) {
		code := p.Code
		jit, err := Compile(code, offsets)
		if err != nil {
			b.Fatal(err)
		}
		defer jit.Release()

		for i := 0; i < b.N; i++ {
			interp := interp.New(p)
			interp.Clear()
			jit.Execute(unsafe.Pointer(interp))
		}
	})
}

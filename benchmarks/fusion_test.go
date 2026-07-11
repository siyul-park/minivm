package bench

import (
	"context"
	"runtime"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkFusion(b *testing.B) {
	fn := types.NewFunctionBuilder(nil).Emit(instr.New(instr.RETURN)).MustBuild()
	var numeric []instr.Instruction
	var calls []instr.Instruction
	for range 64 {
		numeric = append(numeric,
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_ADD),
		)
		calls = append(calls,
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		)
	}
	tests := []struct {
		name string
		prog *program.Program
	}{
		{
			name: "local_get_i32_const_i32_add",
			prog: program.New(numeric, program.WithLocals(types.TypeI32)),
		},
		{
			name: "const_get_function_call",
			prog: program.New(calls, program.WithConstants(fn)),
		},
	}

	for _, tt := range tests {
		b.Run(tt.name+"/threaded", func(b *testing.B) {
			benchmarkFusion(b, tt.prog, false)
		})
		if runtime.GOARCH == "arm64" {
			b.Run(tt.name+"/jit", func(b *testing.B) {
				benchmarkFusion(b, tt.prog, true)
			})
		}
	}
}

func BenchmarkRefFusion(b *testing.B) {
	fused, err := refFusionProgram(false)
	require.NoError(b, err)
	standalone, err := refFusionProgram(true)
	require.NoError(b, err)
	tests := []struct {
		name string
		prog *program.Program
	}{
		{name: "local_get_ref_ref_is_null_br_if", prog: fused},
		{name: "local_get_ref_nop_ref_is_null_br_if", prog: standalone},
	}

	for _, tt := range tests {
		b.Run(tt.name+"/threaded", func(b *testing.B) {
			benchmarkFusion(b, tt.prog, false)
		})
		if runtime.GOARCH == "arm64" {
			b.Run(tt.name+"/jit", func(b *testing.B) {
				benchmarkFusion(b, tt.prog, true)
			})
		}
	}
}

func benchmarkFusion(b *testing.B, prog *program.Program, jit bool) {
	var vm *interp.Interpreter
	if jit {
		vm = interp.New(prog, interp.WithTick(1), interp.WithThreshold(1))
	} else {
		vm = interp.New(prog, interp.WithThreshold(-1))
	}
	defer vm.Close()
	ctx := context.Background()

	require.NoError(b, vm.Run(ctx))
	vm.Reset()

	b.ReportAllocs()
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		if err := vm.Run(ctx); err != nil {
			b.StopTimer()
			require.NoError(b, err)
		}
		vm.Reset()
	}
}

func refFusionProgram(nop bool) (*program.Program, error) {
	b := program.NewBuilder().Locals(types.TypeRef, types.TypeI32)
	loop := b.Label()
	taken := b.Label()
	b.Emit(instr.I32_CONST, 1024).Emit(instr.LOCAL_SET, 1).Bind(loop)
	b.Emit(instr.LOCAL_GET, 0)
	if nop {
		b.Emit(instr.NOP)
	}
	b.Emit(instr.REF_IS_NULL).BrIf(taken)
	b.Emit(instr.NOP).Bind(taken)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.I32_CONST, 1)
	b.Emit(instr.I32_SUB)
	b.Emit(instr.LOCAL_TEE, 1).BrIf(loop)
	return b.Build()
}

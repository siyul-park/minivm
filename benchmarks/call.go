package benchmarks

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
)

func recursiveFib(n int32) *program.Program {
	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).WithParams(types.TypeI32)
	base := b.Label()
	fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	return program.New(
		[]instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(n))), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
		program.WithConstants(fn),
	)
}

func indirectRecursiveFib(n int32) *program.Program {
	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		WithParams(types.TypeI32, types.TypeRef)
	base := b.Label()
	fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(n))),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn),
	)
}

func closureCounter(count int) *program.Program {
	fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		WithCaptures(types.TypeI32).
		Emit(
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DUP),
			instr.New(instr.UPVAL_SET, 0),
			instr.New(instr.RETURN),
		).
		MustBuild()
	code := []instr.Instruction{
		instr.New(instr.I32_CONST, 0),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CLOSURE_NEW),
		instr.New(instr.LOCAL_SET, 0),
	}
	for index := range count {
		code = append(code, instr.New(instr.LOCAL_GET, 0), instr.New(instr.CALL))
		if index+1 < count {
			code = append(code, instr.New(instr.DROP))
		}
	}
	return program.New(code, program.WithConstants(fn), program.WithLocals(types.TypeRef))
}

func recursiveFibReference(n int32) int32 {
	if n < 2 {
		return n
	}
	return recursiveFibReference(n-1) + recursiveFibReference(n-2)
}

package interp

import (
	"context"
	"fmt"
	"maps"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestFusion(t *testing.T) {
	testFusionRules(t)

	type test struct {
		name     string
		prog     *program.Program
		fusionAt int
		stack    int
	}
	tests := []test{
		{
			name: "string constant preserves interning ownership",
			prog: program.New([]instr.Instruction{
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.DROP),
				instr.New(instr.CONST_GET, 0),
			}, program.WithConstants(types.String("fused"))),
		},
		{
			name: "heap I64 stays numeric and owned",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I64_CONST, i64operand(1<<60)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I64_CONST, i64operand(7)),
				instr.New(instr.I64_ADD),
			}, program.WithLocals(types.TypeI64)),
		},
		{
			name: "conditional branch taken",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(2)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, i32operand(5)),
				instr.New(instr.I32_LT_S),
				instr.New(instr.BR_IF, 8),
				instr.New(instr.I32_CONST, i32operand(100)),
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, i32operand(200)),
			}, program.WithLocals(types.TypeI32)),
		},
		{
			name: "conditional branch not taken",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(9)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, i32operand(5)),
				instr.New(instr.I32_LT_S),
				instr.New(instr.BR_IF, 8),
				instr.New(instr.I32_CONST, i32operand(100)),
				instr.New(instr.BR, 5),
				instr.New(instr.I32_CONST, i32operand(200)),
			}, program.WithLocals(types.TypeI32)),
		},
		{
			name: "integer trap keeps machine state",
			prog: program.New([]instr.Instruction{
				instr.New(instr.I32_CONST, i32operand(7)),
				instr.New(instr.LOCAL_SET, 0),
				instr.New(instr.LOCAL_GET, 0),
				instr.New(instr.I32_CONST, i32operand(0)),
				instr.New(instr.I32_DIV_S),
			}, program.WithLocals(types.TypeI32)),
		},
	}
	integerTests := []struct {
		name  string
		op    instr.Opcode
		kind  types.Kind
		lhs   int64
		rhs   int64
		stack int
	}{
		{name: "i32.div_s succeeds", op: instr.I32_DIV_S, kind: types.KindI32, lhs: 21, rhs: 3},
		{name: "i32.div_u succeeds", op: instr.I32_DIV_U, kind: types.KindI32, lhs: 21, rhs: 3},
		{name: "i32.rem_s succeeds", op: instr.I32_REM_S, kind: types.KindI32, lhs: 22, rhs: 5},
		{name: "i32.rem_u succeeds", op: instr.I32_REM_U, kind: types.KindI32, lhs: 22, rhs: 5},
		{name: "i64.div_s succeeds", op: instr.I64_DIV_S, kind: types.KindI64, lhs: 21, rhs: 3},
		{name: "i64.div_u succeeds", op: instr.I64_DIV_U, kind: types.KindI64, lhs: 21, rhs: 3},
		{name: "i64.rem_s succeeds", op: instr.I64_REM_S, kind: types.KindI64, lhs: 22, rhs: 5},
		{name: "i64.rem_u succeeds", op: instr.I64_REM_U, kind: types.KindI64, lhs: 22, rhs: 5},
		{name: "i32.div_s traps on zero", op: instr.I32_DIV_S, kind: types.KindI32, lhs: 21},
		{name: "i32.div_u traps on zero", op: instr.I32_DIV_U, kind: types.KindI32, lhs: 21},
		{name: "i32.rem_s traps on zero", op: instr.I32_REM_S, kind: types.KindI32, lhs: 21},
		{name: "i32.rem_u traps on zero", op: instr.I32_REM_U, kind: types.KindI32, lhs: 21},
		{name: "i64.div_s traps on zero", op: instr.I64_DIV_S, kind: types.KindI64, lhs: 21},
		{name: "i64.div_u traps on zero", op: instr.I64_DIV_U, kind: types.KindI64, lhs: 21},
		{name: "i64.rem_s traps on zero", op: instr.I64_REM_S, kind: types.KindI64, lhs: 21},
		{name: "i64.rem_u traps on zero", op: instr.I64_REM_U, kind: types.KindI64, lhs: 21},
		{name: "i32.div_s preserves producer overflow", op: instr.I32_DIV_S, kind: types.KindI32, lhs: 21, rhs: 3, stack: 1},
		{name: "i32.div_u preserves producer overflow", op: instr.I32_DIV_U, kind: types.KindI32, lhs: 21, rhs: 3, stack: 1},
		{name: "i32.rem_s preserves producer overflow", op: instr.I32_REM_S, kind: types.KindI32, lhs: 21, rhs: 3, stack: 1},
		{name: "i32.rem_u preserves producer overflow", op: instr.I32_REM_U, kind: types.KindI32, lhs: 21, rhs: 3, stack: 1},
		{name: "i64.div_s preserves producer overflow", op: instr.I64_DIV_S, kind: types.KindI64, lhs: 21, rhs: 3, stack: 1},
		{name: "i64.div_u preserves producer overflow", op: instr.I64_DIV_U, kind: types.KindI64, lhs: 21, rhs: 3, stack: 1},
		{name: "i64.rem_s preserves producer overflow", op: instr.I64_REM_S, kind: types.KindI64, lhs: 21, rhs: 3, stack: 1},
		{name: "i64.rem_u preserves producer overflow", op: instr.I64_REM_U, kind: types.KindI64, lhs: 21, rhs: 3, stack: 1},
	}
	for _, tt := range integerTests {
		var lhs, rhs instr.Instruction
		if tt.kind == types.KindI32 {
			lhs = instr.New(instr.I32_CONST, i32operand(int32(tt.lhs)))
			rhs = instr.New(instr.I32_CONST, i32operand(int32(tt.rhs)))
		} else {
			lhs = instr.New(instr.I64_CONST, i64operand(tt.lhs))
			rhs = instr.New(instr.I64_CONST, i64operand(tt.rhs))
		}
		tests = append(tests, test{
			name:     tt.name,
			prog:     program.New([]instr.Instruction{lhs, rhs, instr.New(tt.op)}),
			fusionAt: len(lhs),
			stack:    tt.stack,
		})
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			type state struct {
				err      string
				ip       int
				fp       int
				sp       int
				stack    []types.Boxed
				rc       []int
				free     []int
				interned map[string]types.Ref
			}

			if tt.fusionAt > 0 {
				c := threader{code: tt.prog.Code, ip: tt.fusionAt}
				require.NotNil(t, c.fusion())
			}

			threadedOptions := []func(*option){WithTick(1), WithThreshold(-1)}
			fusedOptions := []func(*option){WithThreshold(-1)}
			if tt.stack > 0 {
				threadedOptions = append(threadedOptions, WithStack(tt.stack))
				fusedOptions = append(fusedOptions, WithStack(tt.stack))
			}
			threaded := New(tt.prog, threadedOptions...)
			defer threaded.Close()
			threadedErr := threaded.Run(context.Background())
			threadedState := state{
				err:      fmt.Sprint(threadedErr),
				ip:       threaded.fr.ip,
				fp:       threaded.fp,
				sp:       threaded.sp,
				stack:    append([]types.Boxed(nil), threaded.stack[:threaded.sp]...),
				rc:       append([]int(nil), threaded.rc...),
				free:     append([]int(nil), threaded.free...),
				interned: maps.Clone(threaded.interned),
			}

			fused := New(tt.prog, fusedOptions...)
			defer fused.Close()
			fusedErr := fused.Run(context.Background())
			fusedState := state{
				err:      fmt.Sprint(fusedErr),
				ip:       fused.fr.ip,
				fp:       fused.fp,
				sp:       fused.sp,
				stack:    append([]types.Boxed(nil), fused.stack[:fused.sp]...),
				rc:       append([]int(nil), fused.rc...),
				free:     append([]int(nil), fused.free...),
				interned: maps.Clone(fused.interned),
			}

			require.Equal(t, threadedState, fusedState)
		})
	}
}

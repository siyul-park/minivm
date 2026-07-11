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
	tests := []struct {
		name string
		prog *program.Program
	}{
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

			threaded := New(tt.prog, WithTick(1), WithThreshold(-1))
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

			fused := New(tt.prog, WithThreshold(-1))
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

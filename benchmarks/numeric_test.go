package benchmarks_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkNumeric_BranchTree(b *testing.B) {
	const input int32 = 37
	const nodes = 96
	prog, want := branchTree(input, nodes)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var total int32
			for index := range nodes {
				threshold := int32((index*17 + 11) % 97)
				if input < threshold {
					total += int32(index%7 + 1)
				} else {
					total += int32(index%5 + 2)
				}
			}
			return total
		},
		wazero: "branch_tree",
		args:   []uint64{uint64(uint32(input)), uint64(uint32(nodes))},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { total := 0; for index := 0; index < %d; index++ { threshold := (index*17+11) %% 97; if %d < threshold { total += index %% 7 + 1 } else { total += index %% 5 + 2 } }; return total }()`, nodes, input),
			gopherLua: fmt.Sprintf(`function run() local total = 0; for index = 0, %d - 1 do local threshold = (index*17+11) %% 97; if %d < threshold then total = total + index %% 7 + 1 else total = total + index %% 5 + 2 end end; return total end`, nodes, input),
			goja:      fmt.Sprintf(`function run() { let total = 0; for (let index = 0; index < %d; index++) { const threshold = (index*17+11) %% 97; total += %d < threshold ? index %% 7 + 1 : index %% 5 + 2; } return total; }`, nodes, input),
			gpython: fmt.Sprintf(`def run():
    total = 0
    for index in range(%d):
        threshold = (index * 17 + 11) %% 97
        if %d < threshold: total += index %% 7 + 1
        else: total += index %% 5 + 2
    return total`, nodes, input),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var total int32; for index := 0; index < %d; index++ { threshold := int32((index*17+11) %% 97); if %d < threshold { total += int32(index%%7+1) } else { total += int32(index%%5+2) } }; return total }`, nodes, input),
		},
	}, want)
}

func branchTree(input int32, nodes int) (*program.Program, int32) {
	b := program.NewBuilder()
	b.Locals(types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(input))).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	var want int32
	for index := range nodes {
		left := b.Label()
		join := b.Label()
		threshold := int32((index*17 + 11) % 97)
		leftValue := int32(index%7 + 1)
		rightValue := int32(index%5 + 2)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(threshold))).Emit(instr.I32_LT_S).BrIf(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(rightValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(join)
		b.Bind(left)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(leftValue))).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Bind(join)
		if input < threshold {
			want += leftValue
		} else {
			want += rightValue
		}
	}
	b.Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog, want
}

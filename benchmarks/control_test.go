package benchmarks_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkControl_IterativeFib(b *testing.B) {
	const n int32 = 30
	want := iterativeFibReference(n)
	prog := iterativeFib(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var current int32
			next := int32(1)
			for range n {
				current, next = next, current+next
			}
			return current
		},
		wazero: "iterative_fib",
		args:   []uint64{uint64(uint32(n))},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`result := func() {
    current := 0
    next := 1
    for index := 0; index < %d; index++ {
        sum := current + next
        current = next
        next = sum
    }
    return current
}()`, n),
			gopherLua: fmt.Sprintf(`function run()
    local current = 0
    local next = 1
    for _ = 1, %d do
        local sum = current + next
        current = next
        next = sum
    end
    return current
end`, n),
			goja: fmt.Sprintf(`function run() {
    let current = 0;
    let next = 1;
    for (let index = 0; index < %d; index++) {
        const sum = current + next;
        current = next;
        next = sum;
    }
    return current;
}`, n),
			gpython: fmt.Sprintf(`def run():
    current = 0
    next = 1
    for _ in range(%d):
        current, next = next, current + next
    return current`, n),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var current int32; next := int32(1); for index := int32(0); index < %d; index++ { current, next = next, current + next }; return current }`, n),
		},
	}, want)
}

func BenchmarkControl_Sieve(b *testing.B) {
	const size int32 = 256
	want := sieveReference(size)
	prog := sieve(size)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			composite := make([]int32, size)
			for value := int32(2); value*value < size; value++ {
				for multiple := value * value; multiple < size; multiple += value {
					composite[multiple] = 1
				}
			}
			var count int32
			for value := int32(2); value < size; value++ {
				if composite[value] == 0 {
					count++
				}
			}
			return count
		},
		wazero: "sieve",
		args:   []uint64{uint64(uint32(size))},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`result := func() {
    composite := []
    for index := 0; index < %d; index++ { composite = append(composite, 0) }
    for value := 2; value * value < %d; value++ {
        for multiple := value * value; multiple < %d; multiple += value { composite[multiple] = 1 }
    }
    count := 0
    for value := 2; value < %d; value++ { if composite[value] == 0 { count++ } }
    return count
}()`, size, size, size, size),
			gopherLua: fmt.Sprintf(`function run()
    local composite = {}
    for index = 0, %d - 1 do composite[index] = 0 end
    for value = 2, %d - 1 do
        if value * value >= %d then break end
        for multiple = value * value, %d - 1, value do composite[multiple] = 1 end
    end
    local count = 0
    for value = 2, %d - 1 do if composite[value] == 0 then count = count + 1 end end
    return count
end`, size, size, size, size, size),
			goja: fmt.Sprintf(`function run() {
    const composite = new Int32Array(%d);
    for (let value = 2; value * value < %d; value++) {
        for (let multiple = value * value; multiple < %d; multiple += value) composite[multiple] = 1;
    }
    let count = 0;
    for (let value = 2; value < %d; value++) if (composite[value] === 0) count++;
    return count;
}`, size, size, size, size),
			gpython: fmt.Sprintf(`def run():
    composite = [0] * %d
    value = 2
    while value * value < %d:
        multiple = value * value
        while multiple < %d:
            composite[multiple] = 1
            multiple += value
        value += 1
    count = 0
    for value in range(2, %d):
        if composite[value] == 0: count += 1
    return count`, size, size, size, size),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { composite := make([]int32, %d); for value := int32(2); value*value < %d; value++ { for multiple := value*value; multiple < %d; multiple += value { composite[multiple] = 1 } }; var count int32; for value := int32(2); value < %d; value++ { if composite[value] == 0 { count++ } }; return count }`, size, size, size, size),
		},
	}, want)
}

func iterativeFib(n int32) *program.Program {
	b := program.NewBuilder()
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func sieve(size int32) *program.Program {
	b := program.NewBuilder()
	outer := b.Label()
	inner := b.Label()
	next := b.Label()
	count := b.Label()
	scan := b.Label()
	prime := b.Label()
	advance := b.Label()
	done := b.Label()
	array := b.Type(types.TypeI32Array)
	b.Locals(types.TypeI32Array, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 1)
	b.Bind(outer)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL)
	b.Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(count)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_MUL).Emit(instr.LOCAL_SET, 2)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(next)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(inner)
	b.Bind(next)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(outer)
	b.Bind(count)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.I32_CONST, 2).Emit(instr.LOCAL_SET, 3)
	b.Bind(scan)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.ARRAY_GET)
	b.Emit(instr.I32_CONST, 0).Emit(instr.I32_EQ).BrIf(prime)
	b.Br(advance)
	b.Bind(prime)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Bind(advance)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(scan)
	b.Bind(done).Emit(instr.LOCAL_GET, 4)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func iterativeFibReference(n int32) int32 {
	var current int32
	next := int32(1)
	for range n {
		current, next = next, current+next
	}
	return current
}

func sieveReference(size int32) int32 {
	composite := make([]bool, size)
	for value := int32(2); value*value < size; value++ {
		for multiple := value * value; multiple < size; multiple += value {
			composite[multiple] = true
		}
	}
	var count int32
	for value := int32(2); value < size; value++ {
		if !composite[value] {
			count++
		}
	}
	return count
}

package benchmarks

import (
	"fmt"
	"testing"

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

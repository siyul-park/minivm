package benchmarks

import (
	"context"
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestControl_IterativeFib(t *testing.T) {
	prog := iterativeFib(30)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(iterativeFibReference(30)), value)
}

func TestControl_Sieve(t *testing.T) {
	prog := sieve(256)
	require.NoError(t, program.Verify(prog))
	want := types.BoxI32(sieveReference(256))
	tests := []struct {
		name string
		new  func(*program.Program) *interp.Interpreter
	}{
		{name: "default", new: func(prog *program.Program) *interp.Interpreter {
			return interp.New(prog)
		}},
		{name: "threaded", new: func(prog *program.Program) *interp.Interpreter {
			return interp.New(prog, interp.WithThreshold(-1))
		}},
		{name: "jit", new: func(prog *program.Program) *interp.Interpreter {
			return interp.New(prog, interp.WithThreshold(0))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vm := tt.new(prog)
			defer vm.Close()
			for range 16 {
				require.NoError(t, vm.Run(context.Background()))
				value, err := vm.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, want, value)
				vm.Reset()
			}
		})
	}
}

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

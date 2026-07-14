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

func TestCall_RecursiveFib(t *testing.T) {
	prog := recursiveFib(20)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(recursiveFibReference(20)), value)
}

func TestCall_IndirectRecursiveFib(t *testing.T) {
	prog := indirectRecursiveFib(20)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(recursiveFibReference(20)), value)
}

func TestCall_ClosureCounter(t *testing.T) {
	prog := closureCounter(128)
	require.NoError(t, program.Verify(prog))
	vm := interp.New(prog, interp.WithThreshold(-1))
	defer vm.Close()

	require.NoError(t, vm.Run(context.Background()))
	value, err := vm.Pop()
	require.NoError(t, err)
	require.Equal(t, types.I32(128), value)
}

func BenchmarkCall_RecursiveFib(b *testing.B) {
	for _, n := range []int32{20, 35} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			want := recursiveFibReference(n)
			prog := recursiveFib(n)
			require.NoError(b, program.Verify(prog))

			benchmarkVM(b, prog, types.BoxI32(want))
			benchmarkCompare(b, benchmarkComparison{
				native: func() int32 { return recursiveFibReference(n) },
				wazero: "recursive_fib",
				args:   []uint64{uint64(uint32(n))},
				scripts: benchmarkScripts{
					tengo: fmt.Sprintf(`fib := func(n) { if n < 2 { return n }; return fib(n-1) + fib(n-2) }; result := fib(%d)`, n),
					gopherLua: fmt.Sprintf(`function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end
function run() return fib(%d) end`, n),
					goja: fmt.Sprintf(`function fib(n) { if (n < 2) return n; return fib(n-1) + fib(n-2); }
function run() { return fib(%d); }`, n),
					gpython: fmt.Sprintf(`def fib(n):
    if n < 2: return n
    return fib(n-1) + fib(n-2)
def run(): return fib(%d)`, n),
					yaegi: fmt.Sprintf(`package bench
func fib(n int32) int32 { if n < 2 { return n }; return fib(n-1) + fib(n-2) }
func Run() int32 { return fib(%d) }`, n),
				},
			}, want)
		})
	}
}

func BenchmarkCall_IndirectRecursiveFib(b *testing.B) {
	const n int32 = 20
	want := recursiveFibReference(n)
	prog := indirectRecursiveFib(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			type fib func(int32, fib) int32
			var run fib
			run = func(value int32, self fib) int32 {
				if value < 2 {
					return value
				}
				return self(value-1, self) + self(value-2, self)
			}
			return run(n, run)
		},
		wazero: "indirect_recursive_fib",
		args:   []uint64{uint64(uint32(n))},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`fib := func(n, self) { if n < 2 { return n }; return self(n-1, self) + self(n-2, self) }; result := fib(%d, fib)`, n),
			gopherLua: fmt.Sprintf(`function fib(n, self) if n < 2 then return n end return self(n-1, self) + self(n-2, self) end
function run() return fib(%d, fib) end`, n),
			goja: fmt.Sprintf(`function fib(n, self) { if (n < 2) return n; return self(n-1, self) + self(n-2, self); }
function run() { return fib(%d, fib); }`, n),
			gpython: fmt.Sprintf(`def fib(n, self):
    if n < 2: return n
    return self(n-1, self) + self(n-2, self)
def run(): return fib(%d, fib)`, n),
			yaegi: fmt.Sprintf(`package bench
var run func(int32) int32
func init() { run = func(n int32) int32 { if n < 2 { return n }; return run(n-1) + run(n-2) } }
func Run() int32 { return run(%d) }`, n),
		},
	}, want)
}

func BenchmarkCall_ClosureCounter(b *testing.B) {
	const count = 128
	want := int32(count)
	prog := closureCounter(count)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var value int32
			next := func() int32 { value++; return value }
			for range count {
				value = next()
			}
			return value
		},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { value := 0; next := func() { value++; return value }; for index := 0; index < %d; index++ { value = next() }; return value }()`, count),
			gopherLua: fmt.Sprintf(`function run() local value = 0; local function next() value = value + 1; return value end; for _ = 1, %d do value = next() end; return value end`, count),
			goja:      fmt.Sprintf(`function run() { let value = 0; const next = () => ++value; for (let index = 0; index < %d; index++) value = next(); return value; }`, count),
			gpython: fmt.Sprintf(`def run():
    value = [0]
    def next():
        value[0] += 1
        return value[0]
    for _ in range(%d): value[0] = next()
    return value[0]`, count),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var value int32; next := func() int32 { value++; return value }; for index := 0; index < %d; index++ { value = next() }; return value }`, count),
		},
	}, want)
}

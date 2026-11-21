package benchmarks_test

import (
	"context"
	"fmt"
	"maps"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	_ "github.com/siyul-park/minivm/benchmarks/fixtures"
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/optimize"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// modes lists the minivm execution modes shared by tests and benchmarks;
// optimized runs the O3 bytecode optimizer, then the threaded interpreter.
var modes = []struct {
	name     string
	optimize bool
	opts     []interp.Option
}{
	{name: "threaded", opts: []interp.Option{interp.WithThreshold(-1)}},
	{name: "optimized", optimize: true, opts: []interp.Option{interp.WithThreshold(-1)}},
	{name: "jit"},
}

// readProgram parses the fixture afresh, since optimizing mutates the program.
func readProgram(spec registry.Spec, optimized bool) *program.Program {
	prog, err := program.Parse(strings.NewReader(spec.Source.MVM))
	if err != nil {
		panic("parse fixture " + spec.Name + ".mvm: " + err.Error())
	}
	if optimized {
		prog, err = optimize.New(optimize.O3).Optimize(prog)
		if err != nil {
			panic("optimize fixture " + spec.Name + ".mvm: " + err.Error())
		}
	}
	return prog
}

func TestKernels(t *testing.T) {
	for _, spec := range registry.All() {
		spec := spec
		t.Run(spec.Name, func(t *testing.T) {
			result := spec.Result()
			t.Run("optimizing keeps a source of the program on every instruction", func(t *testing.T) {
				for _, level := range []optimize.Level{optimize.O1, optimize.O2, optimize.O3} {
					prog, err := program.Parse(strings.NewReader(spec.Source.MVM))
					require.NoError(t, err)
					// Each instruction gets a source unique across the program:
					// owner<<16 + ip+1, the module owning 0 and a function its
					// constant index + 1.
					owned := map[*types.Function]map[uint32]bool{}
					annotate := func(code []byte, owner int) ([]instr.Position, map[uint32]bool) {
						var positions []instr.Position
						sources := map[uint32]bool{}
						for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
							source := uint32(owner<<16 + ip + 1)
							positions = append(positions, instr.Position{IP: ip, Source: source})
							sources[source] = true
						}
						return positions, sources
					}
					var module map[uint32]bool
					prog.Positions, module = annotate(prog.Code, 0)
					for i, c := range prog.Constants {
						if fn, ok := c.(*types.Function); ok {
							fn.Positions, owned[fn] = annotate(fn.Code, i+1)
						}
					}
					// A body may take the sources of the functions it reaches
					// through CONST_GET, which is what O3 inlines.
					reach := func(code []byte, own map[uint32]bool) map[uint32]bool {
						allowed, seen := maps.Clone(own), map[*types.Function]bool{}
						queue := [][]byte{code}
						for len(queue) > 0 {
							for _, inst := range instr.Unmarshal(queue[0]) {
								if inst.Opcode() != instr.CONST_GET {
									continue
								}
								if fn, ok := prog.Constants[inst.Operand(0)].(*types.Function); ok && !seen[fn] {
									seen[fn] = true
									maps.Copy(allowed, owned[fn])
									queue = append(queue, fn.Code)
								}
							}
							queue = queue[1:]
						}
						return allowed
					}
					allowed := map[*types.Function]map[uint32]bool{}
					for fn, own := range owned {
						allowed[fn] = own
						if level == optimize.O3 {
							allowed[fn] = reach(fn.Code, own)
						}
					}
					moduleAllowed := module
					if level == optimize.O3 {
						moduleAllowed = reach(prog.Code, module)
					}

					prog, err = optimize.New(level).Optimize(prog)
					require.NoError(t, err)
					require.NoError(t, program.Verify(prog))
					check := func(name string, code []byte, positions []instr.Position, allowed map[uint32]bool) {
						for ip := 0; ip < len(code); ip += instr.Instruction(code[ip:]).Width() {
							source := instr.Find(positions, ip)
							require.True(t, allowed[source], "O%d %s ip %d source %#x", level, name, ip, source)
						}
					}
					check("module", prog.Code, prog.Positions, moduleAllowed)
					for i, c := range prog.Constants {
						if fn, ok := c.(*types.Function); ok {
							check(fmt.Sprint(i), fn.Code, fn.Positions, allowed[fn])
						}
					}
				}
			})
			for _, mode := range modes {
				t.Run(mode.name, func(t *testing.T) {
					prog := readProgram(spec, mode.optimize)
					require.NoError(t, program.Verify(prog))
					vm := interp.MustNew(prog, mode.opts...)
					defer vm.Close()
					wireHost(t, vm, spec.Host)
					const minimum = 5
					const maximum = 50
					const budget = 200 * time.Millisecond
					start := time.Now()
					for round := 0; round < minimum || (round < maximum && time.Since(start) < budget); round++ {
						require.NoError(t, vm.Run(t.Context()))
						got, err := vm.PopBoxed()
						require.NoError(t, err)
						require.Equal(t, result, resolve(t, vm, got))
						vm.Reset()
						wireHost(t, vm, spec.Host)
					}
				})
			}
		})
	}
}

func BenchmarkKernels(b *testing.B) {
	for _, spec := range registry.All() {
		spec := spec
		b.Run(spec.Name, func(b *testing.B) {
			result := spec.Result()
			benchmarkVM(b, spec, result)
			benchmarkNative(b, spec, result)
			benchmarkExternal(b, spec, result)
		})
	}
}

func benchmarkVM(b *testing.B, spec registry.Spec, want types.Value) {
	b.Helper()
	host := spec.Host
	for _, mode := range modes {
		b.Run(mode.name, func(b *testing.B) {
			prog := readProgram(spec, mode.optimize)
			vm := interp.MustNew(prog, mode.opts...)
			defer vm.Close()
			ctx := context.Background()

			wireHost(b, vm, host)
			require.NoError(b, vm.Run(ctx))
			got, err := vm.PopBoxed()
			require.NoError(b, err)
			require.Equal(b, want, resolve(b, vm, got))
			vm.Reset()
			wireHost(b, vm, host)

			const warmups = 4
			const allocations = 32
			byteSamples := make([]uint64, 0, allocations)
			allocSamples := make([]uint64, 0, allocations)
			for index := range warmups + allocations {
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				require.NoError(b, vm.Run(ctx))
				runtime.ReadMemStats(&after)
				got, err := vm.PopBoxed()
				require.NoError(b, err)
				require.Equal(b, want, resolve(b, vm, got))
				vm.Reset()
				wireHost(b, vm, host)
				if index >= warmups {
					byteSamples = append(byteSamples, after.TotalAlloc-before.TotalAlloc)
					allocSamples = append(allocSamples, after.Mallocs-before.Mallocs)
				}
			}
			slices.Sort(byteSamples)
			slices.Sort(allocSamples)
			bytes := byteSamples[len(byteSamples)/2]
			allocs := allocSamples[len(allocSamples)/2]

			const samples = 4096
			var overhead time.Duration
			for range samples {
				start := time.Now()
				overhead += time.Since(start)
			}
			overhead /= samples

			var runErr, popErr error
			var elapsed time.Duration
			var value types.Boxed
			b.ReportAllocs()
			b.ResetTimer()
			for first := true; b.Loop(); first = false {
				if !first {
					vm.Reset()
					wireHost(b, vm, host)
				}
				start := time.Now()
				runErr = vm.Run(ctx)
				elapsed += time.Since(start)
				if runErr != nil {
					break
				}
				value, popErr = vm.PopBoxed()
				if popErr != nil {
					break
				}
			}
			elapsed -= min(elapsed, overhead*time.Duration(b.N))
			b.ReportMetric(float64(elapsed.Nanoseconds())/float64(b.N), "ns/op")
			b.ReportMetric(float64(bytes), "B/op")
			b.ReportMetric(float64(allocs), "allocs/op")
			require.NoError(b, runErr)
			require.NoError(b, popErr)
			require.Equal(b, want, resolve(b, vm, value))
		})
	}
}

func benchmarkNative(b *testing.B, spec registry.Spec, result types.Value) {
	switch result.Kind() {
	case types.KindI64:
		if spec.Native.I64 != nil {
			want, ok := result.(types.I64)
			require.True(b, ok)
			benchmarkInt64(b, spec.Native.I64, want)
		}
	default:
		if spec.Native.I32 != nil {
			want, ok := result.(types.I32)
			require.True(b, ok)
			benchmarkInt32(b, spec.Native.I32, want)
		}
	}
}

func benchmarkInt32(b *testing.B, run func() int32, want types.I32) {
	b.Run("native", func(b *testing.B) {
		require.Equal(b, int32(want), run())
		var value int32
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			value = run()
		}
		b.StopTimer()
		require.Equal(b, int32(want), value)
	})
}

func benchmarkInt64(b *testing.B, run func() int64, want types.I64) {
	b.Run("native", func(b *testing.B) {
		require.Equal(b, int64(want), run())
		var value int64
		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			value = run()
		}
		b.StopTimer()
		require.Equal(b, int64(want), value)
	})
}

// wireHost binds the spec's host function to global 0 through the codec
// default path; Reset reseeds globals, so it runs again after each Reset.
func wireHost(tb testing.TB, vm *interp.Interpreter, host *registry.Host) {
	tb.Helper()
	if host == nil {
		return
	}
	fn, err := vm.Marshal(host.I32)
	require.NoError(tb, err)
	addr, err := vm.Alloc(fn)
	require.NoError(tb, err)
	require.NoError(tb, vm.SetGlobal(0, types.BoxRef(addr)))
}

func resolve(tb testing.TB, vm *interp.Interpreter, got types.Boxed) types.Value {
	tb.Helper()
	if got.Kind() != types.KindRef {
		return types.Unbox(got)
	}
	value, err := vm.Load(got.Ref())
	require.NoError(tb, err)
	require.NoError(tb, vm.Release(got.Ref()))
	return value
}

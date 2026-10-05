package benchmarks_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	tengo "github.com/d5/tengo/v2"
	tengoStdlib "github.com/d5/tengo/v2/stdlib"
	"github.com/dop251/goja"
	"github.com/go-python/gpython/compile"
	"github.com/go-python/gpython/py"
	_ "github.com/go-python/gpython/stdlib"
	"github.com/siyul-park/minivm/benchmarks/registry"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	yaegi "github.com/traefik/yaegi/interp"
	yaegiStdlib "github.com/traefik/yaegi/stdlib"
	lua "github.com/yuin/gopher-lua"
)

func benchmarkExternal(b *testing.B, spec registry.Spec, result types.Value) {
	switch result.Kind() {
	case types.KindI32:
		if spec.Wazero != nil {
			want, ok := result.(types.I32)
			require.True(b, ok)
			benchmarkWazero(b, *spec.Wazero, int32(want))
		}
		want, ok := result.(types.I32)
		require.True(b, ok)
		benchmarkScripts(b, spec.Source, int32(want))
		benchmarkYaegi(b, spec.Name, spec.Source, result)
	case types.KindI64:
		benchmarkScripts64(b, spec.Source, int64(result.(types.I64)))
		benchmarkYaegi(b, spec.Name, spec.Source, result)
	}
}

func benchmarkScripts64(b *testing.B, source registry.Source, want int64) {
	if source.Tengo != "" {
		b.Run("tengo", func(b *testing.B) {
			benchmarkTengo64(b, source.Tengo, want)
		})
	}
	if source.JS != "" {
		b.Run("goja", func(b *testing.B) {
			benchmarkGoja64(b, source.JS, want)
		})
	}
	if source.Python != "" {
		b.Run("gpython", func(b *testing.B) {
			benchmarkGpython64(b, source.Python, want)
		})
		b.Run("cpython", func(b *testing.B) {
			benchmarkCPython64(b, source.Python, want)
		})
	}
}

func benchmarkScripts(b *testing.B, source registry.Source, want int32) {
	if source.Tengo != "" {
		b.Run("tengo", func(b *testing.B) {
			benchmarkTengo(b, source.Tengo, want)
		})
	}
	if source.Lua != "" {
		b.Run("gopher_lua", func(b *testing.B) {
			benchmarkGopherLua(b, source.Lua, want)
		})
	}
	if source.JS != "" {
		b.Run("goja", func(b *testing.B) {
			benchmarkGoja(b, source.JS, want)
		})
	}
	if source.Python != "" {
		b.Run("gpython", func(b *testing.B) {
			benchmarkGpython(b, source.Python, want)
		})
		b.Run("cpython", func(b *testing.B) {
			benchmarkCPython(b, source.Python, want)
		})
	}
}

func benchmarkTengo64(b *testing.B, source string, want int64) {
	script := tengo.NewScript([]byte(source))
	script.SetImports(tengoStdlib.GetModuleMap("math"))
	compiled, err := script.Compile()
	require.NoError(b, err)
	require.NoError(b, compiled.Run())
	require.Equal(b, want, compiled.Get("result").Int64())

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = compiled.Run()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, compiled.Get("result").Int64())
}

func benchmarkTengo(b *testing.B, source string, want int32) {
	script := tengo.NewScript([]byte(source))
	script.SetImports(tengoStdlib.GetModuleMap("math"))
	compiled, err := script.Compile()
	require.NoError(b, err)
	require.NoError(b, compiled.Run())
	require.Equal(b, int64(want), compiled.Get("result").Int64())

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = compiled.Run()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, int64(want), compiled.Get("result").Int64())
}

func benchmarkGopherLua(b *testing.B, source string, want int32) {
	state := lua.NewState()
	defer state.Close()
	require.NoError(b, state.DoString(source))
	function := state.GetGlobal("run")

	var value int32
	call := func() error {
		if err := state.CallByParam(lua.P{Fn: function, NRet: 1, Protect: true}); err != nil {
			return err
		}
		result := state.Get(-1)
		state.Pop(1)
		number, ok := result.(lua.LNumber)
		if !ok {
			return fmt.Errorf("run returned %s", result.Type())
		}
		value = int32(number)
		return nil
	}

	require.NoError(b, call())
	require.Equal(b, want, value)

	var err error
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, value)
}

func benchmarkGoja64(b *testing.B, source string, want int64) {
	vm := goja.New()
	prog, err := goja.Compile("fixture.js", strings.TrimSpace(source), true)
	require.NoError(b, err)
	_, err = vm.RunProgram(prog)
	require.NoError(b, err)
	function, ok := goja.AssertFunction(vm.Get("run"))
	require.True(b, ok)

	var value int64
	call := func() error {
		result, err := function(goja.Undefined())
		if err != nil {
			return err
		}
		parsed, err := strconv.ParseInt(result.String(), 10, 64)
		if err != nil {
			return err
		}
		value = parsed
		return nil
	}

	require.NoError(b, call())
	require.Equal(b, want, value)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, value)
}

func benchmarkGoja(b *testing.B, source string, want int32) {
	vm := goja.New()
	prog, err := goja.Compile("fixture.js", strings.TrimSpace(source), true)
	require.NoError(b, err)
	_, err = vm.RunProgram(prog)
	require.NoError(b, err)
	function, ok := goja.AssertFunction(vm.Get("run"))
	require.True(b, ok)

	var value int32
	call := func() error {
		result, err := function(goja.Undefined())
		if err != nil {
			return err
		}
		value = int32(result.ToInteger())
		return nil
	}

	require.NoError(b, call())
	require.Equal(b, want, value)

	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
	require.Equal(b, want, value)
}

func benchmarkGpython64(b *testing.B, source string, want int64) {
	code, err := compile.Compile(strings.TrimSpace(source), "fixture.py", py.ExecMode, 0, true)
	require.NoError(b, err)
	ctx := py.NewContext(py.DefaultContextOpts())
	defer ctx.Close()
	module, err := py.RunCode(ctx, code, "fixture.py", nil)
	require.NoError(b, err)

	call := func() error {
		result, err := module.Call("run", nil, nil)
		if err != nil {
			return err
		}
		value, ok := result.(py.Int)
		if !ok || int64(value) != want {
			return fmt.Errorf("unexpected gpython result")
		}
		return nil
	}

	require.NoError(b, call())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
}

func benchmarkGpython(b *testing.B, source string, want int32) {
	code, err := compile.Compile(strings.TrimSpace(source), "fixture.py", py.ExecMode, 0, true)
	require.NoError(b, err)
	ctx := py.NewContext(py.DefaultContextOpts())
	defer ctx.Close()
	module, err := py.RunCode(ctx, code, "fixture.py", nil)
	require.NoError(b, err)

	call := func() error {
		result, err := module.Call("run", nil, nil)
		if err != nil {
			return err
		}
		value, ok := result.(py.Int)
		if !ok || int32(value) != want {
			return fmt.Errorf("unexpected gpython result")
		}
		return nil
	}

	require.NoError(b, call())
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		err = call()
		if err != nil {
			break
		}
	}
	b.StopTimer()
	require.NoError(b, err)
}

const cpythonDriver = `
import sys
import time

want = int(sys.argv[1])
iterations = int(sys.argv[2])
result = run()
if result != want:
    raise SystemExit("unexpected result")
start = time.perf_counter()
for _ in range(iterations):
    run()
elapsed = time.perf_counter() - start
print(int(elapsed * 1e9))
`

const cpythonDriver64 = "import sys\nimport time\n\nwant = int(sys.argv[1])\niterations = int(sys.argv[2])\nresult = run()\nif result != want:\n    raise SystemExit(\"unexpected result\")\nstart = time.perf_counter()\nfor _ in range(iterations):\n    run()\nelapsed = time.perf_counter() - start\nprint(int(elapsed * 1e9))\n"

func benchmarkCPython64(b *testing.B, source string, want int64) {
	python, err := exec.LookPath("python3.13")
	if err != nil {
		b.Skipf("python3.13 not found: %v", err)
	}

	path := filepath.Join(b.TempDir(), "fixture.py")
	script := strings.TrimSpace(source) + "\n" + cpythonDriver64
	require.NoError(b, os.WriteFile(path, []byte(script), 0o600))

	cmd := exec.Command(python, path, strconv.FormatInt(want, 10), strconv.Itoa(b.N))
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	b.ResetTimer()
	err = cmd.Run()
	b.StopTimer()
	if err != nil && stderr.Len() > 0 {
		b.Log(stderr.String())
	}
	require.NoError(b, err)

	elapsed, err := strconv.ParseInt(strings.TrimSpace(stdout.String()), 10, 64)
	require.NoError(b, err)
	b.ReportMetric(float64(elapsed)/float64(b.N), "ns/op")
}

func benchmarkCPython(b *testing.B, source string, want int32) {
	python, err := exec.LookPath("python3.13")
	if err != nil {
		b.Skipf("python3.13 not found: %v", err)
	}

	path := filepath.Join(b.TempDir(), "fixture.py")
	script := strings.TrimSpace(source) + "\n" + cpythonDriver
	require.NoError(b, os.WriteFile(path, []byte(script), 0o600))

	cmd := exec.Command(python, path, strconv.Itoa(int(want)), strconv.Itoa(b.N))
	cmd.Env = append(os.Environ(), "PYTHONHASHSEED=0")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	b.ResetTimer()
	err = cmd.Run()
	b.StopTimer()
	if err != nil && stderr.Len() > 0 {
		b.Log(stderr.String())
	}
	require.NoError(b, err)

	elapsed, err := strconv.ParseInt(strings.TrimSpace(stdout.String()), 10, 64)
	require.NoError(b, err)
	b.ReportMetric(float64(elapsed)/float64(b.N), "ns/op")
}

func benchmarkWazero(b *testing.B, spec registry.Wazero, want int32) {
	b.Run("wazero", func(b *testing.B) {
		ctx := context.Background()
		runtime := wazero.NewRuntime(ctx)
		defer runtime.Close(ctx)
		compiled, err := runtime.CompileModule(ctx, spec.Module())
		require.NoError(b, err)
		defer compiled.Close(ctx)
		module, err := runtime.InstantiateModule(ctx, compiled, wazero.NewModuleConfig())
		require.NoError(b, err)
		defer module.Close(ctx)

		function := module.ExportedFunction(spec.Export)
		require.NotNil(b, function)
		results, err := function.Call(ctx, spec.Args...)
		require.NoError(b, err)
		require.Len(b, results, 1)
		require.Equal(b, want, api.DecodeI32(results[0]))

		b.ReportAllocs()
		b.ResetTimer()
		for b.Loop() {
			results, err = function.Call(ctx, spec.Args...)
			if err != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, err)
		require.Len(b, results, 1)
		require.Equal(b, want, api.DecodeI32(results[0]))
	})
}

var (
	yaegiPathOnce sync.Once
	yaegiPath     string
	yaegiPathErr  error
)

func benchmarkYaegi(b *testing.B, name string, source registry.Source, result types.Value) {
	if source.Go == "" {
		return
	}
	b.Run("yaegi", func(b *testing.B) {
		switch name {
		case "typed-array-sum":
			b.Skip("Yaegi does not support generic type instantiation in this fixture")
		case "recursive-fib-35":
			b.Skip("Yaegi execution is impractical for this recursive workload")
		}

		var captured registry.Spec
		register := func(value registry.Spec) {
			captured = value
		}

		gopath, err := yaegiGoPath()
		require.NoError(b, err)
		vm := yaegi.New(yaegi.Options{GoPath: gopath})
		require.NoError(b, vm.Use(yaegiStdlib.Symbols))
		require.NoError(b, vm.Use(interpExports(register)))
		if source.Support != "" {
			_, err = vm.Eval(source.Support)
			require.NoError(b, err)
		}
		_, err = vm.Eval(source.Go)
		require.NoError(b, err)
		require.Equal(b, name, captured.Name)

		var function reflect.Value
		switch result.Kind() {
		case types.KindI32:
			function = reflect.ValueOf(captured.Native.I32)
		case types.KindI64:
			function = reflect.ValueOf(captured.Native.I64)
		default:
			b.Skipf("unsupported yaegi result kind: %s", result.Kind())
		}
		require.True(b, function.IsValid())

		call := func() error {
			out := function.Call(nil)
			if len(out) != 1 {
				return fmt.Errorf("expected one result")
			}
			got, ok := number(out[0].Interface())
			if !ok || got != result.String() {
				return fmt.Errorf("unexpected yaegi result")
			}
			return nil
		}

		require.NoError(b, call())
		b.ReportAllocs()
		b.ResetTimer()
		var runErr error
		for b.Loop() {
			runErr = call()
			if runErr != nil {
				break
			}
		}
		b.StopTimer()
		require.NoError(b, runErr)
	})
}

func interpExports(register func(registry.Spec)) yaegi.Exports {
	return yaegi.Exports{
		"github.com/siyul-park/minivm/benchmarks/registry/registry": {
			"Register": reflect.ValueOf(register),
			"Spec":     reflect.Zero(reflect.TypeOf((*registry.Spec)(nil))),
			"Native":   reflect.Zero(reflect.TypeOf((*registry.Native)(nil))),
			"Wazero":   reflect.Zero(reflect.TypeOf((*registry.Wazero)(nil))),
		},
		"github.com/siyul-park/minivm/types/types": {
			"Value":      reflect.Zero(reflect.TypeOf((*types.Value)(nil))),
			"I32":        reflect.Zero(reflect.TypeOf((*types.I32)(nil))),
			"I64":        reflect.Zero(reflect.TypeOf((*types.I64)(nil))),
			"TypeI32":    reflect.ValueOf(types.TypeI32),
			"TypedArray": reflect.Zero(reflect.TypeOf((*types.TypedArray[int32])(nil))),
		},
	}
}

func yaegiGoPath() (string, error) {
	yaegiPathOnce.Do(func() {
		cwd, err := os.Getwd()
		if err != nil {
			yaegiPathErr = err
			return
		}
		root, err := filepath.Abs(filepath.Join(cwd, ".."))
		if err != nil {
			yaegiPathErr = err
			return
		}
		cmd := exec.Command("go", "list", "-m", "-f", "{{.Dir}}", "github.com/tetratelabs/wabin")
		cmd.Dir = cwd
		output, err := cmd.Output()
		if err != nil {
			yaegiPathErr = err
			return
		}
		wabin := strings.TrimSpace(string(output))
		path, err := os.MkdirTemp("", "minivm-yaegi-gopath-")
		if err != nil {
			yaegiPathErr = err
			return
		}
		src := filepath.Join(path, "src")
		for _, link := range []struct {
			name   string
			target string
		}{
			{name: "github.com/siyul-park/minivm", target: root},
			{name: "github.com/tetratelabs/wabin", target: wabin},
		} {
			linkPath := filepath.Join(src, link.name)
			if err := os.MkdirAll(filepath.Dir(linkPath), 0o755); err != nil {
				_ = os.RemoveAll(path)
				yaegiPathErr = err
				return
			}
			if err := os.Symlink(link.target, linkPath); err != nil {
				_ = os.RemoveAll(path)
				yaegiPathErr = err
				return
			}
		}
		yaegiPath = path
	})
	return yaegiPath, yaegiPathErr
}

func number(value any) (string, bool) {
	switch value := value.(type) {
	case int32:
		return strconv.FormatInt(int64(value), 10), true
	case int64:
		return strconv.FormatInt(value, 10), true
	case int:
		return strconv.Itoa(value), true
	default:
		return "", false
	}
}

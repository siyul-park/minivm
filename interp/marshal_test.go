package interp_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	interp "github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type marshalCustom int32

type marshalContextKey byte

type marshalHostFields struct {
	Count int32
}

func (*marshalHostFields) Context(ctx context.Context) int32 {
	if ctx.Value(marshalContextKey(0)) == "value" {
		return 7
	}
	return 0
}

func (v marshalCustom) MarshalVM(*interp.Interpreter) (types.Value, error) {
	return types.I32(v), nil
}

func (v *marshalCustom) UnmarshalVM(_ *interp.Interpreter, value types.Value) error {
	n, ok := value.(types.I32)
	if !ok {
		return interp.ErrTypeMismatch
	}
	*v = marshalCustom(n)
	return nil
}

func TestInterpreter_Marshal(t *testing.T) {
	t.Run("scalar value", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		v, err := i.Marshal(int32(7))
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("function receives active context", func(t *testing.T) {
		var got context.Context
		setup := interp.New(program.New(nil))
		fn, err := setup.Marshal(func(ctx context.Context) int32 {
			got = ctx
			return 7
		})
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := interp.New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("host method receives active context", func(t *testing.T) {
		setup := interp.New(program.New(nil))
		defer setup.Close()
		value, err := setup.Marshal(marshalHostFields{})
		require.NoError(t, err)
		host := value.(*interp.HostObject)
		method := host.Field(host.Typ.FieldIndex("Context"))
		fn, err := setup.Load(method.Ref())
		require.NoError(t, err)

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := interp.New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), got)
	})

	t.Run("marshaled function is shared and race-safe across interpreters", func(t *testing.T) {
		setup := interp.New(program.New(nil))
		v, err := setup.Marshal(func(a, b int32) int32 { return a + b })
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		// program.New's default constant path keeps the *HostFunction Go
		// value itself (not a copy) in each Interpreter's heap, so two
		// Interpreters built from programs referencing the same fn share one
		// *HostFunction and race on any call-scoped state it caches.
		fn := v.(*interp.HostFunction)

		prog1 := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 1),
				instr.New(instr.I32_CONST, 2),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)
		prog2 := program.New(
			[]instr.Instruction{
				instr.New(instr.I32_CONST, 10),
				instr.New(instr.I32_CONST, 20),
				instr.New(instr.CONST_GET, 0),
				instr.New(instr.CALL),
			},
			program.WithConstants(fn),
		)

		i1 := interp.New(prog1)
		defer i1.Close()
		i2 := interp.New(prog2)
		defer i2.Close()

		var wg sync.WaitGroup
		var err1, err2 error
		var v1, v2 types.Value
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err1 = i1.Run(context.Background()); err1 == nil {
				v1, err1 = i1.Pop()
			}
		}()
		go func() {
			defer wg.Done()
			if err2 = i2.Run(context.Background()); err2 == nil {
				v2, err2 = i2.Pop()
			}
		}()
		wg.Wait()

		require.NoError(t, err1)
		require.NoError(t, err2)
		require.Equal(t, types.I32(3), v1)
		require.Equal(t, types.I32(30), v2)
	})
	t.Run("named scalar and pointers", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		type count int32
		n := count(7)
		value, err := i.Marshal(&n)
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)

		var nilCount *count
		value, err = i.Marshal(nilCount)
		require.NoError(t, err)
		require.Equal(t, types.Ref(0), value)
	})

	t.Run("nested collections", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		src := struct {
			Name   string
			Values []int32
			Fixed  [2]uint16
			Lookup map[string]int64
		}{
			Name:   "vm",
			Values: []int32{1, 2, 3},
			Fixed:  [2]uint16{4, 5},
			Lookup: map[string]int64{"x": 6},
		}

		value, err := i.Marshal(src)
		require.NoError(t, err)
		var dst struct {
			Name   string
			Values []int32
			Fixed  [2]uint16
			Lookup map[string]int64
		}
		require.NoError(t, i.Unmarshal(value, &dst))
		require.Equal(t, src, dst)
	})

	t.Run("custom value marshaler", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		value, err := i.Marshal(marshalCustom(9))
		require.NoError(t, err)
		require.Equal(t, types.I32(9), value)
	})

	t.Run("builtin converter", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		src := time.Unix(1, 2)

		value, err := i.Marshal(src)
		require.NoError(t, err)
		require.Equal(t, types.I64(src.UnixNano()), value)
	})

	t.Run("host object", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		value, err := i.Marshal(marshalHostFields{Count: 7})
		require.NoError(t, err)
		host, ok := value.(*interp.HostObject)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), host.Field(host.Typ.FieldIndex("Count")))
	})

	t.Run("cycle", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		node := &struct{ Next any }{}
		node.Next = node

		_, err := i.Marshal(node)
		require.ErrorIs(t, err, interp.ErrMarshalCycle)
	})

	t.Run("unsupported type", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		_, err := i.Marshal(make(chan int))
		require.ErrorIs(t, err, interp.ErrUnsupportedMarshalType)
	})

}

func TestInterpreter_Unmarshal(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		var dst int32
		require.NoError(t, i.Unmarshal(types.I32(7), &dst))
		require.Equal(t, int32(7), dst)
	})

	t.Run("VM function", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()

		var add func(int32, int32) (int32, error)
		require.NoError(t, i.Unmarshal(fn, &add))
		got, err := add(2, 3)
		require.NoError(t, err)
		require.Equal(t, int32(5), got)
	})

	t.Run("VM function with context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeI32}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN)).MustBuild()

		var add func(context.Context, int32, int32) (int32, error)
		require.NoError(t, i.Unmarshal(fn, &add))
		got, err := add(context.Background(), 2, 3)
		require.NoError(t, err)
		require.Equal(t, int32(5), got)
	})

	t.Run("VM function context identity", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(types.BoxRef(addr), &call))
		ctx := context.WithValue(context.Background(), marshalContextKey(0), "value")
		value, err := call(ctx)
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("VM function canceled context", func(t *testing.T) {
		i := interp.New(program.New(nil), interp.WithTick(1))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()

		var call func(context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(fn, &call))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := call(ctx)
		require.ErrorIs(t, err, context.Canceled)
	})

	t.Run("VM function nil context uses background", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(types.BoxRef(addr), &call))
		value, err := call(nil)
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, context.Background(), got)
	})

	t.Run("VM function non-first context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()

		var call func(int32, context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(fn, &call))
		got, err := call(7, nil)
		require.NoError(t, err)
		require.Equal(t, int32(7), got)
	})

	t.Run("VM closure with context", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		fnAddr, err := i.Alloc(fn)
		require.NoError(t, err)
		closureAddr, err := i.Alloc(types.NewClosure(fn.Typ, types.Ref(fnAddr), nil))
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(closureAddr)) }()

		var call func(context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(types.BoxRef(closureAddr), &call))
		got, err := call(context.Background())
		require.NoError(t, err)
		require.Equal(t, int32(7), got)
	})

	t.Run("VM function without context uses background", func(t *testing.T) {
		var got context.Context
		i := interp.New(program.New(nil), interp.WithTick(2), interp.WithHook(func(i *interp.Interpreter) error {
			got = i.Context()
			return nil
		}))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.NOP), instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)
		defer func() { require.NoError(t, i.Release(addr)) }()

		var call func() (int32, error)
		require.NoError(t, i.Unmarshal(types.BoxRef(addr), &call))
		value, err := call()
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, context.Background(), got)
	})

	t.Run("function ref", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN),
		).MustBuild()
		addr, err := i.Alloc(fn)
		require.NoError(t, err)

		var call func() int32
		require.NoError(t, i.Unmarshal(types.BoxRef(addr), &call))
		require.Equal(t, int32(7), call())
		require.NoError(t, i.Release(addr))
	})

	t.Run("runtime error", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_DIV_S), instr.New(instr.RETURN),
		).MustBuild()

		var call func() (int32, error)
		require.NoError(t, i.Unmarshal(fn, &call))
		got, err := call()
		require.Zero(t, got)
		require.ErrorIs(t, err, interp.ErrDivideByZero)

		got, err = call()
		require.Zero(t, got)
		require.ErrorIs(t, err, interp.ErrDivideByZero)
	})

	t.Run("signature mismatch", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).MustBuild()

		var call func() int64
		require.ErrorIs(t, i.Unmarshal(fn, &call), interp.ErrTypeMismatch)
	})
	t.Run("custom value unmarshaler", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		var dst marshalCustom

		require.NoError(t, i.Unmarshal(types.I32(11), &dst))
		require.Equal(t, marshalCustom(11), dst)
	})

	t.Run("builtin converter", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		var dst time.Time

		require.NoError(t, i.Unmarshal(types.I64(123), &dst))
		require.Equal(t, time.Unix(0, 123), dst)
	})

	t.Run("host object receiver", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(marshalHostFields{Count: 7})
		require.NoError(t, err)
		var dst marshalHostFields

		require.NoError(t, i.Unmarshal(value, &dst))
		require.Equal(t, marshalHostFields{Count: 7}, dst)
	})

	t.Run("invalid target", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Unmarshal(types.I32(1), nil), interp.ErrInvalidUnmarshalTarget)
		require.ErrorIs(t, i.Unmarshal(types.I32(1), int32(0)), interp.ErrInvalidUnmarshalTarget)
		var dst *int32
		require.ErrorIs(t, i.Unmarshal(types.I32(1), dst), interp.ErrInvalidUnmarshalTarget)
	})

	t.Run("overflow", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		var dst int8

		require.ErrorIs(t, i.Unmarshal(types.I64(256), &dst), interp.ErrValueOverflow)
	})

	t.Run("type mismatch", func(t *testing.T) {
		i := interp.New(program.New(nil))
		defer i.Close()
		var dst int32

		require.ErrorIs(t, i.Unmarshal(types.String("x"), &dst), interp.ErrTypeMismatch)
	})

}

type marshalBenchData struct {
	Count int32
	Ratio float64
	Name  string
	Flag  bool
}

type marshalBenchMethods struct {
	Count  int32
	hidden int32
}

func (v *marshalBenchMethods) Bump(n int32) int32 {
	v.Count += n
	v.hidden++
	return v.Count
}

// BenchmarkInterpreter_Marshal records the per-shape cost of the reflection
// codec. Every iteration resets the interpreter so the heap stays at one
// conversion's worth; that reset cost is identical across runs, so it does not
// disturb a before/after comparison.
func BenchmarkInterpreter_Marshal(b *testing.B) {
	elems := make([]int32, 64)
	for idx := range elems {
		elems[idx] = int32(idx)
	}
	entries := make(map[string]int32, 16)
	for idx := range 16 {
		entries[string(rune('a'+idx))] = int32(idx)
	}

	for _, tt := range []struct {
		name  string
		value any
	}{
		{name: "scalar", value: int32(7)},
		{name: "struct", value: marshalBenchData{Count: 7, Ratio: 2.5, Name: "x", Flag: true}},
		{name: "slice", value: elems},
		{name: "map", value: entries},
		{name: "methods", value: &marshalBenchMethods{Count: 7}},
	} {
		b.Run(tt.name, func(b *testing.B) {
			i := interp.New(program.New(nil))
			b.Cleanup(func() { require.NoError(b, i.Close()) })

			_, err := i.Marshal(tt.value)
			require.NoError(b, err)
			i.Reset()

			b.ReportAllocs()
			for b.Loop() {
				if _, err := i.Marshal(tt.value); err != nil {
					b.Fatal(err)
				}
				i.Reset()
			}
		})
	}
}

// BenchmarkInterpreter_Unmarshal records the reverse direction over the same
// shapes. The source value is marshaled once outside the loop, so only decode
// cost is measured; the destination is reused because Unmarshal overwrites it.
func BenchmarkInterpreter_Unmarshal(b *testing.B) {
	elems := make([]int32, 64)
	for idx := range elems {
		elems[idx] = int32(idx)
	}
	entries := make(map[string]int32, 16)
	for idx := range 16 {
		entries[string(rune('a'+idx))] = int32(idx)
	}

	for _, tt := range []struct {
		name  string
		value any
		dst   any
	}{
		{name: "scalar", value: int32(7), dst: new(int32)},
		{name: "struct", value: marshalBenchData{Count: 7, Ratio: 2.5, Name: "x", Flag: true}, dst: new(marshalBenchData)},
		{name: "slice", value: elems, dst: new([]int32)},
		{name: "map", value: entries, dst: new(map[string]int32)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			i := interp.New(program.New(nil))
			b.Cleanup(func() { require.NoError(b, i.Close()) })

			value, err := i.Marshal(tt.value)
			require.NoError(b, err)
			require.NoError(b, i.Unmarshal(value, tt.dst))

			b.ReportAllocs()
			for b.Loop() {
				if err := i.Unmarshal(value, tt.dst); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

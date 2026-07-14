package interp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

type marshalCount int32

type marshalRecord struct {
	Name   string
	Values []int32
	Fixed  [2]uint16
	Lookup map[string]int64
}

type marshalCustom int32

func (v marshalCustom) MarshalVM(*Interpreter) (types.Value, error) {
	return types.I32(v), nil
}

func (v *marshalCustom) UnmarshalVM(_ *Interpreter, value types.Value) error {
	n, ok := value.(types.I32)
	if !ok {
		return ErrTypeMismatch
	}
	*v = marshalCustom(n)
	return nil
}

type marshalNode struct {
	Next *marshalNode
}

func TestInterpreter_Marshal(t *testing.T) {
	t.Run("scalar value", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		v, err := i.Marshal(int32(7))
		require.NoError(t, err)
		require.Equal(t, types.I32(7), v)
	})

	t.Run("function receives active context", func(t *testing.T) {
		var got context.Context
		setup := New(program.New(nil))
		fn, err := setup.Marshal(func(ctx context.Context) int32 {
			got = ctx
			return 7
		})
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), contextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		value, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("host method receives active context", func(t *testing.T) {
		setup := New(program.New(nil))
		defer setup.Close()
		value, err := setup.Marshal(contextHost{})
		require.NoError(t, err)
		host := value.(*HostObject)
		method := host.Field(host.Typ.FieldIndex("Value"))
		fn, err := setup.Load(method.Ref())
		require.NoError(t, err)

		prog := program.New([]instr.Instruction{instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)}, program.WithConstants(fn))
		i := New(prog)
		defer i.Close()
		ctx := context.WithValue(context.Background(), contextKey(0), "value")
		require.NoError(t, i.Run(ctx))
		got, err := i.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), got)
	})

	t.Run("marshaled function is shared and race-safe across interpreters", func(t *testing.T) {
		setup := New(program.New(nil))
		v, err := setup.Marshal(func(a, b int32) int32 { return a + b })
		require.NoError(t, err)
		require.NoError(t, setup.Close())

		// program.New's default constant path keeps the *HostFunction Go
		// value itself (not a copy) in each Interpreter's heap, so two
		// Interpreters built from programs referencing the same fn share one
		// *HostFunction and race on any call-scoped state it caches.
		fn := v.(*HostFunction)

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

		i1 := New(prog1)
		defer i1.Close()
		i2 := New(prog2)
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
		i := New(program.New(nil))
		defer i.Close()

		count := marshalCount(7)
		value, err := i.Marshal(&count)
		require.NoError(t, err)
		require.Equal(t, types.I32(7), value)

		var nilCount *marshalCount
		value, err = i.Marshal(nilCount)
		require.NoError(t, err)
		require.Equal(t, types.Ref(0), value)
	})

	t.Run("nested collections", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		src := marshalRecord{
			Name:   "vm",
			Values: []int32{1, 2, 3},
			Fixed:  [2]uint16{4, 5},
			Lookup: map[string]int64{"x": 6},
		}

		value, err := i.Marshal(src)
		require.NoError(t, err)
		var dst marshalRecord
		require.NoError(t, i.Unmarshal(value, &dst))
		require.Equal(t, src, dst)
	})

	t.Run("custom value marshaler", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value, err := i.Marshal(marshalCustom(9))
		require.NoError(t, err)
		require.Equal(t, types.I32(9), value)
	})

	t.Run("builtin converter", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		src := time.Unix(1, 2)

		value, err := i.Marshal(src)
		require.NoError(t, err)
		require.Equal(t, types.I64(src.UnixNano()), value)
	})

	t.Run("host object", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		value, err := i.Marshal(hostCounter{Count: 7})
		require.NoError(t, err)
		host, ok := value.(*HostObject)
		require.True(t, ok)
		require.Equal(t, types.BoxI32(7), host.Field(host.Typ.FieldIndex("Count")))
	})

	t.Run("cycle", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		node := &marshalNode{}
		node.Next = node

		_, err := i.Marshal(node)
		require.ErrorIs(t, err, ErrMarshalCycle)
	})

	t.Run("unsupported type", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		_, err := i.Marshal(make(chan int))
		require.ErrorIs(t, err, ErrUnsupportedMarshalType)
	})

}

func TestInterpreter_Unmarshal(t *testing.T) {
	t.Run("scalar", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		var dst int32
		require.NoError(t, i.Unmarshal(types.I32(7), &dst))
		require.Equal(t, int32(7), dst)
	})

	t.Run("VM function", func(t *testing.T) {
		i := New(program.New(nil))
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
		i := New(program.New(nil))
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
		i := New(program.New(nil), WithTick(2), WithHook(func(i *Interpreter) error {
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
		ctx := context.WithValue(context.Background(), contextKey(0), "value")
		value, err := call(ctx)
		require.NoError(t, err)
		require.Equal(t, int32(7), value)
		require.Equal(t, ctx, got)
	})

	t.Run("VM function canceled context", func(t *testing.T) {
		i := New(program.New(nil), WithTick(1))
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
		i := New(program.New(nil), WithTick(2), WithHook(func(i *Interpreter) error {
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
		i := New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.TypeI32, types.TypeRef}, Returns: []types.Type{types.TypeI32},
		}).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()

		var call func(int32, context.Context) (int32, error)
		require.NoError(t, i.Unmarshal(fn, &call))
		got, err := call(7, nil)
		require.NoError(t, err)
		require.Equal(t, int32(7), got)
	})

	t.Run("VM closure with context", func(t *testing.T) {
		i := New(program.New(nil))
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
		i := New(program.New(nil), WithTick(2), WithHook(func(i *Interpreter) error {
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
		i := New(program.New(nil))
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
		i := New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Emit(
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_DIV_S), instr.New(instr.RETURN),
		).MustBuild()

		var call func() (int32, error)
		require.NoError(t, i.Unmarshal(fn, &call))
		got, err := call()
		require.Zero(t, got)
		require.ErrorIs(t, err, ErrDivideByZero)

		got, err = call()
		require.Zero(t, got)
		require.ErrorIs(t, err, ErrDivideByZero)
	})

	t.Run("signature mismatch", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).MustBuild()

		var call func() int64
		require.ErrorIs(t, i.Unmarshal(fn, &call), ErrTypeMismatch)
	})
	t.Run("custom value unmarshaler", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		var dst marshalCustom

		require.NoError(t, i.Unmarshal(types.I32(11), &dst))
		require.Equal(t, marshalCustom(11), dst)
	})

	t.Run("builtin converter", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		var dst time.Time

		require.NoError(t, i.Unmarshal(types.I64(123), &dst))
		require.Equal(t, time.Unix(0, 123), dst)
	})

	t.Run("host object receiver", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		value, err := i.Marshal(hostCounter{Count: 7})
		require.NoError(t, err)
		var dst hostCounter

		require.NoError(t, i.Unmarshal(value, &dst))
		require.Equal(t, hostCounter{Count: 7}, dst)
	})

	t.Run("invalid target", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()

		require.ErrorIs(t, i.Unmarshal(types.I32(1), nil), ErrInvalidUnmarshalTarget)
		require.ErrorIs(t, i.Unmarshal(types.I32(1), int32(0)), ErrInvalidUnmarshalTarget)
		var dst *int32
		require.ErrorIs(t, i.Unmarshal(types.I32(1), dst), ErrInvalidUnmarshalTarget)
	})

	t.Run("overflow", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		var dst int8

		require.ErrorIs(t, i.Unmarshal(types.I64(256), &dst), ErrValueOverflow)
	})

	t.Run("type mismatch", func(t *testing.T) {
		i := New(program.New(nil))
		defer i.Close()
		var dst int32

		require.ErrorIs(t, i.Unmarshal(types.String("x"), &dst), ErrTypeMismatch)
	})

}

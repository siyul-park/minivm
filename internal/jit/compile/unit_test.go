package compile_test

import (
	"runtime"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/arm64"
	"github.com/siyul-park/minivm/internal/jit/compile"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/transform"
	"github.com/siyul-park/minivm/types"
)

// stub is a Machine whose methods must never run: every case in TestCompile
// is refused before Compile reaches the backend.
type stub struct{}

func (stub) Arch() asm.Arch                                                   { panic("unused") }
func (stub) Reserve() []asm.PReg                                              { panic("unused") }
func (stub) Prologue(*asm.Assembler, []types.Kind, int)                       { panic("unused") }
func (stub) Epilogue(*asm.Assembler)                                          { panic("unused") }
func (stub) Lower(*asm.Assembler, ssa.Operation, compile.Site) bool           { panic("unused") }
func (stub) Branch(*asm.Assembler, ssa.Terminator, compile.Site, []asm.Label) { panic("unused") }
func (stub) Return(*asm.Assembler, ssa.Terminator, compile.Site)              { panic("unused") }
func (stub) Budget(*asm.Assembler, asm.Label)                                 { panic("unused") }
func (stub) Exit(*asm.Assembler, int, jit.Kind, []asm.VReg)                   { panic("unused") }
func (stub) Results(*asm.Assembler, []asm.VReg)                               { panic("unused") }
func (stub) Call(*asm.Assembler, compile.Call, compile.Site) bool             { panic("unused") }
func (stub) Move(*asm.Assembler, asm.VReg, asm.VReg)                          { panic("unused") }

// noop is a function of one RETURN and no parameters: valid enough for
// translation and verification to succeed.
func noop(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{Typ: &types.FunctionType{}, Code: instr.Marshal(code)}
}

// sum is sum(n) = 0 + 1 + ... + n-1 over one parameter and two locals.
func sum(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32, types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// fibonacci is fib(n) = n < 2 ? n : fib(n-1) + fib(n-2), at address 2,
// calling itself through constant 0.
func fibonacci(t *testing.T) (*types.Function, transform.Module) {
	t.Helper()
	b := instr.NewBuilder()
	small := b.Label()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_LT_S).BrIf(small)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.I32_ADD).Emit(instr.RETURN)
	b.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	fib := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
	return fib, transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: fib}}}
}

// run compiles u with m, publishes it in a Store of its own natives table,
// and runs it over stack.
func run(t *testing.T, u compile.Unit, stack []types.Boxed) (*jit.Context, jit.Trap) {
	t.Helper()
	c, err := compile.Compile(u, arm64.New())
	require.NoError(t, err)

	store := jit.NewStore(u.Address + 1)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	require.True(t, store.Publish(c))

	// rc backs every object CALL retains and releases across the run; index
	// u.Address holds the function's own reference count, high enough that
	// a recursive self-call's nested retain/release pairs never reach zero.
	rc := make([]int, u.Address+1)
	rc[u.Address] = 1

	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))
	ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
	ctx.Limit = uint64(len(ctx.Records))
	ctx.Budget = 1 << 20
	ctx.Natives = store.Natives()
	ctx.RC = uintptr(unsafe.Pointer(&rc[0]))

	trap := jit.Enter(c.Entry(), ctx)
	runtime.KeepAlive(rc)
	return ctx, trap
}

// native skips a case that runs native code off arm64.
func native(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native execution needs arm64")
	}
}

func TestCompile(t *testing.T) {
	t.Run("rejects a function translation cannot express", func(t *testing.T) {
		u := compile.Unit{Function: &types.Function{}, Tier: jit.Baseline}
		_, err := compile.Compile(u, stub{})
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("rejects an unknown tier", func(t *testing.T) {
		u := compile.Unit{Function: noop(t)}
		_, err := compile.Compile(u, stub{})
		require.ErrorIs(t, err, compile.ErrUnsupported)
	})

	t.Run("runs the sum loop at every tier", func(t *testing.T) {
		native(t)
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			stack := []types.Boxed{types.BoxI32(10), 0, 0}
			ctx, trap := run(t, compile.Unit{Address: 1, Function: sum(t), Tier: tier}, stack)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(45), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})

	t.Run("runs fib through its own native code at every tier", func(t *testing.T) {
		native(t)
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			fib, module := fibonacci(t)
			stack := make([]types.Boxed, 64)
			stack[0] = types.BoxI32(15)
			ctx, trap := run(t, compile.Unit{Address: 2, Function: fib, Module: module, Tier: tier}, stack)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(610), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})
}

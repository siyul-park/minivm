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

func (stub) Arch() asm.Arch      { panic("unused") }
func (stub) Reserve() []asm.PReg { panic("unused") }
func (stub) Prologue(*asm.Assembler, []types.Kind, int, bool, int, []types.Kind, []types.Kind) []asm.VReg {
	panic("unused")
}
func (stub) Epilogue(*asm.Assembler)                                          { panic("unused") }
func (stub) Enter(*asm.Assembler, []types.Kind, []types.Kind) asm.Label       { panic("unused") }
func (stub) Lower(*asm.Assembler, ssa.Operation, compile.Site) bool           { panic("unused") }
func (stub) Branch(*asm.Assembler, ssa.Terminator, compile.Site, []asm.Label) { panic("unused") }
func (stub) Return(*asm.Assembler, ssa.Terminator, compile.Site)              { panic("unused") }
func (stub) Budget(*asm.Assembler, asm.Label)                                 { panic("unused") }
func (stub) Exit(*asm.Assembler, int, jit.Kind, []asm.VReg)                   { panic("unused") }
func (stub) Spill(*asm.Assembler, asm.VReg, int)                              { panic("unused") }
func (stub) Results(*asm.Assembler, []asm.VReg)                               { panic("unused") }
func (stub) Call(*asm.Assembler, compile.Call, compile.Site) bool             { panic("unused") }
func (stub) Move(*asm.Assembler, asm.VReg, asm.VReg)                          { panic("unused") }
func (stub) Const(*asm.Assembler, asm.VReg, types.Boxed) bool                 { panic("unused") }

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

// identity is ident(n) = n over one i32 parameter.
func identity(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
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

// run compiles u and every callee with m, publishes them in a Store of
// their own natives table, and runs u over stack.
func run(t *testing.T, u compile.Unit, stack []types.Boxed, callees ...compile.Unit) (*jit.Context, jit.Trap) {
	t.Helper()
	size := u.Address + 1
	for _, callee := range callees {
		size = max(size, callee.Address+1)
	}
	store := jit.NewStore(size)
	t.Cleanup(func() { require.NoError(t, store.Close()) })
	var c *jit.Code
	for _, unit := range append([]compile.Unit{u}, callees...) {
		code, err := compile.Compile(unit, arm64.New())
		require.NoError(t, err)
		require.True(t, store.Publish(code))
		if c == nil {
			c = code
		}
	}

	// rc backs every object CALL retains and releases across the run; each
	// function's own reference count is high enough that a recursive self-
	// call's nested retain/release pairs never reach zero.
	rc := make([]int, size)
	for i := range rc {
		rc[i] = 1
	}
	// entries backs each prologue's own entry count.
	entries := make([]int64, size)

	ctx, err := jit.NewContext(4096)
	require.NoError(t, err)
	ctx.FB = uintptr(unsafe.Pointer(&stack[0]))
	ctx.Top = ctx.FB + uintptr(len(stack))*unsafe.Sizeof(stack[0])
	ctx.Limit = uint64(len(ctx.Records))
	ctx.Budget = 1 << 20
	ctx.Natives = store.Natives()
	ctx.RC = uintptr(unsafe.Pointer(&rc[0]))
	ctx.Entries = uintptr(unsafe.Pointer(&entries[0]))

	trap := jit.Enter(c.Entry(), ctx)
	runtime.KeepAlive(rc)
	runtime.KeepAlive(entries)
	return ctx, trap
}

// native skips a case that runs native code off arm64.
func native(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native execution needs arm64")
	}
}

func TestCompile(t *testing.T) {
	t.Run("counts Baseline function entries", func(t *testing.T) {
		m := new(machine)
		c, err := compile.Compile(compile.Unit{Function: noop(t), Tier: jit.Baseline}, m)
		require.NoError(t, err)
		require.NoError(t, c.Free())
		require.True(t, m.count)
	})

	t.Run("does not count Optimized function entries", func(t *testing.T) {
		m := new(machine)
		c, err := compile.Compile(compile.Unit{Function: noop(t), Tier: jit.Optimized}, m)
		require.NoError(t, err)
		require.NoError(t, c.Free())
		require.False(t, m.count)
	})

	t.Run("does not count OSR entries", func(t *testing.T) {
		m := new(machine)
		c, err := compile.Compile(compile.Unit{Function: noop(t), Tier: jit.Baseline, OSR: true}, m)
		require.NoError(t, err)
		require.NoError(t, c.Free())
		require.False(t, m.count)
	})

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

	t.Run("compiles and runs an OSR unit at every tier", func(t *testing.T) {
		native(t)
		// acc lives on the operand stack across the header; n (the
		// function's own parameter) and bonus (an ordinary local) stay in
		// VM slots throughout.
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32, types.TypeI32},
			Code:   instr.Marshal(code),
		}
		entry := instr.New(instr.I32_CONST, 0).Width()*2 + instr.New(instr.LOCAL_SET, 1).Width()

		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			// n=5, i=3 (partway through the loop), bonus=100, acc=3 (the
			// operand-stack value the interpreter left at the header).
			stack := []types.Boxed{types.BoxI32(5), types.BoxI32(3), types.BoxI32(100), types.BoxI32(3)}
			u := compile.Unit{Address: 1, Function: fn, Tier: tier, Entry: entry, OSR: true}
			ctx, trap := run(t, u, stack)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(105), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})

	t.Run("keeps module code's own completion value count", func(t *testing.T) {
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.I32_CONST, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Code: instr.Marshal(code)}

		c, err := compile.Compile(compile.Unit{Function: fn, Tier: jit.Baseline}, arm64.New())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })
		require.Equal(t, 2, c.Results)
	})

	t.Run("leaves an ordinary function's completion value count at zero", func(t *testing.T) {
		c, err := compile.Compile(compile.Unit{Function: noop(t), Tier: jit.Baseline}, arm64.New())
		require.NoError(t, err)
		t.Cleanup(func() { require.NoError(t, c.Free()) })
		require.Zero(t, c.Results)
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

	t.Run("rereads a register-passed parameter after a call spills it", func(t *testing.T) {
		native(t)
		// f(n) = ident(n) + n: n's incoming register value stays live across
		// the call, so the allocator spills it right where the prologue
		// captures it.
		ident := compile.Unit{Address: 2, Function: identity(t), Tier: jit.Baseline}
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_ADD).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(code),
		}
		module := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: ident.Function}}}
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			stack := make([]types.Boxed, 64)
			stack[0] = types.BoxI32(21)
			ctx, trap := run(t, compile.Unit{Address: 1, Function: fn, Module: module, Tier: tier}, stack, ident)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(42), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})

	t.Run("rereads a register-passed parameter a loop stores to", func(t *testing.T) {
		native(t)
		// f(n) counts n down to zero in its own slot, counting iterations.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_LE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(code),
		}
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			stack := []types.Boxed{types.BoxI32(5), 0}
			ctx, trap := run(t, compile.Unit{Address: 1, Function: fn, Tier: tier}, stack)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(5), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})

	t.Run("passes each scalar parameter kind in registers through the Go entry and a native call", func(t *testing.T) {
		native(t)
		for _, c := range []struct {
			typ  types.Type
			x, y types.Boxed
		}{
			{types.TypeI1, types.BoxI1(false), types.BoxI1(true)},
			{types.TypeI8, types.BoxI8(3), types.BoxI8(-5)},
			{types.TypeI32, types.BoxI32(3), types.BoxI32(-7)},
			{types.TypeF32, types.BoxF32(1.5), types.BoxF32(-2.25)},
			{types.TypeF64, types.BoxF64(1.5), types.BoxF64(-2.25)},
		} {
			typ := &types.FunctionType{Params: []types.Type{c.typ, c.typ}, Returns: []types.Type{c.typ}}
			// g(x, y) = y; f(x, y) = g(x, y): both registers cross both entries.
			b := instr.NewBuilder()
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
			code, err := b.Assemble()
			require.NoError(t, err)
			g := compile.Unit{Address: 2, Function: &types.Function{Typ: typ, Code: instr.Marshal(code)}, Tier: jit.Baseline}
			b = instr.NewBuilder()
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
			code, err = b.Assemble()
			require.NoError(t, err)
			f := &types.Function{Typ: typ, Code: instr.Marshal(code)}
			module := transform.Module{Constants: []types.Boxed{types.BoxRef(2)}, Objects: transform.Objects{2: {Function: g.Function}}}
			for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
				stack := make([]types.Boxed, 64)
				stack[0], stack[1] = c.x, c.y
				_, trap := run(t, compile.Unit{Address: 1, Function: f, Module: module, Tier: tier}, stack, g)
				require.Equal(t, jit.TrapReturn, trap, c.typ.String())
				require.Equal(t, c.y, stack[0], c.typ.String())
			}
		}
	})

	t.Run("rereads a register-passed parameter stored before the read", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(code),
		}
		for _, tier := range []jit.Tier{jit.Baseline, jit.Optimized} {
			stack := []types.Boxed{types.BoxI32(7)}
			ctx, trap := run(t, compile.Unit{Address: 1, Function: fn, Tier: tier}, stack)
			require.Equal(t, jit.TrapReturn, trap)
			require.Equal(t, types.BoxI32(8), stack[0])
			require.Zero(t, ctx.Depth)
		}
	})
}

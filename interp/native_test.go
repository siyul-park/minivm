package interp_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// fibFunction is fib(n) = n < 2 ? n : fib(n-1) + fib(n-2), calling itself
// through constant 0.
func fibFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	small := b.Label()
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).BrIf(small)
	b.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	b.Bind(small).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// fibCallsProgram calls fib(15) calls times, module locals [0] the counter
// and [1] the running sum, and leaves the sum on the stack.
func fibCallsProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithConstants(fib))
}

// fibFlatCallsProgram calls fib(15) calls times through straight-line module
// code, no back edge anywhere: unlike fibCallsProgram's own driving loop,
// module code here is never a loop header, so it can never itself OSR-enter
// and start dispatching fib's own calls through native-to-native CALLs,
// which promote's own entry counter (fed only by the interpreter's own
// CALL dispatch) would never see.
func fibFlatCallsProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	for range calls {
		b.Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	}
	b.Emit(instr.I32_CONST, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(fib))
}

// indirectFibFunction is fib(n, self) = n < 2 ? n : fib(n-1, self)(n-1, self)
// + fib(n-2, self)(n-2, self), calling itself through param 1 (the callee
// arrives on the stack, not by CONST_GET) instead of constant 0.
func indirectFibFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
	small := b.Label()
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).BrIf(small)
	b.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
		instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	b.Bind(small).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// indirectFibCallsProgram calls indirectFibFunction(n, self) calls times,
// module locals [0] the counter and [1] the running sum, and leaves the sum
// on the stack.
func indirectFibCallsProgram(t *testing.T, n, calls int) *program.Program {
	t.Helper()
	fib := indirectFibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithConstants(fib))
}

// incFunction returns its one parameter plus one.
func incFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
	return b.MustBuild()
}

// decFunction returns its one parameter minus one.
func decFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.RETURN))
	return b.MustBuild()
}

// applyFunction calls fn(x) through param 1, a dynamic CALL.
func applyFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
	return b.MustBuild()
}

// applyProgram calls apply(i, inc) warm times, then apply(i, dec) warm times,
// through the same dynamic CALL site: a polymorphic callee at one ip.
// Constants are [apply, inc, dec]. Module locals [0] the counter and [1] the
// running sum, left on the stack.
func applyProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	for _, callee := range []uint64{1, 2} {
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, callee).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
	}
	b.Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
		program.WithConstants(applyFunction(), incFunction(), decFunction()))
}

// applyGlobalProgram reads global 1 (the caller's selector: 0 for inc, 1 for
// dec) once, stores the matching constant-pool address into global 0, then
// calls apply(i, global 0) calls times through the same dynamic CALL site,
// the callee read fresh from global 0 on every call. Constants are [apply,
// inc, dec]. Module locals [0] the counter and [1] the running sum, left on
// the stack. The constant-pool address survives Reset, unlike a heap Alloc,
// so repeated rounds observe the same callee address for the same selector.
// aFunction calls apply(n, global 0) through its own native-to-native CALL:
// global 0 is an owned (retained, then released) argument at apply's own
// borrowed param 1.
func aFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN))
	return b.MustBuild()
}

// lentProgram calls a(i) warm times through global 0 = inc, then warm times
// through global 0 = dec, the same polymorphic feedback and eventual refute
// applyProgram exercises, but one native-to-native call deeper: a's own call
// into apply lends its global-backed argument to apply's borrowed param 1,
// so a refute's deopt rebuilds two native activations, not one. Constants
// are [apply, a, inc, dec]. Module locals [0] the counter and [1] the
// running sum, left on the stack.
func lentProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	for _, callee := range []uint64{2, 3} {
		b.Emit(instr.CONST_GET, callee).Emit(instr.GLOBAL_SET, 0)
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
	}
	b.Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithGlobals(types.TypeAny),
		program.WithConstants(applyFunction(), aFunction(), incFunction(), decFunction()))
}

func applyGlobalProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	useDec, selected := b.Label(), b.Label()
	b.Emit(instr.GLOBAL_GET, 1).BrIf(useDec)
	b.Emit(instr.CONST_GET, 1)
	b.Br(selected)
	b.Bind(useDec).Emit(instr.CONST_GET, 2)
	b.Bind(selected).Emit(instr.GLOBAL_SET, 0)

	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithGlobals(types.TypeAny, types.TypeI32),
		program.WithConstants(applyFunction(), incFunction(), decFunction()))
}

// wrapFunction calls a closure through param 1, a dynamic CALL: native never
// records a closure callee (call's own hook only reaches a *types.Function
// target), so this site never speculates.
func wrapFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
	return b.MustBuild()
}

// closureCallsProgram calls wrap(i, closure) calls times, closure a
// zero-capture closure over incFunction created once and kept alive in
// module local [1]. Constants are [inc, wrap]. Module locals [0] the counter
// and [1] the closure, left holding the counter on the stack.
func closureCallsProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(calls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
		program.WithConstants(incFunction(), wrapFunction()))
}

// sumFunction is sum(n) = 0 + 1 + ... + n-1 over one parameter and two locals.
func sumFunction(t *testing.T) *types.Function {
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

// sumWarmProgram calls sum(1) warm times to give an async compile time to
// finish, then calls sum(n) once and leaves its result on the stack.
func sumWarmProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	sum := sumFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
}

// sumTryProgram is sumWarmProgram whose final call repeats forever inside a
// Try region that catches any trap: only a cancellation ends it, and it must
// escape the handler.
func sumTryProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	sum := sumFunction(t)
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Br(start)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum), program.WithHandlers(b.Handlers()...))
}

// dropFunction takes one string parameter and returns 42 without touching
// it, so RETURN releases the parameter slot without the function body ever
// bridging: a fresh, singly-owned string argument dies exactly there.
func dropFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 42).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
}

// outerInnerProgram calls outer(id) warm times, dropping the result each
// time, then calls it once more keeping the result: outer(s) returns
// inner(s) through a CALL of a function only native code calls, so once
// outer runs natively its own CALL takes ExitCall through a zero natives
// entry until the calls the interpreter serves compile inner.
func outerInnerProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()
	outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)).MustBuild()

	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(inner, outer, types.String("chain")))
}

// divFunction returns 10 / n (i32), so a zero divisor traps inside the
// function body threaded, and inside native code as an ExitDeopt at the
// I32_DIV_S check.
func divFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 10).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_DIV_S).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
}

// divCaughtProgram warms divFunction with a nonzero divisor warm times, then
// calls it once more with a zero divisor inside a module-level Try that
// catches the trap and leaves the caught exception's error code on the
// stack. The one local (the warmup counter) is the Try region's entry depth.
func divCaughtProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
}

// divUncaughtProgram is divCaughtProgram without a handler, so the same trap
// escapes Run as an error instead of landing on a guest catch.
func divUncaughtProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
}

// concatFunction returns the STRING_CONCAT of its two parameters. arm64's
// machine lowers no string opcode, so a compiled call to it always bridges:
// the interpreter performs STRING_CONCAT itself once native code exits.
func concatFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
		Code: instr.Marshal(code),
	}
}

// concatProgram warms concatFunction with two throwaway strings warm times,
// then calls it once more and leaves its result on the stack.
func concatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := concatFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithConstants(fn, types.String("bridge-"), types.String("concat")))
}

// node is the struct type structs (below) allocates each iteration: an i32
// counter and an any-typed held reference.
func node() *types.StructType {
	return types.NewStructType(
		types.NewStructField(types.TypeI32, types.FieldWithName("n")),
		types.NewStructField(types.TypeAny, types.FieldWithName("held")),
	)
}

// Bridges struct.new while preserving ownership: each iteration replaces a record
// and releases the previous one. The loop-carried local stays concretely typed so
// SSA can join the back edge; an inner loop provides enough native work to amortize bridges.
func structs(t *testing.T, n int) *program.Program {
	t.Helper()
	record := node()
	b := instr.NewBuilder()
	loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.CONST_GET, 0)
	b.Emit(instr.STRUCT_NEW, 0)
	b.Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(inner)
	b.Bind(innerDone)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.STRUCT_GET)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, record, types.TypeI32), program.WithTypes(record),
		program.WithConstants(types.String("held")))
}

// arrays loops n times at module level, each pass allocating a
// fixed-length i32-typed array.new_default and summing its own length
// before letting it die: a pure-scalar bridge candidate with no ref
// ownership. arm64 lowers no array.new_default, so every native entry
// bridges. The length is a small constant, not the loop counter: a growing
// length would allocate O(n^2) total elements across the loop.
func arrays(t *testing.T, n int) *program.Program {
	t.Helper()
	const length = 4
	elem := types.NewArrayType(types.TypeI32)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, length).Emit(instr.ARRAY_NEW_DEFAULT, 0)
	b.Emit(instr.ARRAY_LEN)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
}

// Warm native execution at the loop header, then trigger the invalid array.new_default.
// The inner work keeps the site from retiring on unamortized bridges; the bridge declines
// the same ErrSegmentationFault that threaded execution reports.
func trap(t *testing.T, warm int) *program.Program {
	t.Helper()
	elem := types.NewArrayType(types.TypeI32)
	b := instr.NewBuilder()
	loop, done, bad, length, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)+1).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_EQ).BrIf(bad)
	b.Emit(instr.I32_CONST, 1).Br(length)
	b.Bind(bad).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
	b.Bind(length)
	b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.ARRAY_LEN).Emit(instr.DROP)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(inner)
	b.Bind(innerDone)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
}

// texts grows a string by "ab" until it is 2n bytes long, re-deriving each
// "ab" through string.encode_utf32 and string.new_utf32 so the ops see
// borrowed, constant, and owned operands, and chains one struct per pass so
// the heap grows. An inner loop amortizes the bridges.
func texts(t *testing.T, n int) *program.Program {
	t.Helper()
	record := node()
	b := instr.NewBuilder()
	loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_LEN).Emit(instr.LOCAL_TEE, 3)
	b.Emit(instr.I32_CONST, uint64(2*n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(inner)
	b.Bind(innerDone)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 1)
	b.Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_CONCAT).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 1).Emit(instr.STRUCT_NEW, 0).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeString, record, types.TypeI32, types.TypeI32), program.WithTypes(record),
		program.WithConstants(types.String(""), types.String("ab")))
}

// dynamicConcatProgram forces the dynamic-CALL entry path by loading the callee
// from a local rather than CONST_GET;CALL fusion. It warms twice, then leaves one result.
func dynamicConcatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := concatFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
		program.WithConstants(fn, types.String("dyn-"), types.String("call")))
}

// store loops a module-level header, overwriting an any local with a ref
// and then an i32 each pass. Native OpStore must release the ref.
func store(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeAny), program.WithConstants(types.String("leaked")))
}

// fibOverflowProgram warms fib with shallow calls, deep enough for recursion
// but shallow enough to fit WithFrame(limit), then calls fib(n) once with n
// deep enough to exceed limit: native recursion always exceeds ctx.Limit
// before the threaded recursion it continues into would itself overflow, so
// an ExitCall served there must reach the identical frame count and
// error a pure threaded run reaches.
func fibOverflowProgram(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib))
}

// caught is fibOverflowProgram with the deep call wrapped
// in a guest handler, so RefCount after Run is comparable across threaded
// and native: land unwinds every frame above the handler and releases the
// operand stack above it, so an uncaught error's abandoned references (which
// neither run's frames are ever cleaned of) cannot appear in the count.
func caught(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
}

// caughtIndirect warms indirectFibFunction(5, self) warm times, then calls
// indirectFibFunction(n, self) inside a try/catch: self is a borrowed,
// local-backed argument at both of indirectFibFunction's own dynamic CALLs,
// and a deep enough n deopts at WithFrame's limit. The guest handler
// swallows the resulting error so RefCount after Run is comparable across
// threaded and native, as caught's own doc explains.
func caughtIndirect(t *testing.T, warm, n int) *program.Program {
	t.Helper()
	fib := indirectFibFunction()
	b := instr.NewBuilder()
	loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
}

// wideI64Function returns n << 50. A literal outside the inline-boxable
// range is not itself translatable (transform.Translate rejects it), so the
// width has to arise from computation: for n=1 the shift's result exceeds
// the inline range.
func wideI64Function(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
		Code: instr.Marshal(code),
	}
}

// wideI64Program warms wideI64Function(1) warm times, dropping its result
// each time, then calls it once more and leaves the result on the stack.
func wideI64Program(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := wideI64Function(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
}

// wideI64FibFunction is recursiveFib's i64 counterpart: its base case adds
// 1<<50, a runtime i64.shl rather than a literal, so every leaf call's
// result is wide, and its own RETURN and every recursive CALL of it are
// register-eligible (a single i64 result).
func wideI64FibFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}})
	small := b.Label()
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_LT_S)).BrIf(small)
	b.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
		instr.New(instr.I64_ADD), instr.New(instr.RETURN),
	)
	b.Bind(small).Emit(
		instr.New(instr.LOCAL_GET, 0),
		instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 50), instr.New(instr.I64_SHL), instr.New(instr.I64_ADD),
		instr.New(instr.RETURN),
	)
	return b.MustBuild()
}

// wideI64Fib calls wideI64FibFunction(n) once, leaving its wide result on
// the stack.
func wideI64Fib(t *testing.T, n int64) *program.Program {
	t.Helper()
	fn := wideI64FibFunction()
	b := instr.NewBuilder()
	b.Emit(instr.I64_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(fn))
}

// wideI64SumFunction sums 0..n-1 in an i32 local through a loop (OSR's own
// entry, not a CALL; the accumulator stays i32 so no i64 local is ever
// loop-carried), converts to i64 and adds 1<<50 only at RETURN, so the OSR
// unit's own result is wide and register-eligible. Params: 0=n. Locals:
// 1=i, 2=sum.
func wideI64SumFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S)
	b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
	b.Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}},
		Locals: []types.Type{types.TypeI32, types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// wideI64SumProgram calls wideI64SumFunction(n) once, leaving its wide
// result on the stack; n's back edges are what drive its loop header past
// OSR's own threshold.
func wideI64SumProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	fn := wideI64SumFunction(t)
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(fn))
}

// fnvFunction hashes [0,n) with FNV-1a64 inside func(i32) i64, seeded by
// op and operand: an i64.const immediate or a const.get of a pool cell.
func fnvFunction(t *testing.T, op instr.Opcode, operand uint64) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(op, operand).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
	b.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}},
		Locals: []types.Type{types.TypeI64, types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// fnvProgram calls fnvFunction(n) once, leaving its wide result on the
// stack; constants follow the function in the pool.
func fnvProgram(t *testing.T, n int, op instr.Opcode, operand uint64, constants ...types.Value) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(append([]types.Value{fnvFunction(t, op, operand)}, constants...)...))
}

// leakArrayFunction keeps a wide i64 constant on the operand stack across
// array.len, then returns it, discarding the length.
func leakArrayFunction(t *testing.T, seed int64) *types.Function {
	t.Helper()
	fn := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI64}})
	fn.Emit(instr.New(instr.I64_CONST, uint64(seed)))
	fn.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.DROP))
	fn.Emit(instr.New(instr.RETURN))
	return fn.MustBuild()
}

// leakArrayProgram warms leakArrayFunction(warm times) over a freshly built
// typed array, so its shape guard specializes and stays under refute's
// retirement count, then calls it once more over global[0] (external test
// code seeds a *HostArray there): exactly that one call's shape guard fails
// and deopts, matching hostArrayGlobalProgram's own mismatch.
func leakArrayProgram(t *testing.T, warm int, seed int64) *program.Program {
	t.Helper()
	fn := leakArrayFunction(t, seed)
	elem := types.NewArrayType(types.TypeI32)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0)
	b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(elem),
		program.WithConstants(fn), program.WithTypes(elem))
}

// wideArgFunction returns its i64 parameter plus one.
func wideArgFunction() *types.Function {
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_ADD).Emit(instr.RETURN)
	code, err := b.Assemble()
	if err != nil {
		panic(err)
	}
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
		Code: instr.Marshal(code),
	}
}

// wideArgProgram calls wideArgFunction(1) warm times, unrolled so the
// module stays threaded, then once with the wide argument 1<<50, leaving
// that result on the stack.
func wideArgProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	fn := wideArgFunction()
	b := instr.NewBuilder()
	for range warm {
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	}
	b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(fn))
}

// wideRelayFunction spins four back edges, then calls wideArgFunction
// (module constant 0) with its i64 parameter widened past 49 bits: the back
// edges amortize the argument slot's box exit, so the caller stays native and
// the wide word crosses Call's register move, never Enter.
func wideRelayFunction() *types.Function {
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
	b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
	code, err := b.Assemble()
	if err != nil {
		panic(err)
	}
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
		Locals: []types.Type{types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// wideRelayProgram warms wideArgFunction, then wideRelayFunction, each warm
// times with a narrow argument, unrolled so the module stays threaded, then
// calls wideRelayFunction(1) once more, leaving its result on the stack.
func wideRelayProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := wideArgFunction()
	outer := wideRelayFunction()
	b := instr.NewBuilder()
	for index := range 2 * warm {
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, uint64(index/warm)).Emit(instr.CALL).Emit(instr.DROP)
	}
	b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(inner, outer))
}

// narrowArgProgram calls wideArgFunction(5) n times in a module loop,
// XOR-accumulating each (narrow) result into module local 0, an i64
// promoted local whose own loop header is OSR-eligible: closing gap 5 (P)
// is what lets this loop, not just its callee, compile natively.
func narrowArgProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	fn := wideArgFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I64_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I64_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_XOR).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI64, types.TypeI32), program.WithConstants(fn))
}

// fnvModuleProgram runs FNV-1a 64 entirely in a module loop (no CALL): h, an
// i64 promoted local, xors each narrow byte index and multiplies by the FNV
// prime every iteration.
func fnvModuleProgram(t *testing.T, n int, seed int64) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I64_CONST, uint64(seed)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
	b.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL)
	b.Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI64, types.TypeI32))
}

// wideStoreLoopProgram stores an increasing wide i64 into a global every
// iteration: a global is never promoted (promote.go handles locals only),
// so a resumable box exit runs on every store, not just once.
func wideStoreLoopProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_TO_I64_S).Emit(instr.I64_ADD)
	b.Emit(instr.GLOBAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.GLOBAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.TypeI64))
}

// rareTrapFunction is h(n) = div(1) + ... + div(1) + div(0) through constant
// div, n calls in all: every call traps on its last iteration only.
func rareTrapFunction(div int) *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
	loop, done := b.Label(), b.Label()
	b.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(done)
	b.Emit(
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.I32_NE),
		instr.New(instr.CONST_GET, uint64(div)), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
	).Br(loop)
	b.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
	return b.MustBuild()
}

// rareTrapProgram calls rareTrapFunction(n) rounds times, each call inside a
// Try region whose handler drops the error. Constants are [h, div]. Module
// locals [0] the counter and [1] the number of caught errors, left on the
// stack.
func rareTrapProgram(t *testing.T, rounds, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
	b.Bind(start).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Bind(end).Br(next)
	b.Bind(catch).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Bind(next)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	b.Try(start, end, catch, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
		program.WithConstants(rareTrapFunction(1), divFunction(t)), program.WithHandlers(b.Handlers()...))
}

// rareNullProgram calls at(a, 0) rounds times, each call inside a Try region
// whose handler drops the error, where a is a null []i32 every 16th round
// and a three-element one otherwise; at counts to 8 before it reads a[0]. Constants are [at, array]. Module
// locals [0] the counter, [1] the number of caught errors (left on the
// stack), [2] the array, and [3] the null.
func rareNullProgram(t *testing.T, rounds int) *program.Program {
	t.Helper()
	at := types.NewFunctionBuilder(&types.FunctionType{
		Params: []types.Type{types.NewArrayType(types.TypeI32), types.TypeI32}, Returns: []types.Type{types.TypeI32},
	}).Locals(types.TypeI32)
	count, counted := at.Label(), at.Label()
	at.Bind(count).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 8), instr.New(instr.I32_GE_S)).BrIf(counted)
	at.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2)).Br(count)
	at.Bind(counted).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN))
	arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

	b := instr.NewBuilder()
	loop, done, live, start, end, catch, next, null, picked := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
	// A read no run reaches keeps the module loop threaded.
	b.Emit(instr.I32_CONST, 1).BrIf(live)
	b.Emit(instr.REF_NULL).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_GET).Emit(instr.DROP)
	b.Bind(live).Bind(start)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 15).Emit(instr.I32_AND).Emit(instr.I32_CONST, 15).Emit(instr.I32_EQ).BrIf(null)
	b.Emit(instr.LOCAL_GET, 2).Br(picked)
	b.Bind(null).Emit(instr.LOCAL_GET, 3)
	b.Bind(picked).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Bind(end).Br(next)
	b.Bind(catch).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Bind(next)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	b.Try(start, end, catch, 4)
	code, err := b.Assemble()
	require.NoError(t, err)
	array := types.NewArrayType(types.TypeI32)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, array, array),
		program.WithConstants(at.MustBuild(), arr), program.WithHandlers(b.Handlers()...))
}

// refuse emits a prologue that reads an element of a null on a path no run
// takes: the translator declines an array read of unknown element kind, so b's
// function never compiles.
func refuse(b *types.FunctionBuilder) {
	body := b.Label()
	b.Emit(instr.New(instr.I32_CONST, 1)).BrIf(body)
	b.Emit(instr.New(instr.REF_NULL), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.DROP))
	b.Bind(body)
}

// heldFunction is held(x) = x; it never compiles, so a native caller always
// reaches it through ExitCall.
func heldFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	refuse(b)
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// servedProgram calls served(x) = held(x) + ... + held(x), n calls through
// constant 1 in loop-free code, once from module code: served's only work is
// its calls, each one served. Constants are [served, held].
func servedProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	served := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	served.Emit(instr.New(instr.I32_CONST, 0))
	for range n {
		served.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.I32_ADD))
	}
	served.Emit(instr.New(instr.RETURN))

	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(served.MustBuild(), heldFunction()))
}

// protectedFunction is f(n) = the sum of 6400 / ((i+1) & 63) for i in [0, n)
// with 1000 added for each i whose divisor is zero: the division sits inside a
// Try region that catches the trap. Local [3] holds a string for the whole
// call, so a leak or a double release shows in the constant's RefCount.
func protectedFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
		Locals(types.TypeI32, types.TypeI32, types.TypeString)
	loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.New(instr.CONST_GET, 1), instr.New(instr.LOCAL_SET, 3))
	b.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(done)
	b.Bind(start).Emit(
		instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 6400),
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 63), instr.New(instr.I32_AND),
		instr.New(instr.I32_DIV_S), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
	b.Bind(end).Br(next)
	b.Bind(catch).Emit(instr.New(instr.DROP), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1000), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
	b.Bind(next).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1)).Br(loop)
	b.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
	b.Try(start, end, catch, 4)
	return b.MustBuild()
}

// protectedProgram calls protectedFunction(n) rounds times and leaves the last
// result on the stack. Constants are [f, string]; module local [0] is the
// counter.
func protectedProgram(t *testing.T, rounds, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(protectedFunction(), types.String("owned")))
}

// throwProgram sums guard(0) + ... + guard(calls-1) in loop-free module code.
// guard(x) calls thrower(x) inside a Try region whose handler returns the
// length of the caught string; thrower(x) throws a string constant when x's
// low four bits are all set and returns x otherwise. Constants are
// [guard, thrower, string].
func throwProgram(t *testing.T, calls int) *program.Program {
	t.Helper()
	thrower := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	raise := thrower.Label()
	thrower.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 15), instr.New(instr.I32_AND), instr.New(instr.I32_CONST, 15), instr.New(instr.I32_EQ)).BrIf(raise)
	thrower.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	thrower.Bind(raise).Emit(instr.New(instr.CONST_GET, 2), instr.New(instr.THROW))

	guard := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	start, end, catch := guard.Label(), guard.Label(), guard.Label()
	guard.Bind(start).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL))
	guard.Bind(end).Emit(instr.New(instr.RETURN))
	guard.Bind(catch).Emit(instr.New(instr.STRING_LEN), instr.New(instr.RETURN))
	guard.Try(start, end, catch, 1)

	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 0)
	for x := range calls {
		b.Emit(instr.I32_CONST, uint64(x)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.I32_ADD)
	}
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(guard.MustBuild(), thrower.MustBuild(), types.String("boom!")))
}

// moduleTryProgram runs protectedFunction's loop in the module itself, inside
// a Try region, then divides 1 by sum-want outside the region: the trap
// escapes Run exactly when the loop summed to want. Module locals are [0] the
// counter and [1] the sum.
func moduleTryProgram(t *testing.T, n, want int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Bind(start)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 6400)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.I32_CONST, 63).Emit(instr.I32_AND)
	b.Emit(instr.I32_DIV_S).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Bind(end).Br(next)
	b.Bind(catch).Emit(instr.DROP).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1000).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Bind(next).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(want)).Emit(instr.I32_SUB).Emit(instr.I32_DIV_S)
	b.Try(start, end, catch, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithHandlers(b.Handlers()...))
}

// turnProgram calls relay(s, i, fn) n times from a module loop that first
// counts an inner loop to 8, where fn is bump3 when global 0 and i's low bit
// are both set and bump1 otherwise: the site stays monomorphic until global
// 0 turns it polymorphic. Constants are [relay, bump1, bump3, s]. Module
// locals [0] the counter, [1] the running sum (left on the stack), [2] s,
// and [3] the inner counter.
func turnProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done, inner, counted, high, chosen := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 3).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(inner)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 8).Emit(instr.I32_GE_S).BrIf(counted)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(inner)
	b.Bind(counted)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.GLOBAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(high)
	b.Emit(instr.CONST_GET, 1).Br(chosen)
	b.Bind(high).Emit(instr.CONST_GET, 2)
	b.Bind(chosen).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString, types.TypeI32), program.WithGlobals(types.TypeI32),
		program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s")))
}

// nestedConcatOuterFunction calls its constant callee with (a, b), so a hot
// outer's native call enters the callee's native code directly, two native
// activations deep, before the callee's STRING_EQ (which native code neither
// lowers nor resumes) deopts them both; both then run to a normal RETURN
// threaded, unlike an error unwind.
func nestedConcatOuterFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
		Code: instr.Marshal(code),
	}
}

// nestedConcatProgram compiles the caller first so later native-to-native calls
// reach the callee directly; the callee deopts at STRING_EQ and returns the
// STRING_CONCAT of its parameters.
func nestedConcatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	ib := instr.NewBuilder()
	ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_EQ).Emit(instr.DROP)
	ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
	icode, err := ib.Assemble()
	require.NoError(t, err)
	inner := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
		Code: instr.Marshal(icode),
	}
	outer := nestedConcatOuterFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithConstants(inner, outer, types.String("deep-"), types.String("concat")))
}

// identityFunction returns its i32 argument unchanged; used only to make a
// caller's own code not call-free.
func identityFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// typedArrayCallsProgram warms sumArray(arr) warm times, where sumArray
// itself calls the identity function once per loop iteration (constant 0),
// so its own array.get/array.len must translate despite the call: the exact
// bug S2-P10 fixes in transform/walk.go's callFree gate. Constant 1 is the
// i32 array to sum.
func typedArrayCallsProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	ident := identityFunction()
	sum := types.NewFunctionBuilder(&types.FunctionType{
		Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32},
	})
	header, done := sum.Label(), sum.Label()
	sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1))
	sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2))
	sum.Bind(header)
	sum.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.I32_GE_S)).BrIf(done)
	sum.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.DROP))
	sum.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
	sum.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1))
	sum.Br(header)
	sum.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
	sumFn := sum.MustBuild()
	sumFn.Locals = []types.Type{types.TypeI32, types.TypeI32}

	arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(10), types.BoxI32(20), types.BoxI32(30), types.BoxI32(40))
	b := instr.NewBuilder()
	loop, loopDone := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(loopDone)
	b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(loopDone).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sumFn, ident, arr))
}

// refArrayProgram warms readWrite(arr) warm times, where readWrite reads
// element 0 (retaining it), releases it, writes a fresh string into element
// 0 (releasing the old element, adopting the new one), and returns the new
// element: array.get and array.set over KindRef elements, whose ownership
// this proves through RefCount. Constant 1 is a two-element ref array whose
// elements are throwaway strings the warmup loop replaces every time.
func refArrayProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	rw := types.NewFunctionBuilder(&types.FunctionType{
		Params: []types.Type{types.NewArrayType(types.TypeAny)}, Returns: []types.Type{types.TypeAny},
	})
	rw.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.DROP),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0),
		instr.New(instr.CONST_GET, 1), instr.New(instr.CONST_GET, 2), instr.New(instr.STRING_CONCAT),
		instr.New(instr.ARRAY_SET),
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET),
		instr.New(instr.RETURN),
	)
	rwFn := rw.MustBuild()

	arr := types.NewArray(types.NewArrayType(types.TypeAny), types.BoxedNull, types.BoxedNull)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithConstants(rwFn, types.String("held-"), types.String("open"), arr))
}

// structTreeProgram warms sumTree(node) warm times, walking a 3-node linked
// list of structs {value i32, next any} through struct.get on both fields
// and ref.is_null as the loop condition, summing value. Constants are
// [walkFn, leaf(10), mid(20), root(30)]; the module's own code links
// root->mid->leaf->null once, through struct.set, before ever calling
// walkFn, since a struct constant's heap address exists only once the
// interpreter loads it.
func structTreeProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	record := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("value")), types.NewStructField(types.TypeAny, types.FieldWithName("next")))
	// The next field's declared type is the record itself: a self-reference,
	// wired after construction since a field cannot name its own type before
	// it exists. This is what lets w.field (transform/walk.go) resolve
	// struct.get's field kind statically for a value read back out of a
	// "next" field, not only for the root parameter.
	record.Fields[1].Type = record

	walk := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{record}, Returns: []types.Type{types.TypeI32}})
	header, done := walk.Label(), walk.Label()
	walk.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_SET, 1))
	walk.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2))
	walk.Bind(header)
	walk.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.REF_IS_NULL)).BrIf(done)
	walk.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
	walk.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET), instr.New(instr.LOCAL_SET, 1))
	walk.Br(header)
	walk.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
	walkFn := walk.MustBuild()
	walkFn.Locals = []types.Type{record, types.TypeI32}

	leaf := types.NewStruct(record, types.BoxI32(10), types.BoxedNull)
	mid := types.NewStruct(record, types.BoxI32(20), types.BoxedNull)
	root := types.NewStruct(record, types.BoxI32(30), types.BoxedNull)

	b := instr.NewBuilder()
	b.Emit(instr.CONST_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRUCT_SET)
	b.Emit(instr.CONST_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.STRUCT_SET)
	loop, done2 := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done2)
	b.Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32),
		program.WithTypes(record), program.WithConstants(walkFn, leaf, mid, root))
}

// arrayOOBProgram warms readAt(arr, idx) warm times with an in-range index,
// then calls it once more out of range inside a module-level Try that
// catches the trap: a native array.get bounds check deopts and the replayed
// threaded ARRAY_GET raises the identical ErrIndexOutOfRange a guest handler
// catches, matching pure threaded execution exactly.
func arrayOOBProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	at := types.NewFunctionBuilder(&types.FunctionType{
		Params: []types.Type{types.NewArrayType(types.TypeI32), types.TypeI32}, Returns: []types.Type{types.TypeI32},
	})
	at.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN))
	atFn := at.MustBuild()
	arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

	b := instr.NewBuilder()
	warmLoop, warmDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(warmLoop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(warmDone)
	b.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(warmLoop)
	b.Bind(warmDone)
	b.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 99).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(atFn, arr), program.WithHandlers(b.Handlers()...))
}

// hostArrayGlobalProgram declares global[0] as an i32 array and warms and
// finally calls readLen(arr) over whatever the test seeds there. Seeded with
// a Go-backed *interp.HostArray of the same declared element kind, every
// native entry's shape guard fails on a representation it never admits and
// deopts, matching threaded exactly (interp.arrayLen's own *HostArray case).
func hostArrayGlobalProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	ln := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}})
	ln.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))
	lnFn := ln.MustBuild()

	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.NewArrayType(types.TypeI32)), program.WithConstants(lnFn))
}

// guardFunction is guard(s, x) = x + 100 / (limit - x), or, when throw, s
// thrown once x reaches limit. It never compiles, so a native caller always
// reaches it through ExitCall.
func guardFunction(limit int, throw bool) *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	raise := b.Label()
	refuse(b)
	if throw {
		b.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(limit)), instr.New(instr.I32_GE_S)).BrIf(raise)
	}
	b.Emit(
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_CONST, uint64(limit)), instr.New(instr.LOCAL_GET, 1),
		instr.New(instr.I32_SUB), instr.New(instr.I32_DIV_S), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	b.Bind(raise).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.THROW))
	return b.MustBuild()
}

// relayFunction is relay(s, x) = callee(s, x) + 1 through constant callee;
// when guarded, it never compiles.
func relayFunction(callee int, guarded bool) *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	if guarded {
		refuse(b)
	}
	b.Emit(
		instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CONST_GET, uint64(callee)), instr.New(instr.CALL),
		instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	return b.MustBuild()
}

// loopFunction is loop(s, n) = callee(s, 0) + ... + callee(s, n-1) through
// constant callee, counting an inner loop to 8 before each call, so its own
// work pays for a served call; when global, it passes global 0 instead of
// s, which the call owns rather than lends.
func loopFunction(callee int, global bool) *types.Function {
	arg := instr.New(instr.LOCAL_GET, 0)
	if global {
		arg = instr.New(instr.GLOBAL_GET, 0)
	}
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32, types.TypeI32)
	loop, done, count, counted := b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(done)
	b.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 4))
	b.Bind(count).Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 8), instr.New(instr.I32_GE_S)).BrIf(counted)
	b.Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4)).Br(count)
	b.Bind(counted).Emit(
		arg, instr.New(instr.LOCAL_GET, 3), instr.New(instr.CONST_GET, uint64(callee)), instr.New(instr.CALL),
		instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 3),
	).Br(loop)
	b.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
	return b.MustBuild()
}

// loopProgram stores s, constant 2, in global 0, calls constant 1 as f(s, n)
// warm times, then f(s, final) once, and leaves the running sum under s.
// When try, the last call sits in a Try region whose handler drops the
// exception and leaves -1.
func loopProgram(t *testing.T, warm, n, final int, try bool, constants ...types.Value) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done, start, end, catch, after := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 2).Emit(instr.GLOBAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 2).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Bind(start).Emit(instr.CONST_GET, 2).Emit(instr.I32_CONST, uint64(final)).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD)
	if try {
		b.Br(after)
		b.Bind(end)
		b.Bind(catch).Emit(instr.DROP).Emit(instr.I32_CONST, uint64(math.MaxUint32))
		b.Try(start, end, catch, 2)
	}
	b.Bind(after).Emit(instr.CONST_GET, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithGlobals(types.TypeString), program.WithConstants(constants...), program.WithHandlers(b.Handlers()...))
}

// sumrecFunction is sumrec(s, n) = n + sumrec(s, n-1), sumrec(s, 0) = 0,
// calling itself through constant self: one activation per unit of n.
func sumrecFunction(self int) *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	zero := b.Label()
	b.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQZ)).BrIf(zero)
	b.Emit(
		instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
		instr.New(instr.CONST_GET, uint64(self)), instr.New(instr.CALL), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
	)
	b.Bind(zero).Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
	return b.MustBuild()
}

// bumpFunction is bump(s, x) = x + add.
func bumpFunction(add int) *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(add)), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
	return b.MustBuild()
}

// relayDynamicFunction is relay(s, x, fn) = fn(s, x) through param 2, a
// dynamic CALL.
func relayDynamicFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2), instr.New(instr.CALL), instr.New(instr.RETURN))
	return b.MustBuild()
}

// pairFunction is pair(x) = x + 100, taking one parameter where bump takes
// two.
func pairFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
	return b.MustBuild()
}

// mixedProgram calls relay(s, i, bump) warm times, bump alternating between
// two functions of one signature by i's low bit through the same dynamic
// CALL site; when other, it then calls relay once more with a function of
// another signature. Constants are [relay, bump1, bump3, s, pair]. Module
// locals [0] the counter, [1] the running sum, and [2] s, left under the sum
// on the stack.
func mixedProgram(t *testing.T, warm int, other bool) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done, high, chosen := b.Label(), b.Label(), b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 3).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND).BrIf(high)
	b.Emit(instr.CONST_GET, 1).Br(chosen)
	b.Bind(high).Emit(instr.CONST_GET, 2)
	b.Bind(chosen).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	if other {
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 4).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	}
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString),
		program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s"), pairFunction()))
}

// coldFunction is cold(s, n, k, fn) = the sum of i over [0, n), except that
// at i == k it adds fn(s, i) instead: a dynamic CALL no run reaches while k
// is out of range.
func coldFunction() *types.Function {
	b := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32, types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
	loop, done, hit, next := b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(done)
	b.Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_EQ)).BrIf(hit)
	b.Emit(instr.New(instr.LOCAL_GET, 5)).Br(next)
	b.Bind(hit).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 3), instr.New(instr.CALL))
	b.Bind(next).Emit(
		instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
		instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
	).Br(loop)
	b.Bind(done).Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.RETURN))
	return b.MustBuild()
}

// coldProgram calls cold(s, 100, -1, bump) warm times, then cold(s, 100,
// final, bump) once. Constants are [cold, bump, s]. Module locals [0] the
// counter, [1] the running sum, and [2] s, left under the sum on the stack.
func coldProgram(t *testing.T, warm, final int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_SET, 2)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(math.MaxUint32)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(final)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString),
		program.WithConstants(coldFunction(), bumpFunction(1), types.String("s")))
}

func TestWithThreshold(t *testing.T) {
	t.Run("compiles a hot recursive function and enters its native code", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 2*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("keeps a loop-free bridged module native across entries", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		for range 16 {
			b.Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I32_CONST, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithTypes(record), program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var entries float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries >= 64
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.GreaterOrEqual(t, entries, float64(64))

		recent := make([]float64, 0, 32)
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Flush()
			count, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			recent = append(recent, count)
			vm.Reset()
		}
		require.Equal(t, want, result)
		for i := 1; i < len(recent); i++ {
			require.Greater(t, recent[i], recent[i-1])
		}
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
	})

	t.Run("retires a loop whose only work per iteration is a bridge and its release", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(record))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var result types.Value
		var runErr, popErr error
		var bridges float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Greater(t, bridges, float64(0))

		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		before, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		for range 32 {
			require.NoError(t, vm.Run(context.Background()))
			result, err = vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		after, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, want, result)
		require.Equal(t, before, after)
	})

	t.Run("a loop function reaches a safepoint and refills its budget", func(t *testing.T) {
		native(t)
		prog := sumWarmProgram(t, 1000, 200000)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("releases a dying ref parameter through ExitRelease", func(t *testing.T) {
		native(t)
		fn := dropFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 500).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.STRING_CONCAT)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(fn, types.String("native-"), types.String("release")))

		var runErr, popErr, refErr error
		var survivor types.Boxed
		var count int
		var released float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			survivor, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			count, refErr = vm.RefCount(survivor.Ref())
			if refErr != nil {
				return true
			}
			vm.Flush()
			released, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			return released > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, 1, count)
	})

	t.Run("native float comparisons treat NaN as unordered", func(t *testing.T) {
		native(t)
		const warm = 1000
		nan := uint64(math.Float32bits(float32(math.NaN())))
		b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}})
		for _, op := range []instr.Opcode{instr.F32_EQ, instr.F32_NE, instr.F32_LE} {
			b.Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 0))
			b.Emit(instr.New(instr.F32_CONST, nan), instr.New(instr.F32_CONST, nan), instr.New(op), instr.New(instr.SELECT))
			if op != instr.F32_EQ {
				b.Emit(instr.New(instr.I32_ADD))
			}
		}
		fn := b.Emit(instr.New(instr.RETURN)).MustBuild()

		module := instr.NewBuilder()
		loop, done := module.Label(), module.Label()
		module.Bind(loop)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_GE_S).BrIf(done)
		module.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		module.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		module.Br(loop)
		module.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := module.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
		want := runProgram(t, prog)
		require.Equal(t, types.I32(1), want)

		var runErr, popErr error
		var result types.Value
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("a native division by zero is caught by a guest handler, matching threaded", func(t *testing.T) {
		native(t)
		prog := divCaughtProgram(t, 1000)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("an uncaught native division by zero reports the same stack trace as threaded", func(t *testing.T) {
		native(t)
		prog := divUncaughtProgram(t, 1000)
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		var gotErr error
		require.Eventually(t, func() bool {
			vm := interp.New(prog, interp.WithThreshold(0))
			defer vm.Close()
			gotErr = vm.Run(context.Background())
			return gotErr != nil
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("a bridged STRING_CONCAT inside compiled code matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := concatProgram(t, 1000)
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("a bridged STRUCT_NEW resumes native code and matches threaded, including a field's RefCount", func(t *testing.T) {
		native(t)
		prog := structs(t, 200_000)
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		// Every STRUCT_NEW bridge resumes: none of them fall back to a deopt.
		require.Equal(t, float64(0), deopts)
	})

	t.Run("a bridged ARRAY_NEW_DEFAULT resumes native code and matches threaded", func(t *testing.T) {
		native(t)
		prog := arrays(t, 200_000)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var bridges, deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Equal(t, float64(0), deopts)
	})

	t.Run("a trapping ARRAY_NEW_DEFAULT declines and matches threaded's error", func(t *testing.T) {
		native(t)
		prog := trap(t, 200_000)
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		// A declined bridge's own exit is still kind=bridge (the metric
		// names the exit, not its outcome), so only error/stack-trace parity
		// with threaded execution proves native code correctly handed the
		// trapping instruction back instead of silently accepting it.
		var gotErr error
		var bridges float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridged string ops resume native code and match threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := texts(t, 20_000)
		threaded := interp.New(prog)
		require.NoError(t, threaded.Run(context.Background()))
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var runErr, popErr, constErr error
		var value string
		var count, constCount int
		var bridges, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)
		// An entry that declined its first bridge could not reach a second one.
		require.Greater(t, bridges, entries)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("a string op bridge that exhausts the heap declines and matches threaded's error and RefCount", func(t *testing.T) {
		native(t)
		const limit = 20_000
		prog := texts(t, 2*limit)
		threaded := interp.New(prog, interp.WithHeapLimit(limit))
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrHeapExhausted)
		ab, err := threaded.Const(1)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(ab.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, constErr error
		var constCount int
		var bridges, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithHeapLimit(limit), interp.WithProfiler(profiler))
			defer vm.Close()
			gotErr = vm.Run(context.Background())
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return bridges > entries
		}, 5*time.Second, time.Millisecond)
		require.Greater(t, bridges, entries)
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, constErr)
		require.Equal(t, wantConst, constCount)
	})

	t.Run("unlowered operators in a loop bridge and resume native code, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		dict, list := types.NewMapType(types.TypeI32, types.TypeI32), types.NewArrayType(types.TypeI32)
		wide, named := types.NewMapType(types.TypeI32, types.TypeI64), types.NewMapType(types.TypeString, types.TypeI32)
		b := instr.NewBuilder()
		loop, done, first, firstDone, second, secondDone, third, thirdDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 3).Emit(instr.LOCAL_SET, 5)
		b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 4).Emit(instr.LOCAL_SET, 6)
		b.Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_NEW_DEFAULT, 1).Emit(instr.LOCAL_SET, 3)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)
		// Each group of bridges follows an inner loop's native work, enough
		// to pay for the bridges (jit.Ledger).
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(first)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(firstDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(first)
		b.Bind(firstDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND).Emit(instr.LOCAL_SET, 3)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.STRING_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_LOOKUP).Emit(instr.I32_ADD)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_SLICE).Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(second)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(secondDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(second)
		b.Bind(secondDone)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_FILL)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_COPY)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 0).Emit(instr.ARRAY_DELETE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 7).Emit(instr.ERROR_NEW).Emit(instr.ERROR_CODE)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.CONST_GET, 0).Emit(instr.REF_TEST, 2)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 3).Emit(instr.REF_EQ)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(third)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 16).Emit(instr.I32_GE_S).BrIf(thirdDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(third)
		b.Bind(thirdDone)
		// A wide i64 crosses the bridge boxed on the heap both ways.
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1<<60).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_GET)
		b.Emit(instr.I64_CONST, 58).Emit(instr.I64_SHR_U).Emit(instr.I64_TO_I32)
		b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
		b.Emit(instr.LOCAL_GET, 5).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_DELETE)
		// map.delete adopts its key: native code hands it the reference.
		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.MAP_SET)
		b.Emit(instr.LOCAL_GET, 6).Emit(instr.CONST_GET, 0).Emit(instr.MAP_DELETE)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done)
		b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_APPEND)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32, types.TypeI32, dict, list, types.TypeI32, wide, named),
			program.WithTypes(dict, list, types.TypeString, wide, named),
			program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.New(prog)
		require.NoError(t, threaded.Run(context.Background()))
		boxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		want, err := threaded.Load(boxed.Ref())
		require.NoError(t, err)
		wantCount, err := threaded.RefCount(boxed.Ref())
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		wantConst, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		entries := metric("vm_jit_entries_total", "tier", "optimized")
		bridges := metric("vm_jit_exits_total", "kind", "bridge")
		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			boxed, err := vm.PopBoxed()
			require.NoError(t, err)
			got, err := vm.Load(boxed.Ref())
			require.NoError(t, err)
			count, err := vm.RefCount(boxed.Ref())
			require.NoError(t, err)
			constCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count)
			require.Equal(t, wantConst, constCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), entries)
		require.Greater(t, metric("vm_jit_exits_total", "kind", "bridge"), bridges)
		require.Zero(t, metric("vm_jit_exits_total", "kind", "deopt"))
	})

	t.Run("a native select between two refs keeps threaded's RefCount for both", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 300).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_AND)
		b.Emit(instr.SELECT).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(types.String("x"), types.String("y")))

		threaded := interp.New(prog)
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		y, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantY, err := threaded.RefCount(y.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name, key, value string) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, prof.Label{Key: key, Value: value})
			return v
		}

		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_entries_total", "tier", "optimized") > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		for range 8 {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			xCount, err := vm.RefCount(x.Ref())
			require.NoError(t, err)
			yCount, err := vm.RefCount(y.Ref())
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantX, xCount)
			require.Equal(t, wantY, yCount)
			vm.Reset()
		}
		require.Greater(t, metric("vm_jit_entries_total", "tier", "optimized"), 0.0)
	})

	t.Run("an unlowered operator that traps deoptimizes and reports threaded's error and RefCount", func(t *testing.T) {
		native(t)
		const warm = 4000
		b := instr.NewBuilder()
		loop, done, inner, innerDone, same := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2*warm).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1).Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, warm).Emit(instr.I32_NE).BrIf(same)
		b.Emit(instr.CONST_GET, 1).Emit(instr.LOCAL_SET, 2)
		b.Bind(same)
		// string.eq releases "x" before it finds the array at iteration warm.
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.STRING_EQ).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code,
			program.WithLocals(types.TypeI32, types.TypeI32, types.TypeAny),
			program.WithConstants(types.String("x"), types.TypedArray[int32]{1}))

		threaded := interp.New(prog)
		wantErr := threaded.Run(context.Background())
		require.ErrorIs(t, wantErr, interp.ErrTypeMismatch)
		x, err := threaded.Const(0)
		require.NoError(t, err)
		array, err := threaded.Const(1)
		require.NoError(t, err)
		wantX, err := threaded.RefCount(x.Ref())
		require.NoError(t, err)
		wantArray, err := threaded.RefCount(array.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		var gotErr, xErr, arrayErr error
		var gotX, gotArray int
		var bridges float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			gotErr = vm.Run(context.Background())
			gotX, xErr = vm.RefCount(x.Ref())
			gotArray, arrayErr = vm.RefCount(array.Ref())
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		// A bridge that deoptimizes leaves native code for the rest of the Run.
		require.Greater(t, bridges, float64(1))
		require.True(t, errorsEqual(gotErr, wantErr), "got %v, want %v", gotErr, wantErr)
		require.NoError(t, xErr)
		require.NoError(t, arrayErr)
		require.Equal(t, wantX, gotX)
		require.Equal(t, wantArray, gotArray)
	})

	t.Run("a callee's unamortized threaded-entry bridges do not retire it while its native calls amortize theirs", func(t *testing.T) {
		native(t)
		// The loop adds f(i) for i < n. f(0) bridges three string ops with no
		// native work between them; every other f(i) loops four times first,
		// amortizing its bridges.
		const n = 2000
		fb := instr.NewBuilder()
		work, loop, done := fb.Label(), fb.Label(), fb.Label()
		fb.Emit(instr.LOCAL_GET, 0).BrIf(work)
		fb.Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fb.Bind(work).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.STRING_NEW_UTF32).Emit(instr.STRING_LEN).Emit(instr.RETURN)
		fcode, err := fb.Assemble()
		require.NoError(t, err)
		f := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(fcode),
		}

		b := instr.NewBuilder()
		header, exit, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(exit)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(header)
		b.Bind(exit).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeI32), program.WithConstants(f, types.String("ab")))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		// f(0) runs threaded-entered at most once per Run; enough Runs pass
		// for its bridges to reach the retirement limit several times over.
		const runs = 16
		var got types.Value
		var runErr, popErr error
		var round int
		var entries, prior float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			round++
			total, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries, prior = total-prior, total
			return round >= runs && entries < n/2
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, round, runs)
		// A retired f leaves the loop's native call nothing to enter, so the
		// loop runs threaded and enters f's OSR code for nearly every f(i).
		require.Less(t, entries, float64(n/2))
	})

	t.Run("native OpStore releases a ref-typed slot's old value, matching threaded RefCount", func(t *testing.T) {
		native(t)
		const n = 200_000
		prog := store(t, n)

		threaded := interp.New(store(t, n))
		require.NoError(t, threaded.Run(context.Background()))
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		want, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)
		require.NoError(t, threaded.Close())

		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		gotConst, err := vm.Const(0)
		require.NoError(t, err)
		got, err := vm.RefCount(gotConst.Ref())
		require.NoError(t, err)

		require.Equal(t, want, got)
	})

	t.Run("a bridged call entered dynamically (unfused) matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := dynamicConcatProgram(t, 1000)
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("recursion past a small frame limit overflows inside a served call, matching threaded", func(t *testing.T) {
		native(t)
		prog := fibOverflowProgram(t, 1000, 30)
		wantErr := runProgramErr(t, prog, interp.WithFrame(8))
		require.Error(t, wantErr)

		var gotErr error
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
			defer vm.Close()
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, overflowEqual(gotErr, wantErr))
	})

	t.Run("a self-recursive fib deopting at a frame limit keeps the CSE'd callee's RefCount equal to threaded", func(t *testing.T) {
		native(t)
		// The handler unwinds every native and interpreter frame the deopt
		// left above it and releases the operand stack, so RefCount after a
		// successful Run reflects only durable state, comparable across
		// threaded and native runs. (An uncaught error leaves abandoned
		// frames on both paths, by design, and is not comparable this way.)
		prog := caught(t, 1000, 30)
		wantVM := interp.New(prog, interp.WithFrame(8))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)
		require.Equal(t, 1, wantRC)

		var runErr, rcErr error
		var gotRC int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, rcErr)
		// The module's own constant pool is fib's only live reference: a
		// mid-depth deopt through the CSE'd, borrowed callee must neither
		// leak an extra retain nor drop the constant pool's own.
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("a deopt inside a callee with register-passed ref and i32 parameters matches threaded, including the ref's RefCount", func(t *testing.T) {
		native(t)
		// rec(s, n) = n < 1 ? n : rec(s, n-1) + 1. The module warms rec(s, 2),
		// then calls rec(s, 30) under a handler: WithFrame(10) deopts it
		// several native activations deep.
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		small := fb.Label()
		fb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_LT_S)).BrIf(small)
		fb.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		fb.Bind(small).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.RETURN))
		rec := fb.MustBuild()

		mb := instr.NewBuilder()
		loop, done, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(loop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
		mb.Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(loop)
		mb.Bind(done)
		mb.Bind(start).Emit(instr.CONST_GET, 1).Emit(instr.I32_CONST, 30).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(rec, types.String("s")), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.New(prog, interp.WithFrame(8))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(1)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotRC int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			c, _ := vm.Const(1)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("a native call to a function without native code resumes its caller instead of deoptimizing, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		s := types.String("s")
		deep := []interp.Option{interp.WithFrame(1024), interp.WithStack(1 << 14)}
		for _, c := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
		}{
			{"a callee that never compiles", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, false), s), nil},
			{"a callee that never compiles, passed an owned argument", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, true), s), nil},
			{"a callee that never compiles calling native code that calls it again", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(4, false), s, relayFunction(0, false), relayFunction(3, true)), nil},
			{"recursion past the native activation limit", loopProgram(t, 8, 10, 600, false, guardFunction(1000, false), sumrecFunction(1), s), deep},
		} {
			want := interp.New(c.prog, c.opts...)
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(c.prog, append([]interp.Option{interp.WithThreshold(0), interp.WithProfiler(profiler)}, c.opts...)...)
			exits := func(kind string) float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: kind})
				return v
			}
			run := func() {
				require.NoError(t, vm.Run(context.Background()), c.name)
				sum, count, err := popLoop(vm)
				require.NoError(t, err, c.name)
				require.Equal(t, wantSum, sum, c.name)
				require.Equal(t, wantCount, count, c.name)
				vm.Reset()
			}
			require.Eventually(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits("call") > 0
			}, 5*time.Second, time.Millisecond, c.name)
			for range 16 {
				run()
			}
			before := exits("call")
			for range 16 {
				run()
			}
			require.Greater(t, exits("call"), before, c.name)
			require.Zero(t, exits("deopt"), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("a dynamic call whose callees share one signature resumes native code through call exits, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		prog := mixedProgram(t, 64, false)
		want := interp.New(prog)
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		// Both callees are seen before the caller compiles.
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) + metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
		}
		call, deopt := prof.Label{Key: "kind", Value: "call"}, prof.Label{Key: "kind", Value: "deopt"}
		require.Eventually(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_exits_total", call) > 0
		}, 5*time.Second, time.Millisecond)
		beforeEntries, beforeCalls := entries(), metric("vm_jit_exits_total", call)
		for range 16 {
			run()
		}
		require.Greater(t, entries(), beforeEntries)
		require.Greater(t, metric("vm_jit_exits_total", call), beforeCalls)
		require.Zero(t, metric("vm_jit_exits_total", deopt))
		// The caller of the dynamic call compiles instead of being refused.
		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("a caller whose served calls cost more than its own work retires, matching threaded", func(t *testing.T) {
		native(t)
		const runs = 16
		prog := servedProgram(t, 16)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		calls := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return v
		}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || calls() > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		// Until the ledger retires served.
		require.Eventually(t, func() bool {
			before := calls()
			for range runs {
				if runErr = vm.Run(context.Background()); runErr != nil {
					return true
				}
				vm.Reset()
			}
			return calls() == before
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		before := calls()
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.Equal(t, before, calls())
	})

	t.Run("a dynamic call whose callee takes other parameters deopts and matches threaded including RefCount", func(t *testing.T) {
		native(t)
		prog := mixedProgram(t, 64, true)
		want := interp.New(prog)
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)
		require.NoError(t, want.Close())

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(8), interp.WithProfiler(profiler))
		defer vm.Close()
		entries := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return v
		}
		for range 64 {
			require.NoError(t, vm.Run(context.Background()))
			sum, count, err := popLoop(vm)
			require.NoError(t, err)
			require.Equal(t, wantSum, sum)
			require.Equal(t, wantCount, count)
			vm.Reset()
			time.Sleep(time.Millisecond)
		}
		require.Positive(t, entries())
	})

	t.Run("a dynamic call no run has reached costs native code no deopt until it runs, then matches threaded including RefCount", func(t *testing.T) {
		native(t)
		for _, c := range []struct {
			name  string
			final int
		}{
			{"it stays cold", -1},
			{"it runs", 3},
		} {
			prog := coldProgram(t, 64, c.final)
			want := interp.New(prog)
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			metric := func(name string, labels ...prof.Label) float64 {
				vm.Flush()
				v, _ := profiler.Metric(name, labels...)
				return v
			}
			run := func() {
				require.NoError(t, vm.Run(context.Background()), c.name)
				sum, count, err := popLoop(vm)
				require.NoError(t, err, c.name)
				require.Equal(t, wantSum, sum, c.name)
				require.Equal(t, wantCount, count, c.name)
				vm.Reset()
			}
			entered := prof.Label{Key: "tier", Value: "baseline"}
			require.Eventually(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || metric("vm_jit_entries_total", entered) > 0
			}, 5*time.Second, time.Millisecond, c.name)
			for range 16 {
				run()
			}
			deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			if c.final < 0 {
				require.Zero(t, deopts, c.name)
			} else {
				require.Positive(t, deopts, c.name)
			}
			require.NoError(t, vm.Close())
		}
	})

	t.Run("a native call to a coroutine function deoptimizes and matches threaded including RefCount", func(t *testing.T) {
		native(t)
		// Its CALL returns a coroutine handle, not the results the native
		// caller's own call site types: the caller cannot resume.
		co := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeAny}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.YIELD), instr.New(instr.RETURN)).MustBuild()
		drive := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32)
		loop, done := drive.Label(), drive.Label()
		drive.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(done)
		drive.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 2), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.DROP),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		).Br(loop)
		drive.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		prog := loopProgram(t, 64, 8, 8, false, co, drive.MustBuild(), types.String("s"))

		want := interp.New(prog)
		defer want.Close()
		require.NoError(t, want.Run(context.Background()))
		wantSum, wantCount, err := popLoop(want)
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		exits := func() float64 {
			vm.Flush()
			v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return v
		}
		require.Eventually(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || exits() > 0
		}, 5*time.Second, time.Millisecond)

		require.NoError(t, vm.Run(context.Background()))
		gotSum, gotCount, err := popLoop(vm)
		require.NoError(t, err)
		require.Equal(t, wantSum, gotSum)
		require.Equal(t, wantCount, gotCount)
		require.Positive(t, exits())
	})

	t.Run("an uncaught trap inside a resumed call's callee reports threaded's error", func(t *testing.T) {
		native(t)
		prog := loopProgram(t, 8, 50, 100, false, guardFunction(60, false), loopFunction(0, false), types.String("s"))
		wantErr := runProgramErr(t, prog)
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		var gotErr error
		var exits float64
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return !errorsEqual(gotErr, wantErr) || exits > 0
		}, 5*time.Second, time.Millisecond)
		require.True(t, errorsEqual(gotErr, wantErr), "%v != %v", gotErr, wantErr)
		require.Positive(t, exits)
	})

	t.Run("a trap or throw inside a resumed call's callee reaches the module's handler with threaded's value and RefCount", func(t *testing.T) {
		native(t)
		s := types.String("s")
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"trap", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, false), s)},
			{"trap with an owned argument", loopProgram(t, 8, 50, 100, true, guardFunction(60, false), loopFunction(0, true), s)},
			{"throw", loopProgram(t, 8, 50, 100, true, guardFunction(60, true), loopFunction(0, false), s)},
		} {
			want := interp.New(c.prog)
			require.NoError(t, want.Run(context.Background()), c.name)
			wantSum, wantCount, err := popLoop(want)
			require.NoError(t, err, c.name)
			require.NoError(t, want.Close())

			profiler := prof.New()
			vm := interp.New(c.prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			exits := func() float64 {
				vm.Flush()
				v, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
				return v
			}
			require.Eventually(t, func() bool {
				err := vm.Run(context.Background())
				vm.Reset()
				return err != nil || exits() > 0
			}, 5*time.Second, time.Millisecond, c.name)
			require.NoError(t, vm.Run(context.Background()), c.name)
			sum, count, err := popLoop(vm)
			require.NoError(t, err, c.name)
			require.Equal(t, wantSum, sum, c.name)
			require.Equal(t, wantCount, count, c.name)
			require.Positive(t, exits(), c.name)
			require.NoError(t, vm.Close())
		}
	})

	t.Run("a deopt two native activations deep matches threaded, including the borrowed callee's RefCount", func(t *testing.T) {
		native(t)
		prog := nestedConcatProgram(t, 20000)
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			innerCount, refErr = vm.RefCount(1)
			if refErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("a borrowed callee's RefCount survives a served call, matching threaded", func(t *testing.T) {
		native(t)
		prog := outerInnerProgram(t, 20000)
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			innerCount, refErr = vm.RefCount(1)
			if refErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Equal(t, 1, innerCount)
	})

	t.Run("a borrowed parameter passed through native recursion matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		// self (param 1) is borrowed at both of indirectFibFunction's own
		// dynamic CALLs: a native caller never retains it, so a deopt at the
		// frame limit exercises the served call's own retain of it.
		prog := caughtIndirect(t, 1000, 30)
		wantVM := interp.New(prog, interp.WithFrame(8))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, rcErr error
		var gotRC int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, rcErr)
		// The module's own constant pool is fib's only live reference: a
		// mid-depth deopt through the borrowed self parameter must neither
		// leak an extra retain nor drop the constant pool's own.
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("an owned argument lent to a borrowed parameter transfers to the materialized callee", func(t *testing.T) {
		native(t)
		prog := lentProgram(t, 3000)

		wantVM := interp.New(lentProgram(t, 3000))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(2)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(3)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(3)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		// If the !Owned filter in compile.call's Lent were missing, a's own
		// global-backed argument would be retained twice at the deopt: once
		// by a's own translator-side owning of it, once by a wrongly
		// populated Lent entry for apply's borrowed param 1.
		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))
		require.Less(t, deopts, float64(3000))
	})

	t.Run("a deep recursion resuming safepoints and releases then deopting at its frame limit matches threaded", func(t *testing.T) {
		native(t)

		// rec(n): a 20000-iteration loop (a resumed safepoint in every
		// activation), a dropped fresh struct (a resumed release), then
		// rec(n-1). Param 0 is n; local 1 the counter.
		record := types.NewStructType(types.NewStructField(types.TypeI32, types.FieldWithName("n")))
		fb := instr.NewBuilder()
		loop, done, small := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		fb.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fb.Br(loop)
		fb.Bind(done)
		fb.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.DROP)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_LT_S).BrIf(small)
		fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		fb.Bind(small).Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
		fnCode, err := fb.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}

		// The module warms rec(2), then calls rec(15) under a handler:
		// WithFrame(8) makes the native chain take ExitCall several
		// activations deep, and the handler catches the overflow.
		mb := instr.NewBuilder()
		wloop, wdone, start, end, catch := mb.Label(), mb.Label(), mb.Label(), mb.Label(), mb.Label()
		mb.Bind(wloop)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_GE_S).BrIf(wdone)
		mb.Emit(instr.I32_CONST, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		mb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		mb.Br(wloop)
		mb.Bind(wdone)
		mb.Bind(start).Emit(instr.I32_CONST, 15).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		mb.Bind(end)
		mb.Bind(catch).Emit(instr.ERROR_CODE)
		mb.Try(start, end, catch, 1)
		code, err := mb.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(record), program.WithConstants(fn), program.WithHandlers(mb.Handlers()...))

		wantVM := interp.New(prog, interp.WithFrame(8))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)

		var runErr, popErr error
		var got types.Value
		var safepoints, releases, calls float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			safepoints, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			releases, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			calls, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return safepoints > 0 && releases > 0 && calls > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("a wide i64 result from native code matches threaded", func(t *testing.T) {
		native(t)
		prog := wideI64Program(t, 1000)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		require.Zero(t, deopts)
		require.Equal(t, want, result)
	})

	t.Run("a wide i64 recursion result from native code matches threaded", func(t *testing.T) {
		native(t)
		prog := wideI64Fib(t, 20)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("a wide i64 argument from threaded code into a native function matches threaded", func(t *testing.T) {
		native(t)
		prog := wideArgProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("a native caller passes a wide i64 register argument matching threaded", func(t *testing.T) {
		native(t)
		prog := wideRelayProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("an OSR unit returns a wide i64 result matching threaded", func(t *testing.T) {
		native(t)
		prog := wideI64SumProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	seed := int64(-3750763034362895579)
	for _, c := range []struct {
		name string
		prog *program.Program
	}{
		{"a function FNV loop seeded with a wide i64.const compiles and matches threaded", fnvProgram(t, 100_000, instr.I64_CONST, uint64(seed))},
		{"a function FNV loop seeded by a wide i64 pool cell compiles and matches threaded", fnvProgram(t, 100_000, instr.CONST_GET, 1, types.I64(seed))},
	} {
		t.Run(c.name, func(t *testing.T) {
			native(t)
			prog := c.prog
			want := runProgram(t, prog)

			var got types.Value
			var runErr, popErr error
			var deopts, compiles float64
			require.Eventually(t, func() bool {
				profiler := prof.New()
				vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
				defer vm.Close()
				runErr = vm.Run(context.Background())
				if runErr != nil {
					return true
				}
				got, popErr = vm.Pop()
				if popErr != nil {
					return true
				}
				vm.Flush()
				deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
				return compiles > 0
			}, 5*time.Second, time.Millisecond)
			require.NoError(t, runErr)
			require.NoError(t, popErr)
			require.Zero(t, deopts)
			require.Equal(t, want, got)
		})
	}

	t.Run("a wide i64 constant kept across a shape-guard deopt keeps threaded's RefCount", func(t *testing.T) {
		native(t)
		const seed = -3750763034362895579
		prog := leakArrayProgram(t, 20000, seed)

		threaded := interp.New(prog)
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		wantBoxed, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantRC, err := threaded.RefCount(wantBoxed.Ref())
		require.NoError(t, err)
		require.Equal(t, 1, wantRC)

		var runErr, popErr, rcErr, marshalErr, allocErr, globalErr error
		var gotBoxed types.Boxed
		var gotRC int
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			var hostVal types.Value
			hostVal, marshalErr = vm.Marshal([]int32{1, 2, 3})
			if marshalErr != nil {
				return true
			}
			var hostAddr int
			hostAddr, allocErr = vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotBoxed, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			gotRC, rcErr = vm.RefCount(gotBoxed.Ref())
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, wantRC, gotRC)
	})

	t.Run("an OSR module loop promotes a narrow i64 accumulator matching threaded (narrowarg)", func(t *testing.T) {
		native(t)
		prog := narrowArgProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("an OSR module FNV loop promotes its i64 accumulator matching threaded (fnv-module-narrowseed)", func(t *testing.T) {
		native(t)
		prog := fnvModuleProgram(t, 100_000, 0x1234567)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		// OpComplete's own wide box used to deopt once (B); it now resumes.
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("a wide store in a loop resumes through a box exit every iteration, matching threaded", func(t *testing.T) {
		native(t)
		prog := wideStoreLoopProgram(t, 50_000)
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			entries = baseline + optimized
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("a hot function tiers up to Optimized after enough native entries", func(t *testing.T) {
		native(t)
		prog := fibFlatCallsProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var compiles float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("a callee's division by zero caught by a guest handler costs its native caller nothing: the caller keeps entering past refute traps, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		prog := rareTrapProgram(t, rounds, 100)

		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(rounds), want)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 2 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCounts, counts(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("a hot loop inside a protected region keeps its function native past caught traps, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		prog := protectedProgram(t, rounds, 100)

		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		count := func(vm *interp.Interpreter) int {
			c, err := vm.Const(1)
			require.NoError(t, err)
			n, err := vm.RefCount(c.Ref())
			require.NoError(t, err)
			return n
		}
		wantCount := count(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCount, count(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("a native callee's throw is caught by its native caller's handler, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		const calls, batches = 32, 64
		prog := throwProgram(t, calls)

		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		counts := func(vm *interp.Interpreter) []int {
			var out []int
			for index := range 3 {
				c, err := vm.Const(index)
				require.NoError(t, err)
				count, err := vm.RefCount(c.Ref())
				require.NoError(t, err)
				out = append(out, count)
			}
			return out
		}
		wantCounts := counts(threaded)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"}) > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			require.Equal(t, wantCounts, counts(vm))
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*calls))
		// Neither the caller nor its callee is refused.
		require.Zero(t, metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"}))
	})

	t.Run("a module loop inside a protected region enters natively and reports a trap outside the region as threaded does", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := 0
		for i := range n {
			if d := (i + 1) & 63; d == 0 {
				want += 1000
			} else {
				want += 6400 / d
			}
		}
		prog := moduleTryProgram(t, n, want)
		wantErr := runProgramErr(t, prog)
		require.ErrorIs(t, wantErr, interp.ErrDivideByZero)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		var runErr error
		var entries float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.Greater(t, entries, float64(0))
		require.EqualError(t, runErr, wantErr.Error())
	})

	t.Run("a cancelled context during a native loop escapes guest handlers as the context error", func(t *testing.T) {
		native(t)
		// The final call repeats natively until the timer cancels it.
		prog := sumTryProgram(t, 200_000, 2_000_000_000)

		var runErr error
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.Canceled) {
				return false
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 20*time.Second, time.Second)
		require.Error(t, runErr)
		require.ErrorIs(t, runErr, context.Canceled)
	})

	t.Run("a function allocated after construction runs interpreted", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		fn := &types.Function{Typ: &types.FunctionType{Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CALL)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithGlobals(types.TypeAny))

		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		addr, err := vm.Alloc(fn)
		require.NoError(t, err)
		require.NoError(t, vm.SetGlobal(0, types.BoxRef(addr)))
		require.NoError(t, vm.Run(context.Background()))
		result, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(7), result)
	})

	t.Run("a wide i64 argument boxed at a borrowed call keeps the callee alive", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.RETURN)
		code, err := b.Assemble()
		require.NoError(t, err)
		callee := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		code, err = b.Assemble()
		require.NoError(t, err)
		caller := &types.Function{Typ: &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI32}}, Code: instr.Marshal(code)}
		b = instr.NewBuilder()
		warm, warmed := b.Label(), b.Label()
		b.Bind(warm)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(warmed)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(warm)
		b.Bind(warmed).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 0).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.I32_ADD)
		code, err = b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(callee, caller))
		want := runProgram(t, prog)

		var result types.Value
		var rc int
		var deopts, entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			if err = vm.Run(context.Background()); err != nil {
				return true
			}
			result, _ = vm.Pop()
			c, _ := vm.Const(0)
			rc, _ = vm.RefCount(c.Ref())
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, err)
		require.Zero(t, deopts)
		require.Equal(t, want, result)
		require.Equal(t, 1, rc)
	})

	t.Run("sums a typed i32 array in a function that also calls, matching threaded", func(t *testing.T) {
		native(t)
		prog := typedArrayCallsProgram(t, 20000)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reads and writes a ref array element natively, matching threaded RefCount", func(t *testing.T) {
		native(t)
		// Each interpreter gets its own freshly built program: array.set
		// mutates the array constant in place, and program.Program shares
		// that *types.Array Go object across every interp.New call that
		// reuses it, so a program run to completion once must not be reused
		// for a second run (want, then every retry below).
		wantValue, wantCount := runProgramString(t, refArrayProgram(t, 20000))

		var runErr, popErr error
		var value string
		var count int
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(refArrayProgram(t, 20000), interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("walks a struct tree natively through struct.get and ref.is_null, matching threaded", func(t *testing.T) {
		native(t)
		// Each interpreter gets its own freshly built program: the module's
		// own linking code (struct.set) mutates the leaf/mid/root struct
		// constants in place, another instance of the refArrayProgram
		// cross-run-reuse hazard above.
		want := runProgram(t, structTreeProgram(t, 20000))

		var runErr, popErr error
		var result types.Value
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(structTreeProgram(t, 20000), interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("an out-of-bounds array.get deopts and is caught by a guest handler, matching threaded", func(t *testing.T) {
		native(t)
		prog := arrayOOBProgram(t, 20000)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("a host array fails its shape guard and deopts, matching threaded", func(t *testing.T) {
		native(t)
		prog := hostArrayGlobalProgram(t, 20000)

		threaded := interp.New(prog)
		defer threaded.Close()
		hostVal, err := threaded.Marshal([]int32{1, 2, 3, 4, 5})
		require.NoError(t, err)
		hostAddr, err := threaded.Alloc(hostVal)
		require.NoError(t, err)
		require.NoError(t, threaded.SetGlobal(0, types.BoxRef(hostAddr)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(5), want)

		var runErr, popErr, marshalErr, allocErr, globalErr error
		var result types.Value
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			hostVal, marshalErr := vm.Marshal([]int32{1, 2, 3, 4, 5})
			if marshalErr != nil {
				return true
			}
			hostAddr, allocErr := vm.Alloc(hostVal)
			if allocErr != nil {
				return true
			}
			globalErr = vm.SetGlobal(0, types.BoxRef(hostAddr))
			if globalErr != nil {
				return true
			}
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, marshalErr)
		require.NoError(t, allocErr)
		require.NoError(t, globalErr)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("a container guard refuted by a null array recompiles with its site bridged: later nulls trap without retiring it, matching threaded", func(t *testing.T) {
		native(t)
		const rounds, batches = 64, 8
		prog := rareNullProgram(t, rounds)
		want := runProgram(t, prog)
		require.Equal(t, types.I32(rounds/16), want)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		entries := func() float64 {
			return metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"}) +
				metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
		}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
			time.Sleep(time.Millisecond)
		}

		before := entries()
		for range batches {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		require.GreaterOrEqual(t, entries()-before, float64(batches*rounds))
	})

	t.Run("a quiet loop reading a null array only under a condition never true stays native across runs, matching threaded", func(t *testing.T) {
		native(t)
		const runs = 32
		// A null []i32 local read only under a branch no iteration takes.
		b := instr.NewBuilder()
		loop, skip, done := b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 0).Emit(instr.I32_GE_S).BrIf(skip)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Bind(skip)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.NewArrayType(types.TypeI32), types.TypeI32, types.TypeI32))
		want := runProgram(t, prog)

		var errs []error
		var results []types.Value
		var entries, deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			errs, results = nil, nil
			for range runs {
				if err := vm.Run(context.Background()); err != nil {
					errs = append(errs, err)
					return true
				}
				result, err := vm.Pop()
				if err != nil {
					errs = append(errs, err)
					return true
				}
				results = append(results, result)
				vm.Reset()
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return entries >= runs/2
		}, 5*time.Second, time.Millisecond)
		require.Empty(t, errs)
		require.Len(t, results, runs)
		for _, result := range results {
			require.Equal(t, want, result)
		}
		require.Less(t, deopts, float64(2))
	})

	t.Run("an array replaced inside an Optimized loop is guarded afresh each iteration, matching threaded", func(t *testing.T) {
		native(t)
		// The []i32 local is replaced by a fresh array of varying length every iteration.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		b.Emit(instr.ARRAY_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_SET)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 3).Emit(instr.I32_AND).Emit(instr.ARRAY_GET).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 2)
		code, err := b.Assemble()
		require.NoError(t, err)
		elem := types.NewArrayType(types.TypeI32)
		prog := program.New(code, program.WithLocals(elem, types.TypeI32, types.TypeI32), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			result, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("stays off by default: no vm_jit metrics are reported", func(t *testing.T) {
		profiler := prof.New()
		vm := interp.New(fibCallsProgram(t, 3), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()

		for _, m := range profiler.Metrics() {
			require.NotContains(t, m.Name, "vm_jit_")
		}
	})

	t.Run("OSR enters a module-code loop natively, matching threaded", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("OSR compiles a module loop straight to Optimized, matching threaded", func(t *testing.T) {
		native(t)
		const n = 20000
		want := runProgram(t, concat(t, n))
		prog := concat(t, n)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Greater(t, compiles, float64(0))
	})

	t.Run("a callee deopting under an Optimized loop restores the loop's promoted locals", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := runProgram(t, deopt(t, n, n-10))
		prog := deopt(t, n, n-10)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.Greater(t, compiles, float64(0))
		deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Greater(t, deopts, float64(0))
	})

	t.Run("a callee reached only from native code still tiers up to Optimized", func(t *testing.T) {
		native(t)
		const n = 200_000
		want := runProgram(t, fib(t, n))
		prog := fib(t, n)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		vm.Flush()

		require.Equal(t, want, got)
		// One Optimized compile is the header's own OSR unit; a second is
		// only possible if fib, called solely from that native loop, also
		// reached the promote threshold.
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		require.GreaterOrEqual(t, compiles, float64(2))
	})

	t.Run("a callee reached only from native code tiers up across Runs too short to reach a safepoint", func(t *testing.T) {
		native(t)
		// inc is entered once per iteration: its 1024 Baseline entries
		// arrive only after the loop itself runs natively.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 100).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var runErr, popErr error
		var compiles float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiles, float64(2))
	})

	t.Run("OSR resolved during earlier calls enters at a function's loop header mid-call", func(t *testing.T) {
		native(t)
		// warmCalls*warmEach back edges warm sum's own loop-header OSR site
		// across calls too few (warmCalls) to ever reach its entry-0 CALL
		// threshold on their own, so entry-0 never compiles: the final call
		// starts threaded and can only enter native code through OSR, mid-call,
		// once the site resolves.
		const threshold, warmCalls, warmEach, n = 1000, 20, 300, 200_000
		prog := sumHeaderProgram(t, warmCalls, warmEach, n)
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(threshold), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("a deopt inside an OSR'd loop called from module code is caught by a guest handler, matching threaded including RefCount", func(t *testing.T) {
		native(t)
		// The OSR-eligible loop lives in a called function with no handlers
		// of its own; the module's handler wraps the call and catches the
		// real trap once it unwinds out of that frame.
		prog := moduleDivCaughtProgram(t, 2_000_000, 1_500_000)
		wantValue, wantCode := runModuleDivCaught(t, prog)

		var gotCode types.Boxed
		var gotValue int
		var exits float64
		var runErr, popErr, constErr, refErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotCode, popErr = vm.PopBoxed()
			if popErr != nil {
				return true
			}
			str, err := vm.Const(0)
			constErr = err
			if constErr != nil {
				return true
			}
			gotValue, refErr = vm.RefCount(str.Ref())
			if refErr != nil {
				return true
			}
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, constErr)
		require.NoError(t, refErr)
		require.Equal(t, wantCode, gotCode)
		require.Equal(t, wantValue, gotValue)
	})

	t.Run("a loop header without a trapping operation cannot compile: OSR unwraps and still matches threaded", func(t *testing.T) {
		native(t)
		prog := emptyHeaderProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var unsupported float64
		var runErr, popErr error
		var vm *interp.Interpreter
		var profiler *prof.Profiler
		require.Eventually(t, func() bool {
			if vm != nil {
				vm.Close()
			}
			profiler = prof.New()
			vm = interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
			return unsupported > 0
		}, 5*time.Second, time.Millisecond)
		defer vm.Close()
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		// A site submits its OSR unit once: the failed compile is never
		// resubmitted, however many further back edges the loop takes.
		require.Equal(t, float64(1), unsupported)

		// The failed site restores its threaded handler and stops polling:
		// a second Run on the same interpreter takes the same many back
		// edges again, and the metric does not move.
		vm.Reset()
		require.NoError(t, vm.Run(context.Background()))
		got2, popErr2 := vm.Pop()
		require.NoError(t, popErr2)
		require.Equal(t, want, got2)
		vm.Flush()
		again, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
		require.Equal(t, float64(1), again)
	})

	t.Run("a loop header whose body always bridges retires its OSR site and never re-enters native code", func(t *testing.T) {
		native(t)
		const n = 600
		prog := bridge(t, n)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		ctx := context.Background()

		// The header's own back edges cross its cadence within the first
		// Run and it retires (refute deopts) within a handful of Runs. The
		// ip-0 site's submit waits for the header to resolve (resolved),
		// then needs its own cadence (interval) of Runs for a submit
		// window and again for its first lookup to drain and publish the
		// compile: at n=600 back edges per Run that lands around Run 512.
		var got types.Value
		var runErr, popErr error
		var compiles float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(ctx)
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		// Two submits, two compiles: the header's own OSR unit, and module
		// code's own ip-0 unit (reachable prefix and loop both, including
		// the same always-bridging op); every native entry bridges immediately.
		require.Equal(t, float64(2), compiles)

		// The ip-0 unit's own bridge-and-deopt count starts fresh once
		// published (materializing hides the rest of a Run from its
		// observer, so it takes one Run per deopt, like the header's own
		// retirement): run comfortably past refute (8) deopts so it
		// retires too, then confirm two further runs report the same
		// steady-state exits count.
		for range 12 {
			require.NoError(t, vm.Run(ctx))
			_, err := vm.Pop()
			require.NoError(t, err)
			vm.Reset()
		}
		vm.Flush()
		exits, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})

		require.NoError(t, vm.Run(ctx))
		got2, err2 := vm.Pop()
		require.NoError(t, err2)
		require.Equal(t, want, got2)
		vm.Reset()
		vm.Flush()
		compilesAgain, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		exitsAgain, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, float64(2), compilesAgain)
		require.Equal(t, exits, exitsAgain)
	})

	t.Run("OSR survives Reset: a second run reuses the resolved site", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var gotFirst, gotSecond types.Value
		var entries float64
		var runErr, firstPopErr, secondPopErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()

			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			first, err := vm.Pop()
			firstPopErr = err
			if firstPopErr != nil {
				return true
			}
			vm.Reset()

			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			second, err := vm.Pop()
			secondPopErr = err
			if secondPopErr != nil {
				return true
			}
			vm.Flush()
			gotFirst, gotSecond = first, second
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, firstPopErr)
		require.NoError(t, secondPopErr)
		require.Equal(t, want, gotFirst)
		require.Equal(t, want, gotSecond)
	})

	t.Run("loop-free module code enters native code at ip 0 once hot, matching threaded", func(t *testing.T) {
		native(t)
		// Loop-free module code: its only OSR site is ip 0.
		b := instr.NewBuilder()
		big, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 7).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5).Emit(instr.I32_GT_S).BrIf(big)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 2).Emit(instr.I32_MUL).Br(done)
		b.Bind(big).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 3).Emit(instr.I32_MUL)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var entries float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 2*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("module code with a loop enters at ip 0 after its header unit publishes", func(t *testing.T) {
		native(t)
		// Straight-line prefix (i, a, b init) before the header, like the
		// loop-free case, but here the loop makes it a module-with-loop ip-0
		// site: no per-ip metric exists, so readiness uses both units'
		// optimized compiles (header's own OSR unit plus ip 0's), the same
		// proxy the callee-tiers-up case below uses.
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var compiles float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("the ip-0 unit of module code with loops never compiles before its header units resolve", func(t *testing.T) {
		native(t)
		// i starts past the loop's own exit check, so the header is a loop
		// (analysis.Headers reads the back edge statically) but its Br(loop)
		// is never taken: the header is hit once per Run — a fall-through,
		// not a back edge — matching the entry site's own once-per-Run
		// count exactly. With WithThreshold(3) both reach threshold on the
		// same Run, the entry site's ip-0 handler dispatching first in
		// program order: an ungated entry site would win the address's one
		// queue slot and block the header until its next retry, cadence
		// (256) Runs later. A gated entry site waits, so both resolve fast.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(3), interp.WithProfiler(profiler))
		defer vm.Close()

		var results []types.Value
		var errs []error
		var compiles float64
		require.Eventually(t, func() bool {
			runErr := vm.Run(context.Background())
			if runErr != nil {
				errs = append(errs, runErr)
				return true
			}
			result, popErr := vm.Pop()
			if popErr != nil {
				errs = append(errs, popErr)
				return true
			}
			results = append(results, result)
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		}, 5*time.Second, time.Millisecond)
		require.Empty(t, errs)
		for _, result := range results {
			require.Equal(t, want, result)
		}
	})

	t.Run("module code whose prefix bridges keeps entering through its headers", func(t *testing.T) {
		native(t)
		// STRING_EQ sits in the straight-line prefix, outside every loop:
		// the ip-0 unit's own compile fails the prefix gate
		// (internal/jit/compile Lower's l.gate, ErrUnsupported), so drain
		// restores its threaded handler and deletes its site
		// (interp/native.go drain, OSR branch) instead of ever entering or
		// retrying. The header's own unit, translated from the loop alone,
		// carries no prefix and is unaffected.
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.STRING_EQ).Emit(instr.DROP)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 5_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(types.String("x")))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var got types.Value
		var runErr, popErr error
		var entries, unsupported float64
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
			return entries > 0 && unsupported > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("a callee reached only from natively entered module code still tiers up to Optimized", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 41).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(incFunction()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		// One Optimized compile is module code's ip-0 unit; a second is
		// only possible if inc, called solely from it, also tiered up.
		var got types.Value
		var compiles float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles >= 2
		}, 2*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("a trap in natively entered module code reports threaded's error every run", func(t *testing.T) {
		native(t)
		// Divides by a zero local: every run traps.
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 10).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_DIV_S)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32))
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var errs []error
		var entries float64
		require.Eventually(t, func() bool {
			errs = append(errs, vm.Run(context.Background()))
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 2*time.Second, time.Millisecond)
		for _, err := range errs {
			require.True(t, errorsEqual(err, wantErr))
		}
	})

	t.Run("a cancelled context stops hot loop-free module code as threaded does", func(t *testing.T) {
		native(t)
		// Loop-free module code longer than one tick.
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 0)
		for range 200 {
			b.Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		wantErr := func() error {
			vm := interp.New(prog)
			defer vm.Close()
			return vm.Run(ctx)
		}()
		require.ErrorIs(t, wantErr, context.Canceled)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()

		var entries float64
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return runErr != nil || entries > 0
		}, 2*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.ErrorIs(t, vm.Run(ctx), context.Canceled)
	})

	t.Run("a Pool of two interpreters shares a published OSR code", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var gotFirst, gotSecond types.Value
		var entries float64
		var runErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			pool := interp.NewPool(prog, 2, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer pool.Close()

			first, err := pool.Get(context.Background())
			if err != nil {
				runErr = err
				return true
			}
			second, err := pool.Get(context.Background())
			if err != nil {
				runErr = err
				return true
			}

			runErr = first.Run(context.Background())
			if runErr != nil {
				return true
			}
			firstResult, err := first.Pop()
			if err != nil {
				runErr = err
				return true
			}

			runErr = second.Run(context.Background())
			if runErr != nil {
				return true
			}
			secondResult, err := second.Pop()
			if err != nil {
				runErr = err
				return true
			}

			first.Flush()
			second.Flush()
			gotFirst, gotSecond = firstResult, secondResult
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})

			pool.Put(first)
			pool.Put(second)
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.Equal(t, want, gotFirst)
		require.Equal(t, want, gotSecond)
	})

	t.Run("an indirect self call through a parameter runs native and matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		// Both dynamic sites are recorded before fib compiles.
		prog := indirectFibCallsProgram(t, 20, 50)

		wantVM := interp.New(indirectFibCallsProgram(t, 20, 50))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantConst, err := wantVM.Const(0)
		require.NoError(t, err)
		wantRC, err := wantVM.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotRC int
		var compiles, entries, deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(100), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotConst, err := vm.Const(0)
			if err != nil {
				popErr = err
				return true
			}
			gotRC, rcErr = vm.RefCount(gotConst.Ref())
			if rcErr != nil {
				return true
			}
			vm.Flush()
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return compiles > 0 && entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantRC, gotRC)
		require.Zero(t, deopts)
	})

	t.Run("a unit retired by a trap at a site recorded since recompiles from the new feedback and stays native", func(t *testing.T) {
		native(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		run := func() {
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		require.Eventually(t, func() bool {
			err := vm.Run(context.Background())
			vm.Reset()
			return err != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		}, 5*time.Second, time.Millisecond)
		// Settled: the recompiled code traps at no site.
		for range 16 {
			run()
			time.Sleep(time.Millisecond)
		}
		deopts := metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
		for range 16 {
			run()
		}
		require.Equal(t, deopts, metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}))
	})

	t.Run("a unit compiled before its dynamic sites ran compiles and traps at them instead of declining, matching threaded", func(t *testing.T) {
		native(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		var runErr, popErr error
		var got types.Value
		var unsupported, ok float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			vm.Flush()
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"})
			ok, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return ok >= 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Zero(t, unsupported)
	})

	t.Run("a speculated callee refuted by a second function deopts, retires, and matches threaded, including RefCount", func(t *testing.T) {
		native(t)
		prog := applyProgram(t, 3000)

		wantVM := interp.New(applyProgram(t, 3000))
		defer wantVM.Close()
		require.NoError(t, wantVM.Run(context.Background()))
		want, err := wantVM.Pop()
		require.NoError(t, err)
		wantInc, err := wantVM.Const(1)
		require.NoError(t, err)
		wantIncRC, err := wantVM.RefCount(wantInc.Ref())
		require.NoError(t, err)
		wantDec, err := wantVM.Const(2)
		require.NoError(t, err)
		wantDecRC, err := wantVM.RefCount(wantDec.Ref())
		require.NoError(t, err)

		var runErr, popErr, rcErr error
		var got types.Value
		var gotIncRC, gotDecRC int
		var deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			if popErr != nil {
				return true
			}
			gotInc, err := vm.Const(1)
			if err != nil {
				popErr = err
				return true
			}
			gotIncRC, rcErr = vm.RefCount(gotInc.Ref())
			if rcErr != nil {
				return true
			}
			gotDec, err := vm.Const(2)
			if err != nil {
				popErr = err
				return true
			}
			gotDecRC, rcErr = vm.RefCount(gotDec.Ref())
			if rcErr != nil {
				return true
			}
			vm.Flush()
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts >= 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, rcErr)
		require.Equal(t, want, got)
		require.Equal(t, wantIncRC, gotIncRC)
		require.Equal(t, wantDecRC, gotDecRC)
		require.GreaterOrEqual(t, deopts, float64(1))
		// A retired site stops deopting long before its 3000 refuting calls end.
		require.Less(t, deopts, float64(3000))
	})

	t.Run("a speculated callee site turned polymorphic recompiles generic after its refutation and stays native through call exits, matching threaded", func(t *testing.T) {
		native(t)
		const n, runs = 200, 16
		prog := turnProgram(t, n)

		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.SetGlobal(0, types.BoxI32(1)))
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			vm.Flush()
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		deopts := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"}) }
		calls := func() float64 { return metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"}) }
		// Reset clears globals: every polymorphic Run sets global 0 again.
		run := func() {
			require.NoError(t, vm.SetGlobal(0, types.BoxI32(1)))
			require.NoError(t, vm.Run(context.Background()))
			got, err := vm.Pop()
			require.NoError(t, err)
			require.Equal(t, want, got)
			vm.Reset()
		}
		// Monomorphic until relay compiles with its one observed callee.
		compiled := prof.Label{Key: "tier", Value: "baseline"}
		ok := prof.Label{Key: "outcome", Value: "ok"}
		var runErr error
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			vm.Reset()
			return runErr != nil || metric("vm_jit_compiles_total", compiled, ok) >= 2
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		for range runs {
			require.NoError(t, vm.Run(context.Background()))
			vm.Reset()
		}

		turned := deopts()
		for range runs {
			run()
			time.Sleep(time.Millisecond)
		}
		// Fewer than refute: the moved feedback retires the code at once.
		require.Less(t, deopts()-turned, float64(8))

		settled, served := deopts(), calls()
		for range runs {
			run()
		}
		require.Equal(t, settled, deopts())
		require.Greater(t, calls(), served)
	})

	t.Run("a closure callee's site traps until native code records it, and the caller matches threaded", func(t *testing.T) {
		native(t)
		prog := closureCallsProgram(t, 50)
		want := runProgram(t, prog)

		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("native closure calls match threaded on value and RefCount, with native entries and no deopt or call exits per Run", func(t *testing.T) {
		native(t)
		prog := counterProgram(t)

		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCounter, err := threaded.Pop()
		require.NoError(t, err)
		wantValue, wantCount, err := popString(threaded)
		require.NoError(t, err)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry := prof.Label{Key: "tier", Value: "optimized"}
		deopt, call := prof.Label{Key: "kind", Value: "deopt"}, prof.Label{Key: "kind", Value: "call"}

		var counter types.Value
		var value string
		var count int
		var runErr, popErr error
		var compiled, entries, deopts, called float64
		require.Eventually(t, func() bool {
			vm.Flush()
			entered, deopted, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", deopt), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if counter, popErr = vm.Pop(); popErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, deopts, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", deopt)-deopted, metric("vm_jit_exits_total", call)-exited
			// The closure body is the only Baseline unit: compiled, it is
			// called natively, or the Run takes a call exit.
			compiled, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiled > 0 && entries > 0 && deopts == 0 && called == 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantCounter, counter)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Positive(t, compiled)
		require.Positive(t, entries)
		require.Zero(t, deopts)
		require.Zero(t, called)
	})

	t.Run("a native caller keeps entering while its closure callee's Baseline is pending", func(t *testing.T) {
		native(t)
		// The callee counts only on its ExitCalls, so at a threshold
		// above refute its Baseline is pending for more entries than a
		// refuted caller survives.
		prog := counterProgram(t)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(2*8), interp.WithProfiler(profiler))
		defer vm.Close()

		metric := func(name string, label prof.Label) float64 {
			v, _ := profiler.Metric(name, label)
			return v
		}
		entry, call := prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "kind", Value: "call"}

		var got types.Value
		var runErr, popErr error
		var entries, called float64
		require.Eventually(t, func() bool {
			vm.Flush()
			entered, exited := metric("vm_jit_entries_total", entry), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, called = metric("vm_jit_entries_total", entry)-entered, metric("vm_jit_exits_total", call)-exited
			return entries > 0 && called == 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Positive(t, entries)
		require.Zero(t, called)
	})

	t.Run("a closure body reached only from native code compiles through served calls without a threaded hook", func(t *testing.T) {
		native(t)
		counter := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
			Captures(types.TypeI32).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.DUP), instr.New(instr.UPVAL_SET, 0), instr.New(instr.RETURN)).
			MustBuild()
		gb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeAny, types.TypeI32)
		loop, done := gb.Label(), gb.Label()
		gb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.LOCAL_SET, 1))
		gb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 64), instr.New(instr.I32_GE_S)).BrIf(done)
		gb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.DROP))
		gb.Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2)).Br(loop)
		gb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 7).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(counter, gb.MustBuild()))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline, optimized := prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "tier", Value: "optimized"}
		call := prof.Label{Key: "kind", Value: "call"}
		entered := func() float64 {
			return metric("vm_jit_entries_total", baseline) + metric("vm_jit_entries_total", optimized)
		}

		var got types.Value
		var runErr, popErr error
		var compiled, entries, called float64
		require.Eventually(t, func() bool {
			vm.Flush()
			before, exited := entered(), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			entries, called = entered()-before, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && entries > 0 && called == 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, entries)
		require.Zero(t, called)
	})

	t.Run("a declined bridge inside a natively called closure rebuilds its frame with its upvals", func(t *testing.T) {
		native(t)
		const calls = 64
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI1}, Returns: []types.Type{types.TypeI32}}).Captures(types.TypeI32)
		loop, done, slow := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_EQZ)).BrIf(done)
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.UPVAL_SET, 0))
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1)).BrIf(slow)
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN))
		fb.Bind(slow).Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW, 0), instr.New(instr.ARRAY_LEN))
		fb.Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_ADD), instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
		gb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Locals(types.TypeAny, types.TypeI32, types.TypeI32)
		loop, done = gb.Label(), gb.Label()
		gb.Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CLOSURE_NEW), instr.New(instr.LOCAL_SET, 0))
		gb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, calls), instr.New(instr.I32_GE_S)).BrIf(done)
		gb.Emit(instr.New(instr.I32_CONST, 100), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, calls-1), instr.New(instr.I32_EQ))
		gb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CALL), instr.New(instr.LOCAL_SET, 2))
		gb.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1)).Br(loop)
		gb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		b.Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fb.MustBuild(), gb.MustBuild()), program.WithTypes(types.NewArrayType(types.TypeI32)))
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		metric := func(name string, labels ...prof.Label) float64 {
			v, _ := profiler.Metric(name, labels...)
			return v
		}
		baseline := prof.Label{Key: "tier", Value: "baseline"}
		call := prof.Label{Key: "kind", Value: "call"}
		bridge := prof.Label{Key: "kind", Value: "bridge"}

		var got types.Value
		var runErr, popErr error
		var compiled, bridged, called float64
		require.Eventually(t, func() bool {
			vm.Flush()
			declined, exited := metric("vm_jit_exits_total", bridge), metric("vm_jit_exits_total", call)
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if got, popErr = vm.Pop(); popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			// A closure runs natively only through a native caller: an
			// ARRAY_NEW bridge exit, which never resumes, with no call exit
			// means every closure call ran natively, the last one into it.
			compiled = metric("vm_jit_compiles_total", baseline, prof.Label{Key: "outcome", Value: "ok"})
			bridged, called = metric("vm_jit_exits_total", bridge)-declined, metric("vm_jit_exits_total", call)-exited
			return compiled >= 2 && bridged > 0 && called == 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.GreaterOrEqual(t, compiled, float64(2))
		require.Positive(t, bridged)
		require.Zero(t, called)
	})

	t.Run("a bridged CLOSURE_NEW resumes native code and matches threaded, including a capture's RefCount", func(t *testing.T) {
		native(t)
		const n = 200_000
		fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeString}}).
			Captures(types.TypeString).
			Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN)).
			MustBuild()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, n).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
		b.Bind(inner)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(innerDone)
		b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		b.Br(inner)
		b.Bind(innerDone)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny, types.TypeI32), program.WithConstants(types.String("held"), fn))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			// A declined bridge deopts and its site retires within a few
			// entries; only resumed ones number in the thousands.
			return bridges > 1000
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
		require.Greater(t, bridges, float64(1000))
		require.Zero(t, deopts)
	})

	t.Run("a function with captures called directly stays threaded and faults on its upval as threaded does", func(t *testing.T) {
		native(t)
		const calls = 200
		fb := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI1}, Returns: []types.Type{types.TypeI32}}).Captures(types.TypeI32)
		loop, done, read := fb.Label(), fb.Label(), fb.Label()
		fb.Bind(loop).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_EQZ)).BrIf(done)
		fb.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 0)).Br(loop)
		fb.Bind(done).Emit(instr.New(instr.LOCAL_GET, 1)).BrIf(read)
		fb.Emit(instr.New(instr.I32_CONST, 7), instr.New(instr.RETURN))
		fb.Bind(read).Emit(instr.New(instr.UPVAL_GET, 0), instr.New(instr.RETURN))
		b := instr.NewBuilder()
		loop, done = b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1000).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, calls-1).Emit(instr.I32_EQ)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fb.MustBuild()))
		want := runProgramErr(t, prog)
		require.Error(t, want)

		got := runProgramErr(t, prog, interp.WithThreshold(0))
		require.True(t, errorsEqual(got, want), "got %v, want %v", got, want)
	})

	t.Run("a natively called function reads each unwritten local as its declared kind's zero, as threaded does", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 20_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		for range zeroTypes {
			b.Emit(instr.DROP)
		}
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(zeroFunction()))

		var got []types.Value
		var entries float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range zeroValues {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, zeroValues, got)
	})

	t.Run("OSR leaves each unwritten module local and global at its declared kind's zero, as threaded does", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 200_000).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		for j := range zeroTypes {
			b.Emit(instr.LOCAL_GET, uint64(j+1))
		}
		for j := range zeroTypes {
			b.Emit(instr.GLOBAL_GET, uint64(j))
		}
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(append([]types.Type{types.TypeI32}, zeroTypes...)...), program.WithGlobals(zeroTypes...))
		want := slices.Concat(zeroValues, zeroValues)

		var got []types.Value
		var entries float64
		var runErr, popErr error
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got = got[:0]
			for range want {
				var v types.Value
				if v, popErr = vm.Pop(); popErr != nil {
					return true
				}
				got = append(got, v)
			}
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("every primitive operator matches threaded bit for bit in native code", func(t *testing.T) {
		native(t)
		w32 := func(v int32) uint64 { return uint64(uint32(v)) }
		w64 := func(v int64) uint64 { return uint64(v) }
		f32 := func(v float32) uint64 { return uint64(math.Float32bits(v)) }
		f64 := math.Float64bits
		nan32, neg32 := float32(math.NaN()), float32(math.Copysign(0, -1))
		i32, i64, t32, t64 := types.TypeI32, types.TypeI64, types.TypeF32, types.TypeF64

		for _, c := range []struct {
			code   instr.Opcode
			params []types.Type
			args   [][]uint64
		}{
			{instr.I32_CLZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_CTZ, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(math.MinInt32)}}},
			{instr.I32_POPCNT, []types.Type{i32}, [][]uint64{{w32(0)}, {w32(-1)}, {w32(0x55555555)}}},
			{instr.I32_ROTL, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I32_ROTR, []types.Type{i32, i32}, [][]uint64{{w32(0x12345678), w32(4)}, {w32(0x12345678), w32(-1)}, {w32(0x12345678), w32(37)}}},
			{instr.I64_CLZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_CTZ, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(1 << 47)}}},
			{instr.I64_POPCNT, []types.Type{i64}, [][]uint64{{w64(0)}, {w64(-1)}, {w64(0x555555555555)}}},
			{instr.I64_ROTL, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.I64_ROTR, []types.Type{i64, i64}, [][]uint64{{w64(0x123456789ABC), w64(4)}, {w64(0x123456789ABC), w64(-1)}, {w64(0x123456789ABC), w64(69)}}},
			{instr.F32_COPYSIGN, []types.Type{t32, t32}, [][]uint64{{f32(1.5), f32(-1)}, {f32(nan32), f32(-1)}, {f32(0), f32(neg32)}}},
			{instr.F64_COPYSIGN, []types.Type{t64, t64}, [][]uint64{{f64(1.5), f64(-1)}, {f64(math.NaN()), f64(-1)}, {f64(0), f64(math.Copysign(0, -1))}}},
			{instr.F32_REM, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F32_MOD, []types.Type{t32, t32}, [][]uint64{{f32(5.5), f32(2)}, {f32(-5.5), f32(2)}}},
			{instr.F64_REM, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
			{instr.F64_MOD, []types.Type{t64, t64}, [][]uint64{{f64(5.5), f64(2)}, {f64(-5.5), f64(2)}}},
		} {
			fb := types.NewFunctionBuilder(&types.FunctionType{Params: c.params, Returns: c.params[:1]})
			for i := range c.params {
				fb.Emit(instr.New(instr.LOCAL_GET, uint64(i)))
			}
			fn := fb.Emit(instr.New(c.code), instr.New(instr.RETURN)).MustBuild()

			for _, args := range c.args {
				// The module calls fn with args 1000 times so fn compiles,
				// then once more for the result.
				b := instr.NewBuilder()
				loop, done := b.Label(), b.Label()
				call := func() {
					for i, arg := range args {
						b.Emit(map[types.Kind]instr.Opcode{types.KindI32: instr.I32_CONST, types.KindI64: instr.I64_CONST, types.KindF32: instr.F32_CONST, types.KindF64: instr.F64_CONST}[c.params[i].Kind()], arg)
					}
					b.Emit(instr.CONST_GET, 0).Emit(instr.CALL)
				}
				b.Bind(loop).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1000).Emit(instr.I32_GE_S).BrIf(done)
				call()
				b.Emit(instr.DROP).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0).Br(loop)
				b.Bind(done)
				call()
				code, err := b.Assemble()
				require.NoError(t, err)
				prog := program.New(code, program.WithLocals(i32), program.WithConstants(fn))

				// Boxed words compare bits: NaN payloads and the sign of zero.
				threaded := interp.New(prog)
				require.NoError(t, threaded.Run(context.Background()))
				want, err := threaded.PopBoxed()
				require.NoError(t, err)
				require.NoError(t, threaded.Close())

				var got types.Boxed
				var entries, deopts float64
				var runErr, popErr error
				require.Eventually(t, func() bool {
					profiler := prof.New()
					vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
					defer vm.Close()
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					if got, popErr = vm.PopBoxed(); popErr != nil {
						return true
					}
					vm.Flush()
					entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
					deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
					return entries > 0
				}, 5*time.Second, time.Millisecond)
				require.NoError(t, runErr, "%s %x", c.code, args)
				require.NoError(t, popErr, "%s %x", c.code, args)
				require.Equal(t, want, got, "%s %x", c.code, args)
				require.Positive(t, entries, "%s %x", c.code, args)
				require.Zero(t, deopts, "%s %x", c.code, args)
			}
		}
	})
}

// iterativeFibProgram computes the nth Fibonacci number in a module-level
// loop, carrying every value in locals so the loop header's own operand
// stack is empty: a module-code OSR site, address 0 included.
func iterativeFibProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // a = 0
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 2) // b = 1
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3) // tmp = a+b
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 1)                                              // a = b
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.LOCAL_SET, 2)                                              // b = tmp
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0) // i++
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32))
}

// concat runs an address-0 loop with one unpromoted any local beside four
// promoted i32 locals. STRING_CONCAT bridges with the locals live in the exit map.
func concat(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 4)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 3).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 7).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_ADD).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_ADD)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32),
		program.WithConstants(types.String("seed-"), types.String("tail")))
}

// deopt loops at module level over n calls of f(i) = i, except f(k), which
// adds string.eq of a constant with itself: an operation native code neither
// lowers nor resumes, so f's native code deoptimizes there while the loop's
// promoted locals live in the caller's native frame.
func deopt(t *testing.T, n, k int) *program.Program {
	t.Helper()
	fb := instr.NewBuilder()
	slow := fb.Label()
	fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(k)).Emit(instr.I32_EQ).BrIf(slow)
	fb.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	fb.Bind(slow).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 1).Emit(instr.STRING_EQ).Emit(instr.I32_ADD).Emit(instr.RETURN)
	fcode, err := fb.Assemble()
	require.NoError(t, err)
	f := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(fcode),
	}

	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	for slot := range 4 {
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, uint64(slot))
	}
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 3).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_ADD)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32),
		program.WithConstants(f, types.String("abc")))
}

// counterProgram builds one closure over an i32 counter and a string, calls
// it 64 times from loop-free module code, and leaves the last call's
// results: the ClosureCounter shape.
func counterProgram(t *testing.T) *program.Program {
	t.Helper()
	fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeString, types.TypeI32}}).
		Captures(types.TypeI32, types.TypeString).
		Emit(
			instr.New(instr.UPVAL_GET, 1), instr.New(instr.UPVAL_SET, 1), instr.New(instr.UPVAL_GET, 1),
			instr.New(instr.UPVAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.DUP), instr.New(instr.UPVAL_SET, 0),
			instr.New(instr.RETURN),
		).MustBuild()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 0)
	for range 64 {
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.CALL).Emit(instr.DROP).Emit(instr.DROP)
	}
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeAny), program.WithConstants(fn, types.String("held")))
}

// fib loops at module level and calls fib(8). Once the header is native,
// the callee enters native code directly and its prologue counts every entry.
func fib(t *testing.T, n int) *program.Program {
	t.Helper()
	fib := fibFunction()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 8).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib))
}

// sumHeaderProgram calls sum(warmEach) warmCalls times, then sum(n) once,
// leaving the final call's result on the stack. warmCalls*warmEach back
// edges warm sum's own loop-header OSR site across calls, but warmCalls
// itself stays far below any reasonable CALL threshold, so entry-0 never
// gets its own CALL-based compile: the final call starts threaded and can
// only enter native code through the already-resolved OSR site, mid-call.
func sumHeaderProgram(t *testing.T, warmCalls, warmEach, n int) *program.Program {
	t.Helper()
	sum := sumFunction(t)
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warmCalls)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, uint64(warmEach)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
}

// loopDivFunction sums 100/(i-k) for i in [0,n), k fixed well past OSR's own
// submit threshold: i==k divides by zero mid-loop, after OSR has resolved
// and entered. It has no handlers of its own, so the trap unwinds its frame
// to the module's handler.
func loopDivFunction(t *testing.T, n, k int) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	loop, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // sum = 0
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.I32_CONST, 100)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(k)).Emit(instr.I32_SUB) // i-k
	b.Emit(instr.I32_DIV_S)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeI32, types.TypeI32},
		Code:   instr.Marshal(code),
	}
}

// moduleDivCaughtProgram calls loopDivFunction inside a module-level Try:
// the callee's own loop OSR-compiles and, once it deopts on the trapping
// iteration, the real trap unwinds the callee's frame (no handler there) up
// to the module's own handler. The call is fused CONST_GET;CALL, borrowing
// loopDivFunction's own reference, so its RefCount must return to the
// constant pool's own baseline once the unwind completes.
func moduleDivCaughtProgram(t *testing.T, n, k int) *program.Program {
	t.Helper()
	fn := loopDivFunction(t, n, k)
	b := instr.NewBuilder()
	start, end, catch := b.Label(), b.Label(), b.Label()
	b.Bind(start).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE)
	b.Try(start, end, catch, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
}

// runModuleDivCaught runs prog threaded and returns the "caught" constant's
// baseline RefCount and the caught error code moduleDivCaughtProgram leaves.
func runModuleDivCaught(t *testing.T, prog *program.Program) (int, types.Boxed) {
	t.Helper()
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	code, err := vm.PopBoxed()
	require.NoError(t, err)
	str, err := vm.Const(0)
	require.NoError(t, err)
	count, err := vm.RefCount(str.Ref())
	require.NoError(t, err)
	return count, code
}

// bridge loops n times at module level over MAP_KEYS, which native code
// neither lowers nor resumes: every native entry of its header bridges.
func bridge(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	header, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
	b.Bind(header)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 0).Emit(instr.MAP_NEW_DEFAULT, 0).Emit(instr.MAP_KEYS).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(header)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)))
}

// emptyHeaderProgram loops n times purely on raw local reads at its own
// header (LOCAL_GET flag; BrIf done — no comparison, so no operation there
// ever carries a deopt state): compile.Lower refuses a header without one,
// so its OSR site can never compile.
func emptyHeaderProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	header, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // counter = 0
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // flag = 0
	b.Bind(header)
	b.Emit(instr.LOCAL_GET, 1).BrIf(done)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)          // counter++
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).Emit(instr.LOCAL_SET, 1) // flag = counter >= n
	b.Br(header)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32))
}

// native skips a case that runs native code off arm64.
func native(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native execution needs arm64")
	}
}

// runProgram runs prog threaded and returns the single value it leaves on
// the stack.
func runProgram(t *testing.T, prog *program.Program) types.Value {
	t.Helper()
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	v, err := vm.Pop()
	require.NoError(t, err)
	return v
}

// runProgramErr runs prog threaded with opts and returns Run's error.
func runProgramErr(t *testing.T, prog *program.Program, opts ...interp.Option) error {
	t.Helper()
	vm := interp.New(prog, opts...)
	defer vm.Close()
	return vm.Run(context.Background())
}

// runProgramString runs prog threaded and returns the single stack ref
// value it leaves, as its string content and live RefCount.
func runProgramString(t *testing.T, prog *program.Program) (string, int) {
	t.Helper()
	vm := interp.New(prog)
	defer vm.Close()
	require.NoError(t, vm.Run(context.Background()))
	value, count, err := popString(vm)
	require.NoError(t, err)
	return value, count
}

// popString pops vm's single stack value, returning its string content and
// live RefCount. It does not release the reference PopBoxed transfers, since
// the caller inspects RefCount before the VM closes.
func popString(vm *interp.Interpreter) (string, int, error) {
	boxed, err := vm.PopBoxed()
	if err != nil {
		return "", 0, err
	}
	loaded, err := vm.Load(boxed.Ref())
	if err != nil {
		return "", 0, err
	}
	s, ok := loaded.(types.String)
	if !ok {
		return "", 0, interp.ErrTypeMismatch
	}
	count, err := vm.RefCount(boxed.Ref())
	if err != nil {
		return "", 0, err
	}
	return string(s), count, nil
}

// popLoop pops what loopProgram leaves: s, reporting its live RefCount and
// releasing the reference PopBoxed hands over, then the sum under it.
func popLoop(vm *interp.Interpreter) (types.Value, int, error) {
	boxed, err := vm.PopBoxed()
	if err != nil {
		return nil, 0, err
	}
	count, err := vm.RefCount(boxed.Ref())
	if err != nil {
		return nil, 0, err
	}
	if err := vm.Release(boxed.Ref()); err != nil {
		return nil, 0, err
	}
	sum, err := vm.Pop()
	return sum, count, err
}

// errorsEqual reports whether got and want both carry an *interp.RuntimeError
// with the same cause and the same call stack.
func errorsEqual(got, want error) bool {
	var g, w *interp.RuntimeError
	return errors.As(got, &g) && errors.As(want, &w) && reflect.DeepEqual(g, w)
}

// overflowEqual reports whether got and want both carry an
// *interp.RuntimeError over the same ErrFrameOverflow with the same
// function at every stack level. It ignores the innermost frame's IP: a
// deoptimized frame runs exact code, so a trap at a fused CONST_GET;CALL
// call site (fib's own recursive call) resumes at CALL's own IP rather than
// the fused unit's, unlike the threaded baseline which never separates them.
func overflowEqual(got, want error) bool {
	var g, w *interp.RuntimeError
	if !errors.As(got, &g) || !errors.As(want, &w) {
		return false
	}
	if !errors.Is(g.Err, interp.ErrFrameOverflow) || !errors.Is(w.Err, interp.ErrFrameOverflow) {
		return false
	}
	if len(g.Frames) != len(w.Frames) {
		return false
	}
	for i := range g.Frames {
		if g.Frames[i].Func != w.Frames[i].Func {
			return false
		}
		if i > 0 && g.Frames[i].IP != w.Frames[i].IP {
			return false
		}
	}
	return true
}

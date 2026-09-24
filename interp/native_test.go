package interp_test

import (
	"context"
	"errors"
	"math"
	"reflect"
	"runtime"
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
// inner(s) through a CALL of a function that never separately crosses the
// threshold, so once outer runs natively its own CALL always takes ExitCall
// through a zero natives entry.
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

// structs loops n times at module level. Each pass builds a fresh struct.new
// whose held field is a shared string constant, storing the struct over the
// previous one: LOCAL_SET releases the prior struct, cascading a release of
// its own held field. arm64 lowers no struct.new, so every native entry
// bridges; struct.new's own Adopts is 0, so a resumed bridge must retain a
// fresh reference for held regardless of its Owned bit, matching
// structNew()'s own no-release field transfer. An inner 4-iteration loop
// after each struct.new gives native.go's amortize check real back edges,
// so the site stays native for the whole run instead of retiring after
// resume unamortized bridges in a row (see arrays below for the same
// pattern; a held field built by STRING_CONCAT instead, tried first, is not
// bridgeable — see bridgeable — and its own decline dominates every pass
// before struct.new is ever reached). The loop-carried local is declared as
// record itself, not TypeAny: a TypeAny-declared local seeded by REF_NULL
// joins two structurally different facts at the loop header (unrefined null
// vs. a record-typed struct), which transform.Translate declines to merge,
// unrelated to bridging. Seeding with struct.new_default instead keeps both
// the preheader and back-edge facts record-typed, so the header's join and
// translation succeed. The final value is the surviving struct's held
// field, retained by struct.get.
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

// trap loops warm+1 times at module level over the same native loop header
// array.new_default resumes through, calling array.new_default(1) every
// iteration but the last, whose length is negative (0-1): straight-line
// code outside any loop or call never enters native code at all, so the
// trapping call must share the warmup's own header to ever reach native.
// An inner 4-iteration loop after each bridge gives native.go's amortize
// check real back edges to see, so the site never retires before the
// trapping call reaches it (resume would otherwise retire this
// bridge-per-iteration site well before warm iterations, same as arrays
// below, and the trap would then run threaded — not through a native
// decline at all). arm64's bridge attempt runs arrayNewDefault()'s own
// ErrSegmentationFault check Go-side and must decline rather than resume
// there, matching threaded.
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

// dynamicConcatProgram exercises native.call's dynamic-CALL entry (release
// true): the callee reference is seeded into local 1 once, then loaded with
// LOCAL_GET (which retains) at each call site instead of an immediately
// preceding CONST_GET, so the threader's CONST_GET;CALL fusion never applies
// and every entry into concatFunction's native code goes through the
// non-fused path the fused-only concatProgram never exercises. Warms with
// two throwaway strings warm times, then calls once more and leaves the
// result on the stack.
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
// deoptimizing an ExitCall there must reach the identical frame count and
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

// wideI64Function returns n << 50. A literal outside the inline-boxable
// range is not itself translatable (transform.Translate rejects it), so the
// width has to arise from computation: for n=1 the shift's result exceeds
// the inline range, and a wide i64 result deopts at its own RETURN, which
// this proves alongside the value.
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

// divFailProgram warms divFunction with a nonzero divisor warm times, then
// calls it with a zero divisor fails times in a loop whose body is one Try
// region: every one of those native entries deoptimizes, so the address
// retires once its deopt count reaches native.go's refute threshold, and
// its Baseline tier is then marked permanently failed — it never
// recompiles and every later call in the loop runs threaded.
// Locals are [0]=warm counter, [1]=fail counter, so the Try region's entry
// depth (params + locals + live operands, per instr.Handler) is 2.
func divFailProgram(t *testing.T, warm, fails int) *program.Program {
	t.Helper()
	fn := divFunction(t)
	b := instr.NewBuilder()
	warmLoop, warmDone := b.Label(), b.Label()
	b.Bind(warmLoop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(warm)).Emit(instr.I32_GE_S).BrIf(warmDone)
	b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(warmLoop)
	b.Bind(warmDone)

	failLoop, failDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
	b.Bind(failLoop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(fails)).Emit(instr.I32_GE_S).BrIf(failDone)
	b.Bind(start).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	b.Bind(end)
	b.Bind(catch).Emit(instr.ERROR_CODE).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(failLoop)
	b.Bind(failDone).Emit(instr.LOCAL_GET, 1)
	b.Try(start, end, catch, 2)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
		program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
}

// nestedConcatOuterFunction calls concatFunction(a, b) through a constant
// callee, so a hot outer's native call enters concatFunction's native code
// directly, two native activations deep, before concatFunction's own
// STRING_CONCAT (arm64 lowers no string opcode) deopts them both; both then
// run to a normal RETURN threaded, unlike an error unwind.
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

// nestedConcatProgram warms only its caller (constant 1) directly, so every
// call to concatFunction (constant 0) goes through it: the early ones, while
// concatFunction is still uncompiled, deoptimize outer alone through
// ExitCall and replay threaded (which still counts toward concatFunction's
// own threshold); once concatFunction compiles, outer's later native calls
// enter it directly (a borrowed constant callee) before its own
// STRING_CONCAT deopts both activations. The final call leaves its result
// on the stack.
func nestedConcatProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	inner := concatFunction(t)
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
			return bridges > 0
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
			return bridges > 0
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
			return bridges > 0
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
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

	t.Run("recursion past a small frame limit deoptimizes an ExitCall, matching threaded", func(t *testing.T) {
		native(t)
		prog := fibOverflowProgram(t, 1000, 30)
		wantErr := runProgramErr(t, prog, interp.WithFrame(10))
		require.Error(t, wantErr)

		var gotErr error
		var exits float64
		require.Eventually(t, func() bool {
			profiler := prof.New()
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(10), interp.WithProfiler(profiler))
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
		wantVM := interp.New(prog, interp.WithFrame(10))
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
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(10), interp.WithProfiler(profiler))
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

		wantVM := interp.New(prog, interp.WithFrame(10))
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
			vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(10), interp.WithProfiler(profiler))
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

	t.Run("a native call to an uncompiled function deoptimizes an ExitCall, matching threaded", func(t *testing.T) {
		native(t)
		prog := outerInnerProgram(t, 1000)
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
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

	t.Run("a borrowed callee's RefCount survives an ExitCall replay, matching threaded", func(t *testing.T) {
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

	t.Run("a function that deoptimizes every call retires once and never re-enters native code", func(t *testing.T) {
		native(t)
		// Warmup is long enough for Baseline and Optimized publication; each
		// retired tier is permanently blocked, bounding deopts at 2*refute.
		const fails = 200
		const refute = 8 // interp/native.go's unexported refute constant.
		prog := divFailProgram(t, 200_000, fails)
		want := runProgram(t, prog)

		var deopts float64
		var runErr, popErr error
		var result types.Value
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
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.LessOrEqual(t, deopts, float64(2*refute))
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

	t.Run("a wide i64 argument deopting at a borrowed call keeps the callee alive", func(t *testing.T) {
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
			deopts, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return deopts > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, err)
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
		// A module with a handler of its own can never OSR-compile (a
		// function's own Handlers alone gate compile.Compile, module
		// included), so the OSR-eligible loop lives in a called function
		// with no handlers of its own; the module's handler wraps the call
		// and catches the real trap once it unwinds out of that frame.
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
		const refute = 8
		const n = 600
		prog := bridge(t, n)
		want := runProgram(t, prog)

		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		ctx := context.Background()

		// One run bridges at most once: a materialized frame finishes threaded.
		var got types.Value
		var runErr, popErr error
		var exits float64
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits >= float64(refute)
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		compiles, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		// One submit, one compile; every native entry bridges immediately,
		// so refute retires the site at exactly the deopt threshold.
		require.Equal(t, float64(1), compiles)
		require.Equal(t, float64(refute), exits)

		// The retired site never polls or re-enters again: a further run
		// takes the same back edges threaded, and both metrics hold.
		require.NoError(t, vm.Run(ctx))
		got2, err2 := vm.Pop()
		require.NoError(t, err2)
		require.Equal(t, want, got2)
		vm.Reset()
		vm.Flush()
		compilesAgain, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
		exitsAgain, _ := profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
		require.Equal(t, float64(1), compilesAgain)
		require.Equal(t, float64(refute), exitsAgain)
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
// adds string.len of a constant: an operation native code does not lower, so
// f's native code deoptimizes there while the loop's promoted locals live in
// the caller's native frame.
func deopt(t *testing.T, n, k int) *program.Program {
	t.Helper()
	fb := instr.NewBuilder()
	slow := fb.Label()
	fb.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(k)).Emit(instr.I32_EQ).BrIf(slow)
	fb.Emit(instr.LOCAL_GET, 0).Emit(instr.RETURN)
	fb.Bind(slow).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 1).Emit(instr.STRING_LEN).Emit(instr.I32_ADD).Emit(instr.RETURN)
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
// and entered. It has no handlers of its own, so its loop header stays
// OSR-eligible even though the module that calls it does (a function's own
// Handlers alone gate compile.Compile, matching translate.go's "declines...
// a protected region" contract).
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

// bridge loops n times at module level over STRING_LEN, which native code
// does not lower: every native entry of its header bridges.
func bridge(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	header, done := b.Label(), b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
	b.Bind(header)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(n)).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.CONST_GET, 0).Emit(instr.STRING_LEN).Emit(instr.DROP)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(header)
	b.Bind(done).Emit(instr.LOCAL_GET, 0)
	code, err := b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(types.String("x")))
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

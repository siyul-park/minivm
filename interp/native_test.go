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

// lentProgram calls a(i) warm times through global 0 = inc, then warm times
// through global 0 = dec, the same polymorphic feedback and eventual refute
// applyProgram exercises, but one native-to-native call deeper: a's own call
// into apply lends its global-backed argument to apply's borrowed param 1,
// so a refute's deopt rebuilds two native activations, not one. Constants
// are [apply, a, inc, dec]. Module locals [0] the counter and [1] the
// running sum, left on the stack.
func lentProgram(t *testing.T, warm int) *program.Program {
	t.Helper()
	// a calls apply(n, global 0) through its own native-to-native CALL:
	// global 0 is an owned (retained, then released) argument at apply's own
	// borrowed param 1.
	a := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	a.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.GLOBAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN))
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
		program.WithConstants(applyFunction(), a.MustBuild(), incFunction(), decFunction()))
}

// applyGlobalProgram reads global 1 (the caller's selector: 0 for inc, 1 for
// dec) once, stores the matching constant-pool address into global 0, then
// calls apply(i, global 0) calls times through the same dynamic CALL site,
// the callee read fresh from global 0 on every call. Constants are [apply,
// inc, dec]. Module locals [0] the counter and [1] the running sum, left on
// the stack. The constant-pool address survives Reset, unlike a heap Alloc,
// so repeated rounds observe the same callee address for the same selector.
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

// node is the struct type structs (below) allocates each iteration: an i32
// counter and an any-typed held reference.
func node() *types.StructType {
	return types.NewStructType(
		types.NewStructField(types.TypeI32, types.FieldWithName("n")),
		types.NewStructField(types.TypeAny, types.FieldWithName("held")),
	)
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

// fnvProgram calls fnvFunction(n) once, leaving its wide result on the
// stack; constants follow the function in the pool.
func fnvProgram(t *testing.T, n int, op instr.Opcode, operand uint64, constants ...types.Value) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(n)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
	code, err := b.Assemble()
	require.NoError(t, err)
	fnBuilder := instr.NewBuilder()
	loop, done := fnBuilder.Label(), fnBuilder.Label()
	fnBuilder.Emit(op, operand).Emit(instr.LOCAL_SET, 1)
	fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)
	fnBuilder.Bind(loop)
	fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
	fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
	fnBuilder.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL).Emit(instr.LOCAL_SET, 1)
	fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	fnBuilder.Br(loop)
	fnBuilder.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
	fnCode, err := fnBuilder.Assemble()
	require.NoError(t, err)
	fn := &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}},
		Locals: []types.Type{types.TypeI64, types.TypeI32},
		Code:   instr.Marshal(fnCode),
	}
	return program.New(code, program.WithConstants(append([]types.Value{fn}, constants...)...))
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

// refuse emits a prologue that reads an element of a null on a path no run
// takes: the translator declines an array read of unknown element kind, so b's
// function never compiles.
func refuse(b *types.FunctionBuilder) {
	body := b.Label()
	b.Emit(instr.New(instr.I32_CONST, 1)).BrIf(body)
	b.Emit(instr.New(instr.REF_NULL), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.DROP))
	b.Bind(body)
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
	pairBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
	pairBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 100), instr.New(instr.I32_ADD), instr.New(instr.RETURN))
	pair := pairBuilder.MustBuild()
	return program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString),
		program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s"), pair))
}

func TestWithThreshold(t *testing.T) {
	t.Run("enters a hot recursive function's native code", func(t *testing.T) {
		native(t)
		prog := fibCallsProgram(t, 20)
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 2*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("keeps a bridged loop-free module native", func(t *testing.T) {
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

	t.Run("retires a loop that only bridges and releases", func(t *testing.T) {
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

	t.Run("refills a loop's budget at a safepoint", func(t *testing.T) {
		native(t)
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(200000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("releases a dying ref parameter on exit", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.I32_CONST, 42).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeI32}},
			Code: instr.Marshal(fnCode),
		}
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
			vm.Flush()
			released, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			return released > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.NoError(t, refErr)
		require.Equal(t, 1, count)
	})

	t.Run("treats NaN as unordered in float comparisons", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches a native division by zero in a guest handler", func(t *testing.T) {
		native(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
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
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports an uncaught division by zero's stack trace", func(t *testing.T) {
		native(t)
		fn := divFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		var gotErr error
		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			vm.Reset()
			return gotErr != nil
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges STRING_CONCAT", func(t *testing.T) {
		native(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(fn, types.String("bridge-"), types.String("concat")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("bridges STRUCT_NEW", func(t *testing.T) {
		native(t)
		record := node()
		b := instr.NewBuilder()
		loop, done, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.STRUCT_NEW_DEFAULT, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
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
		prog := program.New(code, program.WithLocals(types.TypeI32, record, types.TypeI32), program.WithTypes(record),
			program.WithConstants(types.String("held")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
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

	t.Run("bridges ARRAY_NEW_DEFAULT", func(t *testing.T) {
		native(t)
		const length = 4
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, length).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.ARRAY_LEN)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var bridges, deopts float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
		require.Equal(t, float64(0), deopts)
	})

	t.Run("declines a trapping ARRAY_NEW_DEFAULT", func(t *testing.T) {
		native(t)
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done, bad, length, inner, innerDone := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)+1).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_EQ).BrIf(bad)
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
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithTypes(elem))
		wantErr := runProgramErr(t, prog)
		require.Error(t, wantErr)

		// A declined bridge's own exit is still kind=bridge (the metric
		// names the exit, not its outcome), so only error/stack-trace parity
		// with threaded execution proves native code correctly handed the
		// trapping instruction back instead of silently accepting it.
		var gotErr error
		var bridges float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			bridges, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return bridges > 1
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		require.True(t, errorsEqual(gotErr, wantErr))
	})

	t.Run("bridges string ops", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
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

	t.Run("declines a string op that exhausts the heap", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithHeapLimit(limit), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			constCount, constErr = vm.RefCount(ab.Ref())
			vm.Reset()
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

	t.Run("bridges unlowered operators in a loop", func(t *testing.T) {
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

	t.Run("selects between two refs", func(t *testing.T) {
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

	t.Run("deoptimizes an unlowered operator that traps", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			gotX, xErr = vm.RefCount(x.Ref())
			gotArray, arrayErr = vm.RefCount(array.Ref())
			vm.Reset()
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

	t.Run("keeps a callee native while its native calls amortize bridges", func(t *testing.T) {
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

	t.Run("releases a ref slot's old value on store", func(t *testing.T) {
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

	t.Run("bridges a dynamically entered call", func(t *testing.T) {
		native(t)
		fn := concatFunction(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
			program.WithConstants(fn, types.String("dyn-"), types.String("call")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr error
		var value string
		var count int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "bridge"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("overflows a small frame limit inside a served call", func(t *testing.T) {
		native(t)
		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib))
		wantErr := runProgramErr(t, prog, interp.WithFrame(8))
		require.Error(t, wantErr)

		var gotErr error
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			gotErr = vm.Run(context.Background())
			if gotErr == nil {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.Error(t, gotErr)
		// The innermost frame's IP is ignored: a deoptimized frame runs exact
		// code, so a trap at a fused CONST_GET;CALL call site (fib's own
		// recursive call) resumes at CALL's own IP rather than the fused
		// unit's, unlike the threaded baseline which never separates them.
		var got, want *interp.RuntimeError
		require.ErrorAs(t, gotErr, &got)
		require.ErrorAs(t, wantErr, &want)
		require.ErrorIs(t, got.Err, interp.ErrFrameOverflow)
		require.ErrorIs(t, want.Err, interp.ErrFrameOverflow)
		require.Len(t, got.Frames, len(want.Frames))
		for i := range got.Frames {
			require.Equal(t, want.Frames[i].Func, got.Frames[i].Func)
			if i > 0 {
				require.Equal(t, want.Frames[i].IP, got.Frames[i].IP)
			}
		}
	})

	t.Run("deopts self-recursive fib at a frame limit", func(t *testing.T) {
		native(t)
		// The handler unwinds every native and interpreter frame the deopt
		// left above it and releases the operand stack, so RefCount after a
		// successful Run reflects only durable state, comparable across
		// threaded and native runs. (An uncaught error leaves abandoned
		// frames on both paths, by design, and is not comparable this way.)
		fib := fibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
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

	t.Run("deopts a callee with register-passed ref and i32 parameters", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			got, popErr = vm.Pop()
			c, _ := vm.Const(1)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
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

	t.Run("resumes a caller after a call to a function without native code", func(t *testing.T) {
		native(t)
		s := types.String("s")
		deep := []interp.Option{interp.WithFrame(1024), interp.WithStack(1 << 14)}
		sumrecBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		zero := sumrecBuilder.Label()
		sumrecBuilder.Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQZ)).BrIf(zero)
		sumrecBuilder.Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, uint64(1)), instr.New(instr.CALL), instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		)
		sumrecBuilder.Bind(zero).Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN))
		sumrec := sumrecBuilder.MustBuild()
		for _, c := range []struct {
			name string
			prog *program.Program
			opts []interp.Option
		}{
			{"a callee that never compiles", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, false), s), nil},
			{"a callee that never compiles, passed an owned argument", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(0, true), s), nil},
			{"a callee that never compiles calling native code that calls it again", loopProgram(t, 8, 64, 64, false, guardFunction(1000, false), loopFunction(4, false), s, relayFunction(0, false), relayFunction(3, true)), nil},
			{"recursion past the native activation limit", loopProgram(t, 8, 10, 600, false, guardFunction(1000, false), sumrec, s), deep},
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

	t.Run("resumes a dynamic call through call exits", func(t *testing.T) {
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

	t.Run("retires a caller whose served calls outweigh its work", func(t *testing.T) {
		native(t)
		const runs = 16
		// held(x) = x never compiles, so a native caller always reaches it
		// through ExitCall.
		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		refuse(held)
		held.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		served := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		served.Emit(instr.New(instr.I32_CONST, 0))
		for range 16 {
			served.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 1), instr.New(instr.CALL), instr.New(instr.I32_ADD))
		}
		served.Emit(instr.New(instr.RETURN))

		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(served.MustBuild(), held.MustBuild()))
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

	t.Run("deopts a dynamic call whose callee takes other parameters", func(t *testing.T) {
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

	t.Run("deopts a dynamic call only once it runs", func(t *testing.T) {
		native(t)
		for _, c := range []struct {
			name  string
			final int
		}{
			{"it stays cold", -1},
			{"it runs", 3},
		} {
			b := instr.NewBuilder()
			loop, done := b.Label(), b.Label()
			b.Emit(instr.CONST_GET, 2).Emit(instr.LOCAL_SET, 2)
			b.Bind(loop)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(64)).Emit(instr.I32_GE_S).BrIf(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(math.MaxUint32)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
			b.Br(loop)
			b.Bind(done)
			b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 100).Emit(instr.I32_CONST, uint64(c.final)).Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
			b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 2)
			code, err := b.Assemble()
			require.NoError(t, err)
			coldBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString, types.TypeI32, types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
			coldLoop, coldDone, hit, next := coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label(), coldBuilder.Label()
			coldBuilder.Bind(coldLoop).Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).BrIf(coldDone)
			coldBuilder.Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_EQ)).BrIf(hit)
			coldBuilder.Emit(instr.New(instr.LOCAL_GET, 5)).Br(next)
			coldBuilder.Bind(hit).Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 3), instr.New(instr.CALL))
			coldBuilder.Bind(next).Emit(
				instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
				instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
			).Br(coldLoop)
			coldBuilder.Bind(coldDone).Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.RETURN))
			cold := coldBuilder.MustBuild()
			prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString),
				program.WithConstants(cold, bumpFunction(1), types.String("s")))
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

	t.Run("reports an uncaught trap in a resumed callee", func(t *testing.T) {
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

	t.Run("serves a trapping, throwing, or coroutine callee by call exit", func(t *testing.T) {
		native(t)
		s := types.String("s")
		// A coroutine's CALL returns a handle, not the results the native
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
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"coroutine", loopProgram(t, 64, 8, 8, false, co, drive.MustBuild(), s)},
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

	t.Run("deopts two native activations deep", func(t *testing.T) {
		native(t)
		ib := instr.NewBuilder()
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_EQ).Emit(instr.DROP)
		ib.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.STRING_CONCAT).Emit(instr.RETURN)
		icode, err := ib.Assemble()
		require.NoError(t, err)
		inner := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
			Code: instr.Marshal(icode),
		}
		outerBuilder := instr.NewBuilder()
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		require.NoError(t, err)
		outer := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeString, types.TypeString}, Returns: []types.Type{types.TypeString}},
			Code: instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 3).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32),
			program.WithConstants(inner, outer, types.String("deep-"), types.String("concat")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("keeps a borrowed callee's RefCount across a served call", func(t *testing.T) {
		native(t)
		// inner never compiles, so native outer always serves its call.
		held := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}})
		refuse(held)
		inner := held.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).MustBuild()
		outer := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeString}, Returns: []types.Type{types.TypeString}}).
			Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL), instr.New(instr.RETURN)).MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(inner, outer, types.String("chain")))
		wantValue, wantCount := runProgramString(t, prog)

		var runErr, popErr, refErr error
		var value string
		var count, innerCount int
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("passes a borrowed parameter through native recursion", func(t *testing.T) {
		native(t)
		// self (param 1) is borrowed at both of indirectFibFunction's own
		// dynamic CALLs: a native caller never retains it, so a deopt at the
		// frame limit exercises the served call's own retain of it.
		fib := indirectFibFunction()
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(30)).Emit(instr.CONST_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fib), program.WithHandlers(b.Handlers()...))
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			c, _ := vm.Const(0)
			gotRC, rcErr = vm.RefCount(c.Ref())
			vm.Reset()
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

	t.Run("transfers an owned argument lent to a borrowed parameter", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("deopts a deep recursion at its frame limit", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithFrame(8), interp.WithProfiler(profiler))
		defer vm.Close()
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
			safepoints, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			releases, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "release"})
			calls, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "call"})
			return safepoints > 0 && releases > 0 && calls > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("returns a wide i64 from native code", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:  &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Code: instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(fn))
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

	t.Run("returns a wide i64 from native recursion", func(t *testing.T) {
		native(t)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}})
		small := fnBuilder.Label()
		fnBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_LT_S)).BrIf(small)
		fnBuilder.Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 1), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I64_CONST, 2), instr.New(instr.I64_SUB), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I64_ADD), instr.New(instr.RETURN),
		)
		fnBuilder.Bind(small).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I64_CONST, 1), instr.New(instr.I64_CONST, 50), instr.New(instr.I64_SHL), instr.New(instr.I64_ADD),
			instr.New(instr.RETURN),
		)
		fn := fnBuilder.MustBuild()
		b := instr.NewBuilder()
		b.Emit(instr.I64_CONST, uint64(20)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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

	t.Run("passes a wide i64 argument into a native function", func(t *testing.T) {
		native(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		for range 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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

	t.Run("passes a wide i64 register argument from a native caller", func(t *testing.T) {
		native(t)
		inner := wideArgFunction()
		outerBuilder := instr.NewBuilder()
		loop, done := outerBuilder.Label(), outerBuilder.Label()
		outerBuilder.Bind(loop)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 4).Emit(instr.I32_GE_S).BrIf(done)
		outerBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		outerBuilder.Br(loop)
		outerBuilder.Bind(done)
		outerBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		outerBuilder.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
		outerCode, err := outerBuilder.Assemble()
		if err != nil {
			panic(err)
		}
		outer := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI64}, Returns: []types.Type{types.TypeI64}},
			Locals: []types.Type{types.TypeI32},
			Code:   instr.Marshal(outerCode),
		}
		b := instr.NewBuilder()
		for index := range 2 * 2000 {
			b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, uint64(index/2000)).Emit(instr.CALL).Emit(instr.DROP)
		}
		b.Emit(instr.I64_CONST, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(inner, outer))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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

	t.Run("returns a wide i64 from an OSR unit", func(t *testing.T) {
		native(t)
		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_GET, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
		fnBuilder.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done)
		fnBuilder.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S)
		fnBuilder.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL).Emit(instr.I64_ADD)
		fnBuilder.Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI64}},
			Locals: []types.Type{types.TypeI32, types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		b.Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("compiles a loop seeded with a wide i64", func(t *testing.T) {
		native(t)
		seed := int64(-3750763034362895579)
		for _, c := range []struct {
			name string
			prog *program.Program
		}{
			{"const", fnvProgram(t, 100_000, instr.I64_CONST, uint64(seed))},
			{"pool cell", fnvProgram(t, 100_000, instr.CONST_GET, 1, types.I64(seed))},
		} {
			want := runProgram(t, c.prog)

			var got types.Value
			var runErr, popErr error
			var deopts, compiles float64
			profiler := prof.New()
			vm := interp.New(c.prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
			defer vm.Close()
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
				deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
				compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
				return compiles > 0
			}, 5*time.Second, time.Millisecond, c.name)
			require.NoError(t, runErr, c.name)
			require.NoError(t, popErr, c.name)
			require.Zero(t, deopts, c.name)
			require.Equal(t, want, got, c.name)
		}
	})

	t.Run("keeps a wide i64 constant's RefCount across a shape-guard deopt", func(t *testing.T) {
		native(t)
		seed := int64(-3750763034362895579)
		fnBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI64}})
		fnBuilder.Emit(instr.New(instr.I64_CONST, uint64(seed)))
		fnBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.DROP))
		fnBuilder.Emit(instr.New(instr.RETURN))
		fn := fnBuilder.MustBuild()
		elem := types.NewArrayType(types.TypeI32)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 4).Emit(instr.ARRAY_NEW_DEFAULT, 0)
		b.Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(elem),
			program.WithConstants(fn), program.WithTypes(elem))

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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("promotes a narrow i64 accumulator in an OSR loop", func(t *testing.T) {
		native(t)
		fn := wideArgFunction()
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 5).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I64_XOR).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64, types.TypeI32), program.WithConstants(fn))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			deopts, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			compiles, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "ok"})
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Zero(t, deopts)
		require.Equal(t, want, got)
	})

	t.Run("promotes an i64 accumulator in an OSR FNV loop", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I64_CONST, uint64(0x1234567)).Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(100_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_TO_I64_U).Emit(instr.I64_XOR)
		b.Emit(instr.I64_CONST, 1099511628211).Emit(instr.I64_MUL)
		b.Emit(instr.LOCAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI64, types.TypeI32))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, compiles float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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

	t.Run("boxes a wide store in a loop each iteration", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I64_CONST, 1).Emit(instr.I64_CONST, 50).Emit(instr.I64_SHL)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_TO_I64_S).Emit(instr.I64_ADD)
		b.Emit(instr.GLOBAL_SET, 0)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.TypeI64))
		want := runProgram(t, prog)

		var got types.Value
		var runErr, popErr error
		var deopts, entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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

	t.Run("tiers a hot function up to Optimized", func(t *testing.T) {
		native(t)
		prog := fibFlatCallsProgram(t, 2000)
		want := runProgram(t, prog)

		var got types.Value
		var compiles float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			return compiles > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("keeps a caller entering past a callee's caught traps", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done, start, end, catch, next := b.Label(), b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
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
		rareBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).Locals(types.TypeI32, types.TypeI32)
		rareLoop, rareDone := rareBuilder.Label(), rareBuilder.Label()
		rareBuilder.Bind(rareLoop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(rareDone)
		rareBuilder.Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.I32_NE),
			instr.New(instr.CONST_GET, uint64(1)), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
		).Br(rareLoop)
		rareBuilder.Bind(rareDone).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		rare := rareBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32),
			program.WithConstants(rare, divFunction(t)), program.WithHandlers(b.Handlers()...))

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

	t.Run("keeps a protected hot loop native past caught traps", func(t *testing.T) {
		native(t)
		const rounds, batches = 32, 64
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(rounds)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.I32_CONST, uint64(100)).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		protectedBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}}).
			Locals(types.TypeI32, types.TypeI32, types.TypeString)
		protectedLoop, protectedDone, start, end, catch, next := protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label(), protectedBuilder.Label()
		protectedBuilder.Emit(instr.New(instr.CONST_GET, 1), instr.New(instr.LOCAL_SET, 3))
		protectedBuilder.Bind(protectedLoop).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).BrIf(protectedDone)
		protectedBuilder.Bind(start).Emit(
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 6400),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 63), instr.New(instr.I32_AND),
			instr.New(instr.I32_DIV_S), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(end).Br(next)
		protectedBuilder.Bind(catch).Emit(instr.New(instr.DROP), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1000), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2))
		protectedBuilder.Bind(next).Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1)).Br(protectedLoop)
		protectedBuilder.Bind(protectedDone).Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN))
		protectedBuilder.Try(start, end, catch, 4)
		protected := protectedBuilder.MustBuild()
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(protected, types.String("owned")))

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

	t.Run("catches a native callee's throw in its native caller", func(t *testing.T) {
		native(t)
		const calls, batches = 32, 64
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
		prog := program.New(code, program.WithConstants(guard.MustBuild(), thrower.MustBuild(), types.String("boom!")))

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

	t.Run("enters a protected module loop and reports a trap outside it", func(t *testing.T) {
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
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32), program.WithHandlers(b.Handlers()...))
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

	t.Run("escapes guest handlers on a cancelled context", func(t *testing.T) {
		native(t)
		// The final call repeats natively until the timer cancels it.
		sum := sumFunction(t)
		b := instr.NewBuilder()
		loop, done, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.I32_CONST, 1).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done)
		b.Bind(start).Emit(instr.I32_CONST, uint64(2_000_000_000)).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Br(start)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 1)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum), program.WithHandlers(b.Handlers()...))

		var runErr error
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			ctx, cancel := context.WithCancel(context.Background())
			go func() {
				time.Sleep(time.Second)
				cancel()
			}()
			runErr = vm.Run(ctx)
			if !errors.Is(runErr, context.Canceled) {
				return false
			}
			vm.Reset()
			vm.Flush()
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "safepoint"})
			return exits > 0
		}, 20*time.Second, time.Second)
		require.Error(t, runErr)
		require.ErrorIs(t, runErr, context.Canceled)
	})

	t.Run("runs a function allocated after construction interpreted", func(t *testing.T) {
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

	t.Run("keeps a callee alive across a boxed wide i64 argument", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			if err = vm.Run(context.Background()); err != nil {
				return true
			}
			result, _ = vm.Pop()
			c, _ := vm.Const(0)
			rc, _ = vm.RefCount(c.Ref())
			vm.Reset()
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

	t.Run("sums a typed i32 array in a calling function", func(t *testing.T) {
		native(t)
		identBuilder := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32}, Returns: []types.Type{types.TypeI32}})
		identBuilder.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN))
		ident := identBuilder.MustBuild()
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
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(loopDone)
		b.Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(loopDone).Emit(instr.CONST_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sumFn, ident, arr))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reads and writes a ref array element", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(refArrayProgram(t, 20000), interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			runErr = vm.Run(context.Background())
			if runErr != nil {
				return true
			}
			value, count, popErr = popString(vm)
			if popErr != nil {
				return true
			}
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, wantValue, value)
		require.Equal(t, wantCount, count)
	})

	t.Run("walks a struct tree", func(t *testing.T) {
		native(t)
		// Each interpreter gets its own freshly built program: the module's
		// own linking code (struct.set) mutates the leaf/mid/root struct
		// constants in place, another instance of the refArrayProgram
		// cross-run-reuse hazard above.
		want := runProgram(t, structTreeProgram(t, 20000))

		var runErr, popErr error
		var result types.Value
		var entries float64
		profiler := prof.New()
		vm := interp.New(structTreeProgram(t, 20000), interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("catches an out-of-bounds array.get in a guest handler", func(t *testing.T) {
		native(t)
		at := types.NewFunctionBuilder(&types.FunctionType{
			Params: []types.Type{types.NewArrayType(types.TypeI32), types.TypeI32}, Returns: []types.Type{types.TypeI32},
		})
		at.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_GET), instr.New(instr.RETURN))
		atFn := at.MustBuild()
		arr := types.NewArray(types.NewArrayType(types.TypeI32), types.BoxI32(1), types.BoxI32(2), types.BoxI32(3))

		b := instr.NewBuilder()
		warmLoop, warmDone, start, end, catch := b.Label(), b.Label(), b.Label(), b.Label(), b.Label()
		b.Bind(warmLoop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(warmDone)
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
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(atFn, arr), program.WithHandlers(b.Handlers()...))
		want := runProgram(t, prog)

		var runErr, popErr error
		var result types.Value
		var exits float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			exits, _ = profiler.Metric("vm_jit_exits_total", prof.Label{Key: "kind", Value: "deopt"})
			return exits > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("deopts a host array that fails its shape guard", func(t *testing.T) {
		native(t)
		ln := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.NewArrayType(types.TypeI32)}, Returns: []types.Type{types.TypeI32}})
		ln.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_LEN), instr.New(instr.RETURN))
		lnFn := ln.MustBuild()

		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(20000)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.GLOBAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithGlobals(types.NewArrayType(types.TypeI32)), program.WithConstants(lnFn))

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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("recompiles a null array's refuted container guard with its site bridged", func(t *testing.T) {
		native(t)
		const rounds, batches = 64, 8
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
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, array, array),
			program.WithConstants(at.MustBuild(), arr), program.WithHandlers(b.Handlers()...))
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

	t.Run("keeps a loop with a never-true null array read native", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("guards an array replaced inside an Optimized loop afresh", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, result)
	})

	t.Run("reports no vm_jit metrics by default", func(t *testing.T) {
		profiler := prof.New()
		vm := interp.New(fibCallsProgram(t, 3), interp.WithProfiler(profiler))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		vm.Flush()

		for _, m := range profiler.Metrics() {
			require.NotContains(t, m.Name, "vm_jit_")
		}
	})

	t.Run("enters a module loop by OSR", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("compiles a module loop straight to Optimized", func(t *testing.T) {
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

	t.Run("restores promoted locals when a callee deopts under a loop", func(t *testing.T) {
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

	t.Run("tiers up a callee reached only from native code", func(t *testing.T) {
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

	t.Run("tiers up a native-only callee across short Runs", func(t *testing.T) {
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

	t.Run("enters a function's loop header mid-call by OSR", func(t *testing.T) {
		native(t)
		// warmCalls*warmEach back edges warm sum's own loop-header OSR site
		// across calls too few (warmCalls) to ever reach its entry-0 CALL
		// threshold on their own, so entry-0 never compiles: the final call
		// starts threaded and can only enter native code through OSR, mid-call,
		// once the site resolves.
		const threshold, warmCalls, warmEach, n = 1000, 20, 300, 200_000
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
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithConstants(sum))
		want := runProgram(t, prog)

		var got types.Value
		var entries float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(threshold), interp.WithProfiler(profiler))
		defer vm.Close()
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
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("catches a deopt inside an OSR loop in a guest handler", func(t *testing.T) {
		native(t)
		// The OSR-eligible loop lives in a called function with no handlers
		// of its own; the module's handler wraps the call and catches the
		// real trap once it unwinds out of that frame.
		fnBuilder := instr.NewBuilder()
		loop, done := fnBuilder.Label(), fnBuilder.Label()
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // i = 0
		fnBuilder.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // sum = 0
		fnBuilder.Bind(loop)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(2_000_000)).Emit(instr.I32_GE_S).BrIf(done)
		fnBuilder.Emit(instr.LOCAL_GET, 1)
		fnBuilder.Emit(instr.I32_CONST, 100)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(1_500_000)).Emit(instr.I32_SUB) // i-1_500_000
		fnBuilder.Emit(instr.I32_DIV_S)
		fnBuilder.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
		fnBuilder.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		fnBuilder.Br(loop)
		fnBuilder.Bind(done).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
		fnCode, err := fnBuilder.Assemble()
		require.NoError(t, err)
		fn := &types.Function{
			Typ:    &types.FunctionType{Returns: []types.Type{types.TypeI32}},
			Locals: []types.Type{types.TypeI32, types.TypeI32},
			Code:   instr.Marshal(fnCode),
		}
		b := instr.NewBuilder()
		start, end, catch := b.Label(), b.Label(), b.Label()
		b.Bind(start).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.DROP)
		b.Bind(end)
		b.Bind(catch).Emit(instr.ERROR_CODE)
		b.Try(start, end, catch, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithConstants(fn), program.WithHandlers(b.Handlers()...))
		threaded := interp.New(prog)
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		wantCode, err := threaded.PopBoxed()
		require.NoError(t, err)
		wantConst, err := threaded.Const(0)
		require.NoError(t, err)
		wantValue, err := threaded.RefCount(wantConst.Ref())
		require.NoError(t, err)

		var gotCode types.Boxed
		var gotValue int
		var exits float64
		var runErr, popErr, constErr, refErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("unwraps an OSR loop header that cannot compile", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		header, done := b.Label(), b.Label()
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0) // counter = 0
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1) // flag = 0
		b.Bind(header)
		b.Emit(instr.LOCAL_GET, 1).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)                // counter++
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(200_000)).Emit(instr.I32_GE_S).Emit(instr.LOCAL_SET, 1) // flag = counter >= 200_000
		b.Br(header)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32))
		want := runProgram(t, prog)

		var got types.Value
		var unsupported float64
		var runErr, popErr error
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
			return unsupported > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		// A site submits its OSR unit once: the failed compile is never
		// resubmitted, however many further back edges the loop takes.
		require.Equal(t, float64(1), unsupported)

		// The failed site restores its threaded handler and stops polling:
		// a second Run on the same interpreter takes the same many back
		// edges again, and the metric does not move.
		require.NoError(t, vm.Run(context.Background()))
		got2, popErr2 := vm.Pop()
		require.NoError(t, popErr2)
		require.Equal(t, want, got2)
		vm.Flush()
		again, _ := profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "optimized"}, prof.Label{Key: "outcome", Value: "unsupported"})
		require.Equal(t, float64(1), again)
	})

	t.Run("retires an OSR site whose loop body always bridges", func(t *testing.T) {
		native(t)
		const n = 600
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
		prog := program.New(code, program.WithLocals(types.TypeI32), program.WithTypes(types.NewMapType(types.TypeI32, types.TypeI32)))
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

	t.Run("reuses a resolved OSR site after Reset", func(t *testing.T) {
		native(t)
		prog := iterativeFibProgram(t, 200_000)
		want := runProgram(t, prog)

		profiler := prof.New()
		pool := interp.NewPool(prog, 2, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer pool.Close()
		first, err := pool.Get(context.Background())
		require.NoError(t, err)
		second, err := pool.Get(context.Background())
		require.NoError(t, err)

		var gotFirst, gotSecond types.Value
		var entries float64
		var runErr, firstErr, secondErr error
		require.Eventually(t, func() bool {
			runErr = first.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotFirst, firstErr = first.Pop()
			if firstErr != nil {
				return true
			}
			runErr = second.Run(context.Background())
			if runErr != nil {
				return true
			}
			gotSecond, secondErr = second.Pop()
			if secondErr != nil {
				return true
			}
			first.Reset()
			second.Reset()
			first.Flush()
			second.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, firstErr)
		require.NoError(t, secondErr)
		require.Equal(t, want, gotFirst)
		require.Equal(t, want, gotSecond)
	})

	t.Run("runs an indirect self call through a parameter", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(100), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("recompiles a unit retired by a later-recorded site", func(t *testing.T) {
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

	t.Run("compiles a unit before its dynamic sites ran", func(t *testing.T) {
		native(t)
		prog := indirectFibCallsProgram(t, 20, 50)
		want := runProgram(t, prog)

		var runErr, popErr error
		var got types.Value
		var unsupported, ok float64
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
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
			unsupported, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "unsupported"})
			ok, _ = profiler.Metric("vm_jit_compiles_total", prof.Label{Key: "tier", Value: "baseline"}, prof.Label{Key: "outcome", Value: "ok"})
			return ok >= 1
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
		require.Zero(t, unsupported)
	})

	t.Run("retires a speculated callee refuted by a second function", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
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

	t.Run("recompiles a polymorphic speculated site generic", func(t *testing.T) {
		native(t)
		const n, runs = 200, 16
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
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeI32, types.TypeString, types.TypeI32), program.WithGlobals(types.TypeI32),
			program.WithConstants(relayDynamicFunction(), bumpFunction(1), bumpFunction(3), types.String("s")))

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

	t.Run("traps a closure callee's site until native code records it", func(t *testing.T) {
		native(t)
		b := instr.NewBuilder()
		loop, done := b.Label(), b.Label()
		b.Emit(instr.CONST_GET, 0).Emit(instr.CLOSURE_NEW).Emit(instr.LOCAL_SET, 1)
		b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
		b.Bind(loop)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(50)).Emit(instr.I32_GE_S).BrIf(done)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, 1).Emit(instr.CALL).Emit(instr.DROP)
		b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
		b.Br(loop)
		b.Bind(done).Emit(instr.LOCAL_GET, 0)
		code, err := b.Assemble()
		require.NoError(t, err)
		// wrap calls a closure through param 1, a dynamic CALL: native never
		// records a closure callee (call's own hook only reaches a
		// *types.Function target), so this site never speculates.
		wrap := types.NewFunctionBuilder(&types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}})
		wrap.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL), instr.New(instr.RETURN))
		prog := program.New(code, program.WithLocals(types.TypeI32, types.TypeAny),
			program.WithConstants(incFunction(), wrap.MustBuild()))
		want := runProgram(t, prog)

		vm := interp.New(prog, interp.WithThreshold(0))
		defer vm.Close()
		require.NoError(t, vm.Run(context.Background()))
		got, err := vm.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
	})

	t.Run("calls closures natively", func(t *testing.T) {
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

	t.Run("keeps a caller entering while its closure callee's Baseline is pending", func(t *testing.T) {
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

	t.Run("compiles a closure body reached only from native code", func(t *testing.T) {
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

	t.Run("rebuilds a closure frame with its upvals after a declined bridge", func(t *testing.T) {
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

	t.Run("bridges CLOSURE_NEW", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
			if runErr = vm.Run(context.Background()); runErr != nil {
				return true
			}
			if value, count, popErr = popString(vm); popErr != nil {
				return true
			}
			vm.Reset()
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

	t.Run("faults on a directly called function's missing upval", func(t *testing.T) {
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

	t.Run("zeroes each unwritten local of a native function", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, zeroValues, got)
	})

	t.Run("zeroes each unwritten module local and global under OSR", func(t *testing.T) {
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
		profiler := prof.New()
		vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
		defer vm.Close()
		require.Eventually(t, func() bool {
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
			vm.Reset()
			vm.Flush()
			entries, _ = profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
			return entries > 0
		}, 5*time.Second, time.Millisecond)
		require.NoError(t, runErr)
		require.NoError(t, popErr)
		require.Equal(t, want, got)
	})

	t.Run("computes every primitive operator bit for bit", func(t *testing.T) {
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
				profiler := prof.New()
				vm := interp.New(prog, interp.WithThreshold(0), interp.WithProfiler(profiler))
				defer vm.Close()
				require.Eventually(t, func() bool {
					if runErr = vm.Run(context.Background()); runErr != nil {
						return true
					}
					if got, popErr = vm.PopBoxed(); popErr != nil {
						return true
					}
					vm.Reset()
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

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

// tailRefFunction is sum(n, acc, held) = n == 0 ? acc : sum(n-1, acc+n, held),
// a self tail call through constant 0 that carries a ref parameter and parks
// it in a ref local first, so the call must release the old ref slots.
func tailRefFunction(t *testing.T) *types.Function {
	t.Helper()
	b := instr.NewBuilder()
	base := b.Label()
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 0).Emit(instr.I32_EQ).BrIf(base)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.LOCAL_SET, 3)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_SUB)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 0).Emit(instr.I32_ADD)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, 0).Emit(instr.RETURN_CALL)
	b.Bind(base).Emit(instr.LOCAL_GET, 1).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	return &types.Function{
		Typ:    &types.FunctionType{Params: []types.Type{types.TypeI32, types.TypeI32, types.TypeAny}, Returns: []types.Type{types.TypeI32}},
		Locals: []types.Type{types.TypeAny},
		Code:   instr.Marshal(code),
	}
}

// tailRefProgram calls tailRefFunction(n, 0, "held") through a wrapper that
// lends it its own ref parameter, and leaves the result; constant 1 is the
// string the call carries.
func tailRefProgram(t *testing.T, n int) *program.Program {
	t.Helper()
	b := instr.NewBuilder()
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, 0).Emit(instr.CALL).Emit(instr.RETURN)
	code, err := b.Assemble()
	require.NoError(t, err)
	wrapper := &types.Function{
		Typ:  &types.FunctionType{Params: []types.Type{types.TypeAny}, Returns: []types.Type{types.TypeI32}},
		Code: instr.Marshal(code),
	}
	b = instr.NewBuilder()
	b.Emit(instr.CONST_GET, 1).Emit(instr.CONST_GET, 2).Emit(instr.CALL)
	code, err = b.Assemble()
	require.NoError(t, err)
	return program.New(code, program.WithConstants(tailRefFunction(t), types.String("held"), wrapper))
}

// nativeEntries is the number of native entries at either tier.
func nativeEntries(profiler *prof.Profiler) float64 {
	baseline, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "baseline"})
	optimized, _ := profiler.Metric("vm_jit_entries_total", prof.Label{Key: "tier", Value: "optimized"})
	return baseline + optimized
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

// pollDeadline bounds poll. It is a hang guard, not a performance budget: a
// condition holds in milliseconds, but -race plus atomic coverage on a loaded
// CI runner slows native warmup by an order of magnitude over the 5s a plain
// run needs.
const pollDeadline = 60 * time.Second

// poll runs cond on the calling goroutine until it holds, so nothing cond
// touches outlives the case's deferred teardown. It fails the test after
// pollDeadline.
func poll(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(pollDeadline)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("condition never satisfied")
		}
		time.Sleep(time.Millisecond)
	}
}

// runProgram runs prog threaded and returns the single value it leaves on
// the stack.
func runProgram(t *testing.T, prog *program.Program) types.Value {
	t.Helper()
	vm := interp.New(prog, interp.WithThreshold(-1))
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
	vm := interp.New(prog, interp.WithThreshold(-1))
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

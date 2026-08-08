package benchmarks_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkCall_RecursiveFib(b *testing.B) {
	for _, n := range []int32{20, 35} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			want := recursiveFibReference(n)
			prog := recursiveFib(n)
			require.NoError(b, program.Verify(prog))

			benchmarkVM(b, prog, types.BoxI32(want))
			fibScript := fmt.Sprintf(`def fib(n):
    if n < 2: return n
    return fib(n-1) + fib(n-2)
def run(): return fib(%d)`, n)
			benchmarkCompare(b, benchmarkComparison{
				native: func() int32 { return recursiveFibReference(n) },
				wazero: "recursive_fib",
				args:   []uint64{uint64(uint32(n))},
				scripts: benchmarkScripts{
					tengo: fmt.Sprintf(`fib := func(n) { if n < 2 { return n }; return fib(n-1) + fib(n-2) }; result := fib(%d)`, n),
					gopherLua: fmt.Sprintf(`function fib(n) if n < 2 then return n end return fib(n-1) + fib(n-2) end
function run() return fib(%d) end`, n),
					goja: fmt.Sprintf(`function fib(n) { if (n < 2) return n; return fib(n-1) + fib(n-2); }
function run() { return fib(%d); }`, n),
					gpython: fibScript,
					cpython: fibScript,
					yaegi: fmt.Sprintf(`package bench
func fib(n int32) int32 { if n < 2 { return n }; return fib(n-1) + fib(n-2) }
func Run() int32 { return fib(%d) }`, n),
				},
			}, want)
		})
	}
}

func BenchmarkCall_IndirectRecursiveFib(b *testing.B) {
	const n int32 = 20
	want := recursiveFibReference(n)
	prog := indirectRecursiveFib(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			type fib func(int32, fib) int32
			var run fib
			run = func(value int32, self fib) int32 {
				if value < 2 {
					return value
				}
				return self(value-1, self) + self(value-2, self)
			}
			return run(n, run)
		},
		wazero: "indirect_recursive_fib",
		args:   []uint64{uint64(uint32(n))},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`fib := func(n, self) { if n < 2 { return n }; return self(n-1, self) + self(n-2, self) }; result := fib(%d, fib)`, n),
			gopherLua: fmt.Sprintf(`function fib(n, self) if n < 2 then return n end return self(n-1, self) + self(n-2, self) end
function run() return fib(%d, fib) end`, n),
			goja: fmt.Sprintf(`function fib(n, self) { if (n < 2) return n; return self(n-1, self) + self(n-2, self); }
function run() { return fib(%d, fib); }`, n),
			gpython: fmt.Sprintf(`def fib(n, self):
    if n < 2: return n
    return self(n-1, self) + self(n-2, self)
def run(): return fib(%d, fib)`, n),
			yaegi: fmt.Sprintf(`package bench
var run func(int32) int32
func init() { run = func(n int32) int32 { if n < 2 { return n }; return run(n-1) + run(n-2) } }
func Run() int32 { return run(%d) }`, n),
		},
	}, want)
}

func BenchmarkCall_ClosureCounter(b *testing.B) {
	const count = 128
	want := int32(count)
	prog := closureCounter(count)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var value int32
			next := func() int32 { value++; return value }
			for range count {
				value = next()
			}
			return value
		},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { value := 0; next := func() { value++; return value }; for index := 0; index < %d; index++ { value = next() }; return value }()`, count),
			gopherLua: fmt.Sprintf(`function run() local value = 0; local function next() value = value + 1; return value end; for _ = 1, %d do value = next() end; return value end`, count),
			goja:      fmt.Sprintf(`function run() { let value = 0; const next = () => ++value; for (let index = 0; index < %d; index++) value = next(); return value; }`, count),
			gpython: fmt.Sprintf(`def run():
    value = [0]
    def next():
        value[0] += 1
        return value[0]
    for _ in range(%d): value[0] = next()
    return value[0]`, count),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var value int32; next := func() int32 { value++; return value }; for index := 0; index < %d; index++ { value = next() }; return value }`, count),
		},
	}, want)
}

func BenchmarkCall_NQueens(b *testing.B) {
	const n int32 = 7
	want := nqueensReference(n)
	prog := nqueens(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`def solve(row, n, cols, diag1, diag2):
    if row == n:
        return 1
    count = 0
    col = 0
    while col < n:
        d1 = row - col + n - 1
        d2 = row + col
        if not cols[col] and not diag1[d1] and not diag2[d2]:
            cols[col] = True
            diag1[d1] = True
            diag2[d2] = True
            count = count + solve(row + 1, n, cols, diag1, diag2)
            cols[col] = False
            diag1[d1] = False
            diag2[d2] = False
        col = col + 1
    return count


def run():
    n = %d
    cols = [False for i in range(n)]
    diag1 = [False for i in range(2 * n - 1)]
    diag2 = [False for i in range(2 * n - 1)]
    return solve(0, n, cols, diag1, diag2)`, n)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return nqueensReference(n) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func BenchmarkCall_Fannkuch(b *testing.B) {
	const n int32 = 6
	want := fannkuchReference(n)
	prog := fannkuch(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`def count_flips(perm):
    a = perm[:]
    flips = 0
    k = a[0]
    while k != 0:
        i = 0
        j = k
        while i < j:
            t = a[i]
            a[i] = a[j]
            a[j] = t
            i = i + 1
            j = j - 1
        flips = flips + 1
        k = a[0]
    return flips


def permute(a, k, permcount, checksum, maxflips):
    if k == 1:
        flips = count_flips(a)
        if flips > maxflips:
            maxflips = flips
        if permcount %% 2 == 0:
            checksum = checksum + flips
        else:
            checksum = checksum - flips
        return (permcount + 1, checksum, maxflips)
    i = 0
    while i < k:
        permcount, checksum, maxflips = permute(a, k - 1, permcount, checksum, maxflips)
        if k %% 2 == 0:
            tmp = a[i]
            a[i] = a[k - 1]
            a[k - 1] = tmp
        else:
            tmp = a[0]
            a[0] = a[k - 1]
            a[k - 1] = tmp
        i = i + 1
    return (permcount, checksum, maxflips)


def run():
    n = %d
    a = [0] * n
    i = 0
    while i < n:
        a[i] = i
        i = i + 1
    permcount, checksum, maxflips = permute(a, n, 0, 0, 0)
    return checksum * 1000 + maxflips`, n)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return fannkuchReference(n) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func recursiveFib(n int32) *program.Program {
	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).Params(types.TypeI32)
	base := b.Label()
	fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	return program.New(
		[]instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(n))), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
		program.WithConstants(fn),
	)
}

func indirectRecursiveFib(n int32) *program.Program {
	b := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32, types.TypeRef)
	base := b.Label()
	fn := b.Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_LT_S)).
		BrIf(base).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.RETURN)).
		MustBuild()
	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(n))),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		},
		program.WithConstants(fn),
	)
}

func closureCounter(count int) *program.Program {
	fn := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Captures(types.TypeI32).
		Emit(
			instr.New(instr.UPVAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.DUP),
			instr.New(instr.UPVAL_SET, 0),
			instr.New(instr.RETURN),
		).
		MustBuild()
	code := []instr.Instruction{
		instr.New(instr.I32_CONST, 0),
		instr.New(instr.CONST_GET, 0),
		instr.New(instr.CLOSURE_NEW),
		instr.New(instr.LOCAL_SET, 0),
	}
	for index := range count {
		code = append(code, instr.New(instr.LOCAL_GET, 0), instr.New(instr.CALL))
		if index+1 < count {
			code = append(code, instr.New(instr.DROP))
		}
	}
	return program.New(code, program.WithConstants(fn), program.WithLocals(types.TypeRef))
}

// nqueens builds the eight-queens backtracking-count kernel. solve is
// genuinely recursive, called through CONST_GET+CALL once per board column,
// with cols/diag1/diag2 passed as ref parameters on every recursive call.
func nqueens(n int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeI1Array)

	// solve params: 0=row,1=n,2=cols,3=diag1,4=diag2; locals: 5=count,6=col,7=d1,8=d2
	fb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32, types.TypeI32, types.TypeRef, types.TypeRef, types.TypeRef).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	base := fb.Label()
	colLoop := fb.Label()
	colDone := fb.Label()
	skip := fb.Label()
	solveFn := fb.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_EQ)).
		BrIf(base).
		Emit(
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 6),
		).
		Bind(colLoop).
		Emit(instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).
		BrIf(colDone).
		Emit(
			// d1 = row - col + n - 1; d2 = row + col
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_ADD),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 7),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 8),
			// blocked = cols[col] | diag1[d1] | diag2[d2]
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 6), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 7), instr.New(instr.ARRAY_GET), instr.New(instr.I32_OR),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 8), instr.New(instr.ARRAY_GET), instr.New(instr.I32_OR),
		).
		BrIf(skip).
		Emit(
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 7), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 8), instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_SET),
			// count += solve(row+1, n, cols, diag1, diag2)
			instr.New(instr.LOCAL_GET, 5),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 7), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 8), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_SET),
		).
		Bind(skip).
		Emit(instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 6)).
		Br(colLoop).
		Bind(colDone).
		Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.RETURN)).
		Bind(base).
		Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.RETURN)).
		MustBuild()
	solveIdx := b.Const(solveFn)

	// main locals: 0=cols,1=diag1,2=diag2
	b.Locals(types.TypeRef, types.TypeRef, types.TypeRef)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, uint64(uint32(2*n-1))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, uint64(uint32(2*n-1))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 2)

	b.Emit(instr.I32_CONST, 0)
	b.Emit(instr.I32_CONST, uint64(uint32(n)))
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 2)
	b.Emit(instr.CONST_GET, uint64(solveIdx))
	b.Emit(instr.CALL)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// fannkuch builds the pancake-flip permutation-search kernel using the
// recursive Heap's-algorithm formulation from minipy's fannkuch.py (the
// classic iterative fannkuch-redux state machine is a different algorithm and
// is not ported here). permute returns three values (permcount, checksum,
// maxflips) through minivm's native multi-return CALL/RETURN instead of a
// tuple: program/verify.go's call already pushes every declared return, and
// RETURN copies the top len(Returns) stack values to the caller in order.
func fannkuch(n int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeI32Array)

	// count_flips params: 0=perm; locals: 1=a,2=flips,3=k,4=i,5=j,6=t
	// perm is not read again after the copy, so the copy consuming its one
	// retained LOCAL_GET instance via ARRAY_SLICE needs no DUP.
	cfBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeRef).
		Locals(types.TypeRef, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	outer := cfBuilder.Label()
	outerDone := cfBuilder.Label()
	inner := cfBuilder.Label()
	innerDone := cfBuilder.Label()
	countFlipsFn := cfBuilder.
		Emit(
			// a = perm[:]; flips = 0; k = a[0]
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, uint64(uint32(n))),
			instr.New(instr.ARRAY_SLICE), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 3),
		).
		Bind(outer).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQ)).
		BrIf(outerDone).
		Emit(
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_SET, 5),
		).
		Bind(inner).
		Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_GE_S)).
		BrIf(innerDone).
		Emit(
			// t = a[i]; a[i] = a[j]; a[j] = t
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 4), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 6),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 5), instr.New(instr.ARRAY_GET), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 6), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 5),
		).
		Br(inner).
		Bind(innerDone).
		Emit(
			// flips++; k = a[0]
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 3),
		).
		Br(outer).
		Bind(outerDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()
	countFlipsIdx := b.Const(countFlipsFn)
	permuteIdx := countFlipsIdx + 1 // permute is the next constant registered below

	// permute params: 0=a,1=k,2=permcount,3=checksum,4=maxflips
	// locals: 5=flips,6=i,7=tmp; returns (permcount,checksum,maxflips)
	pBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32, types.TypeI32, types.TypeI32}}).
		Params(types.TypeRef, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32)
	base := pBuilder.Label()
	loop := pBuilder.Label()
	loopDone := pBuilder.Label()
	oddSwap := pBuilder.Label()
	swapDone := pBuilder.Label()
	afterMax := pBuilder.Label()
	oddChecksum := pBuilder.Label()
	checksumDone := pBuilder.Label()
	permuteFn := pBuilder.
		Emit(instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_EQ)).
		BrIf(base).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 6)).
		Bind(loop).
		Emit(instr.New(instr.LOCAL_GET, 6), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).
		BrIf(loopDone).
		Emit(
			// permcount, checksum, maxflips = permute(a, k-1, permcount, checksum, maxflips)
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.CONST_GET, uint64(permuteIdx)), instr.New(instr.CALL),
			instr.New(instr.LOCAL_SET, 4), instr.New(instr.LOCAL_SET, 3), instr.New(instr.LOCAL_SET, 2),
			// k % 2 == 0 ? swap a[i],a[k-1] : swap a[0],a[k-1]
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_REM_S),
			instr.New(instr.I32_CONST, 0), instr.New(instr.I32_NE),
		).
		BrIf(oddSwap).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 6), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 7),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 6),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.ARRAY_GET),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 7), instr.New(instr.ARRAY_SET),
		).
		Br(swapDone).
		Bind(oddSwap).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 7),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.ARRAY_GET),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 7), instr.New(instr.ARRAY_SET),
		).
		Bind(swapDone).
		Emit(instr.New(instr.LOCAL_GET, 6), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 6)).
		Br(loop).
		Bind(loopDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4), instr.New(instr.RETURN)).
		Bind(base).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.CONST_GET, uint64(countFlipsIdx)), instr.New(instr.CALL), instr.New(instr.LOCAL_SET, 5)).
		Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_LE_S)).
		BrIf(afterMax).
		Emit(instr.New(instr.LOCAL_GET, 5), instr.New(instr.LOCAL_SET, 4)).
		Bind(afterMax).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_REM_S), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_NE)).
		BrIf(oddChecksum).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 3)).
		Br(checksumDone).
		Bind(oddChecksum).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 3)).
		Bind(checksumDone).
		Emit(
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 3),
			instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.RETURN),
		).
		MustBuild()
	b.Const(permuteFn)

	// main locals: 0=a,1=i,2=maxflips,3=checksum
	b.Locals(types.TypeRef, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)).Emit(instr.LOCAL_SET, 0)
	fillLoop := b.Label()
	fillDone := b.Label()
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(fillLoop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(fillDone)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 1).Emit(instr.ARRAY_SET)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(fillLoop)
	b.Bind(fillDone)

	// permcount, checksum, maxflips = permute(a, n, 0, 0, 0)
	b.Emit(instr.LOCAL_GET, 0)
	b.Emit(instr.I32_CONST, uint64(uint32(n)))
	b.Emit(instr.I32_CONST, 0)
	b.Emit(instr.I32_CONST, 0)
	b.Emit(instr.I32_CONST, 0)
	b.Emit(instr.CONST_GET, uint64(permuteIdx))
	b.Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 2) // maxflips
	b.Emit(instr.LOCAL_SET, 3) // checksum
	b.Emit(instr.DROP)         // permcount unused

	// checksum*1000 + maxflips
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1000).Emit(instr.I32_MUL)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func recursiveFibReference(n int32) int32 {
	if n < 2 {
		return n
	}
	return recursiveFibReference(n-1) + recursiveFibReference(n-2)
}

// nqueensReference transcribes nqueens' solve recursion operation-for-
// operation so its result is identical to the bytecode kernel.
func nqueensReference(n int32) int32 {
	cols := make([]bool, n)
	diag1 := make([]bool, 2*n-1)
	diag2 := make([]bool, 2*n-1)
	return nqueensSolve(0, n, cols, diag1, diag2)
}

func nqueensSolve(row, n int32, cols, diag1, diag2 []bool) int32 {
	if row == n {
		return 1
	}
	var count int32
	for col := int32(0); col < n; col++ {
		d1 := row - col + n - 1
		d2 := row + col
		if !cols[col] && !diag1[d1] && !diag2[d2] {
			cols[col] = true
			diag1[d1] = true
			diag2[d2] = true
			count += nqueensSolve(row+1, n, cols, diag1, diag2)
			cols[col] = false
			diag1[d1] = false
			diag2[d2] = false
		}
	}
	return count
}

// fannkuchReference transcribes fannkuch's recursive Heap's-algorithm
// permutation search operation-for-operation so its result is identical to
// the bytecode kernel.
func fannkuchReference(n int32) int32 {
	a := make([]int32, n)
	for i := range a {
		a[i] = int32(i)
	}
	_, checksum, maxflips := fannkuchPermute(a, n, 0, 0, 0)
	return checksum*1000 + maxflips
}

func fannkuchPermute(a []int32, k, permcount, checksum, maxflips int32) (int32, int32, int32) {
	if k == 1 {
		flips := fannkuchCountFlips(a)
		if flips > maxflips {
			maxflips = flips
		}
		if permcount%2 == 0 {
			checksum += flips
		} else {
			checksum -= flips
		}
		return permcount + 1, checksum, maxflips
	}
	for i := int32(0); i < k; i++ {
		permcount, checksum, maxflips = fannkuchPermute(a, k-1, permcount, checksum, maxflips)
		if k%2 == 0 {
			a[i], a[k-1] = a[k-1], a[i]
		} else {
			a[0], a[k-1] = a[k-1], a[0]
		}
	}
	return permcount, checksum, maxflips
}

func fannkuchCountFlips(perm []int32) int32 {
	a := append([]int32(nil), perm...)
	var flips int32
	k := a[0]
	for k != 0 {
		i, j := int32(0), k
		for i < j {
			a[i], a[j] = a[j], a[i]
			i++
			j--
		}
		flips++
		k = a[0]
	}
	return flips
}

package benchmarks_test

import (
	"fmt"
	"strings"
	"testing"

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

// recursiveFib calls itself through CONST_GET+CALL: fib(n) = fib(n-1) +
// fib(n-2), with n < 2 short-circuiting at "base".
const recursiveFibListing = `
.constants
func(i32) i32
	local.get 0
	i32.const 2
	i32.lt_s
	br_if base
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	local.get 0
	i32.const 2
	i32.sub
	const.get 0
	call
	i32.add
	return
	base:
	local.get 0
	return
.code
	i32.const %d
	const.get 0
	call
`

func recursiveFib(n int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(recursiveFibListing, n))
}

// indirectRecursiveFib calls itself through a value parameter (param 1)
// instead of a constant index, exercising CALL against a callee that arrives
// on the stack rather than by CONST_GET.
const indirectRecursiveFibListing = `
.constants
func(i32, any) i32
	local.get 0
	i32.const 2
	i32.lt_s
	br_if base
	local.get 0
	i32.const 1
	i32.sub
	local.get 1
	local.get 1
	call
	local.get 0
	i32.const 2
	i32.sub
	local.get 1
	local.get 1
	call
	i32.add
	return
	base:
	local.get 0
	return
.code
	i32.const %d
	const.get 0
	const.get 0
	call
`

func indirectRecursiveFib(n int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(indirectRecursiveFibListing, n))
}

// closureCounter builds a closure over one i32 upvalue that increments and
// returns it, then calls it count times, dropping every result but the
// last. The call/drop pairs are generated rather than spelled out: writing
// count of them out longhand would not make the fixture more readable.
const closureCounterHeader = `
.locals
any
.constants
func() i32
	capture i32
	upval.get 0
	i32.const 1
	i32.add
	dup
	upval.set 0
	return
.code
i32.const 0
const.get 0
closure.new
local.set 0
`

func closureCounter(count int) *program.Program {
	var sb strings.Builder
	sb.WriteString(closureCounterHeader)
	for index := range count {
		sb.WriteString("local.get 0\ncall\n")
		if index+1 < count {
			sb.WriteString("drop\n")
		}
	}
	return mustParseProgram(sb.String())
}

// nqueens builds the eight-queens backtracking-count kernel. solve is
// genuinely recursive, called through CONST_GET+CALL once per board column,
// with cols/diag1/diag2 passed as ref parameters on every recursive call.
// solve params: 0=row, 1=n, 2=cols, 3=diag1, 4=diag2;
// solve locals:  5=count, 6=col, 7=d1, 8=d2.
// Type 0 is the []i1 element type shared by cols/diag1/diag2.
const nqueensListing = `
.locals
any
any
any
.types
[]i1
.constants
func(i32, i32, any, any, any) i32
	i32
	i32
	i32
	i32
	local.get 0
	local.get 1
	i32.eq
	br_if base
	i32.const 0
	local.set 5
	i32.const 0
	local.set 6
	colLoop:
	local.get 6
	local.get 1
	i32.ge_s
	br_if colDone
	local.get 0
	local.get 6
	i32.sub
	local.get 1
	i32.add
	i32.const 1
	i32.sub
	local.set 7
	local.get 0
	local.get 6
	i32.add
	local.set 8
	local.get 2
	local.get 6
	array.get
	local.get 3
	local.get 7
	array.get
	i32.or
	local.get 4
	local.get 8
	array.get
	i32.or
	br_if skip
	local.get 2
	local.get 6
	i32.const 1
	array.set
	local.get 3
	local.get 7
	i32.const 1
	array.set
	local.get 4
	local.get 8
	i32.const 1
	array.set
	local.get 5
	local.get 0
	i32.const 1
	i32.add
	local.get 1
	local.get 2
	local.get 3
	local.get 4
	const.get 0
	call
	i32.add
	local.set 5
	local.get 2
	local.get 6
	i32.const 0
	array.set
	local.get 3
	local.get 7
	i32.const 0
	array.set
	local.get 4
	local.get 8
	i32.const 0
	array.set
	skip:
	local.get 6
	i32.const 1
	i32.add
	local.set 6
	br colLoop
	colDone:
	local.get 5
	return
	base:
	i32.const 1
	return
.code
	i32.const %[1]d
	array.new_default 0
	local.set 0
	i32.const %[2]d
	array.new_default 0
	local.set 1
	i32.const %[2]d
	array.new_default 0
	local.set 2
	i32.const 0
	i32.const %[1]d
	local.get 0
	local.get 1
	local.get 2
	const.get 0
	call
`

func nqueens(n int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(nqueensListing, n, 2*n-1))
}

// fannkuch builds the pancake-flip permutation-search kernel using the
// recursive Heap's-algorithm formulation from minipy's fannkuch.py (the
// classic iterative fannkuch-redux state machine is a different algorithm and
// is not ported here). permute returns three values (permcount, checksum,
// maxflips) through minivm's native multi-return CALL/RETURN instead of a
// tuple: program/verify.go's call already pushes every declared return, and
// RETURN copies the top len(Returns) stack values to the caller in order.
//
// Constant 0 is count_flips (params: 0=perm; locals: 1=a, 2=flips, 3=k, 4=i,
// 5=j, 6=t). perm is not read again after the copy, so the copy consuming its
// one retained local.get instance via array.slice needs no dup.
//
// Constant 1 is permute (params: 0=a, 1=k, 2=permcount, 3=checksum,
// 4=maxflips; locals: 5=flips, 6=i, 7=tmp), self-recursive through
// const.get 1 and returning (permcount, checksum, maxflips).
//
// Type 0 is the []i32 element type shared by perm/a.
const fannkuchListing = `
.locals
any
i32
i32
i32
.types
[]i32
.constants
func(any) i32
	any
	i32
	i32
	i32
	i32
	i32
	local.get 0
	i32.const 0
	i32.const %[1]d
	array.slice
	local.set 1
	i32.const 0
	local.set 2
	local.get 1
	i32.const 0
	array.get
	local.set 3
	outer:
	local.get 3
	i32.const 0
	i32.eq
	br_if outerDone
	i32.const 0
	local.set 4
	local.get 3
	local.set 5
	inner:
	local.get 4
	local.get 5
	i32.ge_s
	br_if innerDone
	local.get 1
	local.get 4
	array.get
	local.set 6
	local.get 1
	local.get 4
	local.get 1
	local.get 5
	array.get
	array.set
	local.get 1
	local.get 5
	local.get 6
	array.set
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	local.get 5
	i32.const 1
	i32.sub
	local.set 5
	br inner
	innerDone:
	local.get 2
	i32.const 1
	i32.add
	local.set 2
	local.get 1
	i32.const 0
	array.get
	local.set 3
	br outer
	outerDone:
	local.get 2
	return
func(any, i32, i32, i32, i32) (i32, i32, i32)
	i32
	i32
	i32
	local.get 1
	i32.const 1
	i32.eq
	br_if base
	i32.const 0
	local.set 6
	loop:
	local.get 6
	local.get 1
	i32.ge_s
	br_if loopDone
	local.get 0
	local.get 1
	i32.const 1
	i32.sub
	local.get 2
	local.get 3
	local.get 4
	const.get 1
	call
	local.set 4
	local.set 3
	local.set 2
	local.get 1
	i32.const 2
	i32.rem_s
	i32.const 0
	i32.ne
	br_if oddSwap
	local.get 0
	local.get 6
	array.get
	local.set 7
	local.get 0
	local.get 6
	local.get 0
	local.get 1
	i32.const 1
	i32.sub
	array.get
	array.set
	local.get 0
	local.get 1
	i32.const 1
	i32.sub
	local.get 7
	array.set
	br swapDone
	oddSwap:
	local.get 0
	i32.const 0
	array.get
	local.set 7
	local.get 0
	i32.const 0
	local.get 0
	local.get 1
	i32.const 1
	i32.sub
	array.get
	array.set
	local.get 0
	local.get 1
	i32.const 1
	i32.sub
	local.get 7
	array.set
	swapDone:
	local.get 6
	i32.const 1
	i32.add
	local.set 6
	br loop
	loopDone:
	local.get 2
	local.get 3
	local.get 4
	return
	base:
	local.get 0
	const.get 0
	call
	local.set 5
	local.get 5
	local.get 4
	i32.le_s
	br_if afterMax
	local.get 5
	local.set 4
	afterMax:
	local.get 2
	i32.const 2
	i32.rem_s
	i32.const 0
	i32.ne
	br_if oddChecksum
	local.get 3
	local.get 5
	i32.add
	local.set 3
	br checksumDone
	oddChecksum:
	local.get 3
	local.get 5
	i32.sub
	local.set 3
	checksumDone:
	local.get 2
	i32.const 1
	i32.add
	local.get 3
	local.get 4
	return
.code
	i32.const %[1]d
	array.new_default 0
	local.set 0
	i32.const 0
	local.set 1
fillLoop:
	local.get 1
	i32.const %[1]d
	i32.ge_s
	br_if fillDone
	local.get 0
	local.get 1
	local.get 1
	array.set
	local.get 1
	i32.const 1
	i32.add
	local.set 1
	br fillLoop
fillDone:
	local.get 0
	i32.const %[1]d
	i32.const 0
	i32.const 0
	i32.const 0
	const.get 1
	call
	local.set 2
	local.set 3
	drop
	local.get 3
	i32.const 1000
	i32.mul
	local.get 2
	i32.add
`

func fannkuch(n int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(fannkuchListing, n))
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

package benchmarks_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func BenchmarkMemory_TypedArraySum(b *testing.B) {
	const size int32 = 256
	want := typedArraySumReference(size)
	prog := typedArraySum(size)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var total int32
			for value := int32(1); value <= size; value++ {
				total += value
			}
			return total
		},
		wazero: "typed_array_sum",
		args:   []uint64{uint64(uint32(size))},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { total := 0; for value := 1; value <= %d; value++ { total += value }; return total }()`, size),
			gopherLua: fmt.Sprintf(`function run() local total = 0; for value = 1, %d do total = total + value end; return total end`, size),
			goja:      fmt.Sprintf(`function run() { let total = 0; for (let value = 1; value <= %d; value++) total += value; return total; }`, size),
			gpython: fmt.Sprintf(`def run():
    total = 0
    for value in range(1, %d + 1): total += value
    return total`, size),
			yaegi: fmt.Sprintf(`package bench
func Run() int32 { var total int32; for value := int32(1); value <= %d; value++ { total += value }; return total }`, size),
		},
	}, want)
}

func BenchmarkMemory_AllocationGraph(b *testing.B) {
	const depth int32 = 128
	want := depth
	prog := allocationGraph(depth)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			type node struct{ next *node }
			root := &node{}
			for index := int32(1); index < depth; index++ {
				root = &node{next: root}
			}
			if root == nil {
				return 0
			}
			return depth
		},
		scripts: benchmarkScripts{
			tengo:     fmt.Sprintf(`result := func() { root := [undefined]; for index := 1; index < %d; index++ { root = [root] }; return %d + len(root) - 1 }()`, depth, depth),
			gopherLua: fmt.Sprintf(`function run() local root = {false}; for _ = 2, %d do root = {root} end; return %d + #root - 1 end`, depth, depth),
			goja:      fmt.Sprintf(`function run() { let root = [null]; for (let index = 1; index < %d; index++) root = [root]; return %d + root.length - 1; }`, depth, depth),
			gpython: fmt.Sprintf(`def run():
    root = [None]
    for _ in range(1, %d): root = [root]
    return %d + len(root) - 1`, depth, depth),
			yaegi: fmt.Sprintf(`package bench
type node struct { next *node }
func Run() int32 { root := &node{}; for index := int32(1); index < %d; index++ { root = &node{next: root} }; if root == nil { return 0 }; return %d }`, depth, depth),
		},
	}, want)
}

func typedArraySum(size int32) *program.Program {
	values := make(types.TypedArray[int32], size)
	for index := range values {
		values[index] = int32(index + 1)
	}
	b := program.NewBuilder()
	array := b.Const(values)
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(size))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.CONST_GET, uint64(array)).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_GET)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 0)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func BenchmarkMemory_PermutationFlips(b *testing.B) {
	const size int32 = 24
	const depth int32 = 64
	want := permutationFlipsReference(size, depth)
	prog := permutationFlips(size, depth)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			var walk func(d int32) int32
			walk = func(d int32) int32 {
				if d == 0 {
					return 0
				}
				arr := make([]int32, size)
				for index := range arr {
					arr[index] = size - 1 - int32(index)
				}
				lo, hi := int32(0), size-1
				for lo < hi {
					arr[lo], arr[hi] = arr[hi], arr[lo]
					lo++
					hi--
				}
				return arr[size-1] + walk(d-1)
			}
			return walk(depth)
		},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`walk := func(d) {
    if d == 0 { return 0 }
    arr := []
    for i := 0; i < %d; i++ { arr = append(arr, %d - 1 - i) }
    lo := 0
    hi := %d - 1
    for lo < hi {
        t := arr[lo]
        arr[lo] = arr[hi]
        arr[hi] = t
        lo++
        hi--
    }
    return arr[%d - 1] + walk(d - 1)
}
result := walk(%d)`, size, size, size, size, depth),
			gopherLua: fmt.Sprintf(`function walk(d)
    if d == 0 then return 0 end
    local arr = {}
    for i = 0, %d - 1 do arr[i] = %d - 1 - i end
    local lo, hi = 0, %d - 1
    while lo < hi do
        local t = arr[lo]
        arr[lo] = arr[hi]
        arr[hi] = t
        lo = lo + 1
        hi = hi - 1
    end
    return arr[%d - 1] + walk(d - 1)
end
function run() return walk(%d) end`, size, size, size, size, depth),
			goja: fmt.Sprintf(`function walk(d) {
    if (d === 0) return 0;
    let arr = [];
    for (let i = 0; i < %d; i++) arr[i] = %d - 1 - i;
    let lo = 0, hi = %d - 1;
    while (lo < hi) {
        let t = arr[lo]; arr[lo] = arr[hi]; arr[hi] = t;
        lo++; hi--;
    }
    return arr[%d - 1] + walk(d - 1);
}
function run() { return walk(%d); }`, size, size, size, size, depth),
			gpython: fmt.Sprintf(`def walk(d):
    if d == 0: return 0
    arr = [0] * %d
    for i in range(%d): arr[i] = %d - 1 - i
    lo, hi = 0, %d - 1
    while lo < hi:
        arr[lo], arr[hi] = arr[hi], arr[lo]
        lo += 1
        hi -= 1
    return arr[%d - 1] + walk(d - 1)
def run(): return walk(%d)`, size, size, size, size, size, depth),
			yaegi: fmt.Sprintf(`package bench
func walk(d int32) int32 {
    if d == 0 { return 0 }
    arr := make([]int32, %d)
    for i := range arr { arr[i] = int32(%d) - 1 - int32(i) }
    lo, hi := 0, %d - 1
    for lo < hi {
        arr[lo], arr[hi] = arr[hi], arr[lo]
        lo++
        hi--
    }
    return arr[%d - 1] + walk(d - 1)
}
func Run() int32 { return walk(%d) }`, size, size, size, size, depth),
		},
	}, want)
}

func BenchmarkMemory_StructTreeWalk(b *testing.B) {
	const depth int32 = 9
	want := structTreeWalkReference(depth)
	prog := structTreeWalk(depth)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	benchmarkCompare(b, benchmarkComparison{
		native: func() int32 {
			type node struct{ left, right *node }
			var build func(d int32) *node
			build = func(d int32) *node {
				n := &node{}
				if d > 0 {
					n.left = build(d - 1)
					n.right = build(d - 1)
				}
				return n
			}
			var check func(n *node) int32
			check = func(n *node) int32 {
				if n == nil {
					return 0
				}
				return 1 + check(n.left) + check(n.right)
			}
			return check(build(depth))
		},
		scripts: benchmarkScripts{
			tengo: fmt.Sprintf(`build := func(d) {
    n := {left: undefined, right: undefined}
    if d > 0 {
        n.left = build(d - 1)
        n.right = build(d - 1)
    }
    return n
}
check := func(n) {
    if n == undefined { return 0 }
    return 1 + check(n.left) + check(n.right)
}
result := check(build(%d))`, depth),
			gopherLua: fmt.Sprintf(`function build(d)
    local n = {left=false, right=false}
    if d > 0 then
        n.left = build(d - 1)
        n.right = build(d - 1)
    end
    return n
end
function check(n)
    if not n then return 0 end
    return 1 + check(n.left) + check(n.right)
end
function run() return check(build(%d)) end`, depth),
			goja: fmt.Sprintf(`function build(d) {
    let n = { left: null, right: null };
    if (d > 0) { n.left = build(d - 1); n.right = build(d - 1); }
    return n;
}
function check(n) {
    if (n === null) return 0;
    return 1 + check(n.left) + check(n.right);
}
function run() { return check(build(%d)); }`, depth),
			gpython: fmt.Sprintf(`class Node:
    def __init__(self):
        self.left = None
        self.right = None
def build(d):
    n = Node()
    if d > 0:
        n.left = build(d - 1)
        n.right = build(d - 1)
    return n
def check(n):
    if n is None: return 0
    return 1 + check(n.left) + check(n.right)
def run(): return check(build(%d))`, depth),
			yaegi: fmt.Sprintf(`package bench
type node struct { left, right *node }
func build(d int32) *node {
    n := &node{}
    if d > 0 {
        n.left = build(d - 1)
        n.right = build(d - 1)
    }
    return n
}
func check(n *node) int32 {
    if n == nil { return 0 }
    return 1 + check(n.left) + check(n.right)
}
func Run() int32 { return check(build(%d)) }`, depth),
		},
	}, want)
}

func BenchmarkMemory_BinaryTrees(b *testing.B) {
	const minDepth, maxDepth int32 = 4, 6
	want := binaryTreesReference(minDepth, maxDepth)
	prog := binaryTrees(minDepth, maxDepth)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`class Node:
    def __init__(self, item, left, right):
        self.item = item
        self.left = left
        self.right = right


def item_check(t):
    if t is None:
        return 0
    if t.left is None:
        return t.item
    return t.item + item_check(t.left) - item_check(t.right)


def bottom_up_tree(item, depth):
    if depth > 0:
        return Node(item, bottom_up_tree(2 * item - 1, depth - 1), bottom_up_tree(2 * item, depth - 1))
    return Node(item, None, None)


def run():
    min_depth = %d
    max_depth = %d
    stretch_tree = bottom_up_tree(0, max_depth + 1)
    checksum = item_check(stretch_tree)

    long_lived_tree = bottom_up_tree(0, max_depth)

    depth = min_depth
    while depth <= max_depth:
        iterations = 1
        shift = 0
        while shift < (max_depth - depth + min_depth):
            iterations = iterations * 2
            shift = shift + 1
        acc = 0
        i = 1
        while i <= iterations:
            t1 = bottom_up_tree(i, depth)
            acc = acc + item_check(t1)
            t2 = bottom_up_tree(0 - i, depth)
            acc = acc + item_check(t2)
            i = i + 1
        checksum = checksum + acc
        depth = depth + 2

    checksum = checksum + item_check(long_lived_tree)
    return checksum`, minDepth, maxDepth)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return binaryTreesReference(minDepth, maxDepth) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

func BenchmarkMemory_SortStress(b *testing.B) {
	const n, rounds int32 = 128, 2
	want := sortStressReference(n, rounds)
	prog := sortStress(n, rounds)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	// minivm has no sort opcode, so sortstress.py's xs.sort() is written out
	// as an insertion sort in bytecode; the kernel therefore measures VM
	// dispatch over an insertion sort rather than over a builtin sort. Every
	// compared implementation below runs that same insertion sort, so the
	// comparison stays VM-against-VM instead of pitting bytecode against a
	// C-implemented Timsort.
	script := fmt.Sprintf(`def make_list(n, seed):
    xs = [0] * n
    s = seed
    i = 0
    while i < n:
        s = (s * 1103515245 + 12345) %% 2147483648
        xs[i] = s %% 1000000
        i = i + 1
    return xs


def insertion_sort(xs, n):
    i = 1
    while i < n:
        key = xs[i]
        j = i - 1
        while j >= 0 and xs[j] > key:
            xs[j + 1] = xs[j]
            j = j - 1
        xs[j + 1] = key
        i = i + 1


def run():
    n = %d
    rounds = %d
    checksum = 0
    seed = 1
    r = 0
    while r < rounds:
        xs = make_list(n, seed + r)
        insertion_sort(xs, n)
        i = 0
        while i < n:
            checksum = checksum + xs[i] * (i %% 7)
            i = i + 1
        checksum = checksum %% 1000000007
        r = r + 1
    return checksum`, n, rounds)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return sortStressReference(n, rounds) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

// BenchmarkMemory_StringBuild ports only strbuild.py's digits(n) token
// generation, the per-character checksum over each token, and the
// "big = big + tok + ' '" concatenation, ending in token_checksum + len(big).
// minivm has no upper/lower/replace/strip/count/find/startswith/endswith
// opcodes, so strbuild.py's trailing str-method chain is dropped; the
// expected checksum below already reflects this reduced program.
func BenchmarkMemory_StringBuild(b *testing.B) {
	const n int32 = 512
	want := stringBuildReference(n)
	prog := stringBuild(n)
	require.NoError(b, program.Verify(prog))

	benchmarkVM(b, prog, types.BoxI32(want))
	script := fmt.Sprintf(`def digits(n):
    if n == 0:
        return "0"
    v = n
    s = ""
    while v > 0:
        d = v %% 10
        s = chr(48 + d) + s
        v = v // 10
    return s


def run():
    n = %d
    big = ""
    token_checksum = 0
    i = 0
    while i < n:
        tok = digits(i * 2654435761 %% 99999)
        j = 0
        while j < len(tok):
            token_checksum = token_checksum + ord(tok[j]) * (j + 1)
            j = j + 1
        token_checksum = token_checksum %% 1000000007
        big = big + tok + " "
        i = i + 1
    return token_checksum + len(big)`, n)
	benchmarkCompare(b, benchmarkComparison{
		native:  func() int32 { return stringBuildReference(n) },
		scripts: benchmarkScripts{cpython: script, gpython: script},
	}, want)
}

// allocationGraphListing wraps a single-element []any depth times, each new
// array holding the previous root at index 0 (root = [root]).
const allocationGraphListing = `
.locals
any
i32
.types
[]any
.code
	i32.const 1
	array.new_default 0
	local.set 0
	i32.const 1
	local.set 1
loop:
	local.get 1
	i32.const %d
	i32.ge_s
	br_if done
	i32.const 1
	array.new_default 0
	dup
	i32.const 0
	local.get 0
	array.set
	local.set 0
	local.get 1
	i32.const 1
	i32.add
	local.set 1
	br loop
done:
	local.get 1
`

func allocationGraph(depth int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(allocationGraphListing, depth))
}

// permutationFlipsListing builds a self-recursive walk (constant 0; params:
// 0=depth; locals: 1=arr, 2=i, 3=lo, 4=hi, 5=t) that reverses a fresh
// size-length []any array each call and adds arr[size-1] to the recursive
// result. %[1]d substitutes size, %[2]d substitutes size-1, %[3]d substitutes
// depth.
const permutationFlipsListing = `
.types
[]any
.constants
func(i32) i32
	any
	i32
	i32
	i32
	i32
	local.get 0
	i32.const 0
	i32.eq
	br_if base
	i32.const %[1]d
	array.new_default 0
	local.set 1
	i32.const 0
	local.set 2
	fillLoop:
	local.get 2
	i32.const %[1]d
	i32.ge_s
	br_if fillDone
	local.get 1
	local.get 2
	i32.const %[2]d
	local.get 2
	i32.sub
	array.set
	local.get 2
	i32.const 1
	i32.add
	local.set 2
	br fillLoop
	fillDone:
	i32.const 0
	local.set 3
	i32.const %[2]d
	local.set 4
	swapLoop:
	local.get 3
	local.get 4
	i32.ge_s
	br_if swapDone
	local.get 1
	local.get 3
	array.get
	local.set 5
	local.get 1
	local.get 3
	local.get 1
	local.get 4
	array.get
	array.set
	local.get 1
	local.get 4
	local.get 5
	array.set
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	local.get 4
	i32.const 1
	i32.sub
	local.set 4
	br swapLoop
	swapDone:
	local.get 1
	i32.const %[2]d
	array.get
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	i32.add
	return
	base:
	i32.const 0
	return
.code
	i32.const %[3]d
	const.get 0
	call
`

func permutationFlips(size, depth int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(permutationFlipsListing, size, size-1, depth))
}

// structTreeWalkListing builds a self-recursive binary-tree build/count
// kernel over a named struct type (constant 0=build, 1=check; type 0=Node
// {value, left, right}). build's params: 0=d; its result and local 1=n are
// Node. check's param 0=node is Node, and may be the null ref at a leaf.
const structTreeWalkListing = `
.types
struct {value: i64; left: any; right: any}
.constants
func(i32) struct {value: i64; left: any; right: any}
	struct {value: i64; left: any; right: any}
	struct.new_default 0
	local.set 1
	local.get 1
	i32.const 0
	local.get 0
	i32.to_i64_s
	struct.set
	local.get 0
	i32.const 0
	i32.le_s
	br_if buildDone
	local.get 1
	i32.const 1
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	local.get 1
	i32.const 2
	local.get 0
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	buildDone:
	local.get 1
	return
func(struct {value: i64; left: any; right: any}) i32
	local.get 0
	ref.is_null
	br_if nullCase
	local.get 0
	i32.const 1
	struct.get
	const.get 1
	call
	local.get 0
	i32.const 2
	struct.get
	const.get 1
	call
	i32.add
	i32.const 1
	i32.add
	return
	nullCase:
	i32.const 0
	return
.code
	i32.const %d
	const.get 0
	call
	const.get 1
	call
`

func structTreeWalk(depth int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(structTreeWalkListing, depth))
}

// binaryTreesListing builds the benchmarks-game binary-trees kernel over a
// named struct type (type 0=Node{item, left, right}; constant 0=
// bottom_up_tree, 1=item_check; each is self-recursive, calling back through
// its own const.get index). bottom_up_tree params: 0=item,1=depth; its
// result and local 2=n are Node. item_check param 0=t is Node, and may be
// the null ref at a leaf. STRUCT_NEW_DEFAULT zero-initializes ref fields to
// the null heap ref, so the depth<=0 base case can leave left/right unset
// instead of writing a null ref. Main locals: 0=stretchTree (Node),
// 1=checksum,2=longLivedTree (Node),3=depth,4=iterations,5=shift,
// 6=acc,7=i,8=t1 (Node),9=t2 (Node). %[1]d substitutes min_depth, %[2]d max_depth, %[3]d
// max_depth+1.
const binaryTreesListing = `
.locals
struct {item: i32; left: any; right: any}
i32
struct {item: i32; left: any; right: any}
i32
i32
i32
i32
i32
struct {item: i32; left: any; right: any}
struct {item: i32; left: any; right: any}
.types
struct {item: i32; left: any; right: any}
.constants
func(i32, i32) struct {item: i32; left: any; right: any}
	struct {item: i32; left: any; right: any}
	struct.new_default 0
	local.set 2
	local.get 2
	i32.const 0
	local.get 0
	struct.set
	local.get 1
	i32.const 0
	i32.le_s
	br_if buDone
	local.get 2
	i32.const 1
	local.get 0
	i32.const 2
	i32.mul
	i32.const 1
	i32.sub
	local.get 1
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	local.get 2
	i32.const 2
	local.get 0
	i32.const 2
	i32.mul
	local.get 1
	i32.const 1
	i32.sub
	const.get 0
	call
	struct.set
	buDone:
	local.get 2
	return
func(struct {item: i32; left: any; right: any}) i32
	local.get 0
	ref.is_null
	br_if nullCase
	local.get 0
	i32.const 1
	struct.get
	ref.is_null
	br_if leafCase
	local.get 0
	i32.const 0
	struct.get
	local.get 0
	i32.const 1
	struct.get
	const.get 1
	call
	i32.add
	local.get 0
	i32.const 2
	struct.get
	const.get 1
	call
	i32.sub
	return
	leafCase:
	local.get 0
	i32.const 0
	struct.get
	return
	nullCase:
	i32.const 0
	return
.code
	i32.const 0
	i32.const %[3]d
	const.get 0
	call
	local.set 0
	local.get 0
	const.get 1
	call
	local.set 1
	i32.const 0
	i32.const %[2]d
	const.get 0
	call
	local.set 2
	i32.const %[1]d
	local.set 3
depthLoop:
	local.get 3
	i32.const %[2]d
	i32.gt_s
	br_if depthDone
	i32.const 1
	local.set 4
	i32.const 0
	local.set 5
shiftLoop:
	local.get 5
	i32.const %[2]d
	local.get 3
	i32.sub
	i32.const %[1]d
	i32.add
	i32.ge_s
	br_if shiftDone
	local.get 4
	i32.const 2
	i32.mul
	local.set 4
	local.get 5
	i32.const 1
	i32.add
	local.set 5
	br shiftLoop
shiftDone:
	i32.const 0
	local.set 6
	i32.const 1
	local.set 7
iLoop:
	local.get 7
	local.get 4
	i32.gt_s
	br_if iDone
	local.get 7
	local.get 3
	const.get 0
	call
	local.set 8
	local.get 6
	local.get 8
	const.get 1
	call
	i32.add
	local.set 6
	i32.const 0
	local.get 7
	i32.sub
	local.get 3
	const.get 0
	call
	local.set 9
	local.get 6
	local.get 9
	const.get 1
	call
	i32.add
	local.set 6
	local.get 7
	i32.const 1
	i32.add
	local.set 7
	br iLoop
iDone:
	local.get 1
	local.get 6
	i32.add
	local.set 1
	local.get 3
	i32.const 2
	i32.add
	local.set 3
	br depthLoop
depthDone:
	local.get 1
	local.get 2
	const.get 1
	call
	i32.add
`

func binaryTrees(minDepth, maxDepth int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(binaryTreesListing, minDepth, maxDepth, maxDepth+1))
}

// sortStressListing builds the sortstress kernel. minivm has no sort opcode,
// so the sort is written directly in bytecode as an insertion sort over the
// i32 array: it is a simple, obviously-correct in-place algorithm and the
// least code among the alternatives, keeping the kernel a measure of VM
// dispatch rather than of an algorithm choice. The LCG state overflows i32
// (s*1103515245 can reach ~2.4e18), so make_list (constant 0; params: 0=n,
// 1=seed; result and local 2=xs are []i32; locals 3=s(i64),4=i) keeps s in
// i64; the sorted values (s % 1000000) fit i32, so the array itself stays
// i32. insertion_sort (constant 1; params: 0=arr ([]i32),1=n; locals:
// 2=i,3=key,4=j) sorts in place. Main locals: 0=xs ([]i32),1=checksum,2=r,
// 3=i. %[1]d substitutes n, %[2]d rounds.
const sortStressListing = `
.locals
[]i32
i32
i32
i32
.types
[]i32
.constants
func(i32, i32) []i32
	[]i32
	i64
	i32
	local.get 0
	array.new_default 0
	local.set 2
	local.get 1
	i32.to_i64_s
	local.set 3
	i32.const 0
	local.set 4
	mlLoop:
	local.get 4
	local.get 0
	i32.ge_s
	br_if mlDone
	local.get 3
	i64.const 1103515245
	i64.mul
	i64.const 12345
	i64.add
	i64.const 2147483648
	i64.rem_s
	local.set 3
	local.get 2
	local.get 4
	local.get 3
	i64.const 1000000
	i64.rem_s
	i64.to_i32
	array.set
	local.get 4
	i32.const 1
	i32.add
	local.set 4
	br mlLoop
	mlDone:
	local.get 2
	return
func([]i32, i32)
	i32
	i32
	i32
	i32.const 1
	local.set 2
	outer:
	local.get 2
	local.get 1
	i32.ge_s
	br_if outerDone
	local.get 0
	local.get 2
	array.get
	local.set 3
	local.get 2
	i32.const 1
	i32.sub
	local.set 4
	inner:
	local.get 4
	i32.const 0
	i32.lt_s
	br_if innerDone
	local.get 0
	local.get 4
	array.get
	local.get 3
	i32.le_s
	br_if innerDone
	local.get 0
	local.get 4
	i32.const 1
	i32.add
	local.get 0
	local.get 4
	array.get
	array.set
	local.get 4
	i32.const 1
	i32.sub
	local.set 4
	br inner
	innerDone:
	local.get 0
	local.get 4
	i32.const 1
	i32.add
	local.get 3
	array.set
	local.get 2
	i32.const 1
	i32.add
	local.set 2
	br outer
	outerDone:
	return
.code
	i32.const 0
	local.set 1
	i32.const 0
	local.set 2
roundLoop:
	local.get 2
	i32.const %[2]d
	i32.ge_s
	br_if roundDone
	i32.const %[1]d
	i32.const 1
	local.get 2
	i32.add
	const.get 0
	call
	local.set 0
	local.get 0
	i32.const %[1]d
	const.get 1
	call
	i32.const 0
	local.set 3
sumLoop:
	local.get 3
	i32.const %[1]d
	i32.ge_s
	br_if sumDone
	local.get 1
	local.get 0
	local.get 3
	array.get
	local.get 3
	i32.const 7
	i32.rem_s
	i32.mul
	i32.add
	local.set 1
	local.get 3
	i32.const 1
	i32.add
	local.set 3
	br sumLoop
sumDone:
	local.get 1
	i32.const 1000000007
	i32.rem_s
	local.set 1
	local.get 2
	i32.const 1
	i32.add
	local.set 2
	br roundLoop
roundDone:
	local.get 1
`

func sortStress(n, rounds int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(sortStressListing, n, rounds))
}

// stringBuildListing builds the strbuild kernel: digits(n) token generation
// (constant 0; params: 0=n; locals: 1=count,2=v,3=arr ([]i32),4=idx,5=d), a
// per-character checksum read back through string.encode_utf32, and
// "big = big + tok + ' '" through string.concat (the allocating operation
// this kernel exists to measure). Type 0 is the []i32 code-point array used
// to build each token. Constant 1 is the " " separator, constant 2 the ""
// used to seed big. Main locals: 0=big,1=tokenChecksum,2=i,3=tok,
// 4=codePoints ([]i32),5=tokLen,6=j. %d substitutes n.
const stringBuildListing = `
.locals
any
i32
i32
any
[]i32
i32
i32
.types
[]i32
.constants
func(i32) any
	i32
	i32
	[]i32
	i32
	i32
	local.get 0
	i32.const 0
	i32.eq
	br_if zeroCase
	i32.const 0
	local.set 1
	local.get 0
	local.set 2
	countLoop:
	local.get 2
	i32.const 0
	i32.le_s
	br_if countDone
	local.get 1
	i32.const 1
	i32.add
	local.set 1
	local.get 2
	i32.const 10
	i32.div_s
	local.set 2
	br countLoop
	countDone:
	local.get 1
	array.new_default 0
	local.set 3
	local.get 0
	local.set 2
	local.get 1
	i32.const 1
	i32.sub
	local.set 4
	fillLoop:
	local.get 2
	i32.const 0
	i32.le_s
	br_if fillDone
	local.get 2
	i32.const 10
	i32.rem_s
	local.set 5
	local.get 3
	local.get 4
	local.get 5
	i32.const 48
	i32.add
	array.set
	local.get 2
	i32.const 10
	i32.div_s
	local.set 2
	local.get 4
	i32.const 1
	i32.sub
	local.set 4
	br fillLoop
	fillDone:
	local.get 3
	string.new_utf32
	return
	zeroCase:
	i32.const 1
	array.new_default 0
	local.set 3
	local.get 3
	i32.const 0
	i32.const 48
	array.set
	local.get 3
	string.new_utf32
	return
string " "
string ""
.code
	const.get 2
	local.set 0
	i32.const 0
	local.set 1
	i32.const 0
	local.set 2
outer:
	local.get 2
	i32.const %[1]d
	i32.ge_s
	br_if outerDone
	local.get 2
	i32.to_i64_s
	i64.const 2654435761
	i64.mul
	i64.const 99999
	i64.rem_s
	i64.to_i32
	const.get 0
	call
	local.set 3
	local.get 3
	string.encode_utf32
	local.set 4
	local.get 3
	string.len
	local.set 5
	i32.const 0
	local.set 6
charLoop:
	local.get 6
	local.get 5
	i32.ge_s
	br_if charDone
	local.get 1
	local.get 4
	local.get 6
	array.get
	local.get 6
	i32.const 1
	i32.add
	i32.mul
	i32.add
	local.set 1
	local.get 6
	i32.const 1
	i32.add
	local.set 6
	br charLoop
charDone:
	local.get 1
	i32.const 1000000007
	i32.rem_s
	local.set 1
	local.get 0
	local.get 3
	string.concat
	const.get 1
	string.concat
	local.set 0
	local.get 2
	i32.const 1
	i32.add
	local.set 2
	br outer
outerDone:
	local.get 1
	local.get 0
	string.len
	i32.add
`

func stringBuild(n int32) *program.Program {
	return mustParseProgram(fmt.Sprintf(stringBuildListing, n))
}

func typedArraySumReference(size int32) int32 {
	return size * (size + 1) / 2
}

func permutationFlipsReference(size, depth int32) int32 {
	return depth * (size - 1)
}

func structTreeWalkReference(depth int32) int32 {
	return int32(1<<uint(depth+1)) - 1
}

// binaryTreesReference transcribes binarytrees' recursive build/checksum
// operation-for-operation so its result is identical to the bytecode kernel.
func binaryTreesReference(minDepth, maxDepth int32) int32 {
	type node struct {
		item        int32
		left, right *node
	}
	var build func(item, depth int32) *node
	build = func(item, depth int32) *node {
		n := &node{item: item}
		if depth > 0 {
			n.left = build(2*item-1, depth-1)
			n.right = build(2*item, depth-1)
		}
		return n
	}
	var check func(t *node) int32
	check = func(t *node) int32 {
		if t == nil {
			return 0
		}
		if t.left == nil {
			return t.item
		}
		return t.item + check(t.left) - check(t.right)
	}

	stretchTree := build(0, maxDepth+1)
	checksum := check(stretchTree)

	longLivedTree := build(0, maxDepth)

	for depth := minDepth; depth <= maxDepth; depth += 2 {
		iterations := int32(1)
		for shift := int32(0); shift < maxDepth-depth+minDepth; shift++ {
			iterations *= 2
		}
		var acc int32
		for i := int32(1); i <= iterations; i++ {
			acc += check(build(i, depth))
			acc += check(build(-i, depth))
		}
		checksum += acc
	}

	checksum += check(longLivedTree)
	return checksum
}

// sortStressReference transcribes sortStress's LCG generator and insertion
// sort operation-for-operation so its result is identical to the bytecode
// kernel.
func sortStressReference(n, rounds int32) int32 {
	makeList := func(n, seed int32) []int32 {
		xs := make([]int32, n)
		s := int64(seed)
		for i := int32(0); i < n; i++ {
			s = (s*1103515245 + 12345) % 2147483648
			xs[i] = int32(s % 1000000)
		}
		return xs
	}
	insertionSort := func(arr []int32) {
		for i := 1; i < len(arr); i++ {
			key := arr[i]
			j := i - 1
			for j >= 0 && arr[j] > key {
				arr[j+1] = arr[j]
				j--
			}
			arr[j+1] = key
		}
	}

	var checksum int32
	const seed int32 = 1
	for r := int32(0); r < rounds; r++ {
		xs := makeList(n, seed+r)
		insertionSort(xs)
		for i := int32(0); i < n; i++ {
			checksum += xs[i] * (i % 7)
		}
		checksum %= 1000000007
	}
	return checksum
}

// stringBuildReference transcribes stringBuild's digits/checksum/concat loop
// operation-for-operation so its result is identical to the bytecode kernel;
// it omits the same upper/lower/replace/strip/count/find/startswith/endswith
// chain the kernel drops.
func stringBuildReference(n int32) int32 {
	digits := func(v int32) string {
		if v == 0 {
			return "0"
		}
		var buf []byte
		for v > 0 {
			d := v % 10
			buf = append([]byte{byte(48 + d)}, buf...)
			v /= 10
		}
		return string(buf)
	}

	var big string
	var tokenChecksum int32
	for i := int32(0); i < n; i++ {
		tok := digits(int32(int64(i) * 2654435761 % 99999))
		for j := 0; j < len(tok); j++ {
			tokenChecksum += int32(tok[j]) * int32(j+1)
		}
		tokenChecksum %= 1000000007
		big = big + tok + " "
	}
	return tokenChecksum + int32(len(big))
}

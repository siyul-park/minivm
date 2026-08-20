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

func allocationGraph(depth int32) *program.Program {
	b := program.NewBuilder()
	array := b.Type(types.NewArrayType(types.TypeAny))
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeAny, types.TypeI32)
	b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 1)
	b.Bind(loop)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, uint64(uint32(depth))).Emit(instr.I32_GE_S).BrIf(done)
	b.Emit(instr.I32_CONST, 1).Emit(instr.ARRAY_NEW_DEFAULT, uint64(array)).Emit(instr.DUP)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 0).Emit(instr.ARRAY_SET).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Br(loop)
	b.Bind(done).Emit(instr.LOCAL_GET, 1)
	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

func permutationFlips(size, depth int32) *program.Program {
	arrayType := types.NewArrayType(types.TypeAny)

	// locals: 0=depth (param), 1=arr, 2=i, 3=lo, 4=hi, 5=t
	fb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32).
		Locals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
	base := fb.Label()
	fillLoop := fb.Label()
	fillDone := fb.Label()
	swapLoop := fb.Label()
	swapDone := fb.Label()

	fn := fb.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQ)).
		BrIf(base).
		Emit(
			instr.New(instr.I32_CONST, uint64(uint32(size))), instr.New(instr.ARRAY_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 2),
		).
		Bind(fillLoop).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, uint64(uint32(size))), instr.New(instr.I32_GE_S)).
		BrIf(fillDone).
		Emit(
			// arr[i] = size-1-i
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 2),
			instr.New(instr.I32_CONST, uint64(uint32(size-1))), instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_SUB),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		).
		Br(fillLoop).
		Bind(fillDone).
		Emit(
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.I32_CONST, uint64(uint32(size-1))), instr.New(instr.LOCAL_SET, 4),
		).
		Bind(swapLoop).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_GE_S)).
		BrIf(swapDone).
		Emit(
			// t = arr[lo]
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 3), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 5),
			// arr[lo] = arr[hi]
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 3),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 4), instr.New(instr.ARRAY_GET),
			instr.New(instr.ARRAY_SET),
			// arr[hi] = t
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 5), instr.New(instr.ARRAY_SET),
			// lo++; hi--
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 4),
		).
		Br(swapLoop).
		Bind(swapDone).
		Emit(
			// arr[size-1] + walk(depth-1)
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, uint64(uint32(size-1))), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.RETURN),
		).
		Bind(base).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	return program.New(
		[]instr.Instruction{instr.New(instr.I32_CONST, uint64(uint32(depth))), instr.New(instr.CONST_GET, 0), instr.New(instr.CALL)},
		program.WithConstants(fn),
		program.WithTypes(arrayType),
	)
}

func structTreeWalk(depth int32) *program.Program {
	nodeType := types.NewStructType(
		types.NewStructField(types.TypeI64, types.FieldWithName("value")),
		types.NewStructField(types.TypeAny, types.FieldWithName("left")),
		types.NewStructField(types.TypeAny, types.FieldWithName("right")),
	)

	// build locals: 0=d (param), 1=n
	buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
		Params(types.TypeI32).
		Locals(types.TypeAny)
	buildDone := buildBuilder.Label()
	buildFn := buildBuilder.
		Emit(
			instr.New(instr.STRUCT_NEW_DEFAULT, 0), instr.New(instr.LOCAL_SET, 1),
			// n.value = i64(d)
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_TO_I64_S),
			instr.New(instr.STRUCT_SET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
		).
		BrIf(buildDone).
		Emit(
			// n.left = build(d-1)
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
			// n.right = build(d-1)
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 2),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
		).
		Bind(buildDone).
		Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).
		MustBuild()

	// check locals: 0=node (param)
	checkBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeAny)
	nullCase := checkBuilder.Label()
	checkFn := checkBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
		BrIf(nullCase).
		Emit(
			// m := ref.cast[Node](node); left := m.left; check(left)
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, 0),
			instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
			// right := m.right; check(right)
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
			instr.New(instr.I32_ADD), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.RETURN),
		).
		Bind(nullCase).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()

	return program.New(
		[]instr.Instruction{
			instr.New(instr.I32_CONST, uint64(uint32(depth))),
			instr.New(instr.CONST_GET, 0), instr.New(instr.CALL),
			instr.New(instr.CONST_GET, 1), instr.New(instr.CALL),
		},
		program.WithConstants(buildFn, checkFn),
		program.WithTypes(nodeType),
	)
}

// binaryTrees builds the benchmarks-game binary-trees kernel. bottom_up_tree
// and item_check are each self-recursive, so each reserves its own future
// constant index before its body is built (matching fannkuch's permuteIdx
// pattern) so its own recursive calls can CONST_GET that index.
// STRUCT_NEW_DEFAULT zero-initializes ref fields to the null heap ref, so the
// depth<=0 base case can leave left/right unset instead of writing REF_NULL.
func binaryTrees(minDepth, maxDepth int32) *program.Program {
	b := program.NewBuilder()
	nodeType := b.Type(types.NewStructType(
		types.NewStructField(types.TypeI32, types.FieldWithName("item")),
		types.NewStructField(types.TypeAny, types.FieldWithName("left")),
		types.NewStructField(types.TypeAny, types.FieldWithName("right")),
	))

	bottomUpTreeIdx := 0
	// bottom_up_tree params: 0=item,1=depth; locals: 2=n
	buBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
		Params(types.TypeI32, types.TypeI32).
		Locals(types.TypeAny)
	buDone := buBuilder.Label()
	bottomUpTreeFn := buBuilder.
		Emit(
			instr.New(instr.STRUCT_NEW_DEFAULT, uint64(nodeType)), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.STRUCT_SET),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S),
		).
		BrIf(buDone).
		Emit(
			// n.left = bottom_up_tree(2*item-1, depth-1)
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_MUL),
			instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, uint64(bottomUpTreeIdx)), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
			// n.right = bottom_up_tree(2*item, depth-1)
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 2),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.I32_MUL),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB),
			instr.New(instr.CONST_GET, uint64(bottomUpTreeIdx)), instr.New(instr.CALL),
			instr.New(instr.STRUCT_SET),
		).
		Bind(buDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()
	b.Const(bottomUpTreeFn)

	itemCheckIdx := bottomUpTreeIdx + 1
	// item_check params: 0=t
	icBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeAny)
	nullCase := icBuilder.Label()
	leafCase := icBuilder.Label()
	itemCheckFn := icBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_IS_NULL)).
		BrIf(nullCase).
		Emit(
			// left := ref.cast[Node](t).left; if left is None: return t.item
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.REF_CAST, uint64(nodeType)),
			instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
			instr.New(instr.REF_IS_NULL),
		).
		BrIf(leafCase).
		Emit(
			// t.item + item_check(t.left) - item_check(t.right)
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 1), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, uint64(itemCheckIdx)), instr.New(instr.CALL),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 2), instr.New(instr.STRUCT_GET),
			instr.New(instr.CONST_GET, uint64(itemCheckIdx)), instr.New(instr.CALL),
			instr.New(instr.I32_SUB),
			instr.New(instr.RETURN),
		).
		Bind(leafCase).
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.STRUCT_GET), instr.New(instr.RETURN)).
		Bind(nullCase).
		Emit(instr.New(instr.I32_CONST, 0), instr.New(instr.RETURN)).
		MustBuild()
	b.Const(itemCheckFn)

	// main locals: 0=stretchTree,1=checksum,2=longLivedTree,3=depth,
	// 4=iterations,5=shift,6=acc,7=i,8=t1,9=t2
	b.Locals(
		types.TypeAny, types.TypeI32, types.TypeAny, types.TypeI32, types.TypeI32,
		types.TypeI32, types.TypeI32, types.TypeI32, types.TypeAny, types.TypeAny,
	)

	// stretch_tree = bottom_up_tree(0, max_depth+1); checksum = item_check(stretch_tree)
	b.Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, uint64(uint32(maxDepth+1)))
	b.Emit(instr.CONST_GET, uint64(bottomUpTreeIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.CONST_GET, uint64(itemCheckIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 1)

	// long_lived_tree = bottom_up_tree(0, max_depth)
	b.Emit(instr.I32_CONST, 0).Emit(instr.I32_CONST, uint64(uint32(maxDepth)))
	b.Emit(instr.CONST_GET, uint64(bottomUpTreeIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 2)

	b.Emit(instr.I32_CONST, uint64(uint32(minDepth))).Emit(instr.LOCAL_SET, 3)

	depthLoop := b.Label()
	depthDone := b.Label()
	shiftLoop := b.Label()
	shiftDone := b.Label()
	iLoop := b.Label()
	iDone := b.Label()

	b.Bind(depthLoop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(maxDepth))).Emit(instr.I32_GT_S).BrIf(depthDone)

	// iterations = 1; shift = 0; while shift < max_depth-depth+min_depth: iterations *= 2; shift++
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 5)
	b.Bind(shiftLoop)
	b.Emit(instr.LOCAL_GET, 5)
	b.Emit(instr.I32_CONST, uint64(uint32(maxDepth))).Emit(instr.LOCAL_GET, 3).Emit(instr.I32_SUB).Emit(instr.I32_CONST, uint64(uint32(minDepth))).Emit(instr.I32_ADD)
	b.Emit(instr.I32_GE_S).BrIf(shiftDone)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.I32_CONST, 2).Emit(instr.I32_MUL).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 5).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 5)
	b.Br(shiftLoop)
	b.Bind(shiftDone)

	// acc = 0; i = 1; while i <= iterations: t1=build(i,depth); acc+=check(t1); t2=build(-i,depth); acc+=check(t2); i++
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 6)
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_SET, 7)
	b.Bind(iLoop)
	b.Emit(instr.LOCAL_GET, 7).Emit(instr.LOCAL_GET, 4).Emit(instr.I32_GT_S).BrIf(iDone)

	b.Emit(instr.LOCAL_GET, 7).Emit(instr.LOCAL_GET, 3)
	b.Emit(instr.CONST_GET, uint64(bottomUpTreeIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 8)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 8).Emit(instr.CONST_GET, uint64(itemCheckIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 6)

	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_GET, 7).Emit(instr.I32_SUB).Emit(instr.LOCAL_GET, 3)
	b.Emit(instr.CONST_GET, uint64(bottomUpTreeIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 9)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 9).Emit(instr.CONST_GET, uint64(itemCheckIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 6)

	b.Emit(instr.LOCAL_GET, 7).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 7)
	b.Br(iLoop)
	b.Bind(iDone)

	// checksum += acc; depth += 2
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.LOCAL_GET, 6).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 2).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(depthLoop)
	b.Bind(depthDone)

	// checksum += item_check(long_lived_tree)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.CONST_GET, uint64(itemCheckIdx)).Emit(instr.CALL)
	b.Emit(instr.I32_ADD)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// sortStress builds the sortstress kernel. minivm has no sort opcode, so the
// sort is written directly in bytecode as an insertion sort over the i32
// array: it is a simple, obviously-correct in-place algorithm and the least
// code among the alternatives, keeping the kernel a measure of VM dispatch
// rather than of an algorithm choice. The LCG state overflows i32
// (s*1103515245 can reach ~2.4e18), so make_list keeps s in i64; the sorted
// values (s % 1000000) fit i32, so the array itself stays i32.
func sortStress(n, rounds int32) *program.Program {
	b := program.NewBuilder()
	arrayType := b.Type(types.TypeI32Array)

	// make_list params: 0=n,1=seed; locals: 2=xs,3=s(i64),4=i
	mlBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
		Params(types.TypeI32, types.TypeI32).
		Locals(types.TypeAny, types.TypeI64, types.TypeI32)
	mlLoop := mlBuilder.Label()
	mlDone := mlBuilder.Label()
	makeListFn := mlBuilder.
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.ARRAY_NEW_DEFAULT, uint64(arrayType)), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_TO_I64_S), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 4),
		).
		Bind(mlLoop).
		Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_GE_S)).
		BrIf(mlDone).
		Emit(
			// s = (s*1103515245 + 12345) % 2147483648
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I64_CONST, 1103515245), instr.New(instr.I64_MUL),
			instr.New(instr.I64_CONST, 12345), instr.New(instr.I64_ADD),
			instr.New(instr.I64_CONST, 2147483648), instr.New(instr.I64_REM_S),
			instr.New(instr.LOCAL_SET, 3),
			// xs[i] = i32(s % 1000000)
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I64_CONST, 1000000), instr.New(instr.I64_REM_S), instr.New(instr.I64_TO_I32),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 4),
		).
		Br(mlLoop).
		Bind(mlDone).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.RETURN)).
		MustBuild()
	makeListIdx := b.Const(makeListFn)

	// insertion_sort params: 0=arr,1=n; locals: 2=i,3=key,4=j
	isBuilder := types.NewFunctionBuilder(&types.FunctionType{}).
		Params(types.TypeAny, types.TypeI32).
		Locals(types.TypeI32, types.TypeI32, types.TypeI32)
	outer := isBuilder.Label()
	outerDone := isBuilder.Label()
	inner := isBuilder.Label()
	innerDone := isBuilder.Label()
	insertionSortFn := isBuilder.
		Emit(instr.New(instr.I32_CONST, 1), instr.New(instr.LOCAL_SET, 2)).
		Bind(outer).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_GE_S)).
		BrIf(outerDone).
		Emit(
			// key = arr[i]; j = i-1
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 2), instr.New(instr.ARRAY_GET), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 4),
		).
		Bind(inner).
		Emit(instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LT_S)).
		BrIf(innerDone).
		Emit(
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 4), instr.New(instr.ARRAY_GET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_LE_S),
		).
		BrIf(innerDone).
		Emit(
			// arr[j+1] = arr[j]; j--
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 4), instr.New(instr.ARRAY_GET),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 4),
		).
		Br(inner).
		Bind(innerDone).
		Emit(
			// arr[j+1] = key; i++
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_GET, 3),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 2),
		).
		Br(outer).
		Bind(outerDone).
		Emit(instr.New(instr.RETURN)).
		MustBuild()
	insertionSortIdx := b.Const(insertionSortFn)

	// main locals: 0=xs,1=checksum,2=r,3=i
	b.Locals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeI32)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)

	roundLoop := b.Label()
	roundDone := b.Label()
	sumLoop := b.Label()
	sumDone := b.Label()

	b.Bind(roundLoop)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(rounds))).Emit(instr.I32_GE_S).BrIf(roundDone)

	// xs = make_list(n, seed+r); insertion_sort(xs, n)
	b.Emit(instr.I32_CONST, uint64(uint32(n)))
	b.Emit(instr.I32_CONST, 1).Emit(instr.LOCAL_GET, 2).Emit(instr.I32_ADD)
	b.Emit(instr.CONST_GET, uint64(makeListIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 0)

	b.Emit(instr.LOCAL_GET, 0).Emit(instr.I32_CONST, uint64(uint32(n)))
	b.Emit(instr.CONST_GET, uint64(insertionSortIdx)).Emit(instr.CALL)

	// checksum += sum(xs[i]*(i%7) for i in range(n)); checksum %= 1000000007
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 3)
	b.Bind(sumLoop)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(sumDone)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 7).Emit(instr.I32_REM_S)
	b.Emit(instr.I32_MUL).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 3)
	b.Br(sumLoop)
	b.Bind(sumDone)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1000000007).Emit(instr.I32_REM_S).Emit(instr.LOCAL_SET, 1)

	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(roundLoop)
	b.Bind(roundDone)

	b.Emit(instr.LOCAL_GET, 1)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
}

// stringBuild builds the strbuild kernel: digits(n) token generation, a
// per-character checksum read back through STRING_ENCODE_UTF32, and
// "big = big + tok + ' '" through STRING_CONCAT (the allocating operation
// this kernel exists to measure).
func stringBuild(n int32) *program.Program {
	b := program.NewBuilder()
	charType := b.Type(types.TypeI32Array)

	// digits params: 0=n; locals: 1=count,2=v,3=arr,4=idx,5=d
	dBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeAny}}).
		Params(types.TypeI32).
		Locals(types.TypeI32, types.TypeI32, types.TypeAny, types.TypeI32, types.TypeI32)
	zeroCase := dBuilder.Label()
	countLoop := dBuilder.Label()
	countDone := dBuilder.Label()
	fillLoop := dBuilder.Label()
	fillDone := dBuilder.Label()
	digitsFn := dBuilder.
		Emit(instr.New(instr.LOCAL_GET, 0), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_EQ)).
		BrIf(zeroCase).
		Emit(
			// count = number of decimal digits in n; v walks n down to 0
			instr.New(instr.I32_CONST, 0), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_SET, 2),
		).
		Bind(countLoop).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(countDone).
		Emit(
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_ADD), instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_DIV_S), instr.New(instr.LOCAL_SET, 2),
		).
		Br(countLoop).
		Bind(countDone).
		Emit(
			// arr = new i32[count]; v = n again; idx = count-1
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.ARRAY_NEW_DEFAULT, uint64(charType)), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 0), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 1), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 4),
		).
		Bind(fillLoop).
		Emit(instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_LE_S)).
		BrIf(fillDone).
		Emit(
			// arr[idx] = '0' + (v%10); v /= 10; idx--
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_REM_S), instr.New(instr.LOCAL_SET, 5),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.LOCAL_GET, 4),
			instr.New(instr.LOCAL_GET, 5), instr.New(instr.I32_CONST, 48), instr.New(instr.I32_ADD),
			instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 2), instr.New(instr.I32_CONST, 10), instr.New(instr.I32_DIV_S), instr.New(instr.LOCAL_SET, 2),
			instr.New(instr.LOCAL_GET, 4), instr.New(instr.I32_CONST, 1), instr.New(instr.I32_SUB), instr.New(instr.LOCAL_SET, 4),
		).
		Br(fillLoop).
		Bind(fillDone).
		Emit(instr.New(instr.LOCAL_GET, 3), instr.New(instr.STRING_NEW_UTF32), instr.New(instr.RETURN)).
		Bind(zeroCase).
		Emit(
			instr.New(instr.I32_CONST, 1), instr.New(instr.ARRAY_NEW_DEFAULT, uint64(charType)), instr.New(instr.LOCAL_SET, 3),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.I32_CONST, 0), instr.New(instr.I32_CONST, 48), instr.New(instr.ARRAY_SET),
			instr.New(instr.LOCAL_GET, 3), instr.New(instr.STRING_NEW_UTF32), instr.New(instr.RETURN),
		).
		MustBuild()
	digitsIdx := b.Const(digitsFn)
	spaceIdx := b.Const(types.String(" "))

	// main locals: 0=big,1=tokenChecksum,2=i,3=tok,4=codePoints,5=tokLen,6=j
	b.Locals(types.TypeAny, types.TypeI32, types.TypeI32, types.TypeAny, types.TypeAny, types.TypeI32, types.TypeI32)
	b.ConstGet(types.String("")).Emit(instr.LOCAL_SET, 0)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 2)

	outer := b.Label()
	outerDone := b.Label()
	charLoop := b.Label()
	charDone := b.Label()

	b.Bind(outer)
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, uint64(uint32(n))).Emit(instr.I32_GE_S).BrIf(outerDone)

	// tok = digits(i * 2654435761 % 99999); the multiply overflows i32 for
	// i near this fixture's upper bound, so it runs in i64.
	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_TO_I64_S)
	b.Emit(instr.I64_CONST, 2654435761).Emit(instr.I64_MUL)
	b.Emit(instr.I64_CONST, 99999).Emit(instr.I64_REM_S)
	b.Emit(instr.I64_TO_I32)
	b.Emit(instr.CONST_GET, uint64(digitsIdx)).Emit(instr.CALL)
	b.Emit(instr.LOCAL_SET, 3)

	// token_checksum += sum(ord(tok[j])*(j+1) for j in range(len(tok))) via
	// STRING_ENCODE_UTF32's code-point array; token_checksum %= 1000000007
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.STRING_ENCODE_UTF32).Emit(instr.LOCAL_SET, 4)
	b.Emit(instr.LOCAL_GET, 3).Emit(instr.STRING_LEN).Emit(instr.LOCAL_SET, 5)
	b.Emit(instr.I32_CONST, 0).Emit(instr.LOCAL_SET, 6)
	b.Bind(charLoop)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.LOCAL_GET, 5).Emit(instr.I32_GE_S).BrIf(charDone)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 4).Emit(instr.LOCAL_GET, 6).Emit(instr.ARRAY_GET)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD)
	b.Emit(instr.I32_MUL).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 1)
	b.Emit(instr.LOCAL_GET, 6).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 6)
	b.Br(charLoop)
	b.Bind(charDone)
	b.Emit(instr.LOCAL_GET, 1).Emit(instr.I32_CONST, 1000000007).Emit(instr.I32_REM_S).Emit(instr.LOCAL_SET, 1)

	// big = big + tok + " "
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.LOCAL_GET, 3).Emit(instr.STRING_CONCAT)
	b.Emit(instr.CONST_GET, uint64(spaceIdx)).Emit(instr.STRING_CONCAT)
	b.Emit(instr.LOCAL_SET, 0)

	b.Emit(instr.LOCAL_GET, 2).Emit(instr.I32_CONST, 1).Emit(instr.I32_ADD).Emit(instr.LOCAL_SET, 2)
	b.Br(outer)
	b.Bind(outerDone)

	// return token_checksum + len(big)
	b.Emit(instr.LOCAL_GET, 1)
	b.Emit(instr.LOCAL_GET, 0).Emit(instr.STRING_LEN)
	b.Emit(instr.I32_ADD)

	prog, err := b.Build()
	if err != nil {
		panic(err)
	}
	return prog
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

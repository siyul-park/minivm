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

func allocationGraph(depth int32) *program.Program {
	b := program.NewBuilder()
	array := b.Type(types.NewArrayType(types.TypeRef))
	loop := b.Label()
	done := b.Label()
	b.Locals(types.TypeRef, types.TypeI32)
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
	arrayType := types.NewArrayType(types.TypeRef)

	// locals: 0=depth (param), 1=arr, 2=i, 3=lo, 4=hi, 5=t
	fb := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeI32}}).
		Params(types.TypeI32).
		Locals(types.TypeRef, types.TypeI32, types.TypeI32, types.TypeI32, types.TypeI32)
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
		types.NewStructField(types.TypeRef, types.FieldWithName("left")),
		types.NewStructField(types.TypeRef, types.FieldWithName("right")),
	)

	// build locals: 0=d (param), 1=n
	buildBuilder := types.NewFunctionBuilder(&types.FunctionType{Returns: []types.Type{types.TypeRef}}).
		Params(types.TypeI32).
		Locals(types.TypeRef)
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
		Params(types.TypeRef)
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

func typedArraySumReference(size int32) int32 {
	return size * (size + 1) / 2
}

func permutationFlipsReference(size, depth int32) int32 {
	return depth * (size - 1)
}

func structTreeWalkReference(depth int32) int32 {
	return int32(1<<uint(depth+1)) - 1
}

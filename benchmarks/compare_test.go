//go:build compare

package benchmarks

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func compareIterativeFib(b *testing.B, n, want int32) {
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 {
			var current int32
			next := int32(1)
			for range n {
				current, next = next, current+next
			}
			return current
		}, want)
	})
	benchmarkWazero(b, "iterative_fib", want, uint64(uint32(n)))
	benchmarkScripts(b, compareScripts{
		tengo: fmt.Sprintf(`result := func() {
    current := 0
    next := 1
    for index := 0; index < %d; index++ {
        sum := current + next
        current = next
        next = sum
    }
    return current
}()`, n),
		gopherLua: fmt.Sprintf(`function run()
    local current = 0
    local next = 1
    for _ = 1, %d do
        local sum = current + next
        current = next
        next = sum
    end
    return current
end`, n),
		goja: fmt.Sprintf(`function run() {
    let current = 0;
    let next = 1;
    for (let index = 0; index < %d; index++) {
        const sum = current + next;
        current = next;
        next = sum;
    }
    return current;
}`, n),
	}, nil, want)
}

func compareSieve(b *testing.B, size, want int32) {
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 {
			composite := make([]int32, size)
			for value := int32(2); value*value < size; value++ {
				for multiple := value * value; multiple < size; multiple += value {
					composite[multiple] = 1
				}
			}
			var count int32
			for value := int32(2); value < size; value++ {
				if composite[value] == 0 {
					count++
				}
			}
			return count
		}, want)
	})
	benchmarkWazero(b, "sieve", want, uint64(uint32(size)))
	benchmarkScripts(b, compareScripts{
		tengo: fmt.Sprintf(`result := func() {
    composite := []
    for index := 0; index < %d; index++ { composite = append(composite, 0) }
    for value := 2; value * value < %d; value++ {
        for multiple := value * value; multiple < %d; multiple += value { composite[multiple] = 1 }
    }
    count := 0
    for value := 2; value < %d; value++ { if composite[value] == 0 { count++ } }
    return count
}()`, size, size, size, size),
		gopherLua: fmt.Sprintf(`function run()
    local composite = {}
    for index = 0, %d - 1 do composite[index] = 0 end
    for value = 2, %d - 1 do
        if value * value >= %d then break end
        for multiple = value * value, %d - 1, value do composite[multiple] = 1 end
    end
    local count = 0
    for value = 2, %d - 1 do if composite[value] == 0 then count = count + 1 end end
    return count
end`, size, size, size, size, size),
		goja: fmt.Sprintf(`function run() {
    const composite = new Int32Array(%d);
    for (let value = 2; value * value < %d; value++) {
        for (let multiple = value * value; multiple < %d; multiple += value) composite[multiple] = 1;
    }
    let count = 0;
    for (let value = 2; value < %d; value++) if (composite[value] === 0) count++;
    return count;
}`, size, size, size, size),
	}, nil, want)
}

func compareRecursiveFib(b *testing.B, n, want int32) {
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 { return recursiveFibReference(n) }, want)
	})
	benchmarkWazero(b, "recursive_fib", want, uint64(uint32(n)))
	benchmarkScripts(b, recursiveFibScripts(n, false), nil, want)
}

func compareIndirectRecursiveFib(b *testing.B, n, want int32) {
	b.Run("native", func(b *testing.B) {
		type fib func(int32, fib) int32
		var run fib
		run = func(value int32, self fib) int32 {
			if value < 2 {
				return value
			}
			return self(value-1, self) + self(value-2, self)
		}
		benchmarkNative(b, func() int32 { return run(n, run) }, want)
	})
	benchmarkWazero(b, "indirect_recursive_fib", want, uint64(uint32(n)))
	benchmarkScripts(b, recursiveFibScripts(n, true), nil, want)
}

func compareClosureCounter(b *testing.B, count int, want int32) {
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 {
			var value int32
			next := func() int32 {
				value++
				return value
			}
			for range count {
				value = next()
			}
			return value
		}, want)
	})
	benchmarkScripts(b, compareScripts{
		tengo: fmt.Sprintf(`result := func() {
    value := 0
    next := func() { value++; return value }
    for index := 0; index < %d; index++ { value = next() }
    return value
}()`, count),
		gopherLua: fmt.Sprintf(`function run()
    local value = 0
    local function next() value = value + 1; return value end
    for _ = 1, %d do value = next() end
    return value
end`, count),
		goja: fmt.Sprintf(`function run() {
    let value = 0;
    const next = () => ++value;
    for (let index = 0; index < %d; index++) value = next();
    return value;
}`, count),
	}, nil, want)
}

func compareTypedArraySum(b *testing.B, size, want int32) {
	values := make([]int32, size)
	for index := range values {
		values[index] = int32(index + 1)
	}
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 {
			var total int32
			for _, value := range values {
				total += value
			}
			return total
		}, want)
	})
	benchmarkWazero(b, "typed_array_sum", want, uint64(uint32(size)))
	benchmarkScripts(b, compareScripts{
		tengo: `result := func() {
    total := 0
    for index := 0; index < len(values); index++ { total += values[index] }
    return total
}()`,
		gopherLua: `function run()
    local total = 0
    for index = 1, #values do total = total + values[index] end
    return total
end`,
		goja: `function run() {
    let total = 0;
    for (let index = 0; index < values.length; index++) total += values[index];
    return total;
}`,
	}, values, want)
}

func compareAllocationGraph(b *testing.B, depth, want int32) {
	b.Run("native", func(b *testing.B) {
		type node struct{ next *node }
		var sink *node
		benchmarkNative(b, func() int32 {
			root := &node{}
			for index := int32(1); index < depth; index++ {
				root = &node{next: root}
			}
			sink = root
			return depth
		}, want)
		require.NotNil(b, sink)
	})
	benchmarkScripts(b, compareScripts{
		tengo: fmt.Sprintf(`result := func() {
    root := [undefined]
    for index := 1; index < %d; index++ { root = [root] }
    return %d + len(root) - 1
}()`, depth, depth),
		gopherLua: fmt.Sprintf(`function run()
    local root = {false}
    for _ = 2, %d do root = {root} end
    return %d + #root - 1
end`, depth, depth),
		goja: fmt.Sprintf(`function run() {
    let root = [null];
    for (let index = 1; index < %d; index++) root = [root];
    return %d + root.length - 1;
}`, depth, depth),
	}, nil, want)
}

func compareBranchTree(b *testing.B, input int32, nodes int, want int32) {
	b.Run("native", func(b *testing.B) {
		benchmarkNative(b, func() int32 {
			var total int32
			for index := range nodes {
				threshold := int32((index*17 + 11) % 97)
				if input < threshold {
					total += int32(index%7 + 1)
				} else {
					total += int32(index%5 + 2)
				}
			}
			return total
		}, want)
	})
	benchmarkWazero(b, "branch_tree", want, uint64(uint32(input)), uint64(uint32(nodes)))
	benchmarkScripts(b, compareScripts{
		tengo: fmt.Sprintf(`result := func() {
    total := 0
    for index := 0; index < %d; index++ {
        threshold := (index * 17 + 11) %% 97
        if %d < threshold { total += index %% 7 + 1 } else { total += index %% 5 + 2 }
    }
    return total
}()`, nodes, input),
		gopherLua: fmt.Sprintf(`function run()
    local total = 0
    for index = 0, %d - 1 do
        local threshold = (index * 17 + 11) %% 97
        if %d < threshold then total = total + index %% 7 + 1 else total = total + index %% 5 + 2 end
    end
    return total
end`, nodes, input),
		goja: fmt.Sprintf(`function run() {
    let total = 0;
    for (let index = 0; index < %d; index++) {
        const threshold = (index * 17 + 11) %% 97;
        total += %d < threshold ? index %% 7 + 1 : index %% 5 + 2;
    }
    return total;
}`, nodes, input),
	}, nil, want)
}

func recursiveFibScripts(n int32, indirect bool) compareScripts {
	if indirect {
		return compareScripts{
			tengo: fmt.Sprintf(`fib := func(n, self) {
    if n < 2 { return n }
    return self(n-1, self) + self(n-2, self)
}
result := fib(%d, fib)`, n),
			gopherLua: fmt.Sprintf(`function fib(n, self)
    if n < 2 then return n end
    return self(n - 1, self) + self(n - 2, self)
end
function run() return fib(%d, fib) end`, n),
			goja: fmt.Sprintf(`function fib(n, self) {
    if (n < 2) return n;
    return self(n - 1, self) + self(n - 2, self);
}
function run() { return fib(%d, fib); }`, n),
		}
	}
	return compareScripts{
		tengo: fmt.Sprintf(`fib := func(n) {
    if n < 2 { return n }
    return fib(n-1) + fib(n-2)
}
result := fib(%d)`, n),
		gopherLua: fmt.Sprintf(`function fib(n)
    if n < 2 then return n end
    return fib(n - 1) + fib(n - 2)
end
function run() return fib(%d) end`, n),
		goja: fmt.Sprintf(`function fib(n) {
    if (n < 2) return n;
    return fib(n - 1) + fib(n - 2);
}
function run() { return fib(%d); }`, n),
	}
}

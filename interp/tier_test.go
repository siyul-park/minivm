package interp

import (
	"context"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// TestARM64_StackReserve ties nativeFrameLimit (interp's private native
// call-depth cap) to the arm64 invoke trampoline's hard-coded stack reserve
// and total frame size in abi_arm64.s. arm64.StackReserve and arm64.FrameSize
// own the byte arithmetic — one 64-bit spill slot per asm.MaxSpillSlots plus
// one journal frame record per native call-depth level, plus the
// callee-saved save area — so nativeFrameLimit changing without
// abi_arm64.s keeping pace fails this test instead of the mismatch
// surfacing as a corrupted native stack at runtime. arm64.TestFrameSize
// (internal/asm/arm64/stack_test.go) carries the complementary half of the
// invariant: that abi_arm64.s's own two literals are consistent with each
// other independent of nativeFrameLimit, a check this package cannot make
// since it owns neither literal. See docs/jit-internals.md for the full
// explanation.
func TestARM64_StackReserve(t *testing.T) {
	reserve := arm64.StackReserve(1<<journal.Shift, nativeFrameLimit)
	frame := arm64.FrameSize(1<<journal.Shift, nativeFrameLimit)

	_, thisFile, _, ok := runtime.Caller(0)
	require.True(t, ok)
	abiFile := filepath.Join(filepath.Dir(thisFile), "..", "internal", "asm", "arm64", "abi_arm64.s")
	src, err := os.ReadFile(abiFile)
	require.NoError(t, err)

	reserveLiteral := regexp.MustCompile(`ADD\s+\$(\d+),\s*RSP`).FindSubmatch(src)
	require.NotNil(t, reserveLiteral, "expected an ADD $N, RSP reserve instruction in %s", abiFile)
	reserveVal, err := strconv.Atoi(string(reserveLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, reserveVal, reserve,
		"arm64.StackReserve(1<<journal.Shift, nativeFrameLimit) must equal the trampoline's ADD $N, RSP reserve")

	frameLiteral := regexp.MustCompile(`TEXT ·invoke\(SB\), \$(\d+)-`).FindSubmatch(src)
	require.NotNil(t, frameLiteral, "expected a TEXT ·invoke(SB), $N-M frame size in %s", abiFile)
	frameVal, err := strconv.Atoi(string(frameLiteral[1]))
	require.NoError(t, err)
	require.Equal(t, frameVal, frame,
		"arm64.FrameSize(1<<journal.Shift, nativeFrameLimit) must equal the trampoline's TEXT frame size")
}

// Backedge covers when a module loop is attempted for compilation, which
// interp records only as private install bookkeeping: whether the root was
// already tried and whether an entry is installed. No profiling metric
// separates "attempted, not installed" from "not attempted", so the claim has
// no public observable.
//
// Exception (docs/coding-patterns.md §1.1): the symbols it reads (`tried`,
// `exits`, `tracer.headers`) are owned by interp/interp.go and interp/trace.go,
// not by the ARM64 backend that lives in internal/jit/arm64 — this bookkeeping
// stays interp-private regardless of which architecture compiles the trace, so
// it has no home outside this package.
func TestARM64_Backedge(t *testing.T) {
	if runtime.GOARCH != "arm64" {
		t.Skip("native JIT is only available on arm64")
	}

	tests := []struct {
		name      string
		limit     int32
		threshold int
		attempted []bool
		installed bool
	}{
		{name: "compiles module loop", limit: 64, threshold: 8, attempted: []bool{true}, installed: true},
		{name: "warms loop across runs", limit: 4, threshold: 3, attempted: []bool{false, true}},
		{name: "keeps hot threshold", limit: 4, threshold: 64, attempted: []bool{false, false}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := program.NewBuilder()
			loop := b.Label()
			done := b.Label()
			b.Locals(types.TypeI32)
			b.Emit(instr.I32_CONST, 0).
				Emit(instr.LOCAL_SET, 0).
				Bind(loop).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, uint64(uint32(tt.limit))).
				Emit(instr.I32_GE_S).
				BrIf(done).
				Emit(instr.LOCAL_GET, 0).
				Emit(instr.I32_CONST, 1).
				Emit(instr.I32_ADD).
				Emit(instr.LOCAL_SET, 0).
				Br(loop).
				Bind(done).
				Emit(instr.LOCAL_GET, 0)
			prog, err := b.Build()
			require.NoError(t, err)

			i := New(prog, WithTick(1<<20), WithThreshold(tt.threshold))
			defer i.Close()
			headers := i.tracer.headers(i, 0)
			require.NotEmpty(t, headers)
			root := jit.Anchor{IP: headers[0].header}

			for run, attempted := range tt.attempted {
				require.NoError(t, i.Run(context.Background()))
				value, err := i.PopBoxed()
				require.NoError(t, err)
				require.Equal(t, types.BoxI32(tt.limit), value)
				require.Equal(t, attempted, i.tried[root])
				if run+1 < len(tt.attempted) {
					i.Reset()
				}
			}
			if tt.installed {
				require.NotEmpty(t, i.exits)
			}
		})
	}
}

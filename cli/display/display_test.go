package display

import (
	"bytes"
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/interp"
	"github.com/siyul-park/minivm/program"
	"github.com/stretchr/testify/require"
)

func TestStack(t *testing.T) {
	t.Run("empty stack produces no output", func(t *testing.T) {
		vm := interp.New(program.New(nil))
		defer vm.Close()
		var out bytes.Buffer
		Stack(&out, vm)
		require.Empty(t, out.String())
	})

	t.Run("renders top-down with one trailing newline", func(t *testing.T) {
		vm := interp.New(program.New([]instr.Instruction{
			instr.New(instr.I32_CONST, 10),
			instr.New(instr.I32_CONST, 20),
		}))
		defer vm.Close()
		require.NoError(t, vm.Run(t.Context()))

		var out bytes.Buffer
		Stack(&out, vm)
		require.Equal(t, "10 20\n", out.String())
	})
}

func TestValue(t *testing.T) {
	vm := interp.New(program.New(nil))
	defer vm.Close()

	t.Run("i32 has no type suffix", func(t *testing.T) {
		vm := interp.New(program.New([]instr.Instruction{instr.New(instr.I32_CONST, 42)}))
		defer vm.Close()
		require.NoError(t, vm.Run(t.Context()))
		v, err := vm.Peek(0)
		require.NoError(t, err)
		require.Equal(t, "42", Value(v, vm))
	})

	t.Run("i64 carries (i64) suffix", func(t *testing.T) {
		vm := interp.New(program.New([]instr.Instruction{instr.New(instr.I64_CONST, 42)}))
		defer vm.Close()
		require.NoError(t, vm.Run(t.Context()))
		v, err := vm.Peek(0)
		require.NoError(t, err)
		require.Equal(t, "42 (i64)", Value(v, vm))
	})
}

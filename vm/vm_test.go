package vm

import (
	"testing"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

func TestVM_Run(t *testing.T) {
	tests := []struct {
		program *program.Program
		values  []types.Value
	}{
		{
			program: program.New(instr.New(instr.NOP)),
			values:  nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.program.String(), func(t *testing.T) {
			vm := New(tt.program)
			err := vm.Run()
			require.NoError(t, err)

			for _, val := range tt.values {
				v, err := vm.Pop()
				require.NoError(t, err)
				require.Equal(t, val, v)
			}
		})
	}
}

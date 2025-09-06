package instr

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypeOf(t *testing.T) {
	typ := TypeOf(NOP)
	require.Equal(t, types[NOP], typ)
}

func TestType_Size(t *testing.T) {
	typ := TypeOf(NOP)
	require.Equal(t, 1, typ.Size())
}

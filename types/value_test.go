package types

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsNull(t *testing.T) {
	tests := []struct {
		val  Value
		want bool
	}{
		{Null, true},
		{BoxedNull, true},
		{Ref(1), false},
		{BoxRef(1), false},
		{I32(0), false},
		{I32Array{0}, false},
		{nil, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprint(tt.val), func(t *testing.T) {
			require.Equal(t, tt.want, IsNull(tt.val))
		})
	}
}

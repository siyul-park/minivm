package testenv_test

import (
	"fmt"
	"testing"

	"github.com/siyul-park/minivm/internal/testenv"
	"github.com/stretchr/testify/require"
)

func TestGuarded(t *testing.T) {
	for _, n := range []int{0, 1, 5, 5000} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			data := testenv.Guarded(t, n)
			require.Len(t, data, n)
			for i := range data {
				data[i] = byte(i)
			}
			if n > 0 {
				require.Equal(t, byte(n-1), data[n-1])
			}
		})
	}
}

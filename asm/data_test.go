package asm_test

import (
	"sync"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"

	"github.com/siyul-park/minivm/asm"
)

func TestData_Alloc(t *testing.T) {
	t.Run("returns distinct, stable slot addresses", func(t *testing.T) {
		d, err := asm.NewData(64)
		require.NoError(t, err)
		defer d.Free()

		a, err := d.Alloc()
		require.NoError(t, err)
		b, err := d.Alloc()
		require.NoError(t, err)
		require.NotEqual(t, a, b)
	})

	t.Run("stores and loads pointers", func(t *testing.T) {
		d, err := asm.NewData(64)
		require.NoError(t, err)
		defer d.Free()

		slot, err := d.Alloc()
		require.NoError(t, err)

		target := 42
		want := unsafe.Pointer(&target)
		d.Set(slot, want)
		require.Equal(t, want, d.Load(slot))
	})

	t.Run("survives concurrent stores", func(t *testing.T) {
		d, err := asm.NewData(64)
		require.NoError(t, err)
		defer d.Free()

		slot, err := d.Alloc()
		require.NoError(t, err)

		targets := make([]int, 64)
		var wg sync.WaitGroup
		for i := range targets {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				d.Set(slot, unsafe.Pointer(&targets[i]))
			}(i)
		}
		wg.Wait()

		got := d.Load(slot)
		require.NotNil(t, got)
	})
}

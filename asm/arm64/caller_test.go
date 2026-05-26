//go:build arm64

package arm64

import (
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/stretchr/testify/require"
)

func TestNewCaller(t *testing.T) {
	t.Run("basic", func(t *testing.T) {
		buf, err := asm.NewBuffer(64)
		require.NoError(t, err)
		defer buf.Free()

		chk, err := buf.Append([]byte{
			0x00, 0x00, 0x00, 0x8B, // ADD X0, X0, X0
			0xC0, 0x03, 0x5F, 0xD6, // RET
		})
		require.NoError(t, err)
		require.NoError(t, buf.Seal())

		sig := &asm.Signature{
			Params: []asm.PReg{asm.NewPReg(0, asm.RegTypeInt, asm.Width64)},
			Returns: map[int][]asm.PReg{
				0: {asm.NewPReg(0, asm.RegTypeInt, asm.Width64)},
			},
		}

		c, err := NewCaller(sig, chk)
		require.NoError(t, err)

		rets, err := c.Call([]asm.Value{asm.I64(1)}, nil)
		require.NoError(t, err)
		require.Len(t, rets, 1)
		require.Equal(t, []asm.Value{asm.I64(2)}, rets)
	})

	t.Run("reserved input output", func(t *testing.T) {
		buf, err := asm.NewBuffer(128)
		require.NoError(t, err)
		defer buf.Free()

		a := asm.NewAssembler(Arch, buf)
		stack := a.Scratch()
		heap := a.Scratch()
		next := a.Scratch()
		a.Emit(ADD(next, stack, heap))
		a.Emit(RET())

		obj, err := a.Compile()
		require.NoError(t, err)
		callers, err := a.Link([]*asm.RelocObject{obj})
		require.NoError(t, err)
		require.Len(t, callers, 1)

		rsv := []uint64{11, 31, 0}
		_, err = callers[0].Call(nil, &rsv)
		require.NoError(t, err)
		require.Equal(t, uint64(42), rsv[2])
	})

	t.Run("uses updated header", func(t *testing.T) {
		buf, err := asm.NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()

		a := asm.NewAssembler(Arch, buf)
		a.Emits(LDI(X15, Header(nil, []asm.PReg{X0, X1}, 0))...)
		a.Emits(LDI(X0, 11)...)
		a.Emits(LDI(X1, 31)...)
		a.Emit(RET())
		obj, err := a.Compile()
		require.NoError(t, err)

		sig := &asm.Signature{
			Returns: map[int][]asm.PReg{
				0: {X0},
				1: {X0, X1},
			},
		}
		c, err := NewCaller(sig, obj.Chunk)
		require.NoError(t, err)

		out, err := c.Call(nil, nil)
		require.NoError(t, err)
		require.Equal(t, []asm.Value{asm.I64(11), asm.I64(31)}, out)
	})

	t.Run("defaults to site zero output layout", func(t *testing.T) {
		buf, err := asm.NewBuffer(256)
		require.NoError(t, err)
		defer buf.Free()

		a := asm.NewAssembler(Arch, buf)
		a.Emits(LDI(X0, 11)...)
		a.Emit(RET())
		obj, err := a.Compile()
		require.NoError(t, err)

		sig := &asm.Signature{
			Returns: map[int][]asm.PReg{
				0: {X0},
				1: {D0},
			},
		}
		c, err := NewCaller(sig, obj.Chunk)
		require.NoError(t, err)

		out, err := c.Call(nil, nil)
		require.NoError(t, err)
		require.Equal(t, []asm.Value{asm.I64(11)}, out)
	})

	t.Run("reuses executable buffer", func(t *testing.T) {
		buf, err := asm.NewBuffer(8192)
		require.NoError(t, err)
		defer buf.Free()

		a := asm.NewAssembler(Arch, buf)
		for n := range 64 {
			out := a.NewVReg(asm.RegTypeInt, asm.Width64)
			require.NoError(t, a.Pin(out, X0))

			a.Emits(LDI(out, uint64(n))...)
			idx := a.Index()
			a.Site(idx, []asm.VReg{out})
			a.Emit(RET())

			obj, err := a.Compile()
			require.NoError(t, err)

			callers, err := a.Link([]*asm.RelocObject{obj})
			require.NoError(t, err)
			require.Len(t, callers, 1)
			require.NotNil(t, callers[0])

			values, err := callers[0].Call(nil, nil)
			require.NoError(t, err)
			require.Equal(t, []asm.Value{asm.I64(uint64(n))}, values)
		}
	})
}

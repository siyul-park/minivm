//go:build arm64

package interp

import (
	"context"
	"testing"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/asm/arm64"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/program"
	"github.com/siyul-park/minivm/types"
	"github.com/stretchr/testify/require"
)

// extJITDouble doubles the top i32. When jit is set, Lower emits a native ADD;
// when dirty is set, Lower mutates the Emitter and then declines.
type extJITDouble struct {
	jit   bool
	dirty bool
}

func (extJITDouble) Types() []instr.Type {
	return []instr.Type{{Mnemonic: "j.double", Widths: nil}}
}

func (extJITDouble) Compile(instr.Instruction) func(*Interpreter) error {
	return func(i *Interpreter) error {
		v, err := i.Pop()
		if err != nil {
			return err
		}
		n, ok := v.(types.I32)
		if !ok {
			return ErrTypeMismatch
		}
		return i.Push(types.I32(n * 2))
	}
}

func (x extJITDouble) Lower(inst instr.Instruction, e *Emitter) bool {
	if x.dirty {
		reg, kind, raw := e.Pop()
		if kind != types.KindI32 || !raw {
			return false
		}
		dst := e.Reg(asm.RegTypeInt, asm.Width64)
		e.Emit(arm64.ADD(dst, reg, reg))
		e.Push(dst, types.KindI32)
		return false
	}
	if !x.jit {
		return false
	}
	reg, kind, raw := e.Pop()
	if kind != types.KindI32 || !raw {
		return false
	}
	dst := e.Reg(asm.RegTypeInt, asm.Width64)
	e.Emit(arm64.ADD(dst, reg, reg))
	e.Push(dst, types.KindI32)
	return true
}

func TestExtension_Lower(t *testing.T) {
	requireJIT(t)

	t.Run("lower emits native", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extJITDouble{jit: true})
		loop := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32, types.TypeI32)
		header := loop.Label()
		exit := loop.Label()
		fn := loop.Emit(
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.EXT, uint64(id)<<8, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		threaded, _ := New(prog, WithRegistry(r), WithThreshold(-1))
		defer threaded.Close()
		require.NoError(t, threaded.Run(context.Background()))
		want, err := threaded.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(600), want)

		jit, _ := New(prog, WithRegistry(r), WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Greater(t, jit.local.Value("vm_jit_emits_total"), float64(0))
	})

	t.Run("declined lower deopts cleanly", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extJITDouble{jit: false})
		loop := types.NewFunctionBuilder(&types.FunctionType{
			Returns: []types.Type{types.TypeI32},
		}).WithLocals(types.TypeI32, types.TypeI32)
		header := loop.Label()
		exit := loop.Label()
		fn := loop.Emit(
			instr.New(instr.I32_CONST, 300),
			instr.New(instr.LOCAL_SET, 0),
			instr.New(instr.I32_CONST, 0),
			instr.New(instr.LOCAL_SET, 1),
		).Bind(header).Emit(
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_EQZ),
		).BrIf(exit).Emit(
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.EXT, uint64(id)<<8, 0),
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.I32_ADD),
			instr.New(instr.LOCAL_SET, 1),
			instr.New(instr.LOCAL_GET, 0),
			instr.New(instr.I32_CONST, 1),
			instr.New(instr.I32_SUB),
			instr.New(instr.LOCAL_SET, 0),
		).Br(header).Bind(exit).Emit(
			instr.New(instr.LOCAL_GET, 1),
			instr.New(instr.RETURN),
		).MustBuild()
		prog := program.New([]instr.Instruction{
			instr.New(instr.CONST_GET, 0),
			instr.New(instr.CALL),
		}, program.WithConstants(fn))

		jit, _ := New(prog, WithRegistry(r), WithTick(1), WithThreshold(1))
		defer jit.Close()
		require.NoError(t, jit.Run(context.Background()))
		got, err := jit.Pop()
		require.NoError(t, err)
		require.Equal(t, types.I32(600), got)
	})

	t.Run("declined lower rolls back emitter changes", func(t *testing.T) {
		r := NewRegistry()
		id := r.Register(extJITDouble{dirty: true})
		inst := instr.New(instr.EXT, uint64(id)<<8, 0)
		asmb := asm.New(arm64.New())
		reg := asmb.Reg(asm.RegTypeInt, asm.Width64)
		ctx := &lowering{
			assembler: asmb,
			exts:      r.exts,
			scratch:   []asm.PReg{arm64.X10, arm64.X11, arm64.X12, arm64.X13, arm64.X14},
			values:    []value{{reg: reg, kind: types.KindI32, raw: true}},
			frames: []activation{{
				code: []byte(inst),
			}},
		}

		require.True(t, arm64Lowerer{}.walk(ctx, []step{{op: instr.EXT, ip: 0}}))
		require.Len(t, ctx.values, 1)
		require.Equal(t, reg, ctx.values[0].reg)
		require.Equal(t, types.KindI32, ctx.values[0].kind)
		require.True(t, ctx.values[0].raw)
	})
}

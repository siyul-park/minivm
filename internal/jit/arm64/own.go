package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
)

// retain lowers OpRetain: the count a second owner of a reference takes. It
// cannot fail and cannot free anything, which is why the operation carries no
// state to leave through.
func (e *emitter) retain(op ssa.Operation) bool {
	if len(op.Args) != 1 || e.c.Func().Type(op.Args[0]) != ssa.TypeRef {
		return false
	}
	e.count(e.c.Reg(op.Args[0]), e.pin(scratchCtrl))
	return true
}

// release lowers OpRelease: the count one owner drops. Only a count the cell
// survives is dropped here. The last one frees it, and freeing walks the value
// for the references it held and releases each in turn, allocating, finalizing,
// and reclaiming as it goes - work that needs the interpreter, because a native
// frame carries no unwind metadata and never leaves invoke's pre-checked stack
// reserve (see docs/jit-internals.md). That is the state OpRelease carries and
// OpRetain has none of.
//
// The exit is reserved, and the count read, before anything is written, so a
// release that declines has emitted no decrement. The reverse - a decrement
// already emitted and a later exit resuming where the interpreter would redo
// it, dropping the same count twice - is what the spent state refuses.
func (e *emitter) release(op ssa.Operation) bool {
	if len(op.Args) != 1 || e.c.Func().Type(op.Args[0]) != ssa.TypeRef {
		return false
	}
	fail, ok := e.exit(op.State, prof.ExitGuardValue)
	if !ok {
		return false
	}
	e.spent = append(e.spent, op.State)
	e.drop(e.c.Reg(op.Args[0]), e.pin(scratchCtrl), fail)
	return true
}

// count takes one reference count on the cell boxed names. It skips a null
// reference: Interpreter.releaseBox drops nothing for cell zero, so a count
// taken here would have no matching release.
func (e *emitter) count(boxed, ctrl asm.VReg) {
	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	done := e.a.Label()
	e.a.Emit(
		arm64.ANDI(addr, boxed, maskI32),
		arm64.CMPI(addr, 0),
		arm64.BCondLabel(arm64.OpBEQ, done),
	)
	base := e.a.Reg(asm.RegTypeInt, asm.Width64)
	rc := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(base, ctrl, int16(journal.CellRC*8)),
		arm64.LDRR(rc, base, addr),
		arm64.ADDI(rc, rc, 1),
		arm64.STRR(rc, base, addr),
	)
	e.a.Bind(done)
}

// drop decrements the reference count held by the cell boxed names, exiting
// to fail rather than freeing it when the count would reach zero - freeing
// needs the interpreter, for the reason release's own doc explains - and
// does nothing for a null reference, mirroring count's own null skip.
//
// release and store's own overwritten-slot release both need exactly this
// guarded decrement, one on an ssa.Value argument's register and the other on
// a register a slot's word was loaded into, so this is the shape they share
// rather than each duplicating.
func (e *emitter) drop(boxed, ctrl asm.VReg, fail asm.Label) {
	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	done := e.a.Label()
	e.a.Emit(
		arm64.ANDI(addr, boxed, maskI32),
		arm64.CMPI(addr, 0),
		arm64.BCondLabel(arm64.OpBEQ, done),
	)
	base := e.a.Reg(asm.RegTypeInt, asm.Width64)
	rc := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(base, ctrl, int16(journal.CellRC*8)),
		arm64.LDRR(rc, base, addr),
		arm64.CMPI(rc, 1),
		arm64.BCondLabel(arm64.OpBLE, fail),
		arm64.SUBI(rc, rc, 1),
		arm64.STRR(rc, base, addr),
	)
	e.a.Bind(done)
}

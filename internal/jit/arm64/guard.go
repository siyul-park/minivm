package arm64

import (
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// stub pairs one guard's mismatch label with the deopt state Leave resumes
// it into.
type stub struct {
	label asm.Label
	deopt backend.Deopt
}

// fused reports whether get is the array or struct field read guard admits:
// get pops exactly the container guard produced and an i32 index, and pushes
// one result. read, structRead, and hostRead each check this before their own
// container-specific shape, because a guard whose next operation is not one
// of these would otherwise satisfy every check the rest of them make (see
// emit.go's Lower).
func (e *emitter) fused(guard, get ssa.Operation) bool {
	if len(get.Args) != 2 || len(get.Results) != 1 {
		return false
	}
	if len(guard.Results) != 1 || guard.Results[0] != get.Args[0] {
		return false
	}
	return e.lanes(ssa.TypeI32, get.Args[1])
}

// guardI64 admits only a slot-loaded i64 word whose runtime tag is still
// inline, and refines it into the raw sign-extended i64 view every consumer
// this machine emits for one expects. It is what frontend/walk.go's load
// pairs with an i64-typed OpLoad, because the loaded word may equally be a
// reference to a value Interpreter.boxI64 heap-promoted, and only the tag
// says which. It shares cell's own tag test (see admit) rather than
// reopening it, and the sign-extract mirrors deopt.go's sign64: a value that
// loads successfully is boxable by construction, so nothing after this
// re-checks its range.
func (e *emitter) guardI64(op ssa.Operation) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	if e.c.Func().Type(op.Args[0]) != ssa.TypeI64 || e.c.Func().Type(op.Results[0]) != ssa.TypeI64 {
		return false
	}
	fail, ok := e.exit(op.State, prof.ExitGuardKind)
	if !ok {
		return false
	}
	src := e.c.Reg(op.Args[0])
	dst := e.c.Reg(op.Results[0])
	tag := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LSRI(tag, src, uint8(types.VBits)))
	e.admit(tag, uint64(types.Tag(types.KindI64))>>types.VBits, fail)
	e.a.Emit(arm64.SBFX(dst, src, 0, types.VBits))
	return true
}

// boxable rejects results outside the inline i64 range through the operation's state.
// The state captures the pre-op operands, which remain boxable by construction.
func (e *emitter) boxable(state ssa.Value, v asm.VReg) bool {
	fail, ok := e.exit(state, prof.ExitGuardValue)
	if !ok {
		return false
	}
	ext := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.SBFX(ext, v, 0, boxableWidth))
	e.a.Emit(arm64.CMP(ext, v))
	e.a.Emit(arm64.BCondLabel(arm64.OpBNE, fail))
	return true
}

// guard admits only a container whose runtime type is the shape this compile
// was specialized against, and returns the heap cell it walked the reference
// to, which is what the read behind it loads through instead of resolving the
// same cell again.
//
// The plan pipeline writes the same rule once per access: guardHeap proves
// the operand is a reference and walks it to its cell, guardItab proves the
// cell's concrete type, and a struct access compares the type pointer as
// well. Here it is one operation, because the IR already names it one - and
// it runs before the access it admits, which is what the mismatch has to
// happen before (see docs/jit-internals.md, Speculation).
//
// Itab is all guard proves: a struct or host container names more in its
// Shape (Typ, Host), but each is a check the container's own concrete type
// does not decide by itself - a struct's Typ pointer and a host field's Go
// kind - so structRead and hostRead prove them, reusing the fail label this
// returns rather than opening a second ExitGuardShape for the same mismatch
// reason heap.go's structGet reports through one.
func (e *emitter) guard(op ssa.Operation) (asm.VReg, asm.Label, bool) {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return asm.VReg{}, 0, false
	}
	if e.c.Func().Type(op.Args[0]) != ssa.TypeRef || e.c.Func().Type(op.Results[0]) != ssa.TypeRef {
		return asm.VReg{}, 0, false
	}
	if op.Shape.Itab == 0 {
		return asm.VReg{}, 0, false
	}
	fail, ok := e.exit(op.State, prof.ExitGuardShape)
	if !ok {
		return asm.VReg{}, 0, false
	}
	ref := e.c.Reg(op.Args[0])
	data := e.cell(ref, op.Shape.Itab, fail)
	e.a.Emit(arm64.MOV(e.c.Reg(op.Results[0]), ref))
	return data, fail, true
}

// cell walks a reference to the heap cell it names, admits only the concrete
// type want, and returns the cell's data word. The tag is tested before the
// payload is used as an index because a value the IR types ref is a boxed
// word at runtime, and a container slot's null is not always the boxed-null
// bit pattern (see docs/memory-model.md).
func (e *emitter) cell(ref asm.VReg, want uintptr, fail asm.Label) asm.VReg {
	tag := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LSRI(tag, ref, uint8(types.VBits)))
	e.admit(tag, tagRef>>types.VBits, fail)

	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	heap := e.a.Reg(asm.RegTypeInt, asm.Width64)
	off := e.a.Reg(asm.RegTypeInt, asm.Width64)
	cell := e.a.Reg(asm.RegTypeInt, asm.Width64)
	itab := e.a.Reg(asm.RegTypeInt, asm.Width64)
	data := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.ANDI(addr, ref, maskI32),
		arm64.LDR(heap, e.pin(scratchCtrl), int16(journal.CellHeap*8)),
		arm64.LSLI(off, addr, 4),
		arm64.ADD(cell, heap, off),
		arm64.LDR(itab, cell, 0),
		arm64.LDR(data, cell, 8),
	)
	e.admit(itab, uint64(want), fail)
	return data
}

func (e *emitter) admit(got asm.VReg, want uint64, fail asm.Label) {
	reg := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(reg, want)...)
	e.a.Emit(arm64.CMP(got, reg), arm64.BCondLabel(arm64.OpBNE, fail))
}

// exit reserves the cold path a mismatch leaves through and returns the label
// to branch to. state is the interpreter state the IR says the failing
// operation resumes into; a guard and the access it admits share one, which
// is what lets a bounds test inside a read exit through the state its own
// guard carries.
//
// Everything the stub cannot write is refused here rather than half-emitted:
// exactly one frame, because a deeper chain is a callee a frontend inlined,
// which this machine declines with the call that opened it, and every
// coordinate within the range its immediate encodes.
func (e *emitter) exit(state ssa.Value, reason prof.ExitReason) (asm.Label, bool) {
	d := e.c.Exit(state, reason, e.opcode(state))
	if len(d.Frames) != 1 || !addressable(d.SP) || !addressable(d.Frames[0].BP) {
		return 0, false
	}
	for _, flush := range d.Slots {
		if !addressable(flush.Slot) {
			return 0, false
		}
	}
	label := e.a.Label()
	e.stubs = append(e.stubs, stub{label: label, deopt: d})
	return label, true
}

// addressable reports whether a VM stack coordinate fits the immediates a
// stub writes it through: a load-store offset scaled by eight, and an add.
func addressable(slot int) bool {
	return slot >= 0 && slot <= maxSlot
}

func (e *emitter) opcode(state ssa.Value) int {
	op, ok := e.c.Def(state)
	if !ok || op.Op != ssa.OpState || len(op.Frames) == 0 {
		return prof.OpcodeNone
	}
	frame := op.Frames[len(op.Frames)-1]
	fn := e.c.Input().Objects.Function(frame.Addr)
	if fn == nil || frame.IP < 0 || frame.IP >= len(fn.Code) {
		return prof.OpcodeNone
	}
	return int(fn.Code[frame.IP])
}

// unwind emits one cold stub. The interpreter reads its whole resume state
// out of the VM stack and the journal, so every value the hot path kept in a
// register is written back boxed here, the stack pointer and the frame chain
// are published, and the trap names where threaded dispatch picks up.
//
// A flushed reference the interpreter does not already own is retained here
// as well. The interpreter adopts every stack entry it resumes with and
// releases it, so an entry deriving its count from storage this path is about
// to leave behind would be released once more than it was retained; Owned is
// the IR's own answer to which entries those are (see
// docs/jit-internals.md, Reference Ownership).
func (e *emitter) unwind(d backend.Deopt) bool {
	ctrl := e.pin(scratchCtrl)
	for _, flush := range d.Slots {
		boxed, ok := e.box(flush.Value)
		if !ok {
			return false
		}
		e.a.Emit(arm64.STR(boxed, e.base, int16(flush.Slot*8)))
		if !flush.Owned && e.c.Func().Type(flush.Value) == ssa.TypeRef {
			e.retain(boxed, ctrl)
		}
	}

	bp := e.pin(scratchBP)
	sp := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.ADDI(sp, bp, uint16(d.SP)),
		arm64.STR(sp, ctrl, int16(journal.CellSP*8)),
	)
	for n, frame := range d.Frames {
		e.record(n, frame, ctrl, bp)
	}
	e.publish(ctrl, journal.CellDepth, uint64(len(d.Frames)))
	e.publish(ctrl, journal.CellExitID, uint64(d.ID+1))
	e.publish(ctrl, journal.CellTrap, uint64(journal.TrapFallback))
	e.publish(ctrl, journal.CellNextIP, uint64(d.Resume))
	e.a.Emit(arm64.RET())
	return true
}

// record writes one journal frame record. The interpreter rebuilds the chain
// innermost first, which is the order Deopt.Frames already holds it in, and
// every base is a delta from the entry frame's own.
func (e *emitter) record(n int, r backend.Record, ctrl, bp asm.VReg) {
	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(addr, uint64(r.Addr))...)
	if r.BP != 0 {
		shifted := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.ADDI(shifted, bp, uint16(r.BP)))
		bp = shifted
	}
	e.a.Emit(arm64.STP(addr, bp, ctrl, int16(journal.At(n, journal.RecordAddr)*8)))

	ip := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(ip, uint64(r.IP))...)
	returns := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(returns, uint64(r.Returns))...)
	e.a.Emit(arm64.STP(ip, returns, ctrl, int16(journal.At(n, journal.RecordIP)*8)))
}

func (e *emitter) publish(ctrl asm.VReg, at journal.Cell, word uint64) {
	reg := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDI(reg, word)...)
	e.a.Emit(arm64.STR(reg, ctrl, int16(at*8)))
}

// retain skips a null reference: Interpreter.releaseBox drops nothing for
// cell zero, so a count taken here would have no matching release.
func (e *emitter) retain(boxed, ctrl asm.VReg) {
	addr := e.a.Reg(asm.RegTypeInt, asm.Width64)
	done := e.a.Label()
	e.a.Emit(
		arm64.ANDI(addr, boxed, maskI32),
		arm64.CMPI(addr, 0),
		arm64.BCondLabel(arm64.OpBEQ, done),
	)
	base := e.a.Reg(asm.RegTypeInt, asm.Width64)
	count := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(base, ctrl, int16(journal.CellRC*8)),
		arm64.LDRR(count, base, addr),
		arm64.ADDI(count, count, 1),
		arm64.STRR(count, base, addr),
	)
	e.a.Bind(done)
}

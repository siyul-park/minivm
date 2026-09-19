package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/prof"
	"github.com/siyul-park/minivm/types"
)

// machine is the ARM64 SSA machine: what this target lowers, and the emitter
// it opens for one compile. It holds the pinned journal registers and nothing
// a compile mutates, so one machine serves concurrent compiles.
//
// What it lowers is narrow while the port runs; see Lowers for the set. A
// root needing anything outside it compiles through jit.Compiler's plan
// pipeline instead.
type machine struct {
	scratch []asm.PReg
}

// emitter emits one function's ARM64 code from SSA. It holds nothing shared
// across compiles; anything not named on the struct - a value's register and
// type, a block's label, the journal words a deopt writes - it asks
// backend.Compiler for.
type emitter struct {
	c       *backend.Compiler
	a       *asm.Assembler
	scratch []asm.PReg

	kind    jit.EntryKind
	locals  int
	returns int

	// recursive marks a function selfCall reaches through its own BL, which
	// switches addr from base's single cached derivation to a fresh one on
	// every read (see addr). Set once, from Enter, before any block lowers.
	recursive bool

	// base is the frame base every slot is addressed through for the whole
	// compile: the VM stack plus the frame pointer scaled to bytes. It is
	// derived once, at block zero's own position (see baseFor) rather than in
	// Enter, which runs before any block's label is bound: past its label is
	// what a back edge lands on, so jumping back to block zero re-derives base
	// fresh every iteration instead of reading whatever the budget check
	// between the loop body's last use and the edge left in its register (the
	// allocator frees a register at its value's last textual reference,
	// without regard for a later back edge - see internal/asm/rewriter.go).
	// Every other block, and a cold stub materialize emits after every block
	// is laid out, reuses the same register untouched. Unused when recursive.
	base  asm.VReg
	seen  []bool
	stubs []stub
	spent []ssa.Value
}

// maxSlot is the largest slot index a load or store reaches: the offset is
// scaled by eight into the 12-bit unsigned immediate LDR and STR encode.
const maxSlot = 4095

// Lowers reports whether this machine emits native code for code. It is the
// opcode ratchet the port advances: a function holding an opcode missing here
// is refused whole, and its root compiles through the plan pipeline.
//
// It restates the set exec has a rule for, because the seam asks the question
// before anything is planned and no answer can come from emitting. The two
// disagreeing costs a wasted compile rather than a wrong one: an opcode named
// here that exec declines abandons the compile it had already begun, and one
// exec handles but this refuses is simply never reached.
func (m machine) Lowers(code instr.Opcode) bool {
	switch code {
	case instr.I32_ADD, instr.I32_SUB, instr.I32_MUL,
		instr.I32_DIV_S, instr.I32_DIV_U, instr.I32_REM_S, instr.I32_REM_U,
		instr.I32_AND, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_EQZ, instr.I32_EQ, instr.I32_NE,
		instr.I32_LT_S, instr.I32_LE_S, instr.I32_GT_S, instr.I32_GE_S,
		instr.I32_LT_U, instr.I32_LE_U, instr.I32_GT_U, instr.I32_GE_U,
		instr.I64_ADD, instr.I64_SUB, instr.I64_MUL,
		instr.I64_DIV_S, instr.I64_DIV_U, instr.I64_REM_S, instr.I64_REM_U,
		instr.I64_AND, instr.I64_OR, instr.I64_XOR, instr.I64_EQZ,
		instr.I64_EQ, instr.I64_NE, instr.I64_LT_S, instr.I64_LE_S,
		instr.I64_GT_S, instr.I64_GE_S, instr.I64_LT_U, instr.I64_LE_U,
		instr.I64_GT_U, instr.I64_GE_U, instr.I64_SHL, instr.I64_SHR_S, instr.I64_SHR_U,
		instr.I32_TO_I64_S, instr.I32_TO_I64_U,
		instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV,
		instr.F32_ABS, instr.F32_NEG, instr.F32_SQRT,
		instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_LE, instr.F32_GT, instr.F32_GE,
		instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV,
		instr.F64_ABS, instr.F64_NEG, instr.F64_SQRT,
		instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_LE, instr.F64_GT, instr.F64_GE,
		instr.ARRAY_GET, instr.ARRAY_SET, instr.STRUCT_GET, instr.STRUCT_SET, instr.CALL:
		return true
	default:
		return false
	}
}

// Traps reports whether lowering code ends the block by handing control back.
// Nothing this machine lowers does: an opcode it cannot compute it declines
// outright rather than running as an exit, because an unconditional exit ends
// the block, and every exit this machine emits is the cold path behind a
// guard the hot path falls through.
func (m machine) Traps(instr.Opcode) bool {
	return false
}

// Open begins one compile.
func (m machine) Open(c *backend.Compiler) backend.Lowering {
	return &emitter{c: c, a: c.Asm(), scratch: m.scratch, seen: make([]bool, c.Func().Len())}
}

// Enter mirrors the journal header into the pinned registers and clears the
// callee locals a function entry owns. The frame base every slot is
// addressed from is derived later, at block zero's own position (see
// baseFor), not here: Enter runs before any block's label is bound, and
// deriving base past that label is what lets a back edge to block zero
// re-derive it. The zero-init loop below runs before block zero, so it
// derives its own one-off base instead of waiting for baseFor's (see
// frameBase).
//
// It declines a root whose frame this machine cannot own: a loop entry
// re-enters a frame that is already live, so its locals are left alone
// rather than cleared, and a frame holding a reference in a local or handing
// one back needs the ownership accounting the port has not reached. A loop
// header carrying live operands is declined too: native entry loads nothing
// into block parameters, so there is no state to hand them.
func (e *emitter) Enter() bool {
	root := e.c.Root()
	in := e.c.Input()
	if in.Function == nil || root.Addr != in.Address {
		return false
	}
	e.kind = root.Kind()
	if e.kind != jit.EntryFunction && e.kind != jit.EntryModule && e.kind != jit.EntryLoop {
		return false
	}
	if len(e.c.Func().Block(0).Params) > 0 {
		return false
	}

	declared := in.Function.Declared()
	params := 0
	if in.Function.Typ != nil {
		params = len(in.Function.Typ.Params)
		e.returns = len(in.Function.Typ.Returns)
		for _, typ := range in.Function.Typ.Returns {
			if typ.Kind() == types.KindRef {
				return false
			}
		}
	}
	for _, typ := range declared {
		if typ.Kind() == types.KindRef {
			return false
		}
	}
	e.locals = len(declared)
	if e.locals+e.returns > maxSlot {
		return false
	}

	e.a.Emit(
		arm64.MOV(e.scratch[scratchCtrl], arm64.X0),
		arm64.LDP(e.scratch[scratchStack], e.scratch[scratchGlobals], e.scratch[scratchCtrl], int16(journal.CellStack*8)),
		arm64.LDR(e.scratch[scratchBP], e.scratch[scratchCtrl], int16(journal.CellBP*8)),
	)
	// Only a whole-function entry clears: a loop plan re-enters a frame whose
	// locals are live, and module code has no caller that would have cleared
	// them. One register per distinct zero word serves every local carrying
	// it, since the kinds a frame declares repeat.
	if e.kind != jit.EntryFunction {
		return true
	}
	e.recursive = e.selfRecursive()
	zeros := map[types.Boxed]asm.VReg{}
	var base asm.VReg
	for idx := params; idx < len(declared); idx++ {
		if base.Width() == asm.WidthUndefined {
			base = e.frameBase()
		}
		zero := types.Zero(declared[idx].Kind())
		reg, ok := zeros[zero]
		if !ok {
			reg = e.a.Reg(asm.RegTypeInt, asm.Width64)
			e.a.Emit(arm64.LDI(reg, uint64(zero))...)
			zeros[zero] = reg
		}
		e.a.Emit(arm64.STR(reg, base, int16(idx*8)))
	}
	return true
}

// Lower emits one operation, or the two a guarded heap read is written in.
// Those two are the one shape this machine fuses, and it fuses them because
// they are one operation of the bytecode: they resume into a single
// interpreter state, which describes the operand stack at that instruction
// and nowhere else. Consuming them together is what makes their adjacency a
// structural fact rather than a rule something has to keep true - ops never
// reaches past the end of a block or past a trap, so a guard whose read is
// not the very next operation lowers as neither.
func (e *emitter) Lower(block int, ops []ssa.Operation) (int, bool) {
	e.seen[block] = true
	e.baseFor(block)
	op := ops[0]
	switch op.Op {
	case ssa.OpConst:
		return 1, e.constant(op)
	case ssa.OpLoad:
		return 1, e.load(op)
	case ssa.OpStore:
		return 1, e.store(op)
	case ssa.OpExec:
		return 1, e.exec(op)
	case ssa.OpRetain:
		return 1, e.retain(op)
	case ssa.OpRelease:
		return 1, e.release(op)
	case ssa.OpGuardKind:
		// guardI64 is the only OpGuardKind admission this machine emits, and
		// it validates its own arity and type - no fusion window to inspect
		// here, unlike OpGuardShape below, so nothing is left to check first.
		return 1, e.guardI64(op)
	case ssa.OpGuardShape:
		// The opcode test is load-bearing, not defence in depth. Each of
		// read, structRead, hostRead, write, structWrite, and hostWrite
		// re-derives everything else it needs from the pair, but nothing in
		// any of them re-derives which access it is: an opcode admitted
		// through a bare-itab shape, popping two and pushing one (or three
		// and pushing none), would satisfy every other check. Three separate
		// facts keep that from happening today - frontend/walk.go guards no
		// access but these six opcodes, transform/dce.go keeps a
		// heap-reading or heap-writing exec alive so a guard is never
		// stranded, and Lowers admits no other guard producer - so an edit to
		// any of them belongs here too. Shape.Host is what then tells
		// STRUCT_GET's and STRUCT_SET's two containers apart: a
		// *types.Struct guard never sets it, and a *HostStruct guard always
		// does (see ssa.Shape).
		if len(ops) < 2 || ops[1].Op != ssa.OpExec {
			return 1, false
		}
		switch ops[1].Code {
		case instr.ARRAY_GET:
			return 2, e.read(op, ops[1])
		case instr.ARRAY_SET:
			return 2, e.write(op, ops[1])
		case instr.STRUCT_GET:
			if op.Shape.Host != 0 {
				return 2, e.hostRead(op, ops[1])
			}
			return 2, e.structRead(op, ops[1])
		case instr.STRUCT_SET:
			if op.Shape.Host != 0 {
				return 2, e.hostWrite(op, ops[1])
			}
			return 2, e.structWrite(op, ops[1])
		default:
			return 1, false
		}
	case ssa.OpState:
		// A state materializes nothing where it stands: it names the values a
		// deopt writes back, and the cold stub that writes them is emitted
		// behind the guard that resumes into it (see emitter.exit).
		return 1, true
	default:
		return 1, false
	}
}

// Term ends a block. A terminator leaving through interpreter state - an
// exit, a suspension - unwinds inline where it stands, and a branch table,
// whose edge order this machine does not yet read, is declined with the
// operations that need one.
func (e *emitter) Term(block int, t ssa.Terminator) bool {
	e.seen[block] = true
	e.baseFor(block)
	switch t.Op {
	case ssa.OpJump:
		return e.jump(block, t)
	case ssa.OpBranch:
		return e.branch(block, t)
	case ssa.OpReturn:
		return e.ret(t)
	case ssa.OpComplete:
		return e.complete(t)
	case ssa.OpExit:
		// A tail call retires its frame for another one, which no operation
		// states and no block here is laid out for: it stays on the plan
		// pipeline, whose tail morph lowers it, until the backend represents
		// the morph itself.
		if e.opcode(t.State) == int(instr.RETURN_CALL) {
			return false
		}
		// An exit out of a loop is how a loop normally ends, so the tier
		// never counts it toward giving up; every other exit is a deopt the
		// frontend intended, which the tier counts the same way. Under-count
		// rather than over-count here is deliberate: a cold path that stays
		// cold is still caught by the throughput probe, while a healthy loop
		// retired on its normal exits has no such backstop.
		reason := prof.ExitTerminalOp
		if e.kind == jit.EntryLoop {
			reason = prof.ExitLoop
		}
		return e.terminal(t.State, reason)
	case ssa.OpSuspend:
		// A suspension is a deopt the frontend intended: the interpreter
		// performs the real suspend at the opcode's own IP, so it never
		// counts toward giving up.
		return e.terminal(t.State, prof.ExitTerminalOp)
	default:
		return false
	}
}

// Leave emits the cold stub behind every guard, after the last block, so a
// guard costs one rarely-taken branch on the hot path and none of the stores
// that hand control back.
func (e *emitter) Leave() bool {
	for _, s := range e.stubs {
		e.a.Bind(s.label)
		if !e.unwind(s.deopt, journal.TrapFallback) {
			return false
		}
	}
	return true
}

// constant materializes a compile-time value. An i1, i8, and i32 load their
// payload straight into the W lane, which is already this machine's raw
// representation for them (see backend.bank); an f32's bits load into a
// temporary and then move into the float bank, and an f64's stored word is
// its IEEE bits already.
func (e *emitter) constant(op ssa.Operation) bool {
	if len(op.Results) != 1 {
		return false
	}
	dst := e.c.Reg(op.Results[0])
	typ := e.c.Func().Type(op.Results[0])
	if ssa.TypeOf(op.Const.Kind()) != typ {
		return false
	}
	switch typ {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		e.a.Emit(arm64.LDI(dst, uint64(uint32(op.Const)))...)
	case ssa.TypeI64:
		e.a.Emit(arm64.LDI(dst, uint64(op.Const.I64()))...)
	case ssa.TypeF32:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDI(bits, uint64(uint32(op.Const)))...)
		e.a.Emit(arm64.FMOV(dst, narrow32(bits)))
	case ssa.TypeF64:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDI(bits, uint64(op.Const))...)
		e.a.Emit(arm64.FMOV(dst, bits))
	case ssa.TypeRef:
		// A reference is held boxed, so the pool word is the value itself. It
		// carries no count of its own: the pool holds the retain, which is
		// what leaves the operand borrowed and a cold path owing it a retain.
		e.a.Emit(arm64.LDI(dst, uint64(op.Const))...)
	default:
		return false
	}
	return true
}

// load reads one interpreter slot into the value's register. Every boxed
// word carries its payload in the low 32 bits, so an i1, i8, or i32 needs one
// LDR of exactly that width: it reads only those four bytes and zero-extends
// them, which turns the boxed word back into this machine's raw W-lane
// representation without a mask. A ref reads the same LDR at its own 64-bit
// width instead, taking the whole boxed word verbatim, since it stays boxed
// throughout - the width each takes is dst's own declared one (see
// backend.bank), so one call serves both. A ref takes no count either - the
// slot keeps the one it holds, and the operand borrows it until something
// hands it to storage the interpreter can see.
func (e *emitter) load(op ssa.Operation) bool {
	base, off, ok := e.slot(op.Slot)
	if !ok || len(op.Results) != 1 {
		return false
	}
	dst := e.c.Reg(op.Results[0])
	switch e.c.Func().Type(op.Results[0]) {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32, ssa.TypeI64, ssa.TypeRef:
		e.a.Emit(arm64.LDR(dst, base, int16(off*8)))
	case ssa.TypeF32:
		boxed := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDR(boxed, base, int16(off*8)), arm64.FMOV(dst, narrow32(boxed)))
	case ssa.TypeF64:
		boxed := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDR(boxed, base, int16(off*8)), arm64.FMOV(dst, boxed))
	default:
		return false
	}
	return true
}

// store writes one interpreter slot boxed. A slot able to hold a reference is
// refused whenever what is written disagrees with what the slot is declared
// to hold - a mismatch program.Verify would already have rejected, so this is
// a defensive shape check rather than a real path - because the release below
// assumes the word it loads out of the slot is a reference like the one
// replacing it. Only a global is asked whether it holds references: a frame
// declaring a reference local is refused whole (see Enter), so every local
// slot reached here is scalar, and the value's own SSA type already answers
// the question for it.
//
// A ref-capable slot releases the count it held before the write publishes
// the new one: the old word is not an SSA value the frontend can name, only
// whatever the interpreter would find sitting in that slot, so this is the
// one place that count can be dropped at all. The drop always runs, aliased
// or not: frontend/walk.go's own already gave the new value its own count
// before this op was built, so releasing the overwritten word - even when it
// names the same reference - only ever drops that pre-existing count back to
// what the new value's own count leaves behind. A slot store never retains
// what it writes, only releases what it replaces.
func (e *emitter) store(op ssa.Operation) bool {
	base, off, ok := e.slot(op.Slot)
	if !ok || len(op.Args) != 1 {
		return false
	}
	declared := op.Slot.Space == ssa.SpaceGlobal && e.c.Input().Globals[op.Slot.Index] == types.KindRef
	ref := e.c.Func().Type(op.Args[0]) == ssa.TypeRef
	if declared != ref {
		return false
	}
	boxed, ok := e.box(op.Args[0])
	if !ok {
		return false
	}
	if !ref {
		e.a.Emit(arm64.STR(boxed, base, int16(off*8)))
		return true
	}
	fail, ok := e.exit(op.State, prof.ExitGuardValue)
	if !ok {
		return false
	}
	e.spent = append(e.spent, op.State)

	old := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.LDR(old, base, int16(off*8)))
	e.drop(old, e.pin(scratchCtrl), fail)
	e.a.Emit(arm64.STR(boxed, base, int16(off*8)))
	return true
}

// exec lowers one bytecode operation. Each case names the register lane its
// opcode works in, so an operand the frontend typed otherwise is declined
// rather than run through an instruction of the wrong bank or width.
func (e *emitter) exec(op ssa.Operation) bool {
	switch op.Code {
	case instr.I32_ADD:
		return e.binary(op, ssa.TypeI32, arm64.ADD)
	case instr.I32_SUB:
		return e.binary(op, ssa.TypeI32, arm64.SUB)
	case instr.I32_MUL:
		return e.binary(op, ssa.TypeI32, arm64.MUL)
	case instr.I32_DIV_S:
		return e.divide(op, ssa.TypeI32, arm64.SDIV, false)
	case instr.I32_DIV_U:
		return e.divide(op, ssa.TypeI32, arm64.UDIV, false)
	case instr.I32_REM_S:
		return e.divide(op, ssa.TypeI32, arm64.SDIV, true)
	case instr.I32_REM_U:
		return e.divide(op, ssa.TypeI32, arm64.UDIV, true)
	case instr.I32_AND:
		return e.binary(op, ssa.TypeI32, arm64.AND)
	case instr.I32_OR:
		return e.binary(op, ssa.TypeI32, arm64.ORR)
	case instr.I32_XOR:
		return e.binary(op, ssa.TypeI32, arm64.EOR)
	case instr.I32_SHL:
		return e.shift(op, ssa.TypeI32, 0x1F, arm64.LSL)
	case instr.I32_SHR_S:
		return e.shift(op, ssa.TypeI32, 0x1F, arm64.ASR)
	case instr.I32_SHR_U:
		return e.shift(op, ssa.TypeI32, 0x1F, arm64.LSR)
	case instr.I32_EQZ:
		return e.eqz(op, ssa.TypeI32)
	case instr.I32_EQ:
		return e.compare(op, ssa.TypeI32, arm64.CondEQ)
	case instr.I32_NE:
		return e.compare(op, ssa.TypeI32, arm64.CondNE)
	case instr.I32_LT_S:
		return e.compare(op, ssa.TypeI32, arm64.CondLT)
	case instr.I32_LE_S:
		return e.compare(op, ssa.TypeI32, arm64.CondLE)
	case instr.I32_GT_S:
		return e.compare(op, ssa.TypeI32, arm64.CondGT)
	case instr.I32_GE_S:
		return e.compare(op, ssa.TypeI32, arm64.CondGE)
	case instr.I32_LT_U:
		return e.compare(op, ssa.TypeI32, arm64.CondCC)
	case instr.I32_LE_U:
		return e.compare(op, ssa.TypeI32, arm64.CondLS)
	case instr.I32_GT_U:
		return e.compare(op, ssa.TypeI32, arm64.CondHI)
	case instr.I32_GE_U:
		return e.compare(op, ssa.TypeI32, arm64.CondCS)

	case instr.I64_ADD:
		return e.binary(op, ssa.TypeI64, arm64.ADD)
	case instr.I64_SUB:
		return e.binary(op, ssa.TypeI64, arm64.SUB)
	case instr.I64_MUL:
		return e.binary(op, ssa.TypeI64, arm64.MUL)
	case instr.I64_DIV_S:
		return e.divide(op, ssa.TypeI64, arm64.SDIV, false)
	case instr.I64_DIV_U:
		return e.divide(op, ssa.TypeI64, arm64.UDIV, false)
	case instr.I64_REM_S:
		return e.divide(op, ssa.TypeI64, arm64.SDIV, true)
	case instr.I64_REM_U:
		return e.divide(op, ssa.TypeI64, arm64.UDIV, true)
	case instr.I64_AND:
		return e.binary(op, ssa.TypeI64, arm64.AND)
	case instr.I64_OR:
		return e.binary(op, ssa.TypeI64, arm64.ORR)
	case instr.I64_XOR:
		return e.binary(op, ssa.TypeI64, arm64.EOR)
	case instr.I64_EQZ:
		return e.eqz(op, ssa.TypeI64)
	case instr.I64_EQ:
		return e.compare(op, ssa.TypeI64, arm64.CondEQ)
	case instr.I64_NE:
		return e.compare(op, ssa.TypeI64, arm64.CondNE)
	case instr.I64_LT_S:
		return e.compare(op, ssa.TypeI64, arm64.CondLT)
	case instr.I64_LE_S:
		return e.compare(op, ssa.TypeI64, arm64.CondLE)
	case instr.I64_GT_S:
		return e.compare(op, ssa.TypeI64, arm64.CondGT)
	case instr.I64_GE_S:
		return e.compare(op, ssa.TypeI64, arm64.CondGE)
	case instr.I64_LT_U:
		return e.compare(op, ssa.TypeI64, arm64.CondCC)
	case instr.I64_LE_U:
		return e.compare(op, ssa.TypeI64, arm64.CondLS)
	case instr.I64_GT_U:
		return e.compare(op, ssa.TypeI64, arm64.CondHI)
	case instr.I64_GE_U:
		return e.compare(op, ssa.TypeI64, arm64.CondCS)
	case instr.I64_SHL:
		return e.shift(op, ssa.TypeI64, 0x3F, arm64.LSL)
	case instr.I64_SHR_S:
		return e.shift(op, ssa.TypeI64, 0x3F, arm64.ASR)
	case instr.I64_SHR_U:
		return e.shift(op, ssa.TypeI64, 0x3F, arm64.LSR)
	case instr.I32_TO_I64_S:
		return e.widen(op, true)
	case instr.I32_TO_I64_U:
		return e.widen(op, false)

	case instr.F32_ADD:
		return e.binary(op, ssa.TypeF32, arm64.FADD)
	case instr.F32_SUB:
		return e.binary(op, ssa.TypeF32, arm64.FSUB)
	case instr.F32_MUL:
		return e.binary(op, ssa.TypeF32, arm64.FMUL)
	case instr.F32_DIV:
		return e.binary(op, ssa.TypeF32, arm64.FDIV)
	case instr.F32_ABS:
		return e.unary(op, ssa.TypeF32, arm64.FABS)
	case instr.F32_NEG:
		return e.unary(op, ssa.TypeF32, arm64.FNEG)
	case instr.F32_SQRT:
		return e.unary(op, ssa.TypeF32, arm64.FSQRT)
	case instr.F32_EQ:
		return e.compare(op, ssa.TypeF32, arm64.CondEQ)
	case instr.F32_NE:
		return e.compare(op, ssa.TypeF32, arm64.CondNE)
	case instr.F32_LT:
		return e.compare(op, ssa.TypeF32, arm64.CondMI)
	case instr.F32_LE:
		return e.compare(op, ssa.TypeF32, arm64.CondLS)
	case instr.F32_GT:
		return e.compare(op, ssa.TypeF32, arm64.CondGT)
	case instr.F32_GE:
		return e.compare(op, ssa.TypeF32, arm64.CondGE)

	case instr.F64_ADD:
		return e.binary(op, ssa.TypeF64, arm64.FADD)
	case instr.F64_SUB:
		return e.binary(op, ssa.TypeF64, arm64.FSUB)
	case instr.F64_MUL:
		return e.binary(op, ssa.TypeF64, arm64.FMUL)
	case instr.F64_DIV:
		return e.binary(op, ssa.TypeF64, arm64.FDIV)
	case instr.F64_ABS:
		return e.unary(op, ssa.TypeF64, arm64.FABS)
	case instr.F64_NEG:
		return e.unary(op, ssa.TypeF64, arm64.FNEG)
	case instr.F64_SQRT:
		return e.unary(op, ssa.TypeF64, arm64.FSQRT)
	case instr.F64_EQ:
		return e.compare(op, ssa.TypeF64, arm64.CondEQ)
	case instr.F64_NE:
		return e.compare(op, ssa.TypeF64, arm64.CondNE)
	case instr.F64_LT:
		return e.compare(op, ssa.TypeF64, arm64.CondMI)
	case instr.F64_LE:
		return e.compare(op, ssa.TypeF64, arm64.CondLS)
	case instr.F64_GT:
		return e.compare(op, ssa.TypeF64, arm64.CondGT)
	case instr.F64_GE:
		return e.compare(op, ssa.TypeF64, arm64.CondGE)

	case instr.CALL:
		return e.call(op)
	default:
		// A heap read is not here: it lowers only as the second half of the
		// guarded pair Lower fuses, never on its own.
		return false
	}
}

// binary lowers a two-operand opcode over the lane want names. An i32 runs on
// the W lane its operands and result already occupy - the register width
// itself is what keeps the computation to 32 bits and zero-extends the
// result, with nothing left to mask - which is what lets i1 and i8 flow
// through it keeping their own result kinds; a float runs in the bank its
// operands already occupy.

func (e *emitter) binary(op ssa.Operation, want ssa.Type, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1], op.Results[0]) {
		return false
	}
	dst := e.c.Reg(op.Results[0])
	e.a.Emit(emit(dst, e.c.Reg(op.Args[0]), e.c.Reg(op.Args[1])))
	if op.State != ssa.NoValue {
		return e.guardBoxable(op.State, dst)
	}
	return true
}

// divide lowers a division or remainder over want's own lane: emit computes
// the quotient, and rem recovers the remainder from it with one MSUB, since
// ARM64 has no remainder instruction - a % b is a - (a/b)*b for whichever
// rounding SDIV or UDIV already used, so the quotient this always computes
// first is exactly the one that subtraction needs.
//
// The divisor is guarded against zero before either divides: ARM64's SDIV
// and UDIV return zero for a zero divisor rather than trapping, but the
// threaded interpreter panics ErrDivideByZero (see interp/threaded.go's
// I32_DIV_S and I64_DIV_S handlers), so native code raises that fault
// itself by deopting through the op's own pre-op state - both operands
// still on the stack, as store's replaced-ref guard resumes into - rather
// than computing a wrong answer.
//
// A signed divide needs no further guard against INT_MIN/-1: Go's own
// division wraps on that overflow exactly as ARM64's SDIV does, so the two
// already agree without one. An i64 divide's quotient still needs the same
// boxable-range guard I64_ADD/SUB/MUL/SHL/SHR_U already run after computing
// (see guardBoxable and ssa.OverflowsI64), because dividing two in-range
// operands can still leave the payload - the boxed range is asymmetric, so
// its own minimum divided by -1 is one past the maximum. A remainder never
// needs it: its magnitude is bounded by its divisor's, itself already
// inside the boxed range.
func (e *emitter) divide(op ssa.Operation, want ssa.Type, emit func(dst, src1, src2 asm.Reg) asm.Instruction, rem bool) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1], op.Results[0]) {
		return false
	}
	a, b := e.c.Reg(op.Args[0]), e.c.Reg(op.Args[1])
	fail, ok := e.exit(op.State, prof.ExitGuardValue)
	if !ok {
		return false
	}
	e.a.Emit(arm64.CMPI(b, 0), arm64.BCondLabel(arm64.OpBEQ, fail))

	dst := e.c.Reg(op.Results[0])
	if !rem {
		e.a.Emit(emit(dst, a, b))
		if want == ssa.TypeI64 {
			return e.guardBoxable(op.State, dst)
		}
		return true
	}
	width := asm.Width32
	if want == ssa.TypeI64 {
		width = asm.Width64
	}
	quotient := e.a.Reg(asm.RegTypeInt, width)
	e.a.Emit(emit(quotient, a, b))
	e.a.Emit(arm64.MSUB(dst, quotient, b, a))
	return true
}

// widen lowers I32_TO_I64_S/U. Either direction is always boxable: an i32's
// magnitude is at most 2^31, far inside the 49-bit boxed lane, so neither
// needs the guard an i64 slot load does (see guardI64). Signed widening
// sign-extends the W-lane value into the X lane with one SXTW; unsigned
// widening zero-extends it, which every W-register write already does for
// free (see widen64) - the raw i32 view is already the correct raw i64 one,
// so UXTW here is the same one-instruction move into the result's own
// register rather than a reinterpretation of the source's.
func (e *emitter) widen(op ssa.Operation, signed bool) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 || e.c.Func().Type(op.Args[0]) != ssa.TypeI32 || e.c.Func().Type(op.Results[0]) != ssa.TypeI64 {
		return false
	}
	dst, src := e.c.Reg(op.Results[0]), e.c.Reg(op.Args[0])
	if signed {
		e.a.Emit(arm64.SXTW(dst, src))
	} else {
		e.a.Emit(arm64.UXTW(dst, src))
	}
	return true
}

func (e *emitter) unary(op ssa.Operation, want ssa.Type, emit func(dst, src asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Results[0]) {
		return false
	}
	e.a.Emit(emit(e.c.Reg(op.Results[0]), e.c.Reg(op.Args[0])))
	return true
}

// shift lowers an i32 or i64 shift. The value and the amount already sit raw
// in want's own lane, so nothing prepares either: the amount is masked to
// the width the underlying instruction shifts by (five bits for a 32-bit
// register, six for a 64-bit one, matching what the threaded handler shifts
// by - see interp/threaded.go's I64_SHL/I64_SHR_U handlers), and the shift
// itself runs entirely within the register's own bits - an arithmetic right
// shift included, since the sign bit it reads is already the register's own
// top bit, never one belonging to a wider sign-extended view.

func (e *emitter) shift(op ssa.Operation, want ssa.Type, mask uint64, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1]) || e.c.Func().Type(op.Results[0]) != want {
		return false
	}
	width := asm.Width32
	if want == ssa.TypeI64 {
		width = asm.Width64
	}
	amount := e.a.Reg(asm.RegTypeInt, width)
	e.a.Emit(arm64.ANDI(amount, e.c.Reg(op.Args[1]), mask))
	dst := e.c.Reg(op.Results[0])
	e.a.Emit(emit(dst, e.c.Reg(op.Args[0]), amount))
	if op.State != ssa.NoValue {
		return e.guardBoxable(op.State, dst)
	}
	return true
}

// compare lowers a comparison to the flag test the lane want names and sets
// the i1 its result is. An i32 or i64 already sits raw in the lane it
// compares on - the W lane for the former, the X lane for the latter - so a
// signed and an unsigned condition both read correct flags with no
// narrowing; a float compares in its own bank.
func (e *emitter) compare(op ssa.Operation, want ssa.Type, cond uint8) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1]) || e.c.Func().Type(op.Results[0]) != ssa.TypeI1 {
		return false
	}
	a, b := e.c.Reg(op.Args[0]), e.c.Reg(op.Args[1])
	if want == ssa.TypeI32 || want == ssa.TypeI64 {
		e.a.Emit(arm64.CMP(a, b))
	} else {
		e.a.Emit(arm64.FCMP(a, b))
	}
	e.a.Emit(arm64.CSET(e.c.Reg(op.Results[0]), cond))
	return true
}

// eqz lowers a zero test over want's own lane to the i1 its result is.
func (e *emitter) eqz(op ssa.Operation, want ssa.Type) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0]) || e.c.Func().Type(op.Results[0]) != ssa.TypeI1 {
		return false
	}
	e.a.Emit(
		arm64.CMPI(e.c.Reg(op.Args[0]), 0),
		arm64.CSET(e.c.Reg(op.Results[0]), arm64.CondEQ),
	)
	return true
}

func (e *emitter) jump(block int, t ssa.Terminator) bool {
	if len(t.Edges) != 1 {
		return false
	}
	edge := t.Edges[0]
	if e.reached(edge) {
		return e.back(edge)
	}
	if !e.moves(edge) {
		return false
	}
	if next, ok := e.c.Next(block); ok && next == edge.Block {
		return true
	}
	e.a.Emit(arm64.BLabel(e.c.Block(edge.Block)))
	return true
}

// branch tests the condition and continues on one of two blocks. The layout
// puts a terminator's first edge next wherever it can, so the common shape is
// one inverted test over the taken block that falls into it. That shape only
// serves edges carrying nothing: an edge with block parameters emits its moves
// on a stub instead, and a back edge spends the safepoint budget there.
func (e *emitter) branch(block int, t ssa.Terminator) bool {
	if len(t.Edges) != 2 || len(t.Args) != 1 || !e.lanes(ssa.TypeI32, t.Args[0]) {
		return false
	}
	if !e.wired(t.Edges[0]) || !e.wired(t.Edges[1]) {
		return false
	}
	if len(e.c.Moves(t.Edges[0])) == 0 && len(e.c.Moves(t.Edges[1])) == 0 &&
		!e.reached(t.Edges[0]) && !e.reached(t.Edges[1]) {
		cond := e.c.Reg(t.Args[0])
		next, falls := e.c.Next(block)
		if falls && next == t.Edges[0].Block {
			e.a.Emit(arm64.CBZLabel(cond, e.c.Block(t.Edges[1].Block)))
			return true
		}
		e.a.Emit(arm64.CBNZLabel(cond, e.c.Block(t.Edges[0].Block)))
		if falls && next == t.Edges[1].Block {
			return true
		}
		e.a.Emit(arm64.BLabel(e.c.Block(t.Edges[1].Block)))
		return true
	}
	cond := e.c.Reg(t.Args[0])
	taken := e.a.Label()
	e.a.Emit(arm64.CBNZLabel(cond, taken))
	if e.reached(t.Edges[1]) {
		if !e.back(t.Edges[1]) {
			return false
		}
	} else {
		if !e.moves(t.Edges[1]) {
			return false
		}
		e.a.Emit(arm64.BLabel(e.c.Block(t.Edges[1].Block)))
	}
	e.a.Bind(taken)
	if e.reached(t.Edges[0]) {
		return e.back(t.Edges[0])
	}
	if !e.moves(t.Edges[0]) {
		return false
	}
	if next, ok := e.c.Next(block); ok && next == t.Edges[0].Block {
		return true
	}
	e.a.Emit(arm64.BLabel(e.c.Block(t.Edges[0].Block)))
	return true
}

// ret closes a function entry: the boxed results land at the frame base for
// the Go wrapper, which tears the frame down, and in the ABI return registers
// for a native caller that entered through this function's own slot. A loop
// root inside a function returns the same way; a loop owning the whole module
// completes instead.
func (e *emitter) ret(t ssa.Terminator) bool {
	if e.kind != jit.EntryFunction && e.kind != jit.EntryLoop {
		return false
	}
	if len(t.Args) != e.returns {
		return false
	}
	for idx, arg := range t.Args {
		boxed, ok := e.box(arg)
		if !ok {
			return false
		}
		e.a.Emit(arm64.STR(boxed, e.addr(), int16(idx*8)))
		if idx < len(arm64.IntRets) {
			e.a.Emit(arm64.MOV(e.pinTo(arm64.IntRets[idx]), boxed))
		}
	}
	e.a.Emit(arm64.RET())
	return true
}

// complete finishes top-level module code, which has no return: the operands
// it leaves behind are boxed back onto the VM stack, the stack pointer is
// published, and the wrapper marks the frame exhausted.
func (e *emitter) complete(t ssa.Terminator) bool {
	if e.kind != jit.EntryModule && e.kind != jit.EntryLoop {
		return false
	}
	if e.locals+len(t.Args) > maxSlot {
		return false
	}
	for idx, arg := range t.Args {
		boxed, ok := e.box(arg)
		if !ok {
			return false
		}
		e.a.Emit(arm64.STR(boxed, e.base, int16((e.locals+idx)*8)))
	}
	ctrl := e.pin(scratchCtrl)
	sp := e.a.Reg(asm.RegTypeInt, asm.Width64)
	trap := e.a.Reg(asm.RegTypeInt, asm.Width64)
	next := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.ADDI(sp, e.pin(scratchBP), uint16(e.locals+len(t.Args))))
	e.a.Emit(arm64.STR(sp, ctrl, int16(journal.CellSP*8)))
	e.a.Emit(arm64.LDI(trap, uint64(journal.TrapNone))...)
	e.a.Emit(arm64.STR(trap, ctrl, int16(journal.CellTrap*8)))
	e.a.Emit(arm64.LDI(next, uint64(len(e.c.Input().Function.Code)))...)
	e.a.Emit(arm64.STR(next, ctrl, int16(journal.CellNextIP*8)))
	e.a.Emit(arm64.RET())
	return true
}

// box produces v's boxed word: the form every VM slot holds. An i1, i8, and
// i32 need no mask: every producer in this machine writes one through a
// genuine W-register instruction, and AArch64 zeroes the upper 32 bits of
// the corresponding X register on every such write (see docs/jit-internals.md's
// ARM64 SSA machine section), so the value is already the clean low 32 bits of
// its own 64-bit view - boxing just copies that view into a fresh register
// and MOVKs the kind tag into its top 16 bits; the three kinds differ only in
// that tag, which is what collapses them into one case (see rawTag). An f32's
// bits leave the float bank the same way, straight into a fresh register at
// full width (an FMOV from a 32-bit float source zero-extends its 64-bit
// integer destination for the same architectural reason), so it takes the
// identical single MOVK; an f64's boxed word is its bit pattern already; and
// a reference is held boxed throughout, so it is handed straight back.
func (e *emitter) box(v ssa.Value) (asm.VReg, bool) {
	src := e.c.Reg(v)
	typ := e.c.Func().Type(v)
	if typ == ssa.TypeRef {
		return src, true
	}
	out := e.a.Reg(asm.RegTypeInt, asm.Width64)
	switch typ {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		e.a.Emit(arm64.MOV(out, widen64(src)), arm64.MOVK(out, uint16(rawTag(typ)>>48), 48))
	case ssa.TypeI64:
		// A raw i64's top 16 bits are not free the way i1/i8/i32's are: bit 48
		// is the sign bit of the 49-bit boxed payload (types.VBits), not
		// always zero, so MOVK-ing the tag over bits 48-63 would overwrite it
		// and corrupt every negative value. Mask to the payload width first,
		// then OR the tag in - Tag's own bits never reach below bit 49, so the
		// two halves never collide (see types.Box).
		e.a.Emit(arm64.ANDI(out, src, maskI64))
		tag := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.LDI(tag, tagI64)...)
		e.a.Emit(arm64.ORR(out, out, tag))
	case ssa.TypeF32:
		e.a.Emit(arm64.FMOV(out, src), arm64.MOVK(out, uint16(tagF32>>48), 48))
	case ssa.TypeF64:
		e.a.Emit(arm64.FMOV(out, src))
	default:
		return asm.VReg{}, false
	}
	return out, true
}

// raw produces v's untagged bit pattern in a fresh 64-bit register: the form
// a typed array element or a struct field stores, as opposed to box's tagged
// VM slot word. An i1, i8, or i32 widens its already-clean 32-bit view (see
// widen64) rather than allocating, since every producer in this machine
// already leaves its upper 32 bits zero; an i64 is already this form; a
// float's bits leave the float bank through the same zero-extending FMOV box
// uses, for the identical architectural reason.
func (e *emitter) raw(v ssa.Value) (asm.VReg, bool) {
	src := e.c.Reg(v)
	switch e.c.Func().Type(v) {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return widen64(src), true
	case ssa.TypeI64:
		return src, true
	case ssa.TypeF32, ssa.TypeF64:
		out := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.FMOV(out, src))
		return out, true
	default:
		return asm.VReg{}, false
	}
}

// rawTag is the boxed kind tag for one of box's raw W-lane types. The caller
// has already narrowed typ to one of these three, so there is nothing for a
// fourth case to report.
func rawTag(typ ssa.Type) uint64 {
	switch typ {
	case ssa.TypeI1:
		return tagI1
	case ssa.TypeI8:
		return tagI8
	default:
		return tagI32
	}
}

// baseFor derives the frame base exactly once, at block zero's own position:
// past its label, before any of its operations. Lower and Term both call it,
// on every block, but it acts only for block zero and only the first time -
// every other block, and every later call for block zero itself, reuses the
// register already derived there, which is what a back edge to block zero
// reads fresh on every iteration (see emitter.base). It caches nothing for a
// recursive function; addr re-derives instead (see addr).
func (e *emitter) baseFor(block int) {
	if block != 0 || e.recursive || e.base.Width() != asm.WidthUndefined {
		return
	}
	e.base = e.frameBase()
}

// frameBase derives a fresh frame base into its own register: the VM stack
// plus the frame pointer scaled to bytes. Enter's zero-init loop calls it
// directly for a one-off base, since it runs before block zero derives the
// function's own (see baseFor).
func (e *emitter) frameBase() asm.VReg {
	base := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LSLI(base, e.pin(scratchBP), 3),
		arm64.ADD(base, e.pin(scratchStack), base),
	)
	return base
}

// addr returns the current activation's frame base: base's single cached
// derivation, or - once selfCall overwrites bp around its own BL (see
// selfcall.go) - a fresh derivation off the pinned bp register every read.
// base's one long-lived vreg would need two values live across that BL,
// which the allocator's self-recursive-call barrier refuses to carry (see
// internal/asm/eligibility.go's barriers); a fresh vreg per site never spans
// the call.
func (e *emitter) addr() asm.VReg {
	if e.recursive {
		return e.frameBase()
	}
	return e.base
}

// slot resolves the base register and word offset one interpreter slot lives
// at. A frame base other than the entry frame's names an inlined callee's
// storage and an upvalue's base is the closure's: this machine reaches
// neither.
func (e *emitter) slot(s ssa.Slot) (asm.VReg, int, bool) {
	switch s.Space {
	case ssa.SpaceLocal:
		if s.Base != 0 || s.Index < 0 || s.Index >= e.locals {
			return asm.VReg{}, 0, false
		}
		return e.addr(), s.Index, true
	case ssa.SpaceGlobal:
		if s.Index < 0 || s.Index >= len(e.c.Input().Globals) || s.Index > maxSlot {
			return asm.VReg{}, 0, false
		}
		return e.pin(scratchGlobals), s.Index, true
	default:
		return asm.VReg{}, 0, false
	}
}

// terminal unwinds inline where control leaves native execution: the cold
// stub a guard exits through cannot serve a block that ends there, because no
// hot path falls through past it.
func (e *emitter) terminal(state ssa.Value, reason prof.ExitReason) bool {
	d := e.c.Exit(state, reason, e.opcode(state))
	if !fits(d) {
		return false
	}
	return e.unwind(d, journal.TrapFallback)
}

// reached reports whether edge targets a block already laid out: a back edge,
// whose loop-carried values arrive on block parameters this machine places
// and whose safepoint budget it spends.
func (e *emitter) reached(edge ssa.Edge) bool {
	return edge.Block >= 0 && edge.Block < len(e.seen) && e.seen[edge.Block]
}

// wired reports whether edge names a successor this machine emits: a laid-out
// block, or a forward one. Anything else - out of range, or an already
// laid-out block that is not the loop header - is declined rather than
// branched to.
func (e *emitter) wired(edge ssa.Edge) bool {
	if edge.Block < 0 || edge.Block >= len(e.seen) {
		return false
	}
	return !e.seen[edge.Block] || edge.Block == 0
}

// moves emits the register copies edge's arguments must make into its target
// block's parameters. A length mismatch is declined rather than dropped: the
// verifier pairs every argument with a parameter, so one here is a malformed
// function, not an empty edge.
func (e *emitter) moves(edge ssa.Edge) bool {
	if edge.Block < 0 || edge.Block >= e.c.Func().Len() {
		return false
	}
	if len(e.c.Func().Block(edge.Block).Params) != len(edge.Args) {
		return false
	}
	for _, m := range e.c.Moves(edge) {
		if m.Dst.Type() == asm.RegTypeFloat {
			e.a.Emit(arm64.FMOV(m.Dst, m.Src))
		} else {
			e.a.Emit(arm64.MOV(m.Dst, m.Src))
		}
	}
	return true
}

// back continues a loop at its header: the edge's arguments have already been
// moved into the header's parameters, so what remains is the safepoint
// budget. While budget remains control stays native; once spent, the frame
// yields at the header with whatever the stores left in the VM slots - locals
// are committed by every store, and the header takes no operands, which Enter
// refused any entry carrying - and the interpreter continues threaded there.
func (e *emitter) back(edge ssa.Edge) bool {
	if edge.Block != 0 || len(edge.Args) != 0 {
		return false
	}
	root := e.c.Root()
	ctrl := e.pin(scratchCtrl)
	budget := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LDR(budget, ctrl, int16(journal.CellBudget*8)),
		arm64.SUBI(budget, budget, 1),
		arm64.STR(budget, ctrl, int16(journal.CellBudget*8)),
		arm64.CBNZLabel(budget, e.c.Block(0)),
	)
	d := backend.Deopt{
		ID:     -1,
		Resume: root.IP,
		SP:     e.locals,
		Frames: []backend.Record{{Addr: root.Addr, BP: 0, IP: root.IP, Returns: e.returns}},
	}
	if !fits(d) {
		return false
	}
	return e.unwind(d, journal.TrapYield)
}

func (e *emitter) lanes(want ssa.Type, vs ...ssa.Value) bool {
	for _, v := range vs {
		if lane(e.c.Func().Type(v)) != want {
			return false
		}
	}
	return true
}

// widen64 is v's 64-bit view: the same physical register, reinterpreted at
// full width. Safe only when every write to v went through a genuine
// 32-bit-register instruction - true of every value box calls this on - since
// AArch64 zeroes the upper 32 bits of the corresponding 64-bit register on
// such a write, so the view this produces is a clean, already-zero-extended
// 64-bit value rather than one this call has to clean itself.
func widen64(v asm.VReg) asm.VReg {
	return asm.NewVReg(v.ID(), v.Type(), asm.Width64)
}

func (e *emitter) sign32(v asm.VReg) asm.VReg {
	out := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.SXTW(out, v))
	return out
}

func (e *emitter) pin(idx int) asm.VReg {
	return e.pinTo(e.scratch[idx])
}

func (e *emitter) pinTo(pr asm.PReg) asm.VReg {
	v := e.a.Reg(asm.RegTypeInt, asm.Width64)
	_ = e.a.Pin(v, pr)
	return v
}

// lane is the register form values of t share, or the zero Type for one no
// arithmetic here computes in: a reference, which is moved rather than
// computed, and the interpreter state an OpState defines. An i64 holds a lane
// of its own, which only a value a guard has already proven inline may enter.
func lane(t ssa.Type) ssa.Type {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return ssa.TypeI32
	case ssa.TypeI64:
		return ssa.TypeI64
	case ssa.TypeF32, ssa.TypeF64:
		return t
	default:
		return 0
	}
}

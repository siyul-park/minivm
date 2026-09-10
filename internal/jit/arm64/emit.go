package arm64

import (
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/asm/arm64"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/jit/backend"
	"github.com/siyul-park/minivm/internal/journal"
	"github.com/siyul-park/minivm/internal/ssa"
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

	base  asm.VReg
	seen  []bool
	stubs []stub
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
		instr.I32_AND, instr.I32_OR, instr.I32_XOR,
		instr.I32_SHL, instr.I32_SHR_S, instr.I32_SHR_U,
		instr.I32_EQZ, instr.I32_EQ, instr.I32_NE,
		instr.I32_LT_S, instr.I32_LE_S, instr.I32_GT_S, instr.I32_GE_S,
		instr.I32_LT_U, instr.I32_LE_U, instr.I32_GT_U, instr.I32_GE_U,
		instr.F32_ADD, instr.F32_SUB, instr.F32_MUL, instr.F32_DIV,
		instr.F32_ABS, instr.F32_NEG, instr.F32_SQRT,
		instr.F32_EQ, instr.F32_NE, instr.F32_LT, instr.F32_LE, instr.F32_GT, instr.F32_GE,
		instr.F64_ADD, instr.F64_SUB, instr.F64_MUL, instr.F64_DIV,
		instr.F64_ABS, instr.F64_NEG, instr.F64_SQRT,
		instr.F64_EQ, instr.F64_NE, instr.F64_LT, instr.F64_LE, instr.F64_GT, instr.F64_GE,
		instr.ARRAY_GET:
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

// Enter mirrors the journal header into the pinned registers, derives the
// frame base every slot is addressed from, and clears the callee locals a
// function entry owns. It declines a root whose frame this machine cannot
// own: a loop entry re-enters a frame that is already live, and a frame
// holding a reference in a local or handing one back needs the ownership
// accounting the port has not reached.
func (e *emitter) Enter() bool {
	root := e.c.Root()
	in := e.c.Input()
	if in.Function == nil || root.Addr != in.Address {
		return false
	}
	e.kind = root.Kind()
	if e.kind != jit.EntryFunction && e.kind != jit.EntryModule {
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
	e.base = e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(
		arm64.LSLI(e.base, e.pin(scratchBP), 3),
		arm64.ADD(e.base, e.pin(scratchStack), e.base),
	)

	// Only a whole-function entry clears: a loop plan re-enters a frame whose
	// locals are live, and module code has no caller that would have cleared
	// them. One register per distinct zero word serves every local carrying
	// it, since the kinds a frame declares repeat.
	if e.kind != jit.EntryFunction {
		return true
	}
	zeros := map[types.Boxed]asm.VReg{}
	for idx := params; idx < len(declared); idx++ {
		zero := types.Zero(declared[idx].Kind())
		reg, ok := zeros[zero]
		if !ok {
			reg = e.a.Reg(asm.RegTypeInt, asm.Width64)
			e.a.Emit(arm64.LDI(reg, uint64(zero))...)
			zeros[zero] = reg
		}
		e.a.Emit(arm64.STR(reg, e.base, int16(idx*8)))
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
	case ssa.OpGuardShape:
		// The opcode test is load-bearing, not defence in depth. read
		// re-derives everything else it needs from the pair, but nothing in
		// it re-derives "this is an array read": an opcode admitted through
		// a bare-itab shape, popping two and pushing one, would satisfy
		// every check. Three separate facts keep that from happening today -
		// frontend/walk.go guards no other read, transform/dce.go keeps a
		// heap-reading exec alive so a guard is never stranded, and Lowers
		// admits no other guard producer - so an edit to any of them belongs
		// here too.
		if len(ops) < 2 || ops[1].Op != ssa.OpExec || ops[1].Code != instr.ARRAY_GET {
			return 1, false
		}
		return 2, e.read(op, ops[1])
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
// exit, a suspension - is declined with the operations that need one, and so
// is a branch table, whose edge order this machine does not yet read.
func (e *emitter) Term(block int, t ssa.Terminator) bool {
	e.seen[block] = true
	switch t.Op {
	case ssa.OpJump:
		return e.jump(block, t)
	case ssa.OpBranch:
		return e.branch(block, t)
	case ssa.OpReturn:
		return e.ret(t)
	case ssa.OpComplete:
		return e.complete(t)
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
		if !e.unwind(s.deopt) {
			return false
		}
	}
	return true
}

// constant materializes a compile-time value. The boxed word carries its own
// unboxed form: an i32, i8, i1, and f32 keep it in the low 32 bits, and an
// f64's boxed word is its IEEE bits already.
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

// load reads one interpreter slot into the value's register, unboxed. An i32,
// i8, i1, and ref need no work at all: the boxed word already carries the
// value in the lane every later operation reads it in. A ref takes no count
// either - the slot keeps the one it holds, and the operand borrows it until
// something hands it to storage the interpreter can see.
func (e *emitter) load(op ssa.Operation) bool {
	base, off, ok := e.slot(op.Slot)
	if !ok || len(op.Results) != 1 {
		return false
	}
	dst := e.c.Reg(op.Results[0])
	switch e.c.Func().Type(op.Results[0]) {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32, ssa.TypeRef:
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

// store writes one interpreter slot boxed. Only a slot that cannot be holding
// a reference: overwriting one releases the reference it replaced and adopts
// the one it takes, which is the ownership accounting this machine does not
// emit. The slot decides that and not the value written, because an i32
// stored over a slot that currently holds a reference drops that count just
// as a reference would. Only a global is asked: a frame declaring a reference
// local is refused whole (see Enter), so every local slot reached here is
// scalar. The rule belongs here rather than with the addressing, because
// reading the same slot borrows and owes nothing.
func (e *emitter) store(op ssa.Operation) bool {
	base, off, ok := e.slot(op.Slot)
	if !ok || len(op.Args) != 1 {
		return false
	}
	if op.Slot.Space == ssa.SpaceGlobal && e.c.Input().Globals[op.Slot.Index] == types.KindRef {
		return false
	}
	boxed, ok := e.box(op.Args[0])
	if !ok {
		return false
	}
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
	case instr.I32_AND:
		return e.binary(op, ssa.TypeI32, arm64.AND)
	case instr.I32_OR:
		return e.binary(op, ssa.TypeI32, arm64.ORR)
	case instr.I32_XOR:
		return e.binary(op, ssa.TypeI32, arm64.EOR)
	case instr.I32_SHL:
		return e.shift(op, arm64.LSL, e.zero32)
	case instr.I32_SHR_S:
		return e.shift(op, arm64.ASR, e.sign32)
	case instr.I32_SHR_U:
		return e.shift(op, arm64.LSR, e.zero32)
	case instr.I32_EQZ:
		return e.eqz(op)
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
	default:
		// A heap read is not here: it lowers only as the second half of the
		// guarded pair Lower fuses, never on its own.
		return false
	}
}

// binary lowers a two-operand opcode over the lane want names. An i32 runs on
// the whole register because only the low 32 bits carry the value and boxing
// masks the rest, which is what lets i1 and i8 flow through it keeping their
// own result kinds; a float runs in the bank its operands already occupy.
func (e *emitter) binary(op ssa.Operation, want ssa.Type, emit func(dst, src1, src2 asm.Reg) asm.Instruction) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1], op.Results[0]) {
		return false
	}
	e.a.Emit(emit(e.c.Reg(op.Results[0]), e.c.Reg(op.Args[0]), e.c.Reg(op.Args[1])))
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

// shift lowers an i32 shift. The amount is masked to five bits, matching what
// the threaded handler shifts by, and prep extends the value lane so the bits
// the boxed form leaves above it never enter the result.
func (e *emitter) shift(op ssa.Operation, emit func(dst, src1, src2 asm.Reg) asm.Instruction, prep func(asm.VReg) asm.VReg) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(ssa.TypeI32, op.Args[0], op.Args[1], op.Results[0]) || e.c.Func().Type(op.Results[0]) != ssa.TypeI32 {
		return false
	}
	amount := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.ANDI(amount, e.c.Reg(op.Args[1]), 0x1F))
	e.a.Emit(emit(e.c.Reg(op.Results[0]), prep(e.c.Reg(op.Args[0])), amount))
	return true
}

// compare lowers a comparison to the flag test the lane want names and sets
// the i1 its result is. An integer compares on its 32-bit value lane, so a
// signed and an unsigned condition both read correct flags; a float compares
// in its own bank.
func (e *emitter) compare(op ssa.Operation, want ssa.Type, cond uint8) bool {
	if len(op.Args) != 2 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(want, op.Args[0], op.Args[1]) || e.c.Func().Type(op.Results[0]) != ssa.TypeI1 {
		return false
	}
	a, b := e.c.Reg(op.Args[0]), e.c.Reg(op.Args[1])
	if want == ssa.TypeI32 {
		e.a.Emit(arm64.CMP(narrow32(a), narrow32(b)))
	} else {
		e.a.Emit(arm64.FCMP(a, b))
	}
	e.a.Emit(arm64.CSET(e.c.Reg(op.Results[0]), cond))
	return true
}

func (e *emitter) eqz(op ssa.Operation) bool {
	if len(op.Args) != 1 || len(op.Results) != 1 {
		return false
	}
	if !e.lanes(ssa.TypeI32, op.Args[0]) || e.c.Func().Type(op.Results[0]) != ssa.TypeI1 {
		return false
	}
	e.a.Emit(
		arm64.CMPI(narrow32(e.c.Reg(op.Args[0])), 0),
		arm64.CSET(e.c.Reg(op.Results[0]), arm64.CondEQ),
	)
	return true
}

func (e *emitter) jump(block int, t ssa.Terminator) bool {
	if len(t.Edges) != 1 || !e.forward(t.Edges[0]) {
		return false
	}
	if next, ok := e.c.Next(block); ok && next == t.Edges[0].Block {
		return true
	}
	e.a.Emit(arm64.BLabel(e.c.Block(t.Edges[0].Block)))
	return true
}

// branch tests the condition and continues on one of two blocks. The layout
// puts a terminator's first edge next wherever it can, so the common shape is
// one inverted test over the taken block that falls into it.
func (e *emitter) branch(block int, t ssa.Terminator) bool {
	if len(t.Edges) != 2 || len(t.Args) != 1 || !e.lanes(ssa.TypeI32, t.Args[0]) {
		return false
	}
	if !e.forward(t.Edges[0]) || !e.forward(t.Edges[1]) {
		return false
	}
	cond := narrow32(e.c.Reg(t.Args[0]))
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

// ret closes a function entry: the boxed results land at the frame base for
// the Go wrapper, which tears the frame down, and in the ABI return registers
// for a native caller that entered through this function's own slot.
func (e *emitter) ret(t ssa.Terminator) bool {
	if e.kind != jit.EntryFunction || len(t.Args) != e.returns {
		return false
	}
	for idx, arg := range t.Args {
		boxed, ok := e.box(arg)
		if !ok {
			return false
		}
		e.a.Emit(arm64.STR(boxed, e.base, int16(idx*8)))
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
	if e.kind != jit.EntryModule || e.locals+len(t.Args) > maxSlot {
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

// box produces v's boxed word: the form every VM slot holds. A raw i32, i8,
// and i1 keep their value in the low 32 bits, so boxing masks and tags into a
// fresh register; an f32's bits leave the float bank first; an f64's boxed
// word is its bit pattern already; and a reference is held boxed throughout,
// so it is handed straight back.
func (e *emitter) box(v ssa.Value) (asm.VReg, bool) {
	src := e.c.Reg(v)
	typ := e.c.Func().Type(v)
	if typ == ssa.TypeRef {
		return src, true
	}
	out := e.a.Reg(asm.RegTypeInt, asm.Width64)
	switch typ {
	case ssa.TypeI1:
		e.a.Emit(arm64.ANDI(out, src, maskI32), arm64.MOVK(out, uint16(tagI1>>48), 48))
	case ssa.TypeI8:
		e.a.Emit(arm64.ANDI(out, src, maskI32), arm64.MOVK(out, uint16(tagI8>>48), 48))
	case ssa.TypeI32:
		e.a.Emit(arm64.ANDI(out, src, maskI32), arm64.MOVK(out, uint16(tagI32>>48), 48))
	case ssa.TypeF32:
		bits := e.a.Reg(asm.RegTypeInt, asm.Width64)
		e.a.Emit(arm64.FMOV(bits, src), arm64.ANDI(out, bits, maskI32), arm64.MOVK(out, uint16(tagF32>>48), 48))
	case ssa.TypeF64:
		e.a.Emit(arm64.FMOV(out, src))
	default:
		return asm.VReg{}, false
	}
	return out, true
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
		return e.base, s.Index, true
	case ssa.SpaceGlobal:
		if s.Index < 0 || s.Index >= len(e.c.Input().Globals) || s.Index > maxSlot {
			return asm.VReg{}, 0, false
		}
		return e.pin(scratchGlobals), s.Index, true
	default:
		return asm.VReg{}, 0, false
	}
}

// forward reports whether control may branch along edge: to a block this
// compile has not laid out yet, carrying no arguments. A block already laid
// out is reached by a back edge, whose safepoint budget and loop-carried
// registers this machine does not emit, and an argument is a block parameter,
// whose edge copies it does not place.
func (e *emitter) forward(edge ssa.Edge) bool {
	return len(edge.Args) == 0 && edge.Block >= 0 && edge.Block < len(e.seen) && !e.seen[edge.Block]
}

func (e *emitter) lanes(want ssa.Type, vs ...ssa.Value) bool {
	for _, v := range vs {
		if lane(e.c.Func().Type(v)) != want {
			return false
		}
	}
	return true
}

func (e *emitter) zero32(v asm.VReg) asm.VReg {
	out := e.a.Reg(asm.RegTypeInt, asm.Width64)
	e.a.Emit(arm64.ANDI(out, v, maskI32))
	return out
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
// arithmetic here computes in: an i64, which needs the boxability guard this
// machine does not emit; a reference, which is moved rather than computed;
// and the interpreter state an OpState defines.
func lane(t ssa.Type) ssa.Type {
	switch t {
	case ssa.TypeI1, ssa.TypeI8, ssa.TypeI32:
		return ssa.TypeI32
	case ssa.TypeF32, ssa.TypeF64:
		return t
	default:
		return 0
	}
}

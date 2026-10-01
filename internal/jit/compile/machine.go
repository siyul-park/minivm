package compile

import (
	"errors"

	"github.com/siyul-park/minivm/internal/asm"
	"github.com/siyul-park/minivm/internal/jit"
	"github.com/siyul-park/minivm/internal/ssa"
	"github.com/siyul-park/minivm/types"
)

// Machine emits target rows for one function.
type Machine interface {
	// Arch returns the target assembler architecture.
	Arch() asm.Arch
	// Reserve returns registers reserved from allocation.
	Reserve() []asm.PReg
	// Prologue begins a function at address, whose shape is l; count
	// requests its entry hotness counter. A Machine lowers many functions in
	// sequence (a Queue worker reuses one), so Prologue resets all
	// per-function state. Prologue moves each l.Arguments entry's incoming
	// value into args, one register of its class per entry, and keeps
	// l.Results for OpReturn to consult.
	Prologue(a *asm.Assembler, address int, count bool, l Layout, args []asm.VReg)
	// Epilogue ends the native function.
	Epilogue(a *asm.Assembler)
	// Enter emits the Go entry stub after Epilogue and returns its label:
	// what Lower resolves to the native code's entry offset. l.Arguments and
	// l.Results are the function's register-convention parameters and
	// results, empty when none apply.
	Enter(a *asm.Assembler, l Layout) asm.Label
	// Lower emits op and reports false when the target cannot lower it.
	Lower(a *asm.Assembler, op ssa.Operation, s Site) bool
	// Branch transfers control to labels, one per edge of t. next is the
	// label bound right after the branch: an edge to it falls through.
	Branch(a *asm.Assembler, t ssa.Terminator, s Site, labels []asm.Label, next asm.Label)
	// Return ends the function with an OpReturn or OpComplete t.
	Return(a *asm.Assembler, t ssa.Terminator, s Site)
	// Budget counts one loop iteration down and branches to safepoint when
	// the budget is spent.
	Budget(a *asm.Assembler, safepoint asm.Label)
	// Exit leaves native code through exit id of kind k; uses must stay live
	// up to it. Control continues after Exit when the interpreter resumes.
	Exit(a *asm.Assembler, id int, k jit.Kind, uses []asm.VReg)
	// Spill stores reg into fixed spill slot slot.
	Spill(a *asm.Assembler, reg asm.VReg, slot int)
	// Results loads a bridge's results from the Context into regs.
	Results(a *asm.Assembler, regs []asm.VReg)
	// Call emits call site c and reports false when the target cannot.
	Call(a *asm.Assembler, c Call, s Site) bool
	// Move copies one virtual register to another.
	Move(a *asm.Assembler, dst, src asm.VReg)
	// Const loads word into dst, by dst's register bank. Reg calls it at
	// each use of a remat constant.
	Const(a *asm.Assembler, dst asm.VReg, word uint64)
}

// Layout is one function's shape for Prologue and Enter, built once in
// lowering.function.
type Layout struct {
	// Kinds is the function's slots, params first then locals.
	Kinds []types.Kind
	// Zeros is the word Prologue starts each of the trailing len(Zeros)
	// Kinds entries at (types.Zeros of the declared locals); nil for an OSR
	// unit, whose locals are live.
	Zeros []types.Boxed
	// Arguments is the function's register-convention parameters (see
	// arguments), empty when none apply.
	Arguments []types.Kind
	// Results is the function's register-convention results (see
	// registers), empty when none apply.
	Results []types.Kind
	// Borrows is transform.Borrows for a non-OSR unit and nil for an OSR
	// unit, whose threaded-entered frame owns every slot.
	Borrows []bool
	// Upvals reports that the function reads or writes its upvals: Prologue
	// loads their base from Context.Upvals.
	Upvals bool
}

// Site is what Machine sees of the operation or terminator it lowers.
type Site interface {
	// Reg returns the register assigned to v.
	Reg(v ssa.Value) asm.VReg
	// Type returns the static type of v.
	Type(v ssa.Value) ssa.Type
	// Slot returns the static type of slot.
	Slot(slot ssa.Slot) ssa.Type
	// Fuse reports whether v's only use is the condition of the OpBranch
	// right after its operation, so the machine may leave v in condition
	// flags instead of a register.
	Fuse(v ssa.Value) bool
	// Deopt returns the label of an exit that abandons native code at the
	// interpreter state of the operation.
	Deopt() asm.Label
	// Release returns the label of an exit that hands the interpreter ref,
	// whose last reference native code drops, and the label the machine
	// binds where native code resumes.
	Release(ref asm.VReg) (exit, resume asm.Label)
	// Box returns the label of an exit that hands the interpreter word, a
	// wide i64 to heap-box, and the label the machine binds where native
	// code resumes with the boxed ref in the same register as word.
	Box(word asm.VReg) (exit, resume asm.Label)
}

// Call describes a statically resolved CALL.
type Call struct {
	// Address is the callee's function address, its Context.Natives index.
	Address int
	// Callee is the function or closure reference the call adopts when
	// Owned; a borrowed Callee lives in the constant pool or a caller's
	// slot, and native code neither retains nor releases it.
	Callee        ssa.Value
	Args, Results []ssa.Value
	// Base is the callee's frame base in slots from this activation's, and
	// Size the slots from there the callee's frame must fit below
	// Context.Top.
	Base, Size int
	// Exit is the id of the map of the caller's state after the call; Live
	// are the registers it names, which stay live across the call.
	Exit int
	Live []asm.VReg
	// Bridge is the ExitCall the call takes when the callee is not native,
	// the activation is too deep, or the frame does not fit. The interpreter
	// runs the call itself, and native code resumes at Join.
	Bridge asm.Label
	// Safepoint is taken when the call spends the last budget unit.
	Safepoint asm.Label
	// Resume is the call site after the safepoint check.
	Resume asm.Label
	// Join is where Bridge resumes: past the callee's release, where slot
	// results are read, or past the moves of register results, which Bridge
	// loads itself.
	Join asm.Label
	// Owned reports whether the call's state owns Callee's reference, so
	// the call releases it once the callee returns.
	Owned bool
	// Self reports whether Address is the unit being lowered's own address
	// and the unit is not OSR (its entry is a loop header): the call branches
	// to its own entry instead of Context.Natives.
	Self bool
	// Registers is the callee's register-convention results (see registers),
	// empty when the callee returns through the VM frame. It is a static
	// fact of the callee's own types.Function, so caller and callee always
	// agree on it regardless of which unit or tier either compiles as.
	Registers []types.Kind
	// Arguments is the callee's register-convention parameters (see
	// arguments): each also moves into the target's register-convention
	// registers, on top of its slot store.
	Arguments []types.Kind
	// Upvals reports that Callee is a closure over a function with
	// captures: the call writes the closure's upvals base to
	// Context.Upvals.
	Upvals bool
}

// ErrUnsupported reports SSA the backend does not lower.
var ErrUnsupported = errors.New("unsupported lowering")

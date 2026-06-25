package program

import (
	"errors"
	"fmt"
	"slices"

	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// VerifyError reports the first violation found in a program, located by
// function slot (0 = top-level code, j+1 = constant j, matching the
// interpreter's compiled code layout) and instruction byte offset.
type VerifyError struct {
	Slot   int
	IP     int
	Opcode instr.Opcode
	Err    error
}

// Option configures verification context not carried by the Program itself.
type Option func(*config)

type config struct {
	extensions map[uint8]bool
}

// checker verifies one function slot: it proves structural integrity (decode,
// operand bounds, control flow, termination) and then, where the bytecode is
// statically determinable, that the operand stack stays balanced and
// type-consistent.
type checker struct {
	prog *Program
	cfg  *config

	fn       *types.Function
	code     []byte
	locals   []types.Type
	captures []types.Type

	slot    int
	returns int
}

// block is one node of the control-flow graph: a maximal straight-line run of
// code [start, end) plus the indices of its successor and predecessor blocks.
type block struct {
	start int
	end   int
	succs []int
	preds []int
}

// slot is one abstract operand-stack entry: its kind plus, when the entry
// definitely holds a function or closure reference, that reference's signature.
// The signature lets CALL recover a callee's arity statically even though the
// bytecode carries no call type operand.
type slot struct {
	kind types.Kind
	fn   *types.FunctionType
}

// stack is the abstract operand stack threaded through a basic block.
type stack struct {
	slots []slot
}

// anyKind is the verifier's top element over types.Kind: a slot whose concrete
// kind could not be determined statically (a dynamic load, a host result, or a
// control-flow merge of two different kinds). anyKind satisfies any required
// kind and absorbs any merge, so the verifier never rejects on an unknown kind —
// only on a definite disagreement between two concrete kinds.
const anyKind = instr.KindAny

var (
	ErrTruncated        = errors.New("truncated instruction")
	ErrUnknownOpcode    = errors.New("unknown opcode")
	ErrUnknownExtension = errors.New("unknown extension")
	ErrIndexOutOfRange  = errors.New("operand index out of range")
	ErrStackUnderflow   = errors.New("stack underflow")
	ErrStackMismatch    = errors.New("stack mismatch at control-flow merge")
	ErrTypeMismatch     = errors.New("operand type mismatch")
	ErrFallThrough      = errors.New("control falls off end of function")
	ErrInvalidJump      = errors.New("invalid jump")
	ErrHandlerRange     = errors.New("invalid exception handler range")
	ErrHandlerTarget    = errors.New("invalid exception handler target")
)

func newChecker(prog *Program, cfg *config, slot int, fn *types.Function) *checker {
	c := &checker{prog: prog, cfg: cfg, fn: fn, code: fn.Code, captures: fn.Captures, slot: slot}
	if fn.Typ != nil {
		c.locals = append(c.locals, fn.Typ.Params...)
		c.returns = len(fn.Typ.Returns)
	}
	c.locals = append(c.locals, fn.Locals...)
	return c
}

// Verify checks every function slot of prog and returns the first violation as
// a *VerifyError, or nil when the program is well-formed.
func Verify(prog *Program, opts ...Option) error {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	top := &types.Function{Typ: &types.FunctionType{}, Code: prog.Code, Handlers: prog.Handlers}
	if err := newChecker(prog, cfg, 0, top).run(); err != nil {
		return err
	}
	for j, c := range prog.Constants {
		fn, ok := c.(*types.Function)
		if !ok {
			continue
		}
		if err := newChecker(prog, cfg, j+1, fn).run(); err != nil {
			return err
		}
	}
	return nil
}

// WithExtensions registers the extension ids the program may invoke via EXT.
// When set, an EXT to an unregistered id is rejected; when unset, EXT ids are
// not checked because the registry is unknown to the verifier.
func WithExtensions(ids ...uint8) Option {
	return func(c *config) {
		if c.extensions == nil {
			c.extensions = make(map[uint8]bool, len(ids))
		}
		for _, id := range ids {
			c.extensions[id] = true
		}
	}
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("verify: slot %d, ip %d, %s: %v", e.Slot, e.IP, instr.TypeOf(e.Opcode).Mnemonic, e.Err)
}

func (e *VerifyError) Unwrap() error {
	return e.Err
}

// run verifies the slot in four passes: structural decode/bounds, control-flow
// graph (branch targets), termination, then the best-effort stack dataflow.
func (c *checker) run() error {
	if len(c.code) == 0 {
		return nil
	}
	if err := c.structure(); err != nil {
		return err
	}
	if err := c.handlers(); err != nil {
		return err
	}
	blocks, err := c.blocks()
	if err != nil {
		return err
	}
	if err := c.terminate(blocks); err != nil {
		return err
	}
	return c.flow(blocks)
}

// handlers validates the exception table: each protected region is a non-empty
// in-bounds range whose start, end, and catch land on instruction boundaries.
// It runs before the CFG is built so a malformed table cannot split blocks
// mid-instruction.
func (c *checker) handlers() error {
	if len(c.fn.Handlers) == 0 {
		return nil
	}
	starts := c.starts()
	for _, h := range c.fn.Handlers {
		if h.Start < 0 || h.End > len(c.code) || h.Start >= h.End || !starts[h.Start] || !starts[h.End] {
			return c.fail(h.Start, instr.THROW, ErrHandlerRange)
		}
		if h.Catch < 0 || h.Catch >= len(c.code) || !starts[h.Catch] {
			return c.fail(h.Catch, instr.THROW, ErrHandlerTarget)
		}
	}
	return nil
}

// starts is the set of byte offsets that begin an instruction, plus the
// past-the-end offset that closes the final protected region.
func (c *checker) starts() map[int]bool {
	starts := map[int]bool{len(c.code): true}
	for ip := 0; ip < len(c.code); {
		starts[ip] = true
		w, ok := c.width(ip)
		if !ok {
			break
		}
		ip += w
	}
	return starts
}

// structure walks the code once, proving every instruction decodes within
// bounds, names a defined opcode, and carries in-range pool/local/upval indices.
func (c *checker) structure() error {
	for ip := 0; ip < len(c.code); {
		op := instr.Opcode(c.code[ip])
		if !instr.Valid(op) {
			return c.fail(ip, op, ErrUnknownOpcode)
		}
		width, ok := c.width(ip)
		if !ok || ip+width > len(c.code) {
			return c.fail(ip, op, ErrTruncated)
		}
		if err := c.bounds(ip, op); err != nil {
			return err
		}
		ip += width
	}
	return nil
}

func (c *checker) bounds(ip int, op instr.Opcode) error {
	inst := instr.Instruction(c.code[ip:])
	switch op {
	case instr.CONST_GET:
		if int(inst.Operand(0)) >= len(c.prog.Constants) {
			return c.fail(ip, op, ErrIndexOutOfRange)
		}
	case instr.REF_TEST, instr.REF_CAST,
		instr.ARRAY_NEW, instr.ARRAY_NEW_DEFAULT,
		instr.STRUCT_NEW, instr.STRUCT_NEW_DEFAULT,
		instr.MAP_NEW, instr.MAP_NEW_DEFAULT:
		if int(inst.Operand(0)) >= len(c.prog.Types) {
			return c.fail(ip, op, ErrIndexOutOfRange)
		}
	case instr.LOCAL_GET, instr.LOCAL_SET, instr.LOCAL_TEE:
		if int(inst.Operand(0)) >= len(c.locals) {
			return c.fail(ip, op, ErrIndexOutOfRange)
		}
	case instr.UPVAL_GET, instr.UPVAL_SET:
		if int(inst.Operand(0)) >= len(c.captures) {
			return c.fail(ip, op, ErrIndexOutOfRange)
		}
	case instr.EXT:
		if id := uint8(inst.Operand(0) >> 8); c.cfg.extensions != nil && !c.cfg.extensions[id] {
			return c.fail(ip, op, ErrUnknownExtension)
		}
	}
	return nil
}

// blocks builds the control-flow graph, splitting the code at branch targets,
// terminators, and protected-region boundaries, and validates that every branch
// target lands on an instruction boundary inside the function. Throws and traps
// transfer out of band, so no edges are added for them; the flow pass seeds
// catch blocks directly.
func (c *checker) blocks() ([]*block, error) {
	offsets := []int{0}
	mark := func(ip, target int) error {
		if target < 0 || target >= len(c.code) {
			return c.fail(ip, instr.Opcode(c.code[ip]), ErrInvalidJump)
		}
		offsets = append(offsets, target)
		return nil
	}
	for ip := 0; ip < len(c.code); {
		inst := instr.Instruction(c.code[ip:])
		next := ip + inst.Width()
		switch inst.Opcode() {
		case instr.UNREACHABLE, instr.RETURN, instr.THROW:
			if next < len(c.code) {
				offsets = append(offsets, next)
			}
		case instr.BR, instr.BR_IF:
			if err := mark(ip, next+instr.ReadI16(inst.Operand(0))); err != nil {
				return nil, err
			}
			if next < len(c.code) {
				offsets = append(offsets, next)
			}
		case instr.BR_TABLE:
			operands := inst.Operands()
			for j := 1; j < len(operands); j++ {
				if err := mark(ip, next+instr.ReadI16(operands[j])); err != nil {
					return nil, err
				}
			}
		}
		ip = next
	}
	for _, h := range c.fn.Handlers {
		for _, off := range []int{h.Start, h.End, h.Catch} {
			if off > 0 && off < len(c.code) {
				offsets = append(offsets, off)
			}
		}
	}

	slices.Sort(offsets)
	offsets = slices.Compact(offsets)

	blocks := make([]*block, len(offsets))
	for j := range offsets {
		end := len(c.code)
		if j+1 < len(offsets) {
			end = offsets[j+1]
		}
		blocks[j] = &block{start: offsets[j], end: end}
	}
	for j, b := range blocks {
		op, ip := c.last(b)
		inst := instr.Instruction(c.code[ip:])
		switch op {
		case instr.UNREACHABLE, instr.RETURN, instr.THROW:
		case instr.BR:
			if err := c.link(blocks, j, ip+inst.Width()+instr.ReadI16(inst.Operand(0))); err != nil {
				return nil, err
			}
		case instr.BR_IF:
			if err := c.link(blocks, j, ip+inst.Width()+instr.ReadI16(inst.Operand(0))); err != nil {
				return nil, err
			}
			c.chain(blocks, j)
		case instr.BR_TABLE:
			operands := inst.Operands()
			for k := 1; k < len(operands); k++ {
				if err := c.link(blocks, j, ip+inst.Width()+instr.ReadI16(operands[k])); err != nil {
					return nil, err
				}
			}
		default:
			c.chain(blocks, j)
		}
	}
	for _, b := range blocks {
		slices.Sort(b.succs)
		slices.Sort(b.preds)
	}
	return blocks, nil
}

// link records an edge from block src to the block containing dst, failing when
// dst is not the start of any block (a jump into the middle of an instruction).
func (c *checker) link(blocks []*block, src, dst int) error {
	for i, b := range blocks {
		if b.start <= dst && dst < b.end {
			blocks[src].succs = append(blocks[src].succs, i)
			blocks[i].preds = append(blocks[i].preds, src)
			return nil
		}
	}
	return c.fail(blocks[src].start, instr.Opcode(c.code[blocks[src].start]), ErrInvalidJump)
}

// chain links block j to its textual successor j+1.
func (c *checker) chain(blocks []*block, j int) {
	if j+1 < len(blocks) {
		blocks[j].succs = append(blocks[j].succs, j+1)
		blocks[j+1].preds = append(blocks[j+1].preds, j)
	}
}

// terminate requires every exit block of a function body to end in a real
// terminator. Top-level code (slot 0) is exempt: the interpreter ends it by
// running off the end of the code, so a trailing fall-through is expected.
func (c *checker) terminate(blocks []*block) error {
	if c.slot == 0 {
		return nil
	}
	for _, b := range blocks {
		if len(b.succs) > 0 {
			continue
		}
		op, ip := c.last(b)
		switch op {
		case instr.RETURN, instr.RETURN_CALL, instr.UNREACHABLE, instr.THROW:
		default:
			return c.fail(ip, op, ErrFallThrough)
		}
	}
	return nil
}

// flow runs an abstract interpretation of the operand stack over the CFG to a
// fixpoint, checking for underflow, operand type confusion, and height
// disagreement at merges. When an instruction's stack effect cannot be
// determined statically (a dynamic-arity CALL, a stack-counted MAP_NEW, an
// extension op), it stops without a verdict: the structural passes already
// hold, and the interpreter guards the rest at runtime.
func (c *checker) flow(blocks []*block) error {
	entries := make([]*stack, len(blocks))
	entries[0] = &stack{}
	work := []int{0}

	// Catch blocks are reached out of band, not via a CFG edge, so seed each as
	// its own root: the operand stack restored to the protected region's entry
	// depth (Handler.Depth is sp-bp, so subtract the fixed locals area) plus the
	// delivered exception value on top.
	for _, h := range c.fn.Handlers {
		operands := h.Depth - len(c.locals)
		if operands < 0 {
			operands = 0
		}
		seed := &stack{}
		for k := 0; k < operands+1; k++ {
			seed.push(slot{kind: anyKind})
		}
		idx := -1
		for i, b := range blocks {
			if b.start == h.Catch {
				idx = i
				break
			}
		}
		if idx < 0 {
			continue
		}
		if entries[idx] == nil {
			entries[idx] = seed
			work = append(work, idx)
		} else if _, balanced := entries[idx].merge(seed); !balanced {
			return c.fail(h.Catch, instr.THROW, ErrStackMismatch)
		}
	}

	for len(work) > 0 {
		i := work[len(work)-1]
		work = work[:len(work)-1]

		st := entries[i].clone()
		done, err := c.exec(blocks[i], st)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		for _, s := range blocks[i].succs {
			if entries[s] == nil {
				entries[s] = st.clone()
				work = append(work, s)
				continue
			}
			changed, balanced := entries[s].merge(st)
			if !balanced {
				return c.fail(blocks[s].start, instr.Opcode(c.code[blocks[s].start]), ErrStackMismatch)
			}
			if changed {
				work = append(work, s)
			}
		}
	}
	return nil
}

// exec simulates one block's stack effect, returning done when an
// indeterminate op halts the dataflow.
func (c *checker) exec(b *block, st *stack) (bool, error) {
	for ip := b.start; ip < b.end; {
		op := instr.Opcode(c.code[ip])
		done, err := c.step(st, instr.Instruction(c.code[ip:]), op, ip)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}
		if op == instr.RETURN || op == instr.RETURN_CALL || op == instr.UNREACHABLE || op == instr.THROW {
			return false, nil
		}
		width, _ := c.width(ip)
		ip += width
	}
	return false, nil
}

// step applies one instruction's effect to st. Most opcodes use the fixed stack
// effect carried by instr.Type; the rest read program context (constants,
// locals, declared types, the callee signature) or report an indeterminate
// effect.
func (c *checker) step(st *stack, inst instr.Instruction, op instr.Opcode, ip int) (bool, error) {
	switch op {
	case instr.NOP, instr.UNREACHABLE, instr.BR:
		return false, nil
	case instr.LOCAL_TEE, instr.GLOBAL_TEE:
		if st.len() == 0 {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		return false, nil
	case instr.LOCAL_GET:
		t := c.locals[inst.Operand(0)]
		st.push(slot{kind: t.Kind(), fn: signatureOf(t)})
		return false, nil
	case instr.UPVAL_GET:
		t := c.captures[inst.Operand(0)]
		st.push(slot{kind: t.Kind(), fn: signatureOf(t)})
		return false, nil
	case instr.CONST_GET:
		v := c.prog.Constants[inst.Operand(0)]
		st.push(slot{kind: v.Kind(), fn: signatureOf(v.Type())})
		return false, nil
	case instr.DUP:
		if st.len() == 0 {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		st.push(st.top())
		return false, nil
	case instr.SWAP:
		if st.len() < 2 {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		st.swap()
		return false, nil
	case instr.SELECT:
		if st.len() < 3 {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		st.pop()
		b := st.pop()
		a := st.pop()
		st.push(slot{kind: unify(a.kind, b.kind)})
		return false, nil
	case instr.I32_AND, instr.I32_OR, instr.I32_XOR:
		if st.len() < 2 {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		b := st.pop()
		a := st.pop()
		if !accepts(a.kind, types.KindI32) || !accepts(b.kind, types.KindI32) {
			return false, c.fail(ip, op, ErrTypeMismatch)
		}
		st.push(slot{kind: bitwise(a.kind, b.kind)})
		return false, nil
	case instr.CALL:
		return c.call(st, ip, op, false)
	case instr.RETURN_CALL:
		return c.call(st, ip, op, true)
	case instr.RETURN:
		if st.len() < c.returns {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		return false, nil
	case instr.STRUCT_NEW:
		t, ok := c.prog.Types[inst.Operand(0)].(*types.StructType)
		if !ok {
			return true, nil
		}
		if st.len() < len(t.Fields) {
			return false, c.fail(ip, op, ErrStackUnderflow)
		}
		st.drop(len(t.Fields))
		st.push(slot{kind: types.KindRef})
		return false, nil
	case instr.MAP_NEW, instr.CLOSURE_NEW, instr.EXT:
		return true, nil
	}

	t := inst.Type()
	if t.Pop == nil && t.Push == nil {
		return true, nil
	}
	if st.len() < len(t.Pop) {
		return false, c.fail(ip, op, ErrStackUnderflow)
	}
	for _, want := range t.Pop {
		if got := st.pop(); !accepts(got.kind, want) {
			return false, c.fail(ip, op, ErrTypeMismatch)
		}
	}
	for _, k := range t.Push {
		st.push(slot{kind: k})
	}
	return false, nil
}

// call resolves a CALL or RETURN_CALL: it pops the callee reference and, when
// the callee signature is statically known, the arguments, pushing the
// declared results. A dynamic target whose arity is unknown stops the dataflow.
func (c *checker) call(st *stack, ip int, op instr.Opcode, tail bool) (bool, error) {
	if st.len() == 0 {
		return false, c.fail(ip, op, ErrStackUnderflow)
	}
	fn := st.pop().fn
	if fn == nil {
		return true, nil
	}
	if st.len() < len(fn.Params) {
		return false, c.fail(ip, op, ErrStackUnderflow)
	}
	st.drop(len(fn.Params))
	if tail {
		return false, nil
	}
	for _, r := range fn.Returns {
		st.push(slot{kind: r.Kind()})
	}
	return false, nil
}

// width returns the bounds-checked byte length of the instruction at ip,
// reporting ok=false when a variable-width count byte reaches past the code.
func (c *checker) width(ip int) (int, bool) {
	off := 1
	for _, w := range instr.TypeOf(instr.Opcode(c.code[ip])).Widths {
		if w > 0 {
			off += w
			continue
		}
		if ip+off >= len(c.code) {
			return 0, false
		}
		off += 1 + int(c.code[ip+off])*(-w)
	}
	return off, true
}

func (c *checker) last(b *block) (instr.Opcode, int) {
	ip := b.start
	for ip < b.end {
		width, _ := c.width(ip)
		if ip+width >= b.end {
			break
		}
		ip += width
	}
	return instr.Opcode(c.code[ip]), ip
}

func (c *checker) fail(ip int, op instr.Opcode, err error) error {
	return &VerifyError{Slot: c.slot, IP: ip, Opcode: op, Err: err}
}

func (s *stack) len() int {
	return len(s.slots)
}

func (s *stack) top() slot {
	return s.slots[len(s.slots)-1]
}

func (s *stack) push(v slot) {
	s.slots = append(s.slots, v)
}

func (s *stack) pop() slot {
	v := s.slots[len(s.slots)-1]
	s.slots = s.slots[:len(s.slots)-1]
	return v
}

func (s *stack) drop(n int) {
	s.slots = s.slots[:len(s.slots)-n]
}

func (s *stack) swap() {
	n := len(s.slots)
	s.slots[n-1], s.slots[n-2] = s.slots[n-2], s.slots[n-1]
}

func (s *stack) clone() *stack {
	return &stack{slots: append([]slot(nil), s.slots...)}
}

// merge folds other's slots into s at a control-flow join, reporting whether s
// changed (so the successor must be re-evaluated) and whether the two states
// agree on height. A height disagreement is a structural defect; differing
// kinds widen to anyKind rather than failing.
func (s *stack) merge(other *stack) (changed, balanced bool) {
	if len(s.slots) != len(other.slots) {
		return false, false
	}
	for i := range s.slots {
		if k := unify(s.slots[i].kind, other.slots[i].kind); k != s.slots[i].kind {
			s.slots[i].kind = k
			changed = true
		}
		if s.slots[i].fn != nil && s.slots[i].fn != other.slots[i].fn {
			s.slots[i].fn = nil
			changed = true
		}
	}
	return changed, true
}

// signatureOf returns t as a *FunctionType when t is one, so a slot carrying a
// function/closure reference remembers its arity; otherwise nil.
func signatureOf(t types.Type) *types.FunctionType {
	if ft, ok := t.(*types.FunctionType); ok {
		return ft
	}
	return nil
}

// accepts reports whether an actual stack slot of kind got satisfies a required
// operand kind want. anyKind on either side unifies: the verifier stays
// permissive about unknowns and strict only about concrete disagreement.
// Kinds are compared by representation (Repr), so a narrow integer (i1, i8)
// satisfies an i32 operand — they share the i32 representation.
func accepts(got, want types.Kind) bool {
	return got == anyKind || want == anyKind || got.Repr() == want.Repr()
}

// unify folds two slot kinds at a control-flow join: equal kinds survive,
// anything else widens to anyKind.
func unify(a, b types.Kind) types.Kind {
	if a == b {
		return a
	}
	return anyKind
}

// bitwise computes the result kind of a width-closed integer op (and/or/xor):
// when both operands share one narrow kind the result keeps it (i8&i8 → i8,
// i1&i1 → i1, matching Rust/Swift), since the operation cannot escape that
// kind's value range. A mixed pair widens to the i32 representation they
// compute in; an unknown operand yields anyKind because narrowness cannot be
// proven.
func bitwise(a, b types.Kind) types.Kind {
	if a == b {
		return a
	}
	if a == anyKind || b == anyKind {
		return anyKind
	}
	return types.KindI32
}

package verify

import (
	"errors"
	"fmt"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/program"
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
	prog *program.Program
	cfg  *config

	fn       *types.Function
	code     []byte
	locals   []types.Type
	captures []types.Type

	slot    int
	returns int
}

var (
	ErrTruncated        = errors.New("truncated instruction")
	ErrUnknownOpcode    = errors.New("unknown opcode")
	ErrUnknownExtension = errors.New("unknown extension")
	ErrIndexOutOfRange  = errors.New("operand index out of range")
	ErrStackUnderflow   = errors.New("stack underflow")
	ErrStackMismatch    = errors.New("stack mismatch at control-flow merge")
	ErrTypeMismatch     = errors.New("operand type mismatch")
	ErrFallThrough      = errors.New("control falls off end of function")
)

func newChecker(prog *program.Program, cfg *config, slot int, fn *types.Function) *checker {
	c := &checker{prog: prog, cfg: cfg, fn: fn, code: fn.Code, captures: fn.Captures, slot: slot}
	if fn.Typ != nil {
		c.locals = append(c.locals, fn.Typ.Params...)
		c.returns = len(fn.Typ.Returns)
	}
	c.locals = append(c.locals, fn.Locals...)
	return c
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

// Verify checks every function slot of prog and returns the first violation as
// a *VerifyError, or nil when the program is well-formed.
func Verify(prog *program.Program, opts ...Option) error {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}

	top := &types.Function{Typ: &types.FunctionType{}, Code: prog.Code}
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
	blocks, err := c.blocks()
	if err != nil {
		return err
	}
	if err := c.terminate(blocks); err != nil {
		return err
	}
	return c.flow(blocks)
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

// blocks builds the control-flow graph, which also validates that every branch
// target lands on an instruction boundary inside the function.
func (c *checker) blocks() ([]*analysis.BasicBlock, error) {
	m := pass.NewManager()
	pass.Register(m, analysis.NewBasicBlocksAnalysis())
	blocks, err := pass.GetResult[[]*analysis.BasicBlock](m, c.fn)
	if err != nil {
		return nil, &VerifyError{Slot: c.slot, IP: -1, Err: err}
	}
	return blocks, nil
}

// terminate requires every exit block of a function body to end in a real
// terminator. Top-level code (slot 0) is exempt: the interpreter ends it by
// running off the end of the code, so a trailing fall-through is expected.
func (c *checker) terminate(blocks []*analysis.BasicBlock) error {
	if c.slot == 0 {
		return nil
	}
	for _, b := range blocks {
		if len(b.Succs) > 0 {
			continue
		}
		op, ip := c.last(b)
		switch op {
		case instr.RETURN, instr.RETURN_CALL, instr.UNREACHABLE:
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
func (c *checker) flow(blocks []*analysis.BasicBlock) error {
	entries := make([]*stack, len(blocks))
	entries[0] = &stack{}
	work := []int{0}
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
		for _, s := range blocks[i].Succs {
			if entries[s] == nil {
				entries[s] = st.clone()
				work = append(work, s)
				continue
			}
			changed, balanced := entries[s].merge(st)
			if !balanced {
				return c.fail(blocks[s].Start, instr.Opcode(c.code[blocks[s].Start]), ErrStackMismatch)
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
func (c *checker) exec(b *analysis.BasicBlock, st *stack) (bool, error) {
	for ip := b.Start; ip < b.End; {
		op := instr.Opcode(c.code[ip])
		done, err := c.step(st, instr.Instruction(c.code[ip:]), op, ip)
		if err != nil {
			return false, err
		}
		if done {
			return true, nil
		}
		if op == instr.RETURN || op == instr.RETURN_CALL || op == instr.UNREACHABLE {
			return false, nil
		}
		width, _ := c.width(ip)
		ip += width
	}
	return false, nil
}

// step applies one instruction's effect to st. Most opcodes use the fixed
// signature table; the rest read program context (constants, locals, declared
// types, the callee signature) or report an indeterminate effect.
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

	pops, pushes, ok := signature(op)
	if !ok {
		return true, nil
	}
	if st.len() < len(pops) {
		return false, c.fail(ip, op, ErrStackUnderflow)
	}
	for _, want := range pops {
		if got := st.pop(); !accepts(got.kind, want) {
			return false, c.fail(ip, op, ErrTypeMismatch)
		}
	}
	for _, k := range pushes {
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

func (c *checker) last(b *analysis.BasicBlock) (instr.Opcode, int) {
	ip := b.Start
	for ip < b.End {
		width, _ := c.width(ip)
		if ip+width >= b.End {
			break
		}
		ip += width
	}
	return instr.Opcode(c.code[ip]), ip
}

func (c *checker) fail(ip int, op instr.Opcode, err error) error {
	return &VerifyError{Slot: c.slot, IP: ip, Opcode: op, Err: err}
}

package interp

import (
	"sort"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// lowerer is the architecture-specific opcode emitter. jitCompiler drives
// the arch-neutral pipeline — block planning, segment selection, linking —
// and delegates every native-code emission to a lowerer, so the same
// compiler serves any target that implements these operations.
type lowerer interface {
	// prologue loads declared live-ins from the VM stack into segment inputs.
	prologue(ctx *jitContext, fn *types.Function)
	// enter emits the entry sequence (frame/link save) for a whole-function
	// target reached as its own callable.
	enter(ctx *jitContext)
	// lower emits one opcode, advancing ctx; it returns false to reject.
	lower(ctx *jitContext, op instr.Opcode) bool
	// exitIP emits an interpreter exit that resumes threaded dispatch at nextIP.
	exitIP(ctx *jitContext, nextIP int)
}

type jitCompiler struct {
	lowerer lowerer
	arch    asm.Arch
	buffer  *asm.Buffer

	cutoff int
}

type jitModule struct {
	entry    asm.Callable
	entries  map[int]asm.Callable
	segments map[jitEntry]asm.Callable
	stacks   map[jitEntry]int
	bytes    []int
	links    int
	skips    int
}

type jitEntry struct {
	addr int
	ip   int
}

type segment struct {
	addr  int
	start int
	code  *asm.Code
	stack int
	next  int
	force bool
}

type result struct {
	count  int
	reject int
}

type jitTarget struct {
	label   asm.Label
	fn      *types.Function
	blocks  []*analysis.BasicBlock
	labels  map[int]asm.Label
	returns int
}

type jitContext struct {
	assembler *asm.Assembler
	code      []byte
	constants []types.Boxed
	globals   []types.Boxed
	locals    []types.Kind
	scratch   []asm.PReg
	entry     asm.Label
	labels    map[int]asm.Label
	targets   map[int]jitTarget
	stack     []asm.VReg
	inputs    []asm.VReg

	ip        int
	end       int
	successor int
	stop      bool
	closed    bool

	whole   bool
	framed  bool
	addr    int
	returns int
}

const (
	scratchStack = iota
	scratchGlobals
	scratchBP
	scratchSP
	scratchCtrl
	scratchCount
)

// Frame-journal layout. scratchCtrl carries &journal[0] — an Interpreter-owned
// []uint64 that lets native code push a recoverable VM frame per direct call and
// report deopt state back to the Go wrapper. Header cells precede a stack of
// fixed-stride frame records; each record mirrors the int fields the threaded
// interpreter needs to resume a frame.
const (
	journalDepth  = iota // native frames recorded; native read/write
	journalCap           // frame budget len(i.frames)-i.fp; read-only
	journalTrap          // exit kind out: trapNone | trapFallback | trapOverflow | trapYield
	journalNextIP        // resume/fallback IP out for the single-frame path
	journalBudget        // back-edges remaining before the next safepoint; native read/write
	journalHead          // first frame record cell
)

const journalStride = 4

const (
	recordAddr = iota
	recordBP
	recordIP
	recordReturns
)

const (
	trapNone = iota
	trapFallback
	trapOverflow
	trapYield
)

func (c *jitCompiler) Close() error {
	return c.buffer.Free()
}

// Compile attempts to lower fn into native code. Hot profile IPs seed segment
// selection; rejected or short segments fall back to threaded dispatch.
func (c *jitCompiler) Compile(i *Interpreter, addr int, fn *types.Function) (*jitModule, error) {
	mod := &jitModule{
		entries:  map[int]asm.Callable{},
		segments: map[jitEntry]asm.Callable{},
		stacks:   map[jitEntry]int{},
	}
	if fn == nil || len(fn.Code) == 0 {
		return mod, nil
	}
	if addr > 0 {
		if ok, err := c.complete(i, addr, fn, mod); ok || err != nil {
			return mod, err
		}
	}
	locals := fn.LocalKinds()

	seg, ok, err := c.function(i, fn, locals, []*analysis.BasicBlock{{Start: 0, End: len(fn.Code)}}, true)
	if err != nil {
		return nil, err
	}
	if ok {
		if err := c.linkEntry(mod, seg); err != nil {
			return nil, err
		}
		return mod, nil
	}

	seg, blks, ok, err := c.blocks(i, fn, locals)
	if err != nil {
		return nil, err
	}
	if ok {
		if err := c.linkEntry(mod, seg); err != nil {
			return nil, err
		}
		if err := c.reenter(i, addr, fn, locals, blks, mod); err != nil {
			return nil, err
		}
		return mod, nil
	}

	var segs []segment
	for targetAddr, targetFn := range c.component(i, addr, fn) {
		targetSegs, err := c.segments(i, targetAddr, targetFn, targetFn.LocalKinds(), mod)
		if err != nil {
			return nil, err
		}
		segs = append(segs, targetSegs...)
	}
	if len(segs) == 0 {
		return mod, nil
	}
	if err := c.link(mod, segs); err != nil {
		return nil, err
	}
	return mod, nil
}

func (c *jitCompiler) linkEntry(mod *jitModule, seg segment) error {
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{seg.code}, nil)
	if err != nil {
		return err
	}
	mod.entry = linked[0].Callable
	mod.bytes = append(mod.bytes, len(seg.code.Bytes))
	mod.links = 1
	return nil
}

func (c *jitCompiler) complete(i *Interpreter, addr int, fn *types.Function, mod *jitModule) (bool, error) {
	scratch, ok := c.scratch()
	if !ok {
		return false, nil
	}
	funcs := c.component(i, addr, fn)
	targets, ok, err := c.targets(funcs)
	if err != nil || !ok {
		return false, err
	}

	a := asm.New(c.arch)
	for targetAddr, target := range targets {
		target.labels = c.allocLabels(a, target.blocks)
		target.label = target.labels[0]
		targets[targetAddr] = target
	}
	order := []int{addr}
	for targetAddr := range funcs {
		if targetAddr != addr {
			order = append(order, targetAddr)
		}
	}
	for _, targetAddr := range order {
		target := targets[targetAddr]
		ctx := c.newContext(a, i, target.fn, target.fn.LocalKinds(), 0, scratch)
		ctx.whole = true
		ctx.framed = true
		ctx.addr = targetAddr
		ctx.labels = target.labels
		ctx.targets = targets
		ctx.returns = target.returns
		if !c.walkBlocks(ctx, target.fn, target.blocks, target.returns, true) {
			return false, nil
		}
	}

	code, err := a.Build(asm.Signature{Scratch: scratch})
	if err != nil {
		return false, err
	}
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{code}, nil)
	if err != nil {
		return false, err
	}
	mod.bytes = append(mod.bytes, len(code.Bytes))
	mod.links = len(targets)
	for targetAddr, target := range targets {
		callable, ok := linked[0].Entries[target.label]
		if !ok {
			if targetAddr != addr {
				return false, asm.ErrUnresolvedLabel
			}
			callable = linked[0].Callable
		}
		mod.entries[targetAddr] = callable
	}
	for targetAddr, target := range targets {
		if err := c.reenter(i, targetAddr, target.fn, target.fn.LocalKinds(), target.blocks, mod); err != nil {
			return false, err
		}
	}
	return true, nil
}

func (c *jitCompiler) targets(funcs map[int]*types.Function) (map[int]jitTarget, bool, error) {
	out := make(map[int]jitTarget, len(funcs))
	for addr, fn := range funcs {
		if !c.eligible(fn) {
			return nil, false, nil
		}
		m := pass.NewManager()
		if err := m.Run(fn); err != nil {
			return nil, false, err
		}
		blocks, err := analysis.NewBasicBlocksPass().Run(m)
		if err != nil {
			return nil, false, err
		}
		returns := 0
		if fn.Typ != nil {
			returns = len(fn.Typ.Returns)
		}
		out[addr] = jitTarget{
			fn:      fn,
			blocks:  blocks,
			returns: returns,
		}
	}
	return out, true, nil
}

func (c *jitCompiler) eligible(fn *types.Function) bool {
	if fn == nil || fn.Typ == nil {
		return false
	}
	for _, typ := range fn.Typ.Params {
		if typ.Kind() == types.KindRef {
			return false
		}
	}
	for _, typ := range fn.Typ.Returns {
		if typ.Kind() == types.KindRef {
			return false
		}
	}
	for _, typ := range fn.Locals {
		if typ.Kind() == types.KindRef {
			return false
		}
	}
	if len(fn.Captures) > 0 {
		return false
	}
	return true
}

// blocks lowers fn as a sequence of basic blocks connected by
// intra-function labels. Branches (BR_IF, BR) become native branches to
// block-start labels instead of interpreter exits. It also returns the blocks
// so the caller can install loop re-entry segments. Returns (_, _, false, _)
// when a target is not a known block start (e.g. brTable), so segments()
// can handle those cases.
func (c *jitCompiler) blocks(i *Interpreter, fn *types.Function, locals []types.Kind) (segment, []*analysis.BasicBlock, bool, error) {
	m := pass.NewManager()
	if err := m.Run(fn); err != nil {
		return segment{}, nil, false, err
	}
	blks, err := analysis.NewBasicBlocksPass().Run(m)
	if err != nil || len(blks) <= 1 {
		return segment{}, nil, false, nil
	}
	seg, ok, err := c.function(i, fn, locals, blks, false)
	return seg, blks, ok, err
}

// function runs two walks over blks: a feasibility walk that rejects when any
// opcode cannot lower, then an emit walk that produces real code. When
// requireTerminated is true the feasibility walk also checks that the last
// lowered opcode left ctx in a RETURN-terminated state. Labels are bound per
// block only when len(blks) > 1; with a single synthetic block the function
// behaves as a straight-line whole-function compilation.
func (c *jitCompiler) function(i *Interpreter, fn *types.Function, locals []types.Kind, blks []*analysis.BasicBlock, requireTerminated bool) (segment, bool, error) {
	scratch, ok := c.scratch()
	if !ok {
		return segment{}, false, nil
	}
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}
	ctx := c.prepare(i, fn, locals, scratch, blks)
	if !c.walkBlocks(ctx, fn, blks, returns, false) {
		return segment{}, false, nil
	}
	if requireTerminated && !ctx.terminated() {
		return segment{}, false, nil
	}
	if !c.valid(ctx, returns) {
		return segment{}, false, nil
	}

	// Feasibility confirmed; redo on a fresh assembler, this time emitting.
	ctx = c.prepare(i, fn, locals, scratch, blks)
	if !c.walkBlocks(ctx, fn, blks, returns, true) {
		return segment{}, false, nil
	}
	if !c.valid(ctx, returns) {
		return segment{}, false, nil
	}

	code, err := ctx.assembler.Build(asm.Signature{Scratch: scratch})
	if err != nil {
		return segment{}, false, err
	}
	return segment{start: 0, code: code}, true, nil
}

// allocLabels assigns a fresh assembler label to each block start.
func (c *jitCompiler) allocLabels(a *asm.Assembler, blocks []*analysis.BasicBlock) map[int]asm.Label {
	labels := make(map[int]asm.Label, len(blocks))
	for _, blk := range blocks {
		labels[blk.Start] = a.Label()
	}
	return labels
}

// prepare constructs a fresh assembler + jitContext for a whole-function or
// multi-block compilation. Labels are pre-allocated per block when blks
// has more than one entry.
func (c *jitCompiler) prepare(i *Interpreter, fn *types.Function, locals []types.Kind, scratch []asm.PReg, blks []*analysis.BasicBlock) *jitContext {
	a := asm.New(c.arch)
	ctx := c.newContext(a, i, fn, locals, 0, scratch)
	ctx.whole = true
	if len(blks) > 1 {
		ctx.labels = c.allocLabels(a, blks)
	}
	return ctx
}

// walkBlocks drives the per-block feasibility or emit loop for both whole-function
// and framed whole-component compilation. For each reachable block it binds the
// block label, resets ctx scope, runs walk, and forwards merged stacks to
// successor blocks. emit selects between a dry-run feasibility pass and emission.
//
// ctx.framed selects the entry/exit discipline:
//   - non-framed: the entry label is bound once and live-ins loaded via prologue;
//     a RETURN-terminated block emits an interpreter exit (exitIP).
//   - framed: block 0 opens an Entry callable and saves the frame/link (enter);
//     RETURN lowers to a native return itself, so no exitIP is appended, and a
//     block reaching code end without stopping is rejected (a terminated-early
//     block would leave native branch targets unbound). A guard fallback is kept —
//     it deopts safely now that the wrapper rebuilds VM frames.
func (c *jitCompiler) walkBlocks(ctx *jitContext, fn *types.Function, blks []*analysis.BasicBlock, returns int, emit bool) bool {
	reachable := c.reachable(blks)
	inputs := map[int][]asm.VReg{0: nil}
	if !ctx.framed {
		ctx.assembler.Bind(ctx.entry)
		c.lowerer.prologue(ctx, fn)
	}
	for _, blk := range blks {
		if !reachable[blk.Start] {
			continue
		}
		stack, ok := inputs[blk.Start]
		if !ok {
			return false
		}
		switch {
		case ctx.framed && blk.Start == 0:
			ctx.assembler.Entry(ctx.labels[0], asm.Signature{Scratch: ctx.scratch})
			c.lowerer.enter(ctx)
		case ctx.labels != nil:
			ctx.assembler.Bind(ctx.labels[blk.Start])
		}
		ctx.reset(blk, stack)

		res := c.walk(ctx)
		if res.reject >= 0 {
			return false
		}
		if ctx.framed {
			if !ctx.stop && blk.End >= len(fn.Code) {
				return false
			}
		} else if ctx.stop && !ctx.closed {
			if !c.valid(ctx, returns) {
				return false
			}
			if emit {
				c.lowerer.exitIP(ctx, ctx.ip)
			}
		}
		if !c.merge(inputs, blks, blk, ctx.stack) {
			return false
		}
	}
	return true
}

func (c *jitCompiler) segments(i *Interpreter, addr int, fn *types.Function, locals []types.Kind, mod *jitModule) ([]segment, error) {
	ips := i.hot(addr)
	hot := make(map[int]bool, len(ips))
	for _, ip := range ips {
		if ip >= 0 && ip < len(fn.Code) {
			hot[ip] = true
		}
	}
	queue := []int{0}
	if len(ips) > 0 {
		queue = make([]int, 0, len(hot))
		for ip := range hot {
			queue = append(queue, ip)
		}
		sort.Ints(queue)
	}
	seen := make(map[int]bool, len(queue))
	var out []segment

	for len(queue) > 0 {
		start := queue[0]
		queue = queue[1:]
		if seen[start] || start < 0 || start >= len(fn.Code) {
			continue
		}
		seen[start] = true

		seg, ok, err := c.segment(i, fn, locals, start)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		seg.addr = addr
		out = append(out, seg)

		if seg.next < 0 || seen[seg.next] {
			continue
		}
		if seg.force || hot[seg.next] {
			queue = append(queue, seg.next)
			continue
		}
		mod.skips++
	}
	return out, nil
}

// segment lowers a contiguous run of opcodes starting at startIP.
// It walks the bytecode, calling the ARM64 emitter for each opcode. When lower
// returns false the segment terminates by exiting at the current IP, so the
// threaded interpreter resumes from there.
//
// Returns (code, true, nil) when at least cutoff opcodes lowered, otherwise
// (nil, false, nil).
func (c *jitCompiler) segment(i *Interpreter, fn *types.Function, locals []types.Kind, startIP int) (segment, bool, error) {
	scratch, ok := c.scratch()
	if !ok {
		return segment{}, false, nil
	}

	ctx := c.newContext(asm.New(c.arch), i, fn, locals, startIP, scratch)
	probed := c.walk(ctx)
	if probed.count < c.cutoff {
		return segment{}, false, nil
	}
	inputs := len(ctx.inputs)
	endIP := ctx.ip
	next, force := c.next(fn, ctx, probed)

	a := asm.New(c.arch)
	ctx = c.newContext(a, i, fn, locals, startIP, scratch)
	for i := 0; i < inputs; i++ {
		v := ctx.assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.inputs = append(ctx.inputs, v)
		ctx.stack = append(ctx.stack, v)
	}
	c.lowerer.prologue(ctx, fn)
	lowered := c.walk(ctx)
	if lowered.count < c.cutoff || ctx.ip != endIP || len(ctx.inputs) != inputs {
		return segment{}, false, nil
	}

	if !ctx.closed {
		c.lowerer.exitIP(ctx, ctx.ip)
	}

	code, err := a.Build(asm.Signature{Scratch: scratch})
	if err != nil {
		return segment{}, false, err
	}
	return segment{
		start: startIP,
		code:  code,
		stack: inputs,
		next:  next,
		force: force,
	}, true, nil
}

func (c *jitCompiler) link(mod *jitModule, segs []segment) error {
	codes := make([]*asm.Code, len(segs))
	for i, seg := range segs {
		codes[i] = seg.code
		mod.bytes = append(mod.bytes, len(seg.code.Bytes))
	}
	linked, err := asm.Link(c.buffer, c.arch, codes, nil)
	if err != nil {
		return err
	}
	for i, seg := range segs {
		entry := jitEntry{addr: seg.addr, ip: seg.start}
		mod.segments[entry] = linked[i].Callable
		mod.stacks[entry] = seg.stack
	}
	mod.links += len(segs)
	return nil
}

// reenter installs plain re-entry segments at the loop headers in blks, so a
// budget yield from the native entry resumes natively at the header instead of
// dropping to threaded dispatch.
func (c *jitCompiler) reenter(i *Interpreter, addr int, fn *types.Function, locals []types.Kind, blks []*analysis.BasicBlock, mod *jitModule) error {
	var segs []segment
	for _, header := range c.loops(blks) {
		seg, ok, err := c.segment(i, fn, locals, header)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		seg.addr = addr
		segs = append(segs, seg)
	}
	if len(segs) == 0 {
		return nil
	}
	return c.link(mod, segs)
}

// loops returns the start IPs of loop headers — blocks targeted by a back-edge
// from a block at or below them. IP 0 is excluded: re-entry there would re-run
// the entry prologue.
func (c *jitCompiler) loops(blks []*analysis.BasicBlock) []int {
	var out []int
	for _, b := range blks {
		if b.Start == 0 {
			continue
		}
		for _, p := range b.Preds {
			if p >= 0 && p < len(blks) && blks[p].Start >= b.Start {
				out = append(out, b.Start)
				break
			}
		}
	}
	return out
}

func (c *jitCompiler) component(i *Interpreter, addr int, fn *types.Function) map[int]*types.Function {
	funcs := map[int]*types.Function{addr: fn}
	var visit func(*types.Function)
	visit = func(current *types.Function) {
		for _, dst := range c.calls(i, current) {
			target, ok := i.function(dst)
			if !ok {
				continue
			}
			if _, ok := funcs[dst]; !ok {
				funcs[dst] = target
				visit(target)
			}
		}
	}
	visit(fn)
	return funcs
}

func (c *jitCompiler) calls(i *Interpreter, fn *types.Function) []int {
	var out []int
	for ip := 0; ip < len(fn.Code); {
		next := ip + instr.Instruction(fn.Code[ip:]).Width()
		if c.directCall(fn, ip) {
			idx := int(uint16(fn.Code[ip+1]) | uint16(fn.Code[ip+2])<<8)
			if idx >= 0 && idx < len(i.constants) && i.constants[idx].Kind() == types.KindRef {
				if _, ok := i.function(i.constants[idx].Ref()); ok {
					out = append(out, i.constants[idx].Ref())
				}
			}
		}
		ip = next
	}
	return out
}

func (c *jitCompiler) scratch() ([]asm.PReg, bool) {
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < scratchCount {
		return nil, false
	}
	return scratch[:scratchCount], true
}

func (c *jitCompiler) valid(ctx *jitContext, returns int) bool {
	return len(ctx.inputs) == 0 && len(ctx.stack) == returns
}

func (c *jitCompiler) newContext(a *asm.Assembler, i *Interpreter, fn *types.Function, locals []types.Kind, startIP int, scratch []asm.PReg) *jitContext {
	return &jitContext{
		assembler: a,
		code:      fn.Code,
		constants: i.constants,
		globals:   i.globals,
		locals:    locals,
		scratch:   scratch,
		entry:     a.Label(),
		ip:        startIP,
		end:       len(fn.Code),
		successor: -1,
	}
}

// terminated reports whether the last walked block ended in a self-contained
// RETURN: stopped, with no pending successor and no emitted exit.
func (c *jitContext) terminated() bool {
	return c.stop && c.successor < 0 && !c.closed
}

// reset positions ctx to walk block with stack as its entry operands.
func (c *jitContext) reset(block *analysis.BasicBlock, stack []asm.VReg) {
	c.ip = block.Start
	c.end = block.End
	c.stop = false
	c.closed = false
	c.successor = -1
	c.stack = append(c.stack[:0], stack...)
}

// pin returns a fresh Width64 int vreg bound to the scratch register at idx.
func (c *jitContext) pin(idx int) asm.VReg {
	v := c.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.assembler.Pin(v, c.scratch[idx])
	return v
}

// pinTo returns a fresh Width64 int vreg bound to the physical register pr.
func (c *jitContext) pinTo(pr asm.PReg) asm.VReg {
	v := c.assembler.Reg(asm.RegTypeInt, asm.Width64)
	_ = c.assembler.Pin(v, pr)
	return v
}

// walk mirrors threaded compilation: decode one opcode, ask the ARM64 emitter
// to lower it, then advance by the encoded width unless the opcode ended the
// segment itself.
func (c *jitCompiler) walk(ctx *jitContext) result {
	out := result{reject: -1}
	for ctx.ip < ctx.end {
		op := instr.Opcode(ctx.code[ctx.ip])
		ipBefore := ctx.ip
		width := instr.Instruction(ctx.code[ipBefore:]).Width()
		if !c.lowerer.lower(ctx, op) {
			out.reject = ipBefore
			break
		}
		if !ctx.stop && ctx.ip == ipBefore {
			ctx.ip = ipBefore + width
		}
		out.count++
		if ctx.stop {
			break
		}
	}
	return out
}

func (c *jitCompiler) next(fn *types.Function, ctx *jitContext, res result) (int, bool) {
	if ctx.successor >= 0 {
		if ctx.successor >= len(fn.Code) || instr.Opcode(fn.Code[ctx.successor]) == instr.NOP {
			return -1, false
		}
		return ctx.successor, true
	}
	if res.reject < 0 {
		return -1, false
	}
	next := res.reject + instr.Instruction(fn.Code[res.reject:]).Width()
	if next >= len(fn.Code) {
		return -1, false
	}
	if c.directCall(fn, res.reject) {
		next += 1
		if next >= len(fn.Code) {
			return -1, false
		}
		return next, true
	}
	if instr.Opcode(fn.Code[res.reject]) == instr.CALL {
		return next, false
	}
	switch instr.Opcode(fn.Code[next]) {
	case instr.NOP, instr.BR:
		return next, true
	default:
		return next, false
	}
}

func (c *jitCompiler) directCall(fn *types.Function, ip int) bool {
	if ip < 0 || ip >= len(fn.Code) || instr.Opcode(fn.Code[ip]) != instr.CONST_GET {
		return false
	}
	next := ip + instr.Instruction(fn.Code[ip:]).Width()
	if next >= len(fn.Code) || instr.Opcode(fn.Code[next]) != instr.CALL {
		return false
	}
	return true
}

func (c *jitCompiler) reachable(blocks []*analysis.BasicBlock) map[int]bool {
	reachable := map[int]bool{}
	if len(blocks) == 0 {
		return reachable
	}
	queue := []int{0}
	for len(queue) > 0 {
		idx := queue[0]
		queue = queue[1:]
		if idx < 0 || idx >= len(blocks) || reachable[blocks[idx].Start] {
			continue
		}
		reachable[blocks[idx].Start] = true
		queue = append(queue, blocks[idx].Succs...)
	}
	return reachable
}

func (c *jitCompiler) merge(inputs map[int][]asm.VReg, blocks []*analysis.BasicBlock, block *analysis.BasicBlock, stack []asm.VReg) bool {
	for _, idx := range block.Succs {
		if idx < 0 || idx >= len(blocks) {
			return false
		}
		start := blocks[idx].Start
		existing, ok := inputs[start]
		if !ok {
			inputs[start] = append([]asm.VReg(nil), stack...)
			continue
		}
		if len(existing) != len(stack) {
			return false
		}
		for i := range existing {
			if existing[i].ID() != stack[i].ID() {
				return false
			}
		}
	}
	return true
}

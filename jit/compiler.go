package jit

import (
	"fmt"
	"sort"

	"github.com/siyul-park/minivm/analysis"
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/pass"
	"github.com/siyul-park/minivm/types"
)

// Compiler is the top-level driver. It owns the executable buffer, the
// writable data region used for direct-call slot tables, and the cutoff that
// controls when partial segments are kept vs. discarded.
type Compiler struct {
	lowerer Lowerer

	arch   asm.Arch
	buffer *asm.Buffer
	data   *asm.Data
	slots  *Slots

	cutoff int
}

// Option mutates the Compiler's config at construction time.
type Option func(*config)

type segment struct {
	start int
	end   int

	code    *asm.Code
	entries []entry

	stack int
	next  int
	force bool
}

type result struct {
	count int
	end   int

	reject int
}

type entry struct {
	ip    int
	label asm.Label
	stack int
	args  []asm.PReg
}

type plan struct {
	ip    int
	stack []asm.VReg
}

type config struct {
	cutoff  int
	lowerer Lowerer

	buffer *asm.Buffer
	data   *asm.Data
}

// New constructs a Compiler. When no Lowerer is registered for the active
// architecture, Compile returns an empty Module so callers continue running
// the threaded interpreter.
func New(opts ...Option) (*Compiler, error) {
	cfg := config{cutoff: 8}
	for _, option := range opts {
		option(&cfg)
	}

	if cfg.lowerer == nil {
		cfg.lowerer = Active()
	}
	var arch asm.Arch
	if cfg.lowerer != nil {
		arch = cfg.lowerer.Arch()
	}

	if cfg.buffer == nil {
		buf, err := asm.NewBuffer(4096)
		if err != nil {
			return nil, err
		}
		cfg.buffer = buf
	}
	if cfg.data == nil {
		data, err := asm.NewData(4096)
		if err != nil {
			return nil, err
		}
		cfg.data = data
	}

	return &Compiler{
		lowerer: cfg.lowerer,
		arch:    arch,
		buffer:  cfg.buffer,
		data:    cfg.data,
		cutoff:  cfg.cutoff,
	}, nil
}

// WithBuffer overrides the default executable buffer.
func WithBuffer(buffer *asm.Buffer) Option { return func(option *config) { option.buffer = buffer } }

// WithData overrides the default writable data region used for slots.
func WithData(data *asm.Data) Option { return func(option *config) { option.data = data } }

// WithLowerer overrides the Lowerer the compiler dispatches to. By default
// the compiler picks the Lowerer registered for runtime.GOARCH.
func WithLowerer(lowerer Lowerer) Option { return func(option *config) { option.lowerer = lowerer } }

// WithCutoff sets the minimum number of opcodes a segment must lower
// before it is installed.
func WithCutoff(cutoff int) Option { return func(option *config) { option.cutoff = cutoff } }

// Slots returns the direct-call indirection table, creating it on first use.
func (c *Compiler) Slots() (*Slots, error) {
	if c.slots != nil {
		return c.slots, nil
	}
	fallback, err := c.fallback()
	if err != nil {
		return nil, err
	}
	if fallback == nil {
		return nil, nil
	}
	c.slots = NewSlots(c.data, fallback)
	return c.slots, nil
}

// Close releases the underlying buffer and data region.
func (c *Compiler) Close() error {
	if err := c.buffer.Free(); err != nil {
		return err
	}
	return c.data.Free()
}

// Compile attempts to lower fn into native code. Hot profile IPs seed segment
// selection; compatible hot IPs reached inside a segment become internal
// entries on the same Code. Rejected or short segments fall back to threaded
// dispatch.
//
// addr is the heap index of the function in the consumer's heap; it is
// echoed back in Module.Addr so installers can disambiguate. snap carries
// the consumer-side tables (constants, globals, local kinds) that
// kind-sensitive opcodes need at compile time.
func (c *Compiler) Compile(fn *types.Function, addr int, snap Snapshot) (*Module, error) {
	mod := newModule(addr)
	if c.lowerer == nil || fn == nil || len(fn.Code) == 0 {
		return mod, nil
	}

	// Try whole-function Entry, then multi-block Entry. The first one that
	// lowers cleanly makes the function directly callable without a Go-side
	// trampoline per call. blocks() handles BR_IF / BR that whole() cannot.
	for _, attempt := range []func(*types.Function, int, Snapshot) (segment, bool, error){c.whole, c.blocks} {
		seg, ok, err := attempt(fn, addr, snap)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		if err := c.installEntry(mod, seg); err != nil {
			return nil, err
		}
		return mod, nil
	}

	segs, err := c.segments(fn, snap, mod)
	if err != nil {
		return nil, err
	}
	if len(segs) == 0 {
		return mod, nil
	}
	if err := c.link(mod, segs); err != nil {
		return nil, err
	}
	return mod, nil
}

func (c *Compiler) installEntry(mod *Module, seg segment) error {
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{seg.code}, nil)
	if err != nil {
		return err
	}
	mod.Entry = linked[0].Callable
	if c.slots != nil {
		if err := c.slots.Set(mod.Addr, mod.Entry); err != nil {
			return fmt.Errorf("set entry slot: %w", err)
		}
	}
	mod.Signature = seg.code.Signature
	mod.Bytes = append(mod.Bytes, len(seg.code.Bytes))
	mod.Links = 1
	return nil
}

// whole lowers fn as a single native chunk. Succeeds only when every
// opcode lowers cleanly AND the function terminates via RETURN
// (Stop=true, no branch Successor, not Closed by BR_IF inline exits).
// On success the returned segment's code is the Entry.
func (c *Compiler) whole(fn *types.Function, addr int, snap Snapshot) (segment, bool, error) {
	return c.function(fn, addr, snap, []*analysis.BasicBlock{{Start: 0, End: len(fn.Code)}}, true)
}

// blocks lowers fn as a sequence of basic blocks connected by
// intra-function labels. Branches (BR_IF, BR) become native branches to
// block-start labels instead of interpreter exits. Returns (_, false, _)
// when a target is not a known block start (e.g. brTable), so segments()
// can handle those cases.
func (c *Compiler) blocks(fn *types.Function, addr int, snap Snapshot) (segment, bool, error) {
	m := pass.NewManager()
	if err := m.Run(fn); err != nil {
		return segment{}, false, err
	}
	blks, err := analysis.NewBasicBlocksPass().Run(m)
	if err != nil || len(blks) <= 1 {
		return segment{}, false, nil
	}
	return c.function(fn, addr, snap, blks, false)
}

// function runs a two-phase (plan + emit) compilation over blks. When
// requireTerminated is true the plan phase additionally checks that the
// last successfully lowered opcode left ctx in a RETURN-terminated state.
// Labels are bound per block only when len(blks) > 1; with a single
// synthetic block the function behaves as a straight-line whole-function
// compilation.
func (c *Compiler) function(fn *types.Function, addr int, snap Snapshot, blks []*analysis.BasicBlock, requireTerminated bool) (segment, bool, error) {
	scratch, ok := c.scratch()
	if !ok {
		return segment{}, false, nil
	}
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}

	planCtx := c.prepare(fn, addr, snap, scratch, blks)
	if !c.walkBlocks(planCtx, fn, blks, returns, false) {
		return segment{}, false, nil
	}
	if requireTerminated && !(planCtx.Stop && planCtx.Successor < 0 && !planCtx.Closed) {
		return segment{}, false, nil
	}
	if !c.validEntry(planCtx, returns) {
		return segment{}, false, nil
	}

	ctx := c.prepare(fn, addr, snap, scratch, blks)
	if !c.walkBlocks(ctx, fn, blks, returns, true) {
		return segment{}, false, nil
	}
	if !c.validEntry(ctx, returns) {
		return segment{}, false, nil
	}

	code, err := ctx.Assembler.Build(asm.Signature{Args: ctx.Args, Returns: ctx.Returns, Scratch: scratch})
	if err != nil {
		return segment{}, false, err
	}
	return segment{start: 0, end: len(fn.Code), code: code}, true, nil
}

// prepare constructs a fresh assembler + Context for a whole-function or
// multi-block compilation. Labels are pre-allocated per block when blks
// has more than one entry.
func (c *Compiler) prepare(fn *types.Function, addr int, snap Snapshot, scratch []asm.PReg, blks []*analysis.BasicBlock) *Context {
	a := asm.New(c.arch)
	ctx := c.newContext(a, fn, addr, 0, snap, scratch)
	ctx.Whole = true
	if len(blks) > 1 {
		ctx.Labels = make(map[int]asm.Label, len(blks))
		for _, blk := range blks {
			ctx.Labels[blk.Start] = a.Label()
		}
	}
	return ctx
}

// walkBlocks drives the per-block plan or emit loop. For each reachable
// block it resets ctx scope, runs walk, and forwards merged stacks to
// successor blocks. emit selects between planning (dry-run with the
// counting assembler) and emission (real code, includes Exit terminators
// for RETURN-ending blocks).
func (c *Compiler) walkBlocks(ctx *Context, fn *types.Function, blks []*analysis.BasicBlock, returns int, emit bool) bool {
	reachable := c.reachable(blks)
	inputs := map[int][]asm.VReg{0: nil}
	ctx.Assembler.Bind(ctx.Entry)
	c.lowerer.Prologue(ctx, fn)
	for _, blk := range blks {
		if !reachable[blk.Start] {
			continue
		}
		stack, ok := inputs[blk.Start]
		if !ok {
			return false
		}
		if ctx.Labels != nil {
			ctx.Assembler.Bind(ctx.Labels[blk.Start])
		}
		ctx.IP = blk.Start
		ctx.End = blk.End
		ctx.Stop = false
		ctx.Closed = false
		ctx.Successor = -1
		ctx.Stack = append(ctx.Stack[:0], stack...)

		var res result
		if emit {
			res, _ = c.emit(ctx, fn, nil)
		} else {
			res, _ = c.plan(ctx, nil)
		}
		if res.reject >= 0 {
			return false
		}
		if ctx.Stop && !ctx.Closed {
			if !c.validEntry(ctx, returns) {
				return false
			}
			if emit {
				c.lowerer.Exit(ctx, ctx.IP)
			}
		}
		if !c.merge(inputs, blks, blk, ctx.Stack) {
			return false
		}
	}
	return true
}

func (c *Compiler) segments(fn *types.Function, snap Snapshot, mod *Module) ([]segment, error) {
	hot := make(map[int]bool, len(snap.Hot))
	for _, ip := range snap.Hot {
		if ip >= 0 && ip < len(fn.Code) {
			hot[ip] = true
		}
	}
	queue := []int{0}
	if len(snap.Hot) > 0 {
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

		seg, ok, err := c.segment(fn, mod.Addr, start, snap, hot)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		out = append(out, seg)
		for _, ent := range seg.entries {
			seen[ent.ip] = true
		}

		if seg.next < 0 || seen[seg.next] {
			continue
		}
		if seg.force || hot[seg.next] {
			queue = append(queue, seg.next)
			continue
		}
		mod.Skips++
	}
	return out, nil
}

// segment lowers a contiguous run of opcodes starting at startIP.
// It walks the bytecode, calling the Lowerer for each opcode. When Lower
// returns false the segment terminates by exiting at the current IP, so the
// threaded interpreter resumes from there.
//
// Returns (code, true, nil) when at least cutoff opcodes lowered, otherwise
// (nil, false, nil).
func (c *Compiler) segment(fn *types.Function, addr int, startIP int, snap Snapshot, hot map[int]bool) (segment, bool, error) {
	scratch, ok := c.scratch()
	if !ok {
		return segment{}, false, nil
	}

	plan := c.newContext(asm.New(c.arch), fn, addr, startIP, snap, scratch)
	planned, plans := c.plan(plan, hot)
	if planned.count < c.cutoff {
		return segment{}, false, nil
	}
	if !c.fits(plan) {
		return segment{}, false, nil
	}
	plans = c.entries(plan, plans)

	a := asm.New(c.arch)
	ctx := c.newContext(a, fn, addr, startIP, snap, scratch)
	for i := 0; i < len(plan.Inputs); i++ {
		v := ctx.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.Inputs = append(ctx.Inputs, v)
		ctx.Stack = append(ctx.Stack, v)
	}
	c.lowerer.Prologue(ctx, fn)
	var entrySet map[int]bool
	if len(plans) > 0 {
		entrySet = make(map[int]bool, len(plans))
		for _, plan := range plans {
			entrySet[plan.ip] = true
		}
	}
	lowered, entries := c.emit(ctx, fn, entrySet)
	if lowered.count < c.cutoff || ctx.IP != plan.IP || len(ctx.Inputs) != len(plan.Inputs) {
		return segment{}, false, nil
	}
	if !c.fits(ctx) {
		return segment{}, false, nil
	}

	if !ctx.Closed {
		c.lowerer.Exit(ctx, ctx.IP)
	}

	code, err := a.Build(asm.Signature{Args: ctx.Args, Returns: ctx.Returns, Scratch: scratch})
	if err != nil {
		return segment{}, false, err
	}
	sig := code.Signature
	if len(entries) > 0 {
		code.Entries = make(map[asm.Label]asm.Signature, len(entries))
		for _, ent := range entries {
			code.Entries[ent.label] = asm.Signature{
				Args:    ent.args,
				Returns: sig.Returns,
				Scratch: scratch,
			}
		}
	}
	next, force := c.next(fn, plan, planned)
	return segment{
		start:   startIP,
		end:     planned.end,
		code:    code,
		stack:   len(sig.Args),
		entries: entries,
		next:    next,
		force:   force,
	}, true, nil
}

func (c *Compiler) link(mod *Module, segs []segment) error {
	codes := make([]*asm.Code, len(segs))
	for i, seg := range segs {
		codes[i] = seg.code
		mod.Bytes = append(mod.Bytes, len(seg.code.Bytes))
	}
	linked, err := asm.Link(c.buffer, c.arch, codes, nil)
	if err != nil {
		return err
	}
	for i, seg := range segs {
		mod.Segments[seg.start] = linked[i].Callable
		mod.Stacks[seg.start] = seg.stack
		for _, ent := range seg.entries {
			callable := linked[i].Entries[ent.label]
			if callable == nil {
				return fmt.Errorf("missing linked entry %d at ip %d", ent.label, ent.ip)
			}
			mod.Segments[ent.ip] = callable
			mod.Stacks[ent.ip] = ent.stack
		}
	}
	mod.Links = len(mod.Segments)
	return nil
}

func (c *Compiler) fallback() (asm.Callable, error) {
	if c.lowerer == nil {
		return nil, nil
	}
	scratch, ok := c.scratch()
	if !ok {
		return nil, nil
	}

	stub := &types.Function{Code: []byte{}}
	a := asm.New(c.arch)
	ctx := c.newContext(a, stub, -1, 0, Snapshot{}, scratch)
	c.lowerer.Exit(ctx, 0)
	code, err := a.Build(asm.Signature{Scratch: scratch})
	if err != nil {
		return nil, fmt.Errorf("build fallback: %w", err)
	}
	linked, err := asm.Link(c.buffer, c.arch, []*asm.Code{code}, nil)
	if err != nil {
		return nil, fmt.Errorf("link fallback: %w", err)
	}
	return linked[0].Callable, nil
}

func (c *Compiler) scratch() ([]asm.PReg, bool) {
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < ScratchCount {
		return nil, false
	}
	return scratch[:ScratchCount], true
}

func (c *Compiler) validEntry(ctx *Context, returns int) bool {
	return c.fits(ctx) && len(ctx.Inputs) == 0 && len(ctx.Stack) == returns
}

func (c *Compiler) newContext(a *asm.Assembler, fn *types.Function, addr int, startIP int, snap Snapshot, scratch []asm.PReg) *Context {
	return &Context{
		Assembler: a,
		Slots:     c.slots,
		Code:      fn.Code,
		Snap:      snap,
		Scratch:   scratch,
		Entry:     a.Label(),
		IP:        startIP,
		Start:     startIP,
		End:       len(fn.Code),
		Self:      addr,
		Target:    -1,
		Successor: -1,
	}
}

// walk drives the per-opcode lowering loop shared by plan and emit. For each
// entry IP the caller supplies before (run before Lower; in plan this snapshots
// ctx.Stack so the after callback can commit a frozen copy on success) and
// after (run only after Lower advances IP). Either may be nil.
//
// On Lower success the driver advances ctx.IP by the opcode width unless the
// handler set ctx.Stop (in which case the handler owns ctx.IP). Handlers
// therefore never touch ctx.IP on the straight-line path.
func (c *Compiler) walk(ctx *Context, entries map[int]bool, before, after func(ip int)) result {
	out := result{reject: -1}
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		width := instr.Instruction(ctx.Code[ipBefore:]).Width()
		hit := entries[ipBefore] && ipBefore != ctx.Start
		if hit && before != nil {
			before(ipBefore)
		}
		if !c.lowerer.Lower(ctx, op) {
			out.reject = ipBefore
			break
		}
		if !ctx.Stop {
			ctx.IP = ipBefore + width
		}
		out.count++
		out.end = max(out.end, ipBefore+width)
		if hit && after != nil {
			after(ipBefore)
		}
		if ctx.Stop {
			break
		}
	}
	return out
}

func (c *Compiler) plan(ctx *Context, entries map[int]bool) (result, []plan) {
	var plans []plan
	var pending []asm.VReg
	before := func(int) {
		pending = append(pending[:0], ctx.Stack...)
	}
	after := func(ip int) {
		plans = append(plans, plan{ip: ip, stack: append([]asm.VReg(nil), pending...)})
	}
	return c.walk(ctx, entries, before, after), plans
}

func (c *Compiler) emit(ctx *Context, fn *types.Function, entries map[int]bool) (result, []entry) {
	var internal []entry
	before := func(ip int) {
		if ent, ok := c.mark(ctx, fn, ip); ok {
			internal = append(internal, ent)
		}
	}
	return c.walk(ctx, entries, before, nil), internal
}

func (c *Compiler) fits(ctx *Context) bool {
	return len(ctx.Inputs) <= c.arch.ABI().MaxArgs() &&
		len(ctx.Stack) <= c.arch.ABI().MaxReturns()
}

// entries filters plans down to those whose stack shape can be pinned to
// segment-entry ABI Arg registers without conflicting with the segment-wide
// vreg-to-preg basis (Inputs as Args, Stack as Returns). Each candidate
// re-validates against the basis plus its own first-seen assignments.
func (c *Compiler) entries(ctx *Context, plans []plan) []plan {
	if len(plans) == 0 {
		return nil
	}
	abi := c.arch.ABI()

	basis := map[int32]asm.PReg{}
	for i, v := range ctx.Inputs {
		if !c.assign(basis, v, abi.Arg(i, v.Type(), v.Width())) {
			return nil
		}
	}
	for i, v := range ctx.Stack {
		if !c.assign(basis, v, abi.Return(i, v.Type(), v.Width())) {
			return nil
		}
	}

	maxArgs := abi.MaxArgs()
	keep := make([]plan, 0, len(plans))
next:
	for _, p := range plans {
		if len(p.stack) > maxArgs {
			continue
		}
		local := map[int32]asm.PReg{}
		for i, v := range p.stack {
			arg := abi.Arg(i, v.Type(), v.Width())
			if fixed, ok := basis[v.ID()]; ok {
				if fixed.ID() != arg.ID() || fixed.Type() != arg.Type() {
					continue next
				}
				continue
			}
			if !c.assign(local, v, arg) {
				continue next
			}
		}
		keep = append(keep, p)
	}
	return keep
}

func (c *Compiler) mark(ctx *Context, fn *types.Function, ip int) (entry, bool) {
	if len(ctx.Stack) > c.arch.ABI().MaxArgs() {
		return entry{}, false
	}
	entryArgs := make([]asm.PReg, len(ctx.Stack))
	for i, v := range ctx.Stack {
		arg := c.arch.ABI().Arg(i, v.Type(), v.Width())
		if !ctx.Assembler.CanPin(v, arg) {
			return entry{}, false
		}
		entryArgs[i] = arg
	}

	label := ctx.Assembler.Label()
	ctx.Assembler.Bind(label)

	inputs := ctx.Inputs
	args := ctx.Args
	ctx.Inputs = append([]asm.VReg(nil), ctx.Stack...)
	ctx.Args = nil
	c.lowerer.Prologue(ctx, fn)
	ctx.Inputs = inputs
	ctx.Args = args

	return entry{ip: ip, label: label, stack: len(ctx.Stack), args: entryArgs}, true
}

// assign records v's binding to preg in m. Returns false when v is already
// bound to a different preg slot.
func (*Compiler) assign(m map[int32]asm.PReg, v asm.VReg, preg asm.PReg) bool {
	existing, ok := m[v.ID()]
	if !ok {
		m[v.ID()] = preg
		return true
	}
	return existing.ID() == preg.ID() && existing.Type() == preg.Type()
}

func (c *Compiler) next(fn *types.Function, ctx *Context, res result) (int, bool) {
	if ctx.Successor >= 0 {
		if ctx.Successor >= len(fn.Code) || instr.Opcode(fn.Code[ctx.Successor]) == instr.NOP {
			return -1, false
		}
		return ctx.Successor, true
	}
	if res.reject < 0 {
		return -1, false
	}
	next := res.reject + instr.Instruction(fn.Code[res.reject:]).Width()
	if next >= len(fn.Code) {
		return -1, false
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

func (c *Compiler) reachable(blocks []*analysis.BasicBlock) map[int]bool {
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

func (c *Compiler) merge(inputs map[int][]asm.VReg, blocks []*analysis.BasicBlock, block *analysis.BasicBlock, stack []asm.VReg) bool {
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

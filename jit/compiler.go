package jit

import (
	"fmt"
	"sort"

	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Compiler is the top-level driver. It owns the executable buffer, the
// writable data region used for direct-call slot tables, and the cutoff that
// controls when partial segments are kept vs. discarded.
type Compiler struct {
	cutoff  int
	lowerer Lowerer

	arch   asm.Arch
	buffer *asm.Buffer
	data   *asm.Data

	slots *Slots
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
	op     instr.Opcode
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
		cutoff:  cfg.cutoff,
		lowerer: cfg.lowerer,
		arch:    arch,
		buffer:  cfg.buffer,
		data:    cfg.data,
	}, nil
}

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
	mod := NewModule(fn, addr)
	if c.lowerer == nil || fn == nil || len(fn.Code) == 0 {
		return mod, nil
	}

	// Attempt whole-function Entry first. If every opcode lowers cleanly the
	// function becomes directly callable without a Go-side trampoline per call.
	if seg, ok, err := c.whole(fn, snap); err != nil {
		return nil, err
	} else if ok {
		linked, err := asm.LinkAll(c.buffer, c.arch, []*asm.Code{seg.code}, nil)
		if err != nil {
			return nil, err
		}
		mod.Entry = linked[0].Callable
		if c.slots != nil {
			if err := c.slots.Set(addr, mod.Entry); err != nil {
				return nil, fmt.Errorf("set entry slot: %w", err)
			}
		}
		mod.Signature = seg.code.Signature
		mod.Bytes = append(mod.Bytes, len(seg.code.Bytes))
		mod.Links = 1
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

// whole attempts to lower the entire function as a single native chunk.
// It succeeds only when every opcode lowers without rejection. On success the
// returned segment's code is the Entry; its ABI follows the standard segment
// convention (params accessed via LOCAL_GET / scratch slots, results in ABI
// return registers).
func (c *Compiler) whole(fn *types.Function, snap Snapshot) (segment, bool, error) {
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < ScratchCount {
		return segment{}, false, nil
	}
	scratch = scratch[:ScratchCount]
	returns := 0
	if fn.Typ != nil {
		returns = len(fn.Typ.Returns)
	}

	// Plan phase: require all opcodes to lower (reject < 0) and the function
	// to terminate via RETURN (Stop=true, no branch Successor, not Closed by
	// BR_IF inline exits).
	plan := c.context(asm.New(c.arch), fn, 0, snap, scratch)
	plan.Whole = true
	planned, _ := c.plan(plan, nil)
	if planned.reject >= 0 || planned.count == 0 {
		return segment{}, false, nil
	}
	terminated := plan.Stop && plan.Successor < 0 && !plan.Closed
	if !terminated {
		return segment{}, false, nil
	}
	if !c.fits(plan) {
		return segment{}, false, nil
	}
	if len(plan.Stack) != returns {
		return segment{}, false, nil
	}

	// Emit phase: re-lower with the real assembler.
	a := asm.New(c.arch)
	ctx := c.context(a, fn, 0, snap, scratch)
	ctx.Whole = true
	c.lowerer.Prologue(ctx, fn)
	lowered, _ := c.emit(ctx, fn, nil)
	if lowered.reject >= 0 || lowered.count == 0 {
		return segment{}, false, nil
	}
	if !c.fits(ctx) {
		return segment{}, false, nil
	}
	if len(ctx.Stack) != returns {
		return segment{}, false, nil
	}

	if !ctx.Closed {
		c.lowerer.Exit(ctx, ctx.IP)
	}

	sig := asm.Signature{Args: ctx.Args, Returns: ctx.Returns, Scratch: scratch}
	code, err := a.Build(sig)
	if err != nil {
		return segment{}, false, err
	}
	return segment{
		start: 0,
		end:   len(fn.Code),
		code:  code,
	}, true, nil
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

		seg, ok, err := c.segment(fn, start, snap, hot)
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
func (c *Compiler) segment(fn *types.Function, startIP int, snap Snapshot, hot map[int]bool) (segment, bool, error) {
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < ScratchCount {
		return segment{}, false, nil
	}
	scratch = scratch[:ScratchCount]

	plan := c.context(asm.New(c.arch), fn, startIP, snap, scratch)
	planned, plans := c.plan(plan, hot)
	if planned.count < c.cutoff {
		return segment{}, false, nil
	}
	if !c.fits(plan) {
		return segment{}, false, nil
	}
	plans = c.entries(plan, plans)

	a := asm.New(c.arch)
	ctx := c.context(a, fn, startIP, snap, scratch)
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

	sig := asm.Signature{Args: ctx.Args, Returns: ctx.Returns, Scratch: scratch}
	code, err := a.Build(sig)
	if err != nil {
		return segment{}, false, err
	}
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

func (c *Compiler) fallback() (asm.Callable, error) {
	if c.lowerer == nil {
		return nil, nil
	}
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < ScratchCount {
		return nil, nil
	}
	scratch = scratch[:ScratchCount]

	stub := &types.Function{Code: []byte{}}
	a := asm.New(c.arch)
	ctx := c.context(a, stub, 0, Snapshot{}, scratch)
	c.lowerer.Exit(ctx, 0)
	code, err := a.Build(asm.Signature{Scratch: scratch})
	if err != nil {
		return nil, fmt.Errorf("build fallback: %w", err)
	}
	linked, err := asm.LinkAll(c.buffer, c.arch, []*asm.Code{code}, nil)
	if err != nil {
		return nil, fmt.Errorf("link fallback: %w", err)
	}
	return linked[0].Callable, nil
}

func (c *Compiler) link(mod *Module, segs []segment) error {
	codes := make([]*asm.Code, len(segs))
	for i, seg := range segs {
		codes[i] = seg.code
		mod.Bytes = append(mod.Bytes, len(seg.code.Bytes))
	}
	linked, err := asm.LinkAll(c.buffer, c.arch, codes, nil)
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

func (c *Compiler) context(a *asm.Assembler, fn *types.Function, startIP int, snap Snapshot, scratch []asm.PReg) *Context {
	return &Context{
		Assembler: a,
		Code:      fn.Code,
		Start:     startIP,
		End:       len(fn.Code),
		IP:        startIP,
		Successor: -1,
		Snap:      snap,
		Scratch:   scratch,
		Slots:     c.slots,
		Layout:    RuntimeLayout(),
		Target:    -1,
	}
}

func (c *Compiler) plan(ctx *Context, entries map[int]bool) (result, []plan) {
	out := result{reject: -1}
	var plans []plan
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		width := instr.Instruction(ctx.Code[ipBefore:]).Width()
		mark := entries[ipBefore] && ipBefore != ctx.Start
		var stack []asm.VReg
		if mark {
			stack = append([]asm.VReg(nil), ctx.Stack...)
		}
		if !c.lowerer.Lower(ctx, op) {
			out.reject = ipBefore
			out.op = op
			break
		}
		if ctx.IP == ipBefore && !ctx.Stop {
			// Lowerer reported success but did not advance IP.
			out.reject = ipBefore
			out.op = op
			break
		}
		out.count++
		out.end = max(out.end, ipBefore+width)
		if mark {
			plans = append(plans, plan{ip: ipBefore, stack: stack})
		}
		if ctx.Stop {
			break
		}
	}
	return out, plans
}

func (c *Compiler) emit(ctx *Context, fn *types.Function, entries map[int]bool) (result, []entry) {
	out := result{reject: -1}
	var internal []entry
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		width := instr.Instruction(ctx.Code[ipBefore:]).Width()
		if entries[ipBefore] && ipBefore != ctx.Start {
			if ent, ok := c.mark(ctx, fn, ipBefore); ok {
				internal = append(internal, ent)
			}
		}
		if !c.lowerer.Lower(ctx, op) {
			out.reject = ipBefore
			out.op = op
			break
		}
		if ctx.IP == ipBefore && !ctx.Stop {
			// Lowerer reported success but did not advance IP.
			out.reject = ipBefore
			out.op = op
			break
		}
		out.count++
		out.end = max(out.end, ipBefore+width)
		if ctx.Stop {
			break
		}
	}
	return out, internal
}

func (c *Compiler) fits(ctx *Context) bool {
	return len(ctx.Inputs) <= c.arch.ABI().MaxArgs() &&
		len(ctx.Stack) <= c.arch.ABI().MaxReturns()
}

func (c *Compiler) entries(ctx *Context, plans []plan) []plan {
	if len(plans) == 0 {
		return nil
	}

	pins := map[int32]asm.PReg{}
	keep := make([]plan, 0, len(plans))
	for i, v := range ctx.Inputs {
		if !c.pin(pins, v, c.arch.ABI().Arg(i, v.Type(), v.Width())) {
			return nil
		}
	}
	for i, v := range ctx.Stack {
		if !c.pin(pins, v, c.arch.ABI().Return(i, v.Type(), v.Width())) {
			return nil
		}
	}
	for _, plan := range plans {
		if len(plan.stack) > c.arch.ABI().MaxArgs() || !c.pins(pins, plan.stack) {
			continue
		}
		keep = append(keep, plan)
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

func (c *Compiler) pins(pins map[int32]asm.PReg, stack []asm.VReg) bool {
	added := make([]asm.VReg, 0, len(stack))
	for i, v := range stack {
		if _, ok := pins[v.ID()]; !ok {
			added = append(added, v)
		}
		if !c.pin(pins, v, c.arch.ABI().Arg(i, v.Type(), v.Width())) {
			for _, v := range added {
				delete(pins, v.ID())
			}
			return false
		}
	}
	return true
}

func (c *Compiler) pin(pins map[int32]asm.PReg, v asm.VReg, preg asm.PReg) bool {
	existing, ok := pins[v.ID()]
	if !ok {
		pins[v.ID()] = preg
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
	if res.op == instr.CALL {
		return next, false
	}
	switch instr.Opcode(fn.Code[next]) {
	case instr.NOP, instr.BR:
		return next, true
	default:
		return next, false
	}
}

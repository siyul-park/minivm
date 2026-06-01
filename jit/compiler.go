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
	lowerer Lowerer
	arch    asm.Arch
	buffer  *asm.Buffer
	data    *asm.Data
	slots   *Slots
	cutoff  int
}

// Option mutates the Compiler's config at construction time.
type Option func(*config)

type segment struct {
	start   int
	end     int
	code    *asm.Code
	stack   int
	entries []internalEntry
	next    int
	force   bool
}

type result struct {
	count  int
	end    int
	reject int
	op     instr.Opcode
}

type internalEntry struct {
	ip    int
	label asm.Label
	stack int
	args  []asm.PReg
}

type entryPlan struct {
	ip    int
	stack []asm.VReg
}

type config struct {
	lowerer Lowerer
	buffer  *asm.Buffer
	data    *asm.Data
	cutoff  int
}

// WithBuffer overrides the default executable buffer.
func WithBuffer(b *asm.Buffer) Option { return func(c *config) { c.buffer = b } }

// WithData overrides the default writable data region used for slots.
func WithData(d *asm.Data) Option { return func(c *config) { c.data = d } }

// WithLowerer overrides the Lowerer the compiler dispatches to. By default
// the compiler picks the Lowerer registered for runtime.GOARCH.
func WithLowerer(l Lowerer) Option { return func(c *config) { c.lowerer = l } }

// WithCutoff sets the minimum number of opcodes a segment must lower
// before it is installed.
func WithCutoff(n int) Option { return func(c *config) { c.cutoff = n } }

// New constructs a Compiler. When no Lowerer is registered for the active
// architecture, Compile returns an empty Module so callers continue running
// the threaded interpreter.
func New(opts ...Option) (*Compiler, error) {
	cfg := config{cutoff: 8}
	for _, o := range opts {
		o(&cfg)
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

// Slots returns the direct-call indirection table, lazily building it on
// first request. Returns nil when no Lowerer is wired up.
func (c *Compiler) Slots() *Slots { return c.slots }

// SetSlots installs the slot table the Compiler should hand to lowerers.
// Phase A leaves the table nil; Step 4 will wire it in.
func (c *Compiler) SetSlots(s *Slots) { c.slots = s }

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
	mod := newModule(fn, addr)
	if c.lowerer == nil || fn == nil || len(fn.Code) == 0 {
		return mod, nil
	}

	segs, err := c.compileSegments(fn, snap, mod)
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

func (c *Compiler) compileSegments(fn *types.Function, snap Snapshot, mod *Module) ([]segment, error) {
	hot := hotSet(fn, snap.Hot)
	queue := starts(fn, snap.Hot)
	seen := make(map[int]bool, len(queue))
	var out []segment

	for len(queue) > 0 {
		start := queue[0]
		queue = queue[1:]
		if seen[start] || start < 0 || start >= len(fn.Code) {
			continue
		}
		seen[start] = true

		seg, ok, err := c.compileSegment(fn, start, snap, hot)
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

// compileSegment lowers a contiguous run of opcodes starting at startIP.
// It walks the bytecode, calling the Lowerer for each opcode. When Lower
// returns false the segment terminates by exiting at the current IP, so the
// threaded interpreter resumes from there.
//
// Returns (code, true, nil) when at least cutoff opcodes lowered, otherwise
// (nil, false, nil).
func (c *Compiler) compileSegment(fn *types.Function, startIP int, snap Snapshot, hot map[int]bool) (segment, bool, error) {
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
	plans = c.selectEntries(plan, plans)

	a := asm.New(c.arch)
	ctx := c.context(a, fn, startIP, snap, scratch)
	seedInputs(ctx, len(plan.Inputs))
	c.lowerer.Prologue(ctx, fn)
	lowered, entries := c.emit(ctx, fn, entryIPs(plans))
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

func (c *Compiler) context(a *asm.Assembler, fn *types.Function, startIP int, snap Snapshot, scratch []asm.PReg) *Context {
	return &Context{
		Assembler: a,
		Code:      fn.Code,
		Start:     startIP,
		IP:        startIP,
		End:       len(fn.Code),
		Successor: -1,
		Snap:      snap,
		Scratch:   scratch,
		Slots:     c.slots,
		Layout:    RuntimeLayout(),
	}
}

func (c *Compiler) plan(ctx *Context, entries map[int]bool) (result, []entryPlan) {
	out := result{reject: -1}
	var plans []entryPlan
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		width := instrWidth(ctx.Code, ipBefore)
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
			plans = append(plans, entryPlan{ip: ipBefore, stack: stack})
		}
		if ctx.Stop {
			break
		}
	}
	return out, plans
}

func (c *Compiler) emit(ctx *Context, fn *types.Function, entries map[int]bool) (result, []internalEntry) {
	out := result{reject: -1}
	var internal []internalEntry
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		width := instrWidth(ctx.Code, ipBefore)
		if entries[ipBefore] && ipBefore != ctx.Start {
			if ent, ok := c.entry(ctx, fn, ipBefore); ok {
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

func (c *Compiler) selectEntries(ctx *Context, plans []entryPlan) []entryPlan {
	if len(plans) == 0 {
		return nil
	}

	pins := map[int32]asm.PReg{}
	keep := make([]entryPlan, 0, len(plans))
	for i, v := range ctx.Inputs {
		if !addPin(pins, v, c.arch.ABI().Arg(i, v.Type(), v.Width())) {
			return nil
		}
	}
	for i, v := range ctx.Stack {
		if !addPin(pins, v, c.arch.ABI().Return(i, v.Type(), v.Width())) {
			return nil
		}
	}
	for _, plan := range plans {
		if len(plan.stack) > c.arch.ABI().MaxArgs() || !addEntryPins(c.arch, pins, plan.stack) {
			continue
		}
		keep = append(keep, plan)
	}
	return keep
}

func (c *Compiler) entry(ctx *Context, fn *types.Function, ip int) (internalEntry, bool) {
	if len(ctx.Stack) > c.arch.ABI().MaxArgs() {
		return internalEntry{}, false
	}
	entryArgs := make([]asm.PReg, len(ctx.Stack))
	for i, v := range ctx.Stack {
		arg := c.arch.ABI().Arg(i, v.Type(), v.Width())
		if !ctx.Assembler.CanPin(v, arg) {
			return internalEntry{}, false
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

	return internalEntry{ip: ip, label: label, stack: len(ctx.Stack), args: entryArgs}, true
}

func entryIPs(plans []entryPlan) map[int]bool {
	if len(plans) == 0 {
		return nil
	}
	set := make(map[int]bool, len(plans))
	for _, plan := range plans {
		set[plan.ip] = true
	}
	return set
}

func addEntryPins(arch asm.Arch, pins map[int32]asm.PReg, stack []asm.VReg) bool {
	added := make([]asm.VReg, 0, len(stack))
	for i, v := range stack {
		if _, ok := pins[v.ID()]; !ok {
			added = append(added, v)
		}
		if !addPin(pins, v, arch.ABI().Arg(i, v.Type(), v.Width())) {
			for _, v := range added {
				delete(pins, v.ID())
			}
			return false
		}
	}
	return true
}

func addPin(pins map[int32]asm.PReg, v asm.VReg, preg asm.PReg) bool {
	existing, ok := pins[v.ID()]
	if !ok {
		pins[v.ID()] = preg
		return true
	}
	return existing.ID() == preg.ID() && existing.Type() == preg.Type()
}

func seedInputs(ctx *Context, n int) {
	for i := 0; i < n; i++ {
		v := ctx.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.Inputs = append(ctx.Inputs, v)
		ctx.Stack = append(ctx.Stack, v)
	}
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
	next := res.reject + instrWidth(fn.Code, res.reject)
	if next >= len(fn.Code) {
		return -1, false
	}
	return next, forceReject(fn.Code, next, res.op)
}

func hotSet(fn *types.Function, hot []int) map[int]bool {
	set := make(map[int]bool, len(hot))
	for _, ip := range hot {
		if ip >= 0 && ip < len(fn.Code) {
			set[ip] = true
		}
	}
	return set
}

func starts(fn *types.Function, hot []int) []int {
	if len(hot) == 0 {
		return []int{0}
	}
	set := hotSet(fn, hot)
	out := make([]int, 0, len(set))
	for ip := range set {
		out = append(out, ip)
	}
	sort.Ints(out)
	return out
}

func instrWidth(code []byte, ip int) int {
	return instr.Instruction(code[ip:]).Width()
}

func forceReject(code []byte, next int, op instr.Opcode) bool {
	if op == instr.CALL {
		return false
	}
	switch instr.Opcode(code[next]) {
	case instr.NOP, instr.BR:
		return true
	default:
		return false
	}
}

// newModule returns a default Module that carries fn's boxing metadata.
// The Segments map starts empty; the compiler fills it as segments link.
func newModule(fn *types.Function, addr int) *Module {
	var params, returns []types.Kind
	if fn != nil && fn.Typ != nil {
		params = make([]types.Kind, len(fn.Typ.Params))
		for i, t := range fn.Typ.Params {
			params[i] = t.Kind()
		}
		returns = make([]types.Kind, len(fn.Typ.Returns))
		for i, t := range fn.Typ.Returns {
			returns[i] = t.Kind()
		}
	}
	return &Module{
		Addr:        addr,
		Segments:    map[int]asm.Callable{},
		Stacks:      map[int]int{},
		ParamKinds:  params,
		ReturnKinds: returns,
	}
}

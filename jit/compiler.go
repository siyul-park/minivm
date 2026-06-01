package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Compiler is the top-level driver. It owns the executable buffer, the
// writable data region used for direct-BL slot tables, and the cutoff that
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

// Slots returns the direct-BL indirection table, lazily building it on
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

// Compile attempts to lower fn into native code. The current implementation
// emits at most one segment starting at IP 0, falling back to threaded
// dispatch (empty Module) when the active Lowerer rejects any opcode.
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

	seg, ok, err := c.compileSegment(fn, 0, snap)
	if err != nil {
		return nil, err
	}
	if !ok {
		return mod, nil
	}

	callables, err := asm.Link(c.buffer, c.arch, []*asm.Code{seg}, nil)
	if err != nil {
		return nil, err
	}
	mod.Segments[0] = callables[0]
	mod.Stacks[0] = len(seg.Signature.Args)
	return mod, nil
}

// compileSegment lowers a contiguous run of opcodes starting at startIP.
// It walks the bytecode, calling the Lowerer for each opcode. When Lower
// returns false the segment terminates by exiting at the current IP, so the
// threaded interpreter resumes from there.
//
// Returns (code, true, nil) when at least cutoff opcodes lowered, otherwise
// (nil, false, nil).
func (c *Compiler) compileSegment(fn *types.Function, startIP int, snap Snapshot) (*asm.Code, bool, error) {
	scratch := c.arch.ABI().Scratch()
	if len(scratch) < ScratchCount {
		return nil, false, nil
	}
	scratch = scratch[:ScratchCount]

	plan := c.context(asm.New(c.arch), fn, startIP, snap, scratch)
	lowered := c.lower(plan)
	if lowered < c.cutoff {
		return nil, false, nil
	}
	if !c.fits(plan) {
		return nil, false, nil
	}

	a := asm.New(c.arch)
	ctx := c.context(a, fn, startIP, snap, scratch)
	seedInputs(ctx, len(plan.Inputs))
	c.lowerer.Prologue(ctx, fn)
	lowered = c.lower(ctx)
	if lowered < c.cutoff || ctx.IP != plan.IP || len(ctx.Inputs) != len(plan.Inputs) {
		return nil, false, nil
	}
	if !c.fits(ctx) {
		return nil, false, nil
	}

	c.lowerer.Exit(ctx, ctx.IP)

	sig := asm.Signature{Args: ctx.Args, Returns: ctx.Returns, Scratch: scratch}
	code, err := a.Build(sig)
	if err != nil {
		return nil, false, err
	}
	return code, true, nil
}

func (c *Compiler) context(a *asm.Assembler, fn *types.Function, startIP int, snap Snapshot, scratch []asm.PReg) *Context {
	return &Context{
		Assembler: a,
		Code:      fn.Code,
		Start:     startIP,
		IP:        startIP,
		End:       len(fn.Code),
		Snap:      snap,
		Scratch:   scratch,
		Slots:     c.slots,
		Layout:    RuntimeLayout(),
	}
}

func (c *Compiler) lower(ctx *Context) int {
	lowered := 0
	for ctx.IP < ctx.End {
		op := instr.Opcode(ctx.Code[ctx.IP])
		ipBefore := ctx.IP
		if !c.lowerer.Lower(ctx, op) {
			break
		}
		if ctx.IP == ipBefore {
			// Lowerer reported success but did not advance IP.
			break
		}
		lowered++
	}
	return lowered
}

func (c *Compiler) fits(ctx *Context) bool {
	return len(ctx.Inputs) <= c.arch.ABI().MaxArgs() &&
		len(ctx.Stack) <= c.arch.ABI().MaxReturns()
}

func seedInputs(ctx *Context, n int) {
	for i := 0; i < n; i++ {
		v := ctx.Assembler.Reg(asm.RegTypeInt, asm.Width64)
		ctx.Inputs = append(ctx.Inputs, v)
		ctx.Stack = append(ctx.Stack, v)
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

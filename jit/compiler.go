package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/instr"
	"github.com/siyul-park/minivm/types"
)

// Compiler is the top-level driver. It owns the executable buffer, the slot
// table for direct-BL targets, and the cutoff that controls when partial
// segments are kept vs. discarded.
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
	arch    asm.Arch
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
	if cfg.lowerer != nil && cfg.arch == nil {
		cfg.arch = cfg.lowerer.Arch()
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
		arch:    cfg.arch,
		buffer:  cfg.buffer,
		data:    cfg.data,
		cutoff:  cfg.cutoff,
	}, nil
}

// Buffer exposes the executable buffer for callers that need to participate
// in linking (for example, when emitting auxiliary trampolines).
func (c *Compiler) Buffer() *asm.Buffer { return c.buffer }

// Data exposes the writable data region.
func (c *Compiler) Data() *asm.Data { return c.data }

// Slots returns the indirection table backing direct-BL CALL lowering.
// The slot table is lazily created on first use; if no fallback has been
// installed yet, Slots returns nil.
func (c *Compiler) Slots() *Slots { return c.slots }

// SetSlots installs the slot table the Compiler should hand to lowerers.
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
	if c.lowerer == nil {
		return emptyModule(addr, fn), nil
	}
	if fn == nil || len(fn.Code) == 0 {
		return emptyModule(addr, fn), nil
	}

	seg, ok, err := c.compileSegment(fn, 0, snap)
	if err != nil {
		return nil, err
	}
	if !ok {
		return emptyModule(addr, fn), nil
	}

	callables, err := asm.Link(c.buffer, c.arch, []*asm.Code{seg}, nil)
	if err != nil {
		return nil, err
	}

	mod := emptyModule(addr, fn)
	mod.Segments[0] = callables[0]
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
	a := asm.New(c.arch)
	scratch := c.arch.ABI().Scratch()

	ctx := &Context{
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

	lowered := 0
	for ctx.IP < ctx.End {
		op := instr.Opcode(fn.Code[ctx.IP])
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

	if lowered < c.cutoff {
		return nil, false, nil
	}

	c.lowerer.Exit(ctx, ctx.IP)

	sig := asm.Signature{
		Args:    nil,
		Returns: nil,
		Scratch: scratch,
	}
	code, err := a.Build(sig)
	if err != nil {
		return nil, false, err
	}
	return code, true, nil
}

func emptyModule(addr int, fn *types.Function) *Module {
	return &Module{
		Addr:        addr,
		Segments:    map[int]asm.Callable{},
		ParamKinds:  paramKinds(fn),
		ReturnKinds: returnKinds(fn),
	}
}

func paramKinds(fn *types.Function) []types.Kind {
	if fn == nil || fn.Typ == nil {
		return nil
	}
	kinds := make([]types.Kind, len(fn.Typ.Params))
	for i, p := range fn.Typ.Params {
		kinds[i] = p.Kind()
	}
	return kinds
}

func returnKinds(fn *types.Function) []types.Kind {
	if fn == nil || fn.Typ == nil {
		return nil
	}
	kinds := make([]types.Kind, len(fn.Typ.Returns))
	for i, r := range fn.Typ.Returns {
		kinds[i] = r.Kind()
	}
	return kinds
}

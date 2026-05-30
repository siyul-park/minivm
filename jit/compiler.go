package jit

import (
	"github.com/siyul-park/minivm/asm"
	"github.com/siyul-park/minivm/types"
)

// Compiler is the top-level driver. It owns the executable buffer, the slot
// table for direct-BL targets, and the cutoff that controls when partial
// segments are kept vs. discarded.
type Compiler struct {
	buffer *asm.Buffer
	data   *asm.Data
	arch   asm.Arch
	cutoff int
}

type config struct {
	buffer *asm.Buffer
	data   *asm.Data
	arch   asm.Arch
	cutoff int
}

// Option mutates the Compiler's config at construction time.
type Option func(*config)

// WithBuffer overrides the default executable buffer.
func WithBuffer(b *asm.Buffer) Option { return func(c *config) { c.buffer = b } }

// WithData overrides the default writable data region used for slots.
func WithData(d *asm.Data) Option { return func(c *config) { c.data = d } }

// WithArch overrides the architecture the compiler targets. By default the
// compiler picks the Lowerer registered for runtime.GOARCH.
func WithArch(a asm.Arch) Option { return func(c *config) { c.arch = a } }

// WithCutoff sets the minimum number of opcodes a partial segment must
// emit before being installed.
func WithCutoff(n int) Option { return func(c *config) { c.cutoff = n } }

// New constructs a Compiler.
func New(opts ...Option) (*Compiler, error) {
	cfg := config{
		cutoff: 8,
	}
	for _, o := range opts {
		o(&cfg)
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
		buffer: cfg.buffer,
		data:   cfg.data,
		arch:   cfg.arch,
		cutoff: cfg.cutoff,
	}, nil
}

// Buffer exposes the executable buffer for callers that need to participate
// in linking (for example, when emitting auxiliary trampolines).
func (c *Compiler) Buffer() *asm.Buffer { return c.buffer }

// Data exposes the writable data region.
func (c *Compiler) Data() *asm.Data { return c.data }

// Close releases the underlying buffer and data region.
func (c *Compiler) Close() error {
	if err := c.buffer.Free(); err != nil {
		return err
	}
	return c.data.Free()
}

// Compile lowers fn into native code. The Lowerer rejects everything for
// now; the returned Module has nil Entry and an empty Segments map. Callers
// must treat that as "JIT did not produce anything" and stay on the
// threaded path.
func (c *Compiler) Compile(fn *types.Function, addr int) (*Module, error) {
	_ = Active()
	return &Module{
		Addr:        addr,
		Segments:    map[int]asm.Callable{},
		ParamKinds:  paramKinds(fn),
		ReturnKinds: returnKinds(fn),
	}, nil
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
